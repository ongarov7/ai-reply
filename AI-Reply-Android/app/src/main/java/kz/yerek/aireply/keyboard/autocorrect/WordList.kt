package kz.yerek.aireply.keyboard.autocorrect

import java.io.IOException
import java.io.InputStream
import java.util.BitSet

/**
 * One language's correction and completion candidates, read from
 * `<lang>.words`: most frequent first, so a word's position is its rank.
 *
 * Түзету мен толықтыруға арналған сөздер: жиілігі бойынша, жадта ықшам.
 *
 * Memory is what matters here (Russian and Kazakh hold 80 000 words each, and
 * an input method shares the phone with the app it types into), so there is
 * no object per word. The lowercased words of one length sit back to back in
 * one CharArray, in rank order - also the order a candidate scan wants. A
 * rank-to-slot table and a lexicographic index of ranks serve exact and
 * prefix lookups. Immutable once read, so any thread may use it.
 */
class WordList private constructor(
    /** Indexed by word length; null where no word has that length. */
    private val buckets: Array<Bucket?>,
    private val lengthOfRank: ByteArray,
    private val slotOfRank: IntArray,
    /** Every rank, ordered by its lowercased word. */
    private val sortedRanks: IntArray,
    /** Ranks shown with a capital first letter: proper nouns, `I`. */
    private val capitalised: BitSet,
    /** Display forms that differ from the lowercased word in any other way. */
    private val otherDisplayForms: Map<Int, String>
) {

    /** The lowercased words of one length, back to back, and their ranks. */
    internal class Bucket(val length: Int, val chars: CharArray, val ranks: IntArray) {
        val size: Int get() = ranks.size
    }

    val size: Int get() = lengthOfRank.size

    internal fun bucket(length: Int): Bucket? = buckets.getOrNull(length)

    /** The word at [rank] as it is matched: lowercased. */
    fun lowercased(rank: Int): String {
        val length = lengthOfRank[rank].toInt()
        return String(checkNotNull(buckets[length]).chars, slotOfRank[rank] * length, length)
    }

    /** The word at [rank] as it is shown: proper nouns capitalised, `I`. */
    fun display(rank: Int): String {
        otherDisplayForms[rank]?.let { return it }
        val word = lowercased(rank)
        return if (capitalised[rank]) word.replaceFirstChar { it.uppercase() } else word
    }

    /** The rank of [key], a lowercased word, or -1 when the list does not have it. */
    fun rankOf(key: String): Int {
        val index = lowerBound(key)
        if (index == sortedRanks.size) return -1
        val rank = sortedRanks[index]
        return if (compare(rank, key) == 0) rank else -1
    }

    operator fun contains(key: String): Boolean = rankOf(key) >= 0

    /**
     * Up to [limit] ranks of words that start with [prefix] (lowercased) and
     * are longer than it, most frequent first, skipping those [accept]
     * refuses. A binary search finds where the prefix starts; the range is
     * then scanned for its lowest ranks.
     */
    fun completions(prefix: String, limit: Int, accept: (rank: Int) -> Boolean = { true }): List<Int> {
        if (prefix.isEmpty() || limit <= 0) return emptyList()
        val best = IntArray(limit)
        var count = 0
        var index = lowerBound(prefix)
        while (index < sortedRanks.size) {
            val rank = sortedRanks[index++]
            if (!startsWith(rank, prefix)) break
            if (lengthOfRank[rank] <= prefix.length) continue
            if (count == limit && rank > best[count - 1]) continue
            if (!accept(rank)) continue
            // Insertion into the few best, lowest rank first.
            var position = if (count < limit) count++ else limit - 1
            while (position > 0 && best[position - 1] > rank) {
                best[position] = best[position - 1]
                position--
            }
            best[position] = rank
        }
        return best.take(count)
    }

    // -------------------------------------------------------------- ordering

    /** The first index in [sortedRanks] whose word is not before [key]. */
    private fun lowerBound(key: String): Int {
        var low = 0
        var high = sortedRanks.size
        while (low < high) {
            val middle = (low + high) ushr 1
            if (compare(sortedRanks[middle], key) < 0) low = middle + 1 else high = middle
        }
        return low
    }

    /** The word at [rank] against [key], in [String.compareTo] order. */
    private fun compare(rank: Int, key: String): Int {
        val length = lengthOfRank[rank].toInt()
        val chars = checkNotNull(buckets[length]).chars
        val start = slotOfRank[rank] * length
        val common = minOf(length, key.length)
        for (i in 0 until common) {
            val difference = chars[start + i] - key[i]
            if (difference != 0) return difference
        }
        return length - key.length
    }

    private fun startsWith(rank: Int, prefix: String): Boolean {
        val length = lengthOfRank[rank].toInt()
        if (length < prefix.length) return false
        val chars = checkNotNull(buckets[length]).chars
        val start = slotOfRank[rank] * length
        for (i in prefix.indices) {
            if (chars[start + i] != prefix[i]) return false
        }
        return true
    }

    companion object {
        /** The generator stops at 24; anything far longer is not a word list. */
        private const val MAX_WORD_LENGTH = 64

        /**
         * Reads a `.words` file: UTF-8, one word per line, `#` lines are
         * comments. Throws [IOException] on an empty or absurdly long line.
         */
        fun read(input: InputStream): WordList {
            val words = ArrayList<String>(80_000)
            val capitalised = BitSet()
            val otherDisplayForms = HashMap<Int, String>()

            input.bufferedReader(Charsets.UTF_8).lineSequence().forEach { line ->
                if (line.startsWith("#")) return@forEach
                val word = line.lowercase()
                val rank = words.size
                if (word.isEmpty() || word.length > MAX_WORD_LENGTH) {
                    throw IOException("word list: malformed word at rank $rank")
                }
                when (line) {
                    word -> Unit
                    word.replaceFirstChar { it.uppercase() } -> capitalised.set(rank)
                    else -> otherDisplayForms[rank] = line
                }
                words += word
            }
            return build(words, capitalised, otherDisplayForms)
        }

        private fun build(words: List<String>, capitalised: BitSet, otherDisplayForms: Map<Int, String>): WordList {
            val longest = words.maxOfOrNull { it.length } ?: 0
            val counts = IntArray(longest + 1)
            words.forEach { counts[it.length]++ }
            val buckets = Array(longest + 1) { length ->
                if (counts[length] == 0) null else Bucket(length, CharArray(counts[length] * length), IntArray(counts[length]))
            }

            val lengthOfRank = ByteArray(words.size)
            val slotOfRank = IntArray(words.size)
            val filled = IntArray(longest + 1)
            words.forEachIndexed { rank, word ->
                val bucket = checkNotNull(buckets[word.length])
                val slot = filled[word.length]++
                word.toCharArray(bucket.chars, slot * word.length)
                bucket.ranks[slot] = rank
                lengthOfRank[rank] = word.length.toByte()
                slotOfRank[rank] = slot
            }

            val sortedRanks = words.indices.sortedWith(compareBy { words[it] }).toIntArray()
            return WordList(buckets, lengthOfRank, slotOfRank, sortedRanks, capitalised, otherDisplayForms)
        }
    }
}
