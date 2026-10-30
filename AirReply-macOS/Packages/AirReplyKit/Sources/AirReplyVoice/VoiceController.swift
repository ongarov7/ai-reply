import AppKit
import Foundation
import Observation

/// Language hint sent with a recording. `auto` lets the server decide.
public enum DictationLanguage: String, Codable, CaseIterable, Sendable {
    case auto
    case ru
    case kk
    case en
}

public enum VoiceError: Error, Equatable, Sendable {
    case permissionDenied
    case permissionRestricted
    case noInputDevice
    case recordingFailed
    /// Under the minimum length the server accepts; nothing was sent.
    case tooShort
    /// Voice input is switched off on the server (`features.voice_transcription`).
    case unavailable
    /// The daily transcription limit is used up.
    case limitReached
    case rateLimited
    case offline
    /// The server answered with an error; `code` is the server error code when known.
    case server(code: String?)
}

public enum VoicePhase: Equatable, Sendable {
    case idle
    case requestingPermission
    case recording
    case transcribing
    case finished(String)
    case failed(VoiceError)
}

/// A finished recording on disk, deleted by the controller after use.
public struct RecordedAudio: Sendable, Equatable {
    public let fileURL: URL
    public let duration: TimeInterval
    /// MIME type of the file, e.g. "audio/mp4" for AAC in .m4a.
    public let contentType: String
    public let fileName: String

    public init(fileURL: URL, duration: TimeInterval, contentType: String, fileName: String) {
        self.fileURL = fileURL
        self.duration = duration
        self.contentType = contentType
        self.fileName = fileName
    }
}

/// Turns a recording into text. The app's implementation uploads it to the
/// AI Reply server (`POST /api/v1/ai/transcribe`); tests use fakes.
public protocol Transcribing: Sendable {
    func transcribe(_ audio: RecordedAudio, language: DictationLanguage) async throws(VoiceError) -> String
}

public enum MicrophoneStatus: Equatable, Sendable {
    case notDetermined
    case authorized
    case denied
    case restricted
}

@MainActor
public protocol MicrophonePermission: AnyObject {
    var status: MicrophoneStatus { get }
    /// Shows the system prompt when undetermined; returns whether access is granted.
    func request() async -> Bool
}

/// Push-to-talk dictation: idle → requestingPermission → recording → transcribing
/// → finished(text) | failed(error). The microphone is open only while recording.
@MainActor
@Observable
public final class VoiceController {
    public private(set) var phase: VoicePhase = .idle
    /// Recent input levels 0…1 for the waveform, oldest first, fixed length.
    public private(set) var levels: [Float] = VoiceController.silence
    public private(set) var elapsed: TimeInterval = 0
    /// Hard cap; recording stops by itself and goes on to transcription.
    public var maxDuration: TimeInterval

    /// Length of `levels`: one value per 50 ms, so the waveform shows the last two seconds.
    public nonisolated static let levelCount = 40

    /// Shorter recordings are dropped as `.tooShort` without an upload.
    static let minimumDuration: TimeInterval = 0.5
    static let tickInterval: TimeInterval = 0.1
    static let leftoverAge: TimeInterval = 60 * 60
    static let contentType = "audio/mp4"
    static let fileName = "voice.m4a"
    static var defaultDirectory: URL {
        FileManager.default.temporaryDirectory.appendingPathComponent("AirReplyVoice", isDirectory: true)
    }
    private static let silence = [Float](repeating: 0, count: levelCount)
    private static let screenLocked = Notification.Name("com.apple.screenIsLocked")

    private let transcriber: any Transcribing
    private let permission: any MicrophonePermission
    private let capture: any AudioCapturing
    private let clock: any VoiceClock
    private let directory: URL
    /// Bumped by every start and cancel, so callbacks of an older attempt change nothing.
    @ObservationIgnored private var generation = 0
    @ObservationIgnored private var language = DictationLanguage.auto
    @ObservationIgnored private var fileURL: URL?
    @ObservationIgnored private var startedAt: TimeInterval = 0
    @ObservationIgnored private var ticker: (any VoiceTicker)?
    @ObservationIgnored private(set) var transcriptionTask: Task<Void, Never>?
    @ObservationIgnored private var systemObservers: NotificationObservers?

