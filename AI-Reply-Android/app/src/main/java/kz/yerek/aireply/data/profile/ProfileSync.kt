package kz.yerek.aireply.data.profile

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kz.yerek.aireply.ai.AIFeatures
import kz.yerek.aireply.data.account.AccountProfile
import kz.yerek.aireply.data.account.AccountService
import kz.yerek.aireply.data.account.ProfileUpdate
import kz.yerek.aireply.data.settings.DeviceStateStore
import kz.yerek.aireply.domain.model.GrammaticalGender

/**
 * Keeps the grammatical gender the same on every device of one account, gives
 * the server its informational copy of the onboarding version, and tells it
 * the language for the account's notifications ([PreferredLanguageSync]).
 *
 * Жыныс алдымен құрылғыда сақталады, серверге мүмкін болғанда жіберіледі.
 *
 * The local profile is the source of truth for this phone: a choice is saved
 * here first and sent when it can be. A change that did not reach the server
 * stays marked ([DeviceStateStore.profilePendingSync]) and is retried on the
 * next foreground. Nothing here is ever sent to a server that did not announce
 * [AIFeatures.senderProfile].
 */
class ProfileSync(
    private val service: AccountService,
    private val configuration: ConfigurationRepository,
    private val device: DeviceStateStore,
    private val isSignedIn: () -> Boolean,
    private val features: () -> AIFeatures,
    /** Outlives the screen that made the change. */
    private val scope: CoroutineScope,
    private val preferredLanguage: PreferredLanguageSync? = null
) {

    /** One request at a time, so an older choice can never land after a newer one. */
    private val sending = Mutex()

    /** The user chose [gender]; [GrammaticalGender.UNSPECIFIED] when they skipped. */
    fun setGender(gender: GrammaticalGender) {
        configuration.updateProfile { it.copy(grammaticalGender = gender) }
        device.profilePendingSync = true
        scope.launch { pushPending() }
    }

    /** The user picked another app language in Settings. */
    fun appLanguageChanged() {
        preferredLanguage?.languageChanged()
    }

    /** Sends a change that has not reached the server yet. A no-op when nothing is waiting. */
    suspend fun pushPending() {
        pushGender()
        preferredLanguage?.pushPending()
    }

    private suspend fun pushGender() {
        if (!device.profilePendingSync || !canSend()) return
        sending.withLock {
            val gender = configuration.profile.grammaticalGender
            if (!device.profilePendingSync || gender == null) return
            // Best effort: offline or refused, it stays pending for the next foreground.
            runCatching { service.updateProfile(ProfileUpdate(grammaticalGender = gender.raw)) }
                .onSuccess {
                    // A newer choice made while this one was in flight is still pending.
                    if (configuration.profile.grammaticalGender == gender) device.profilePendingSync = false
                }
        }
    }

    /** The account signed out: its unsent change must not reach the next one ([DeviceStateStore.accountSignedOut]). */
    fun signedOut() {
        device.accountSignedOut()
    }

    /** After `/me`: a choice made on another device of the same account is taken over. */
    fun adopt(server: AccountProfile) {
        val local = configuration.profile.grammaticalGender
        val adopted = adoptedGender(local, server.grammaticalGender, device.profilePendingSync) ?: return
        configuration.updateProfile { it.copy(grammaticalGender = adopted) }
    }

    /** After sign-in or `/me`: [server] is the account's `preferred_language`. */
    fun adoptPreferredLanguage(server: String?) {
        preferredLanguage?.adopt(server)
    }

    /** Onboarding [version] finished on this device. Informational for the server. */
    fun reportOnboardingCompleted(version: Int) {
        if (!canSend()) return
        scope.launch {
            // Best effort: the version that matters is the device-local one.
            runCatching { service.updateProfile(ProfileUpdate(onboardingVersion = version)) }
        }
    }

    private fun canSend(): Boolean = isSignedIn() && features().senderProfile

    companion object {
        /**
         * What a `/me` answer changes locally, or null for nothing.
         *
         * Never while a local change is unsent (this phone's newer choice
         * wins), and never `unspecified`: that is the server's default for
         * every account, not something the user said.
         */
        fun adoptedGender(local: GrammaticalGender?, server: String?, pendingSync: Boolean): GrammaticalGender? {
            if (pendingSync) return null
            val remote = GrammaticalGender.fromRaw(server) ?: return null
            if (!remote.isSpecified || remote == local) return null
            return remote
        }
    }
}
