import AVFoundation

/// Converts tap buffers to 16 kHz mono, encodes them as AAC into an .m4a file and meters them.
/// `append` runs on the engine's tap thread and `finish` on the main actor; a lock keeps them apart.
final class CaptureWriter: @unchecked Sendable {
    static let sampleRate: Double = 16_000
    /// About 4 KB a second of speech: a 40 s recording stays far below the server's 4 MiB cap.
    static let bitRate = 32_000

    private let lock = NSLock()
    private let outputFormat: AVAudioFormat
    private let maxFrames: AVAudioFramePosition
    private let onLevels: @Sendable ([Float]) -> Void
    private let onFailure: @Sendable () -> Void
    private var file: AVAudioFile?
    private var converter: AVAudioConverter?
    private var meter = LevelMeter()
    private var framesWritten: AVAudioFramePosition = 0
    private var failed = false

    /// `onLevels` and `onFailure` are called on the thread that appends; `onFailure` at most once.
    init(fileURL: URL, limit: TimeInterval,
         onLevels: @escaping @Sendable ([Float]) -> Void,
         onFailure: @escaping @Sendable () -> Void) throws {
        guard let format = AVAudioFormat(commonFormat: .pcmFormatFloat32, sampleRate: Self.sampleRate, channels: 1, interleaved: false) else {
            throw CaptureWriterError()
        }
        let settings: [String: Any] = [
            AVFormatIDKey: kAudioFormatMPEG4AAC,
            AVSampleRateKey: Self.sampleRate,
            AVNumberOfChannelsKey: 1,
            AVEncoderBitRateKey: Self.bitRate,
        ]
        file = try AVAudioFile(forWriting: fileURL, settings: settings, commonFormat: .pcmFormatFloat32, interleaved: false)
        outputFormat = format
        maxFrames = AVAudioFramePosition(exactly: (limit * Self.sampleRate).rounded(.up)).map { max($0, 0) } ?? .max
        self.onLevels = onLevels
        self.onFailure = onFailure
    }

    /// Made here, outside the main actor, because the engine calls it on its own thread.
    var tapBlock: AVAudioNodeTapBlock {
        { [self] buffer, _ in append(buffer) }
    }

    var hasAudio: Bool {
        lock.withLock { framesWritten > 0 }
    }

    func append(_ buffer: AVAudioPCMBuffer) {
        let (levels, justFailed) = lock.withLock { () -> ([Float], Bool) in
            // Drained inside the lock, so no reference to the file outlives a `finish` that follows.
            autoreleasepool {
                guard !failed, let file else { return ([], false) }
                do {
                    return (try write(buffer, to: file), false)
                } catch {
                    failed = true
                    self.file = nil
                    converter = nil
                    return ([], true)
                }
            }
        }
        if justFailed { onFailure() }
        if !levels.isEmpty { onLevels(levels) }
    }

    /// Closes the file and returns the seconds of audio in it, or 0 when writing failed. Safe to call again.
    func finish() -> TimeInterval {
        lock.withLock {
            autoreleasepool {
                if !failed, let file, let converter {
                    do {
                        _ = try drain(converter, into: file)
                    } catch {
                        failed = true
                    }
                }
                if #available(macOS 15, *) { file?.close() }
                // On macOS 14 releasing the file is what writes its header.
                file = nil
                converter = nil
                return failed ? 0 : Double(framesWritten) / outputFormat.sampleRate
            }
        }
    }

    /// Converts one tap buffer and appends it up to the frame limit; returns the new levels.
    private func write(_ buffer: AVAudioPCMBuffer, to file: AVAudioFile) throws -> [Float] {
        guard framesWritten < maxFrames, buffer.frameLength > 0 else { return [] }
        var levels: [Float] = []
        if let converter, converter.inputFormat != buffer.format {
            // A device switch changed the tap format; the file's format stays the same.
            levels += try drain(converter, into: file)
            self.converter = nil
        }
        let converter = try self.converter ?? makeConverter(from: buffer.format)
        let ratio = outputFormat.sampleRate / buffer.format.sampleRate
        let capacity = AVAudioFrameCount((Double(buffer.frameLength) * ratio).rounded(.up)) + 1024
        let input = PendingInput(buffer)
        while true {
            let output = try makeOutput(capacity: capacity)
            var error: NSError?
            let status = converter.convert(to: output, error: &error) { _, inputStatus in
                input.next(inputStatus)
            }
            guard status != .error else { throw CaptureWriterError() }
            levels += try store(output, in: file)
            // `.haveData` means the output filled up while input is left to convert.
            if status != .haveData { return levels }
        }
    }

