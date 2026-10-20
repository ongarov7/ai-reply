import Foundation
import Observation
import UIKit

/// Drives «Создать ответ»: paste, type or dictate a message, choose who it
/// is for, generate.
///
/// «Создать ответ» экраны: хабарлама, кімге жауап беретіні, жауап.
///
/// It calls the SAME `AIReplyService` the keyboard calls, with the same profile,
/// the same template and the same working-hours context, so what the user sees
/// here is what the keyboard will produce. A separate code path would drift.
@MainActor
@Observable
final class ComposeViewModel {

    var message: String = ""
    /// Who the reply is for: the keyboard's selected persona too
    /// (`SharedSettings.lastTemplateID`), so both start where the other left.
    private(set) var selectedTemplateID: String?
    /// Why a paste brought nothing, as a catalog key; cleared by any edit.
    private(set) var pasteNoticeKey: String?
    private(set) var reply: String = ""
    private(set) var isGenerating = false
    private(set) var errorMessage: String?
    /// The failure behind `errorMessage`: CONSENT_REQUIRED sends the app back
    /// to its consent screen.
    private(set) var failure: AIReplyError?
    private(set) var didCopy = false
    /// «Лимит ответов исчерпан», after a generation refused for a spent limit.
    private(set) var exhausted: ExhaustedOffer?

    @ObservationIgnored private let service: AIReplyService
    @ObservationIgnored private let settings: SharedSettings
    /// What the server said about the limit that refused the last request.
    @ObservationIgnored private let limitRefusal: () -> LimitRefusal?
    @ObservationIgnored private var task: Task<Void, Never>?

    init(settings: SharedSettings = .shared,
         service: AIReplyService = AIReplyService(),
         limitRefusal: @escaping () -> LimitRefusal? = { AccountUsageCache.lastLimitRefusal() }) {
        self.settings = settings
        self.service = service
        self.limitRefusal = limitRefusal
    }

    var characterCount: Int { AIReplyService.characterCount(message) }
    var characterLimit: Int { AIConfiguration.maximumMessageCharacters }
    var isOverLimit: Bool { characterCount > characterLimit }

    var canGenerate: Bool {
        !isGenerating && !isOverLimit && characterCount > 0 && selectedTemplateID != nil
    }

    // MARK: Recipient

    /// The keyboard's last persona when it is still shown, else the first.
    /// Nothing is written here: only the user's own choice changes what the
    /// keyboard has selected.
    func restoreRecipient(visible templates: [ReplyTemplate]) {
        let ids = templates.map(\.id)
        if let current = selectedTemplateID, ids.contains(current) { return }
        if let saved = settings.lastTemplateID, ids.contains(saved) {
            selectedTemplateID = saved
        } else {
            selectedTemplateID = ids.first
        }
    }

    func selectRecipient(_ id: String) {
        selectedTemplateID = id
        settings.setLastTemplateID(id)
    }

    // MARK: Message

    /// What the system Paste button handed over. iOS gives it only on the
    /// user's tap and never asks for permission; the app itself never reads
    /// the clipboard.
    func paste(_ text: String?) {
        let trimmed = text?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        guard !trimmed.isEmpty else {
            pasteNoticeKey = "compose.paste.empty"
            return
        }
        message = trimmed
        pasteNoticeKey = nil
        errorMessage = nil
        ProductEvents.track(.pasteSucceeded)
    }

    /// «Попробовать на примере».
    func useSample(_ text: String) {
        message = text
        pasteNoticeKey = nil
        errorMessage = nil
    }

    func messageDidChange() {
        pasteNoticeKey = nil
    }

    func appendDictated(_ transcript: String) {
        let addition = transcript.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !addition.isEmpty else { return }
        let separator = message.isEmpty || message.hasSuffix(" ") ? "" : " "
        message += separator + addition
        errorMessage = nil
    }

    func copyReply() {
        guard !reply.isEmpty else { return }
        UIPasteboard.general.string = reply
        didCopy = true
        Task {
            try? await Task.sleep(for: .seconds(2))
            didCopy = false
        }
    }

    /// - Parameter language: drives which localized error sentence is shown, and
    ///   which name a template is referred to by. It does NOT decide the reply's
    ///   language: that follows the incoming message.
    ///   - purchasesEnabled: replies can be bought on iOS right now, so a
    ///     spent limit may offer «Докупить ответы».
    func generate(configuration: ReplyConfiguration, language: AppLanguage, purchasesEnabled: Bool = false) {
        guard let templateID = selectedTemplateID,
              let template = configuration.template(id: templateID) else { return }

        // One request at a time. A second tap replaces the first rather than
        // racing it, so rapid taps cannot produce two answers or two bills.
        task?.cancel()
        errorMessage = nil
        failure = nil
        exhausted = nil
        reply = ""
        isGenerating = true

        let strings = AIReplyStrings.forLanguage(language)
        let request = AIReplyService.Request(
            message: message,
            template: template,
            configuration: configuration,
            uiLanguage: language
        )

        task = Task { [weak self] in
            guard let self else { return }
            do {
                let generated = try await self.service.generate(request)
                guard !Task.isCancelled else { return }
                self.reply = generated.text
                self.isGenerating = false
            } catch {
                guard !Task.isCancelled else { return }
                self.isGenerating = false
                let mapped = (error as? AIReplyError) ?? .serviceUnavailable
                guard mapped != .cancelled else { return }
                self.failure = mapped
                self.errorMessage = strings.message(for: mapped)
                self.exhausted = ExhaustedOffer.decide(failure: mapped, refusal: self.limitRefusal(),
                                                       purchasesEnabled: purchasesEnabled)
            }
        }
    }

    /// The sheet was closed; the error line under the reply stays.
    func dismissExhausted() {
        exhausted = nil
    }

    func cancel() {
        task?.cancel()
        task = nil
        isGenerating = false
    }
}
