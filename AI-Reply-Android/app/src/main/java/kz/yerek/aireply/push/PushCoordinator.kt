package kz.yerek.aireply.push

import android.content.Context
import android.os.Handler
import android.os.Looper
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kz.yerek.aireply.BuildConfig
import kz.yerek.aireply.R
import kz.yerek.aireply.data.account.AccountObserver
import kz.yerek.aireply.data.account.ApiException
import kz.yerek.aireply.data.account.ServerFeaturesDto
import kz.yerek.aireply.data.account.SessionAuth
import kz.yerek.aireply.data.account.diagnosticCode
import kz.yerek.aireply.data.settings.SettingsStore
import kz.yerek.aireply.telemetry.EventReporter
import java.util.UUID

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
    val shareDiagnostics: Boolean = true,
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
 * Push notifications, the installation registration and the diagnostics that
 * go with them, in one place the Activity, the screens and the Firebase
 * service talk to.
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
    private val api: HttpPushApi,
    private val events: EventReporter,
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

    val registrar = InstallationRegistrar(
        api = api,
        session = session,
        store = store,
        snapshot = ::snapshot,
        isEnabled = { consentGiven() && features()?.installations == true },
        scope = scope,
        clock = clock,
        listener = object : InstallationRegistrar.Listener {
            override fun onSynced(request: InstallationRequest, response: InstallationResponse) =
                onRegistered(request, response)

            override fun onFailed(request: InstallationRequest, failure: ApiException?) =
                onRegistrationFailed(request, failure)
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

    private val reportedTokenFailures = HashSet<String>()

    private val mainHandler = Handler(Looper.getMainLooper())

    // --------------------------------------------------------------- app

    /** MainActivity was created (a launch, or a re-creation after a language change). */
    fun onAppLaunched() {
        refreshUi()
        scope.launch {
            NotificationChannels.ensure(appContext, localized())
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
            shareDiagnostics = settings.shareDiagnostics,
            debugForced = BuildConfig.DEBUG && settings.debugForcePushPrompt
        )
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
            PushSupport.TokenResult.Failed -> reportTokenFailure("fcm", FCM_TOKEN_UNAVAILABLE, null)
            PushSupport.TokenResult.Unsupported -> Unit
        }
    }

    /** The config is fetched by the app's start-up; this covers a failed or skipped one. */
    private suspend fun ensureServerFeatures() {
        if (featuresLoaded()) return
        val now = clock()
        if (now - lastConfigAttempt < CONFIG_RETRY_MS) return
        lastConfigAttempt = now
        runCatching { loadServerConfig() }
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
     * A notification was tapped: [data] are its String extras. Records
     * `notification_opened` and returns where to go, or null when the intent
     * was not a notification of ours.
     */
    fun onNotificationOpened(data: Map<String, String?>): AppLink? {
        val payload = PushPayload.from(data) ?: return null
        // Posted: on a cold start this runs in onCreate, before the session
        // the tap opens has begun (it starts in onStart).
        payload.openedEventProperties()?.let { properties ->
            mainHandler.post { events.notificationOpened(properties) }
        }
        return payload.link
    }

    /** DEBUG BUILDS ONLY: the foreground path, with a sample payload, after [delayMillis]. */
    fun simulatePush(delayMillis: Long) {
        if (!BuildConfig.DEBUG) return
        scope.launch {
            delay(delayMillis)
            val strings = localized()
            onMessageReceived(
                data = mapOf(
                    PushPayload.KEY_NOTIFICATION_ID to UUID.randomUUID().toString(),
                    PushPayload.KEY_DELIVERY_ID to "debug-" + UUID.randomUUID().toString().take(8),
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
        events.pushPermissionRequested()
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
            events.pushPermissionGranted()
        } else {
            events.pushPermissionDenied()
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

    fun setShareDiagnostics(enabled: Boolean) {
        settings.shareDiagnostics = enabled
        if (!enabled) events.clear()
        refreshUi()
    }

    /** DEBUG BUILDS ONLY. */
    fun setDebugForcePrompt(enabled: Boolean) {
        if (!BuildConfig.DEBUG) return
        settings.debugForcePushPrompt = enabled
        if (enabled) store.promptDismissed = false
        refreshUi()
    }

    // ----------------------------------------------------------- account

    /** The terms were just accepted: register, fetch the token, send what diagnostics hold. */
    override fun onLegalAccepted() {
        refreshUi()
        scope.launch {
            if (isSupportedInBuild && !tokenFetched) fetchToken()
            registrar.requestSync()
        }
        events.onConsentGiven()
    }

    override fun onServerFeatures(features: ServerFeaturesDto?) {
        refreshUi()
        events.onServerFeaturesChanged()
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
        if (userInitiated) events.logout()
        registrar.requestSync()
    }

    override fun onSignInFailedLocally(method: String, errorCode: String) {
        events.loginFailed(method, errorCode)
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

    private fun onRegistered(request: InstallationRequest, response: InstallationResponse) {
        val token = request.push?.token
        val previous = store.registeredToken
        if (token == null) {
            store.registeredToken = null
        } else if (response.pushStatus == PUSH_ACTIVE && token != previous) {
            if (previous == null) events.pushTokenRegistered() else events.pushTokenRefreshed()
            store.registeredToken = token
        }
        response.preferences?.let(preferences::adopt)
    }

    private fun onRegistrationFailed(request: InstallationRequest, failure: ApiException?) {
        val token = request.push?.token ?: return
        if (token == store.registeredToken) return
        reportTokenFailure(token, failure?.diagnosticCode() ?: "unknown", failure?.requestId)
    }

    /** Once per token (or per kind of failure) per process: retries must not repeat it. */
    private fun reportTokenFailure(key: String, code: String, requestId: String?) {
        synchronized(reportedTokenFailures) {
            if (!reportedTokenFailures.add(key)) return
        }
        events.pushTokenRegistrationFailed(code, requestId)
    }

    companion object {
        const val PROVIDER = "fcm"
        private const val PUSH_ACTIVE = "active"
        private const val FCM_TOKEN_UNAVAILABLE = "fcm_token_unavailable"
        private const val CONFIG_RETRY_MS = 5L * 60 * 1000
    }
}
