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
struct ComposeSession: Equatable, Sendable {
    var instruction: String = ""
    var flow = ReplyComposerFlow()

    /// Whether "New" has anything to clear.
    var hasContent: Bool {
        !instruction.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !flow.drafts.isEmpty
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
/// only text it can send is the instruction the user typed here.
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

    /// A session parked when the keyboard last went away.
    func restore(_ parked: ComposeSession) {
        cancelTask()
        var restored = parked
        restored.flow.resume()
        session = restored
    }

    // MARK: Generation

    /// Write from the instruction, or Regenerate from a result: the same
    /// instruction, a NEW version.
    func generate() {
        guard var current = session, !current.flow.isGenerating, task == nil else { return }

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
        session?.flow.fail(error)
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
