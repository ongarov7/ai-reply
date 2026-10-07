import Foundation

/// One "Create" session: what the user asked for and the versions written.
///
/// Deliberately its own type, not a `ReplySession` with an empty message:
/// a compose session has no source, no persona and no clipboard origin, so
/// there is nothing a copied message or a reply could leak into.
///
/// The stages are the reply composer's (`ReplyComposerFlow`), which is exactly
/// the compose state machine too:
///
///     composing (instruction) ──Write──▶ generating ──▶ result ──Insert──▶ host field
///         ▲    │ empty: error                 │ fail: back, error shown      │
///         │    ▼                              ▼                              │
///         └─ Back (versions kept) ◀── result ──Regenerate──▶ generating ─────┘
///     New: everything above is discarded, back to an empty instruction.
///
///     composing ──Reply to copied──▶ the reply composer, with the copied
///         message and this instruction (nothing generated); nothing copied
///         or no Full Access: an error here, and Create stays.
struct ComposeSession: Equatable, Sendable {
    var instruction: String = ""
    var flow = ReplyComposerFlow()
    /// "Reply to copied" could not start: nothing copied, no Full Access, or
    /// an instruction too long for a reply. Kept apart from `flow.error`
    /// because it is not a failure of writing - Write stays Write, not Retry.
    var copiedMessageError: AIReplyError?

    /// Whether "New" has anything to clear.
    var hasContent: Bool {
        !instruction.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !flow.drafts.isEmpty
    }

    /// What the panel says under the field.
    struct Notice: Equatable, Sendable {
        let text: String
        /// Whether the primary button offers Retry. Only for a failed
        /// Write: after "Reply to copied" failed, Retry would write a new
        /// message, which is not what was just tried.
        let offersRetry: Bool
    }

    func notice(_ strings: AIReplyStrings) -> Notice? {
        if let copied = copiedMessageError {
            // The reply's sentences: they are about the copied message.
            let text = strings.message(for: copied)
            return text.isEmpty ? nil : Notice(text: text, offersRetry: false)
        }
        guard let error = flow.error else { return nil }
        // The reply sentence for Full Access talks about a copied message;
        // writing has none.
        let text = error == .fullAccessRequired ? strings.compose.fullAccessRequired : strings.message(for: error)
        return text.isEmpty ? nil : Notice(text: text, offersRetry: true)
    }
}

extension ReplyConfiguration {

    /// The persona a reply started from Create opens with: the one used
    /// last, while the keyboard still shows it; otherwise Friend; otherwise
    /// the first one shown. The chip in the reply header changes it.
    func personaForCopiedReply(lastUsedID: String?) -> ReplyTemplate {
        let visible = visibleTemplates
        if let lastUsedID, let last = visible.first(where: { $0.id == lastUsedID }) { return last }
        let friend = RelationshipKind.friend.rawValue
        if let shown = visible.first(where: { $0.id == friend }) ?? visible.first { return shown }
        return template(id: friend) ?? .builtIn(.friend, sortIndex: 0)
    }
}

/// Keeps an unfinished Create session for a few minutes while the keyboard is
/// off screen (the user switched chats), in process memory only - like
/// `ReplySessionParking`, and never at the same time as a parked reply.
enum ComposeSessionParking {

    static let lifetime: TimeInterval = 10 * 60

    private static var parked: (session: ComposeSession, at: Date)?

    static func park(_ session: ComposeSession, now: Date = Date()) {
        parked = (session, now)
    }

    static func take(now: Date = Date()) -> ComposeSession? {
        defer { parked = nil }
        guard let parked, now.timeIntervalSince(parked.at) <= lifetime else { return nil }
        return parked.session
    }

    static func discard() {
        parked = nil
    }
}

@MainActor
protocol ComposeFlowCoordinatorDelegate: AnyObject {
    func composeCoordinatorDidChange(_ coordinator: ComposeFlowCoordinator)
}

/// Drives instruction -> AI -> versions for Create.
///
/// Mirrors `ReplyFlowCoordinator`'s guarantees: nothing is generated until
/// the user taps Write or Regenerate; one request at a time (a second tap is
/// ignored, so rapid taps cannot spend the quota twice); a late answer to a
/// stopped request never lands; a failure never loses the instruction.
///
/// It has no access to the clipboard or the host field - by construction the
/// only text it can send is the instruction the user typed here. "Reply to
/// copied" is handed a reader for the copied message by the keyboard, uses it
/// once on that tap, and passes what it read straight on to a reply session:
/// it never sends it.
@MainActor
final class ComposeFlowCoordinator {

    weak var delegate: ComposeFlowCoordinatorDelegate?

