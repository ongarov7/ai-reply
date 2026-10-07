import UIKit

// MARK: - Acquisition

/// Resolves "the message the user wants to reply to" using only public,
/// documented iOS APIs.
///
/// IMPORTANT LIMITATION, stated here so no caller can misread the intent:
/// an iOS Custom Keyboard Extension CANNOT read incoming message bubbles in
/// WhatsApp, Telegram, Instagram Direct, Messenger or iMessage.
/// `UITextDocumentProxy` is a view onto the active editable text input only.
/// There is no public API that exposes another application's view hierarchy or
/// its chat content to a keyboard, and this file deliberately contains no
/// Accessibility, screen-scraping, OCR or private-API path to fake one. The
/// universal fallback is the explicit user gesture: long press the message,
/// Copy, then open the composer.
final class ContextTextProvider {

    /// - Note: CLIPBOARD PRIVACY. The clipboard is touched ONLY from inside this
    ///   call, and this call only ever runs as the direct result of a user
    ///   gesture: opening the composer from a persona chip, tapping Paste
    ///   inside it, or "Reply to copied" in Create. There is no polling, no timer, no read on appearance, no
    ///   read on regeneration and no background access. The acquired text is
    ///   then held in the session and reused, so a second generation never
    ///   touches the pasteboard again.
    func acquire(proxy: UITextDocumentProxy, hasFullAccess: Bool) -> Result<ReplyContext, AIReplyError> {
        // STEP 1 - selection inside the active editable input.
        if let selection = Self.normalised(proxy.selectedText) {
            ReplyLog.event("source: selection")
            return .success(ReplyContext(text: selection, source: .editableSelection))
        }

        // STEP 2 - explicitly copied text.
        guard hasFullAccess else {
            ReplyLog.event("clipboard unavailable: full access off")
            return .failure(.fullAccessRequired)
        }

        let pasteboard = UIPasteboard.general
        // `hasStrings` is a metadata query and does not trigger the system
        // paste prompt; only reading `string` does.
        guard pasteboard.hasStrings, let copied = Self.normalised(pasteboard.string) else {
            ReplyLog.event("clipboard: no usable text")
            return .failure(.noSourceMessage)
        }

        ReplyLog.event("source: clipboard, length \(copied.count)")
        return .success(ReplyContext(text: copied, source: .clipboard))
    }

    private static func normalised(_ value: String?) -> String? {
        guard let trimmed = value?.trimmingCharacters(in: .whitespacesAndNewlines),
              !trimmed.isEmpty else { return nil }
        return trimmed
    }
}

// MARK: - Parking

/// Keeps an unfinished session for a few minutes while the keyboard is off
/// screen - the user went to copy one more message, or glanced at another
/// chat - so coming back does not mean typing the instruction again.
///
/// PRIVACY. Process memory only: nothing is written to disk or to the App
/// Group, and a parked session expires on its own.
enum ReplySessionParking {

    static let lifetime: TimeInterval = 10 * 60

    private static var parked: (session: ReplySession, at: Date)?

    static func park(_ session: ReplySession, now: Date = Date()) {
        parked = (session, now)
    }

    static func take(now: Date = Date()) -> ReplySession? {
        defer { parked = nil }
        guard let parked, now.timeIntervalSince(parked.at) <= lifetime else { return nil }
        return parked.session
    }

    static func discard() {
        parked = nil
    }
}

// MARK: - Coordinator

@MainActor
protocol ReplyFlowCoordinatorDelegate: AnyObject {
    /// The composer should show this session. NOTHING has been generated.
    func coordinator(_ coordinator: ReplyFlowCoordinator, didOpen session: ReplySession)
    /// The session changed: a stage, a version, an error, the source text.
    func coordinatorDidChange(_ coordinator: ReplyFlowCoordinator)
}

/// Drives source -> instruction -> AI -> versions.
///
/// GENERATION IS NEVER AUTOMATIC. Copying text does not start a request;
/// opening the keyboard does not; picking a persona does not. Exactly one
/// thing does: the user tapping Generate or Regenerate. That is what keeps
/// token cost, accidental clipboard content and privacy under the user's
/// control at once.
@MainActor
final class ReplyFlowCoordinator {

    weak var delegate: ReplyFlowCoordinatorDelegate?

    private let provider: ContextTextProvider
    private let normalizer: ReplyDraftNormalizer
    private let service: AIReplyService

    /// The in-flight request. Holding it is what makes cancellation, duplicate
    /// suppression and "keyboard closed mid-request" one mechanism.
    private var task: Task<Void, Never>?
    /// Increments per request, so the answer to a request the user already
    /// stopped can never land on screen.
    private var generation = 0

    private(set) var session: ReplySession?

    /// True while the persona row is showing over a session the user has not
    /// finished. Picking a persona resumes it with everything intact.
    private(set) var isSuspended = false

    /// Configuration snapshot taken when the keyboard appeared, so no request
    /// has to touch storage.
    var configuration: ReplyConfiguration = .initial

