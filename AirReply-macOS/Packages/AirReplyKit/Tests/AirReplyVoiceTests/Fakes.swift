import Foundation
@testable import AirReplyVoice

/// Time that moves only when a test says so; due ticks fire in order, synchronously.
@MainActor
final class ManualClock: VoiceClock {
    private(set) var now: TimeInterval = 0
    private var tickers: [ManualTicker] = []

    func every(_ interval: TimeInterval, _ action: @escaping @MainActor @Sendable () -> Void) -> any VoiceTicker {
        let ticker = ManualTicker(interval: interval, start: now, action: action)
        tickers.append(ticker)
        return ticker
    }

    func advance(by seconds: TimeInterval) {
        let target = now + seconds
        while let ticker = tickers.filter({ !$0.isCancelled && $0.nextFire <= target }).min(by: { $0.nextFire < $1.nextFire }) {
            now = ticker.nextFire
            ticker.fired += 1
            ticker.action()
        }
        now = target
    }
}

@MainActor
final class ManualTicker: VoiceTicker {
    let interval: TimeInterval
    let start: TimeInterval
    let action: @MainActor @Sendable () -> Void
    var fired = 0
    private(set) var isCancelled = false

    init(interval: TimeInterval, start: TimeInterval, action: @escaping @MainActor @Sendable () -> Void) {
        self.interval = interval
        self.start = start
        self.action = action
    }

    /// Multiplied rather than summed, so tick times stay exact for round numbers.
    var nextFire: TimeInterval { start + interval * Double(fired + 1) }

    func cancel() {
        isCancelled = true
    }
}

/// Stands in for the microphone: writes a placeholder file and reports clock time as audio.
@MainActor
final class FakeCapture: AudioCapturing {
    var startError: VoiceError?
    /// What `stop()` reports; nil means the clock time since start.
    var recordedDuration: TimeInterval?
    private(set) var startCount = 0
    private(set) var isRunning = false
    private(set) var fileURL: URL?
    private(set) var limit: TimeInterval?
    private var onLevel: (@MainActor @Sendable (Float) -> Void)?
    private var onInterruption: (@MainActor @Sendable (CaptureInterruption) -> Void)?
    private var startedAt: TimeInterval = 0
    private var lastDuration: TimeInterval = 0
    private let clock: ManualClock

    init(clock: ManualClock) {
        self.clock = clock
    }

    func start(writingTo fileURL: URL, limit: TimeInterval,
               onLevel: @escaping @MainActor @Sendable (Float) -> Void,
               onInterruption: @escaping @MainActor @Sendable (CaptureInterruption) -> Void) throws(VoiceError) {
        startCount += 1
        if let startError { throw startError }
        FileManager.default.createFile(atPath: fileURL.path, contents: Data("not really aac".utf8))
        self.fileURL = fileURL
        self.limit = limit
        self.onLevel = onLevel
        self.onInterruption = onInterruption
        startedAt = clock.now
        isRunning = true
    }

    func stop() -> TimeInterval {
        if isRunning {
            isRunning = false
            lastDuration = recordedDuration ?? clock.now - startedAt
            onInterruption = nil
        }
        return lastDuration
    }

    /// Still delivered after `stop()`, like a level the real capture queued just before it.
    func send(level: Float) {
        onLevel?(level)
    }

    /// Ends capture by itself, as a device change or a failed write does.
    func interrupt(_ interruption: CaptureInterruption) {
        let handler = onInterruption
        _ = stop()
        handler?(interruption)
    }
}

@MainActor
final class FakePermission: MicrophonePermission {
    var status: MicrophoneStatus
    /// What the user picks in the system prompt.
    let answer: MicrophoneStatus
    /// Keeps the prompt up until `answerPrompt()`.
    let holdsPrompt: Bool
    private(set) var requestCount = 0
    private var prompt: CheckedContinuation<Void, Never>?

    init(_ status: MicrophoneStatus, answer: MicrophoneStatus = .authorized, holdsPrompt: Bool = false) {
        self.status = status
        self.answer = answer
        self.holdsPrompt = holdsPrompt
    }

    func request() async -> Bool {
        requestCount += 1
        if holdsPrompt {
            await withCheckedContinuation { prompt = $0 }
        }
        status = answer
        return answer == .authorized
    }

    func answerPrompt() {
        prompt?.resume()
        prompt = nil
    }
}

actor FakeTranscriber: Transcribing {
    struct Call: Sendable {
        let audio: RecordedAudio
        let language: DictationLanguage
        let fileExisted: Bool
    }

    private let result: Result<String, VoiceError>
    /// Keeps `transcribe` waiting until `release()`.
    private let holds: Bool
    private(set) var calls: [Call] = []
    private var callWaiters: [CheckedContinuation<Void, Never>] = []
    private var gate: CheckedContinuation<Void, Never>?
    private var released = false

    init(_ result: Result<String, VoiceError>, holds: Bool = false) {
        self.result = result
        self.holds = holds
    }

    func transcribe(_ audio: RecordedAudio, language: DictationLanguage) async throws(VoiceError) -> String {
        let exists = FileManager.default.fileExists(atPath: audio.fileURL.path)
        calls.append(Call(audio: audio, language: language, fileExisted: exists))
        callWaiters.forEach { $0.resume() }
        callWaiters = []
        if holds, !released {
            await withCheckedContinuation { gate = $0 }
        }
        switch result {
        case .success(let text): return text
        case .failure(let error): throw error
        }
    }

    /// Returns once `transcribe` has been entered.
    func waitForCall() async {
        guard calls.isEmpty else { return }
        await withCheckedContinuation { callWaiters.append($0) }
    }

    func release() {
        released = true
        gate?.resume()
        gate = nil
    }
}

/// A controller wired to fakes in a private temporary directory, removed with the harness.
@MainActor
final class Harness {
    let clock = ManualClock()
    let capture: FakeCapture
    let permission: FakePermission
    let transcriber: FakeTranscriber
    let directory: URL
    let workspace = NotificationCenter()
    let distributed = NotificationCenter()
    let controller: VoiceController

    init(permission: MicrophoneStatus = .authorized, answer: MicrophoneStatus = .authorized, holdsPrompt: Bool = false,
         result: Result<String, VoiceError> = .success("Привет"), holdsTranscription: Bool = false,
         maxDuration: TimeInterval = 40) {
        capture = FakeCapture(clock: clock)
        self.permission = FakePermission(permission, answer: answer, holdsPrompt: holdsPrompt)
        transcriber = FakeTranscriber(result, holds: holdsTranscription)
        directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("AirReplyVoiceTests-\(UUID().uuidString)", isDirectory: true)
        controller = VoiceController(
            transcriber: transcriber, permission: self.permission, maxDuration: maxDuration,
            capture: capture, clock: clock, directory: directory,
            workspaceNotifications: workspace, distributedNotifications: distributed
        )
    }

    deinit {
        try? FileManager.default.removeItem(at: directory)
    }

    var recordings: [URL] {
        (try? FileManager.default.contentsOfDirectory(at: directory, includingPropertiesForKeys: nil)) ?? []
    }

    func fileExists(_ url: URL?) -> Bool {
        url.map { FileManager.default.fileExists(atPath: $0.path) } ?? false
    }
}
