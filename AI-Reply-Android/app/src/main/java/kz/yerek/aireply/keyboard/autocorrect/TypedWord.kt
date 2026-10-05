package kz.yerek.aireply.keyboard.autocorrect

/**
 * What it costs to turn the typed word into a dictionary word (DESIGN §7.4).
 *
 * Қате түрлерінің «бағасы»: көрші перне, қос әріп, қазақ әрпі — арзанырақ.
 *
 * Costs are in tenths so that sums compare exactly: "1.0 + 0.3 is within 1.3"
 * must never depend on floating-point rounding, here or on iOS.
 */
internal object EditCost {
    const val INSERTION = 10
    const val DELETION = 10

    /** Deleting a letter typed twice: "сеггодня". */
    const val DOUBLED_DELETION = 6
    const val TRANSPOSITION = 7
    const val SUBSTITUTION = 10

    /** A key next to the intended one. */
    const val ADJACENT_SUBSTITUTION = 6

    /** The plain letter typed for a Kazakh one: `к` for `қ`. */
    const val KAZAKH_LETTER = 3

    /** `е` for `ё`, or the other way round. */
    const val YO = 1

    /** A Kazakh letter the user typed never becomes its plain letter. */
    const val FORBIDDEN = 1_000

    fun substitution(typed: Char, candidate: Char, proximity: KeyProximity): Int = when {
        typed == candidate -> 0
        KazakhLetters.isPlainFor(typed, candidate) -> KAZAKH_LETTER
        KazakhLetters.isPlainFor(candidate, typed) -> FORBIDDEN
        typed == 'е' && candidate == 'ё' || typed == 'ё' && candidate == 'е' -> YO
        proximity.areAdjacent(typed, candidate) -> ADJACENT_SUBSTITUTION
        else -> SUBSTITUTION
    }

    /**
     * How far a candidate may be from a typed word of [length] letters to be
     * offered at all (DESIGN §10.2): wide enough for a missing letter plus a
     * swap in a five-letter word ("превт" → "привет"). Applying a correction
     * by itself takes a much closer match (see [AutocorrectEngine]).
     */
    fun maxDistance(length: Int): Int = when {
        length <= 3 -> 10
        length <= 5 -> 17
        else -> 20
    }
}

/**
 * Kazakh letters and the plain letter people type for each when the Kazakh key
 * is out of reach (a Russian layout, or simply haste).
 */
internal object KazakhLetters {

    /** Plain → Kazakh. */
    private val PAIRS: List<Pair<Char, Char>> = listOf(
        'а' to 'ә', 'г' to 'ғ', 'к' to 'қ', 'н' to 'ң', 'о' to 'ө', 'у' to 'ұ',
        'у' to 'ү', 'ұ' to 'ү', 'х' to 'һ', 'и' to 'і', 'ы' to 'і'
    )

    /** The letters only Kazakh writes (lowercase). */
    private const val KAZAKH_ONLY = "әғқңөұүһі"

    fun isPlainFor(plain: Char, kazakh: Char): Boolean =
        PAIRS.any { it.first == plain && it.second == kazakh }

    /** True when the lowercased [word] has a letter Russian does not write. */
    fun hasKazakhLetter(word: String): Boolean = word.any { it in KAZAKH_ONLY }

    /**
     * True when [candidate] would turn a Kazakh letter of [typed] into its
     * plain letter by any route: the forbidden substitution, or deleting `қ`
     * and inserting `к`, which the distance alone would allow in long words.
     */
    fun losesKazakhLetter(typed: String, candidate: String): Boolean = PAIRS.any { (plain, kazakh) ->
        typed.occurrences(kazakh) > candidate.occurrences(kazakh) &&
            candidate.occurrences(plain) > typed.occurrences(plain)
    }

    private fun String.occurrences(letter: Char): Int = count { it == letter }
}

/**
 * The typed word, lowercased and prepared for thousands of distance
 * computations: the weighted optimal string alignment distance of §7.4
 * (Damerau-Levenshtein where a transposed pair is not edited again).
 *
 * Substitution costs against every letter of the word's alphabet are worked
 * out once, so the inner loop is array reads, and a cheap bound on the
 * letters alone turns most dictionary words away before the table is filled.
 * Owns scratch arrays, so one instance belongs to one lookup on one thread.
 */
internal class TypedWord(val text: String, private val proximity: KeyProximity) {

    val length: Int get() = text.length

    /** The 256-character block of the word's script: Basic Latin or Cyrillic. */
    private val block: Int = if (text.any { it.code in CYRILLIC until CYRILLIC + BLOCK_SIZE }) CYRILLIC else 0

    private val substitutions = IntArray(text.length * BLOCK_SIZE) { index ->
        EditCost.substitution(text[index / BLOCK_SIZE], (block + index % BLOCK_SIZE).toChar(), proximity)
    }

    private val deletions = IntArray(text.length) { i ->
        if (i > 0 && text[i] == text[i - 1]) EditCost.DOUBLED_DELETION else EditCost.DELETION
    }

    // The letters bound. A typed letter the candidate lacks must be
    // substituted or deleted, and a candidate letter the typed word lacks must
    // be substituted in or inserted - each at no less than its cheapest way.

    /** How many of each letter of the block the typed word has. */
    private val letterCounts = IntArray(BLOCK_SIZE)

