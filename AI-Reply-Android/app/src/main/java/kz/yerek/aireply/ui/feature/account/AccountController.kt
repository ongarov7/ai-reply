package kz.yerek.aireply.ui.feature.account

import kz.yerek.aireply.ai.AILimits
import androidx.annotation.StringRes
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kz.yerek.aireply.BuildConfig
import kz.yerek.aireply.R
import kz.yerek.aireply.data.account.AccountCredentials
import kz.yerek.aireply.data.account.AccountService
import kz.yerek.aireply.data.account.AccountUsageCache
import kz.yerek.aireply.data.account.AccountUser
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.ApiException
import kz.yerek.aireply.data.account.CountryDto
import kz.yerek.aireply.data.account.LegalConfigDto
import kz.yerek.aireply.data.account.LegalConsentDto
import kz.yerek.aireply.data.account.PlanDto
import kz.yerek.aireply.data.account.ProfileUpdate
import kz.yerek.aireply.data.account.SubscriptionDto
import kz.yerek.aireply.data.account.UsageDto
import kz.yerek.aireply.data.legal.LegalConsentStore
import java.util.TimeZone

/**
 * Account state for the app's screens.
 *
 * Тіркелгі күйі: кірген/кірмеген, тариф, күндік квота.
 *
 * WHY NOT A ViewModel. The sign-in flow outlives any one screen — the gate, the
 * code screen and Settings all read the same state — and this project already
 * keeps shared state in the service locator rather than in per-screen
 * ViewModels. It owns no networking ([AccountService] does) and no tokens
 * ([kz.yerek.aireply.data.account.AccountSession] does): what it owns is what
 * the screens render.
 */
