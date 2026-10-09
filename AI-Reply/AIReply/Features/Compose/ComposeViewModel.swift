import Foundation
import Observation
import UIKit

/// Drives the in-app reply flow: type or dictate a message, choose a template,
/// generate.
///
/// It calls the SAME `AIReplyService` the keyboard calls, with the same profile,
/// the same template and the same working-hours context, so what the user sees
/// here is what the keyboard will produce. A separate code path would drift.
@MainActor
@Observable
final class ComposeViewModel {

    var message: String = ""
    var selectedTemplateID: String?
    private(set) var reply: String = ""
    private(set) var isGenerating = false
    private(set) var errorMessage: String?
    /// The failure behind `errorMessage`: CONSENT_REQUIRED sends the app back
    /// to its consent screen.
    private(set) var failure: AIReplyError?
    private(set) var didCopy = false

    @ObservationIgnored private let service = AIReplyService()
    @ObservationIgnored private var task: Task<Void, Never>?

    var characterCount: Int { AIReplyService.characterCount(message) }
    var characterLimit: Int { AIConfiguration.maximumMessageCharacters }
    var isOverLimit: Bool { characterCount > characterLimit }

    var canGenerate: Bool {
        !isGenerating && !isOverLimit && characterCount > 0 && selectedTemplateID != nil
    }

    func pasteFromClipboard() {
        guard UIPasteboard.general.hasStrings, let value = UIPasteboard.general.string else { return }
        message = value
        errorMessage = nil
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
    func generate(configuration: ReplyConfiguration, language: AppLanguage) {
        guard let templateID = selectedTemplateID,
              let template = configuration.template(id: templateID) else { return }

        // One request at a time. A second tap replaces the first rather than
        // racing it, so rapid taps cannot produce two answers or two bills.
        task?.cancel()
        errorMessage = nil
        failure = nil
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
            }
        }
    }

    func cancel() {
        task?.cancel()
        task = nil
        isGenerating = false
    }
}
