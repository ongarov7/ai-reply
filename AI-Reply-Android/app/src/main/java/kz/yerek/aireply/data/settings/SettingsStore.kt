package kz.yerek.aireply.data.settings

import android.content.Context
import android.content.SharedPreferences
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.flow.conflate
import kotlinx.coroutines.flow.onStart
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.domain.model.TemplateSummary

/**
 * The small amount of NON-SENSITIVE state the app screens and the keyboard both
 * read.
 *
 * WHY SharedPreferences AND NOT DataStore. DataStore exists to keep settings
 * reads off the main thread, and for an Activity that is exactly right. An
 * input method has the opposite problem: `onCreateInputView` must produce a
 * correct first frame synchronously, and the three values it needs to do that —
 * the app language, the appearance, and the cached template row — are a few
 * hundred bytes. With DataStore the choices are `runBlocking` on the main
 * thread (worse than what DataStore was avoiding) or a visible one-frame swap
 * of the whole chip row.
 *
 * SharedPreferences loads once and is an in-memory map afterwards, which is
 * what iOS's `UserDefaults` is and what that project relies on for the same
 * reason. The load is warmed in `AIReplyApplication` so even the first read in
 * the process is already in memory. Observers get a [Flow] all the same, so the
 * Compose screens never poll.
 *
 * Nothing secret belongs here — no tokens, no credentials, no message text.
 * The credential lives in [kz.yerek.aireply.data.secure.SecureCredentialStore].
 */
class SettingsStore internal constructor(private val prefs: SharedPreferences) {

    constructor(context: Context) : this(
        context.applicationContext.getSharedPreferences(NAME, Context.MODE_PRIVATE)
    )

    private val json = Json { ignoreUnknownKeys = true; encodeDefaults = true }

    /** Forces the backing file to be read, so no later call can block. */
    fun warmUp() {
        prefs.all
    }

    /** Emits on every change, starting with one immediate tick. */
    fun changes(): Flow<Unit> = callbackFlow {
        val listener = SharedPreferences.OnSharedPreferenceChangeListener { _, _ ->
            trySend(Unit)
        }
        prefs.registerOnSharedPreferenceChangeListener(listener)
        awaitClose { prefs.unregisterOnSharedPreferenceChangeListener(listener) }
    }.onStart { emit(Unit) }.conflate()

    // ---------------------------------------------------------------- layout

    /**
     * Raw layout code. Kept as a string so nothing has to compile the keyboard's
     * layout tables just to read it.
     */
    var keyboardLanguage: KeyboardLanguage
        get() {
            val stored = KeyboardLanguage.fromCode(prefs.getString(KEY_KEYBOARD_LANGUAGE, null))
            val enabled = enabledKeyboardLanguages
            if (stored != null && stored in enabled) return stored
            // First run: the layout of the app's own language, when it is on.
            val preferred = when (effectiveAppLanguage) {
                AppLanguage.KAZAKH -> KeyboardLanguage.KAZAKH
                AppLanguage.RUSSIAN -> KeyboardLanguage.RUSSIAN
                else -> KeyboardLanguage.ENGLISH
            }
            return if (preferred in enabled) preferred else enabled.first()
        }
        set(value) = prefs.edit().putString(KEY_KEYBOARD_LANGUAGE, value.code).apply()

    // ------------------------------------------------------------- interface

    /** null means "follow the system language". */
    var appLanguage: AppLanguage?
        get() = AppLanguage.fromCode(prefs.getString(KEY_APP_LANGUAGE, null))
        set(value) {
            prefs.edit().apply {
                if (value == null) remove(KEY_APP_LANGUAGE) else putString(KEY_APP_LANGUAGE, value.code)
            }.apply()
        }

    /** The language actually in effect right now. */
    val effectiveAppLanguage: AppLanguage
        get() = appLanguage ?: AppLanguage.systemDefault()

    var appearance: AppearancePreference
        get() = AppearancePreference.fromRaw(prefs.getString(KEY_APPEARANCE, null))
        set(value) = prefs.edit().putString(KEY_APPEARANCE, value.raw).apply()

    // ------------------------------------------------------------ chip cache

    /**
     * The visible templates, in bar order, as the app last saved them.
     *
     * null when nothing has been written yet, which is the honest answer for a
     * fresh install: the caller then falls back to the defaults rather than
     * showing an empty row.
     */
    var templateSummaries: List<TemplateSummary>?
        get() {
            val raw = prefs.getString(KEY_TEMPLATE_SUMMARIES, null) ?: return null
            return runCatching {
                json.decodeFromString<List<TemplateSummary>>(raw)
            }.getOrNull()?.takeIf { it.isNotEmpty() }
        }
        set(value) {
            if (value.isNullOrEmpty()) {
                prefs.edit().remove(KEY_TEMPLATE_SUMMARIES).apply()
                return
            }
            val encoded = runCatching { json.encodeToString(value) }.getOrNull() ?: return
            prefs.edit().putString(KEY_TEMPLATE_SUMMARIES, encoded).apply()
        }

