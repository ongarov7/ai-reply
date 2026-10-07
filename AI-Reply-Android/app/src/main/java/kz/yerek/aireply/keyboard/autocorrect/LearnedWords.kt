package kz.yerek.aireply.keyboard.autocorrect

import kz.yerek.aireply.core.lang.KeyboardLanguage

/**
 * Words the user taught the keyboard in one language: kept when they undid a
 * correction or tapped what they typed. Most recent first, at most
 * [CAPACITY]; a learned word is never corrected.
 *
 * Пайдаланушы үйреткен сөздер: тек осы телефонда, ешқайда жіберілмейді.
 *
 * Immutable: learning returns a new value, so a lookup on the suggestion
 * thread never sees a list halfway through a change.
 */
class LearnedWords private constructor(
    /** Lowercased, most recent first. */
    val words: List<String>
) {
    private val lookup: Set<String> = words.toHashSet()

    /** [key] is a lowercased word. */
    operator fun contains(key: String): Boolean = key in lookup

    /** [key] first, older words after it, the oldest dropped past [CAPACITY]. */
    fun adding(key: String): LearnedWords =
        LearnedWords((sequenceOf(key) + words.asSequence().filter { it != key }).take(CAPACITY).toList())

    /** One word per line, the form [LearnedWordsStore] keeps. */
    fun encoded(): String = words.joinToString("\n")

    companion object {
        const val CAPACITY = 500

        val EMPTY = LearnedWords(emptyList())

        /** Reads what [encoded] wrote; tolerates blank lines and duplicates. */
        fun decode(text: String?): LearnedWords {
            if (text.isNullOrEmpty()) return EMPTY
            val words = text.lineSequence()
                .map { it.trim() }
                .filter { it.isNotEmpty() }
                .distinct()
                .take(CAPACITY)
                .toList()
            return LearnedWords(words)
        }
    }
}

/**
 * Where learned words live between sessions: on this phone only, never synced
 * and never sent. The keyboard keeps each language under [key] in its
 * preferences; tests use a map.
 */
interface LearnedWordsStore {

    /** What [write] last stored for [language], or null. */
    fun read(language: KeyboardLanguage): String?

    fun write(language: KeyboardLanguage, value: String)

    companion object {
        /** `autocorrect.learned.<lang>`. */
        fun key(language: KeyboardLanguage): String = "autocorrect.learned.${language.code}"
    }
}
