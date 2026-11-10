import Foundation

// MARK: - Keyboard state

/// What the keyboard's composer is doing, for a command that would use it.
enum WatchComposerState: Equatable, Sendable {
    /// Closed.
    case idle
    /// A reply is open with no instruction typed yet: a command fills it in,
    /// keeping the message and persona already there.
    case replyDraft(ReplyContext?)
    /// Create is open with nothing typed yet.
    case composeDraft
    /// The user has work on screen. A command must not take it away.
    case busy
}

/// What the keyboard's row and strip should show.
struct WatchVoiceUI: Equatable, Sendable {
    var indicator: WatchKeyboardIndicator = .hidden
    var strip: WatchVoiceStrip?
}

enum WatchVoiceStrip: Equatable, Sendable {
    case waiting
    case openWatchApp
    case installWatchApp
    case connectionLost
    case fullAccessRequired
    case generating
    case instructionReceived(preview: String)
    case responseReady(preview: String)
    case failure(WatchVoiceFailure)
}

/// What the coordinator asks of the keyboard. Everything here is UIKit, the
/// host field or the composer, none of which a test has.
@MainActor
protocol WatchVoiceHost: AnyObject {
    var watchHasFullAccess: Bool { get }
    var watchComposerState: WatchComposerState { get }
    /// The host field right now, read from the proxy.
    func watchInsertContext(sessionID: String) -> WatchInsertContext
    /// The existing reader: selection first, then the copied text.
    func watchReadCopiedMessage() -> Result<ReplyContext, AIReplyError>
    /// The selection, or the text in the field, for a rewrite.
    func watchExistingText() -> String?
    /// `UIPasteboard.changeCount`: metadata, read without any prompt.
    var watchPasteboardChangeCount: Int { get }
    /// The text on screen in the composer, edits included.
    func watchCurrentDraft() -> String?
    /// Opens the composer on `plan` and starts the request.
    func watchStart(_ plan: WatchGenerationPlan, command: WatchVoiceCommand)
    /// Opens the reply composer with the instruction and an empty message.
    func watchOpenReplyForTypedMessage(instruction: String, command: WatchVoiceCommand)
    func watchCancelGeneration()
    /// `textDocumentProxy.insertText`, and nothing more.
    func watchInsert(_ text: String)
    /// Shows a saved text in the composer, with Insert.
    func watchShow(_ saved: WatchVoiceOutcome)
    /// The composer's session is no longer needed; the keyboard is off screen.
    func watchDiscardComposer()
    func watchUIDidChange(_ ui: WatchVoiceUI)
}

// MARK: - Coordinator

/// Watch Voice in the keyboard: its session, its inbox and what becomes of
/// each command.
///
/// Пернетақтадағы сағат режимі: команданы алады, тексереді, бір рет орындайды.
///
/// WHAT IT GUARANTEES, and the tests pin:
/// * a command is started at most once, by one keyboard (`store.claim`);
/// * text reaches a host field at most once (`.inserted` marker);
/// * automatic insert happens only if `WatchInsertPolicy` verifies the field;
///   anything else is saved for a manual "Insert Response";
/// * a command arriving with no keyboard for it never spends a reply or reads
///   the clipboard by itself: the user taps Generate first.
///
/// EFFICIENCY. Nothing here runs on a keystroke. It listens to a Darwin
/// notification while on screen. The only timer is a two-second re-read while
/// the user has explicitly started waiting, for three minutes at most, as the
/// safety net for a notification that was lost.
@MainActor
final class WatchVoiceCoordinator {

    /// How long Watch Voice waits after the user taps its control.
    static let waitLimit: TimeInterval = 3 * 60
    static let waitInterval: TimeInterval = 2

    weak var host: WatchVoiceHost?

    private let store: WatchVoiceStore
    private let settings: SharedSettings
    private let limits: () -> WatchVoiceRouter.Limits
    private let now: @Sendable () -> Date
    private let post: @Sendable (WatchVoiceSignal) -> Void
    private let observe: (@escaping @Sendable (WatchVoiceSignal) -> Void) -> AnyObject?
    private let sleep: @Sendable (TimeInterval) async -> Void

    private(set) var session: WatchKeyboardSession?
    private(set) var ui = WatchVoiceUI()
    private(set) var isWaiting = false
    private(set) var isVisible = false