    fun migrateToBackendOnly() {
        if (prefs.getBoolean(KEY_BACKEND_ONLY_MIGRATED, false)) return
        val legacyDeviceId = prefs.getString(KEY_INSTALL_ID, null)
        val editor = prefs.edit()
        if (accountDeviceId.isNullOrEmpty() && !legacyDeviceId.isNullOrEmpty()) {
            editor.putString(KEY_ACCOUNT_DEVICE, legacyDeviceId)
        }
        editor
            .remove(KEY_AI_MODE)
            .remove(KEY_AI_MODEL)
            .remove(KEY_AI_BACKEND)
            .remove(KEY_INSTALL_ID)
            .putBoolean(KEY_BACKEND_ONLY_MIGRATED, true)
            .commit()
    }

    // ---------------------------------------------------------- account state

    /**
     * NON-SECRET account state only. The tokens live in the Keystore-backed
     * store; what is here is what a screen needs to draw itself before any
     * network call returns.
     */
    var accessTokenExpiry: Long
        get() = prefs.getLong(KEY_ACCESS_EXPIRY, 0L)
        set(value) = prefs.edit().putLong(KEY_ACCESS_EXPIRY, value).apply()

    /** Masked by the server before it ever reached us. */
    var accountIdentifier: String?
        get() = prefs.getString(KEY_ACCOUNT_IDENTIFIER, null)
        set(value) {
            prefs.edit().apply {
                if (value.isNullOrEmpty()) remove(KEY_ACCOUNT_IDENTIFIER)
                else putString(KEY_ACCOUNT_IDENTIFIER, value)
            }.apply()
        }

    /** Server-issued device id, so a re-install does not orphan a device row. */
    var accountDeviceId: String?
        get() = prefs.getString(KEY_ACCOUNT_DEVICE, null)
        set(value) {
            prefs.edit().apply {
                if (value.isNullOrEmpty()) remove(KEY_ACCOUNT_DEVICE)
                else putString(KEY_ACCOUNT_DEVICE, value)
            }.apply()
        }

    /**
     * Last known quota, for display only.
     *
     * The server enforces the limit; if this and the server disagree, the
     * server is right. It exists so the keyboard can show "2 left today" the
     * instant it opens instead of blanking a label until a call returns.
     */
    var cachedDailyLimit: Int
        get() = prefs.getInt(KEY_USAGE_LIMIT, 0)
        set(value) = prefs.edit().putInt(KEY_USAGE_LIMIT, value).apply()

    var cachedUsedToday: Int
        get() = prefs.getInt(KEY_USAGE_USED, 0)
        set(value) = prefs.edit().putInt(KEY_USAGE_USED, value).apply()

    var cachedRemainingToday: Int
        get() = prefs.getInt(KEY_USAGE_REMAINING, 0)
        set(value) = prefs.edit().putInt(KEY_USAGE_REMAINING, value).apply()

    var cachedPlanCode: String?
        get() = prefs.getString(KEY_USAGE_PLAN, null)
        set(value) {
            prefs.edit().apply {
                if (value.isNullOrEmpty()) remove(KEY_USAGE_PLAN) else putString(KEY_USAGE_PLAN, value)
            }.apply()
        }

    fun clearAccountState() {
        prefs.edit()
            .remove(KEY_ACCESS_EXPIRY)
            .remove(KEY_ACCOUNT_IDENTIFIER)
            .remove(KEY_USAGE_LIMIT)
            .remove(KEY_USAGE_USED)
            .remove(KEY_USAGE_REMAINING)
            .remove(KEY_USAGE_PLAN)
            .apply()
    }

    // ------------------------------------------------------------ keyboard

    /**
     * The layouts the language key cycles through, in ҚАЗ → РУС → ENG order.
     * Never empty: switching the last one off is refused by the setter.
     */
    var enabledKeyboardLanguages: List<KeyboardLanguage>
        get() {
            val codes = prefs.getStringSet(KEY_ENABLED_LAYOUTS, null) ?: return KeyboardLanguage.CYCLE_ORDER
            return KeyboardLanguage.CYCLE_ORDER.filter { it.code in codes }.ifEmpty { KeyboardLanguage.CYCLE_ORDER }
        }
        set(value) {
            if (value.isEmpty()) return
            prefs.edit().putStringSet(KEY_ENABLED_LAYOUTS, value.map { it.code }.toSet()).apply()
        }

    /**
     * "Vibration on key press", on unless the user switched it off. It can only
     * silence the keyboard: the system's own touch-feedback setting still has
     * the last word, so this never forces vibration on.
     */
    var keyboardHaptics: Boolean
        get() = prefs.getBoolean(KEY_KEYBOARD_HAPTICS, true)
        set(value) = prefs.edit().putBoolean(KEY_KEYBOARD_HAPTICS, value).apply()

