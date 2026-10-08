package kz.yerek.aireply.push

import android.content.Context
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kz.yerek.aireply.BuildConfig
import kz.yerek.aireply.R
import kz.yerek.aireply.data.account.AccountObserver
import kz.yerek.aireply.data.account.ServerFeaturesDto
import kz.yerek.aireply.data.account.SessionAuth
import kz.yerek.aireply.data.settings.SettingsStore
import kz.yerek.aireply.platform.ReplyLog
import java.util.UUID
import java.util.concurrent.atomic.AtomicBoolean

/**
 * What the screens show about notifications. Pure data; [showsPrompt] and
 * [isAvailable] are the rules the Home card and Settings follow.
 */
data class PushUiState(
    /** Firebase is configured in this build (google-services.json was present). */
    val supportedInBuild: Boolean = false,
    /** The server can deliver pushes (`features.push_notifications`). */
    val serverDelivers: Boolean = false,
    /** The server has installations and notification preferences (`features.installations`). */
    val serverHasInstallations: Boolean = false,
    /** Android 13+: POST_NOTIFICATIONS is a runtime permission. */
    val runtimePermission: Boolean = false,
    /** The runtime permission is held (always true before Android 13). */
    val granted: Boolean = false,
    /** Notifications would actually show: permission held and not switched off in system settings. */
    val canPost: Boolean = false,
    /** authorized | denied | not_determined | unknown, as registered. */
    val permission: String = NotificationPermission.UNKNOWN,
    val permanentlyDenied: Boolean = false,
    val promptDismissed: Boolean = false,
    /** The in-app switch. */
    val notificationsEnabled: Boolean = true,
    /** Debug builds: show the card and the controls regardless. */
    val debugForced: Boolean = false
) {
    /** Push can work end to end: this build has Firebase and the server can send. */
    val isAvailable: Boolean get() = debugForced || (supportedInBuild && serverDelivers)

    /**
     * The Home card: never at first launch, only after sign-in (Home comes
     * after it), only where the system dialog can still be shown, never while
     * the in-app switch is off, and never again after "Not now" or a denial.
     */
    val showsPrompt: Boolean
        get() = debugForced || (
            supportedInBuild && serverDelivers && runtimePermission && !granted &&
                !permanentlyDenied && !promptDismissed && notificationsEnabled
            )

    /** The system dialog can still be shown; otherwise only system settings can help. */
    val canAskSystem: Boolean get() = runtimePermission && !granted && !permanentlyDenied
}

/**
 * Push notifications and the installation registration, in one place the
 * Activity, the screens and the Firebase service talk to.
 *
 * Push хабарламалары: орнатуды тіркеу, рұқсат, арналар, басылған хабарлама.
 *
 * Every entry point returns at once; network work runs on the application
 * scope and never throws into the caller.
 */