    private let service: ComposeService
    private let normalizer: ReplyDraftNormalizer

    private var task: Task<Void, Never>?
    private var generation = 0

    private(set) var session: ComposeSession?

    /// The APP's language: sent so the server can decide when the
    /// instruction's own language is unclear.
    var uiLanguage: AppLanguage = .systemDefault

    /// The keyboard layout to report with the next request; the keyboard
    /// sets it as Write is tapped.
    var inputLanguage: KeyboardLanguage?

    /// From the profile the keyboard loaded when it appeared.
    var grammaticalGender: GrammaticalGender?

    init(service: ComposeService = ComposeService(), normalizer: ReplyDraftNormalizer = ReplyDraftNormalizer()) {
        self.service = service
        self.normalizer = normalizer
    }

    var isActive: Bool { session != nil }
    var flow: ReplyComposerFlow { session?.flow ?? ReplyComposerFlow() }
    var isRequestInFlight: Bool { task != nil }

    // MARK: Entry points

    /// Create was tapped: a fresh, empty session. It does NOT generate.
    func open() {
        cancelTask()
        ComposeSessionParking.discard()
        session = ComposeSession()
        ReplyLog.event("ai_compose_opened")
    }

    /// A session parked when the keyboard last went away. It comes back
    /// without any error: the user usually left to copy the message that
    /// "Copy a message first" asked for, so that sentence would now be wrong.
    func restore(_ parked: ComposeSession) {
        cancelTask()
        var restored = parked
        restored.flow.resume()
        restored.copiedMessageError = nil
        session = restored
    }

    // MARK: Generation

    /// Write from the instruction, or Regenerate from a result: the same
    /// instruction, a NEW version.
    func generate() {
        guard var current = session, !current.flow.isGenerating, task == nil else { return }
        current.copiedMessageError = nil

        if case .failure(let error) = ComposeService.validate(instruction: current.instruction) {
            current.flow.fail(error)
            session = current
            delegate?.composeCoordinatorDidChange(self)
            return
        }
        let isRegeneration = !current.flow.drafts.isEmpty
        guard current.flow.startGeneration() else { return }
        session = current
        delegate?.composeCoordinatorDidChange(self)
        ReplyLog.event(isRegeneration ? "ai_compose_regenerated" : "ai_compose_generate_started")
        run(ComposeService.Request(
            instruction: current.instruction,
            uiLanguage: uiLanguage,
            isRegeneration: isRegeneration,
            inputLanguage: inputLanguage,
            grammaticalGender: grammaticalGender
        ))
    }

    /// A failure found before any request could start - Full Access off.
    func showError(_ error: AIReplyError) {
        guard session != nil, session?.flow.isGenerating == false else { return }
        session?.copiedMessageError = nil
        session?.flow.fail(error)
        delegate?.composeCoordinatorDidChange(self)
    }

    // MARK: Reply to copied

    /// "Reply to copied": the same instruction, now for the message the
    /// user copied. On success Create closes and the reply session to open
    /// comes back - with the copied message, the persona given and this
    /// instruction, in its first stage: NOTHING has been generated, and
    /// nothing will be until the user taps Reply.
    ///
    /// On failure (nothing copied, no Full Access, an instruction longer
    /// than a reply takes) Create stays as it was, saying why, and nil
    /// comes back.
    ///
    /// - Parameters:
    ///   - persona: who the reply is to; changeable in the reply header.
    ///   - instructionLimit: the reply instruction's limit, which is shorter
    ///     than Create's. A longer instruction is not cut - the user would
    ///     send something they never saw - but refused here, before the
    ///     clipboard is touched.
    ///   - read: reads the copied message. Called at most once, and only
    ///     from here, as part of the user's tap.
    func replyToCopied(
        persona: ReplyTemplate,
        instructionLimit: Int,
        read: () -> Result<ReplyContext, AIReplyError>
    ) -> ReplySession? {
        guard let current = session, current.flow.stage == .composing, task == nil else { return nil }

        let instruction = current.instruction.trimmingCharacters(in: .whitespacesAndNewlines)
        if instruction.unicodeScalars.count > instructionLimit {
            refuseReplyToCopied(.instructionTooLong(limit: instructionLimit))
            return nil
        }

        switch read() {
        case .failure(let error):
            refuseReplyToCopied(error)
            return nil
        case .success(let copied):
            var reply = ReplySession(sourceMessage: copied.text, source: copied.source, template: persona)
            reply.instruction = instruction
            ReplyLog.event("ai_compose_reply_to_copied, instruction length \(instruction.count)")
            clear()
            return reply
        }
    }

