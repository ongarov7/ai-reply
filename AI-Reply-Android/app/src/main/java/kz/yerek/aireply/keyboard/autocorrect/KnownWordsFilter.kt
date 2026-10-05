package kz.yerek.aireply.keyboard.autocorrect

import java.io.IOException
import java.io.InputStream
import java.nio.ByteBuffer
import java.nio.ByteOrder

/**
 * "Is this a real word?" for one language: a Bloom filter of every word form
 * the corpus saw at least twice, read from `<lang>.known`.
 *
 * Сөздің бар-жоғы: «жоқ» деген жауап нақты, «бар» деген жауап сирек қателеседі.
 *
 * A Bloom filter answers "no" exactly and "yes" with a small false-positive
 * rate (0.4 % as generated). That is the safe way round for autocorrect: a
 * false "yes" only leaves one misspelling alone, it never rewrites a word.
 *
 * The file format and the hashing are fixed by
 * `tools/dictionaries/build_dictionaries.py` and shared with the iOS keyboard,
 * so [bitIndices] has to stay bit-for-bit equal to both.
 */
class KnownWordsFilter private constructor(
    /** k: how many bits one word sets. */
    val hashCount: Int,
    /** m: the size of the bit array. */
    val bitCount: Long,
    /** n: how many word forms the generator inserted. */
    val wordCount: Long,
    /** The whole file; the bit array starts at [HEADER_SIZE]. */
    private val data: ByteArray
) {

    /** [key] is a lowercased word. */
    operator fun contains(key: String): Boolean {
        val h1 = fnv1a64(key.toByteArray(Charsets.UTF_8))
        val h2 = splitmix64(h1) or 1uL
        for (i in 0 until hashCount) {
            val bit = bitIndex(h1, h2, i, bitCount)
            val byte = data[HEADER_SIZE + (bit ushr 3).toInt()].toInt()
            if ((byte and (1 shl (bit and 7).toInt())) == 0) return false
        }
        return true
    }

    companion object {
        private const val MAGIC = "AIRB"
        private const val FORMAT_VERSION = 1
        private const val HEADER_SIZE = 20

        private const val FNV_OFFSET = 0xcbf29ce484222325uL
        private const val FNV_PRIME = 0x100000001b3uL

        /**
         * Reads a whole `.known` file. Throws [IOException] when it is not
         * one this reader understands, so a damaged or newer file is refused
         * instead of answering nonsense.
         */
        fun read(input: InputStream): KnownWordsFilter {
            val data = input.readBytes()
            if (data.size < HEADER_SIZE || String(data, 0, MAGIC.length, Charsets.US_ASCII) != MAGIC) {
                throw IOException("known-words filter: not an AIRB file")
            }
            val version = data[4].toInt() and 0xFF
            if (version != FORMAT_VERSION) throw IOException("known-words filter: format $version is not supported")

            val header = ByteBuffer.wrap(data, 0, HEADER_SIZE).order(ByteOrder.LITTLE_ENDIAN)
            val hashCount = data[5].toInt() and 0xFF
            val wordCount = header.getInt(8).toLong() and 0xFFFF_FFFFL
            val bitCount = header.getLong(12)
            if (hashCount == 0 || bitCount <= 0 || bitCount % 8 != 0L || data.size - HEADER_SIZE.toLong() != bitCount / 8) {
                throw IOException("known-words filter: header does not match the file")
            }
            return KnownWordsFilter(hashCount, bitCount, wordCount, data)
        }

        /** The bits [key] sets in a filter of [bitCount] bits with [hashCount] hashes. */
        internal fun bitIndices(key: String, hashCount: Int, bitCount: Long): LongArray {
            val h1 = fnv1a64(key.toByteArray(Charsets.UTF_8))
            val h2 = splitmix64(h1) or 1uL
            return LongArray(hashCount) { bitIndex(h1, h2, it, bitCount) }
        }

        /** Double hashing: (h1 + i·h2) mod 2^64, then mod m. */
        private fun bitIndex(h1: ULong, h2: ULong, i: Int, bitCount: Long): Long =
            ((h1 + i.toULong() * h2) % bitCount.toULong()).toLong()

        /** FNV-1a, 64 bit. */
        internal fun fnv1a64(bytes: ByteArray): ULong {
            var hash = FNV_OFFSET
            for (byte in bytes) {
                hash = (hash xor (byte.toULong() and 0xFFuL)) * FNV_PRIME
            }
            return hash
        }

        /** The splitmix64 finaliser, used to derive the second hash from the first. */
        internal fun splitmix64(value: ULong): ULong {
            var z = value + 0x9e3779b97f4a7c15uL
            z = (z xor (z shr 30)) * 0xbf58476d1ce4e5b9uL
            z = (z xor (z shr 27)) * 0x94d049bb133111ebuL
            return z xor (z shr 31)
        }
    }
}
