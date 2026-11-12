import AppKit
import Observation
import AirReplyCore
import AirReplySystem
import AirReplyVoice
import UniformTypeIdentifiers

public enum DesktopTemplate: String, CaseIterable, Sendable {
    case friend, business, customer, custom

    var labelKey: String.LocalizationValue {
        switch self {
        case .friend: "bar.template.friend"
        case .business: "bar.template.business"
        case .customer: "bar.template.customer"
        case .custom: "bar.template.custom"
        }
    }

    var recipient: Recipient {
        switch self {
        case .friend: .friend
        case .business: .business
        case .customer: .client
        case .custom: .work
        }
    }
}

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
        case manualReply
    }

    public enum Phase: Equatable {
        case editing
        case generating
        case result
    }

    /// What was sent last, so "Retry" can reuse its idempotency key (never charged twice).
    private struct ContextSnapshot: Sendable {
        let options: GenerationOptions
        let profile: ContextProfile?
        let query: String

        func resolve() async -> GenerationOptions {
            var resolved = options
            if let profile {
                let query = query
                resolved.context = await Task.detached(priority: .userInitiated) {
                    ContextRetriever.context(profile: profile, query: query)
                }.value
            }
            return resolved
        }
    }

    private enum Request {
        case compose(instruction: String, regenerate: Bool, key: String, options: ContextSnapshot)
        case reply(source: String, recipient: Recipient, instruction: String?, key: String, options: ContextSnapshot)
    }

    public var draft = ""
    public var emailInput = ""
    public var codeInput = ""
    public var manualReplySource = ""
    public var advancedExpanded = false
    public var template: DesktopTemplate = .customer
    public var selectedProfileID: UUID?
    public var customInstructions = ""
    public var outputLength: OutputLength = .standard
    public var editingProfile: ContextProfile?
    public private(set) var contextIssue: String?
    public private(set) var importingDocument = false
    public private(set) var contextStorageBusy = true
    public private(set) var referenceAttachments: [ContextAttachment] = []
    public let contextProfiles: ContextProfileStore
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
    private var choosingDocument = false
    private var importRequestID: UUID?
    private var contextAccountID: String?

    /// Hides the panel.
    public var requestClose: () -> Void = {}
    /// The reply was copied; the coordinator closes the panel and pastes if the user wants that.
    public var didCopy: () -> Void = {}
    public var openURL: (URL) -> Void = { NSWorkspace.shared.open($0) }
    public var openPrivacySettings: () -> Void = {}
    public var openAccountSettings: () -> Void = {}

    public init(auth: AuthSession, account: AccountStore, voice: VoiceController, generator: any Generating,
                clipboard: ClipboardMonitor, settings: AppSettings, now: @escaping () -> Date = Date.init,
                contextProfiles: ContextProfileStore = ContextProfileStore()) {
        self.auth = auth
        self.account = account
        self.voice = voice
        self.generator = generator
        self.clipboard = clipboard
        self.settings = settings
        self.now = now
        self.contextProfiles = contextProfiles
        contextAccountID = account.me?.user.id
        recipient = Recipient(rawValue: settings.lastRecipient) ?? .client
        switch recipient {
        case .friend: template = .friend
        case .business, .work: template = .business
        case .client: template = .customer
        }
        Task { [weak self] in
            guard let self else { return }
            defer { self.contextStorageBusy = false }
            do { try await self.contextProfiles.loadAsync() }
            catch { self.contextIssue = String(localized: "bar.context.load_failed") }
        }
        observeVoice()
        observeAccount()
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
            // A website purchase can complete seconds after the last read.
            if auth.isSignedIn { await account.refreshForForeground() }
            clearLimitIfLifted()
        }
    }

    /// The "no replies left" card goes as soon as a fresh balance has replies
    /// again (a purchase on the website), instead of waiting for the next submit.
    func clearLimitIfLifted() {
        if case .generation(.limitReached) = issue, let balance = account.balance, !balance.isExhausted {
            issue = nil
        }
    }

    public func panelDidClose() {
        lastActivity = now()
        if voice.phase == .recording || voice.phase == .requestingPermission { voice.cancel() }
    }

    /// Keep the panel on screen while a system prompt or the microphone needs it.
    public var holdsPanelOpen: Bool {
        if choosingDocument { return true }
        return switch voice.phase {
        case .requestingPermission, .recording, .transcribing: true
        default: false
        }
    }

    private func resetConversation() {
        draft = ""
        manualReplySource = ""
        referenceAttachments = []
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
    public var isReplyMode: Bool { mode != .compose }
    public var sourceText: String { copiedText?.text ?? (isReplyMode ? manualReplySource : "") }
    public var sourceLength: Int { TextMetrics.serverLength(sourceText) }
    public var draftLength: Int { TextMetrics.serverLength(draft) }
    public var sourceTooLong: Bool { sourceLength > sourceLimit }
    public var voiceAvailable: Bool { account.config.voiceTranscription }
    public var desktopContextAvailable: Bool { account.config.desktopContext }
    public var selectedProfile: ContextProfile? { contextProfiles.profiles.first { $0.id == selectedProfileID } }
    public var attachments: [ContextAttachment] { (selectedProfile?.attachments ?? []) + referenceAttachments }
    public var hasAdvancedContext: Bool {
        selectedProfile != nil || !customInstructions.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !attachments.isEmpty || outputLength == .extended
    }

    public var canSubmit: Bool {
        guard auth.isSignedIn, !needsConsent, phase != .generating else { return false }
        guard !importingDocument, !hasAdvancedContext || desktopContextAvailable else { return false }
        guard !hasAdvancedContext || !contextStorageBusy else { return false }
        guard TextMetrics.serverLength(customInstructions) <= 1000 else { return false }
        if isReplyMode { return sourceLength > 0 && !sourceTooLong && draftLength <= instructionLimit }
        return draftLength > 0 && draftLength <= instructionLimit
    }

    // MARK: Generation

    /// Enter in the field.
    public func submit() {
        guard canSubmit else { return }
        if isReplyMode {
            reply(to: recipient)
        } else {
            let instruction = draft.trimmingCharacters(in: .whitespacesAndNewlines)
            run(.compose(instruction: instruction, regenerate: false, key: Self.newKey(), options: generationOptions(query: instruction)))
        }
    }

    /// One click on a recipient chip writes the reply at once.
    public func reply(to recipient: Recipient) {
        self.recipient = recipient
        switch recipient {
        case .friend: template = .friend
        case .client: template = .customer
        case .business: template = .business
        case .work: if template != .custom { template = .business }
        }
        settings.lastRecipient = recipient.rawValue
        guard isReplyMode, canSubmit else { return }
        let instruction = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        run(.reply(source: sourceText, recipient: recipient, instruction: instruction.isEmpty ? nil : instruction,
                   key: Self.newKey(), options: generationOptions(query: sourceText + " " + instruction)))
    }

    /// "Another variant": a new request with a new key.
    public func regenerate() {
        guard let lastRequest, phase != .generating else { return }
        switch lastRequest {
        case let .compose(instruction, _, _, options):
            run(.compose(instruction: instruction, regenerate: true, key: Self.newKey(), options: options))
        case let .reply(source, recipient, instruction, _, options):
            run(.reply(source: source, recipient: recipient, instruction: instruction, key: Self.newKey(), options: options))
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
                case let .compose(instruction, regenerate, key, options):
                    let resolved = await options.resolve()
                    guard !Task.isCancelled else { return }
                    output = try await generator.compose(instruction, regenerate: regenerate, inputLanguage: inputLanguage,
                                                         idempotencyKey: key, options: resolved)
                case let .reply(source, recipient, instruction, key, options):
                    let resolved = await options.resolve()
                    guard !Task.isCancelled else { return }
                    output = try await generator.reply(to: source, recipient: recipient, instruction: instruction,
                                                       inputLanguage: inputLanguage, idempotencyKey: key, options: resolved)
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
        case .consentRequired: Task { try? await account.refreshMe() }
        case .limitReached:
            Task {
                try? await account.refreshMe()
                clearLimitIfLifted()
            }
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
        if let copied = copiedText { clipboard.markHandled(copied) }
        mode = .compose
        issue = nil
        focusRequest += 1
    }

    public func selectReplyMode() {
        if !isReplyMode { mode = .manualReply }
        focusRequest += 1
    }

    public func selectTemplate(_ value: DesktopTemplate) {
        template = value
        recipient = value.recipient
        settings.lastRecipient = recipient.rawValue
        if value == .custom { advancedExpanded = true }
    }

    private func generationOptions(query: String) -> ContextSnapshot {
        guard desktopContextAvailable else { return ContextSnapshot(options: GenerationOptions(), profile: nil, query: query) }
        var profile = selectedProfile ?? ContextProfile(name: "")
        profile.attachments += referenceAttachments
        let custom = customInstructions.trimmingCharacters(in: .whitespacesAndNewlines)
        if !custom.isEmpty { profile.instructions += (profile.instructions.isEmpty ? "" : "\n") + custom }
        return ContextSnapshot(options: GenerationOptions(outputCharacterLimit: outputLength.rawValue, recipient: template.recipient),
                               profile: profile, query: query)
    }

    public func beginProfileEdit() {
        editingProfile = selectedProfile ?? ContextProfile(name: "")
        contextIssue = nil
    }

    public func saveProfile() {
        guard let profile = editingProfile, !contextStorageBusy else { return }
        contextStorageBusy = true
        let editingAccountID = account.me?.user.id
        Task { [weak self] in
            guard let self else { return }
            defer { self.contextStorageBusy = false }
            do {
                try await self.contextProfiles.saveAsync(profile)
                if self.account.me?.user.id == editingAccountID { self.selectedProfileID = profile.id }
                self.editingProfile = nil
                self.contextIssue = nil
            } catch { self.contextIssue = String(localized: "bar.context.save_failed") }
        }
    }

    public func removeSelectedProfile() {
        guard let id = selectedProfileID, !contextStorageBusy else { return }
        contextStorageBusy = true
        Task { [weak self] in
            guard let self else { return }
            defer { self.contextStorageBusy = false }
            do {
                try await self.contextProfiles.removeAsync(id)
                if self.selectedProfileID == id { self.selectedProfileID = nil; self.editingProfile = nil }
                self.contextIssue = nil
            } catch { self.contextIssue = String(localized: "bar.context.save_failed") }
        }
    }

    public func removeAttachment(_ id: UUID) {
        guard !contextStorageBusy else { return }
        guard let profile = selectedProfile, profile.attachments.contains(where: { $0.id == id }) else {
            referenceAttachments.removeAll { $0.id == id }
            return
        }
        contextStorageBusy = true
        Task { [weak self] in
            guard let self else { return }
            defer { self.contextStorageBusy = false }
            do {
                try await self.contextProfiles.removeAttachmentAsync(profileID: profile.id, attachmentID: id)
                self.contextIssue = nil
            } catch { self.contextIssue = String(localized: "bar.context.save_failed") }
        }
    }

    public func clearContext() {
        importRequestID = nil
        selectedProfileID = nil
        customInstructions = ""
        referenceAttachments = []
        editingProfile = nil
        outputLength = .standard
        contextIssue = nil
    }

    public func chooseDocument() {
        guard !importingDocument, attachments.count < 4 else { return }
        let picker = NSOpenPanel()
        picker.allowedContentTypes = ["xlsx", "csv", "pdf", "docx", "txt"].compactMap { UTType(filenameExtension: $0) }
        picker.allowsMultipleSelection = false
        picker.canChooseDirectories = false
        picker.canChooseFiles = true
        choosingDocument = true
        picker.begin { [weak self] result in
            Task { @MainActor in
                guard let self else { return }
                self.choosingDocument = false
                guard result == .OK, let url = picker.url else { return }
                await self.importDocument(url)
            }
        }
    }

    public func importDocument(_ url: URL) async {
        guard !importingDocument, attachments.count < 4 else { return }
        importingDocument = true
        let requestID = UUID()
        importRequestID = requestID
        let targetProfileID = selectedProfileID
        contextIssue = nil
        defer { importingDocument = false }
        do {
            let attachment = try await DocumentContextImporter().parse(url: url)
            guard importRequestID == requestID else { return }
            if let targetProfileID {
                guard var profile = contextProfiles.profiles.first(where: { $0.id == targetProfileID }) else { return }
                profile.attachments.append(attachment)
                try await contextProfiles.saveAsync(profile)
            } else { referenceAttachments.append(attachment) }
        } catch {
            if importRequestID == requestID { contextIssue = String(localized: "bar.context.import_failed") }
        }
    }

    private func observeAccount() {
        withObservationTracking {
            _ = account.me?.user.id
            _ = account.balance
        } onChange: { [weak self] in
            Task { @MainActor [weak self] in
                guard let self else { return }
                let accountID = self.account.me?.user.id
                if self.contextAccountID != accountID {
                    self.clearContext()
                    self.contextAccountID = accountID
                }
                self.clearLimitIfLifted()
                self.observeAccount()
            }
        }
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
        requestClose()
        Task {
            let handoff = auth.isSignedIn ? await account.paymentHandoffURL(locale: APIClient.preferredLanguage()) : nil
            openURL(handoff ?? url)
        }
    }
}
