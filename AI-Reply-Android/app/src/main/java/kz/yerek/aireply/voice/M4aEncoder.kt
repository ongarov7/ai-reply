package kz.yerek.aireply.voice

import android.media.MediaCodec
import android.media.MediaCodecInfo
import android.media.MediaFormat
import android.media.MediaMuxer
import java.io.File
import java.nio.ByteOrder
import kotlin.math.min

/**
 * Mono 16-bit PCM into one M4A file: AAC-LC through the phone's [MediaCodec],
 * in an MPEG-4 container through [MediaMuxer].
 *
 * Small enough to upload quickly over a weak connection - 40 s at 32 kbit/s is
 * about 160 KB - and a format `gpt-transcribe` reads directly.
 *
 * Blocking and slow by phone standards (a few hundred milliseconds for 40 s):
 * the recorder calls it from a background dispatcher, never the main thread.
 * Any failure throws, and the caller falls back to WAV.
 */
internal object M4aEncoder {

    private const val MIME = MediaFormat.MIMETYPE_AUDIO_AAC

    /** Bytes per input buffer: 4096 samples, four AAC frames. */
    private const val MAX_INPUT_BYTES = 8_192

    private const val TIMEOUT_US = 10_000L

    /** An encoder that produces nothing for this many polls after the end of input has stalled: ~3 s. */
    private const val MAX_IDLE_POLLS = 300

    /** 32 kbit/s is plenty for speech at 16 kHz; higher capture rates get more. */
    fun bitRateFor(sampleRate: Int): Int = if (sampleRate <= 16_000) 32_000 else 64_000

    /**
     * Writes the first [count] of [samples] to [file]. [checkpoint] runs on
     * every pass of the loop; throwing from it (a cancelled coroutine) stops
     * the encoding there, with the codec and the muxer released. The partial
     * file is the caller's to delete.
     */
    fun encode(samples: ShortArray, count: Int, sampleRate: Int, file: File, checkpoint: () -> Unit) {
        require(count in 1..samples.size) { "nothing to encode" }
        val format = MediaFormat.createAudioFormat(MIME, sampleRate, 1).apply {
            setInteger(MediaFormat.KEY_AAC_PROFILE, MediaCodecInfo.CodecProfileLevel.AACObjectLC)
            setInteger(MediaFormat.KEY_BIT_RATE, bitRateFor(sampleRate))
            setInteger(MediaFormat.KEY_MAX_INPUT_SIZE, MAX_INPUT_BYTES)
        }

        var codec: MediaCodec? = null
        var muxer: MediaMuxer? = null
        var muxerStarted = false
        try {
            codec = MediaCodec.createEncoderByType(MIME)
            codec.configure(format, null, null, MediaCodec.CONFIGURE_FLAG_ENCODE)
            muxer = MediaMuxer(file.path, MediaMuxer.OutputFormat.MUXER_OUTPUT_MPEG_4)
            codec.start()

            val info = MediaCodec.BufferInfo()
            var fed = 0
            var inputDone = false
            var track = -1
            var lastPtsUs = 0L
            var idle = 0

            while (true) {
                checkpoint()

                if (!inputDone) {
                    val index = codec.dequeueInputBuffer(TIMEOUT_US)
                    if (index >= 0) {
                        val buffer = checkNotNull(codec.getInputBuffer(index)) { "no input buffer" }
                        buffer.clear()
                        // Timestamps come from the samples fed, never a clock:
                        // the file is exactly as long as the audio held.
                        val ptsUs = fed * 1_000_000L / sampleRate
                        val n = min(count - fed, buffer.remaining() / 2)
                        if (n <= 0) {
                            codec.queueInputBuffer(index, 0, 0, ptsUs, MediaCodec.BUFFER_FLAG_END_OF_STREAM)
                            inputDone = true
                        } else {
                            // MediaCodec takes PCM in the platform's byte order.
                            buffer.order(ByteOrder.nativeOrder()).asShortBuffer().put(samples, fed, n)
                            codec.queueInputBuffer(index, 0, n * 2, ptsUs, 0)
                            fed += n
                        }
                    }
                }

                val out = codec.dequeueOutputBuffer(info, if (inputDone) TIMEOUT_US else 0L)
                when {
                    out == MediaCodec.INFO_TRY_AGAIN_LATER -> {
                        if (inputDone && ++idle > MAX_IDLE_POLLS) error("encoder stalled")
                    }

                    out == MediaCodec.INFO_OUTPUT_FORMAT_CHANGED -> {
                        idle = 0
                        check(track < 0) { "output format changed twice" }
                        track = muxer.addTrack(codec.outputFormat)
                        muxer.start()
                        muxerStarted = true
                    }

                    out >= 0 -> {
                        idle = 0
                        val buffer = checkNotNull(codec.getOutputBuffer(out)) { "no output buffer" }
                        // The codec config (the AAC header) reaches the muxer
                        // through the output format, not as a sample.
                        val isConfig = info.flags and MediaCodec.BUFFER_FLAG_CODEC_CONFIG != 0
                        if (!isConfig && info.size > 0) {
                            check(muxerStarted) { "audio before the output format" }
                            // The MPEG-4 writer refuses audio that goes back in time.
                            if (info.presentationTimeUs < lastPtsUs) {
                                info.set(info.offset, info.size, lastPtsUs, info.flags)
                            }
                            lastPtsUs = info.presentationTimeUs
                            buffer.position(info.offset)
                            buffer.limit(info.offset + info.size)
                            muxer.writeSampleData(track, buffer, info)
                        }
                        codec.releaseOutputBuffer(out, false)
                        if (info.flags and MediaCodec.BUFFER_FLAG_END_OF_STREAM != 0) break
                    }

                    // INFO_OUTPUT_BUFFERS_CHANGED: getOutputBuffer(index) already
                    // returns the current buffers, nothing to do.
                    else -> Unit
                }
            }

            check(muxerStarted) { "the encoder produced no audio" }
            // Writes the index the file cannot be played without; a failure
            // here is a failure of the file.
            muxer.stop()
            muxerStarted = false
        } finally {
            codec?.let { runCatching { it.stop() }; runCatching { it.release() } }
            muxer?.let {
                if (muxerStarted) runCatching { it.stop() }
                runCatching { it.release() }
            }
        }
    }
}
