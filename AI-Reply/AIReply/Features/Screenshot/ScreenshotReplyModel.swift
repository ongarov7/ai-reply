import Foundation
import Observation
import UIKit

/// A spoken instruction for a screenshot reply: Apple speech recognition, at
/// most 40 seconds in all, with pause and resume. Only the text reaches AI
/// Reply; the audio never leaves Apple's recogniser.
@MainActor
@Observable
final class ScreenshotInstructionRecorder {
    nonisolated static let limit: TimeInterval = 40

    /// Text of the segments recorded before a pause.
    private(set) var committed = ""
    private(set) var used: TimeInterval = 0
    private(set) var dictation: VoiceReplyViewModel
    private(set) var isPaused = false
    private var language: AppLanguage

    init(language: AppLanguage) {
        self.language = language
        dictation = VoiceReplyViewModel(maximumDuration: Self.limit)
        dictation.updateLanguage(language)
    }

    var isRecording: Bool { dictation.isRecording }
    var remaining: TimeInterval { max(0, Self.limit - used - (dictation.isRecording ? dictation.elapsed : 0)) }
    var text: String { Self.join(committed, dictation.transcript) }
    var statusMessage: String { dictation.statusMessage }
    var isAtLimit: Bool { remaining <= 0.5 }
    var hasStarted: Bool { used > 0 || dictation.isRecording || !text.isEmpty }

    func start() {
        guard !isRecording, !isAtLimit else { return }
        if isPaused || !dictation.transcript.isEmpty {
            // A new segment with what is left of the 40 seconds.
            committed = text
            dictation = VoiceReplyViewModel(maximumDuration: remaining)
            dictation.updateLanguage(language)
        }
        isPaused = false
        dictation.startRecording()
    }

    func pause() {
        guard isRecording else { return }
        dictation.stopRecording()
        used = min(Self.limit, used + dictation.elapsed)
        isPaused = true
    }

    /// Stops and keeps the text.
    func finish() {
        if isRecording {
            dictation.stopRecording()
            used = min(Self.limit, used + dictation.elapsed)
        }
        isPaused = false
    }

    /// Stops and throws the text away.
    func cancel() {
        dictation.stopRecording()
        committed = ""
        used = 0
        isPaused = false
        dictation = VoiceReplyViewModel(maximumDuration: Self.limit)
        dictation.updateLanguage(language)
    }

    nonisolated static func join(_ a: String, _ b: String) -> String {
        let first = a.trimmingCharacters(in: .whitespacesAndNewlines), second = b.trimmingCharacters(in: .whitespacesAndNewlines)
        if first.isEmpty { return second }
        if second.isEmpty { return first }
        return first + " " + second
    }
}

/// The screenshot screen: what was read, who wrote what, the instruction, the
/// price and the reply.
@MainActor
@Observable
final class ScreenshotReplyModel {

    enum CostState: Equatable {
        case idle
        case loading
        case ready(ScreenshotQuote)
        case failed
    }

    private(set) var context: ScreenshotContext?
    private(set) var image: UIImage?
    var conversation = ScreenshotConversation()
    var instruction = ""
    var instructionIsVoice = false
    var mode: ScreenshotReplyMode = .text
    var templateID: String = SharedSettings.shared.lastTemplateID ?? "friend"
    var replyLanguage: ReplyLanguagePreference?

    private(set) var cost: CostState = .idle
    private(set) var isGenerating = false
    private(set) var isWorking = false
    var reply: String?
    private(set) var lastCharge: (units: Int, refunded: Int)?
    var notice: String?
    /// The price waiting for the person's approval.
    var pendingApproval: ScreenshotQuote?
    var insufficient: (quote: ScreenshotQuote, canBuy: Bool)?

    private let store: ScreenshotContextStore
    private var observer: ScreenshotSignalObserver?
    private var estimateTask: Task<Void, Never>?
    private var generateTask: Task<Void, Never>?
    private var language: AppLanguage = .systemDefault

    init(store: ScreenshotContextStore = .shared) {
        self.store = store
    }

    var hasScreenshot: Bool { context != nil }
    var target: ScreenshotMessage? { conversation.target }
    var canGenerate: Bool {
        context != nil && !isGenerating && (mode == .image || target != nil)
    }

    // MARK: Lifecycle

    func appear(language: AppLanguage) {
        self.language = language
        observer = ScreenshotSignalObserver { [weak self] in
            Task { @MainActor in self?.reload(keepEdits: true) }
        }
        reload(keepEdits: false)
    }

