package kz.yerek.aireply.keyboard.autocorrect

import android.content.Context
import android.content.SharedPreferences
import kz.yerek.aireply.core.lang.KeyboardLanguage
import java.util.concurrent.atomic.AtomicInteger

/**
 * The words the user taught the keyboard, one preferences key per layout
 * (`autocorrect.learned.<lang>`), in a file of their own.
 *
 * Үйретілген сөздер тек осы телефонда: сақтық көшірмеге де түспейді.
 *
 * They are words the user typed, so the file stays on this phone: it is
 * excluded from Auto Backup and device transfer (`res/xml/backup_rules.xml`,
 * `res/xml/data_extraction_rules.xml`), never synced and never sent.
 */
class PrefsLearnedWordsStore internal constructor(private val prefs: SharedPreferences) : LearnedWordsStore {

    constructor(context: Context) : this(
        context.applicationContext.getSharedPreferences(NAME, Context.MODE_PRIVATE)
    )

    override fun read(language: KeyboardLanguage): String? = prefs.getString(LearnedWordsStore.key(language), null)

    override fun write(language: KeyboardLanguage, value: String) {
        prefs.edit().putString(LearnedWordsStore.key(language), value).apply()
    }

    override fun clear() {
        // The copy in memory is out of date first, so it cannot be written back after the removal.
        clears.incrementAndGet()
        prefs.edit().apply { KeyboardLanguage.entries.forEach { remove(LearnedWordsStore.key(it)) } }.apply()
    }

    /** Shared by every instance: the app clears the file the keyboard's engine has in memory. */
    override val generation: Int get() = clears.get()

    private companion object {
        const val NAME = "aireply_autocorrect"

        val clears = AtomicInteger()
    }
}
