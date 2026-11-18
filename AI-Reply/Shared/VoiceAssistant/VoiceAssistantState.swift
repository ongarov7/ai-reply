import Foundation

/// One foreground-authorized microphone session. Private text never belongs
/// in ActivityKit attributes; only this protected App Group record carries it.
enum VoiceAssistantPhase: String, Codable, CaseIterable, Sendable {
    case ready, listening, speechDetected, transcribing, generating
    case responseReady, paused, stopped, error

    var isRecording: Bool { self == .listening || self == .speechDetected }
    var isActive: Bool { self != .stopped && self != .error }
}

enum VoiceAssistantKeyboardCaption: Equatable, Sendable {
    case speak
    case recognized(preview: String, fullText: String)
}

struct VoiceAssistantSnapshot: Codable, Equatable, Sendable {
    var sessionID: String
    var phase: VoiceAssistantPhase
    var transcript: String = ""
    var response: String = ""
    var requestID: String?
    var updatedAt: Date = Date()
    var expiresAt: Date = Date().addingTimeInterval(180)
    var keyboardSessionID: String?
    var error: String?
    var sourceMessage: String = ""
    var templateID: String?

    func isFresh(now: Date = Date()) -> Bool {
        now < expiresAt && updatedAt <= now.addingTimeInterval(10)
    }

    /// Text retained during another request/error must never become an
    /// insertable response for that request's new identity.
    func readyResponse(now: Date = Date()) -> String? {
        guard phase == .responseReady, isFresh(now: now),
              !response.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return nil }
        return response
    }

    /// The keyboard is an active-session surface, not a launcher. A killed
    /// recording owner cannot leave a misleading microphone for the full
    /// result lifetime; listening therefore requires a recent heartbeat.
    func isVisibleInKeyboard(now: Date = Date()) -> Bool {
        guard isFresh(now: now) else { return false }
        switch phase {
        case .listening, .speechDetected:
            return now.timeIntervalSince(updatedAt) <= 10
        case .transcribing, .generating:
            return now.timeIntervalSince(updatedAt) <= 90
        case .responseReady:
            return readyResponse(now: now) != nil
        case .error:
            return now.timeIntervalSince(updatedAt) < 10 &&
                !(error?.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ?? true)
        case .ready, .paused, .stopped:
            return false
        }
    }

    func showsMicrophoneInKeyboard(now: Date = Date()) -> Bool {
        isVisibleInKeyboard(now: now) && phase.isRecording
    }

    func keyboardCaptureCaption() -> VoiceAssistantKeyboardCaption? {
        guard phase.isRecording else { return nil }
        let text = transcript.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return .speak }
        return .recognized(preview: String(text.suffix(200)), fullText: text)
    }

    /// Owner heartbeats alone do not redraw UIKit; changed live speech does.
    /// Private text stays inside this in-memory identity and the keyboard.
    func keyboardRenderRevision(now: Date = Date()) -> String? {
        guard isVisibleInKeyboard(now: now) else { return nil }
        let content: String
        if phase.isRecording { content = transcript }
        else if phase == .responseReady { content = readyResponse(now: now) ?? "" }
        else if phase == .error { content = error ?? "" }
        else { content = "" }
        return [sessionID, phase.rawValue, requestID ?? "", content].joined(separator: "\u{1f}")
    }
}

/// Immutable keyboard/intent -> app commands. The app alone writes snapshots,
/// so no process has to lock or merge another process's mutable file.
struct VoiceAssistantCommand: Codable, Equatable, Identifiable, Sendable {
    enum Kind: String, Codable, Sendable {
        case stop, pause, finish, request, regenerate, context
    }

    var id: String = UUID().uuidString
    var kind: Kind
    var sessionID: String
    var keyboardSessionID: String?
    var sourceMessage: String?
    var templateID: String?
    var requestID: String?
    var createdAt: Date = Date()

    func isFresh(now: Date = Date()) -> Bool {
        now.timeIntervalSince(createdAt) < 180 && createdAt <= now.addingTimeInterval(10)
    }
}
