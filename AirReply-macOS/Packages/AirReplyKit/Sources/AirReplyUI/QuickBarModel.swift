import AppKit
import Observation
import AirReplyCore
import AirReplySystem
import AirReplyVoice

/// Something the bar explains in one line with one action.
public enum QuickBarIssue: Equatable, Sendable {
    case generation(GenerationError)
    case voice(VoiceError)
    case nothingHeard
    case consentFailed
}

/// State and actions of the quick bar. The panel and menu bar only call into it;
/// the views only render it.
@MainActor
@Observable
public final class QuickBarModel {
    public enum Mode: Equatable {
        case compose
        /// Replying to text copied in another app.
        case reply(CopiedText)
    }

    public enum Phase: Equatable {
        case editing
        case generating
        case result
    }

    /// What was sent last, so "Retry" can reuse its idempotency key (never charged twice).
    private enum Request {
        case compose(instruction: String, regenerate: Bool, key: String)
        case reply(source: String, recipient: Recipient, instruction: String?, key: String)
    }

    public var draft = ""
    public var emailInput = ""
    public var codeInput = ""
    public private(set) var mode: Mode = .compose
    public private(set) var phase: Phase = .editing
    public private(set) var result = ""
    public private(set) var issue: QuickBarIssue?
    public private(set) var justCopied = false
    public private(set) var recipient: Recipient
    /// Bumped whenever the text field should take focus.
    public private(set) var focusRequest = 0
    /// The panel sits in the lower half of the screen: cards stack upward.
    public var growsUpward = false

    public let auth: AuthSession
    public let account: AccountStore
    public let voice: VoiceController
    let settings: AppSettings
    private let generator: any Generating
    private let clipboard: ClipboardMonitor
    private let now: () -> Date
    private var lastRequest: Request?
    private var lastActivity: Date?
    private var work: Task<Void, Never>?

    /// Hides the panel.
    public var requestClose: () -> Void = {}
    /// The reply was copied; the coordinator closes the panel and pastes if the user wants that.
    public var didCopy: () -> Void = {}
    public var openURL: (URL) -> Void = { NSWorkspace.shared.open($0) }
    public var openPrivacySettings: () -> Void = {}

    public init(auth: AuthSession, account: AccountStore, voice: VoiceController, generator: any Generating,
                clipboard: ClipboardMonitor, settings: AppSettings, now: @escaping () -> Date = Date.init) {
        self.auth = auth
        self.account = account
        self.voice = voice
        self.generator = generator
        self.clipboard = clipboard
        self.settings = settings
        self.now = now
        recipient = Recipient(rawValue: settings.lastRecipient) ?? .client
        observeVoice()
    }

    // MARK: Panel lifecycle

    /// Draft and result survive this long while the panel is closed.
    static let keepAlive: TimeInterval = 10 * 60

    public func panelDidOpen(copied: CopiedTextResult) {
        if let lastActivity, now().timeIntervalSince(lastActivity) > Self.keepAlive, phase != .generating {
            resetConversation()
        }
        if settings.offerCopiedText, case let .text(text) = copied, mode != .reply(text) {
            mode = .reply(text)
            if phase == .result { phase = .editing; result = "" }
            issue = nil
        }
        focusRequest += 1
        Task {
            await account.refreshConfig()
            if auth.isSignedIn { try? await account.refreshMe(maxAge: 30) }
        }
    }

    public func panelDidClose() {
        lastActivity = now()
        if voice.phase == .recording || voice.phase == .requestingPermission { voice.cancel() }
    }

    /// Keep the panel on screen while a system prompt or the microphone needs it.
    public var holdsPanelOpen: Bool {
        switch voice.phase {
        case .requestingPermission, .recording, .transcribing: true
        default: false
        }
    }

    private func resetConversation() {
        draft = ""
        result = ""
        issue = nil
        phase = .editing
        mode = .compose
        lastRequest = nil
    }

    // MARK: Derived state

    public var needsConsent: Bool { auth.isSignedIn && account.hasConsent == false }

    public var copiedText: CopiedText? {
        if case let .reply(text) = mode { return text }
        return nil
    }

    public var sourceLimit: Int { account.config.maxSourceCharacters }
    public var instructionLimit: Int { account.config.maxInstructionLength }
    public var sourceLength: Int { copiedText.map { TextMetrics.serverLength($0.text) } ?? 0 }
    public var draftLength: Int { TextMetrics.serverLength(draft) }
    public var sourceTooLong: Bool { sourceLength > sourceLimit }
    public var voiceAvailable: Bool { account.config.voiceTranscription }

    public var canSubmit: Bool {
        guard auth.isSignedIn, !needsConsent, phase != .generating else { return false }
        if copiedText != nil { return !sourceTooLong }
        return draftLength > 0 && draftLength <= instructionLimit
    }

    // MARK: Generation

    /// Enter in the field.
    public func submit() {
        guard canSubmit else { return }
        if copiedText != nil {
            reply(to: recipient)
        } else {
            let instruction = draft.trimmingCharacters(in: .whitespacesAndNewlines)
            run(.compose(instruction: instruction, regenerate: false, key: Self.newKey()))
        }
    }

    /// One click on a recipient chip writes the reply at once.
    public func reply(to recipient: Recipient) {
        self.recipient = recipient
        settings.lastRecipient = recipient.rawValue
        guard let copied = copiedText, canSubmit else { return }
        let instruction = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        run(.reply(source: copied.text, recipient: recipient, instruction: instruction.isEmpty ? nil : instruction,
                   key: Self.newKey()))
    }

