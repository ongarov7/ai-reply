package kz.yerek.aireply.data.account

import kz.yerek.aireply.data.secure.SecureCredentialStore
import kz.yerek.aireply.data.settings.SettingsStore
import java.util.UUID

/**
 * The session as stored on the device: tokens in the Keystore, everything
 * non-secret in settings.
 *
 * Токендер — шифрланған қоймада, қалғаны — қарапайым баптауларда.
 *
 * The split is the whole point. A token is a bearer credential and belongs in
 * the encrypted store the keyboard can also read; an expiry timestamp and a
 * masked phone number are not secrets and belong where a screen can read them
 * synchronously while it draws.
 */
class AccountCredentials(
    private val secure: SecureCredentialStore,
    private val settings: SettingsStore
) : SessionCredentials {

    override val accessToken: String? get() = secure.accessToken()
    override val refreshToken: String? get() = secure.refreshToken()

    /** True when a session exists at all — checked before every network call. */
    override val isSignedIn: Boolean get() = refreshToken != null

    /**
     * Whether the access token is still comfortably valid.
     *
     * A minute of headroom, because a token that expires while the request is
     * in flight costs the user a visible retry.
     */
    override val isAccessTokenFresh: Boolean
        get() {
            if (accessToken == null) return false
            val expiry = settings.accessTokenExpiry
            return expiry > 0 && expiry - System.currentTimeMillis() > FRESHNESS_MARGIN_MS
        }

    /** Stores a freshly issued pair. Called from one place only. */
    override fun store(accessToken: String, refreshToken: String, expiresInSeconds: Int) {
        secure.setAccessToken(accessToken)
        secure.setRefreshToken(refreshToken)
        settings.accessTokenExpiry =
            System.currentTimeMillis() + maxOf(expiresInSeconds, 60) * 1000L
    }

    override fun clear() {
        secure.clearAccountTokens()
        settings.clearAccountState()
    }

    /** Random per-install id, replaced when the server returns a canonical one. */
    override var deviceId: String
        get() {
            settings.accountDeviceId?.let { return it }
            return UUID.randomUUID().toString().also { settings.accountDeviceId = it }
        }
        set(value) {
            if (value.isNotEmpty()) settings.accountDeviceId = value
        }

    /** Masked phone or e-mail, shown so the user knows which account they are on. */
    override var displayIdentifier: String?
        get() = settings.accountIdentifier
        set(value) {
            settings.accountIdentifier = value
        }

    private companion object {
        const val FRESHNESS_MARGIN_MS = 60_000L
    }
}

/**
 * Last known quota, shared by the app and the keyboard.
 *
 * Көрсету үшін ғана: шешімді әрқашан сервер қабылдайды.
 */
class AccountUsageCache(private val settings: SettingsStore) {

    data class Snapshot(
        val dailyLimit: Int,
        val usedToday: Int,
        val remainingToday: Int,
        val planCode: String
    ) {
        val isKnown: Boolean get() = dailyLimit > 0
        val isExhausted: Boolean get() = dailyLimit > 0 && remainingToday <= 0
    }

    fun store(usage: UsageDto) {
        settings.cachedDailyLimit = usage.dailyLimit
        settings.cachedUsedToday = usage.usedToday
        settings.cachedRemainingToday = usage.remainingToday
    }

    fun storePlanCode(code: String) {
        settings.cachedPlanCode = code
    }

    fun current(): Snapshot = Snapshot(
        dailyLimit = settings.cachedDailyLimit,
        usedToday = settings.cachedUsedToday,
        remainingToday = settings.cachedRemainingToday,
        planCode = settings.cachedPlanCode.orEmpty()
    )
}
