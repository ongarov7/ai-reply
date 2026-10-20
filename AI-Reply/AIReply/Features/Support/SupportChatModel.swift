import Foundation
import Observation

/// The support entry's state: whether the chat exists for this account, and
/// how many operator messages wait unread.
///
/// Қолдау чаты: бар ма, неше жауап оқылмаған.
///
/// One instance for the process (`AppServices`). The unread count is read
/// from the server on every return to the app and when a `support_reply`
/// push arrives; the open chat lowers it as it marks messages read.
@MainActor
@Observable
final class SupportInbox {

    private(set) var unreadCount = 0
    /// `features.support_chat`, a signed-in account and the current consent
    /// (the server asks for it on every support request).
    private(set) var isAvailable = false

    /// The chat is on screen: a reply push refreshes it instead of a banner.
    @ObservationIgnored var isChatVisible = false
    /// Set by the open chat: a reply arrived, read it now.
    @ObservationIgnored var onReplyWhileVisible: (() -> Void)?

    @ObservationIgnored let transport: SupportTransport
    @ObservationIgnored private var isRefreshing = false

    init(transport: SupportTransport = BackendSupportTransport()) {
        self.transport = transport
    }

    func accountDidChange(isSignedIn: Bool, hasAcceptedLegal: Bool, features: AccountAPI.Features?) {
        let available = isSignedIn && hasAcceptedLegal && features?.supportChat == true
        guard available != isAvailable else { return }
        isAvailable = available
        if available {
            Task { await refresh() }
        } else {
            unreadCount = 0
        }
    }

    /// `GET /api/v1/support/conversation`: on foreground and after a reply push.
    func refresh() async {
        guard isAvailable, !isRefreshing else { return }
        isRefreshing = true
        defer { isRefreshing = false }
        do {
            let conversation = try await transport.conversation()
            unreadCount = max(0, conversation?.unreadCount ?? 0)
        } catch {
            // Offline or refused: the badge stays as it was.
        }
    }

    /// The chat marked messages read; what the server says is left.
    func didRead(unreadCount: Int) {
        self.unreadCount = max(0, unreadCount)
    }

    /// A `support_reply` push. Whether iOS should show its banner: not over
    /// the chat itself, which reads the reply at once.
    func replyArrived() -> Bool {
        guard isAvailable else { return true }
        if isChatVisible, let onReplyWhileVisible {
            onReplyWhileVisible()
            return false
        }
        Task { await refresh() }
        return true
    }
}

/// «Чат с оператором»: the conversation, older pages on scroll, sending with
/// per-message states, and polling while it is on screen.
///
/// Чат моделі: тарих, жіберу/қате/қайталау, экранда тұрғанда ғана сұрау.
///
/// The server's messages are kept in the server's order (oldest first);
/// messages being sent, or that failed, follow them. A message is sent with
/// a `client_message_id` made once, so Retry can never post it twice. The
/// text of a message is never logged or stored on the device.
@MainActor
@Observable
final class SupportChatModel {

    struct Item: Identifiable, Equatable {
        enum Delivery: Equatable {
            case sent
            case sending
            /// Retry can send it.
            case failed
            /// The server will not take it as it is; Retry would not help.
            case refused
        }

        /// The server's id, or the client message id while unsent.
        let id: String
        let isFromUser: Bool
        let text: String
        let date: Date
        var delivery: Delivery
    }

    enum LoadState: Equatable {
        case idle, loading, loaded, failed
    }

    nonisolated static let pageSize = 30
    /// The server's limit, in Unicode code points (`draftLength`).
    nonisolated static let maximumLength = 2000
    nonisolated static let pollInterval: Duration = .seconds(5)

    private(set) var items: [Item] = []
    private(set) var loadState: LoadState = .idle
    private(set) var hasOlder = false
    private(set) var isLoadingOlder = false
    /// Why the last send failed, as a catalog key.
    private(set) var sendErrorKey: String?
    var draft = ""

    /// The app is in the foreground with the chat on screen: polling and
    /// marking read happen only then.
    @ObservationIgnored var isActive = true

    @ObservationIgnored private var server: [SupportAPI.Message] = []
    @ObservationIgnored private var unsent: [Item] = []
    @ObservationIgnored private let transport: SupportTransport
    @ObservationIgnored private weak var inbox: SupportInbox?
    @ObservationIgnored private let now: () -> Date
    @ObservationIgnored private var lastReadMessageID: String?
    @ObservationIgnored private var isPolling = false

    init(transport: SupportTransport, inbox: SupportInbox? = nil, now: @escaping () -> Date = Date.init) {
        self.transport = transport
        self.inbox = inbox
        self.now = now
    }

    var trimmedDraft: String { draft.trimmingCharacters(in: .whitespacesAndNewlines) }

    /// The draft's length as the server counts it: code points, not the
    /// characters on screen (a family emoji is one character, and seven).
    var draftLength: Int { trimmedDraft.unicodeScalars.count }

    var canSend: Bool {
        let count = draftLength
        return loadState == .loaded && count > 0 && count <= Self.maximumLength
    }

    var isEmpty: Bool { loadState == .loaded && items.isEmpty }