class AccountController(
    private val service: AccountService,
    private val credentials: AccountCredentials,
    private val usageCache: AccountUsageCache,
    private val legalConsentStore: LegalConsentStore
) {

    /** Where the user is in the sign-in flow. */
    sealed interface Phase {
        data object SignedOut : Phase
        data class AwaitingCode(
            val identifier: String,
            val masked: String
        ) : Phase
        data object SignedIn : Phase
    }

    data class State(
        val phase: Phase = Phase.SignedOut,
        val user: AccountUser? = null,
        val subscription: SubscriptionDto? = null,
        val usage: UsageDto = UsageDto.UNKNOWN,
        val plans: List<PlanDto> = emptyList(),
        val countries: List<CountryDto> = emptyList(),
        val legalConfig: LegalConfigDto = LegalConfigDto.PRODUCTION,
        val hasAcceptedLegal: Boolean = false,
        val bootstrapComplete: Boolean = false,
        val busy: Boolean = false,
        /** A string resource, never a server sentence. */
        @StringRes val errorMessage: Int? = null
    ) {
        val isSignedIn: Boolean get() = phase is Phase.SignedIn
        val remainingToday: Int get() = usage.remainingToday
    }

    private val _state = MutableStateFlow(
        State(
            phase = if (credentials.isSignedIn) Phase.SignedIn else Phase.SignedOut,
            hasAcceptedLegal = legalConsentStore.hasAccepted(LegalConfigDto.PRODUCTION)
        )
    )
    val state: StateFlow<State> = _state.asStateFlow()

    /** Masked phone or e-mail, for Settings. */
    val displayIdentifier: String
        get() = _state.value.user?.identifier ?: credentials.displayIdentifier.orEmpty()

    // ------------------------------------------------------------- loading

    /** Countries and current legal versions, needed before the first screen. */
    suspend fun loadServerConfig() {
        runCatching { service.serverConfig() }.getOrNull()?.let { config ->
            // The administrator's character limits, for the keyboard too.
            AILimits.apply(config)
            val legal = config.legal ?: LegalConfigDto.PRODUCTION
            _state.update {
                it.copy(
                    countries = config.countries,
                    legalConfig = legal,
                    hasAcceptedLegal = legalConsentStore.hasAccepted(legal)
                )
            }
        }
    }

    suspend fun bootstrap() {
        if (_state.value.bootstrapComplete) return
        loadServerConfig()
        if (credentials.isSignedIn) {
            refresh()
            syncPendingLegalConsent()
        }
        _state.update {
            it.copy(
                hasAcceptedLegal = legalConsentStore.hasAccepted(it.legalConfig),
                bootstrapComplete = true
            )
        }
    }

    suspend fun loadPlans() {
        runCatching { service.plans() }.getOrNull()?.let { plans ->
            _state.update { it.copy(plans = plans) }
        }
    }

    /** Profile, plan and quota in one call. Safe on every appearance. */
    suspend fun refresh() {
        if (!credentials.isSignedIn) {
            _state.update { it.copy(phase = Phase.SignedOut) }
            return
        }
        try {
            val account = service.account()
            usageCache.store(account.usage)
            usageCache.storePlanCode(account.subscription.plan.code)
            credentials.displayIdentifier = account.user.identifier
            applyLegalConsent(account.legalConsent)
            _state.update {
                it.copy(
                    phase = Phase.SignedIn,
                    user = account.user,
                    subscription = account.subscription,
                    usage = account.usage,
                    errorMessage = null
                )
            }
        } catch (exception: ApiException) {
            when (exception.error) {
                is ApiError.Unauthorized -> signOutLocally()
                is ApiError.AccountDisabled -> {
                    signOutLocally()
                    _state.update { it.copy(errorMessage = R.string.account_error_disabled) }
                }
                // A refresh failing offline is no reason to sign anyone out.
                else -> Unit
            }
        }
    }

    suspend fun refreshUsage() {
        if (!credentials.isSignedIn) return
        runCatching { service.usage() }.getOrNull()?.let { usage ->
            usageCache.store(usage)
            _state.update { it.copy(usage = usage) }
        }
    }

    // ------------------------------------------------------------- sign-in

    suspend fun requestCode(identifier: String, locale: String) {
        _state.update { it.copy(busy = true, errorMessage = null) }
        try {
            val challenge = service.requestCode(identifier, locale)
            _state.update {
                it.copy(
                    phase = Phase.AwaitingCode(identifier, challenge.maskedIdentifier),
                    busy = false
                )
            }
        } catch (exception: Throwable) {
            _state.update { it.copy(busy = false, errorMessage = messageFor(exception)) }
        }
    }

    /**
     * Returns true when the account was created just now, so the caller can
     * show the short profile step instead of dropping the user onto Home.
     */
    suspend fun verify(code: String): Boolean {
        val phase = _state.value.phase as? Phase.AwaitingCode ?: return false
        _state.update { it.copy(busy = true, errorMessage = null) }
        return try {
            val session = service.verifyCode(phase.identifier, code)
            usageCache.store(session.usage)
            usageCache.storePlanCode(session.subscription.plan.code)
            applyLegalConsent(session.legalConsent)
            _state.update {
                it.copy(
                    phase = Phase.SignedIn,
                    user = session.user,
                    subscription = session.subscription,
                    usage = session.usage,
                    busy = false
                )
            }
            // Best effort: a failed device registration must not block sign-in.
            runCatching { service.registerDevice() }
            syncPendingLegalConsent()
            session.isNewUser
        } catch (exception: Throwable) {
            _state.update { it.copy(busy = false, errorMessage = messageFor(exception)) }
            false
        }
    }

    suspend fun resendCode(locale: String) {
        val phase = _state.value.phase as? Phase.AwaitingCode ?: return
        requestCode(phase.identifier, locale)
    }

    fun cancelCodeEntry() {
        _state.update {
            it.copy(
                phase = if (credentials.isSignedIn) Phase.SignedIn else Phase.SignedOut,
                errorMessage = null
            )
        }
    }

    suspend fun acceptLegal(locale: String) {
        legalConsentStore.accept(_state.value.legalConfig, locale, BuildConfig.VERSION_NAME)
        _state.update { it.copy(hasAcceptedLegal = true) }
        syncPendingLegalConsent()
    }

    suspend fun signOut() {
        _state.update { it.copy(busy = true) }
        service.signOut()
        signOutLocally()
        _state.update { it.copy(busy = false) }
    }

    private fun signOutLocally() {
        credentials.clear()
        _state.update {
            it.copy(
                phase = Phase.SignedOut,
                user = null,
                subscription = null,
                usage = UsageDto.UNKNOWN,
                plans = emptyList(),
                busy = false
            )
        }
    }

    private fun applyLegalConsent(consent: LegalConsentDto?) {
        val config = _state.value.legalConfig
        if (consent == null || consent.termsVersion != config.termsVersion ||
            consent.privacyVersion != config.privacyVersion
        ) return
        legalConsentStore.restore(consent)
        _state.update { it.copy(hasAcceptedLegal = true) }
    }

    private suspend fun syncPendingLegalConsent() {
        if (!credentials.isSignedIn) return
        val record = legalConsentStore.current() ?: return
        val config = _state.value.legalConfig
        if (!record.pendingSync || record.termsVersion != config.termsVersion ||
            record.privacyVersion != config.privacyVersion
        ) return
        runCatching { service.recordLegalConsent(record) }
            .getOrNull()
            ?.let(::applyLegalConsent)
    }

    // ------------------------------------------------------------- profile

    /** Sends the minimal registration answers and marks onboarding complete. */
    suspend fun completeRegistration(
        role: String,
        description: String,
        tone: String,
        locale: String
    ): Boolean {
        _state.update { it.copy(busy = true, errorMessage = null) }
        return try {
            service.updateProfile(
                ProfileUpdate(
                    role = role,
                    description = description,
                    preferredTone = tone,
                    locale = locale,
                    timezone = TimeZone.getDefault().id,
                    onboardingCompleted = true
                )
            )
            _state.update { it.copy(busy = false) }
            refresh()
            true
        } catch (exception: Throwable) {
            _state.update { it.copy(busy = false, errorMessage = messageFor(exception)) }
            false
        }
    }

    // --------------------------------------------------------------- plans

    /**
     * Demo checkout: create a payment and confirm it in one step. With a real
     * acquirer this becomes create → redirect → webhook, and only this method
     * changes.
     */
    suspend fun choosePlan(plan: PlanDto): Boolean {
        if (plan.isFree) return false
        _state.update { it.copy(busy = true, errorMessage = null) }
        return try {
            val checkout = service.startCheckout(plan.id)
            val result = service.confirmCheckout(checkout.paymentId)
            usageCache.store(result.usage)
            usageCache.storePlanCode(result.subscription.plan.code)
            _state.update {
                it.copy(
                    busy = false,
                    subscription = result.subscription,
                    usage = result.usage
                )
            }
            true
        } catch (exception: Throwable) {
            _state.update { it.copy(busy = false, errorMessage = messageFor(exception)) }
            false
        }
    }

    companion object {
        /**
         * Maps a failure onto a string resource. The server's English message is
         * never shown to a user.
         */
        @StringRes
        fun messageFor(throwable: Throwable): Int {
            val error = (throwable as? ApiException)?.error ?: return R.string.account_error_generic
            return when (error) {
                is ApiError.Offline -> R.string.error_offline
                is ApiError.TimedOut -> R.string.error_timed_out
                is ApiError.InvalidOtp -> R.string.account_error_invalid_code
                is ApiError.OtpExpired -> R.string.account_error_code_expired
                is ApiError.RateLimited -> R.string.account_error_too_many_attempts
                is ApiError.Unauthorized -> R.string.account_error_session_expired
                is ApiError.AccountDisabled -> R.string.account_error_disabled
                is ApiError.InvalidRequest -> R.string.account_error_invalid_number
                is ApiError.DailyLimitReached -> R.string.account_error_limit_reached
                is ApiError.PaymentRequired, is ApiError.SubscriptionExpired ->
                    R.string.account_error_payment_required
                else -> R.string.account_error_generic
            }
        }
    }
}