    private var link: WatchLinkSnapshot?
    private var observer: AnyObject?
    private var waitTask: Task<Void, Never>?
    private var waitWasConnected = false
    /// Read when the user tapped Watch Voice: that tap is the gesture.
    private var armedMessage: Result<ReplyContext, AIReplyError>?
    /// The pasteboard as it was then: something copied since is read anew.
    private var armedChangeCount = 0

    /// The copied message as read for a persona's screen on the watch, kept
    /// so a command sent from that screen answers exactly what it showed.
    private struct Preview {
        let id: String
        let sessionID: String
        let copied: Result<ReplyContext, AIReplyError>
        let changeCount: Int
        /// The app's last ask this answered: each ask is answered once.
        let answered: Date
    }
    private var preview: Preview?

    private struct Active {
        let command: WatchVoiceCommand
        /// The field at the start; nil for a command that may not auto-insert.
        let bound: WatchInsertContext?
        let kind: WatchVoiceOutcome.Kind
    }

    /// The command being generated. May outlive the keyboard being on screen.
    private var active: Active?
    /// A command's text is in the composer and not inserted yet.
    private var awaitingInsert: String?
    /// A command that needs the user's tap, or for the composer to be free.
    private var held: WatchVoiceCommand?
    private var saved: WatchVoiceOutcome?
    private var notice: WatchVoiceStrip?
    private var isReconciling = false
    private var reconcileAgain = false

    init(
        store: WatchVoiceStore = .shared,
        settings: SharedSettings = .shared,
        limits: @escaping () -> WatchVoiceRouter.Limits = {
            WatchVoiceRouter.Limits(replyInstruction: ReplyInstruction.maximumCharacters,
                                    composeInstruction: AILimits.current.instructionCharacters)
        },
        now: @escaping @Sendable () -> Date = { Date() },
        post: @escaping @Sendable (WatchVoiceSignal) -> Void = { $0.post() },
        observe: @escaping (@escaping @Sendable (WatchVoiceSignal) -> Void) -> AnyObject? = {
            WatchVoiceSignalObserver(signals: [.command, .link, .preview], handler: $0)
        },
        sleep: @escaping @Sendable (TimeInterval) async -> Void = { try? await Task.sleep(for: .seconds($0)) }
    ) {
        self.store = store
        self.settings = settings
        self.limits = limits
        self.now = now
        self.post = post
        self.observe = observe
        self.sleep = sleep
    }

    /// Whether the composer on screen belongs to a command: it is then not
    /// parked like the user's own work when the keyboard goes away.
    var ownsComposer: Bool { active != nil || awaitingInsert != nil }

    /// A command's request is running.
    var hasGenerationInFlight: Bool { active != nil }

    // MARK: Keyboard lifecycle

    func keyboardDidAppear() async {
        guard !isVisible else { return }
        isVisible = true
        beginSession()
        observer = observe { [weak self] _ in
            Task { @MainActor in await self?.reconcile() }
        }
        await reconcile()
    }

    /// Off screen: nothing keeps running for the keyboard's sake, except a
    /// request already on its way, whose answer is saved.
    ///
    /// A request on its way is stopped and the command given back: iOS may
    /// make a new keyboard for the next appearance, and the answer must not
    /// land in one nobody holds. The next keyboard offers it again, and runs
    /// it with the same key, so the server does not charge twice.
    func keyboardWillDisappear() {
        guard isVisible else { return }
        isVisible = false
        observer = nil
        stopWaiting()
        held = nil
        notice = nil
        preview = nil
        endSession()
        if let running = active {
            active = nil
            host?.watchCancelGeneration()
            store.release(running.command.id)
            write(running.command.id, status: .queued)
            host?.watchDiscardComposer()
        }
        if let id = awaitingInsert {
            awaitingInsert = nil
            // What is on screen, edits and later versions included.
            if let draft = host?.watchCurrentDraft()?.trimmingCharacters(in: .whitespacesAndNewlines), !draft.isEmpty,
               var outcome = store.outcome(for: id) {
                outcome.text = draft
                outcome.updatedAt = now()
                store.write(outcome)
            }
            host?.watchDiscardComposer()
        }
        publishUI()
    }

    /// The caret moved to another field: what was bound to the old one no
    /// longer matches, which is the point.
    func hostFieldDidChange() {
        guard isVisible else { return }
        beginSession()
    }

