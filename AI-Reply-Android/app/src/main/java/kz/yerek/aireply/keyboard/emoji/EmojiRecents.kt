package kz.yerek.aireply.keyboard.emoji

import android.content.Context
import android.content.SharedPreferences

/** Where the recent emoji are kept: one string. */
interface EmojiRecentsStore {
    fun read(): String?
    fun write(value: String)
}

/**
 * The recent emoji, in a preferences file of their own.
 *
 * They are what the user typed, so the file stays on this phone: it is
 * excluded from Auto Backup and device transfer (`res/xml/backup_rules.xml`,
 * `res/xml/data_extraction_rules.xml`), never synced and never sent.
 *
 * Соңғы эмодзилер тек осы телефонда: сақтық көшірмеге түспейді.
 */
class PrefsEmojiRecentsStore internal constructor(private val prefs: SharedPreferences) : EmojiRecentsStore {

    constructor(context: Context) : this(
        context.applicationContext.getSharedPreferences(NAME, Context.MODE_PRIVATE)
    )

    override fun read(): String? = prefs.getString(KEY, null)

    override fun write(value: String) {
        prefs.edit().putString(KEY, value).apply()
    }

    private companion object {
        const val NAME = "aireply_emoji"
        const val KEY = "emoji.recents"
    }
}

/**
 * The emoji the user typed last, newest first, at most [max]: a tap moves an
 * emoji to the front, and the oldest drops off the end.
 *
 * [load] reads the store and belongs off the main thread; [record] changes
 * the list in memory at once and writes it in the background (an `apply`).
 */
class EmojiRecents(private val store: EmojiRecentsStore, val max: Int = MAX) {

    @Volatile
    var items: List<String> = emptyList()
        private set

    fun load() {
        val stored = runCatching { store.read() }.getOrNull() ?: return
        items = stored.split(SEPARATOR).filter { it.isNotBlank() && it.length <= MAX_ENTRY_LENGTH }.distinct().take(max)
    }

    fun record(emoji: String) {
        if (emoji.isBlank() || emoji.length > MAX_ENTRY_LENGTH || SEPARATOR in emoji) return
        val next = (listOf(emoji) + items.filter { it != emoji }).take(max)
        if (next == items) return
        items = next
        runCatching { store.write(next.joinToString(SEPARATOR)) }
    }

    companion object {
        const val MAX = 30

        /** Emoji never contain a line break. */
        private const val SEPARATOR = "\n"

        /** The catalog's longest emoji is 14 UTF-16 units (a tag flag); anything far longer is not one. */
        private const val MAX_ENTRY_LENGTH = 32
    }
}
