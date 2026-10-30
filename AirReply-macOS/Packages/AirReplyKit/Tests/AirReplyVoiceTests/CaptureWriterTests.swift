import AVFoundation
import Foundation
import Testing
@testable import AirReplyVoice

/// The conversion and encoding behind the microphone tap, fed with generated audio.
@Suite("Capture writer")
struct CaptureWriterTests {
    @Test func writesSixteenKilohertzMonoAACFromStereoInput() throws {
        let scratch = try Scratch()
        let levels = LevelLog()
        let writer = try CaptureWriter(fileURL: scratch.file, limit: 40, onLevels: levels.append, onFailure: { Issue.record("write failed") })
        let format = try #require(AVAudioFormat(standardFormatWithSampleRate: 48_000, channels: 2))
        for buffer in try sine(format, seconds: 1) {
            writer.append(buffer)
        }
        #expect(writer.hasAudio)
        let duration = writer.finish()
        // Exact: the converter's held-back tail is flushed into the file on finish.
        #expect(duration == 1)

        let file = try AVAudioFile(forReading: scratch.file)
        #expect(file.fileFormat.sampleRate == 16_000)
        #expect(file.fileFormat.channelCount == 1)
        #expect(file.fileFormat.settings[AVFormatIDKey] as? UInt32 == kAudioFormatMPEG4AAC)
        #expect(file.length == 16_000)

        // One level per 50 ms of converted audio; the resampler lags by up to one 85 ms block.
        // A −9 dBFS tone reads well above the middle.
        #expect((17...20).contains(levels.values.count))
        #expect(levels.values.allSatisfy { (0...1).contains($0) })
        #expect((levels.values.last ?? 0) > 0.75)
    }

    @Test func stopsWritingAtTheLimit() throws {
        let scratch = try Scratch()
        let writer = try CaptureWriter(fileURL: scratch.file, limit: 0.5, onLevels: { _ in }, onFailure: { Issue.record("write failed") })
        let format = try #require(AVAudioFormat(standardFormatWithSampleRate: 44_100, channels: 1))
        for buffer in try sine(format, seconds: 1) {
            writer.append(buffer)
        }
        #expect(writer.finish() == 0.5)
        #expect(try AVAudioFile(forReading: scratch.file).length == 8_000)
    }

    @Test func keepsWritingAcrossAnInputFormatChange() throws {
        let scratch = try Scratch()
        let writer = try CaptureWriter(fileURL: scratch.file, limit: 40, onLevels: { _ in }, onFailure: { Issue.record("write failed") })
        let first = try #require(AVAudioFormat(standardFormatWithSampleRate: 48_000, channels: 2))
        let second = try #require(AVAudioFormat(standardFormatWithSampleRate: 24_000, channels: 1))
        for buffer in try sine(first, seconds: 0.6) + sine(second, seconds: 0.6) {
            writer.append(buffer)
        }
        #expect(abs(writer.finish() - 1.2) < 0.005)
        #expect(abs(try AVAudioFile(forReading: scratch.file).duration - 1.2) < 0.005)
    }

    @Test func finishIsFinalAndRepeatable() throws {
        let scratch = try Scratch()
        let writer = try CaptureWriter(fileURL: scratch.file, limit: 40, onLevels: { _ in }, onFailure: { Issue.record("write failed") })
        let format = try #require(AVAudioFormat(standardFormatWithSampleRate: 48_000, channels: 1))
        let buffers = try sine(format, seconds: 0.6)
        buffers.forEach(writer.append)
        let duration = writer.finish()
        buffers.forEach(writer.append)
        #expect(writer.finish() == duration)
        #expect(try AVAudioFile(forReading: scratch.file).duration == duration)
    }

    @Test func unwritableLocationThrows() {
        let missing = URL(fileURLWithPath: "/nonexistent-\(UUID().uuidString)/voice.m4a")
        #expect(throws: (any Error).self) {
            try CaptureWriter(fileURL: missing, limit: 40, onLevels: { _ in }, onFailure: {})
        }
    }
}

