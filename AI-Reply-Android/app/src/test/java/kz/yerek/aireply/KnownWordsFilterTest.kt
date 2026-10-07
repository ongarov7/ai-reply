package kz.yerek.aireply

import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.keyboard.autocorrect.KnownWordsFilter
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.ByteArrayInputStream
import java.io.IOException

/**
 * The Bloom filter reader: the hashing matches the generator (and therefore
 * iOS) bit for bit, the shipped files read, and damaged files are refused.
 *
 * Блум сүзгісі генератормен және iOS-пен бірдей есептеледі.
 */
class KnownWordsFilterTest {

    private fun filter(language: KeyboardLanguage): KnownWordsFilter =
        checkNotNull(AutocorrectTestDictionaries.shipped.filter(language))

    // --------------------------------------------------------------- hashing

    @Test
    fun `hash vectors match the generator`() {
        val privet = KnownWordsFilter.fnv1a64("привет".toByteArray())
        assertEquals(0x1bd8a912173e871fuL, privet)
        assertEquals(0x94b9fd4594fbdd15uL, KnownWordsFilter.splitmix64(privet) or 1uL)
        assertArrayEquals(
            longArrayOf(53506719, 35078196, 5298953, 50870622, 21091379, 55312328, 36883805, 7104562),
            KnownWordsFilter.bitIndices("привет", 8, 64000192)
        )

        val hello = KnownWordsFilter.fnv1a64("hello".toByteArray())
        assertEquals(0xa430d84680aabd0buL, hello)
        assertEquals(0xf3e8eec5eb46e501uL, KnownWordsFilter.splitmix64(hello) or 1uL)
        assertArrayEquals(
            longArrayOf(16701707, 31912268, 47122829, 62333390, 13543759, 28754320, 43964881, 59175442),
            KnownWordsFilter.bitIndices("hello", 8, 64000192)
        )

        val empty = KnownWordsFilter.fnv1a64(ByteArray(0))
        assertEquals(0xcbf29ce484222325uL, empty)
        assertEquals(0xc3817c016ba4ff31uL, KnownWordsFilter.splitmix64(empty) or 1uL)
    }

    // ------------------------------------------------------------ real files

    /**
     * The exact counts change whenever the lists are regenerated, so only what
     * the reader relies on is pinned: eight hashes, and enough bits per word
     * for the generated false-positive rate (about 0.4 %).
     */
    @Test
    fun `the shipped headers are the generated ones`() {
        KeyboardLanguage.entries.forEach { language ->
            with(filter(language)) {
                assertEquals(8, hashCount)
                assertTrue("$language: $wordCount words", wordCount in 100_000L..1_000_000L)
                assertTrue("$language: ${bitCount.toDouble() / wordCount} bits per word", bitCount >= wordCount * 11)
            }
        }
    }

    @Test
    fun `real words are known`() {
        assertTrue("привет" in filter(KeyboardLanguage.RUSSIAN))
        assertTrue("созвониться" in filter(KeyboardLanguage.RUSSIAN))
        assertTrue("қалайсың" in filter(KeyboardLanguage.KAZAKH))
        assertTrue("the plain spelling a Russian layout gives", "калайсын" in filter(KeyboardLanguage.KAZAKH))
        assertTrue("tomorrow" in filter(KeyboardLanguage.ENGLISH))
    }

    @Test
    fun `typos are not known`() {
        KeyboardLanguage.entries.forEach { language ->
            assertFalse("превт" in filter(language))
            assertFalse("zzqx" in filter(language))
        }
    }

    /** DESIGN §10.1: misspellings the corpora count as words are generated out of the filters. */
    @Test
    fun `common misspellings are not known`() {
        listOf("teh", "thier", "recieve", "definately").forEach { assertFalse(it, it in filter(KeyboardLanguage.ENGLISH)) }
        listOf(KeyboardLanguage.RUSSIAN, KeyboardLanguage.KAZAKH).forEach { language ->
            listOf("извени", "сдесь", "здраствуйте").forEach { assertFalse("$language $it", it in filter(language)) }
        }
    }

    // --------------------------------------------------------- damaged files

    private fun header(magic: String = "AIRB", version: Int = 1, k: Int = 8, bits: Long = 64): ByteArray {
        val bytes = ByteArray(20 + (bits / 8).toInt())
        magic.toByteArray().copyInto(bytes)
        bytes[4] = version.toByte()
        bytes[5] = k.toByte()
        for (i in 0 until 8) bytes[12 + i] = (bits ushr (8 * i)).toByte()
        return bytes
    }

    private fun refuses(bytes: ByteArray) {
        try {
            KnownWordsFilter.read(ByteArrayInputStream(bytes))
        } catch (expected: IOException) {
            return
        }
        throw AssertionError("expected the file to be refused")
    }

    @Test
    fun `a well-formed empty filter knows nothing`() {
        val empty = KnownWordsFilter.read(ByteArrayInputStream(header()))
        assertFalse("привет" in empty)
    }

    @Test
    fun `damaged or foreign files are refused`() {
        refuses(ByteArray(10))
        refuses(header(magic = "XXXX"))
        refuses(header(version = 2))
        refuses(header(k = 0))
        refuses(header().copyOf(24))
    }
}
