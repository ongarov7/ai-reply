package kz.yerek.aireply.push

import kz.yerek.aireply.data.account.HeaderScope
import kz.yerek.aireply.data.account.RequestMetadata
import java.io.File
import java.util.TimeZone
import java.util.UUID

/**
 * What the app says about itself to the server: the installation, the build,
 * the OS and the interface language. Nothing that identifies the person, and
 * no hardware identifier (no IMEI, MAC, serial, ANDROID_ID or advertising id).
 *
 * Қосымша өзі туралы айтатыны: орнату, нұсқа, ОЖ және тіл. Жеке дерек жоқ.
 *
 * It is also the one [RequestMetadata] provider: every backend request gets
 * its metadata headers from here, through [kz.yerek.aireply.data.account.ApiClient].
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
    private val timeZone: () -> String = { TimeZone.getDefault().id },
    /** The foreground session, or null before the app was first opened. */
    private val sessionId: () -> String? = { null }
) : RequestMetadata {

    val appVersion: String = headerSafe(appVersion)
    val appBuild: String = headerSafe(appBuild)
    val osVersion: String = headerSafe(osVersion)
    val manufacturer: String = manufacturer.trim().take(64)
    val deviceModel: String = deviceModel.trim().take(64)

    val platform: String get() = PLATFORM
    val osName: String get() = OS_NAME

    /** Created on first use; see [InstallationIdStore]. Reads a file the first time. */
    val installationId: String get() = installationIds.id()

    val locale: String get() = language()
    val timezone: String get() = timeZone()
    val currentSessionId: String? get() = sessionId()

    override fun headers(scope: HeaderScope): Map<String, String> {
        val headers = LinkedHashMap<String, String>(8)
        headers["X-Platform"] = PLATFORM
        if (appVersion.isNotEmpty()) headers["X-App-Version"] = appVersion
        if (appBuild.isNotEmpty()) headers["X-App-Build"] = appBuild
        if (osVersion.isNotEmpty()) headers["X-OS-Version"] = osVersion
        if (scope == HeaderScope.APP) {
            headers["X-Installation-ID"] = installationId
            sessionId()?.takeIf(InstallationIdStore::isValid)?.let { headers["X-Session-ID"] = it }
        }
        return headers
    }

    companion object {
        const val PLATFORM = "android"
        const val OS_NAME = "Android"

        private val VERSION_CHARS = Regex("[^0-9A-Za-z.+_ ()-]")

        /**
         * What the server keeps of a version (`^[0-9A-Za-z.+_ ()-]{1,32}$`), and
         * what an HTTP header can carry: anything else is dropped, never sent.
         */
        fun headerSafe(value: String): String = VERSION_CHARS.replace(value.trim(), "").take(32).trim()
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
        val existing = runCatching { file.readText(Charsets.UTF_8).trim() }.getOrNull()
        if (existing != null && isValid(existing)) return existing

        val fresh = UUID.randomUUID().toString()
        // Written through a temporary file and renamed, so a crash half-way
        // never leaves a truncated id that would be replaced next launch.
        runCatching {
            file.parentFile?.mkdirs()
            val temporary = File(file.parentFile, "$FILE_NAME.tmp")
            temporary.writeText(fresh, Charsets.UTF_8)
            if (!temporary.renameTo(file)) {
                file.writeText(fresh, Charsets.UTF_8)
                temporary.delete()
            }
        }
        // Even if the disk refused, this process keeps one stable id.
        return fresh
    }

    companion object {
        const val FILE_NAME = "installation_id"

        private val PATTERN = Regex("^[A-Za-z0-9-]{8,64}$")

        /** The server's installation and session id format. */
        fun isValid(value: String): Boolean = PATTERN.matches(value)
    }
}