    public init(transcriber: any Transcribing, permission: (any MicrophonePermission)? = nil, maxDuration: TimeInterval = 40) {
        self.transcriber = transcriber
        self.permission = permission ?? SystemMicrophonePermission()
        self.maxDuration = maxDuration
        capture = AVAudioEngineCapture()
        clock = SystemVoiceClock()
        directory = Self.defaultDirectory
        prepare(workspace: NSWorkspace.shared.notificationCenter, distributed: DistributedNotificationCenter.default())
    }

    /// Every dependency injectable, so tests run without a microphone, real time or system events.
    init(transcriber: any Transcribing, permission: any MicrophonePermission, maxDuration: TimeInterval = 40,
         capture: any AudioCapturing, clock: any VoiceClock, directory: URL,
         workspaceNotifications: NotificationCenter = NotificationCenter(),
         distributedNotifications: NotificationCenter = NotificationCenter()) {
        self.transcriber = transcriber
        self.permission = permission
        self.maxDuration = maxDuration
        self.capture = capture
        self.clock = clock
        self.directory = directory
        prepare(workspace: workspaceNotifications, distributed: distributedNotifications)
    }

    /// Asks for the microphone if needed and starts recording. Ignored while an attempt is in
    /// progress; a finished or failed one is reset first.
    public func start(language: DictationLanguage) async {
        switch phase {
        case .idle: break
        case .finished, .failed: reset()
        case .requestingPermission, .recording, .transcribing: return
        }
        generation &+= 1
        let attempt = generation
        self.language = language
        phase = .requestingPermission
        let denial = await permissionDenial()
        // Cancelled while the system prompt was up.
        guard attempt == generation else { return }
        if let denial {
            phase = .failed(denial)
        } else {
            beginRecording()
        }
    }

    /// Stops recording and transcribes what was said. Returns once the attempt is over, with the
    /// text or the error in `phase`.
    public func stop() async {
        switch phase {
        case .recording:
            await finishRecording()?.value
        case .transcribing:
            // Already stopped by itself (time limit, device change): wait for the same result.
            await transcriptionTask?.value
        case .requestingPermission:
            // Nothing recorded yet; make sure the microphone does not open after the prompt.
            cancel()
        case .idle, .finished, .failed:
            break
        }
    }

    /// Stops recording and drops the audio without sending it.
    public func cancel() {
        generation &+= 1
        stopTicker()
        _ = capture.stop()
        transcriptionTask?.cancel()
        transcriptionTask = nil
        discardFile()
        phase = .idle
        levels = Self.silence
        elapsed = 0
    }

    /// Back to idle after a finished or failed attempt. An attempt still in progress is
    /// cancelled, so the microphone never stays open behind an idle phase.
    public func reset() {
        guard phase != .idle else { return }
        cancel()
    }

    // MARK: - Recording

    /// nil when the microphone may be used; asks the user only while undetermined.
    private func permissionDenial() async -> VoiceError? {
        switch permission.status {
        case .authorized: return nil
        case .denied: return .permissionDenied
        case .restricted: return .permissionRestricted
        case .notDetermined:
            if await permission.request() { return nil }
            return permission.status == .restricted ? .permissionRestricted : .permissionDenied
        }
    }

    private func beginRecording() {
        let attempt = generation
        let url = directory.appendingPathComponent(UUID().uuidString).appendingPathExtension("m4a")
        do {
            try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        } catch {
            phase = .failed(.recordingFailed)
            return
        }
        do throws(VoiceError) {
            try capture.start(
                writingTo: url,
                limit: maxDuration,
                onLevel: { [weak self] level in self?.receive(level, attempt: attempt) },
                onInterruption: { [weak self] interruption in self?.captureInterrupted(interruption, attempt: attempt) }
            )
        } catch {
            Self.delete(url)
            phase = .failed(error)
            return
        }
        fileURL = url
        levels = Self.silence
        elapsed = 0
        startedAt = clock.now
        phase = .recording
        ticker = clock.every(Self.tickInterval) { [weak self] in self?.tick() }
    }

    private func tick() {
        guard phase == .recording else { return }
        elapsed = clock.now - startedAt
        if elapsed >= maxDuration {
            finishRecording()
        }
    }

