package kz.yerek.aireply.push

import kz.yerek.aireply.platform.ReplyLog
import java.io.File
import java.util.TimeZone
import java.util.UUID

/**
 * What the app says about itself in the installation registration: the
 * installation, the build, the OS, the phone model and the interface language.
 * Nothing that identifies the person, and no hardware identifier (no IMEI, MAC,
 * serial, ANDROID_ID or advertising id).
 *
 * Қосымша өзі туралы айтатыны: орнату, нұсқа, ОЖ, модель және тіл. Жеке дерек жоқ.
 */
class ClientContext(
    private val installationIds: InstallationIdStore,
    appVersion: String,
    appBuild: String,
    osVersion: String,
    manufacturer: String,
    deviceModel: String,
    /** The app's interface language code: en, ru, kk or uz. */
    private val language: () -> String,
    private val timeZone: () -> String = { TimeZone.getDefault().id }
) {

    val appVersion: String = versionSafe(appVersion)
    val appBuild: String = versionSafe(appBuild)
    val osVersion: String = versionSafe(osVersion)
    val manufacturer: String = manufacturer.trim().take(64)
    val deviceModel: String = deviceModel.trim().take(64)

    val platform: String get() = PLATFORM
    val osName: String get() = OS_NAME

    /** Created on first use; see [InstallationIdStore]. Reads a file the first time. */
    val installationId: String get() = installationIds.id()

    val locale: String get() = language()
    val timezone: String get() = timeZone()

    companion object {
        const val PLATFORM = "android"
        const val OS_NAME = "Android"

        private val VERSION_CHARS = Regex("[^0-9A-Za-z.+_ ()-]")

        /** What the server keeps of a version (`^[0-9A-Za-z.+_ ()-]{1,32}$`): anything else is dropped. */
        fun versionSafe(value: String): String = VERSION_CHARS.replace(value.trim(), "").take(32).trim()
    }
}

/**
 * The installation id: a random UUID made once, in a file under
 * `Context.noBackupFilesDir`.
 *
 * Орнату идентификаторы — кездейсоқ UUID, сақтық көшірмеге кірмейді.
 *
 * It names this install of the app, not the phone and not the person: it is
 * not derived from any hardware id and it is not a credential (the server never
 * grants anything for knowing it). Because the directory is never backed up,
 * a reinstall, a restore onto a new phone or clearing the app's data produces
 * a new id, and the server sees a new installation. That is deliberate: two
 * phones restored from one backup must never share an id, or one account's
 * notifications could reach the other.
 */
class InstallationIdStore(private val directory: () -> File) {

    @Volatile
    private var cached: String? = null

    fun id(): String {
        cached?.let { return it }
        return synchronized(this) {
            cached ?: loadOrCreate().also { cached = it }
        }
    }

    private fun loadOrCreate(): String {
        val file = File(directory(), FILE_NAME)
        val existing = if (file.isFile) runCatching { file.readText(Charsets.UTF_8).trim() }.getOrNull() else null
        if (existing != null && isValid(existing)) return existing

        val fresh = UUID.randomUUID().toString()
        // Written through a temporary file and renamed, so a crash half-way
        // never leaves a truncated id that would be replaced next launch.
        try {
            file.parentFile?.mkdirs()
            val temporary = File(file.parentFile, "$FILE_NAME.tmp")
            temporary.writeText(fresh, Charsets.UTF_8)
            if (!temporary.renameTo(file)) {
                file.writeText(fresh, Charsets.UTF_8)
                temporary.delete()
            }
        } catch (failure: Exception) {
            // Even if the disk refused, this process keeps one stable id.
            ReplyLog.warn(failure) { "installation id kept in memory only" }
        }
        return fresh
    }

    companion object {
        const val FILE_NAME = "installation_id"

        private val PATTERN = Regex("^[A-Za-z0-9-]{8,64}$")

        /** The server's installation id format. */
        fun isValid(value: String): Boolean = PATTERN.matches(value)
    }
}