    /// The APP's language. Names the persona in the prompt the way the user
    /// saw it on the chip.
    var uiLanguage: AppLanguage = .systemDefault

    /// The keyboard layout to report with the next request; the keyboard
    /// sets it as Reply is tapped.
    var inputLanguage: KeyboardLanguage?

    init(
        provider: ContextTextProvider = ContextTextProvider(),
        normalizer: ReplyDraftNormalizer = ReplyDraftNormalizer(),
        service: AIReplyService = AIReplyService()
    ) {
        self.provider = provider
        self.normalizer = normalizer
        self.service = service
    }

    var isComposing: Bool { session != nil && !isSuspended }
    var flow: ReplyComposerFlow { session?.flow ?? ReplyComposerFlow() }

    // MARK: Entry points

    /// The user picked a persona. Opens the composer; it does NOT generate.
    ///
    /// Resuming a suspended session keeps the message, the instruction and
    /// every version, and only swaps the persona.
    func open(template: ReplyTemplate, proxy: UITextDocumentProxy, hasFullAccess: Bool) {
        if session != nil {
            session?.template = template
            session?.flow.resume()
            isSuspended = false
            if let session { delegate?.coordinator(self, didOpen: session) }
            return
        }

        var opened = ReplySession(sourceMessage: "", source: nil, template: template)
        switch provider.acquire(proxy: proxy, hasFullAccess: hasFullAccess) {
        case .success(let context):
            opened.sourceMessage = context.text
            opened.source = context.source
        case .failure(let error):
            // An empty clipboard is not an error worth shouting about: the
            // composer opens anyway and its placeholder says what to do. Full
            // Access being off IS worth saying, because nothing the user does
            // inside the keyboard can fix it.
            if error != .noSourceMessage { opened.flow.fail(error) }
        }

        session = opened
        isSuspended = false
        delegate?.coordinator(self, didOpen: opened)
    }

    /// "Reply to copied" from Create: the session Create built from the
    /// message the user copied and the instruction they typed there. Like
    /// `open`, it does NOT generate - the user still taps Reply.
    func open(handedOff session: ReplySession) {
        cancelTask()
        ReplySessionParking.discard()
        self.session = session
        isSuspended = false
        delegate?.coordinator(self, didOpen: session)
    }

    /// The copied message, for "Reply to copied" in Create. The same reader
    /// and the same rules as a persona tap: only on that tap, selection
    /// first, then the clipboard - with Full Access.
    func readCopiedMessage(proxy: UITextDocumentProxy, hasFullAccess: Bool) -> Result<ReplyContext, AIReplyError> {
        provider.acquire(proxy: proxy, hasFullAccess: hasFullAccess)
    }

    /// A session parked when the keyboard last went away.
    func restore(_ parked: ReplySession) {
        cancelTask()
        var restored = parked
        restored.flow.resume()
        session = restored
        isSuspended = false
        delegate?.coordinator(self, didOpen: restored)
    }

    /// Explicit Paste inside the composer. The only other place the clipboard
    /// is read, and again only from a direct user gesture.
    func pasteSource(proxy: UITextDocumentProxy, hasFullAccess: Bool) {
        guard session != nil else { return }
        switch provider.acquire(proxy: proxy, hasFullAccess: hasFullAccess) {
        case .success(let context):
            session?.sourceMessage = context.text
            session?.source = context.source
            session?.flow.clearError()
        case .failure(let error):
            session?.flow.fail(error)
        }
        delegate?.coordinatorDidChange(self)
    }

    /// Keeps the session but hands the screen back to the persona row.
    func suspend() {
        guard session != nil else { return }
        cancelTask()
        session?.flow.cancelGeneration()
        isSuspended = true
    }

    // MARK: Generation

    /// Generate from the instruction, or Regenerate from a reply: same
    /// message, same instruction, same persona - a NEW version.
    func generate() {
        guard var current = session, !current.flow.isGenerating, task == nil else { return }

        guard let source = current.usableSource else {
            current.flow.fail(.noSourceMessage)
            session = current
            delegate?.coordinatorDidChange(self)
            return
        }
        // The length rule is checked BEFORE the request, so an over-long
        // paste costs nothing and reports immediately.
        if case .failure(let error) = AIReplyService.validate(message: source) {
            current.flow.fail(error)
            session = current
            delegate?.coordinatorDidChange(self)
            return
        }
        guard current.flow.startGeneration() else { return }
        session = current
        delegate?.coordinatorDidChange(self)
        run(current)
    }

    /// A failure found before any request could start - Full Access off.
    func showError(_ error: AIReplyError) {
        guard session != nil, session?.flow.isGenerating == false else { return }
        session?.flow.fail(error)
        delegate?.coordinatorDidChange(self)
    }

    /// Stop the request in flight. Everything typed stays.
    func cancelGeneration() {
        guard session?.flow.isGenerating == true else { return }
        cancelTask()
        session?.flow.cancelGeneration()
        delegate?.coordinatorDidChange(self)
    }