    private func receive(_ level: Float, attempt: Int) {
        guard attempt == generation, phase == .recording else { return }
        var next = levels
        next.removeFirst()
        next.append(level.isNaN ? 0 : min(max(level, 0), 1))
        levels = next
    }

    private func captureInterrupted(_ interruption: CaptureInterruption, attempt: Int) {
        guard attempt == generation, phase == .recording else { return }
        switch interruption {
        case .deviceChanged(let inputAvailable):
            // What was said before the change is kept and sent on.
            finishRecording(ifTooShort: inputAvailable ? .recordingFailed : .noInputDevice)
        case .failed:
            stopTicker()
            _ = capture.stop()
            discardFile()
            phase = .failed(.recordingFailed)
        }
    }

    /// Closes the microphone and hands the file to the transcriber, or fails without sending
    /// when it holds too little audio. `shortError` replaces `.tooShort` when capture ended by itself.
    @discardableResult
    private func finishRecording(ifTooShort shortError: VoiceError? = nil) -> Task<Void, Never>? {
        stopTicker()
        let duration = capture.stop()
        elapsed = clock.now - startedAt
        guard let fileURL else {
            phase = .failed(.recordingFailed)
            return nil
        }
        guard duration >= Self.minimumDuration else {
            // A device that delivered nothing for a while is broken, not a short recording.
            let silentDevice = duration == 0 && elapsed >= Self.minimumDuration
            discardFile()
            phase = .failed(shortError ?? (silentDevice ? .recordingFailed : .tooShort))
            return nil
        }
        phase = .transcribing
        let audio = RecordedAudio(fileURL: fileURL, duration: duration, contentType: Self.contentType, fileName: Self.fileName)
        let attempt = generation
        let transcriber = transcriber
        let language = language
        let task = Task { [weak self] in
            let result: Result<String, VoiceError>
            do throws(VoiceError) {
                result = .success(try await transcriber.transcribe(audio, language: language))
            } catch {
                result = .failure(error)
            }
            Self.delete(audio.fileURL)
            self?.transcriptionEnded(result, attempt: attempt)
        }
        transcriptionTask = task
        return task
    }

    private func transcriptionEnded(_ result: Result<String, VoiceError>, attempt: Int) {
        guard attempt == generation, phase == .transcribing else { return }
        transcriptionTask = nil
        fileURL = nil
        switch result {
        case .success(let text): phase = .finished(text.trimmingCharacters(in: .whitespacesAndNewlines))
        case .failure(let error): phase = .failed(error)
        }
    }

    private func stopTicker() {
        ticker?.cancel()
        ticker = nil
    }

    private func discardFile() {
        guard let fileURL else { return }
        Self.delete(fileURL)
        self.fileURL = nil
    }

    private static func delete(_ url: URL) {
        try? FileManager.default.removeItem(at: url)
    }

    // MARK: - System

    private func prepare(workspace: NotificationCenter, distributed: NotificationCenter) {
        Self.removeLeftovers(in: directory, olderThan: Self.leftoverAge)
        let interrupt: @Sendable (Notification) -> Void = { [weak self] _ in
            performOnMain { self?.systemWillInterrupt() }
        }
        systemObservers = NotificationObservers([
            (workspace, workspace.addObserver(forName: NSWorkspace.willSleepNotification, object: nil, queue: nil, using: interrupt)),
            (distributed, distributed.addObserver(forName: Self.screenLocked, object: nil, queue: nil, using: interrupt)),
        ])
    }

    /// Sleep or a locked screen: never keep listening behind the user's back. A transcription
    /// already under way is left to finish, so its text is there when the user comes back.
    private func systemWillInterrupt() {
        if phase == .recording || phase == .requestingPermission {
            cancel()
        }
    }

    /// Deletes recordings an earlier run left behind (a crash or a forced quit mid-recording).
    static func removeLeftovers(in directory: URL, olderThan age: TimeInterval, now: Date = Date()) {
        let manager = FileManager.default
        let key = URLResourceKey.contentModificationDateKey
        guard let files = try? manager.contentsOfDirectory(at: directory, includingPropertiesForKeys: [key], options: .skipsHiddenFiles) else {
            return
        }
        for file in files {
            guard let modified = try? file.resourceValues(forKeys: [key]).contentModificationDate,
                  now.timeIntervalSince(modified) > age else { continue }
            try? manager.removeItem(at: file)
        }
    }
}