    private func beginSession() {
        let stamp = now()
        let context = host?.watchInsertContext(sessionID: "")
        let next = WatchKeyboardSession(
            id: WatchVoiceIdentifier.make(),
            documentID: context?.documentID?.uuidString,
            startedAt: stamp,
            updatedAt: stamp,
            isActive: true,
            isWaiting: isWaiting,
            hasFullAccess: host?.watchHasFullAccess ?? false
        )
        session = next
        publish(next)
    }

    private func endSession() {
        guard var ended = session else { return }
        ended.isActive = false
        ended.isWaiting = false
        ended.updatedAt = now()
        session = ended
        publish(ended)
    }

    private func publish(_ session: WatchKeyboardSession) {
        // Off the main thread, and only with Full Access: without it the App
        // Group is read-only and the write would not land.
        guard host?.watchHasFullAccess == true else { return }
        let store = self.store
        let post = self.post
        Task.detached(priority: .utility) {
            // Without a paired watch nobody will ever read this.
            guard store.readLink()?.isPaired == true else { return }
            store.writeSession(session)
            post(.session)
        }
    }

    // MARK: Reading the store

    /// Looks in the App Group: at appearance, on a signal, while waiting.
    func reconcile() async {
        guard isVisible else { return }
        if isReconciling {
            reconcileAgain = true
            return
        }
        isReconciling = true
        defer { isReconciling = false }
        repeat {
            reconcileAgain = false
            let store = self.store
            let stamp = now()
            let state = await Task.detached(priority: .userInitiated) { () -> StoreState in
                store.purge(now: stamp)
                return StoreState(link: store.readLink(),
                                  pending: store.pendingCommands(now: stamp),
                                  saved: store.savedResults(now: stamp),
                                  previewAsk: store.readPreviewRequest())
            }.value
            guard isVisible else { return }
            apply(state)
        } while reconcileAgain
    }

    private struct StoreState: Sendable {
        let link: WatchLinkSnapshot?
        let pending: [WatchVoiceCommand]
        let saved: [WatchVoiceOutcome]
        let previewAsk: WatchPreviewRequest?
    }

    private func apply(_ state: StoreState) {
        link = state.link
        answerPreview(state.previewAsk)
        let stamp = now()
        if let running = active, store.has(.cancelled, for: running.command.id) {
            active = nil
            host?.watchCancelGeneration()
            finish(running.command.id, status: .cancelled)
        }
        if let current = held, current.isExpired(now: stamp) || store.has(.cancelled, for: current.id) {
            held = nil
        }
        // The text in the composer is not also offered in the strip.
        saved = state.saved.first { $0.id != awaitingInsert }
        if active == nil, awaitingInsert == nil {
            // One meant for this keyboard goes first; one held back only
            // because the composer was busy is tried again.
            let candidate = state.pending.first { $0.targetSessionID == session?.id }
                ?? held.flatMap { current in state.pending.first { $0.id == current.id } }
                ?? state.pending.first
            if let candidate { accept(candidate) }
        }
        publishUI()
    }

    // MARK: Commands

    private func accept(_ command: WatchVoiceCommand) {
        guard let host, let session else { return }
        // Meant for this keyboard, just confirmed, and the user is free:
        // start. Otherwise it waits for their tap - it was never asked for
        // here and now.
        let isLive = now().timeIntervalSince(command.createdAt) <= WatchVoice.liveCommandWindow
        guard isLive, command.targetSessionID == session.id, answersPreview(command),
              host.watchComposerState != .busy, host.watchHasFullAccess else {
            held = command
            return
        }
        start(command, bindsField: true)
    }