    private func run(_ snapshot: ReplySession) {
        let request = AIReplyService.Request(
            message: snapshot.sourceMessage,
            template: snapshot.template,
            configuration: configuration,
            uiLanguage: uiLanguage,
            instruction: snapshot.instruction,
            inputLanguage: inputLanguage
        )
        generation += 1
        let ticket = generation
        let service = self.service

        task = Task { [weak self] in
            let outcome: Result<GeneratedReply, AIReplyError>
            do {
                outcome = .success(try await service.generate(request))
            } catch {
                outcome = .failure((error as? AIReplyError) ?? .serviceUnavailable)
            }
            guard let self, !Task.isCancelled, ticket == self.generation else { return }
            self.task = nil
            // The session can have been closed while the request was in
            // flight. Dropping the answer is correct: there is nowhere to put it.
            guard self.session != nil else { return }
            switch outcome {
            case .success(let reply):
                // The model's formatting is tidied once, as it arrives. What
                // the user then sees - and edits - is exactly what Insert uses.
                self.session?.flow.receive(reply: self.normalizer.normalize(reply.text))
            case .failure(let error):
                self.session?.flow.fail(error)
            }
            self.delegate?.coordinatorDidChange(self)
        }
    }

    private func cancelTask() {
        task?.cancel()
        task = nil
        generation += 1
    }

    // MARK: Composer events

    func updateSource(_ text: String) {
        guard session != nil, session?.sourceMessage != text else { return }
        session?.sourceMessage = text
        if session?.source == nil { session?.source = .typed }
        session?.flow.clearError()
    }

    func updateInstruction(_ text: String) {
        guard session != nil, session?.instruction != text else { return }
        session?.instruction = text
        session?.flow.clearError()
    }

    /// The reply on screen was edited.
    func updateDraft(_ text: String) {
        session?.flow.editDraft(text)
    }

    func back() {
        guard session != nil else { return }
        if session?.flow.isGenerating == true { cancelTask() }
        session?.flow.back()
        delegate?.coordinatorDidChange(self)
    }

    func beginEditing() {
        session?.flow.beginEditing()
        delegate?.coordinatorDidChange(self)
    }

    /// Typing while a reply is on screen edits it. Returns whether it does.
    func beginEditingForTyping() -> Bool {
        guard session != nil else { return false }
        let editing = session?.flow.beginEditingForTyping() ?? false
        if editing { delegate?.coordinatorDidChange(self) }
        return editing
    }

    func endEditing() {
        session?.flow.endEditing()
        delegate?.coordinatorDidChange(self)
    }

    func showPreviousVersion() {
        session?.flow.showPreviousVersion()
        delegate?.coordinatorDidChange(self)
    }

    func showNextVersion() {
        session?.flow.showNextVersion()
        delegate?.coordinatorDidChange(self)
    }

    // MARK: Insert

    /// What to do with the reply on screen: the text exactly as shown, edits
    /// included - never the message and never the instruction.
    func requestInsert(hostHasText: Bool) -> ReplyComposerFlow.InsertDecision {
        guard session != nil else { return .nothing }
        let decision: ReplyComposerFlow.InsertDecision = session?.flow.requestInsert(hostHasText: hostHasText) ?? .nothing
        switch decision {
        case .insert(let text):
            return outgoing(text).map { .insert($0) } ?? .nothing
        case .askAboutExistingText:
            delegate?.coordinatorDidChange(self)
            return .askAboutExistingText
        case .nothing:
            return .nothing
        }
    }

    func resolveConflict(_ choice: ReplyComposerFlow.ConflictChoice) -> ReplyComposerFlow.ConflictResolution {
        guard session != nil else { return .cancelled }
        let resolution: ReplyComposerFlow.ConflictResolution = session?.flow.resolveConflict(choice) ?? .cancelled
        switch resolution {
        case .replace(let text):
            return outgoing(text).map { .replace($0) } ?? .cancelled
        case .append(let text):
            return outgoing(text).map { .append($0) } ?? .cancelled
        case .cancelled:
            delegate?.coordinatorDidChange(self)
            return .cancelled
        }
    }

    /// Only the whitespace around the reply goes. It is NOT normalised again:
    /// that happened once, on arrival, and running it over the user's edits
    /// would quietly rewrite them ("Рахмет :)" would become "Рахмет:)").
    private func outgoing(_ text: String) -> String? {
        let draft = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !draft.isEmpty else { return nil }
        ReplyLog.event("insert accepted, length \(draft.count)")
        return draft
    }

    // MARK: Teardown

    /// The keyboard is going off screen: stop any request and keep the rest
    /// for a few minutes (see `ReplySessionParking`).
    func park() {
        guard var current = session else { return }
        cancelTask()
        current.flow.cancelGeneration()
        ReplySessionParking.park(current)
        session = nil
        isSuspended = false
    }

    /// Cancels any request and drops every trace of the message.
    ///
    /// PRIVACY. The message, the instruction and the versions live only here
    /// and in the composer's views, only for as long as the composer is open.
    /// Nothing is written to disk, to the App Group or to a log.
    func clear() {
        cancelTask()
        session = nil
        isSuspended = false
        ReplySessionParking.discard()
    }
}
