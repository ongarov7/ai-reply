import XCTest
@testable import AIReply

/// «Чат с оператором» against an imitated server: pages, polling, sending
/// with retry, read marks and the unread badge (contract §3.7).
///
/// Қолдау чаты: беттер, жіберу, қайталау, оқылмағандар.
@MainActor
final class SupportChatModelTests: XCTestCase {

    private actor FakeSupport: SupportTransport {
        var pages: [SupportAPI.Cursor: SupportAPI.Page] = [:]
        var conversationValue: SupportAPI.Conversation?
        var sendFailures: [APIError] = []
        var unreadAfterRead = 0
        private(set) var cursors: [SupportAPI.Cursor] = []
        private(set) var sent: [(text: String, id: String)] = []
        private(set) var readUpTo: [String] = []
        private(set) var conversationCalls = 0
        private var holdsSends = false
        private var held: CheckedContinuation<Void, Never>?
        private var serverID = 0

        func setPage(_ cursor: SupportAPI.Cursor, _ messages: [SupportAPI.Message], hasMore: Bool = false) {
            pages[cursor] = SupportAPI.Page(messages: messages, hasMore: hasMore)
        }
        func setSendFailures(_ failures: [APIError]) { sendFailures = failures }
        func setConversation(_ value: SupportAPI.Conversation?) { conversationValue = value }
        func setUnreadAfterRead(_ value: Int) { unreadAfterRead = value }
        func holdSends(_ hold: Bool) { holdsSends = hold }
        var isHoldingASend: Bool { held != nil }
        func releaseSend() {
            held?.resume()
            held = nil
        }

        func conversation() async throws -> SupportAPI.Conversation? {
            conversationCalls += 1
            return conversationValue
        }

        func messages(_ cursor: SupportAPI.Cursor, limit: Int) async throws -> SupportAPI.Page {
            cursors.append(cursor)
            return pages[cursor] ?? SupportAPI.Page(messages: [], hasMore: false)
        }

        func send(text: String, clientMessageID: String) async throws -> SupportAPI.Message {
            sent.append((text, clientMessageID))
            if holdsSends { await withCheckedContinuation { held = $0 } }
            if !sendFailures.isEmpty { throw sendFailures.removeFirst() }
            serverID += 1
            return SupportAPI.Message(id: "s\(serverID)", sender: "user", text: text,
                                      createdAt: "2026-10-20T10:01:00Z", readAt: nil)
        }

        func markRead(upTo messageID: String) async throws -> Int {
            readUpTo.append(messageID)
            return unreadAfterRead
        }
    }

    private static func message(_ id: String, user: Bool, text: String = "text", read: Bool = false) -> SupportAPI.Message {
        SupportAPI.Message(id: id, sender: user ? "user" : "operator", text: text, createdAt: "2026-10-20T10:00:00Z",
                           readAt: read ? "2026-10-20T10:05:00Z" : nil)
    }

    private func message(_ id: String, user: Bool, text: String = "text", read: Bool = false) -> SupportAPI.Message {
        Self.message(id, user: user, text: text, read: read)
    }

    private func waitUntil(_ condition: @escaping () async -> Bool) async throws {
        for _ in 0..<200 {
            if await condition() { return }
            try await Task.sleep(for: .milliseconds(5))
        }
        XCTFail("timed out")
    }

    // MARK: Pages

    func testOpensOnTheNewestPageAndMarksTheReplyRead() async {
        let server = FakeSupport()
        await server.setPage(.latest, [message("u1", user: true), message("o1", user: false)], hasMore: true)
        let inbox = SupportInbox(transport: server)
        let model = SupportChatModel(transport: server, inbox: inbox)

        await model.load()

        XCTAssertEqual(model.items.map(\.id), ["u1", "o1"])
        XCTAssertTrue(model.hasOlder)
        XCTAssertEqual(model.loadState, .loaded)
        let read = await server.readUpTo
        XCTAssertEqual(read, ["o1"])
        XCTAssertEqual(inbox.unreadCount, 0)
    }