    private func start(_ command: WatchVoiceCommand, bindsField: Bool) {
        guard let host, let session else { return }
        guard host.watchHasFullAccess else {
            notice = .fullAccessRequired
            publishUI()
            return
        }
        // After this, every other delivery of this command is a duplicate.
        guard store.claim(command.id, now: now()) else {
            if held?.id == command.id { held = nil }
            return
        }
        held = nil
        notice = nil
        write(command.id, status: .received)

        let composer = host.watchComposerState
        var copied: Result<ReplyContext, AIReplyError>?
        var existing: String?
        if command.options.mode == .reply {
            if let shown = shownPreview(for: command) {
                // What the watch showed is what gets answered.
                copied = shown
            } else if case .replyDraft(let context?) = composer {
                copied = .success(context)
            } else if let armedMessage, host.watchPasteboardChangeCount == armedChangeCount {
                copied = armedMessage
            } else {
                // The one read of the clipboard for this command: the user
                // just confirmed "reply to the copied message" on the watch.
                copied = host.watchReadCopiedMessage()
            }
        } else if command.options.mode.editsExistingText {
            existing = host.watchExistingText()
        }
        stopWaiting()

        let plan = WatchVoiceRouter.plan(for: command, copied: copied, existingText: existing, limits: limits())
        switch plan {
        case .refused(let failure, let limit):
            write(command.id, status: .failed, failure: failure, failureLimit: limit)
            store.removeCommand(command.id)
            notice = .failure(failure)
            publishUI()
        case .needsCopiedMessage(let instruction):
            write(command.id, status: .failed, failure: .copiedMessageUnreadable)
            store.removeCommand(command.id)
            host.watchOpenReplyForTypedMessage(instruction: instruction, command: command)
            publishUI()
        case .reply, .compose:
            let kind: WatchVoiceOutcome.Kind
            if case .reply = plan { kind = .reply } else { kind = .compose }
            active = Active(command: command,
                            bound: bindsField ? host.watchInsertContext(sessionID: session.id) : nil,
                            kind: kind)
            write(command.id, status: .generating)
            publishUI()
            host.watchStart(plan, command: command)
        }
    }

    /// The request started by `watchStart` is over, however it went.
    func generationDidFinish(_ result: Result<String, AIReplyError>) {
        guard let finished = active else { return }
        active = nil
        let id = finished.command.id

        switch result {
        case .failure(.cancelled):
            finish(id, status: .cancelled)
        case .failure(let error):
            // The composer shows it, with Retry where that can work.
            write(id, status: .failed, failure: WatchVoiceFailure(error), failureLimit: limit(of: error))
            store.removeCommand(id)
        case .success(let text):
            if store.has(.cancelled, for: id) {
                // Taken back on the watch while it was being written.
                finish(id, status: .cancelled)
            } else {
                deliver(text, finished)
            }
        }
        publishUI()
        Task { await reconcile() }
    }

    private func deliver(_ text: String, _ finished: Active) {
        let id = finished.command.id
        let current = isVisible ? session.flatMap { session in host?.watchInsertContext(sessionID: session.id) } : nil
        let decision = WatchInsertPolicy.decide(
            isAutoInsertEnabled: settings.watchAutoInsertEnabled,
            bound: finished.bound,
            current: current,
            alreadyInserted: store.has(.inserted, for: id)
        )
        if decision == .automatic, store.create(.inserted, for: id) {
            finish(id, status: .inserted)
            host?.watchInsert(text)
            return
        }
        // Saved first: the keyboard can be gone a moment from now.
        write(id, status: .ready, text: text, instruction: finished.command.instruction, kind: finished.kind)
        store.removeCommand(id)
        if isVisible {
            awaitingInsert = id
        } else {
            host?.watchDiscardComposer()
        }
    }

    // MARK: From the composer

    /// Insert in the composer: the text the user saw, edits included.
    func composerDidInsert() {
        guard let id = awaitingInsert else { return }
        awaitingInsert = nil
        store.create(.inserted, for: id)
        finish(id, status: .inserted)
        publishUI()
    }

    /// The composer closed. A command that started it is over; one held
    /// back while it was open can go now.
    func composerDidClose() {
        if let id = awaitingInsert {
            awaitingInsert = nil
            finish(id, status: .dismissed)
        }
        if let running = active {
            active = nil
            host?.watchCancelGeneration()
            finish(running.command.id, status: .cancelled)
        }
        publishUI()
        Task { await reconcile() }
    }

    // MARK: The copied message, for the watch

