package kz.yerek.aireply.keyboard.autocorrect

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kz.yerek.aireply.core.lang.KeyboardLanguage
import java.util.concurrent.ConcurrentHashMap
import kotlin.coroutines.CoroutineContext

/**
 * Local autocorrect: which words are real, what the user probably meant, what
 * they are about to type (DESIGN §7). Pure and deterministic; no UI, no
 * network, and nothing typed ever leaves the phone.
 *
 * Құрылғыдағы автотүзету: ештеңе серверге жіберілмейді.
 *
 * Thread-safe. Dictionaries are immutable once loaded; learned words are
 * swapped whole; the session's rejected corrections are a concurrent set. The
 * keyboard calls [analyze] on a background thread after each keystroke and
 * [learn] / [undo] from the main thread. Until [load] has finished for a
 * layout, [analyze] says nothing, so typing never waits for a dictionary.
 */
class AutocorrectEngine(
    private val dictionaries: AutocorrectDictionaries,
    private val learnedStore: LearnedWordsStore,
    private val loadContext: CoroutineContext = Dispatchers.IO
) {

    private val learned = ConcurrentHashMap<KeyboardLanguage, LearnedWords>()
    private val rejected: MutableSet<RejectedCorrection> = ConcurrentHashMap.newKeySet()
    private val learning = Any()

    fun isReady(language: KeyboardLanguage): Boolean =
        dictionaries.isLoaded(language) && learned.containsKey(language)

    /**
     * Reads [language]'s dictionaries and learned words on [loadContext].
     * Returns at once when they are already in memory. Throws
     * [java.io.IOException] when a dictionary cannot be read; the layout then
     * stays without suggestions and a later call tries again.
     */
    suspend fun load(language: KeyboardLanguage) {
        if (isReady(language)) return
        withContext(loadContext) {
            dictionaries.load(language)
            if (!learned.containsKey(language)) {
                learned.putIfAbsent(language, LearnedWords.decode(learnedStore.read(language)))
            }
        }
    }

    // ------------------------------------------------------------- lookups

    /**
     * §7.2: a learned word, or a word in one of the layout's filters (Russian
     * and Kazakh accept each other's words), `ё` also tried as `е`. Until the
     * layout is loaded only learned words count.
     */
    fun isKnown(word: String, language: KeyboardLanguage): Boolean = isKnownKey(lookupKey(word), language)

    /**
     * The nearest dictionary words, best first (§7.4); on the Kazakh layout
     * from the Kazakh and the Russian list (§10.4). Empty until loaded.
     */
    fun candidates(word: String, language: KeyboardLanguage, limit: Int = SUGGESTION_COUNT): List<Candidate> {
        if (!dictionaries.isLoaded(language)) return emptyList()
        return nearest(lookupKey(word), language, limit)
    }

    /**
     * The most frequent longer words that start with [prefix], in their
     * display form; nothing for a prefix shorter than two characters.
     */
    fun completions(prefix: String, language: KeyboardLanguage, limit: Int = SUGGESTION_COUNT): List<String> {
        val words = dictionaries.wordList(language) ?: return emptyList()
        return completionRanks(lookupKey(prefix), words, language, limit).map(words::display)
    }

    // ------------------------------------------------------------ decisions

    /**
     * What a separator after [word] should turn it into, or null to leave it
     * (§7.5). [before] is the text before the word in the field.
     */
    fun correction(word: String, before: String, language: KeyboardLanguage): String? {
        val lookup = lookup(word, before, language) ?: return null
        return decide(lookup, nearestIfUnknown(lookup))
    }

    /**
     * The correction and the suggestion strip for the word being typed (§7.5,
     * §7.6 with the §10.3 order): with a pending correction, the typed word,
     * the correction and one more word; otherwise the Kazakh spelling hint,
     * then completions of what was typed, then the nearest words.
     */
    fun analyze(word: String, before: String, language: KeyboardLanguage): AutocorrectAnalysis {
        val lookup = lookup(word, before, language) ?: return AutocorrectAnalysis.NONE
        val candidates = nearestIfUnknown(lookup)
        val correction = decide(lookup, candidates)
        val strip = Strip()
        val completions = { completionRanks(lookup.key, lookup.words, language, SUGGESTION_COUNT) }

        if (correction != null) {
            strip.add(word, Suggestion.Kind.TYPED)
            strip.add(correction, Suggestion.Kind.CORRECTION)
            candidates.forEach { strip.add(transferCase(word, it.word), Suggestion.Kind.CANDIDATE) }
            if (!strip.isFull) {
                completions().forEach { strip.add(transferCase(word, lookup.words.display(it)), Suggestion.Kind.COMPLETION) }
            }
        } else {
            kazakhHint(lookup)?.let { strip.add(it, Suggestion.Kind.HINT) }
            completions().forEach { strip.add(transferCase(word, lookup.words.display(it)), Suggestion.Kind.COMPLETION) }
            candidates.forEach { strip.add(transferCase(word, it.word), Suggestion.Kind.CANDIDATE) }
        }
        return AutocorrectAnalysis(correction, strip.items)
    }

    // ------------------------------------------------------------ learning

    /**
     * Remembers [word] on [language]'s layout: it is never corrected again.
     * The caller skips this in fields that ask for no personalised learning.
     */
    fun learn(word: String, language: KeyboardLanguage) {
        val key = lookupKey(word.trim())
        if (key.none(Char::isLetter)) return
        synchronized(learning) {
            val current = learned[language] ?: LearnedWords.decode(learnedStore.read(language))
            val updated = current.adding(key)
            learned[language] = updated
            learnedStore.write(language, updated.encoded())
        }
    }

    /**
     * Backspace right after [applied]: that correction is not offered again
     * this session, and with [learn] the original word is learned (§7.7). The
     * caller puts the original back, [AppliedCorrection.undoLength] units
     * before the caret.
     */
    fun undo(applied: AppliedCorrection, learn: Boolean) {
        rejected += RejectedCorrection(applied.language, lookupKey(applied.original), lookupKey(applied.correction))
        if (learn) learn(applied.original, applied.language)
    }

    // ------------------------------------------------------------ internals

    /** The typed word and what is known about it, or null when nothing may be said. */
    private class Lookup(
        val word: String,
        val key: String,
        val language: KeyboardLanguage,
        val words: WordList,
        val learned: Boolean,
        val known: Boolean
    )

    private data class RejectedCorrection(val language: KeyboardLanguage, val original: String, val correction: String)

    /** The strip's items: at most three, no word twice. */
    private class Strip {
        val items = ArrayList<Suggestion>(SUGGESTION_COUNT)
        val isFull: Boolean get() = items.size >= SUGGESTION_COUNT

        fun add(text: String, kind: Suggestion.Kind) {
            if (isFull || items.any { it.text == text }) return
            items += Suggestion(text, kind)
        }
    }

    private fun lookup(word: String, before: String, language: KeyboardLanguage): Lookup? {
        if (word.isEmpty() || !isReady(language)) return null
        val words = dictionaries.wordList(language) ?: return null
        if (TokenGuard.isProtected(word, before, language)) return null
        val key = lookupKey(word)
        val learned = key in learnedWords(language)
        return Lookup(word, key, language, words, learned, learned || isKnownKey(key, language))
    }

    private fun nearestIfUnknown(lookup: Lookup): List<Candidate> =
        if (lookup.known) emptyList() else nearest(lookup.key, lookup.language, SUGGESTION_COUNT)

    /**
     * A word known only as the plain spelling of a Kazakh one, on the Kazakh
     * layout: the Kazakh spelling, offered first and never applied by itself.
     * Not for a word the Kazakh or the Russian list has as typed: a listed
     * Russian word (`был`, `куда`, `они`) is meant as written.
     */
    private fun kazakhHint(lookup: Lookup): String? {
        if (!lookup.known || lookup.learned || lookup.language != KeyboardLanguage.KAZAKH || lookup.key in lookup.words) {
            return null
        }
        if (isListedInOtherLanguage(lookup.key, lookup.language)) return null
        val rank = CandidateSearch.kazakhSpelling(lookup.words, lookup.key)
        return if (rank >= 0) transferCase(lookup.word, lookup.words.display(rank)) else null
    }

    private fun decide(lookup: Lookup, candidates: List<Candidate>): String? {
        if (lookup.learned) return null
        if (lookup.language == KeyboardLanguage.ENGLISH) {
            EnglishReplacements[lookup.key]?.let { replacement ->
                val fixed = transferCase(lookup.word, replacement)
                return fixed.takeUnless { it == lookup.word || isRejected(lookup, lookupKey(replacement)) }
            }
        }
        if (lookup.known) return null

        val best = candidates.firstOrNull() ?: return null
        val second = candidates.getOrNull(1)
        val length = lookup.key.length
        val confident = length >= 3 &&
            best.tenths <= autocorrectDistance(length) &&
            (second == null || second.score - best.score >= SCORE_MARGIN) &&
            (best.rank < RARE_RANK || best.tenths <= CLOSE_DISTANCE)
        if (!confident || isRejected(lookup, lookupKey(best.word))) return null
        return transferCase(lookup.word, best.word)
    }

    /** Whether a list [language] draws candidates from besides its own has [key] as typed (§10.4). */
    private fun isListedInOtherLanguage(key: String, language: KeyboardLanguage): Boolean =
        AutocorrectDictionaries.candidateOrder(language)
            .filter { it != language }
            .any { other -> dictionaries.wordList(other)?.contains(key) == true }

    private fun isRejected(lookup: Lookup, correctionKey: String): Boolean =
        RejectedCorrection(lookup.language, lookup.key, correctionKey) in rejected

    /**
     * The best [limit] words from every list of the layout's pool, each scored
     * with its own list's rank (§10.4). A word typed with a Kazakh letter is
     * Kazakh: the Russian list has nothing to offer it. A word in both lists
     * counts once, with its better score; ties keep the layout's own list.
     */
    private fun nearest(key: String, language: KeyboardLanguage, limit: Int): List<Candidate> {
        val pool = AutocorrectDictionaries.candidateOrder(language)
            .filter { it == language || !KazakhLetters.hasKazakhLetter(key) }
            .mapNotNull(dictionaries::wordList)
        val found = pool.flatMap { words -> nearestIn(words, key, language, limit) }
        if (pool.size == 1) return found
        return found
            .sortedWith(compareBy<Candidate>({ it.score }, { it.rank }))
            .distinctBy { it.word.lowercase() }
            .take(limit)
    }

    private fun nearestIn(words: WordList, key: String, language: KeyboardLanguage, limit: Int): List<Candidate> {
        val typed = TypedWord(key, KeyProximity.of(language))
        return CandidateSearch.nearest(words, typed, limit) { rank ->
            val candidate = words.lowercased(rank)
            !(language == KeyboardLanguage.ENGLISH && EnglishReplacements.isReplaced(candidate)) &&
                !KazakhLetters.losesKazakhLetter(key, candidate)
        }
    }

    private fun completionRanks(key: String, words: WordList, language: KeyboardLanguage, limit: Int): List<Int> {
        if (key.length < MIN_COMPLETION_PREFIX) return emptyList()
        return words.completions(key, limit) { rank ->
            language != KeyboardLanguage.ENGLISH || !EnglishReplacements.isReplaced(words.lowercased(rank))
        }
    }

    private fun isKnownKey(key: String, language: KeyboardLanguage): Boolean {
        if (key in learnedWords(language)) return true
        val filters = AutocorrectDictionaries.knownOrder(language).mapNotNull(dictionaries::filter)
        if (filters.any { key in it }) return true
        if (language == KeyboardLanguage.ENGLISH || 'ё' !in key) return false
        val plain = key.replace('ё', 'е')
        return filters.any { plain in it }
    }

    private fun learnedWords(language: KeyboardLanguage): LearnedWords = learned[language] ?: LearnedWords.EMPTY

    companion object {
        /** Slots on the suggestion strip. */
        const val SUGGESTION_COUNT = 3

        private const val MIN_COMPLETION_PREFIX = 2

        /** The runner-up must be this much worse before a correction is applied. */
        private const val SCORE_MARGIN = 0.25

        /** Rarer words are applied only when very close. */
        private const val RARE_RANK = 40_000
        private const val CLOSE_DISTANCE = 6

        /** How far a correction may be from a word of [length] letters to apply it by itself. */
        private fun autocorrectDistance(length: Int): Int = when {
            length <= 4 -> 10
            length <= 7 -> 13
            else -> 20
        }

        /** What words are matched by: lowercased, with a typographic apostrophe made plain. */
        private fun lookupKey(word: String): String = word.lowercase().replace('’', '\'')

        /**
         * A typed Capitalised word gets a capitalised replacement; a lowercase
         * one gets the dictionary's display form (proper nouns stay capitalised).
         */
        internal fun transferCase(typed: String, replacement: String): String =
            if (typed.firstOrNull()?.isUpperCase() == true) replacement.replaceFirstChar { it.uppercase() } else replacement
    }
}
