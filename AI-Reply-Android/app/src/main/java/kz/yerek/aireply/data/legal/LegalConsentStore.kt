package kz.yerek.aireply.data.legal

import android.content.Context
import android.content.SharedPreferences
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.encodeToString
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kz.yerek.aireply.data.account.LegalConfigDto
import kz.yerek.aireply.data.account.LegalConsentDto
import java.time.Instant

@Serializable
data class StoredLegalConsent(
    val termsVersion: String,
    val privacyVersion: String,
    val acceptedAt: String,
    val locale: String,
    val platform: String,
    val appVersion: String,
    val pendingSync: Boolean
)

/**
 * Stores non-secret legal acceptance until it can be attached to an account,
 * and the legal versions the server last published.
 *
 * Келісім мен сервердің соңғы құжат нұсқалары: пернетақта да осыдан оқиды.
 *
 * The keyboard asks [hasAcceptedLatest] when Reply or Write is tapped, so both
 * values are kept in memory after the first read.
 */
class LegalConsentStore internal constructor(private val prefs: SharedPreferences) {

    constructor(context: Context) : this(
        context.applicationContext.getSharedPreferences(NAME, Context.MODE_PRIVATE)
    )

    private val json = Json { ignoreUnknownKeys = true; encodeDefaults = true }

    @Volatile
    private var record: Loaded<StoredLegalConsent>? = null

    @Volatile
    private var latest: Loaded<LegalConfigDto>? = null

    /** A value read once; [value] may be null when nothing is stored. */
    private class Loaded<T>(val value: T?)

    fun current(): StoredLegalConsent? {
        record?.let { return it.value }
        val raw = prefs.getString(KEY_RECORD, null)
        val decoded = raw?.let { runCatching { json.decodeFromString<StoredLegalConsent>(it) }.getOrNull() }
        record = Loaded(decoded)
        return decoded
    }

    fun hasAccepted(config: LegalConfigDto): Boolean {
        val record = current() ?: return false
        return record.termsVersion == config.termsVersion &&
            record.privacyVersion == config.privacyVersion
    }

    /** The versions the server published last, or the built-in ones before it ever answered. */
    fun latestConfig(): LegalConfigDto {
        latest?.let { return it.value ?: LegalConfigDto.PRODUCTION }
        val raw = prefs.getString(KEY_LATEST_CONFIG, null)
        val decoded = raw?.let { runCatching { json.decodeFromString<LegalConfigDto>(it) }.getOrNull() }
        latest = Loaded(decoded)
        return decoded ?: LegalConfigDto.PRODUCTION
    }

    /** What the app and the keyboard check before an AI request. */
    fun hasAcceptedLatest(): Boolean = hasAccepted(latestConfig())

    /** The server's `legal` block from `GET /config`: new versions need a new acceptance. */
    fun rememberConfig(config: LegalConfigDto) {
        if (latestConfig() == config) return
        latest = Loaded(config)
        prefs.edit().putString(KEY_LATEST_CONFIG, json.encodeToString(config)).apply()
    }

    fun accept(config: LegalConfigDto, locale: String, appVersion: String): StoredLegalConsent {
        val record = StoredLegalConsent(
            termsVersion = config.termsVersion,
            privacyVersion = config.privacyVersion,
            acceptedAt = Instant.now().toString(),
            locale = locale,
            platform = "android",
            appVersion = appVersion,
            pendingSync = true
        )
        save(record)
        return record
    }

    fun restore(consent: LegalConsentDto) {
        save(
            StoredLegalConsent(
                termsVersion = consent.termsVersion,
                privacyVersion = consent.privacyVersion,
                acceptedAt = consent.acceptedAt,
                locale = consent.locale,
                platform = consent.platform,
                appVersion = consent.appVersion.orEmpty(),
                pendingSync = false
            )
        )
    }

    /**
     * The consent is withdrawn, the server asked for it again, or the account
     * was deleted: the consent screen shows until it is given again. The
     * published versions are the server's, not the user's, and stay.
     */
    fun clear() {
        record = Loaded(null)
        prefs.edit().remove(KEY_RECORD).apply()
    }

    private fun save(record: StoredLegalConsent) {
        this.record = Loaded(record)
        prefs.edit().putString(KEY_RECORD, json.encodeToString(record)).apply()
    }

    private companion object {
        const val NAME = "aireply_legal"
        const val KEY_RECORD = "legal.consent.v1"
        const val KEY_LATEST_CONFIG = "legal.latestConfig.v1"
    }
}