    func testOlderPagesComeFirstWithoutDuplicates() async {
        let server = FakeSupport()
        await server.setPage(.latest, [message("u1", user: true), message("o1", user: false, read: true)], hasMore: true)
        await server.setPage(.before("u1"), [message("o0", user: false, read: true), message("u1", user: true)])
        let model = SupportChatModel(transport: server)
        await model.load()

        await model.loadOlder()

        XCTAssertEqual(model.items.map(\.id), ["o0", "u1", "o1"])
        XCTAssertFalse(model.hasOlder)
        let read = await server.readUpTo
        XCTAssertTrue(read.isEmpty, "read already: nothing to mark")
        await model.loadOlder()
        let cursors = await server.cursors
        XCTAssertEqual(cursors, [.latest, .before("u1")], "no page past the first")
    }

    /// What arrived since the newest message, appended; each new reply is
    /// marked read once - and only while the chat is in front.
    func testPollingAppendsWhatArrived() async {
        let server = FakeSupport()
        await server.setPage(.latest, [message("u1", user: true)])
        await server.setPage(.after("u1"), [message("o2", user: false)])
        let model = SupportChatModel(transport: server)
        await model.load()

        model.isActive = false
        await model.poll()
        XCTAssertEqual(model.items.map(\.id), ["u1", "o2"])
        var read = await server.readUpTo
        XCTAssertTrue(read.isEmpty, "not marked read in the background")

        model.isActive = true
        await model.poll()
        await model.poll()
        read = await server.readUpTo
        XCTAssertEqual(read, ["o2"])
        let cursors = await server.cursors
        XCTAssertEqual(cursors, [.latest, .after("u1"), .after("o2"), .after("o2")])
    }

    func testAChatWithNothingYetIsEmpty() async {
        let server = FakeSupport()
        let model = SupportChatModel(transport: server)
        XCTAssertFalse(model.isEmpty, "not before it loaded")
        await model.load()
        XCTAssertTrue(model.isEmpty)
        XCTAssertFalse(model.canSend)
        model.draft = "   "
        XCTAssertFalse(model.canSend)
        model.draft = String(repeating: "a", count: SupportChatModel.maximumLength + 1)
        XCTAssertFalse(model.canSend)
        model.draft = "Здравствуйте"
        XCTAssertTrue(model.canSend)
    }

    // MARK: Sending

    func testAMessageIsSendingThenSent() async throws {
        let server = FakeSupport()
        await server.holdSends(true)
        let model = SupportChatModel(transport: server)
        await model.load()
        model.draft = "  Не вижу ответы  "

        let sending = Task { await model.send() }
        try await waitUntil { await server.isHoldingASend }
        XCTAssertEqual(model.draft, "")
        XCTAssertEqual(model.items.last?.text, "Не вижу ответы")
        XCTAssertEqual(model.items.last?.delivery, .sending)

        await server.releaseSend()
        await sending.value
        XCTAssertEqual(model.items.map(\.id), ["s1"])
        XCTAssertEqual(model.items.last?.delivery, .sent)
    }

    /// A failed message stays, marked, and Retry sends it with the same
    /// client message id, so the server can never keep it twice.
    func testRetrySendsTheSameMessageAgain() async throws {
        let server = FakeSupport()
        await server.setSendFailures([.offline])
        let model = SupportChatModel(transport: server)
        await model.load()
        model.draft = "Вопрос"

        await model.send()
        let failed = try XCTUnwrap(model.items.last)
        XCTAssertEqual(failed.delivery, .failed)
        XCTAssertEqual(model.sendErrorKey, "support.error.send")

        await model.retry(failed.id)
        let sent = await server.sent
        XCTAssertEqual(sent.count, 2)
        XCTAssertEqual(sent[0].id, sent[1].id)
        XCTAssertEqual(sent[0].id, failed.id)
        XCTAssertEqual(model.items.map(\.delivery), [.sent])
        XCTAssertNil(model.sendErrorKey)
    }