    /// The resampler holds back up to one block of input (about 85 ms) until it is told the
    /// stream ended; without this the end of every recording would be cut off.
    private func drain(_ converter: AVAudioConverter, into file: AVAudioFile) throws -> [Float] {
        var levels: [Float] = []
        while true {
            let output = try makeOutput(capacity: 4096)
            var error: NSError?
            let status = converter.convert(to: output, error: &error) { _, inputStatus in
                inputStatus.pointee = .endOfStream
                return nil
            }
            guard status != .error else { throw CaptureWriterError() }
            levels += try store(output, in: file)
            if status != .haveData || output.frameLength == 0 { return levels }
        }
    }

    private func store(_ output: AVAudioPCMBuffer, in file: AVAudioFile) throws -> [Float] {
        let room = maxFrames - framesWritten
        guard output.frameLength > 0, room > 0 else { return [] }
        output.frameLength = AVAudioFrameCount(min(AVAudioFramePosition(output.frameLength), room))
        try file.write(from: output)
        framesWritten += AVAudioFramePosition(output.frameLength)
        return meter.add(output)
    }

    private func makeConverter(from format: AVAudioFormat) throws -> AVAudioConverter {
        guard let converter = AVAudioConverter(from: format, to: outputFormat) else { throw CaptureWriterError() }
        // Mix every input channel into the mono file instead of keeping only the first one.
        converter.downmix = true
        self.converter = converter
        return converter
    }

    private func makeOutput(capacity: AVAudioFrameCount) throws -> AVAudioPCMBuffer {
        guard let output = AVAudioPCMBuffer(pcmFormat: outputFormat, frameCapacity: capacity) else { throw CaptureWriterError() }
        return output
    }
}

private struct CaptureWriterError: Error {}

/// Gives the converter one tap buffer, then reports that nothing more is ready yet, which keeps
/// the resampler's state for the next buffer.
private final class PendingInput: @unchecked Sendable {
    private var buffer: AVAudioPCMBuffer?

    init(_ buffer: AVAudioPCMBuffer) {
        self.buffer = buffer
    }

    func next(_ status: UnsafeMutablePointer<AVAudioConverterInputStatus>) -> AVAudioBuffer? {
        guard let buffer else {
            status.pointee = .noDataNow
            return nil
        }
        self.buffer = nil
        status.pointee = .haveData
        return buffer
    }
}

/// One smoothed level 0…1 per 50 ms of 16 kHz mono audio: RMS in dBFS, mapped from −50…0 dB.
struct LevelMeter {
    static let interval: TimeInterval = 0.05
    static let floorDecibels: Float = -50

    private let windowFrames = Int(CaptureWriter.sampleRate * LevelMeter.interval)
    private var sumOfSquares: Float = 0
    private var count = 0
    private var smoothed: Float = 0

    mutating func add(_ buffer: AVAudioPCMBuffer) -> [Float] {
        guard let samples = buffer.floatChannelData?[0] else { return [] }
        return add(UnsafeBufferPointer(start: samples, count: Int(buffer.frameLength)))
    }

    mutating func add(_ samples: UnsafeBufferPointer<Float>) -> [Float] {
        var levels: [Float] = []
        for sample in samples {
            sumOfSquares += sample * sample
            count += 1
            guard count == windowFrames else { continue }
            let level = Self.level(rms: (sumOfSquares / Float(count)).squareRoot())
            // Rise fast and fall slowly, like a VU meter, so speech reads as words rather than flicker.
            smoothed += (level - smoothed) * (level > smoothed ? 0.6 : 0.25)
            levels.append(smoothed)
            sumOfSquares = 0
            count = 0
        }
        return levels
    }

    static func level(rms: Float) -> Float {
        guard rms.isFinite, rms > 0 else { return 0 }
        return min(max(1 - 20 * log10(rms) / floorDecibels, 0), 1)
    }
}
