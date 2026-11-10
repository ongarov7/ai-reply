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
/// endpoint reads, at 24 kbit/s. The hard stop at 40 seconds is the server's
/// limit, enforced here so nothing is recorded just to be refused.
@MainActor
final class WatchAudioRecorder {

    enum RecorderError: Error {
        case permissionDenied
        case alreadyRecording
        case couldNotStart
    }

    private var recorder: AVAudioRecorder?
    private var requestID: String?
    /// The last length seen while recording. `currentTime` reads 0 once the
    /// recorder has stopped by itself at the limit, so it cannot be asked then.
    private var lastElapsed: TimeInterval = 0

    var isRecording: Bool { recorder?.isRecording == true }

    /// Seconds recorded so far.
    var elapsed: TimeInterval {
        if let recorder, recorder.isRecording {
            lastElapsed = max(lastElapsed, recorder.currentTime)
        }
        return lastElapsed
    }

    /// Asks once, on the user's first tap of the microphone.
    func requestPermission() async -> Bool {
        await AVAudioApplication.requestRecordPermission()
    }

    func start() throws {
        guard recorder == nil else { throw RecorderError.alreadyRecording }
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
            lastElapsed = 0
        } catch {
            try? AVAudioSession.sharedInstance().setActive(false)
            try? FileManager.default.removeItem(at: file)
            throw RecorderError.couldNotStart
        }
    }

    /// Stops and hands over the file; nil if nothing usable was recorded.
    /// Also after the recorder stopped by itself at 40 seconds: that is a
    /// full recording, not an empty one.
    func stop() -> WatchRecording? {
        guard let recorder, let requestID else { return nil }
        let seen = elapsed
        if recorder.isRecording { recorder.stop() }
        self.recorder = nil
        self.requestID = nil
        lastElapsed = 0
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)

        let url = recorder.url
        // The file itself says how long it is; what was seen while recording
        // is the fallback.
        let seconds = Self.duration(of: url) ?? seen
        let milliseconds = min(Int(seconds * 1000), WatchVoice.maximumAudioSeconds * 1000)
        let attributes = try? FileManager.default.attributesOfItem(atPath: url.path)
        let bytes = (attributes?[.size] as? NSNumber)?.intValue ?? 0
        guard milliseconds >= WatchVoice.minimumAudioMilliseconds, bytes > 0, bytes <= WatchVoice.maximumAudioBytes else {
            try? FileManager.default.removeItem(at: url)
            return nil
        }
        return WatchRecording(requestID: requestID, file: url, milliseconds: milliseconds, bytes: bytes)
    }

    /// Stops and deletes what was recorded.
    func cancel() {
        guard let recorder else { return }
        recorder.stop()
        recorder.deleteRecording()
        self.recorder = nil
        requestID = nil
        lastElapsed = 0
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
    }

    private static func duration(of url: URL) -> TimeInterval? {
        guard let file = try? AVAudioFile(forReading: url), file.fileFormat.sampleRate > 0 else { return nil }
        let seconds = Double(file.length) / file.fileFormat.sampleRate
        return seconds > 0 ? seconds : nil
    }
}
