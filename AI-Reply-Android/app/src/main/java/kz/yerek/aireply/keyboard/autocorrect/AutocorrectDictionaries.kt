package kz.yerek.aireply.keyboard.autocorrect

import kz.yerek.aireply.core.lang.KeyboardLanguage
import java.io.InputStream
import java.util.concurrent.ConcurrentHashMap

/**
 * Opens a dictionary file by name (`ru.words`, `kk.known`). The caller closes
 * the stream. The keyboard reads the APK's assets ([AssetDictionarySource]);
 * JVM tests read `src/main/assets` directly.
 */
fun interface DictionarySource {
    fun open(fileName: String): InputStream
}

/**
 * The dictionaries of the layouts in use, read once and then shared
 * read-only by every lookup.
 *
 * Сөздіктер бір рет оқылады, кейін тек оқу үшін ортақ.
 *
 * A layout needs the word lists its corrections come from ([candidateOrder])
 * and every filter its known check consults ([knownOrder]). Reading is blocking (a few megabytes from the APK), so
 * [load] belongs off the main thread; until a layout is loaded the engine has
 * nothing to say about it and typing never waits.
 */
class AutocorrectDictionaries(private val source: DictionarySource) {

    private val wordLists = ConcurrentHashMap<KeyboardLanguage, WordList>()
    private val filters = ConcurrentHashMap<KeyboardLanguage, KnownWordsFilter>()

    fun wordList(language: KeyboardLanguage): WordList? = wordLists[language]

    fun filter(language: KeyboardLanguage): KnownWordsFilter? = filters[language]

    fun isLoaded(language: KeyboardLanguage): Boolean =
        candidateOrder(language).all(wordLists::containsKey) && knownOrder(language).all(filters::containsKey)

    /**
     * Reads whatever [language] still lacks. Blocking. Throws
     * [java.io.IOException] when a file is missing or malformed; the layout
     * then stays unloaded and a later call tries again.
     */
    @Synchronized
    fun load(language: KeyboardLanguage) {
        for (listLanguage in candidateOrder(language)) {
            if (!wordLists.containsKey(listLanguage)) {
                wordLists[listLanguage] = source.open("${listLanguage.code}.words").use(WordList::read)
            }
        }
        for (filterLanguage in knownOrder(language)) {
            if (!filters.containsKey(filterLanguage)) {
                filters[filterLanguage] = source.open("${filterLanguage.code}.known").use(KnownWordsFilter::read)
            }
        }
    }

    companion object {
        /**
         * The filters a word is looked up in on [language]'s layout, in order
         * (§7.2). Kazakh is typed on the Russian layout and Russian on the
         * Kazakh one, so neither may "correct" the other.
         */
        fun knownOrder(language: KeyboardLanguage): List<KeyboardLanguage> = when (language) {
            KeyboardLanguage.ENGLISH -> listOf(KeyboardLanguage.ENGLISH)
            KeyboardLanguage.RUSSIAN -> listOf(KeyboardLanguage.RUSSIAN, KeyboardLanguage.KAZAKH)
            KeyboardLanguage.KAZAKH -> listOf(KeyboardLanguage.KAZAKH, KeyboardLanguage.RUSSIAN)
        }

        /**
         * The word lists [language]'s corrections come from, in order (§10.4).
         * People write Russian on the Kazakh layout as well, so it draws on
         * both; Russian and English keep to their own list. Completions always
         * come from the layout's own list.
         */
        fun candidateOrder(language: KeyboardLanguage): List<KeyboardLanguage> = when (language) {
            KeyboardLanguage.KAZAKH -> listOf(KeyboardLanguage.KAZAKH, KeyboardLanguage.RUSSIAN)
            KeyboardLanguage.ENGLISH, KeyboardLanguage.RUSSIAN -> listOf(language)
        }
    }
}