    func disappear() {
        observer = nil
        estimateTask?.cancel()
    }

    /// Reads the stored screenshot. A different one resets the screen; the
    /// same one keeps what the person edited.
    func reload(keepEdits: Bool) {
        let stored = store.read()
        if stored?.id != context?.id || !keepEdits {
            context = stored
            image = stored.flatMap { store.imageData(for: $0) }.flatMap(UIImage.init(data:))
            conversation = stored?.conversation ?? ScreenshotConversation()
            if let preset = stored?.instruction, instruction.isEmpty { instruction = preset }
            reply = stored?.draft?.text
            lastCharge = nil
            notice = nil
            if stored == nil { mode = .text }
            refreshEstimate()
        }
    }

    // MARK: Editing

    func select(_ message: ScreenshotMessage) {
        conversation.targetID = message.id
        persistConversation()
    }

    func setRole(_ role: ScreenshotSenderRole, for message: ScreenshotMessage) {
        guard let index = conversation.messages.firstIndex(where: { $0.id == message.id }) else { return }
        conversation.messages[index].role = role
        conversation.messages[index].roleConfidence = 1
        conversation.messages[index].isEdited = true
        persistConversation()
    }

    func setText(_ text: String, for message: ScreenshotMessage) {
        guard let index = conversation.messages.firstIndex(where: { $0.id == message.id }) else { return }
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty {
            remove(message)
            return
        }
        conversation.messages[index].text = trimmed
        conversation.messages[index].textConfidence = 1
        conversation.messages[index].isEdited = true
        persistConversation()
    }

    func remove(_ message: ScreenshotMessage) {
        conversation.messages.removeAll { $0.id == message.id }
        if conversation.targetID == message.id { conversation.targetID = nil }
        persistConversation()
    }

    private func persistConversation() {
        let conversation = self.conversation
        context = store.update { $0.conversation = conversation; $0.draft = nil } ?? context
        refreshEstimate()
    }

    func instructionChanged() {
        refreshEstimate(debounce: true)
    }

    func modeChanged() {
        refreshEstimate()
    }

    // MARK: Screenshots

    /// A screenshot picked in the app (photo picker).
    func importPicked(_ data: Data) async {
        isWorking = true
        defer { isWorking = false }
        do {
            _ = try await ScreenshotIntake.ingest(data, source: .photos, language: language, store: store)
            instruction = ""
            instructionIsVoice = false
            reload(keepEdits: false)
        } catch {
            notice = AppSettings.localized("screenshot.error.unreadable", language: language)
        }
    }

    func crop(to rect: CGRect) async {
        guard let context else { return }
        isWorking = true
        defer { isWorking = false }
        do {
            _ = try await ScreenshotIntake.crop(context, to: rect, language: language, store: store)
            reload(keepEdits: false)
        } catch {
            notice = AppSettings.localized("screenshot.error.unreadable", language: language)
        }
    }

    func deleteScreenshot() {
        generateTask?.cancel()
        store.delete()
        context = nil
        image = nil
        conversation = ScreenshotConversation()
        reply = nil
        instruction = ""
        cost = .idle
    }

    // MARK: Price

    func refreshEstimate(debounce: Bool = false) {
        estimateTask?.cancel()
        guard let request = request(approved: 1, includeImage: false) else {
            cost = .idle
            return
        }
        cost = .loading
        estimateTask = Task { [weak self] in
            if debounce { try? await Task.sleep(for: .milliseconds(600)) }
            guard !Task.isCancelled else { return }
            do {
                let quote = try await ScreenshotReplyClient.estimate(request)
                guard !Task.isCancelled else { return }
                self?.cost = .ready(quote)
            } catch {
                guard !Task.isCancelled else { return }
                self?.cost = .failed
            }
        }
    }

    // MARK: Generation

    /// Generates with the price the person approved; nil asks the server.
    func generate(approved: Int? = nil) {
        guard canGenerate else { return }
        let approvedUnits: Int
        if let approved {
            approvedUnits = approved
        } else if case .ready(let quote) = cost {
            // An ordinary price needs no extra tap; a higher one does.
            if quote.needsConfirmation {
                if quote.exceedsBalance {
                    insufficient = (quote, false)
                } else {
                    pendingApproval = quote
                }
                return
            }
            approvedUnits = 1
        } else {
            approvedUnits = 1
        }
        guard let request = request(approved: approvedUnits, includeImage: true) else {
            notice = AppSettings.localized("screenshot.error.unreadable", language: language)
            return
        }
        isGenerating = true
        notice = nil
        generateTask = Task { [weak self] in
            defer { self?.isGenerating = false }
            do {
                let result = try await ScreenshotReplyClient.reply(request)
                self?.finish(result)
            } catch let failure as ScreenshotReplyFailure {
                self?.fail(failure)
            } catch {
                self?.fail(.failed(.serviceUnavailable))
            }
        }
    }

