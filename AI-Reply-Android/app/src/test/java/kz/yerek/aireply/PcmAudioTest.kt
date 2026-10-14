package kz.yerek.aireply

import kz.yerek.aireply.voice.LevelMeter
import kz.yerek.aireply.voice.PcmAudio
import kz.yerek.aireply.voice.PcmBuffer
import kz.yerek.aireply.voice.WavFile
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.ByteArrayOutputStream
import java.nio.ByteBuffer
import java.nio.ByteOrder
import kotlin.math.pow

/**
 * The recorder's arithmetic: the limit counted in samples, pauses that keep
 * one stretch of audio, the meter, and the WAV fallback file.
 */
class PcmAudioTest {

    // ---------------------------------------------------------------- clock

    @Test
    fun `forty seconds at 16 kHz is exactly 640 000 samples, and back`() {
        assertEquals(640_000L, PcmAudio.msToSamples(40_000, 16_000))
        assertEquals(40_000L, PcmAudio.samplesToMs(640_000, 16_000))
        assertEquals(1_764_000L, PcmAudio.msToSamples(40_000, 44_100))
        assertEquals(1_920_000L, PcmAudio.msToSamples(40_000, 48_000))
    }

    @Test
    fun `time rounds down, so a partial millisecond is never counted`() {
        assertEquals(0L, PcmAudio.samplesToMs(15, 16_000))
        assertEquals(1L, PcmAudio.samplesToMs(16, 16_000))
        assertEquals(299L, PcmAudio.samplesToMs(4_799, 16_000))
        assertEquals(300L, PcmAudio.samplesToMs(4_800, 16_000))
        assertEquals(0L, PcmAudio.samplesToMs(1_000, 0))
    }

    @Test
    fun `a read is 40 ms of audio`() {
        assertEquals(640, PcmAudio.chunkSamples(16_000))
        assertEquals(1_764, PcmAudio.chunkSamples(44_100))
        assertEquals(1_920, PcmAudio.chunkSamples(48_000))
        assertEquals(1, PcmAudio.chunkSamples(1))
    }

    // ---------------------------------------------------------------- the limit

    @Test
    fun `the limit is exact - a thousand 40 ms reads fill it and nothing more fits`() {
        val buffer = PcmBuffer(PcmAudio.msToSamples(40_000, 16_000).toInt())
        val chunk = ShortArray(PcmAudio.chunkSamples(16_000)) { 1 }
        repeat(1_000) {
            assertFalse(buffer.isFull)
            assertEquals(640, buffer.append(chunk))
        }
        assertTrue(buffer.isFull)
        assertEquals(640_000, buffer.size)
        assertEquals(40_000L, PcmAudio.samplesToMs(buffer.size.toLong(), 16_000))
        assertEquals(0, buffer.append(chunk))
        assertEquals(640_000, buffer.size)
    }

    @Test
    fun `the read that crosses the limit is cut at it`() {
        val buffer = PcmBuffer(640_000)
        assertEquals(639_800, buffer.append(ShortArray(639_800)))
        val chunk = ShortArray(640) { it.toShort() }
        assertEquals(200, buffer.append(chunk))
        assertTrue(buffer.isFull)
        val held = buffer.toShortArray()
        assertEquals(640_000, held.size)
        // The first 200 samples of the chunk are kept, the rest is not.
        assertEquals(0.toShort(), held[639_800])
        assertEquals(199.toShort(), held[639_999])
    }

    @Test
    fun `pause and resume append to the same audio, in order`() {
        val buffer = PcmBuffer(10)
        // First run, then a pause (nothing appended), then the resumed run.
        assertEquals(3, buffer.append(shortArrayOf(1, 2, 3)))
        assertEquals(4, buffer.append(shortArrayOf(9, 4, 5, 6, 7, 9), offset = 1, count = 4))
        assertEquals(7, buffer.size)
        assertArrayEquals(shortArrayOf(1, 2, 3, 4, 5, 6, 7), buffer.toShortArray())
        // The resumed run reaches the limit: one stretch of 10 samples.
        assertEquals(3, buffer.append(shortArrayOf(8, 9, 10, 11)))
        assertArrayEquals(shortArrayOf(1, 2, 3, 4, 5, 6, 7, 8, 9, 10), buffer.toShortArray())
    }