    /// The watch opened a persona's screen and asks what was copied. The
    /// keyboard on screen reads it, as a tap on Watch Voice would: the tap
    /// was on the watch. Each ask is answered once; the same copy is not read
    /// twice, so iOS asks about pasting at most once per copy.
    private func answerPreview(_ ask: WatchPreviewRequest?) {
        guard let ask, let host, let session, ask.sessionID == session.id,
              now().timeIntervalSince(ask.createdAt) <= WatchVoice.previewLifetime else { return }
        if let preview, preview.id == ask.id, preview.answered >= ask.createdAt { return }
        guard host.watchHasFullAccess else { return }

        let changeCount = host.watchPasteboardChangeCount
        let copied: Result<ReplyContext, AIReplyError>
        if case .replyDraft(let context?) = host.watchComposerState {
            // The reply open in the keyboard is the one a command answers.
            copied = .success(context)
        } else if let preview, preview.sessionID == session.id, preview.changeCount == changeCount {
            copied = preview.copied
        } else if let armedMessage, changeCount == armedChangeCount {
            copied = armedMessage
        } else {
            copied = host.watchReadCopiedMessage()
        }
        preview = Preview(id: ask.id, sessionID: session.id, copied: copied, changeCount: changeCount,
                          answered: ask.createdAt)
        store.writePreview(WatchPreviewRecord(id: ask.id, sessionID: session.id, createdAt: now(),
                                              preview: Self.watchPreview(of: copied)))
    }

    private static func watchPreview(of copied: Result<ReplyContext, AIReplyError>) -> WatchSourcePreview {
        switch copied {
        case .success(let context):         return .ready(context.text)
        case .failure(.noSourceMessage):    return WatchSourcePreview(state: .nothingCopied)
        case .failure(.pasteDenied):        return WatchSourcePreview(state: .pasteDenied)
        case .failure(.messageTooLong):     return WatchSourcePreview(state: .tooLong)
        case .failure:                      return WatchSourcePreview(state: .unavailable)
        }
    }

    /// The copied message a command was sent about, while it is still what
    /// this keyboard holds: the same session, and nothing copied since.
    private func shownPreview(for command: WatchVoiceCommand) -> Result<ReplyContext, AIReplyError>? {
        guard let id = command.previewID, let preview, let session, let host,
              preview.id == id, preview.sessionID == session.id,
              preview.changeCount == host.watchPasteboardChangeCount else { return nil }
        return preview.copied
    }

    /// A command sent from a persona's screen starts by itself only while
    /// the message that screen showed is still the one here. Otherwise it
    /// waits for a tap, and the composer shows which message it answers.
    private func answersPreview(_ command: WatchVoiceCommand) -> Bool {
        command.previewID == nil || shownPreview(for: command) != nil
    }

    // MARK: From the keyboard's row

    /// The Watch Voice control. The tap is the user's gesture: the copied
    /// message is read now, the way a persona tap reads it, and held in
    /// memory until a command uses it.
    func controlTapped() {
        guard let host else { return }
        notice = nil
        if isWaiting {
            stopWaiting()
            publishUI()
            return
        }
        guard host.watchHasFullAccess else {
            notice = .fullAccessRequired
            publishUI()
            return
        }
        switch link?.connectionState(now: now()) {
        case .appNotInstalled:
            notice = .installWatchApp
        case .appNotOpen, .connected:
            armedMessage = host.watchReadCopiedMessage()
            armedChangeCount = host.watchPasteboardChangeCount
            startWaiting()
        case .unsupported, .activating, .notPaired, .none:
            break
        }
        publishUI()
    }

    /// Generate or Insert Response in the strip.
    func stripPrimaryTapped() {
        if let current = held {
            guard !current.isExpired(now: now()), !store.has(.cancelled, for: current.id) else {
                held = nil
                publishUI()
                return
            }
            // A deliberate tap, but the field it was asked for is unknown.
            start(current, bindsField: false)
        } else if let result = saved {
            insertSaved(result)
        }
    }

    /// The strip's text: review the saved reply before inserting it.
    func stripPreviewTapped() {
        guard let result = saved else { return }
        saved = nil
        awaitingInsert = result.id
        host?.watchShow(result)
        publishUI()
    }

    /// The X dismisses what the strip shows, in the order it shows it.
    func stripDismissTapped() {
        if notice != nil {
            notice = nil
        } else if let current = held {
            held = nil
            store.create(.claimed, for: current.id)
            finish(current.id, status: .dismissed)
        } else if let result = saved {
            saved = nil
            finish(result.id, status: .dismissed)
        } else if isWaiting {
            stopWaiting()
        }
        publishUI()
    }

    /// The user opened their own composer (Create, a persona) over a
    /// command's. Clearing it stops its request without telling anyone, so
    /// the command is closed here.
    func composerReplaced() {
        guard awaitingInsert != nil || active != nil else { return }
        if let id = awaitingInsert {
            awaitingInsert = nil
            finish(id, status: .dismissed)
        }
        if let running = active {
            active = nil
            finish(running.command.id, status: .cancelled)
        }
        publishUI()
    }

