package kz.yerek.aireply.data.profile

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kz.yerek.aireply.ai.AIFeatures
import kz.yerek.aireply.data.account.ProfileUpdate
import kz.yerek.aireply.data.settings.DeviceStateStore
import kz.yerek.aireply.platform.ReplyLog

/**
 * Tells the server which language the account's notifications and e-mails
 * should be written in (`preferred_language` on `/me`).
 *
 * Хабарлама тілі: қосымша тілі өзгергенде немесе тіркелгіде тіл жоқ болса ғана жіберіледі.
 *
 * Sent in two cases only: the user picked a language in Settings (System
 * sends the language the app actually shows), or the account has none yet, in
 * which case it gets this phone's. An account whose language was set on
 * another device is left alone, so two phones never fight over it. Like the
 * gender in [ProfileSync], an unsent change stays marked
 * ([DeviceStateStore.languagePendingSync]), is retried on the next foreground
 * and is dropped at sign-out. Nothing is sent to a server that did not
 * announce [AIFeatures.preferredLanguage]: it would refuse the unknown field.
 */
class PreferredLanguageSync(
    /** `POST /api/v1/me`; throws when the server did not take it. */
    private val update: suspend (ProfileUpdate) -> Unit,
    private val device: DeviceStateStore,
    private val isSignedIn: () -> Boolean,
    private val features: () -> AIFeatures,
    /** The code of the language the app shows now: en, ru, kk or uz. */
    private val language: () -> String,
    /** Outlives the screen that made the change. */
    private val scope: CoroutineScope
) {

    /** One request at a time, so an older language can never land after a newer one. */
    private val sending = Mutex()

    /** The user chose a language in Settings, System included. */
    fun languageChanged() {
        device.languagePendingSync = true
        scope.launch { pushPending() }
    }

    /**
     * After sign-in or `/me`: [server] is the account's `preferred_language`.
     * An account without one gets this phone's language; otherwise nothing.
     */
    fun adopt(server: String?) {
        if (!features().preferredLanguage || !server.isNullOrBlank()) return
        device.languagePendingSync = true
        scope.launch { pushPending() }
    }

    /** Sends a language the server has not been told yet. A no-op when nothing is waiting. */
    suspend fun pushPending() {
        if (!device.languagePendingSync || !canSend()) return
        sending.withLock {
            if (!device.languagePendingSync || !canSend()) return
            val code = language()
            try {
                update(ProfileUpdate(preferredLanguage = code))
                // A newer choice made while this one was in flight is still pending.
                if (language() == code) device.languagePendingSync = false
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (failure: Exception) {
                // Offline or refused: it stays pending for the next foreground.
                ReplyLog.warn(failure) { "preferred language not sent yet" }
            }
        }
    }

    private fun canSend(): Boolean = isSignedIn() && features().preferredLanguage
}