@Suite("Level meter")
struct LevelMeterTests {
    @Test func mapsMinusFiftyToZeroDecibelsOntoZeroToOne() {
        #expect(LevelMeter.level(rms: 0) == 0)
        #expect(LevelMeter.level(rms: 1) == 1)
        #expect(LevelMeter.level(rms: 4) == 1)
        #expect(LevelMeter.level(rms: 0.001) == 0)
        #expect(abs(LevelMeter.level(rms: pow(10, -25.0 / 20)) - 0.5) < 1e-4)
        #expect(LevelMeter.level(rms: .nan) == 0)
        #expect(LevelMeter.level(rms: .infinity) == 0)
    }

    @Test func emitsOneSmoothedLevelPerFiftyMilliseconds() {
        var meter = LevelMeter()
        let loud = [Float](repeating: 0.5, count: 2_000)
        let first = loud.withUnsafeBufferPointer { meter.add($0) }
        #expect(first.count == 2)
        let rest = [Float](repeating: 0.5, count: 400).withUnsafeBufferPointer { meter.add($0) }
        #expect(rest.count == 1)

        // Rises toward the steady level without overshooting, then falls more slowly than it rose.
        let steady = LevelMeter.level(rms: 0.5)
        let rising = first + rest
        #expect(zip(rising, rising.dropFirst()).allSatisfy { $0 < $1 })
        #expect(rising.allSatisfy { $0 <= steady })
        let falling = [Float](repeating: 0, count: 800).withUnsafeBufferPointer { meter.add($0) }
        #expect(falling.count == 1)
        #expect(falling[0] > 0)
        #expect((rising.last ?? 0) - falling[0] < rising[0])
    }

    @Test func silenceStaysAtZero() {
        var meter = LevelMeter()
        let levels = [Float](repeating: 0, count: 8_000).withUnsafeBufferPointer { meter.add($0) }
        #expect(levels == Array(repeating: 0, count: 10))
    }
}

@Suite("Microphone permission")
struct MicrophonePermissionTests {
    @Test func mapsSystemStatus() {
        #expect(SystemMicrophonePermission.status(.authorized) == .authorized)
        #expect(SystemMicrophonePermission.status(.denied) == .denied)
        #expect(SystemMicrophonePermission.status(.restricted) == .restricted)
        #expect(SystemMicrophonePermission.status(.notDetermined) == .notDetermined)
    }
}

/// A temporary directory with one .m4a path in it, removed on deinit.
private final class Scratch {
    let directory: URL
    let file: URL

    init() throws {
        directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("AirReplyVoiceTests-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        file = directory.appendingPathComponent("take.m4a")
    }

    deinit {
        try? FileManager.default.removeItem(at: directory)
    }
}

private final class LevelLog: @unchecked Sendable {
    private let lock = NSLock()
    private var stored: [Float] = []

    var values: [Float] { lock.withLock { stored } }

    func append(_ levels: [Float]) {
        lock.withLock { stored += levels }
    }
}

/// A 440 Hz tone at half scale in every channel, cut into 100 ms buffers like a tap delivers.
private func sine(_ format: AVAudioFormat, seconds: Double) throws -> [AVAudioPCMBuffer] {
    let chunk = AVAudioFrameCount(format.sampleRate / 10)
    let total = Int(format.sampleRate * seconds)
    var buffers: [AVAudioPCMBuffer] = []
    var offset = 0
    while offset < total {
        let frames = min(Int(chunk), total - offset)
        let buffer = try #require(AVAudioPCMBuffer(pcmFormat: format, frameCapacity: chunk))
        buffer.frameLength = AVAudioFrameCount(frames)
        let channels = try #require(buffer.floatChannelData)
        for channel in 0..<Int(format.channelCount) {
            for frame in 0..<frames {
                channels[channel][frame] = 0.5 * sin(2 * .pi * 440 * Float(offset + frame) / Float(format.sampleRate))
            }
        }
        buffers.append(buffer)
        offset += frames
    }
    return buffers
}

private extension AVAudioFile {
    var duration: TimeInterval { Double(length) / fileFormat.sampleRate }
}