    func testTooManyMessagesAreSaidSo() async {
        let server = FakeSupport()
        await server.setSendFailures([.rateLimited(retryAfter: 30)])
        let model = SupportChatModel(transport: server)
        await model.load()
        model.draft = "Ещё вопрос"
        await model.send()
        XCTAssertEqual(model.sendErrorKey, "support.error.rateLimited")
    }

    /// The server kept the message before its answer came back, and a poll
    /// brought it: it is shown once.
    func testOwnMessageFromAPollIsShownOnce() async throws {
        let server = FakeSupport()
        await server.setPage(.latest, [message("o1", user: false, read: true)])
        await server.setPage(.after("o1"), [message("s1", user: true, text: "Спасибо")])
        await server.holdSends(true)
        let model = SupportChatModel(transport: server)
        await model.load()
        model.draft = "Спасибо"

        let sending = Task { await model.send() }
        try await waitUntil { await server.isHoldingASend }
        await model.poll()
        XCTAssertEqual(model.items.map(\.id), ["o1", "s1"])

        await server.releaseSend()
        await sending.value
        XCTAssertEqual(model.items.map(\.id), ["o1", "s1"])
    }

    // MARK: Badge

    func testTheBadgeFollowsTheServer() async throws {
        let server = FakeSupport()
        await server.setConversation(SupportAPI.Conversation(id: "c1", status: "open", unreadCount: 2))
        let inbox = SupportInbox(transport: server)
        inbox.accountDidChange(isSignedIn: true, hasAcceptedLegal: true,
                               features: try? JSONDecoder().decode(AccountAPI.Features.self, from: Data("{}".utf8)))
        XCTAssertFalse(inbox.isAvailable, "a server without the chat")
        await inbox.refresh()
        var calls = await server.conversationCalls
        XCTAssertEqual(calls, 0)

        let features = try? JSONDecoder().decode(AccountAPI.Features.self, from: Data(#"{"support_chat": true}"#.utf8))
        inbox.accountDidChange(isSignedIn: true, hasAcceptedLegal: false, features: features)
        XCTAssertFalse(inbox.isAvailable, "the server asks for the consent")
        inbox.accountDidChange(isSignedIn: true, hasAcceptedLegal: true, features: features)
        XCTAssertTrue(inbox.isAvailable)
        try await waitUntil { inbox.unreadCount == 2 }
        calls = await server.conversationCalls
        XCTAssertGreaterThanOrEqual(calls, 1)

        await server.setConversation(nil)
        await inbox.refresh()
        XCTAssertEqual(inbox.unreadCount, 0, "no conversation yet")

        inbox.accountDidChange(isSignedIn: false, hasAcceptedLegal: true, features: features)
        XCTAssertFalse(inbox.isAvailable)
    }

    /// A reply push over the open chat reads it there instead of a banner.
    func testAReplyPushOverTheChatIsReadThere() {
        let inbox = SupportInbox(transport: FakeSupport())
        let features = try? JSONDecoder().decode(AccountAPI.Features.self, from: Data(#"{"support_chat": true}"#.utf8))
        inbox.accountDidChange(isSignedIn: true, hasAcceptedLegal: true, features: features)
        var refreshed = 0
        inbox.onReplyWhileVisible = { refreshed += 1 }

        XCTAssertTrue(inbox.replyArrived(), "elsewhere in the app: the banner")
        XCTAssertEqual(refreshed, 0)
        inbox.isChatVisible = true
        XCTAssertFalse(inbox.replyArrived())
        XCTAssertEqual(refreshed, 1)
    }

    func testTheServersMessagesAreRead() throws {
        let page = try JSONDecoder().decode(SupportAPI.Page.self, from: Data("""
        {"messages": [{"id": "m1", "sender": "operator", "text": "Здравствуйте!", "created_at": "2026-10-20T10:00:00Z",
                       "read_at": null}], "has_more": false}
        """.utf8))
        XCTAssertEqual(page.messages.first?.isFromUser, false)
        XCTAssertNil(page.messages.first?.readAt)
        XCTAssertFalse(page.hasMore)
    }
}