    @Test
    fun `the buffer grows past its first allocation and clears to nothing`() {
        val buffer = PcmBuffer(capacity = 100, initialCapacity = 4)
        repeat(10) { i -> buffer.append(ShortArray(10) { (i * 10 + it).toShort() }) }
        assertEquals(100, buffer.size)
        assertArrayEquals(ShortArray(100) { it.toShort() }, buffer.toShortArray())
        buffer.clear()
        assertEquals(0, buffer.size)
        assertFalse(buffer.isFull)
        assertEquals(0, buffer.toShortArray().size)
    }

    @Test(expected = IllegalArgumentException::class)
    fun `a chunk range outside the array is refused`() {
        PcmBuffer(10).append(ShortArray(4), offset = 2, count = 3)
    }

    // ---------------------------------------------------------------- meter

    @Test
    fun `the meter runs from -50 dBFS (0) to -5 dBFS (1), clamped`() {
        assertEquals(0f, PcmAudio.levelForDbfs(-50.0), 0f)
        assertEquals(1f, PcmAudio.levelForDbfs(-5.0), 0f)
        assertEquals(0.5f, PcmAudio.levelForDbfs(-27.5), 1e-6f)
        assertEquals(0f, PcmAudio.levelForDbfs(-80.0), 0f)
        assertEquals(1f, PcmAudio.levelForDbfs(0.0), 0f)
        assertEquals(0f, PcmAudio.levelForDbfs(Double.NEGATIVE_INFINITY), 0f)
        assertEquals(0f, PcmAudio.levelForDbfs(Double.NaN), 0f)
    }

    @Test
    fun `the level is measured from the samples`() {
        // Digital silence shows nothing.
        assertEquals(0f, PcmAudio.level(ShortArray(640)), 0f)
        assertEquals(0f, PcmAudio.level(ShortArray(0)), 0f)
        // Full scale fills the meter.
        assertEquals(1f, PcmAudio.level(ShortArray(640) { Short.MAX_VALUE }), 0f)
        // A steady -27.5 dBFS sits in the middle.
        val amplitude = (PcmAudio.FULL_SCALE * 10.0.pow(-27.5 / 20)).toInt().toShort()
        assertEquals(0.5f, PcmAudio.level(ShortArray(640) { amplitude }), 0.01f)
        // Only the samples asked about count.
        val mixed = ShortArray(640) { if (it < 320) 0 else Short.MAX_VALUE }
        assertEquals(0f, PcmAudio.level(mixed, 0, 320), 0f)
        assertEquals(1f, PcmAudio.level(mixed, 320, 320), 0f)
    }

    @Test
    fun `rms and dbfs`() {
        assertEquals(1_000.0, PcmAudio.rms(shortArrayOf(1_000, -1_000, 1_000, -1_000)), 1e-9)
        assertEquals(0.0, PcmAudio.dbfs(PcmAudio.FULL_SCALE), 1e-9)
        assertEquals(-20.0, PcmAudio.dbfs(PcmAudio.FULL_SCALE / 10), 1e-9)
        assertEquals(Double.NEGATIVE_INFINITY, PcmAudio.dbfs(0.0), 0.0)
    }

    @Test
    fun `the needle rises fast and falls slowly`() {
        val meter = LevelMeter()
        val up = meter.update(1f)
        assertEquals(LevelMeter.ATTACK, up, 1e-6f)
        repeat(20) { meter.update(1f) }
        assertEquals(1f, meter.level, 1e-3f)
        val down = meter.update(0f)
        assertEquals(1f - LevelMeter.RELEASE, down, 1e-6f)
        // One step up covers more ground than one step down.
        assertTrue(up > 1f - down)
        meter.reset()
        assertEquals(0f, meter.level, 0f)
        // Never outside 0..1, whatever it is fed.
        assertEquals(LevelMeter.ATTACK, meter.update(5f), 1e-6f)
        meter.reset()
        assertEquals(0f, meter.update(-3f), 0f)
    }

