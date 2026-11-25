import AppIntents
import Foundation

extension Notification.Name {
    static let voiceAssistantOpenRequested = Notification.Name("AIReply.voiceAssistant.open")
    static let voiceAssistantControlRequested = Notification.Name("AIReply.voiceAssistant.control")
    static let voiceAssistantConsentWithdrawn = Notification.Name("AIReply.voiceAssistant.consentWithdrawn")
}

/// A short-lived, content-free marker survives a cold app launch. Opening a
/// control is an explicit user action; it never starts capture in the widget.
enum VoiceAssistantLaunchRequest {
    private static var url: URL? {
        FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: AppGroup.identifier)?
            .appendingPathComponent("voice-assistant-open.json")
    }

    static func request(now: Date = Date()) {
        guard let url, let data = try? JSONEncoder().encode(now) else { return }
        try? data.write(to: url, options: [.atomic, .completeFileProtectionUnlessOpen])
    }

    static func consume(now: Date = Date()) -> Bool {
        guard let url, let data = try? Data(contentsOf: url) else { return false }
        try? FileManager.default.removeItem(at: url)
        guard let date = try? JSONDecoder().decode(Date.self, from: data) else { return false }
        return now.timeIntervalSince(date) >= -5 && now.timeIntervalSince(date) <= 30
    }
}

enum VoiceAssistantDestination: String, AppEnum {
    case voice

    static var typeDisplayRepresentation: TypeDisplayRepresentation = "AI Reply screen"
    static var caseDisplayRepresentations: [Self: DisplayRepresentation] = [
        .voice: "Voice assistant"
    ]
}

/// Target membership is both app and widget. OpenIntent brings the app to the
/// foreground before the app's permission and recording flow can run.
struct OpenVoiceAssistantIntent: OpenIntent {
    static var title: LocalizedStringResource = "Open AI Reply Voice"
    static var description = IntentDescription("Open AI Reply to dictate an instruction.")

    @Parameter(title: "Screen")
    var target: VoiceAssistantDestination

    init() { target = .voice }

    @MainActor
    func perform() async throws -> some IntentResult {
        VoiceAssistantLaunchRequest.request()
        NotificationCenter.default.post(name: .voiceAssistantOpenRequested, object: nil)
        return .result()
    }
}

/// A direct, user-triggered recording action. LiveActivityIntent routes it to
/// the containing app; AudioRecordingIntent grants the system recording flow.
/// The app must create the Live Activity and activate capture before perform
/// returns. It never prompts for microphone/provider permission in background.
@available(iOS 18.0, *)
struct StartVoiceAssistantIntent: AudioRecordingIntent, LiveActivityIntent {
    static var title: LocalizedStringResource = "Start AI Reply Voice"
    static var description = IntentDescription("Start a voice instruction without opening AI Reply after first-time setup.")
    static var openAppWhenRun: Bool { false }
    static var authenticationPolicy: IntentAuthenticationPolicy { .requiresLocalDeviceAuthentication }

    @available(iOS 26.0, *)
    static var supportedModes: IntentModes { .background }

    @MainActor
    func perform() async throws -> some IntentResult {
        #if AIREPLY_MAIN_APP
        // Directly await the app-owned singleton. Dispatching a notification
        // and returning would leave activation outside this authorized intent.
        try await VoiceAssistantController.shared.startFromControl()
        #else
        // This body is metadata-only in the widget. An unexpected execution
        // target must never try to open hardware or claim capture has begun.
        throw VoiceAssistantIntentSetupRequired()
        #endif
        return .result()
    }
}

private struct VoiceAssistantIntentSetupRequired: LocalizedError {
    var errorDescription: String? {
        String(localized: "Open AI Reply once to allow microphone access and voice processing.")
    }
}

struct StopVoiceAssistantIntent: LiveActivityIntent {
    static var title: LocalizedStringResource = "Cancel voice session"
    static var isDiscoverable = false

    @Parameter(title: "Voice session")
    var sessionID: String

    init() { sessionID = "" }
    init(sessionID: String) { self.sessionID = sessionID }

    @MainActor
    func perform() async throws -> some IntentResult {
        // The private snapshot is intentionally unreadable while locked.
        // The session-bound, text-free command remains writable so Stop also
        // works from the Lock Screen; the app validates the session identity.
        guard !sessionID.isEmpty else { return .result() }
        _ = VoiceAssistantStore.shared.send(.init(kind: .stop, sessionID: sessionID))
        NotificationCenter.default.post(name: .voiceAssistantControlRequested, object: nil)
        return .result()
    }
}

struct PauseVoiceAssistantIntent: LiveActivityIntent {
    static var title: LocalizedStringResource = "Pause recording"
    static var isDiscoverable = false

    @Parameter(title: "Voice session")
    var sessionID: String

    init() { sessionID = "" }
    init(sessionID: String) { self.sessionID = sessionID }

    @MainActor
    func perform() async throws -> some IntentResult {
        guard !sessionID.isEmpty else { return .result() }
        _ = VoiceAssistantStore.shared.send(.init(kind: .pause, sessionID: sessionID))
        NotificationCenter.default.post(name: .voiceAssistantControlRequested, object: nil)
        return .result()
    }
}

struct FinishVoiceAssistantIntent: LiveActivityIntent {
    static var title: LocalizedStringResource = "Finish and generate response"
    static var isDiscoverable = false

    @Parameter(title: "Voice session")
    var sessionID: String

    init() { sessionID = "" }
    init(sessionID: String) { self.sessionID = sessionID }

    @MainActor
    func perform() async throws -> some IntentResult {
        guard !sessionID.isEmpty else { return .result() }
        _ = VoiceAssistantStore.shared.send(.init(kind: .finish, sessionID: sessionID))
        NotificationCenter.default.post(name: .voiceAssistantControlRequested, object: nil)
        return .result()
    }
}

@available(iOS 18.0, *)
struct AIReplyVoiceShortcuts: AppShortcutsProvider {
    static var appShortcuts: [AppShortcut] {
        AppShortcut(
            intent: StartVoiceAssistantIntent(),
            phrases: ["Start voice in \(.applicationName)", "Dictate with \(.applicationName)"],
            shortTitle: "AI Reply Voice",
            systemImageName: "mic.fill"
        )
        AppShortcut(
            intent: OpenVoiceAssistantIntent(),
            phrases: ["Open voice in \(.applicationName)"],
            shortTitle: "Set up AI Reply Voice",
            systemImageName: "mic.fill"
        )
    }
}