    func cancelGeneration() {
        generateTask?.cancel()
        isGenerating = false
    }

    private func finish(_ result: ScreenshotReplyResult) {
        let text = ReplyNetworking.unwrapQuotes(result.reply.trimmingCharacters(in: .whitespacesAndNewlines))
        reply = text
        lastCharge = (result.units, result.refundedUnits)
        cost = .ready(result.quote)
        // Waiting in the AI Reply keyboard for the chat.
        let draft = ScreenshotDraft(requestID: UUID().uuidString, text: text, createdAt: Date(), units: result.units)
        context = store.update { $0.draft = draft } ?? context
        UINotificationFeedbackGenerator().notificationOccurred(.success)
    }

    private func fail(_ failure: ScreenshotReplyFailure) {
        switch failure {
        case .confirmationRequired(let quote):
            cost = .ready(quote)
            pendingApproval = quote
        case .insufficientCredits(let quote, let canBuy):
            cost = .ready(quote)
            insufficient = (quote, canBuy)
        case .imageConsentRequired:
            ScreenshotConsent.revokeImage()
            mode = .text
            notice = AppSettings.localized("screenshot.error.consent", language: language)
        case .imageUnavailable:
            mode = .text
            notice = AppSettings.localized("screenshot.error.imageOff", language: language)
        case .imageLimitReached(let limit):
            mode = .text
            notice = String(format: AppSettings.localized("screenshot.error.imageLimit %lld", language: language), limit)
        case .unreadable:
            notice = AppSettings.localized("screenshot.error.unreadable", language: language)
        case .failed(let error):
            if error == .cancelled { return }
            notice = AIReplyStrings.forLanguage(language).message(for: error)
        }
        UINotificationFeedbackGenerator().notificationOccurred(.error)
    }

    func copyReply() {
        guard let reply, !reply.isEmpty else { return }
        UIPasteboard.general.string = reply
        notice = AppSettings.localized("screenshot.copied", language: language)
    }

    /// The edited reply is what the keyboard inserts.
    func replyEdited() {
        guard let reply else { return }
        context = store.update { context in
            context.draft?.text = reply
        } ?? context
    }

    // MARK: Request

    private func request(approved: Int, includeImage: Bool) -> ScreenshotReplyRequest? {
        guard let context else { return nil }
        let configuration = ProfileStore.shared.load()
        let template = configuration.templates.first { $0.id == templateID }
            ?? configuration.templates.first { $0.isVisible }
            ?? ReplyTemplate.builtIn(.friend, sortIndex: 0)
        var request = ScreenshotReplyRequest(
            mode: mode, conversation: conversation, image: nil,
            imageWidth: context.imageWidth, imageHeight: context.imageHeight,
            instruction: instruction, instructionIsVoice: instructionIsVoice,
            template: template, configuration: configuration, uiLanguage: language,
            replyLanguage: replyLanguage, approvedUnits: approved)
        if mode == .image {
            guard let upload = ScreenshotIntake.uploadImage(for: context, store: store) else { return nil }
            request.imageWidth = upload.1
            request.imageHeight = upload.2
            if includeImage { request.image = upload.0 }
        } else if conversation.target == nil {
            return nil
        }
        return request
    }
}

/// The in-app consents of screenshot replies, on this iPhone. The text mode
/// is covered by the general AI consent; showing the screenshot screen's
/// explanation once is remembered here. Sending the picture itself needs its
/// own explicit "Allow", tied to the server's consent version.
enum ScreenshotConsent {
    private static let introKey = "screenshot.consent.introSeen"
    private static let imageKey = "screenshot.consent.imageVersion"

    static var hasSeenIntro: Bool { AppGroup.defaults.bool(forKey: introKey) }
    static func markIntroSeen() { AppGroup.defaults.set(true, forKey: introKey) }

    static var allowsImage: Bool {
        AppGroup.defaults.string(forKey: imageKey) == ScreenshotFeature.imageConsentVersion
    }

    static func allowImage() { AppGroup.defaults.set(ScreenshotFeature.imageConsentVersion, forKey: imageKey) }
    static func revokeImage() { AppGroup.defaults.removeObject(forKey: imageKey) }
}