    // ---------------------------------------------------------------- wav

    @Test
    fun `the wav header describes mono 16-bit pcm`() {
        val header = ByteBuffer.wrap(WavFile.header(sampleRate = 16_000, sampleCount = 640_000))
            .order(ByteOrder.LITTLE_ENDIAN)
        assertEquals(44, header.capacity())
        assertEquals("RIFF", ascii(header, 0))
        assertEquals(36 + 1_280_000, header.getInt(4)) // RIFF chunk size: file length - 8
        assertEquals("WAVE", ascii(header, 8))
        assertEquals("fmt ", ascii(header, 12))
        assertEquals(16, header.getInt(16)) // fmt chunk size
        assertEquals(1.toShort(), header.getShort(20)) // PCM
        assertEquals(1.toShort(), header.getShort(22)) // mono
        assertEquals(16_000, header.getInt(24)) // sample rate
        assertEquals(32_000, header.getInt(28)) // byte rate
        assertEquals(2.toShort(), header.getShort(32)) // block align
        assertEquals(16.toShort(), header.getShort(34)) // bits per sample
        assertEquals("data", ascii(header, 36))
        assertEquals(1_280_000, header.getInt(40)) // data size
    }

    @Test
    fun `the wav header follows the capture rate`() {
        val header = ByteBuffer.wrap(WavFile.header(sampleRate = 48_000, sampleCount = 0))
            .order(ByteOrder.LITTLE_ENDIAN)
        assertEquals(36, header.getInt(4))
        assertEquals(48_000, header.getInt(24))
        assertEquals(96_000, header.getInt(28))
        assertEquals(0, header.getInt(40))
    }

    @Test
    fun `the wav file is the header and the samples, little-endian`() {
        val samples = shortArrayOf(0x1234, -1, Short.MIN_VALUE, Short.MAX_VALUE, 99)
        val out = ByteArrayOutputStream()
        WavFile.write(out, samples, count = 4, sampleRate = 16_000)
        val bytes = out.toByteArray()
        assertEquals(WavFile.HEADER_BYTES + 8, bytes.size)
        assertArrayEquals(WavFile.header(16_000, 4), bytes.copyOfRange(0, 44))
        assertArrayEquals(
            byteArrayOf(0x34, 0x12, -1, -1, 0x00, -128, -1, 0x7F),
            bytes.copyOfRange(44, bytes.size)
        )
    }

    @Test
    fun `a long wav is written in blocks, each one a chance to stop`() {
        val samples = ShortArray(40_000) { (it % 1_000).toShort() }
        var checkpoints = 0
        val out = ByteArrayOutputStream()
        WavFile.write(out, samples, samples.size, 16_000) { checkpoints++ }
        val bytes = out.toByteArray()
        assertEquals(44 + 80_000, bytes.size)
        assertTrue(checkpoints >= 2)
        val body = ByteBuffer.wrap(bytes, 44, 80_000).order(ByteOrder.LITTLE_ENDIAN).asShortBuffer()
        val read = ShortArray(40_000).also { body.get(it) }
        assertArrayEquals(samples, read)
    }

    @Test
    fun `a cancelled wav write stops at the next block`() {
        val out = ByteArrayOutputStream()
        var calls = 0
        val stopped = runCatching {
            WavFile.write(out, ShortArray(100_000), 100_000, 16_000) {
                if (++calls == 2) throw IllegalStateException("cancelled")
            }
        }
        assertTrue(stopped.isFailure)
        assertTrue(out.size() < 44 + 200_000)
    }

    private fun ascii(buffer: ByteBuffer, at: Int) =
        String(ByteArray(4) { buffer.get(at + it) }, Charsets.US_ASCII)
}