class PushCoordinator(
    private val appContext: Context,
    private val settings: SettingsStore,
    private val store: PushStateStore,
    private val client: ClientContext,
    private val session: SessionAuth,
    private val api: PushApi,
    /** `features` from `GET /api/v1/config`; null before the answer or on an old server. */
    private val features: () -> ServerFeaturesDto?,
    /** Whether `GET /api/v1/config` has answered in this process. */
    private val featuresLoaded: () -> Boolean,
    /** Whether the app's own start-up (which loads the config) has run. */
    private val bootstrapped: () -> Boolean,
    /** The terms are accepted. Before that nothing is registered, fetched or sent. */
    private val consentGiven: () -> Boolean,
    private val loadServerConfig: suspend () -> Unit,
    /** A Context in the app's interface language, for channel names and texts. */
    private val localized: () -> Context,
    private val scope: CoroutineScope,
    private val clock: () -> Long = System::currentTimeMillis
) : AccountObserver {

    val preferences = NotificationPreferencesRepository(api)

    private val tokenRenewal = InvalidTokenRenewal()

    val registrar = InstallationRegistrar(
        api = api,
        session = session,
        store = store,
        snapshot = ::snapshot,
        isEnabled = { consentGiven() && features()?.installations == true },
        scope = scope,
        clock = clock,
        listener = { request, response ->
            // A signed-in registration answers with the account's categories: the switches follow.
            response.preferences?.let(preferences::adopt)
            if (tokenRenewal.shouldRenew(request, response)) renewToken(request.push?.token)
        }
    )

    /** Firebase is configured in this build. Read once: it cannot change while the process lives. */
    val isSupportedInBuild: Boolean by lazy { PushSupport.isAvailable(appContext) }

    private val _ui = MutableStateFlow(PushUiState())
    val ui: StateFlow<PushUiState> = _ui.asStateFlow()

    @Volatile
    private var tokenFetched = false

    @Volatile
    private var lastConfigAttempt = 0L

    /** The language the channels were last named in. */
    @Volatile
    private var channelsLanguage: String? = null

    // --------------------------------------------------------------- app

    /** MainActivity was created (a launch, or a re-creation after a language change). */
    fun onAppLaunched() {
        refreshUi()
        scope.launch {
            ensureChannels()
            // Firebase is told about this install only once the terms are accepted.
            if (isSupportedInBuild && !tokenFetched && consentGiven()) fetchToken()
            registrar.requestSync()
        }
    }

    /**
     * The app came back to the front. The permission may have changed in
     * system settings, the language or the time zone may have too, and a day
     * may have passed: the registrar sends only if something did.
     */
    fun onResume() {
        // Allowed in system settings after a permanent denial: the app may ask again one day.
        if (NotificationPermission.isGranted(appContext) && store.permissionPermanentlyDenied) {
            store.permissionPermanentlyDenied = false
        }
        refreshUi()
        scope.launch {
            ensureChannels()
            if (bootstrapped()) ensureServerFeatures()
            registrar.requestSync()
        }
    }

    fun refreshUi() {
        _ui.value = readUi()
    }

    private fun readUi(): PushUiState {
        val features = features()
        return PushUiState(
            supportedInBuild = isSupportedInBuild,
            serverDelivers = features?.pushNotifications == true,
            serverHasInstallations = features?.installations == true,
            runtimePermission = NotificationPermission.isRuntimePermission,
            granted = NotificationPermission.isGranted(appContext),
            canPost = NotificationPermission.canPost(appContext),
            permission = NotificationPermission.status(appContext, store),
            permanentlyDenied = store.permissionPermanentlyDenied,
            promptDismissed = store.promptDismissed,
            notificationsEnabled = settings.notificationsEnabled,
            debugForced = BuildConfig.DEBUG && settings.debugForcePushPrompt
        )
    }

    /**
     * Channel names follow the app language. Re-creating an existing channel
     * only renames it, so this runs whenever the language may have changed.
     */
    private fun ensureChannels() {
        val language = client.locale
        if (channelsLanguage == language) return
        NotificationChannels.ensure(appContext, localized())
        channelsLanguage = language
    }

    // ------------------------------------------------------------- token

    /** Firebase issued a new registration token. May run with no screen open. */
    fun onNewToken(token: String) {
        if (token.isBlank()) return
        store.fcmToken = token
        scope.launch {
            ensureServerFeatures()
            registrar.requestSync()
        }
    }

    private suspend fun fetchToken() {
        when (val result = PushSupport.fetchToken(appContext)) {
            is PushSupport.TokenResult.Token -> {
                tokenFetched = true
                if (store.fcmToken != result.value) store.fcmToken = result.value
            }
            // Logged by PushSupport; the next launch, or onNewToken, tries again.
            PushSupport.TokenResult.Failed, PushSupport.TokenResult.Unsupported -> Unit
        }
    }

    /**
     * The server found the token it was sent unregistered at FCM: it is
     * deleted and a new one asked for, and the next sync sends that one. The
     * invalid one is never sent again, even if no new one comes now
     * ([onNewToken] brings it later).
     */
    private fun renewToken(invalid: String?) {
        if (!isSupportedInBuild) return
        scope.launch {
            if (invalid != null && store.fcmToken == invalid) store.fcmToken = null
            when (val result = PushSupport.renewToken(appContext)) {
                is PushSupport.TokenResult.Token -> {
                    tokenFetched = true
                    store.fcmToken = result.value
                }
                PushSupport.TokenResult.Failed, PushSupport.TokenResult.Unsupported -> Unit
            }
            registrar.requestSync()
        }
    }

    /** The config is fetched by the app's start-up; this covers a failed or skipped one. */
    private suspend fun ensureServerFeatures() {
        if (featuresLoaded()) return
        val now = clock()
        if (now - lastConfigAttempt < CONFIG_RETRY_MS) return
        lastConfigAttempt = now
        try {
            loadServerConfig()
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (failure: Exception) {
            ReplyLog.warn(failure) { "server config not loaded for push" }
        }
    }

    // ---------------------------------------------------------- messages

    /**
     * A push arrived while the app is in the foreground (Firebase called us
     * instead of showing it). Shown exactly like the system would, unless the
     * in-app switch is off. The debug "simulate push" comes through here too.
     */
    fun onMessageReceived(data: Map<String, String>, title: String?, body: String?): Boolean {
        val payload = PushPayload.from(data) ?: return false
        if (!settings.notificationsEnabled) return false
        return PushNotifier.show(appContext, localized(), payload, data, title, body)
    }

    /**
     * A notification was tapped: [data] are its String extras. Tells the
     * server (best effort) and returns where to go, or null when the intent
     * was not a notification of ours.
     */
    fun onNotificationOpened(data: Map<String, String?>): AppLink? {
        val payload = PushPayload.from(data) ?: return null
        if (consentGiven()) {
            scope.launch {
                // Read here, off the main thread: the first read of the id touches a file.
                val request = payload.openedRequest(client.installationId) ?: return@launch
                try {
                    api.opened(request)
                } catch (cancelled: CancellationException) {
                    throw cancelled
                } catch (failure: Exception) {
                    ReplyLog.warn(failure) { "notification open not recorded" }
                }
            }
        }
        return payload.link
    }

    /**
     * DEBUG BUILDS ONLY: the foreground path, with a sample payload, after
     * [delayMillis]. It has no delivery id, so a tap records nothing.
     */
    fun simulatePush(delayMillis: Long) {
        if (!BuildConfig.DEBUG) return
        scope.launch {
            delay(delayMillis)
            val strings = localized()
            onMessageReceived(
                data = mapOf(
                    PushPayload.KEY_NOTIFICATION_ID to UUID.randomUUID().toString(),
                    PushPayload.KEY_TYPE to "debug_sample",
                    PushPayload.KEY_CATEGORY to "subscription",
                    PushPayload.KEY_LINK to "aireply://subscription"
                ),
                title = strings.getString(R.string.debug_push_title),
                body = strings.getString(R.string.debug_push_body)
            )
        }
    }

    // -------------------------------------------------------- permission

    /** The system dialog is about to be shown. */
    fun onPermissionRequested() {
        store.permissionRequested = true
    }

    /**
     * The dialog answered. [rationaleBefore] / [rationaleAfter] are
     * `shouldShowRequestPermissionRationale` just before the request and
     * right after it (see [NotificationPermission.isPermanentDenial]: a
     * dismissed dialog is not a permanent denial). [fromPrompt]: it was the
     * Home card, which then stays hidden either way.
     */
    fun onPermissionResult(granted: Boolean, rationaleBefore: Boolean, rationaleAfter: Boolean, fromPrompt: Boolean) {
        if (granted) {
            store.permissionPermanentlyDenied = false
        } else {
            val permanent = NotificationPermission.isPermanentDenial(
                granted = false,
                rationaleBefore = rationaleBefore,
                rationaleAfter = rationaleAfter,
                deniedBefore = store.permissionDeniedOnce
            )
            if (permanent) store.permissionPermanentlyDenied = true
            if (rationaleAfter) store.permissionDeniedOnce = true
            if (fromPrompt) store.promptDismissed = true
        }
        refreshUi()
        registrar.requestSync()
    }

    /** "Not now" on the Home card. */
    fun dismissPrompt() {
        store.promptDismissed = true
        refreshUi()
    }

    // ---------------------------------------------------------- settings

    fun setNotificationsEnabled(enabled: Boolean) {
        settings.notificationsEnabled = enabled
        refreshUi()
        registrar.requestSync()
    }

    /** DEBUG BUILDS ONLY. */
    fun setDebugForcePrompt(enabled: Boolean) {
        if (!BuildConfig.DEBUG) return
        settings.debugForcePushPrompt = enabled
        if (enabled) store.promptDismissed = false
        refreshUi()
    }

    // ----------------------------------------------------------- account

    /** The terms were just accepted: fetch the token and register. */
    override fun onLegalAccepted() {
        refreshUi()
        scope.launch {
            if (isSupportedInBuild && !tokenFetched) fetchToken()
            registrar.requestSync()
        }
    }

    override fun onServerFeatures(features: ServerFeaturesDto?) {
        refreshUi()
        registrar.requestSync()
    }

    override fun onSignedIn(userId: String) {
        store.accountUserId = userId
        preferences.clear()
        registrar.requestSync()
    }

    override fun onAccountLoaded(userId: String) {
        if (store.accountUserId == userId) return
        store.accountUserId = userId
        registrar.requestSync()
    }

    /**
     * The logout request already named this installation; registering it
     * anonymously now makes the detachment certain even if that request never
     * arrived.
     */
    override fun onSignedOut(userInitiated: Boolean) {
        store.accountUserId = null
        preferences.clear()
        registrar.requestSync()
    }

    // ------------------------------------------------------ registration

    private fun snapshot(): InstallationRequest = InstallationRequest(
        installationId = client.installationId,
        platform = client.platform,
        appVersion = client.appVersion,
        appBuild = client.appBuild,
        osName = client.osName,
        osVersion = client.osVersion,
        deviceModel = client.deviceModel,
        manufacturer = client.manufacturer,
        locale = client.locale,
        timezone = client.timezone,
        notificationPermission = NotificationPermission.status(appContext, store),
        notificationsEnabled = settings.notificationsEnabled,
        push = store.fcmToken?.takeIf { isSupportedInBuild }?.let { PushTokenDto(PROVIDER, it) }
    )

    companion object {
        const val PROVIDER = "fcm"
        private const val CONFIG_RETRY_MS = 5L * 60 * 1000
    }
}

/**
 * Whether an installation answer calls for a new FCM token: the token that
 * was sent is reported `invalid`. At most once per process, so a phone whose
 * new token is refused as well does not loop.
 *
 * Сервер токенді жарамсыз деді: процесс сайын бір рет қана жаңасы алынады.
 */
class InvalidTokenRenewal {
    private val used = AtomicBoolean(false)

    fun shouldRenew(sent: InstallationRequest, response: InstallationResponse): Boolean =
        sent.push != null && response.pushStatus == STATUS_INVALID && used.compareAndSet(false, true)

    companion object {
        /** `push_status` for a token FCM no longer delivers to. */
        const val STATUS_INVALID = "invalid"
    }
}
