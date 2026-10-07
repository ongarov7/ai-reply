package kz.yerek.aireply.keyboard.autocorrect

import kotlin.math.floor
import kotlin.math.log10

/**
 * Finds the dictionary words nearest to a typed word (DESIGN §7.4).
 *
 * Ең жақын сөздер: қашықтық пен жиілік бірге есептеледі.
 *
 * Only the length buckets a candidate can come from are scanned: L−1 … L+1,
 * and L±2 for words of seven letters or more. Each bucket is in rank order, so
 * once the best few are known, a word's rank alone tells how close it would
 * have to be to still get in; the distance is cut off at that bound, and a
 * bucket is left as soon as no distance could.
 */
internal object CandidateSearch {

    /** What a rarer word costs on top of its distance. */
    fun rankPenalty(rank: Int): Double = 0.15 * log10(rank + 1.0)

    /** The best [limit] candidates [accept] lets through, best first; ties go to the more frequent word. */
    fun nearest(words: WordList, typed: TypedWord, limit: Int, accept: (rank: Int) -> Boolean): List<Candidate> {
        if (limit <= 0 || typed.length == 0) return emptyList()
        val maxDistance = EditCost.maxDistance(typed.length)
        val span = if (typed.length >= 7) 2 else 1
        val best = Best(limit)

        for (length in (typed.length - span).coerceAtLeast(1)..typed.length + span) {
            val bucket = words.bucket(length) ?: continue
            for (slot in 0 until bucket.size) {
                val rank = bucket.ranks[slot]
                val penalty = rankPenalty(rank)
                val bound = best.distanceBound(penalty, maxDistance)
                if (bound < 0) break
                val distance = typed.distanceTo(bucket.chars, slot * length, length, bound)
                if (distance <= bound && accept(rank)) best.offer(rank, distance, distance / 10.0 + penalty)
            }
        }
        return best.candidates(words)
    }

    /**
     * The most frequent word that differs from [key] only by Kazakh letters
     * typed plain (`кайда` → `қайда`), or -1.
     */
    fun kazakhSpelling(words: WordList, key: String): Int {
        val bucket = words.bucket(key.length) ?: return -1
        var bestRank = -1
        var bestScore = Double.MAX_VALUE
        for (slot in 0 until bucket.size) {
            val start = slot * key.length
            var letters = 0
            var index = 0
            while (index < key.length) {
                val candidate = bucket.chars[start + index]
                if (candidate != key[index]) {
                    if (!KazakhLetters.isPlainFor(key[index], candidate)) break
                    letters++
                }
                index++
            }
            if (index < key.length || letters == 0) continue
            val rank = bucket.ranks[slot]
            val score = letters * EditCost.KAZAKH_LETTER / 10.0 + rankPenalty(rank)
            if (score < bestScore) {
                bestScore = score
                bestRank = rank
            }
        }
        return bestRank
    }

    /** The few best candidates so far, kept sorted by score, then rank. */
    private class Best(private val limit: Int) {
        private val ranks = IntArray(limit)
        private val distances = IntArray(limit)
        private val scores = DoubleArray(limit)
        private var count = 0

        /**
         * The largest distance (tenths) a word with this rank penalty may
         * have and still get in; negative when none can.
         */
        fun distanceBound(penalty: Double, maxDistance: Int): Int {
            if (count < limit) return maxDistance
            val room = floor((scores[limit - 1] - penalty) * 10 + 1e-6).toInt()
            return minOf(maxDistance, room)
        }

        fun offer(rank: Int, distance: Int, score: Double) {
            if (count == limit && !isBetter(score, rank, limit - 1)) return
            var position = if (count < limit) count++ else limit - 1
            while (position > 0 && isBetter(score, rank, position - 1)) {
                ranks[position] = ranks[position - 1]
                distances[position] = distances[position - 1]
                scores[position] = scores[position - 1]
                position--
            }
            ranks[position] = rank
            distances[position] = distance
            scores[position] = score
        }

        fun candidates(words: WordList): List<Candidate> =
            List(count) { Candidate(words.display(ranks[it]), ranks[it], distances[it], scores[it]) }

        private fun isBetter(score: Double, rank: Int, than: Int): Boolean =
            score < scores[than] || score == scores[than] && rank < ranks[than]
    }
}
