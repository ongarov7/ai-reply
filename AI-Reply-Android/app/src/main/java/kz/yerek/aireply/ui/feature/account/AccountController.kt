package kz.yerek.aireply.ui.feature.account

import android.app.Activity
import kz.yerek.aireply.ai.AILimits
import androidx.annotation.StringRes
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kz.yerek.aireply.BuildConfig
import kz.yerek.aireply.R
import kz.yerek.aireply.data.account.AccountObserver
import kz.yerek.aireply.data.account.AccountService
import kz.yerek.aireply.data.account.AccountSessionDto
import kz.yerek.aireply.data.account.AccountUsageCache
import kz.yerek.aireply.data.account.AccountUser
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.ApiException
import kz.yerek.aireply.data.account.EmailAddress
import kz.yerek.aireply.data.account.GoogleSignInClient
import kz.yerek.aireply.data.account.LegalConfigDto
import kz.yerek.aireply.data.account.LegalConsentDto
import kz.yerek.aireply.data.account.OtpCode
import kz.yerek.aireply.data.account.PlanDto
import kz.yerek.aireply.data.account.ProfileUpdate
import kz.yerek.aireply.data.account.ServerFeaturesDto
import kz.yerek.aireply.data.account.SessionCredentials
import kz.yerek.aireply.data.account.SubscriptionDto
import kz.yerek.aireply.data.account.UsageDto
import kz.yerek.aireply.data.account.raise
import kz.yerek.aireply.data.legal.LegalConsentStore
import kz.yerek.aireply.data.profile.ProfileSync
import kz.yerek.aireply.keyboard.autocorrect.LearnedWordsStore
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
    private val credentials: SessionCredentials,
    private val usageCache: AccountUsageCache,
    private val legalConsentStore: LegalConsentStore,
    private val google: GoogleSignInClient? = null,
    /** Work that must outlive the screen that started it, such as device registration. */
    private val backgroundScope: CoroutineScope? = null,
    /** Takes over a choice made on another device when the account is read. */
    private val profileSync: ProfileSync? = null,
    /** Drops what was kept for the account that just signed out, such as unsent product events. */
    private val onSignedOut: () -> Unit = {},
    private val clock: () -> Long = System::currentTimeMillis,
    /** Told about sign-in, sign-out, the legal consent and the server's features (the push installation). */
    private val observer: AccountObserver? = null,
    /** The words the keyboard learned; they go with a deleted account. */
    private val learnedWords: LearnedWordsStore? = null
) {

    /**
     * Where the user is in the sign-in flow.
     *
     * Кіру: Google немесе пошта (4 таңбалы код). Телефонмен кіру жоқ.
     */
    sealed interface Phase {
        /** The choice of Google or e-mail. */
        data object SignedOut : Phase
        /** The e-mail step of e-mail sign-in. */
        data object EnteringEmail : Phase
        /** A code went to [email]; a new one may be asked for from [resendAvailableAt] (epoch ms). */
        data class AwaitingCode(
            val email: String,
            val resendAvailableAt: Long
        ) : Phase
        data object SignedIn : Phase
    }

    /** How a sign-in attempt ended, for the screen that started it. */
    sealed interface SignInOutcome {
        data class SignedIn(val isNewUser: Boolean) : SignInOutcome
        /** [clearCode]: the typed code can never work again, so the field empties. */
        data class Failed(val clearCode: Boolean) : SignInOutcome
        data object Cancelled : SignInOutcome
    }

    data class State(
        val phase: Phase = Phase.SignedOut,
        val user: AccountUser? = null,
        val subscription: SubscriptionDto? = null,
        val usage: UsageDto = UsageDto.UNKNOWN,
        val plans: List<PlanDto> = emptyList(),
        /** Null until the server answered; unknown means "offer everything". */
        val features: ServerFeaturesDto? = null,
        /** The server answered `GET /config` (its features may still be null: an old server). */
        val featuresLoaded: Boolean = false,
        /** The address being signed in with, kept so Back and "Change e-mail" do not lose it. */
        val pendingEmail: String = "",
        val legalConfig: LegalConfigDto = LegalConfigDto.PRODUCTION,
        val hasAcceptedLegal: Boolean = false,
        val bootstrapComplete: Boolean = false,
        val busy: Boolean = false,
        /** A string resource, never a server sentence. */
        @StringRes val errorMessage: Int? = null,
        /** A confirmation for the next screen, such as the account having been deleted. */
        @StringRes val notice: Int? = null,
        /** Delete account is on its way; the row shows it, and the other account rows wait. */
        val deletingAccount: Boolean = false,
        /**
         * The last Delete account failed. Kept here rather than on the screen,
         * so it is still shown when Settings comes back.
         */
        val accountDeletionFailed: Boolean = false,
        /** Withdraw AI consent is on its way. */
        val withdrawingConsent: Boolean = false
    ) {
        val isSignedIn: Boolean get() = phase is Phase.SignedIn
        val remainingToday: Int get() = usage.remainingToday
    }

    private val _state = MutableStateFlow(
        State(
            phase = if (credentials.isSignedIn) Phase.SignedIn else Phase.SignedOut,
            // The versions the server published last, so a bump made while
            // the app was away asks again before anything reaches the server.
            legalConfig = legalConsentStore.latestConfig(),
            hasAcceptedLegal = legalConsentStore.hasAcceptedLatest()
        )
    )
    val state: StateFlow<State> = _state.asStateFlow()

    /** The e-mail (or, for an older account, the phone number) shown in Settings. */
    val displayIdentifier: String
        get() = _state.value.user?.identifier?.takeIf(String::isNotEmpty)
            ?: credentials.displayIdentifier.orEmpty()

    /**
     * "Continue with Google" needs this build's OAuth client id and a server
     * that verifies Google tokens. Debug builds show it regardless so the
     * screen can be reviewed before the client id exists; tapping it then says
     * the option is unavailable.
     */
    val offersGoogle: Boolean
        get() {
            val configured = BuildConfig.DEBUG || google?.isConfigured == true
            return configured && (_state.value.features?.googleSignIn ?: true)
        }

    // ------------------------------------------------------------- loading

    /** Sign-in methods and current legal versions, needed before the first screen. */
    suspend fun loadServerConfig() {
        runCatching { service.serverConfig() }.getOrNull()?.let { config ->
            // The administrator's character limits, for the keyboard too.
            AILimits.apply(config)
            val legal = config.legal ?: LegalConfigDto.PRODUCTION
            // The keyboard checks the consent against these versions too.
            legalConsentStore.rememberConfig(legal)
            trackingConsent {
                _state.update {
                    it.copy(
                        features = config.features,
                        featuresLoaded = true,
                        legalConfig = legal,
                        hasAcceptedLegal = legalConsentStore.hasAccepted(legal)
                    )
                }
            }
            observer?.onServerFeatures(config.features)
        }
    }

    suspend fun bootstrap() {
        if (_state.value.bootstrapComplete) return
        loadServerConfig()
        // The refresh also sends an acceptance still waiting for the server.
        if (credentials.isSignedIn) refresh()
        trackingConsent {
            _state.update {
                it.copy(
                    hasAcceptedLegal = legalConsentStore.hasAccepted(it.legalConfig),
                    bootstrapComplete = true
                )
            }
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
            // The account itself went: a deletion whose answer was lost did go through.
            if (deletionMayHaveSucceeded) {
                forgetDeletedAccount()
                return
            }
            // The session ended elsewhere (a refresh token the server revoked).
            val wasSignedIn = _state.value.isSignedIn
            _state.update { it.copy(phase = Phase.SignedOut) }
            if (wasSignedIn) observer?.onSignedOut(userInitiated = false)
            return
        }
        try {
            val account = service.account()
            usageCache.store(account.usage)
            usageCache.storePlanCode(account.subscription.plan.code)
            credentials.displayIdentifier = account.user.identifier
            applyLegalConsent(account.legalConsent)
            profileSync?.adopt(account.profile)
            profileSync?.adoptPreferredLanguage(account.user.preferredLanguage)
            _state.update {
                it.copy(
                    phase = Phase.SignedIn,
                    user = account.user,
                    subscription = account.subscription,
                    usage = account.usage,
                    errorMessage = null
                )
            }
            observer?.onAccountLoaded(account.user.id)
            // Home and Settings refresh on every appearance, so an acceptance
            // the server never got (offline, a timeout) goes again before
            // the keyboard can be refused for it.
            syncPendingLegalConsent()
        } catch (exception: ApiException) {
            when (exception.error) {
                is ApiError.Unauthorized -> sessionEnded()
                is ApiError.AccountDisabled -> {
                    signOutLocally(userInitiated = false)
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

    fun startEmailSignIn() {
        _state.update { it.copy(phase = Phase.EnteringEmail, errorMessage = null) }
    }

    /** Back from the e-mail step to the choice of methods. */
    fun cancelEmailSignIn() {
        _state.update {
            it.copy(
                phase = if (credentials.isSignedIn) Phase.SignedIn else Phase.SignedOut,
                errorMessage = null
            )
        }
    }

    /** "Change e-mail" on the code screen. */
    fun editEmail() {
        _state.update { it.copy(phase = Phase.EnteringEmail, errorMessage = null) }
    }

    /**
     * Sends a code to [email] and moves to code entry. A cooldown refusal also
     * moves there: a code for this address is already on its way.
     */
    suspend fun requestEmailCode(email: String, locale: String) {
        val address = EmailAddress.normalized(email)
        if (!EmailAddress.isPlausible(address)) {
            _state.update { it.copy(errorMessage = R.string.account_error_invalid_email) }
            return
        }
        _state.update { it.copy(busy = true, errorMessage = null, pendingEmail = address) }
        try {
            val challenge = service.requestEmailCode(address, locale)
            awaitCode(address, challenge.resendAfter, errorMessage = null)
        } catch (exception: Throwable) {
            val error = (exception as? ApiException)?.error
            if (error is ApiError.ResendCooldown) {
                awaitCode(address, error.retryAfterSeconds ?: DEFAULT_RESEND_SECONDS, messageFor(exception))
            } else {
                _state.update { it.copy(busy = false, errorMessage = messageFor(exception)) }
            }
        }
    }

    /** "Resend code": a new code to the same address; the old one stops working. */
    suspend fun resendEmailCode(locale: String) {
        val phase = _state.value.phase as? Phase.AwaitingCode ?: return
        _state.update { it.copy(busy = true, errorMessage = null) }
        try {
            val challenge = service.requestEmailCode(phase.email, locale)
            awaitCode(phase.email, challenge.resendAfter, errorMessage = null)
        } catch (exception: Throwable) {
            val error = (exception as? ApiException)?.error
            val retryAfter = (error as? ApiError.ResendCooldown)?.retryAfterSeconds
            _state.update { state ->
                val current = state.phase
                state.copy(
                    busy = false,
                    errorMessage = messageFor(exception),
                    phase = if (retryAfter != null && current is Phase.AwaitingCode) {
                        current.copy(resendAvailableAt = clock() + retryAfter * 1000L)
                    } else {
                        current
                    }
                )
            }
        }
    }

    private fun awaitCode(email: String, resendAfterSeconds: Int, @StringRes errorMessage: Int?) {
        _state.update {
            it.copy(
                phase = Phase.AwaitingCode(email, clock() + resendAfterSeconds.coerceAtLeast(0) * 1000L),
                pendingEmail = email,
                busy = false,
                errorMessage = errorMessage
            )
        }
    }

    /** Checks the code; on success the account is signed in (and created if new). */
    suspend fun verifyEmailCode(code: String): SignInOutcome {
        val phase = _state.value.phase as? Phase.AwaitingCode ?: return SignInOutcome.Cancelled
        val digits = OtpCode.sanitize(code)
        if (digits.length != OtpCode.LENGTH) return SignInOutcome.Failed(clearCode = false)
        _state.update { it.copy(busy = true, errorMessage = null) }
        return try {
            completeSignIn(service.verifyEmailCode(phase.email, digits))
        } catch (exception: Throwable) {
            _state.update { it.copy(busy = false, errorMessage = messageFor(exception)) }
            SignInOutcome.Failed(clearCode = codeIsSpent(exception))
        }
    }

    /** Google's account picker, then the server's check of the token it returned. */
    suspend fun signInWithGoogle(activity: Activity): SignInOutcome {
        val client = google
        if (client == null || !client.isConfigured) {
            _state.update { it.copy(errorMessage = R.string.account_error_provider_unavailable) }
            return SignInOutcome.Failed(clearCode = false)
        }
        _state.update { it.copy(busy = true, errorMessage = null) }
        val token = when (val result = client.requestIdToken(activity)) {
            is GoogleSignInClient.Result.Success -> result
            GoogleSignInClient.Result.Cancelled -> {
                _state.update { it.copy(busy = false) }
                return SignInOutcome.Cancelled
            }
            GoogleSignInClient.Result.NotConfigured, GoogleSignInClient.Result.Unavailable -> {
                _state.update { it.copy(busy = false, errorMessage = R.string.account_error_provider_unavailable) }
                return SignInOutcome.Failed(clearCode = false)
            }
            GoogleSignInClient.Result.Failed -> {
                _state.update { it.copy(busy = false, errorMessage = R.string.account_error_provider_failed) }
                return SignInOutcome.Failed(clearCode = false)
            }
        }
        return try {
            completeSignIn(service.signInWithGoogle(token.idToken, token.nonce))
        } catch (exception: Throwable) {
            _state.update { it.copy(busy = false, errorMessage = messageFor(exception)) }
            SignInOutcome.Failed(clearCode = false)
        }
    }

    /**
     * The session is stored; the screens switch now. Device registration and
     * the consent sync run on the background scope, because the screen that
     * started the sign-in leaves the composition — and cancels its own scope —
     * the moment the phase changes.
     */
    private fun completeSignIn(session: AccountSessionDto): SignInOutcome {
        deletionMayHaveSucceeded = false
        usageCache.store(session.usage)
        usageCache.storePlanCode(session.subscription.plan.code)
        applyLegalConsent(session.legalConsent)
        // Before onboarding asks: a returning user's choice comes back with the session.
        profileSync?.adopt(session.profile)
        profileSync?.adoptPreferredLanguage(session.user.preferredLanguage)
        _state.update {
            it.copy(
                phase = Phase.SignedIn,
                user = session.user,
                subscription = session.subscription,
                usage = session.usage,
                pendingEmail = "",
                busy = false,
                errorMessage = null
            )
        }
        observer?.onSignedIn(session.user.id)
        backgroundScope?.launch {
            // Best effort: a failed device registration must not block sign-in.
            runCatching { service.registerDevice() }
            syncPendingLegalConsent()
        }
        return SignInOutcome.SignedIn(session.isNewUser)
    }

    // --------------------------------------------- adding an e-mail (Settings)

    /**
     * Sends a code to an address the signed-in user wants to sign in with.
     * Returns when the next code may be asked for (epoch ms); throws
     * [ApiException] with the reason otherwise.
     */
    suspend fun requestLinkEmailCode(email: String, locale: String): Long {
        val address = EmailAddress.normalized(email)
        if (!EmailAddress.isPlausible(address)) ApiError.InvalidEmail.raise()
        val challenge = service.requestLinkEmailCode(address, locale)
        return clock() + challenge.resendAfter.coerceAtLeast(0) * 1000L
    }

    /** Proves the address; the account can then be signed in to with it. */
    suspend fun verifyLinkEmailCode(email: String, code: String) {
        val account = service.verifyLinkEmailCode(EmailAddress.normalized(email), OtpCode.sanitize(code))
        credentials.displayIdentifier = account.user.identifier
        _state.update {
            it.copy(user = account.user, subscription = account.subscription, usage = account.usage)
        }
    }

    /**
     * Continue on the consent screen. The upload runs on the background
     * scope, like the one after sign-in: the consent screen leaves the
     * composition - and cancels its own scope - the moment the flag flips.
     */
    suspend fun acceptLegal(locale: String) {
        legalConsentStore.accept(_state.value.legalConfig, locale, BuildConfig.VERSION_NAME)
        trackingConsent { _state.update { it.copy(hasAcceptedLegal = true, notice = null) } }
        backgroundScope?.launch { syncPendingLegalConsent() } ?: syncPendingLegalConsent()
    }

    /**
     * Settings ▸ Withdraw AI consent. The server is told first, so AI requests
     * stop there too; then the consent screen comes back. Offline nothing
     * changes and the reason is returned instead: a string resource, as
     * [messageFor] gives it (not annotated: a suspend function returns an
     * Object to lint).
     */
    suspend fun withdrawLegalConsent(): Int? {
        _state.update { it.copy(busy = true, withdrawingConsent = true) }
        if (credentials.isSignedIn) {
            try {
                service.withdrawLegalConsent()
            } catch (cancellation: CancellationException) {
                _state.update { it.copy(busy = false, withdrawingConsent = false) }
                throw cancellation
            } catch (exception: Throwable) {
                _state.update { it.copy(busy = false, withdrawingConsent = false) }
                return messageFor(exception)
            }
        }
        legalConsentStore.clear()
        _state.update { it.copy(busy = false, withdrawingConsent = false, hasAcceptedLegal = false) }
        return null
    }

    /**
     * An AI request was refused for a missing consent - by the server
     * (CONSENT_REQUIRED: new versions, or withdrawn on another device) or on
     * this phone (the keyboard saw new versions first). The consent screen
     * shows again, for the newest versions known, and accepting there records
     * it on the server. May run on the keyboard's thread.
     */
    fun consentRequired() {
        legalConsentStore.clear()
        legalRecheckPending = true
        _state.update { it.copy(legalConfig = legalConsentStore.latestConfig(), hasAcceptedLegal = false) }
    }

    /**
     * The consent screen appeared after [consentRequired]: the server may have
     * published versions this phone has not seen yet, and accepting older ones
     * would only be refused again. One `GET /config`, then never until the next
     * refusal.
     */
    suspend fun recheckLegalVersions() {
        if (!legalRecheckPending) return
        legalRecheckPending = false
        loadServerConfig()
    }

    @Volatile
    private var legalRecheckPending = false

    /**
     * Settings ▸ Delete account. The server deletes the account, its profile,
     * consents, usage and devices; then this phone forgets everything it kept
     * for it - the session, the consent, the learned words, the quota and the
     * profile the server had a copy of - and shows the consent screen with
     * [R.string.account_deleted]. The keyboard keeps typing.
     *
     * Not cancelled when the screen goes away mid-request: an account deleted
     * on the server must not stay half signed in here.
     *
     * Жауабы жоғалған жою: қайта басқанда 401 келсе, тіркелгі жойылған.
     * A deletion that got no clear answer (a timeout, a dropped connection, a
     * 5xx) may have gone through. If the next try - or the next refresh - is
     * then refused with 401, the account is gone, and the phone forgets it
     * as if the first answer had arrived. Without such an attempt a 401 only
     * means the session ended: the user signs in again, nothing is wiped.
     * The mark lives in memory, so it does not survive the process.
     */
    suspend fun deleteAccount(): Boolean {
        if (!credentials.isSignedIn) return false
        _state.update {
            it.copy(busy = true, errorMessage = null, deletingAccount = true, accountDeletionFailed = false)
        }
        return withContext(NonCancellable) {
            try {
                service.deleteAccount()
            } catch (exception: Throwable) {
                val error = (exception as? ApiException)?.error
                when {
                    error is ApiError.Unauthorized && deletionMayHaveSucceeded -> {
                        forgetDeletedAccount()
                        return@withContext true
                    }
                    error is ApiError.Unauthorized -> {
                        signOutLocally(userInitiated = false)
                        _state.update { it.copy(errorMessage = R.string.account_error_session_expired) }
                    }
                    else -> {
                        if (mayHaveReachedServer(error)) deletionMayHaveSucceeded = true
                        _state.update {
                            it.copy(busy = false, deletingAccount = false, accountDeletionFailed = true)
                        }
                    }
                }
                return@withContext false
            }
            forgetDeletedAccount()
            true
        }
    }

    /** Delete account was tapped again: the last failure is old news. */
    fun clearDeletionFailure() {
        _state.update { it.copy(accountDeletionFailed = false) }
    }

    /**
     * Set by a deletion that got no clear answer; cleared by any sign-in or
     * sign-out. See [deleteAccount].
     */
    @Volatile
    private var deletionMayHaveSucceeded = false

    /**
     * The server deleted the account: the session, the consent, the learned
     * words, the quota and the profile the server had a copy of go too.
     */
    private fun forgetDeletedAccount() {
        // The consent first: the sign-out below must not register this
        // installation again as if the terms were still accepted.
        legalConsentStore.clear()
        _state.update {
            it.copy(
                hasAcceptedLegal = false,
                notice = R.string.account_deleted,
                deletingAccount = false,
                accountDeletionFailed = false
            )
        }
        learnedWords?.clear()
        usageCache.clear()
        profileSync?.accountDeleted()
        signOutLocally(userInitiated = true)
        backgroundScope?.launch { runCatching { google?.signOut() } }
    }

    /** The server refused the session: the account is gone too if a deletion may have reached it. */
    private fun sessionEnded() {
        if (deletionMayHaveSucceeded) forgetDeletedAccount() else signOutLocally(userInitiated = false)
    }

    /**
     * Signs out. The logout request names this installation, so the server
     * detaches it at once; the observer then registers it anonymously.
     */
    suspend fun signOut() {
        _state.update { it.copy(busy = true) }
        service.signOut()
        signOutLocally(userInitiated = true)
        google?.signOut()
        _state.update { it.copy(busy = false) }
    }

    private fun signOutLocally(userInitiated: Boolean) {
        deletionMayHaveSucceeded = false
        credentials.clear()
        profileSync?.signedOut()
        onSignedOut()
        _state.update {
            it.copy(
                phase = Phase.SignedOut,
                user = null,
                subscription = null,
                usage = UsageDto.UNKNOWN,
                plans = emptyList(),
                busy = false,
                deletingAccount = false,
                accountDeletionFailed = false
            )
        }
        observer?.onSignedOut(userInitiated)
    }

    /**
     * Runs a state change and tells the observer if it is the moment the
     * terms became accepted: the installation registration waits for exactly
     * that.
     */
    private inline fun trackingConsent(change: () -> Unit) {
        val before = _state.value.hasAcceptedLegal
        change()
        if (!before && _state.value.hasAcceptedLegal) observer?.onLegalAccepted()
    }

    private fun applyLegalConsent(consent: LegalConsentDto?) {
        val config = _state.value.legalConfig
        if (consent == null || consent.termsVersion != config.termsVersion ||
            consent.privacyVersion != config.privacyVersion
        ) return
        legalConsentStore.restore(consent)
        trackingConsent { _state.update { it.copy(hasAcceptedLegal = true) } }
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
                    onboardingCompleted = true,
                    // The server refuses a field it does not know.
                    preferredLanguage = locale.takeIf { AILimits.features.preferredLanguage }
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
        if (plan.isFree || !plan.purchasable) return false
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
        /** The server's usual wait between codes, for an answer that did not say. */
        const val DEFAULT_RESEND_SECONDS = 32

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
                is ApiError.OtpAlreadyUsed -> R.string.account_error_code_used
                is ApiError.OtpAttemptsExceeded -> R.string.account_error_code_attempts
                is ApiError.ResendCooldown -> R.string.account_error_resend_cooldown
                is ApiError.RateLimited -> R.string.account_error_too_many_attempts
                is ApiError.InvalidEmail -> R.string.account_error_invalid_email
                is ApiError.EmailDeliveryFailed -> R.string.account_error_email_delivery
                is ApiError.EmailInUse -> R.string.account_error_email_in_use
                is ApiError.InvalidIdToken -> R.string.account_error_provider_failed
                is ApiError.AuthProviderUnavailable -> R.string.account_error_provider_unavailable
                is ApiError.Unauthorized -> R.string.account_error_session_expired
                is ApiError.AccountDisabled -> R.string.account_error_disabled
                is ApiError.DailyLimitReached -> R.string.account_error_limit_reached
                is ApiError.MonthlyLimitReached -> R.string.kb_err_quota_monthly
                is ApiError.ConsentRequired -> R.string.kb_err_consent_required
                is ApiError.PaymentRequired, is ApiError.SubscriptionExpired ->
                    R.string.account_error_payment_required
                else -> R.string.account_error_generic
            }
        }

        /**
         * The typed code can never succeed now, so the field is emptied for the
         * next one. A network failure keeps it: the same code may still work.
         */
        fun codeIsSpent(throwable: Throwable): Boolean = when ((throwable as? ApiException)?.error) {
            is ApiError.InvalidOtp, is ApiError.OtpExpired,
            is ApiError.OtpAlreadyUsed, is ApiError.OtpAttemptsExceeded -> true
            else -> false
        }

        /**
         * A failure with no clear answer from the server: the request may have
         * arrived and done its work. Offline it never left the phone.
         */
        fun mayHaveReachedServer(error: ApiError?): Boolean = when (error) {
            is ApiError.TimedOut, is ApiError.Server,
            is ApiError.ProviderTimeout, is ApiError.MalformedResponse -> true
            else -> false
        }
    }
}
