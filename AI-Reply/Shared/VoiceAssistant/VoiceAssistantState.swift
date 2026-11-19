import CryptoKit
import Foundation
#if canImport(UIKit)
import UIKit
#endif

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
    /// The screenshot this turn answers (`ScreenshotContext.id`): the reply
    /// is written from its whole conversation, `sourceMessage` is its target.
    var screenshotID: String?

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

    /// Owner heartbeats alone do not redraw UIKit; changed live speech does,
    /// and so does a copied message attached to the recording.
    /// Private text stays inside this in-memory identity and the keyboard.
    func keyboardRenderRevision(now: Date = Date()) -> String? {
        guard isVisibleInKeyboard(now: now) else { return nil }
        let content: String
        if phase.isRecording { content = transcript }
        else if phase == .responseReady { content = readyResponse(now: now) ?? "" }
        else if phase == .error { content = error ?? "" }
        else { content = "" }
        let context = sourceMessage.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? "-" : "context"
        return [sessionID, phase.rawValue, requestID ?? "", keyboardSessionID ?? "", context, content]
            .joined(separator: "\u{1f}")
    }
}

/// Voice results the keyboard already inserted or closed, by request ID,
/// shared by every keyboard instance (iOS may build a new keyboard for each
/// appearance) for longer than any result lives, so the same response never
/// comes back for a second insertion here or in another chat. Inserted text
/// is remembered only as a one-way digest, to recognise it when it is copied
/// back from the chat; no message or response text is stored.
struct VoiceAssistantResultLedger {
    static let lifetime: TimeInterval = 15 * 60
    private static let key = "voiceAssistant.handledResults.v1"
    private static let capacity = 40
    private let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) { self.defaults = defaults }

    func contains(requestID: String, now: Date = Date()) -> Bool {
        entries(now: now)["id:" + requestID] != nil
    }

    func containsInsertedText(_ text: String, now: Date = Date()) -> Bool {
        guard let digest = Self.digest(text) else { return false }
        return entries(now: now)["text:" + digest] != nil
    }

    func record(requestID: String?, insertedText: String? = nil, now: Date = Date()) {
        var values = entries(now: now)
        let stamp = now.timeIntervalSince1970
        if let requestID, !requestID.isEmpty { values["id:" + requestID] = stamp }
        if let insertedText, let digest = Self.digest(insertedText) { values["text:" + digest] = stamp }
        if values.count > Self.capacity {
            let newest = values.sorted { $0.value > $1.value }.prefix(Self.capacity)
            values = Dictionary(uniqueKeysWithValues: newest.map { ($0.key, $0.value) })
        }
        defaults.set(values, forKey: Self.key)
    }

    private func entries(now: Date) -> [String: Double] {
        let stamp = now.timeIntervalSince1970
        let stored = defaults.dictionary(forKey: Self.key) ?? [:]
        var values: [String: Double] = [:]
        for (key, value) in stored {
            guard let time = (value as? NSNumber)?.doubleValue,
                  stamp - time < Self.lifetime, time <= stamp + 60 else { continue }
            values[key] = time
        }
        return values
    }

    /// Case and spacing do not make a copied response another message.
    static func digest(_ text: String) -> String? {
        let normalized = text.split(whereSeparator: \.isWhitespace).joined(separator: " ").lowercased()
        guard !normalized.isEmpty else { return nil }
        return SHA256.hash(data: Data(normalized.utf8)).map { String(format: "%02x", $0) }.joined()
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

#if canImport(UIKit)
/// Live dictation in a two-line caption reads newest-last: the end of the
/// text that fits, after an ellipsis, instead of UIKit's head truncation,
/// which keeps the first line and cuts the middle ("Ответь / …третиться").
enum VoiceCaption {
    /// The end of `text` that fits in `lines` lines, after an ellipsis and,
    /// when one is close, a word boundary: live dictation reads newest-last.
    static func newestWords(of text: String, fitting width: CGFloat, lines: Int, font: UIFont) -> String {
        let flat = text.split(whereSeparator: \.isNewline).joined(separator: " ")
            .trimmingCharacters(in: .whitespaces)
        guard width > 1, lines > 0, !flat.isEmpty else { return flat }
        let limit = ceil(font.lineHeight * CGFloat(lines) + font.leading * CGFloat(max(0, lines - 1))) + 0.5
        func fits(_ value: String) -> Bool {
            let rect = (value as NSString).boundingRect(with: CGSize(width: width, height: .greatestFiniteMagnitude),
                options: [.usesLineFragmentOrigin, .usesFontLeading], attributes: [.font: font], context: nil)
            return ceil(rect.height) <= limit
        }
        if fits(flat) { return flat }
        let characters = Array(flat)
        var low = 1, high = characters.count - 1, best = 1
        while low <= high {
            let middle = (low + high) / 2
            if fits("…" + String(characters.suffix(middle))) { best = middle; low = middle + 1 } else { high = middle - 1 }
        }
        var tail = String(characters.suffix(best))
        if let space = tail.firstIndex(of: " "), tail.distance(from: tail.startIndex, to: space) <= 14,
           tail.index(after: space) < tail.endIndex {
            tail = String(tail[tail.index(after: space)...])
        }
        return "…" + tail
    }
}
#endif
