import Foundation

/// The AI composer as an explicit state machine: its stages, and the only
/// ways to move between them.
///
///     composing ──Generate──▶ generating(composing) ──reply──▶ result
///         ▲                        │ fail / stop                 │ ▲
///         │                        ▼                             │ │
///         └────────── Back ─── composing          Regenerate ────┘ │
///                                                  generating(result)
///     result ──Edit / typing──▶ editing ──Done──▶ result
///     result / editing ──Insert, field not empty──▶ conflict ──Cancel──▶ back where it was
///
/// Rules it guarantees, and the tests pin:
/// * at most ONE request at a time - a second Generate while one is running is
///   refused, so rapid taps cannot double-spend the user's quota;
/// * Regenerate never destroys a version: it adds one (`ReplyDraftHistory`);
/// * a failure never throws away typing: the stage goes back to where the
///   request started, with the error to show;
/// * Insert always takes the text on screen, edits included.
///
/// Pure value type: no UIKit, no networking, no clock.
struct ReplyComposerFlow: Equatable, Sendable {

    enum Origin: Equatable, Sendable {
        case composing
        case result
    }

    enum Stage: Equatable, Sendable {
        case composing
        case generating(from: Origin)
        case result
        case editing
        /// The host field already has text. `returnTo` is where Cancel goes.
        case conflict(returnTo: Origin, wasEditing: Bool)
    }

    enum InsertDecision: Equatable, Sendable {
        /// Put this text in the host field now.
        case insert(String)
        /// Ask Replace / Add / Cancel first.
        case askAboutExistingText
        /// Nothing to insert.
        case nothing
    }

    enum ConflictChoice: Equatable, Sendable {
        case replace
        case append
        case cancel
    }

    enum ConflictResolution: Equatable, Sendable {
        case replace(String)
        case append(String)
        case cancelled
    }

    private(set) var stage: Stage = .composing
    private(set) var drafts = ReplyDraftHistory()
    /// The last failure, shown inline until the user does something else.
    private(set) var error: AIReplyError?

    init() {}

    // MARK: Derived

    var isGenerating: Bool {
        if case .generating = stage { return true }
        return false
    }

    /// The stage a request started from, while one is running.
    var generationOrigin: Origin? {
        if case .generating(let origin) = stage { return origin }
        return nil
    }

    /// Whether the keys should edit the reply rather than anything else.
    var isEditingDraft: Bool { stage == .editing }

    /// Stages that show the reply rather than the instruction.
    var showsReply: Bool {
        switch stage {
        case .result, .editing, .generating(from: .result): return true
        case .conflict: return true
        case .composing, .generating(from: .composing): return false
        }
    }

    var draftText: String { drafts.currentText }

    // MARK: Generation

    /// Generate from the composer, or Regenerate from the result. Returns
    /// false when a request must not start: one is already running, or the
    /// stage has no Generate button.
    @discardableResult
    mutating func startGeneration() -> Bool {
        switch stage {
        case .composing:
            stage = .generating(from: .composing)
        case .result, .editing:
            stage = .generating(from: .result)
        case .generating, .conflict:
            return false
        }
        error = nil
        return true
    }

    /// A reply arrived. Ignored unless a request is actually running - a late
    /// answer to a request the user stopped must not reappear.
    mutating func receive(reply: String) {
        guard isGenerating else { return }
        drafts.append(generated: reply)
        error = nil
        stage = .result
    }

    /// The request failed. The user lands back where they started, with the
    /// message to read and everything they typed intact.
    mutating func fail(_ failure: AIReplyError) {
        switch stage {
        case .generating(let origin):
            stage = origin == .result && !drafts.isEmpty ? .result : .composing
        case .conflict:
            return
        case .composing, .result, .editing:
            break
        }
        error = failure == .cancelled ? nil : failure
    }

    /// Stop. Same as a failure, without the message.
    mutating func cancelGeneration() {
        guard case .generating(let origin) = stage else { return }
        stage = origin == .result && !drafts.isEmpty ? .result : .composing
        error = nil
    }

    // MARK: Navigation

    /// Back from the reply to the instruction. The versions are kept: a new
    /// Generate adds to them.
    mutating func back() {
        switch stage {
        case .result, .editing:
            stage = .composing
            error = nil
        case .generating(from: .result):
            // Back while regenerating: stop the request and go back.
            stage = .composing
            error = nil
        default:
            break
        }
    }

    mutating func beginEditing() {
        guard stage == .result, !drafts.isEmpty else { return }
        stage = .editing
        error = nil
    }

    /// Typing while the reply is on screen: the reply becomes editable and
    /// the keystroke lands in it. Returns whether the keys now edit the reply.
    mutating func beginEditingForTyping() -> Bool {
        switch stage {
        case .editing:
            return true
        case .result:
            stage = .editing
            error = nil
            return true
        default:
            return false
        }
    }

    mutating func endEditing() {
        guard stage == .editing else { return }
        stage = .result
    }

    /// The text of the version on screen changed.
    mutating func editDraft(_ text: String) {
        guard stage == .editing else { return }
        drafts.edit(text)
        error = nil
    }

    mutating func showPreviousVersion() {
        guard stage == .result || stage == .editing else { return }
        drafts.selectPrevious()
    }

    mutating func showNextVersion() {
        guard stage == .result || stage == .editing else { return }
        drafts.selectNext()
    }

    /// The composer content changed (the source or the instruction): an old
    /// error no longer describes it.
    mutating func clearError() {
        error = nil
    }

    // MARK: Insert

    /// - Parameter hostHasText: whether the host field already has text.
    mutating func requestInsert(hostHasText: Bool) -> InsertDecision {
        switch stage {
        case .result, .editing:
            break
        default:
            return .nothing
        }
        let text = drafts.currentText
        guard !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return .nothing }
        if hostHasText {
            stage = .conflict(returnTo: .result, wasEditing: stage == .editing)
            error = nil
            return .askAboutExistingText
        }
        return .insert(text)
    }

    mutating func resolveConflict(_ choice: ConflictChoice) -> ConflictResolution {
        guard case .conflict(_, let wasEditing) = stage else { return .cancelled }
        let text = drafts.currentText
        switch choice {
        case .cancel:
            stage = wasEditing ? .editing : .result
            return .cancelled
        case .replace:
            return .replace(text)
        case .append:
            return .append(text)
        }
    }

    // MARK: Lifecycle

    /// Reopening a suspended session (the persona was changed): back to the
    /// reply if there is one, otherwise to the instruction.
    mutating func resume() {
        if isGenerating { cancelGeneration() }
        if case .conflict = stage { stage = .result }
        if stage == .editing { stage = .result }
        if stage == .result, drafts.isEmpty { stage = .composing }
        error = nil
    }

    mutating func reset() {
        self = ReplyComposerFlow()
    }
}
