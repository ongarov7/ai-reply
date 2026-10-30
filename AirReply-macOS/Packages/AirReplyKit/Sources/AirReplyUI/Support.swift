import AppKit
import Carbon.HIToolbox
import SwiftUI
import AirReplyCore
import AirReplyVoice

/// Sizes of the quick bar, shared by the views and the panel controller.
enum BarMetrics {
    static let width: CGFloat = 680
    static let cornerRadius: CGFloat = 16
    static let gap: CGFloat = 8
    /// Room around the cards so the window shadow is not clipped.
    static let shadowInset: CGFloat = 18
}

/// `NSVisualEffectView` behind SwiftUI content: the system material of popovers,
/// light or dark with the appearance.
struct VisualEffectBackground: NSViewRepresentable {
    var material: NSVisualEffectView.Material = .popover

    func makeNSView(context: Context) -> NSVisualEffectView {
        let view = NSVisualEffectView()
        view.material = material
        view.blendingMode = .behindWindow
        view.state = .active
        return view
    }

    func updateNSView(_ view: NSVisualEffectView, context: Context) {
        view.material = material
    }
}

/// A card of the quick bar: material, rounded corners and a hairline border.
struct BarCard<Content: View>: View {
    @ViewBuilder var content: Content

    var body: some View {
        content
            .background(VisualEffectBackground())
            .clipShape(RoundedRectangle(cornerRadius: BarMetrics.cornerRadius, style: .continuous))
            .overlay(
                RoundedRectangle(cornerRadius: BarMetrics.cornerRadius, style: .continuous)
                    .strokeBorder(Color.primary.opacity(0.08), lineWidth: 1)
            )
            .shadow(color: .black.opacity(0.18), radius: 14, y: 6)
    }
}

/// Language of the active keyboard layout (kk, ru, en…), a hint for the server.
enum KeyboardLanguage {
    static func current() -> String? {
        guard let source = TISCopyCurrentKeyboardInputSource()?.takeRetainedValue(),
              let pointer = TISGetInputSourceProperty(source, kTISPropertyInputSourceLanguages) else { return nil }
        let languages = Unmanaged<CFArray>.fromOpaque(pointer).takeUnretainedValue() as? [String]
        return languages?.first.map { String($0.prefix(2)).lowercased() }
    }
}

/// Uploads a recording to `POST /api/v1/ai/transcribe` (the server passes it to OpenAI).
struct ServerTranscriber: Transcribing {
    let client: APIClient
    /// Read on the main actor before each upload: the dictation setting and the keyboard layout.
    let languageHints: @MainActor @Sendable () -> [String]

    func transcribe(_ audio: RecordedAudio, language: DictationLanguage) async throws(VoiceError) -> String {
        guard let data = try? Data(contentsOf: audio.fileURL), !data.isEmpty else { throw .recordingFailed }
        let milliseconds = Int((audio.duration * 1000).rounded())
        guard milliseconds >= 300 else { throw .tooShort }
        var form = MultipartForm()
        form.add("request_id", UUID().uuidString.lowercased())
        form.add("duration_ms", String(min(milliseconds, 40_500)))
        let hints = await languageHints()
        if !hints.isEmpty { form.add("languages", hints.joined(separator: ",")) }
        form.add("purpose", "instruction")
        form.addFile("audio", fileName: audio.fileName, contentType: audio.contentType, data: data)

        struct Response: Decodable, Sendable { let text: String }
        do {
            return try await client.send(APIRequest("POST", "/api/v1/ai/transcribe", body: .multipart(form), timeout: 75),
                                         as: Response.self).text
        } catch {
            throw Self.voiceError(error)
        }
    }

    static func voiceError(_ error: APIError) -> VoiceError {
        switch error {
        case .offline, .cancelled: return .offline
        case .signedOut: return .server(code: APIErrorCode.unauthorized)
        case .invalidResponse: return .server(code: nil)
        case let .server(status, code, _, details, _):
            switch code {
            case APIErrorCode.notFound where status == 404: return .unavailable
            case APIErrorCode.voiceLimit: return .limitReached
            case APIErrorCode.rateLimited: return .rateLimited
            case APIErrorCode.invalidRequest where details["reason"]?.stringValue == "too_short": return .tooShort
            default: return .server(code: code)
            }
        }
    }

    /// Most likely language first, at most three, only those the server accepts.
    static func hints(setting: DictationLanguage, keyboard: String?) -> [String] {
        var ordered: [String] = []
        if setting != .auto { ordered.append(setting.rawValue) }
        if let keyboard, ["kk", "ru", "en"].contains(keyboard) { ordered.append(keyboard) }
        ordered += ["ru", "kk", "en"]
        var seen = Set<String>()
        return Array(ordered.filter { seen.insert($0).inserted }.prefix(3))
    }
}

extension Recipient {
    /// Key of the short label: «Другу», «Клиенту»…
    var labelKey: String.LocalizationValue {
        switch self {
        case .friend: "recipient.friend"
        case .client: "recipient.client"
        case .work: "recipient.colleague"
        case .business: "recipient.partner"
        }
    }

    var symbol: String {
        switch self {
        case .friend: "person"
        case .client: "briefcase"
        case .work: "person.2"
        case .business: "building.2"
        }
    }
}
