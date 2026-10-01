package kz.yerek.aireply.push

import android.content.Context
import android.content.SharedPreferences

/**
 * Push and installation state that belongs to THIS device only.
 *
 * Push күйі — тек осы құрылғыға тиесілі, сақтық көшірмеге кірмейді.
 *
 * A separate preferences file, excluded from backup and device transfer
 * (res/xml/backup_rules.xml, data_extraction_rules.xml): an FCM token or a
 * "last synced" mark restored onto another phone would be wrong there — the
 * token belongs to the old phone, and the mark would stop the new one from
 * registering. The user's own choices (the notifications switch, "Share
 * diagnostics") live in SettingsStore and do travel with a backup.
 */
class PushStateStore internal constructor(private val prefs: SharedPreferences) {

    constructor(context: Context) : this(
        context.applicationContext.getSharedPreferences(NAME, Context.MODE_PRIVATE)
    )

    /** The current FCM registration token, as Firebase last reported it. */
    var fcmToken: String?
        get() = prefs.getString(KEY_TOKEN, null)
        set(value) = putString(KEY_TOKEN, value)

    /** The token the server last confirmed as active for this installation. */
    var registeredToken: String?
        get() = prefs.getString(KEY_REGISTERED_TOKEN, null)
        set(value) = putString(KEY_REGISTERED_TOKEN, value)

    /**
     * The signed-in account's user id, for "has the owner changed since the
     * last sync". Never sent anywhere: the server takes the account from the
     * token.
     */
    var accountUserId: String?
        get() = prefs.getString(KEY_ACCOUNT, null)
        set(value) = putString(KEY_ACCOUNT, value)

    // ------------------------------------------------------ registration

    val lastSyncFingerprint: String? get() = prefs.getString(KEY_SYNC_FINGERPRINT, null)
    val lastSyncAt: Long get() = prefs.getLong(KEY_SYNC_AT, 0L)

    val rejectedFingerprint: String? get() = prefs.getString(KEY_REJECTED_FINGERPRINT, null)
    val rejectedAt: Long get() = prefs.getLong(KEY_REJECTED_AT, 0L)

    val failedFingerprint: String? get() = prefs.getString(KEY_FAILED_FINGERPRINT, null)
    val failureCount: Int get() = prefs.getInt(KEY_FAILURES, 0)
    val nextAttemptAt: Long get() = prefs.getLong(KEY_NEXT_ATTEMPT, 0L)

    fun recordSuccess(fingerprint: String, at: Long) {
        prefs.edit()
            .putString(KEY_SYNC_FINGERPRINT, fingerprint)
            .putLong(KEY_SYNC_AT, at)
            .remove(KEY_REJECTED_FINGERPRINT)
            .remove(KEY_REJECTED_AT)
            .remove(KEY_FAILED_FINGERPRINT)
            .remove(KEY_FAILURES)
            .remove(KEY_NEXT_ATTEMPT)
            .apply()
    }

    /** The server refused this exact payload (a 4xx): sending it again cannot help. */
    fun recordRejected(fingerprint: String, at: Long) {
        prefs.edit()
            .putString(KEY_REJECTED_FINGERPRINT, fingerprint)
            .putLong(KEY_REJECTED_AT, at)
            .remove(KEY_FAILED_FINGERPRINT)
            .remove(KEY_FAILURES)
            .remove(KEY_NEXT_ATTEMPT)
            .apply()
    }

    /** One more failure of [fingerprint]; returns the failure count. */
    fun recordFailure(fingerprint: String, nextAttemptAt: Long): Int {
        val count = if (failedFingerprint == fingerprint) failureCount + 1 else 1
        prefs.edit()
            .putString(KEY_FAILED_FINGERPRINT, fingerprint)
            .putInt(KEY_FAILURES, count)
            .putLong(KEY_NEXT_ATTEMPT, nextAttemptAt)
            .apply()
        return count
    }

    /** Forgets the sync marks, so the next trigger registers for certain. */
    fun invalidateSync() {
        prefs.edit()
            .remove(KEY_SYNC_FINGERPRINT)
            .remove(KEY_SYNC_AT)
            .remove(KEY_REJECTED_FINGERPRINT)
            .remove(KEY_REJECTED_AT)
            .apply()
    }

    // -------------------------------------------------------- permission

    /** The app has shown the system notification dialog at least once. */
    var permissionRequested: Boolean
        get() = prefs.getBoolean(KEY_PERMISSION_REQUESTED, false)
        set(value) = prefs.edit().putBoolean(KEY_PERMISSION_REQUESTED, value).apply()

    /** The user answered "Don't allow" at least once (a dismissed dialog does not count). */
    var permissionDeniedOnce: Boolean
        get() = prefs.getBoolean(KEY_PERMISSION_DENIED_ONCE, false)
        set(value) = prefs.edit().putBoolean(KEY_PERMISSION_DENIED_ONCE, value).apply()

    /** Denied with "don't ask again" (or twice): only system settings can change it now. */
    var permissionPermanentlyDenied: Boolean
        get() = prefs.getBoolean(KEY_PERMISSION_BLOCKED, false)
        set(value) = prefs.edit().putBoolean(KEY_PERMISSION_BLOCKED, value).apply()

    /** "Not now" on the Home card, or a denial from it: the card stays hidden. */
    var promptDismissed: Boolean
        get() = prefs.getBoolean(KEY_PROMPT_DISMISSED, false)
        set(value) = prefs.edit().putBoolean(KEY_PROMPT_DISMISSED, value).apply()

    private fun putString(key: String, value: String?) {
        prefs.edit().apply {
            if (value.isNullOrEmpty()) remove(key) else putString(key, value)
        }.apply()
    }

    companion object {
        /** Excluded from backup by name in res/xml; keep the two in step. */
        const val NAME = "aireply_push"

        private const val KEY_TOKEN = "push.fcmToken"
        private const val KEY_REGISTERED_TOKEN = "push.registeredToken"
        private const val KEY_ACCOUNT = "push.accountUserId"
        private const val KEY_SYNC_FINGERPRINT = "installation.syncFingerprint"
        private const val KEY_SYNC_AT = "installation.syncAt"
        private const val KEY_REJECTED_FINGERPRINT = "installation.rejectedFingerprint"
        private const val KEY_REJECTED_AT = "installation.rejectedAt"
        private const val KEY_FAILED_FINGERPRINT = "installation.failedFingerprint"
        private const val KEY_FAILURES = "installation.failures"
        private const val KEY_NEXT_ATTEMPT = "installation.nextAttemptAt"
        private const val KEY_PERMISSION_REQUESTED = "permission.requested"
        private const val KEY_PERMISSION_BLOCKED = "permission.permanentlyDenied"
        private const val KEY_PERMISSION_DENIED_ONCE = "permission.deniedOnce"
        private const val KEY_PROMPT_DISMISSED = "prompt.dismissed"
    }
}
