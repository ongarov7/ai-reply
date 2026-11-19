import AppIntents
import Foundation
import Observation

/// The step after Take Screenshot in the person's own shortcut. iOS lets no
/// app capture another app's screen, and AI Reply does not try: the system's
/// Take Screenshot action captures it when the person runs the shortcut (from
/// Control Center, Back Tap or the Action button), and passes the picture
/// here. Nothing is sent anywhere by these intents: the text is recognised on
/// the iPhone and kept for at most an hour.

/// Reads the screenshot and opens AI Reply's screenshot screen.
struct ReplyToScreenshotIntent: AppIntent {
    static var title: LocalizedStringResource = "Reply to Screenshot"
    static var description = IntentDescription(
        "Reads the conversation in a screenshot on your iPhone and opens AI Reply to answer it. Add it after Take Screenshot in a shortcut.")
    static var openAppWhenRun: Bool { true }
    static var authenticationPolicy: IntentAuthenticationPolicy { .requiresLocalDeviceAuthentication }

    @Parameter(title: "Screenshot", supportedTypeIdentifiers: ["public.image"])
    var screenshot: IntentFile

    @Parameter(title: "Instruction", inputOptions: String.IntentInputOptions(multiline: true))
    var instruction: String?

    static var parameterSummary: some ParameterSummary {
        Summary("Reply to \(\.$screenshot)") { \.$instruction }
    }

    @MainActor
    func perform() async throws -> some IntentResult {
        // The screen opens at once and shows that it is reading.
        ScreenshotIntakeMonitor.shared.isReading = true
        ScreenshotLaunchRequest.request()
        NotificationCenter.default.post(name: .screenshotReplyOpenRequested, object: nil)
        defer { ScreenshotIntakeMonitor.shared.isReading = false }
        do {
            _ = try await ScreenshotIntake.ingest(screenshot.data, source: .shortcut, instruction: instruction,
                                                  language: SharedSettings.shared.effectiveAppLanguage)
        } catch {
            ScreenshotIntakeMonitor.shared.lastFailure = .unreadable
            throw ScreenshotIntentError.unreadable
        }
        ScreenshotIntakeMonitor.shared.lastFailure = nil
        return .result()
    }
}

/// The shortcut that stays in the chat: it reads the screenshot on the
/// iPhone and turns AI Reply Voice on at once (Dynamic Island, 40 seconds),
/// with the conversation as the turn's context. The person says how to
/// reply; the reply waits in the AI Reply keyboard. The app is not opened.
@available(iOS 18.0, *)
struct ReplyToScreenshotByVoiceIntent: AudioRecordingIntent, LiveActivityIntent {
    static var title: LocalizedStringResource = "Reply to Screenshot by Voice"
    static var description = IntentDescription(
        "Reads the conversation in a screenshot on your iPhone and starts AI Reply Voice without leaving the chat: say how to reply, and the reply waits in the AI Reply keyboard.")
    static var openAppWhenRun: Bool { false }
    static var authenticationPolicy: IntentAuthenticationPolicy { .requiresLocalDeviceAuthentication }

    @available(iOS 26.0, *)
    static var supportedModes: IntentModes { .background }

    @Parameter(title: "Screenshot", supportedTypeIdentifiers: ["public.image"])
    var screenshot: IntentFile

    static var parameterSummary: some ParameterSummary {
        Summary("Reply to \(\.$screenshot) by voice")
    }

    @MainActor
    func perform() async throws -> some IntentResult {
        let context: ScreenshotContext
        do {
            context = try await ScreenshotIntake.ingest(screenshot.data, source: .shortcut,
                                                        language: SharedSettings.shared.effectiveAppLanguage)
        } catch {
            throw ScreenshotIntentError.unreadable
        }
        // Capture and its Live Activity begin before perform returns, as
        // AudioRecordingIntent requires.
        try await VoiceAssistantController.shared.startFromScreenshot(context)
        return .result()
    }
}

/// Reads the screenshot and leaves it for the AI Reply keyboard, without
/// leaving the chat.
struct PrepareScreenshotForKeyboardIntent: AppIntent {
    static var title: LocalizedStringResource = "Use Screenshot in Keyboard"
    static var description = IntentDescription(
        "Reads the conversation in a screenshot on your iPhone so the AI Reply keyboard can answer it in the chat. Nothing is sent until you generate a reply.")
    static var openAppWhenRun: Bool { false }
    static var authenticationPolicy: IntentAuthenticationPolicy { .requiresLocalDeviceAuthentication }

    @Parameter(title: "Screenshot", supportedTypeIdentifiers: ["public.image"])
    var screenshot: IntentFile

    @Parameter(title: "Instruction", inputOptions: String.IntentInputOptions(multiline: true))
    var instruction: String?

    static var parameterSummary: some ParameterSummary {
        Summary("Use \(\.$screenshot) in the keyboard") { \.$instruction }
    }

    func perform() async throws -> some IntentResult & ProvidesDialog {
        let context: ScreenshotContext
        do {
            context = try await ScreenshotIntake.ingest(screenshot.data, source: .shortcut, instruction: instruction,
                                                        language: SharedSettings.shared.effectiveAppLanguage)
        } catch {
            throw ScreenshotIntentError.unreadable
        }
        guard !context.hasNoText else {
            return .result(dialog: IntentDialog("No chat text was found in this screenshot. Open the chat and try again."))
        }
        let count = context.conversation.messages.count
        return .result(dialog: IntentDialog("Ready: \(count) messages. Tap the message field and tap Reply on the screenshot strip of the AI Reply keyboard."))
    }
}

enum ScreenshotIntentError: Error, CustomLocalizedStringResourceConvertible {
    case unreadable

    var localizedStringResource: LocalizedStringResource {
        "AI Reply could not read this screenshot. Try again, or choose the screenshot in AI Reply."
    }
}

/// The app is reading a screenshot right now (an intent runs in the app's
/// process): the screenshot screen shows progress instead of the old one.
@MainActor
@Observable
final class ScreenshotIntakeMonitor {
    enum Failure: Equatable { case unreadable }

    static let shared = ScreenshotIntakeMonitor()
    var isReading = false
    var lastFailure: Failure?

    private init() {}
}
