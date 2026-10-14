package kz.yerek.aireply.voice

import java.io.OutputStream
import java.nio.ByteBuffer
import java.nio.ByteOrder
import kotlin.math.log10
import kotlin.math.max
import kotlin.math.min
import kotlin.math.sqrt

/**
 * Microphone audio as numbers: the clock, the meter and the chunk size.
 *
 * Дыбыс - сандар: уақыт үлгілерден саналады, таймерден емес.
 *
 * Pure on purpose: no Android, so the JVM tests pin every rule the recorder
 * depends on - where the 40 s limit falls, what "loud" means on the meter.
 * Mono, 16-bit PCM throughout; that is the only format the recorder captures.
 */
object PcmAudio {

    /** The loudest a 16-bit sample can be: 0 dBFS. */
    const val FULL_SCALE = 32_768.0

    /** At or below this the meter shows nothing: room noise, not a voice. */
    const val SILENT_DBFS = -50.0

    /** At or above this the meter is full: speaking close to the phone. */
    const val LOUD_DBFS = -5.0

    /** How much audio the reader takes from the microphone at a time. */
    const val CHUNK_MS = 40L

    /** Samples in [ms] of audio, rounded down: the limit is never rounded up past. */
    fun msToSamples(ms: Long, sampleRate: Int): Long = ms * sampleRate / 1_000

    /** Milliseconds of audio in [samples], rounded down. */
    fun samplesToMs(samples: Long, sampleRate: Int): Long =
        if (sampleRate <= 0) 0 else samples * 1_000 / sampleRate

    /** One read's worth of samples: [CHUNK_MS] of audio, never zero. */
    fun chunkSamples(sampleRate: Int): Int = msToSamples(CHUNK_MS, sampleRate).toInt().coerceAtLeast(1)

    /** Root mean square of [count] samples from [offset]; 0 for none. */
    fun rms(samples: ShortArray, offset: Int = 0, count: Int = samples.size - offset): Double {
        if (count <= 0) return 0.0
        var sum = 0.0
        for (i in offset until offset + count) {
            val s = samples[i].toDouble()
            sum += s * s
        }
        return sqrt(sum / count)
    }

    /** Loudness relative to full scale; minus infinity for digital silence. */
    fun dbfs(rms: Double): Double =
        if (rms <= 0.0) Double.NEGATIVE_INFINITY else 20 * log10(rms / FULL_SCALE)

    /**
     * The meter position for a loudness: [SILENT_DBFS] is 0, [LOUD_DBFS] is 1,
     * a straight line between, clamped outside.
     */
    fun levelForDbfs(dbfs: Double): Float {
        if (dbfs.isNaN()) return 0f
        return ((dbfs - SILENT_DBFS) / (LOUD_DBFS - SILENT_DBFS)).coerceIn(0.0, 1.0).toFloat()
    }

    /** The meter position for a chunk of audio, unsmoothed. */
    fun level(samples: ShortArray, offset: Int = 0, count: Int = samples.size - offset): Float =
        levelForDbfs(dbfs(rms(samples, offset, count)))
}

/**
 * Everything captured, in order, never more than [capacity] samples - the
 * limit, in audio.
 *
 * Paused and resumed runs append to the same buffer, so a recording with
 * pauses is still one stretch of audio and ends up in one file. It grows as
 * audio arrives instead of reserving the whole limit up front.
 *
 * Not thread-safe: the recorder reads and writes it under its own lock.
 */
class PcmBuffer(val capacity: Int, initialCapacity: Int = DEFAULT_INITIAL_CAPACITY) {

    init {
        require(capacity >= 0) { "capacity must not be negative" }
    }

    private val initialCapacity = initialCapacity.coerceIn(0, capacity)
    private var data = ShortArray(this.initialCapacity)

    /** Samples held. */
    var size: Int = 0
        private set

    val isFull: Boolean
        get() = size >= capacity

    /**
     * Keeps as much of [count] samples from [offset] as fits under [capacity]
     * and returns how many that was: the chunk that crosses the limit is
     * truncated there, and once full nothing more is taken.
     */
    fun append(chunk: ShortArray, offset: Int = 0, count: Int = chunk.size - offset): Int {
        require(offset >= 0 && count >= 0 && offset + count <= chunk.size) { "chunk range out of bounds" }
        val accepted = min(count, capacity - size)
        if (accepted <= 0) return 0
        ensureCapacity(size + accepted)
        chunk.copyInto(data, destinationOffset = size, startIndex = offset, endIndex = offset + accepted)
        size += accepted
        return accepted
    }

