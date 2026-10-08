package kz.yerek.aireply

import kotlinx.coroutines.runBlocking
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.keyboard.autocorrect.AutocorrectDictionaries
import kz.yerek.aireply.keyboard.autocorrect.AutocorrectEngine
import kz.yerek.aireply.keyboard.autocorrect.DictionarySource
import kz.yerek.aireply.keyboard.autocorrect.LearnedWordsStore
import java.io.File

/**
 * The dictionaries the APK ships, read once for every autocorrect test.
 * Gradle runs unit tests with the module directory as the working directory,
 * which is what makes the relative path stable.
 */
object AutocorrectTestDictionaries {

    val source = DictionarySource { name -> File("src/main/assets/dictionaries/$name").inputStream() }

    val shipped: AutocorrectDictionaries by lazy {
        AutocorrectDictionaries(source).apply { KeyboardLanguage.entries.forEach(::load) }
    }

    /** A fresh engine (no learned words, nothing rejected) over the shared dictionaries, every layout loaded. */
    fun engine(store: MemoryLearnedWordsStore = MemoryLearnedWordsStore()): AutocorrectEngine =
        AutocorrectEngine(shipped, store).also { engine ->
            runBlocking { KeyboardLanguage.entries.forEach { engine.load(it) } }
        }
}

/** Learned words kept in a map, as the keyboard keeps them in its preferences. */
class MemoryLearnedWordsStore : LearnedWordsStore {
    val values = HashMap<KeyboardLanguage, String>()
    var writes = 0
        private set

    override fun read(language: KeyboardLanguage): String? = values[language]

    override fun write(language: KeyboardLanguage, value: String) {
        values[language] = value
        writes++
    }

    override var generation = 0
        private set

    override fun clear() {
        values.clear()
        generation++
    }
}
