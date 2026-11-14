package kz.yerek.aireply.data.profile

import android.content.Context
import kz.yerek.aireply.data.BackendEnvironment
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kz.yerek.aireply.domain.model.ReplyConfiguration
import java.io.File

/**
 * Persistence for the profile and templates.
 *
 * WHY A JSON FILE AND NOT Room. The keyboard has to read this, and it has to do
 * so without paying database-stack setup every time it appears inside WhatsApp.
 * The whole configuration is a few kilobytes, written when a settings screen
 * closes and read when the keyboard appears. A database would add a dependency,
 * a migration surface and a startup cost to solve a problem this app does not
 * have. The iOS project reaches the same conclusion about Core Data, for the
 * same reasons.
 *
 * WHY NOT SharedPreferences. Preferences are for flags. A structured document
 * belongs in a file; [kz.yerek.aireply.data.settings.SettingsStore] keeps only
 * the small values the first keyboard frame needs.
 *
 * Thread safety: every entry point is `@Synchronized`, and the cache is
 * invalidated by the file's own modification time rather than by trusting that
 * this process is the only writer.
 */
class ProfileStore(context: Context, environment: BackendEnvironment = BackendEnvironment.current) {

    private val file = File(context.applicationContext.filesDir, environment.storageName(FILE_NAME))

    private val json = Json {
        ignoreUnknownKeys = true
        encodeDefaults = true
        // A field written as null by a future build decodes to the property's
        // default instead of throwing, which is the Kotlin equivalent of the
        // iOS `decodeIfPresent ?? default` the Swift models use throughout.
        coerceInputValues = true
    }

    private var cached: ReplyConfiguration? = null
    private var cachedModified: Long = -1L

    /**
     * Reads the configuration, decoding only when the file has actually changed
     * since the last read.
     *
     * PERFORMANCE. The keyboard calls this once per appearance, never per
     * keystroke. The modification-time check means a keyboard that appears
     * repeatedly inside the same messenger session pays one `stat` rather than
     * a full JSON parse.
     */
    @Synchronized
    fun load(): ReplyConfiguration {
        val modified = if (file.exists()) file.lastModified() else 0L
        cached?.let { if (modified == cachedModified) return it }

        val decoded = runCatching {
            if (!file.exists()) return@runCatching null
            json.decodeFromString<ReplyConfiguration>(file.readText())
        }.getOrNull()

        val value = (decoded ?: ReplyConfiguration.INITIAL).normalized()
        cached = value
        cachedModified = modified
        return value
    }

    /**
     * Persists the configuration. Returns false when the write failed, which
     * callers surface rather than swallow.
     *
     * The write is atomic: a temporary file is renamed over the real one, so a
     * keyboard reading mid-write sees either the old file or the new one, never
     * a truncated one.
     */
    @Synchronized
    fun save(configuration: ReplyConfiguration): Boolean {
        val normalized = configuration.normalized()
        cached = normalized

        return runCatching {
            val encoded = json.encodeToString(normalized)
            val temp = File(file.parentFile, "${file.name}.tmp")
            temp.writeText(encoded)
            if (!temp.renameTo(file)) {
                // renameTo can refuse when the destination exists on some
                // filesystems. Falling back to a direct write is still better
                // than losing the user's edit; the window it opens is the one
                // atomic rename was avoiding, and it is microseconds wide.
                file.writeText(encoded)
                temp.delete()
            }
            cachedModified = file.lastModified()
            true
        }.getOrDefault(false)
    }

    private companion object {
        const val FILE_NAME = "reply-configuration.json"
    }
}