    // MARK: Loading

    /// The newest page, when the chat opens. Opened again, it only reads
    /// what arrived since.
    func load() async {
        if loadState == .loaded {
            await poll()
            return
        }
        guard loadState != .loading else { return }
        loadState = .loading
        do {
            let page = try await transport.messages(.latest, limit: Self.pageSize)
            server = page.messages
            hasOlder = page.hasMore
            loadState = .loaded
            rebuild()
            await markReadIfNeeded()
        } catch {
            loadState = .failed
        }
    }

    /// The page before the oldest message shown: when it scrolls into view.
    func loadOlder() async {
        guard loadState == .loaded, hasOlder, !isLoadingOlder, let oldest = server.first else { return }
        isLoadingOlder = true
        defer { isLoadingOlder = false }
        guard let page = try? await transport.messages(.before(oldest.id), limit: Self.pageSize) else { return }
        let known = Set(server.map(\.id))
        server = page.messages.filter { !known.contains($0.id) } + server
        hasOlder = page.hasMore
        rebuild()
    }

    /// What arrived since the newest message: every few seconds while the
    /// chat is on screen, and at once for a reply push.
    func poll() async {
        guard loadState == .loaded, !isPolling else { return }
        isPolling = true
        defer { isPolling = false }
        do {
            guard let newest = server.last else {
                // Nothing yet: the conversation may have started elsewhere.
                let page = try await transport.messages(.latest, limit: Self.pageSize)
                merge(newer: page.messages)
                hasOlder = page.hasMore
                rebuild()
                await markReadIfNeeded()
                return
            }
            var cursor = newest.id
            for _ in 0..<5 {
                let page = try await transport.messages(.after(cursor), limit: Self.pageSize)
                merge(newer: page.messages)
                guard page.hasMore, let last = page.messages.last else { break }
                cursor = last.id
            }
            rebuild()
            await markReadIfNeeded()
        } catch {
            // The next poll tries again; nothing on screen changes.
        }
    }

    /// Runs `poll` every `pollInterval` until the calling task is cancelled
    /// (the chat left the screen), skipping turns while in the background.
    func pollWhileVisible(every interval: Duration = SupportChatModel.pollInterval) async {
        while !Task.isCancelled {
            try? await Task.sleep(for: interval)
            guard !Task.isCancelled else { return }
            if isActive { await poll() }
        }
    }

    private func merge(newer messages: [SupportAPI.Message]) {
        let known = Set(server.map(\.id))
        let fresh = messages.filter { !known.contains($0.id) }
        server += fresh
        // Our own message saved before its answer came back: it is here now,
        // so the copy still marked "sending" goes.
        for message in fresh where message.isFromUser {
            if let index = unsent.firstIndex(where: { $0.delivery == .sending && $0.text == message.text }) {
                unsent.remove(at: index)
            }
        }
    }

    // MARK: Sending

    func send() async {
        let text = trimmedDraft
        guard canSend else { return }
        draft = ""
        sendErrorKey = nil
        let item = Item(id: UUID().uuidString, isFromUser: true, text: text, date: now(), delivery: .sending)
        unsent.append(item)
        rebuild()
        await deliver(item.id)
    }

    /// The same message again, with the same client message id.
    func retry(_ id: String) async {
        guard let index = unsent.firstIndex(where: { $0.id == id }), unsent[index].delivery == .failed else { return }
        unsent[index].delivery = .sending
        sendErrorKey = nil
        rebuild()
        await deliver(id)
    }

    private func deliver(_ id: String) async {
        guard let item = unsent.first(where: { $0.id == id }) else { return }
        do {
            let saved = try await transport.send(text: item.text, clientMessageID: item.id)
            unsent.removeAll { $0.id == id }
            if !server.contains(where: { $0.id == saved.id }) { server.append(saved) }
        } catch {
            // Already arrived through a poll and taken off: nothing failed.
            guard let index = unsent.firstIndex(where: { $0.id == id }) else { return }
            switch error {
            // The text is the one thing in this request a user can get
            // wrong: longer than the server takes.
            case APIError.invalidRequest:
                unsent[index].delivery = .refused
                sendErrorKey = "support.error.tooLong"
            case APIError.rateLimited:
                unsent[index].delivery = .failed
                sendErrorKey = "support.error.rateLimited"
            default:
                unsent[index].delivery = .failed
                sendErrorKey = "support.error.send"
            }
        }
        rebuild()
    }

    // MARK: Read

    /// Marks the newest operator message read while the chat is on screen,
    /// once per message.
    private func markReadIfNeeded() async {
        guard isActive,
              let newest = server.last(where: { !$0.isFromUser }),
              newest.readAt == nil, newest.id != lastReadMessageID else { return }
        lastReadMessageID = newest.id
        if let unread = try? await transport.markRead(upTo: newest.id) {
            inbox?.didRead(unreadCount: unread)
        } else {
            lastReadMessageID = nil
        }
    }

    private func rebuild() {
        items = server.map { message in
            Item(id: message.id, isFromUser: message.isFromUser, text: message.text,
                 date: ServerTime.date(message.createdAt) ?? now(), delivery: .sent)
        } + unsent
    }
}