    /** A copy of what is held, exactly [size] samples long. */
    fun toShortArray(): ShortArray = data.copyOf(size)

    /** Forgets the audio and gives the memory back. */
    fun clear() {
        size = 0
        data = ShortArray(initialCapacity)
    }

    private fun ensureCapacity(needed: Int) {
        if (needed <= data.size) return
        val grown = max(needed, if (data.isEmpty()) DEFAULT_INITIAL_CAPACITY else data.size * 2)
        data = data.copyOf(min(grown, capacity))
    }

    private companion object {
        /** About a second at 16 kHz. */
        const val DEFAULT_INITIAL_CAPACITY = 16_384
    }
}

/**
 * The meter's needle: rises quickly when the user speaks, falls back more
 * slowly, so it follows a voice instead of flickering with every syllable.
 * It only smooths measured levels - it never moves on its own.
 *
 * Owned by one reader thread; not thread-safe.
 */
class LevelMeter(
    private val attack: Float = ATTACK,
    private val release: Float = RELEASE
) {

    var level: Float = 0f
        private set

    /** Moves towards [measured] (0 to 1) and returns the new position. */
    fun update(measured: Float): Float {
        val target = measured.coerceIn(0f, 1f)
        val step = if (target > level) attack else release
        level = (level + (target - level) * step).coerceIn(0f, 1f)
        return level
    }

    fun reset() {
        level = 0f
    }

    companion object {
        const val ATTACK = 0.6f
        const val RELEASE = 0.2f
    }
}

/**
 * The fallback file: mono 16-bit PCM in a plain 44-byte RIFF/WAVE header,
 * written only when no AAC encoder on the phone works. Bigger than M4A, but
 * every transcription service reads it.
 */
object WavFile {

    const val HEADER_BYTES = 44
    const val CHANNELS = 1
    const val BITS_PER_SAMPLE = 16
    const val BLOCK_ALIGN = CHANNELS * BITS_PER_SAMPLE / 8

    /** Samples converted per write, so a cancelled write stops between blocks. */
    private const val BLOCK_SAMPLES = 16_384

    /** The header for [sampleCount] mono 16-bit samples at [sampleRate]. */
    fun header(sampleRate: Int, sampleCount: Int): ByteArray {
        require(sampleRate > 0 && sampleCount >= 0) { "bad wav format" }
        val dataBytes = sampleCount.toLong() * BLOCK_ALIGN
        require(dataBytes + HEADER_BYTES - 8 <= UINT32_MAX) { "too much audio for a wav file" }
        return ByteBuffer.allocate(HEADER_BYTES).order(ByteOrder.LITTLE_ENDIAN).apply {
            put(ascii("RIFF"))
            putInt((dataBytes + HEADER_BYTES - 8).toInt())
            put(ascii("WAVE"))
            put(ascii("fmt "))
            putInt(16) // size of the fmt chunk that follows
            putShort(1) // PCM
            putShort(CHANNELS.toShort())
            putInt(sampleRate)
            putInt(sampleRate * BLOCK_ALIGN) // byte rate
            putShort(BLOCK_ALIGN.toShort())
            putShort(BITS_PER_SAMPLE.toShort())
            put(ascii("data"))
            putInt(dataBytes.toInt())
        }.array()
    }

    /**
     * Writes the header and the first [count] of [samples], little-endian.
     * [checkpoint] runs before every block; throwing from it (a cancelled
     * coroutine) stops the write there.
     */
    fun write(
        out: OutputStream,
        samples: ShortArray,
        count: Int,
        sampleRate: Int,
        checkpoint: () -> Unit = {}
    ) {
        require(count in 0..samples.size) { "count out of bounds" }
        out.write(header(sampleRate, count))
        val block = ByteArray(min(count, BLOCK_SAMPLES) * BLOCK_ALIGN)
        var written = 0
        while (written < count) {
            checkpoint()
            val n = min(BLOCK_SAMPLES, count - written)
            for (i in 0 until n) {
                val s = samples[written + i].toInt()
                block[2 * i] = s.toByte()
                block[2 * i + 1] = (s shr 8).toByte()
            }
            out.write(block, 0, n * BLOCK_ALIGN)
            written += n
        }
        out.flush()
    }

    private fun ascii(text: String) = text.toByteArray(Charsets.US_ASCII)

    private const val UINT32_MAX = 0xFFFF_FFFFL
}
