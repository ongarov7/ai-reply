import AVFoundation

/// The real microphone permission (System Settings → Privacy & Security → Microphone).
@MainActor
public final class SystemMicrophonePermission: MicrophonePermission {
    /// Privacy & Security → Microphone, for an "open settings" button after a denial.
    public nonisolated static let privacySettingsURL = URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy_Microphone")!

    public init() {}

    public var status: MicrophoneStatus {
        Self.status(AVCaptureDevice.authorizationStatus(for: .audio))
    }

    public func request() async -> Bool {
        await AVCaptureDevice.requestAccess(for: .audio)
    }

    nonisolated static func status(_ status: AVAuthorizationStatus) -> MicrophoneStatus {
        switch status {
        case .authorized: .authorized
        case .denied: .denied
        case .restricted: .restricted
        case .notDetermined: .notDetermined
        @unknown default: .denied
        }
    }
}
