import AVFoundation

/// Why capture ended without being asked to.
enum CaptureInterruption: Equatable, Sendable {
    /// The audio hardware changed (a headset plugged in or out). The file is finished and holds
    /// everything recorded so far.
    case deviceChanged(inputAvailable: Bool)
    /// Writing the file failed; nothing usable was recorded.
    case failed
}

/// The microphone side of `VoiceController`; tests use a fake.
@MainActor
protocol AudioCapturing: AnyObject {
    /// Opens the default input and writes 16 kHz mono AAC to `fileURL`, at most `limit` seconds of it.
    /// Levels 0…1 arrive on the main actor; `onInterruption` fires at most once, and only when capture
    /// ends by itself.
    func start(writingTo fileURL: URL, limit: TimeInterval,
               onLevel: @escaping @MainActor @Sendable (Float) -> Void,
               onInterruption: @escaping @MainActor @Sendable (CaptureInterruption) -> Void) throws(VoiceError)
    /// Closes the microphone and the file. Returns the seconds of audio written, 0 when writing failed.
    /// Safe to call again or when nothing runs.
    func stop() -> TimeInterval
}

/// Microphone → 16 kHz mono AAC in .m4a through AVAudioEngine. Every recording gets a new engine,
/// so it uses the current default input and the microphone indicator goes off with it.
@MainActor
final class AVAudioEngineCapture: AudioCapturing {
    private var engine: AVAudioEngine?
    private var writer: CaptureWriter?
    private var configurationObserver: NotificationObservers?
    private var onInterruption: (@MainActor @Sendable (CaptureInterruption) -> Void)?
    /// Identifies the current recording and engine, so late callbacks of earlier ones are dropped.
    private var recordingID = 0
    private var engineID = 0
    private var restartedAfterChange = false
    private var duration: TimeInterval = 0

    func start(writingTo fileURL: URL, limit: TimeInterval,
               onLevel: @escaping @MainActor @Sendable (Float) -> Void,
               onInterruption: @escaping @MainActor @Sendable (CaptureInterruption) -> Void) throws(VoiceError) {
        _ = stop()
        recordingID &+= 1
        duration = 0
        restartedAfterChange = false
        guard AVCaptureDevice.default(for: .audio) != nil else { throw .noInputDevice }
        let id = recordingID
        let writer: CaptureWriter
        do {
            writer = try CaptureWriter(
                fileURL: fileURL,
                limit: limit,
                onLevels: { levels in
                    // One tap buffer can bring several levels: spread them so the waveform moves evenly.
                    for (index, level) in levels.enumerated() {
                        DispatchQueue.main.asyncAfter(deadline: .now() + LevelMeter.interval * Double(index)) {
                            MainActor.assumeIsolated { onLevel(level) }
                        }
                    }
                },
                onFailure: { [weak self] in
                    DispatchQueue.main.async {
                        MainActor.assumeIsolated { self?.writerFailed(id) }
                    }
                }
            )
        } catch {
            throw .recordingFailed
        }
        self.writer = writer
        self.onInterruption = onInterruption
        do throws(VoiceError) {
            try startEngine(feeding: writer)
        } catch {
            _ = stop()
            throw error
        }
    }

    func stop() -> TimeInterval {
        teardownEngine()
        onInterruption = nil
        if let writer {
            duration = writer.finish()
            self.writer = nil
        }
        return duration
    }

    private func startEngine(feeding writer: CaptureWriter) throws(VoiceError) {
        let engine = AVAudioEngine()
        let input = engine.inputNode
        let hardware = input.inputFormat(forBus: 0)
        let format = input.outputFormat(forBus: 0)
        // A zero format means the engine has no usable input; installing a tap would raise.
        guard hardware.sampleRate > 0, hardware.channelCount > 0, format.sampleRate > 0, format.channelCount > 0 else {
            throw .noInputDevice
        }
        engineID &+= 1
        let id = engineID
        // Observed before starting, so a change while the device opens is not missed.
        let center = NotificationCenter.default
        let token = center.addObserver(forName: .AVAudioEngineConfigurationChange, object: engine, queue: nil) { [weak self] _ in
            // Posted on the engine's internal queue, which must not wait for the engine's teardown.
            DispatchQueue.main.async {
                MainActor.assumeIsolated { self?.configurationChanged(id) }
            }
        }
        configurationObserver = NotificationObservers([(center, token)])
        input.installTap(onBus: 0, bufferSize: AVAudioFrameCount(format.sampleRate / 10), format: format, block: writer.tapBlock)
        engine.prepare()
        do {
            try engine.start()
        } catch {
            configurationObserver = nil
            input.removeTap(onBus: 0)
            throw .recordingFailed
        }
        self.engine = engine
    }

    private func teardownEngine() {
        configurationObserver = nil
        guard let engine else { return }
        engine.inputNode.removeTap(onBus: 0)
        engine.stop()
        self.engine = nil
    }

    /// The input or output hardware changed and the engine stopped itself.
    private func configurationChanged(_ id: Int) {
        guard id == engineID, engine != nil, let writer else { return }
        teardownEngine()
        let inputAvailable = AVCaptureDevice.default(for: .audio) != nil
        // A headset switching profiles as its microphone opens changes the format before any audio
        // arrives: carry on with the new format instead of ending an empty recording.
        if inputAvailable, !restartedAfterChange, !writer.hasAudio {
            restartedAfterChange = true
            do throws(VoiceError) {
                try startEngine(feeding: writer)
                return
            } catch {}
        }
        end(.deviceChanged(inputAvailable: inputAvailable))
    }

    private func writerFailed(_ id: Int) {
        guard id == recordingID, writer != nil else { return }
        end(.failed)
    }

    private func end(_ interruption: CaptureInterruption) {
        let handler = onInterruption
        _ = stop()
        handler?(interruption)
    }
}