    private func insertSaved(_ result: WatchVoiceOutcome) {
        guard let host, let session, let text = result.text else { return }
        guard host.watchInsertContext(sessionID: session.id).isFieldEmpty else {
            // Not over what is already there: the composer asks Replace, Add
            // or Cancel, as for any other reply.
            stripPreviewTapped()
            return
        }
        guard store.create(.inserted, for: result.id) else {
            saved = nil
            publishUI()
            return
        }
        saved = nil
        finish(result.id, status: .inserted)
        host.watchInsert(text)
        publishUI()
    }

    // MARK: Waiting

    private func startWaiting() {
        isWaiting = true
        waitWasConnected = link?.connectionState(now: now()) == .connected
        session?.isWaiting = true
        if let session { publish(session) }
        waitTask?.cancel()
        let interval = Self.waitInterval
        let limit = Self.waitLimit
        let sleep = self.sleep
        waitTask = Task { [weak self] in
            var waited: TimeInterval = 0
            while waited < limit, !Task.isCancelled {
                await sleep(interval)
                waited += interval
                guard !Task.isCancelled else { return }
                await self?.reconcile()
            }
            guard !Task.isCancelled else { return }
            self?.stopWaiting()
            self?.publishUI()
        }
    }

    private func stopWaiting() {
        armedMessage = nil
        waitTask?.cancel()
        waitTask = nil
        guard isWaiting else { return }
        isWaiting = false
        session?.isWaiting = false
        if let session, isVisible { publish(session) }
    }

    // MARK: Outcomes

    private func write(_ id: String, status: WatchVoiceDeliveryStatus, failure: WatchVoiceFailure? = nil,
                       failureLimit: Int? = nil, text: String? = nil, instruction: String? = nil,
                       kind: WatchVoiceOutcome.Kind? = nil) {
        let stamp = now()
        store.write(WatchVoiceOutcome(id: id, status: status, updatedAt: stamp,
                                      expiresAt: stamp.addingTimeInterval(WatchVoice.resultLifetime),
                                      failure: failure, failureLimit: failureLimit,
                                      text: text, instruction: instruction, kind: kind))
        post(.outcome)
    }

    /// A final status: the texts go with it.
    private func finish(_ id: String, status: WatchVoiceDeliveryStatus) {
        write(id, status: status)
        store.removeCommand(id)
    }

    private func limit(of error: AIReplyError) -> Int? {
        switch error {
        case .messageTooLong(let limit), .instructionTooLong(let limit): return limit
        default: return nil
        }
    }

    // MARK: UI

    private func publishUI() {
        let next = makeUI()
        guard next != ui else { return }
        ui = next
        host?.watchUIDidChange(next)
    }

    private func makeUI() -> WatchVoiceUI {
        let connection = link?.connectionState(now: now())
        let hasWork = active != nil || awaitingInsert != nil || held != nil || saved != nil

        var result = WatchVoiceUI()
        switch connection {
        case .unsupported, .activating, .notPaired, .none:
            result.indicator = hasWork ? .ready : .hidden
        case .appNotInstalled:
            result.indicator = .installApp
        case .appNotOpen:
            result.indicator = .openWatchApp
        case .connected:
            result.indicator = .ready
        }

        if active != nil {
            result.indicator = .generating
        } else if awaitingInsert != nil || saved != nil {
            result.indicator = .responseReady
        } else if held != nil {
            result.indicator = .instructionReceived
        } else if isWaiting {
            result.indicator = waitWasConnected && connection == .appNotOpen ? .connectionLost : .waiting
        }

        // The composer shows everything while it owns the row; a reply put
        // aside under the persona row keeps the row for itself.
        if awaitingInsert != nil { return result }
        if active == nil, host?.watchComposerState == .busy { return result }
        if active != nil {
            result.strip = isVisible && host?.watchComposerState == .busy ? nil : .generating
            return result
        }
        if let notice {
            result.strip = notice
        } else if let current = held {
            result.strip = host?.watchHasFullAccess == true
                ? .instructionReceived(preview: current.instruction)
                : .fullAccessRequired
        } else if let text = saved?.text {
            result.strip = .responseReady(preview: text)
        } else if isWaiting {
            switch result.indicator {
            case .connectionLost: result.strip = .connectionLost
            default: result.strip = connection == .connected ? .waiting : .openWatchApp
            }
        }
        return result
    }
}
