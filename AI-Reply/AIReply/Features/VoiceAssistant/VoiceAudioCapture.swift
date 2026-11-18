import AVFoundation
import Foundation

struct VoiceAudioChunk: Sendable {
    let pcm: Data
    let level: Double
}

/// Bounded PCM16/24 kHz capture, shared by streaming and the WAV fallback.
/// No audio is persisted and no playback is used to keep the process alive.
@MainActor
final class VoiceAudioCapture {
    enum Failure: Error { case unavailable, overrun }
    private var engine: AVAudioEngine?
    private var installedTap = false
    private let session = DictationAudioSession()
    private let conversionQueue = DispatchQueue(label: "kz.aireply.voice.convert", qos: .userInitiated)
    private var continuation: AsyncThrowingStream<VoiceAudioChunk, Error>.Continuation?

    func start() throws -> AsyncThrowingStream<VoiceAudioChunk, Error> {
        stop()
        try session.begin()
        let engine = AVAudioEngine()
        self.engine = engine
        let input = engine.inputNode
        let format = input.outputFormat(forBus: 0)
        guard format.sampleRate > 0, format.channelCount > 0,
              let output = AVAudioFormat(commonFormat: .pcmFormatInt16, sampleRate: 24_000,
                                         channels: 1, interleaved: true),
              let converter = AVAudioConverter(from: format, to: output) else {
            stop()
            throw Failure.unavailable
        }
        let pair = AsyncThrowingStream<VoiceAudioChunk, Error>.makeStream(bufferingPolicy: .bufferingOldest(64))
        continuation = pair.continuation
        let queue = conversionQueue
        let sink = pair.continuation
        var tapError: NSError?
        let didInstall = ARVoiceInstallTap(engine, format, { buffer, _ in
            // The engine reuses its memory when the tap returns.
            guard let copy = AVAudioPCMBuffer(pcmFormat: format, frameCapacity: buffer.frameLength) else {
                sink.finish(throwing: Failure.unavailable)
                return
            }
            copy.frameLength = buffer.frameLength
            let source = UnsafeMutableAudioBufferListPointer(buffer.mutableAudioBufferList)
            let target = UnsafeMutableAudioBufferListPointer(copy.mutableAudioBufferList)
            for (from, to) in zip(source, target) {
                // Never copy more than the destination holds, whatever size
                // a route change reports for the source buffer.
                if let src = from.mData, let dst = to.mData {
                    memcpy(dst, src, Int(min(from.mDataByteSize, to.mDataByteSize)))
                }
            }
            queue.async {
                let capacity = AVAudioFrameCount(ceil(Double(copy.frameLength) * 24_000 / format.sampleRate)) + 64
                guard let converted = AVAudioPCMBuffer(pcmFormat: output, frameCapacity: capacity) else { return }
                var supplied = false
                var error: NSError?
                let status = converter.convert(to: converted, error: &error) { _, inputStatus in
                    if supplied { inputStatus.pointee = .noDataNow; return nil }
                    supplied = true
                    inputStatus.pointee = .haveData
                    return copy
                }
                guard status != .error, error == nil, let samples = converted.int16ChannelData?[0] else {
                    sink.finish(throwing: Failure.unavailable)
                    return
                }
                let count = Int(converted.frameLength)
                guard count > 0 else { return }
                var energy = 0.0
                for index in 0..<count { let value = Double(samples[index]) / 32768; energy += value * value }
                let rms = sqrt(energy / Double(count))
                let chunk = VoiceAudioChunk(pcm: Data(bytes: samples, count: count * 2), level: min(1, rms * 12))
                if case .dropped = sink.yield(chunk) { sink.finish(throwing: Failure.overrun) }
            }
        }, &tapError)
        guard didInstall else { stop(); throw tapError ?? Failure.unavailable as NSError }
        installedTap = true
        var startError: NSError?
        guard ARVoiceStartEngine(engine, &startError) else { stop(); throw startError ?? Failure.unavailable as NSError }
        return pair.stream
    }

    func stop() {
        if let engine {
            var stopError: NSError?
            _ = ARVoiceStopEngine(engine, installedTap, &stopError)
        }
        installedTap = false
        engine = nil
        conversionQueue.sync {}
        continuation?.finish()
        continuation = nil
        try? session.end()
    }

    nonisolated static func wav(pcm: Data) -> Data {
        var result = Data()
        func ascii(_ text: String) { result.append(contentsOf: text.utf8) }
        func integer<T: FixedWidthInteger>(_ value: T) {
            var little = value.littleEndian
            withUnsafeBytes(of: &little) { result.append(contentsOf: $0) }
        }
        ascii("RIFF"); integer(UInt32(36 + pcm.count)); ascii("WAVEfmt ")
        integer(UInt32(16)); integer(UInt16(1)); integer(UInt16(1))
        integer(UInt32(24_000)); integer(UInt32(48_000)); integer(UInt16(2)); integer(UInt16(16))
        ascii("data"); integer(UInt32(pcm.count)); result.append(pcm)
        return result
    }
}