    private func refuseReplyToCopied(_ error: AIReplyError) {
        session?.flow.clearError()
        session?.copiedMessageError = error == .cancelled ? nil : error
        ReplyLog.event("ai_compose_reply_to_copied_failed: \(error)")
        delegate?.composeCoordinatorDidChange(self)
    }

    /// Stop the request in flight. The instruction and versions stay.
    func cancelGeneration() {
        guard session?.flow.isGenerating == true else { return }
        cancelTask()
        session?.flow.cancelGeneration()
        delegate?.composeCoordinatorDidChange(self)
    }

    private func run(_ request: ComposeService.Request) {
        generation += 1
        let ticket = generation
        let service = self.service
        let started = Date()

        task = Task { [weak self] in
            let outcome: Result<GeneratedReply, AIReplyError>
            do {
                outcome = .success(try await service.compose(request))
            } catch {
                outcome = .failure((error as? AIReplyError) ?? .serviceUnavailable)
            }
            guard let self, !Task.isCancelled, ticket == self.generation else { return }
            self.task = nil
            guard self.session != nil else { return }
            let latency = Int(Date().timeIntervalSince(started) * 1000)
            switch outcome {
            case .success(let message):
                // Tidied once, on arrival; what the user then edits is exactly
                // what Insert uses.
                let text = self.normalizer.normalize(message.text)
                self.session?.flow.receive(reply: text.isEmpty ? message.text : text)
                ReplyLog.event("ai_compose_generate_success, \(latency) ms, length \(message.text.count)")
            case .failure(let error):
                self.session?.flow.fail(error)
                ReplyLog.event("ai_compose_generate_failed: \(error), \(latency) ms")
            }
            self.delegate?.composeCoordinatorDidChange(self)
        }
    }

    private func cancelTask() {
        task?.cancel()
        task = nil
        generation += 1
    }

    // MARK: Composer events

    func updateInstruction(_ text: String) {
        guard session != nil, session?.instruction != text else { return }
        session?.instruction = text
        session?.flow.clearError()
        session?.copiedMessageError = nil
    }

    func updateDraft(_ text: String) {
        session?.flow.editDraft(text)
    }

    /// Back to the instruction, to change it. The versions are kept.
    func back() {
        guard session != nil else { return }
        if session?.flow.isGenerating == true { cancelTask() }
        session?.flow.back()
        delegate?.composeCoordinatorDidChange(self)
    }

    /// New: the instruction, every version and any error are discarded.
    func reset() {
        guard session != nil else { return }
        cancelTask()
        session = ComposeSession()
        ReplyLog.event("ai_compose_reset")
        delegate?.composeCoordinatorDidChange(self)
    }

    func beginEditing() {
        session?.flow.beginEditing()
        delegate?.composeCoordinatorDidChange(self)
    }

    func beginEditingForTyping() -> Bool {
        guard session != nil else { return false }
        let editing = session?.flow.beginEditingForTyping() ?? false
        if editing { delegate?.composeCoordinatorDidChange(self) }
        return editing
    }

    func endEditing() {
        session?.flow.endEditing()
        delegate?.composeCoordinatorDidChange(self)
    }

    func showPreviousVersion() {
        session?.flow.showPreviousVersion()
        delegate?.composeCoordinatorDidChange(self)
    }

    func showNextVersion() {
        session?.flow.showNextVersion()
        delegate?.composeCoordinatorDidChange(self)
    }

    // MARK: Insert

    /// The message on screen, edits included. Never the instruction.
    func requestInsert(hostHasText: Bool) -> ReplyComposerFlow.InsertDecision {
        guard session != nil else { return .nothing }
        let decision: ReplyComposerFlow.InsertDecision = session?.flow.requestInsert(hostHasText: hostHasText) ?? .nothing
        switch decision {
        case .insert(let text):
            return outgoing(text).map { .insert($0) } ?? .nothing
        case .askAboutExistingText:
            delegate?.composeCoordinatorDidChange(self)
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
            delegate?.composeCoordinatorDidChange(self)
            return .cancelled
        }
    }

    private func outgoing(_ text: String) -> String? {
        let message = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !message.isEmpty else { return nil }
        ReplyLog.event("ai_compose_inserted, length \(message.count)")
        return message
    }

    // MARK: Teardown

    /// The keyboard is going off screen: stop any request, keep the rest for
    /// a few minutes.
    func park() {
        guard var current = session else { return }
        cancelTask()
        current.flow.cancelGeneration()
        ComposeSessionParking.park(current)
        session = nil
    }

    /// Leaving Create: every trace of the instruction and the message goes.
    func clear() {
        cancelTask()
        session = nil
        ComposeSessionParking.discard()
    }
}