    /** The persona the user picked last, shown as selected on the chip row. */
    var lastTemplateId: String?
        get() = prefs.getString(KEY_LAST_TEMPLATE, null)
        set(value) {
            prefs.edit().apply {
                if (value == null) remove(KEY_LAST_TEMPLATE) else putString(KEY_LAST_TEMPLATE, value)
            }.apply()
        }

    /**
     * DEBUG BUILDS ONLY: canned replies instead of the service, so the reply
     * flow can be exercised on an emulator without spending the quota. The
     * switch is in Settings ▸ Developer, which release builds do not have, and
     * the keyboard ignores the value in a release build.
     */
    var debugMockReplies: Boolean
        get() = prefs.getBoolean(KEY_DEBUG_MOCK, false)
        set(value) = prefs.edit().putBoolean(KEY_DEBUG_MOCK, value).apply()

    /**
     * DEBUG BUILDS ONLY: show the Home notification card and the Settings push
     * controls even where push is unavailable, so both can be reviewed on an
     * emulator without Firebase or a new server. Ignored in release builds.
     */
    var debugForcePushPrompt: Boolean
        get() = prefs.getBoolean(KEY_DEBUG_FORCE_PUSH, false)
        set(value) = prefs.edit().putBoolean(KEY_DEBUG_FORCE_PUSH, value).apply()

    // ---------------------------------------------------------- notifications

    /**
     * The in-app notifications switch (Settings ▸ Notifications), on until the
     * user turns it off. The server stops sending to this installation when it
     * is off; the system permission is a separate matter.
     */
    var notificationsEnabled: Boolean
        get() = prefs.getBoolean(KEY_NOTIFICATIONS_ENABLED, true)
        set(value) = prefs.edit().putBoolean(KEY_NOTIFICATIONS_ENABLED, value).apply()

    /**
     * "Share diagnostics": the few app events (launches, notification taps,
     * connection failures), sent with the installation id and, when signed
     * in, attributed to the account. On by default; never message text,
     * typing or the clipboard, and never anything from the keyboard. Off also
     * stops the session id header.
     */
    var shareDiagnostics: Boolean
        get() = prefs.getBoolean(KEY_SHARE_DIAGNOSTICS, true)
        set(value) = prefs.edit().putBoolean(KEY_SHARE_DIAGNOSTICS, value).apply()

    /** The backing file, for [kz.yerek.aireply.ai.AILimits]. */
    val sharedPreferences: SharedPreferences get() = prefs

    // ------------------------------------------------------- onboarding hints

    /** Set once the setup guide has been completed, to stop re-nudging. */
    var hasSeenKeyboardSetup: Boolean
        get() = prefs.getBoolean(KEY_SEEN_SETUP, false)
        set(value) = prefs.edit().putBoolean(KEY_SEEN_SETUP, value).apply()

    private companion object {
        const val NAME = "aireply_settings"

        const val KEY_KEYBOARD_LANGUAGE = "shared.keyboardLanguage"
        const val KEY_APP_LANGUAGE = "shared.appLanguage"
        const val KEY_APPEARANCE = "shared.appearance"
        const val KEY_TEMPLATE_SUMMARIES = "shared.templateSummaries"
        const val KEY_SEEN_SETUP = "shared.seenKeyboardSetup"
        const val KEY_ENABLED_LAYOUTS = "shared.enabledKeyboardLanguages"
        const val KEY_LAST_TEMPLATE = "shared.lastTemplateID"
        const val KEY_KEYBOARD_HAPTICS = "shared.keyboardHaptics"
        const val KEY_DEBUG_MOCK = "debug.mockReplies"
        const val KEY_DEBUG_FORCE_PUSH = "debug.forcePushPrompt"
        const val KEY_NOTIFICATIONS_ENABLED = "notifications.enabled"
        const val KEY_SHARE_DIAGNOSTICS = "diagnostics.share"

        const val KEY_AI_MODE = "ai.transportMode"
        const val KEY_AI_MODEL = "ai.model"
        const val KEY_AI_BACKEND = "ai.backendBaseURL"
        const val KEY_INSTALL_ID = "ai.installIdentifier"
        const val KEY_BACKEND_ONLY_MIGRATED = "migration.backendOnly.v1"

        const val KEY_ACCESS_EXPIRY = "account.accessTokenExpiry"
        const val KEY_ACCOUNT_IDENTIFIER = "account.identifier"
        const val KEY_ACCOUNT_DEVICE = "account.deviceId"
        const val KEY_USAGE_LIMIT = "account.usage.dailyLimit"
        const val KEY_USAGE_USED = "account.usage.usedToday"
        const val KEY_USAGE_REMAINING = "account.usage.remainingToday"
        const val KEY_USAGE_PLAN = "account.usage.planCode"
    }
}