    /** [letterCounts], used up while one candidate is checked and then restored. */
    private val unmatched = IntArray(BLOCK_SIZE)

    /** The typed word's letters, each once, as block offsets. */
    private val typedLetters: IntArray

    /** The cheapest way to get rid of a typed letter: substitute or delete it. */
    private val cheapestRemoval = IntArray(BLOCK_SIZE)

    /** The cheapest way to get a candidate letter: substitute a typed letter into it, or insert it. */
    private val cheapestArrival = IntArray(BLOCK_SIZE)

    init {
        text.forEach { letter ->
            val index = letter.code - block
            if (index in 0 until BLOCK_SIZE) letterCounts[index]++
        }
        letterCounts.copyInto(unmatched)
        typedLetters = (0 until BLOCK_SIZE).filter { letterCounts[it] > 0 }.toIntArray()
        for (index in 0 until BLOCK_SIZE) {
            var removal = EditCost.DOUBLED_DELETION
            var arrival = EditCost.INSERTION
            for (i in text.indices) {
                val cost = substitutions[i * BLOCK_SIZE + index]
                if (cost == 0) continue
                arrival = minOf(arrival, cost)
            }
            if (letterCounts[index] > 0) {
                val row = text.indexOf((block + index).toChar()) * BLOCK_SIZE
                for (other in 0 until BLOCK_SIZE) {
                    if (other != index) removal = minOf(removal, substitutions[row + other])
                }
            }
            cheapestRemoval[index] = removal
            cheapestArrival[index] = arrival
        }
    }

    private var twoRowsBack = IntArray(0)
    private var previousRow = IntArray(0)
    private var currentRow = IntArray(0)

    /**
     * The distance to the [length] characters of [chars] from [offset], in
     * tenths. Gives up as soon as the result must exceed [limit], and then
     * returns something above it.
     */
    fun distanceTo(chars: CharArray, offset: Int, length: Int, limit: Int): Int {
        if (lettersBound(chars, offset, length) > limit) return limit + 1
        ensureRows(length + 1)
        var before = twoRowsBack
        var previous = previousRow
        var current = currentRow
        var previousMinimum = Int.MAX_VALUE
        for (j in 0..length) {
            previous[j] = j * EditCost.INSERTION
            previousMinimum = minOf(previousMinimum, previous[j] + leftover(0, j, length))
        }

        for (i in 1..text.length) {
            val typed = text[i - 1]
            val deletion = deletions[i - 1]
            current[0] = previous[0] + deletion
            var minimum = current[0] + leftover(i, 0, length)
            for (j in 1..length) {
                val candidate = chars[offset + j - 1]
                var best = previous[j] + deletion
                val insertion = current[j - 1] + EditCost.INSERTION
                if (insertion < best) best = insertion
                val substitution = previous[j - 1] + substitution(i - 1, candidate)
                if (substitution < best) best = substitution
                if (i > 1 && j > 1 && typed != candidate &&
                    typed == chars[offset + j - 2] && text[i - 2] == candidate
                ) {
                    val transposition = before[j - 2] + EditCost.TRANSPOSITION
                    if (transposition < best) best = transposition
                }
                current[j] = best
                val bound = best + leftover(i, j, length)
                if (bound < minimum) minimum = bound
            }
            // Every later cell builds on this row, or on the previous one
            // through a transposition; once neither can finish within the
            // limit, the answer cannot either.
            if (minimum > limit && previousMinimum + EditCost.TRANSPOSITION > limit) return limit + 1

            val recycled = before
            before = previous
            previous = current
            current = recycled
            previousMinimum = minimum
        }
        return previous[length]
    }

    /** A lower bound of the distance from the letters alone, whatever their order. */
    private fun lettersBound(chars: CharArray, offset: Int, length: Int): Int {
        var arrivals = 0
        for (j in offset until offset + length) {
            val index = chars[j].code - block
            if (index !in 0 until BLOCK_SIZE) continue
            if (unmatched[index] > 0) unmatched[index]-- else arrivals += cheapestArrival[index]
        }
        var removals = 0
        for (index in typedLetters) {
            removals += unmatched[index] * cheapestRemoval[index]
            unmatched[index] = letterCounts[index]
        }
        return maxOf(arrivals, removals)
    }

    /**
     * The least it can cost to finish from cell ([i], [j]): the lengths left
     * differ, and each extra letter takes an insertion or a deletion (a
     * doubled letter's being the cheapest).
     */
    private fun leftover(i: Int, j: Int, length: Int): Int {
        val surplus = (text.length - i) - (length - j)
        return if (surplus > 0) surplus * EditCost.DOUBLED_DELETION else -surplus * EditCost.INSERTION
    }

    private fun substitution(index: Int, candidate: Char): Int {
        val inBlock = candidate.code - block
        return if (inBlock in 0 until BLOCK_SIZE) {
            substitutions[index * BLOCK_SIZE + inBlock]
        } else {
            EditCost.substitution(text[index], candidate, proximity)
        }
    }

    private fun ensureRows(size: Int) {
        if (previousRow.size >= size) return
        twoRowsBack = IntArray(size)
        previousRow = IntArray(size)
        currentRow = IntArray(size)
    }

    private companion object {
        const val CYRILLIC = 0x400
        const val BLOCK_SIZE = 256
    }
}
