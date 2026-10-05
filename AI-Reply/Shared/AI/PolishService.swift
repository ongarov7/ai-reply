import Foundation

/// A cleaner version of the user's own instruction - typos, punctuation,
/// capitals - offered as a suggestion after a pause (DESIGN §5.5, "Level 2").
///
/// Only the INSTRUCTION is ever sent: the note the user is typing to the AI,
/// inside AI Reply's own field, with the smart-correction setting on. Never
/// the copied message, never a reply, never anything typed into another app.
/// The server answers `changed = false` when it has nothing better; every
/// failure is silent - a missing suggestion is not worth an error.
struct PolishService: Sendable {

    struct Request: Sendable, Equatable {
        /// The instruction as typed.
        var text: String
        /// The keyboard LAYOUT, a hint for a note too short to tell its language.
        var inputLanguage: KeyboardLanguage?
    }

    /// What the server made of the note.
    struct Result: Sendable, Equatable {
        let text: String
        let changed: Bool
    }

    private let configuration: AIConfiguration
    private let transportOverride: (any PolishTransport)?

    init(configuration: AIConfiguration = .shared, transportOverride: (any PolishTransport)? = nil) {
        self.configuration = configuration
        self.transportOverride = transportOverride
    }

    /// The suggested text, or nil when there is nothing to suggest.
    func polish(_ request: Request) async throws -> String? {
        let original = request.text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !original.isEmpty else { return nil }

        let transport: any PolishTransport
        if let transportOverride {
            transport = transportOverride
        } else {
            guard configuration.isReady, AccountSession.shared.isSignedIn else { throw AIReplyError.authenticationFailed }
            transport = AccountPolishTransport()
        }

        var prepared = request
        prepared.text = original
        let result = try await transport.polish(prepared)
        try Task.checkCancellation()
        let suggestion = result.text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard result.changed, !suggestion.isEmpty, suggestion != original else { return nil }
        return suggestion
    }
}

/// Where a polished instruction comes from: the backend, or a test double.
protocol PolishTransport: Sendable {
    func polish(_ request: PolishService.Request) async throws -> PolishService.Result
}

// MARK: - When to ask

/// The rules for asking at all, kept apart from timers so they are testable.
enum InstructionPolishRules {

    /// How long the user has to stop typing before a suggestion is asked for.
    static let pause: Duration = .milliseconds(1400)
    /// How long Undo stays on screen after a suggestion was used.
    static let undoWindow: Duration = .seconds(5)
    /// Failures in a row after which the keyboard stops asking until it
    /// appears again: a server that cannot answer should not be asked on
    /// every pause.
    static let failureLimit = 2

    static let minimumCharacters = 12
    static let minimumWords = 3

    /// Whether `text` is worth a request: long enough to have something to
    /// fix, within the server's limit, and not what was asked last.
    static func shouldRequest(_ text: String, lastRequested: String?, limit: Int) -> Bool {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmed.count >= minimumCharacters, trimmed.unicodeScalars.count <= limit else { return false }
        guard trimmed != lastRequested else { return false }
        let words = trimmed.split(whereSeparator: \.isWhitespace).filter { $0.contains(where: \.isLetter) }
        return words.count >= minimumWords
    }
}

// MARK: - The suggestion on screen

/// Drives the suggestion for one instruction field: waits for a pause, asks,
/// shows the answer, applies it on a tap and offers Undo for a moment.
///
/// Every edit starts over: it hides whatever was shown and drops an answer
/// still on its way, because an answer to older text must never replace
/// newer text.
@MainActor
final class InstructionPolisher {

    /// What the slot under the instruction shows.
    enum Chip: Equatable {
        case none
        /// The suggested text; a tap uses it.
        case suggestion(String)
        /// A suggestion was just used; a tap puts the user's text back.
        case undo
    }

    private(set) var chip: Chip = .none {
        didSet { if chip != oldValue { onChange?(chip) } }
    }

    var onChange: ((Chip) -> Void)?

    /// Server flag, setting and network together - decided when the
    /// keyboard appears. Turning it off hides the chip.
    var isEnabled = false {
        didSet { if !isEnabled { stop() } }
    }

    private let service: PolishService
    private let pause: Duration
    private let undoWindow: Duration