    /// "Another variant": a new request with a new key.
    public func regenerate() {
        guard let lastRequest, phase != .generating else { return }
        switch lastRequest {
        case let .compose(instruction, _, _):
            run(.compose(instruction: instruction, regenerate: true, key: Self.newKey()))
        case let .reply(source, recipient, instruction, _):
            run(.reply(source: source, recipient: recipient, instruction: instruction, key: Self.newKey()))
        }
    }

    /// Repeats the failed request with the same key: a lost answer is replayed, not charged again.
    public func retry() {
        guard let lastRequest, phase != .generating else { return }
        run(lastRequest)
    }

    public func cancelGeneration() {
        work?.cancel()
        work = nil
        if phase == .generating { phase = result.isEmpty ? .editing : .result }
    }

    private func run(_ request: Request) {
        work?.cancel()
        lastRequest = request
        issue = nil
        phase = .generating
        let generator = generator
        let inputLanguage = KeyboardLanguage.current()
        work = Task { [weak self] in
            do throws(GenerationError) {
                let output: GenerationResult
                switch request {
                case let .compose(instruction, regenerate, key):
                    output = try await generator.compose(instruction, regenerate: regenerate, inputLanguage: inputLanguage,
                                                         idempotencyKey: key)
                case let .reply(source, recipient, instruction, key):
                    output = try await generator.reply(to: source, recipient: recipient, instruction: instruction,
                                                       inputLanguage: inputLanguage, idempotencyKey: key)
                }
                guard !Task.isCancelled else { return }
                self?.finish(with: output)
            } catch {
                guard !Task.isCancelled else { return }
                self?.fail(with: error)
            }
        }
    }

    private func finish(with output: GenerationResult) {
        result = output.text
        phase = .result
        lastActivity = now()
        if let usage = output.usage { account.apply(usage: usage) }
        if let copied = copiedText { clipboard.markHandled(copied) }
    }

    private func fail(with error: GenerationError) {
        phase = result.isEmpty ? .editing : .result
        issue = .generation(error)
        switch error {
        case .signedOut: auth.sessionEnded()
        case .consentRequired, .limitReached: Task { try? await account.refreshMe() }
        default: break
        }
    }

    static func newKey() -> String { UUID().uuidString.lowercased() }

    // MARK: Result

    public func copyResult() {
        let text = result.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return }
        clipboard.copy(text)
        justCopied = true
        lastActivity = now()
        Task { [weak self] in
            try? await Task.sleep(for: .milliseconds(700))
            guard let self else { return }
            self.justCopied = false
            self.didCopy()
        }
    }

    /// Esc: stop the microphone first, then a running request, then the panel.
    public func escape() {
        switch voice.phase {
        case .requestingPermission, .recording, .transcribing:
            voice.cancel()
            return
        default:
            break
        }
        if phase == .generating {
            cancelGeneration()
        } else {
            requestClose()
        }
    }

    public func dismissCopied() {
        guard let copied = copiedText else { return }
        clipboard.markHandled(copied)
        mode = .compose
        issue = nil
        focusRequest += 1
    }

    public func clearIssue() {
        issue = nil
    }

    // MARK: Voice

    public func toggleVoice() {
        switch voice.phase {
        case .recording:
            Task { await voice.stop() }
        case .idle, .finished, .failed:
            guard auth.isSignedIn, !needsConsent else { return }
            issue = nil
            if voice.phase != .idle { voice.reset() }
            Task { await voice.start(language: settings.dictationLanguage) }
        case .requestingPermission, .transcribing:
            break
        }
    }

    public func cancelVoice() {
        voice.cancel()
        focusRequest += 1
    }

    private func observeVoice() {
        withObservationTracking {
            _ = voice.phase
        } onChange: { [weak self] in
            Task { @MainActor [weak self] in
                self?.voicePhaseChanged()
                self?.observeVoice()
            }
        }
    }

    private func voicePhaseChanged() {
        switch voice.phase {
        case let .finished(text):
            let spoken = text.trimmingCharacters(in: .whitespacesAndNewlines)
            voice.reset()
            guard !spoken.isEmpty else {
                issue = .nothingHeard
                return
            }
            let current = draft.trimmingCharacters(in: .whitespacesAndNewlines)
            draft = current.isEmpty ? spoken : current + " " + spoken
            focusRequest += 1
        case let .failed(error):
            voice.reset()
            if case .server(code: APIErrorCode.consentRequired?) = error {
                Task { try? await account.refreshMe() }
            }
            issue = .voice(error)
        default:
            break
        }
    }

    // MARK: Sign-in and consent

    public func submitEmail() {
        Task { await auth.requestCode(email: emailInput) }
    }

    public func submitCode() {
        Task {
            if await auth.verify(code: codeInput) {
                codeInput = ""
                focusRequest += 1
            }
        }
    }

    public func resendCode() {
        Task { await auth.resendCode() }
    }

    public func useAnotherEmail() {
        codeInput = ""
        auth.useAnotherEmail()
        focusRequest += 1
    }

    public func acceptConsent() {
        Task {
            do {
                try await account.acceptConsent()
                issue = nil
                focusRequest += 1
            } catch {
                issue = .consentFailed
            }
        }
    }

    /// Plans and payment happen on the website, in the browser.
    public func openPricing(_ url: URL) {
        openURL(url)
        requestClose()
    }
}
