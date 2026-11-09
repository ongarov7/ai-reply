import AVFoundation
import Foundation

/// A finished recording, in a temporary file.
struct WatchRecording: Equatable, Sendable {
    let requestID: String
    let file: URL
    let milliseconds: Int
    let bytes: Int
}

/// The microphone on the watch: on only between the user's two taps.
///
/// AAC in an M4A file at 16 kHz mono, which is what the server's transcription
/// endpoint reads, at a bit rate that keeps a typical instruction small enough
/// for WatchConnectivity's fast channel. The hard stop at 40 seconds is the
/// server's limit, enforced here so nothing is recorded just to be refused.
@MainActor
final class WatchAudioRecorder {

    enum RecorderError: Error {
        case permissionDenied
        case couldNotStart
    }

    private var recorder: AVAudioRecorder?
    private var requestID: String?

    var isRecording: Bool { recorder?.isRecording == true }

    /// Seconds recorded so far.
    var elapsed: TimeInterval { recorder?.currentTime ?? 0 }

    /// Asks once, on the user's first tap of the microphone.
    func requestPermission() async -> Bool {
        await AVAudioApplication.requestRecordPermission()
    }

    func start() throws {
        guard AVAudioApplication.shared.recordPermission == .granted else { throw RecorderError.permissionDenied }
        let id = WatchVoiceIdentifier.make()
        let file = FileManager.default.temporaryDirectory.appendingPathComponent("voice-\(id).m4a")
        let settings: [String: Any] = [
            AVFormatIDKey: kAudioFormatMPEG4AAC,
            AVSampleRateKey: 16_000,
            AVNumberOfChannelsKey: 1,
            AVEncoderBitRateKey: 24_000
        ]
        do {
            let session = AVAudioSession.sharedInstance()
            try session.setCategory(.record, mode: .default, options: [])
            try session.setActive(true)
            let recorder = try AVAudioRecorder(url: file, settings: settings)
            guard recorder.record(forDuration: TimeInterval(WatchVoice.maximumAudioSeconds)) else {
                throw RecorderError.couldNotStart
            }
            self.recorder = recorder
            requestID = id
        } catch {
            try? AVAudioSession.sharedInstance().setActive(false)
            try? FileManager.default.removeItem(at: file)
            throw RecorderError.couldNotStart
        }
    }

    /// Stops and hands over the file; nil if nothing usable was recorded.
    func stop() -> WatchRecording? {
        guard let recorder, let requestID else { return nil }
        let seconds = recorder.currentTime
        recorder.stop()
        self.recorder = nil
        self.requestID = nil
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)

        let milliseconds = min(Int(seconds * 1000), WatchVoice.maximumAudioSeconds * 1000)
        let attributes = try? FileManager.default.attributesOfItem(atPath: recorder.url.path)
        let bytes = (attributes?[.size] as? NSNumber)?.intValue ?? 0
        guard milliseconds >= WatchVoice.minimumAudioMilliseconds, bytes > 0, bytes <= WatchVoice.maximumAudioBytes else {
            try? FileManager.default.removeItem(at: recorder.url)
            return nil
        }
        return WatchRecording(requestID: requestID, file: recorder.url, milliseconds: milliseconds, bytes: bytes)
    }

    /// Stops and deletes what was recorded.
    func cancel() {
        guard let recorder else { return }
        recorder.stop()
        recorder.deleteRecording()
        self.recorder = nil
        requestID = nil
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
    }
}