    private var waiting: Task<Void, Never>?
    private var undoExpiry: Task<Void, Never>?
    private var ticket = 0
    private var failures = 0
    private var lastRequested: String?
    /// What the field held before a suggestion was used, and the suggestion.
    private var applied: (previous: String, suggestion: String)?

    init(service: PolishService, pause: Duration = InstructionPolishRules.pause, undoWindow: Duration = InstructionPolishRules.undoWindow) {
        self.service = service
        self.pause = pause
        self.undoWindow = undoWindow
    }

    /// The keyboard appeared: a fresh start, failures forgotten.
    func reset() {
        stop()
        failures = 0
        lastRequested = nil
    }

    /// The instruction changed - typed, a quick intent, a correction.
    func instructionDidChange(_ text: String, inputLanguage: KeyboardLanguage?, limit: Int) {
        if let applied, text == applied.suggestion { return }
        stop()
        guard isEnabled, failures < InstructionPolishRules.failureLimit,
              InstructionPolishRules.shouldRequest(text, lastRequested: lastRequested, limit: limit) else { return }
        let ticket = self.ticket
        let pause = self.pause
        waiting = Task { [weak self] in
            try? await Task.sleep(for: pause)
            guard !Task.isCancelled else { return }
            await self?.request(text, inputLanguage: inputLanguage, limit: limit, ticket: ticket)
        }
    }

    /// Hides the chip and forgets any answer on its way: the field lost
    /// focus, a request started, the panel closed.
    func stop() {
        ticket += 1
        waiting?.cancel()
        waiting = nil
        undoExpiry?.cancel()
        undoExpiry = nil
        applied = nil
        chip = .none
    }

    /// The user tapped the suggestion: the text to put in the field.
    func accept(replacing current: String) -> String? {
        guard case .suggestion(let suggestion) = chip else { return nil }
        ticket += 1
        waiting?.cancel()
        waiting = nil
        applied = (current, suggestion)
        lastRequested = suggestion
        chip = .undo
        let window = undoWindow
        undoExpiry = Task { [weak self] in
            try? await Task.sleep(for: window)
            guard !Task.isCancelled else { return }
            self?.expireUndo()
        }
        return suggestion
    }

    /// The user tapped Undo: the text they had before.
    func undo() -> String? {
        guard chip == .undo, let previous = applied?.previous else { return nil }
        stop()
        lastRequested = previous.trimmingCharacters(in: .whitespacesAndNewlines)
        return previous
    }

    private func expireUndo() {
        guard chip == .undo else { return }
        applied = nil
        undoExpiry = nil
        chip = .none
    }

    private func request(_ text: String, inputLanguage: KeyboardLanguage?, limit: Int, ticket: Int) async {
        guard ticket == self.ticket else { return }
        lastRequested = text.trimmingCharacters(in: .whitespacesAndNewlines)
        let suggestion: String?
        do {
            suggestion = try await service.polish(PolishService.Request(text: text, inputLanguage: inputLanguage))
        } catch {
            // Silent by design (§2.6); only counted, so a broken server is
            // not asked on every pause. A cancelled request is not a failure.
            if ticket == self.ticket, !(error is CancellationError), (error as? AIReplyError) != .cancelled {
                failures += 1
            }
            return
        }
        guard ticket == self.ticket else { return }
        failures = 0
        guard let suggestion, suggestion.unicodeScalars.count <= limit else { return }
        chip = .suggestion(suggestion)
    }
}

#if DEBUG
/// DEBUG ONLY: a stand-in for the polish endpoint in the Simulator
/// (`-AIReplyMockReplies`): a capital first letter and a closing full stop,
/// enough to see the suggestion and Undo without an account.
struct DebugPolishMock: PolishTransport {

    func polish(_ request: PolishService.Request) async throws -> PolishService.Result {
        try await Task.sleep(for: .milliseconds(500))
        var text = request.text.split(whereSeparator: \.isWhitespace).joined(separator: " ")
        if let first = text.first, first.isLowercase {
            text = first.uppercased() + text.dropFirst()
        }
        if let last = text.last, !".!?…".contains(last) {
            text += "."
        }
        return PolishService.Result(text: text, changed: text != request.text)
    }
}
#endif
