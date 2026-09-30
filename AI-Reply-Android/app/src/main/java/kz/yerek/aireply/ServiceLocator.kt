package kz.yerek.aireply

import android.content.Context
import android.os.Build
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.Dispatchers
import kz.yerek.aireply.ai.AIConfiguration
import kz.yerek.aireply.ai.AIReplyService
import kz.yerek.aireply.ai.AccountReplyTransport
import kz.yerek.aireply.ai.AppStrings
import kz.yerek.aireply.ai.DebugReplyMock
import kz.yerek.aireply.ai.ReplyPromptBuilder
import kz.yerek.aireply.ai.ReplyDraftNormalizer
import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.core.lang.LocalizedContext
import kz.yerek.aireply.core.lang.TemplateNaming
import kz.yerek.aireply.data.account.AccountCredentials
import kz.yerek.aireply.data.account.AccountObserver
import kz.yerek.aireply.data.account.AccountService
import kz.yerek.aireply.data.account.AccountSession
import kz.yerek.aireply.data.account.AccountUsageCache
import kz.yerek.aireply.data.account.ApiClient
import kz.yerek.aireply.data.account.DeviceDescriptor
import kz.yerek.aireply.data.account.GoogleSignInClient
import kz.yerek.aireply.data.account.RequestMetadata
import kz.yerek.aireply.data.account.ServerFeaturesDto
import kz.yerek.aireply.data.legal.LegalConsentStore
import kz.yerek.aireply.data.profile.ConfigurationRepository
import kz.yerek.aireply.data.profile.ProfileStore
import kz.yerek.aireply.data.secure.SecureCredentialStore
import kz.yerek.aireply.data.settings.SettingsStore
import kz.yerek.aireply.push.ClientContext
import kz.yerek.aireply.push.HttpPushApi
import kz.yerek.aireply.push.InstallationIdStore
import kz.yerek.aireply.push.PushCoordinator
import kz.yerek.aireply.push.PushStateStore
import kz.yerek.aireply.telemetry.EventReporter
import kz.yerek.aireply.telemetry.HttpEventsApi
import kz.yerek.aireply.telemetry.SessionTracker
import kz.yerek.aireply.ui.feature.account.AccountController
import kz.yerek.aireply.ui.navigation.PendingNavigation
import java.util.TimeZone

/**
 * The object graph, by hand.
 *
 * WHY NOT Hilt. Half of this app is an input method, and an input method is
 * started at the moment the user has just switched keyboards and is looking at
 * a blank strip waiting for keys. Hilt would add a generated component, a
 * reflective entry point and several hundred classes to the critical path in
 * order to construct seven objects, five of which are lazy anyway. The iOS
 * project uses plain singletons for the same reason.
 *
 * Everything here holds the APPLICATION context. Nothing holds an Activity, a
 * Service or a View, so nothing here can leak one.
 */
class ServiceLocator(context: Context) {

    private val appContext: Context = context.applicationContext

    /**
     * Application-scoped work that must outlive any one screen or keyboard
     * appearance — currently only refreshing the chip-row cache after a save.
     */
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)

    val settings: SettingsStore by lazy { SettingsStore(appContext) }

    val credentials: SecureCredentialStore by lazy { SecureCredentialStore(appContext) }

    init {
        settings.migrateToBackendOnly()
        credentials.removeLegacySecrets()
        // Every ApiClient takes its metadata headers from here, and reports
        // app requests that got no answer at all to diagnostics.
        RequestMetadata.installed = RequestMetadata { headerScope -> clientContext.headers(headerScope) }
        RequestMetadata.transportFailureObserver = { report -> events.apiError(report) }
    }

    private val profileStore: ProfileStore by lazy { ProfileStore(appContext) }

    val configuration: ConfigurationRepository by lazy {
        ConfigurationRepository(appContext, profileStore, settings, scope)
    }

    val aiConfiguration: AIConfiguration by lazy { AIConfiguration { accountCredentials.isSignedIn } }

    // ------------------------------------------------------------- account

    val accountCredentials: AccountCredentials by lazy {
        AccountCredentials(credentials, settings)
    }

    val usageCache: AccountUsageCache by lazy { AccountUsageCache(settings) }

    val legalConsentStore: LegalConsentStore by lazy { LegalConsentStore(appContext) }

    /**
     * One session object for the whole process: the app screens and the input
     * method share it, so a token is never refreshed twice at once.
     */
    val accountSession: AccountSession by lazy {
        AccountSession(
            credentials = accountCredentials,
            baseUrlProvider = { aiConfiguration.backendBaseUrl },
            deviceDescriptor = ::deviceDescriptor
        )
    }

    /**
     * "Continue with Google". The OAuth web client id is baked in at build time
     * (see app/build.gradle.kts); an empty one keeps the option hidden.
     */
    val googleSignIn: GoogleSignInClient by lazy {
        GoogleSignInClient(appContext, BuildConfig.GOOGLE_WEB_CLIENT_ID)
    }

    /** UI-facing account state; one instance for the app and the keyboard. */
    val account: AccountController by lazy {
        AccountController(
            service = accountService,
            credentials = accountCredentials,
            usageCache = usageCache,
            legalConsentStore = legalConsentStore,
            google = googleSignIn,
            backgroundScope = scope,
            observer = accountObserver
        )
    }

    /** Hands account transitions to [push] without constructing it before it is needed. */
    private val accountObserver = object : AccountObserver {
        override fun onServerFeatures(features: ServerFeaturesDto?) = push.onServerFeatures(features)
        override fun onSignedIn(userId: String) = push.onSignedIn(userId)
        override fun onAccountLoaded(userId: String) = push.onAccountLoaded(userId)
        override fun onSignedOut(userInitiated: Boolean) = push.onSignedOut(userInitiated)
        override fun onSignInFailedLocally(method: String, errorCode: String) =
            push.onSignInFailedLocally(method, errorCode)
    }

    val accountService: AccountService by lazy {
        AccountService(
            session = accountSession,
            baseUrlProvider = { aiConfiguration.backendBaseUrl },
            deviceDescriptor = ::deviceDescriptor
        )
    }

    // ------------------------------------------- push, installation, diagnostics

    /**
     * Installation id, versions, OS, model, language and time zone — the
     * metadata headers of every request and the body of the installation
     * registration. No hardware identifier.
     */
    val clientContext: ClientContext by lazy {
        ClientContext(
            installationIds = InstallationIdStore { appContext.noBackupFilesDir },
            appVersion = BuildConfig.VERSION_NAME,
            appBuild = BuildConfig.VERSION_CODE.toString(),
            osVersion = Build.VERSION.RELEASE.orEmpty(),
            manufacturer = Build.MANUFACTURER.orEmpty(),
            deviceModel = Build.MODEL.orEmpty(),
            language = { settings.effectiveAppLanguage.code },
            sessionId = { sessionTracker.sessionId }
        )
    }

    /** The app's foreground sessions; fed by AIReplyApplication from MainActivity only. */
    val sessionTracker: SessionTracker by lazy {
        SessionTracker(listener = object : SessionTracker.Listener {
            override fun onForeground(coldStart: Boolean) {
                events.appOpened(coldStart)
            }

            override fun onBackground(foregroundMillis: Long) {
                events.appBackgrounded(foregroundMillis / 1000)
            }
        })
    }

    /** Minimal app diagnostics; see [EventReporter]. The keyboard never uses it. */
    val events: EventReporter by lazy {
        EventReporter(
            api = HttpEventsApi { ApiClient(aiConfiguration.backendBaseUrl) },
            installationId = { clientContext.installationId },
            sessionId = { sessionTracker.sessionId },
            userAllows = { settings.shareDiagnostics },
            serverAllows = {
                val state = account.state.value
                if (state.featuresLoaded) state.features?.telemetry == true else null
            },
            token = { accountSession.freshAccessTokenOrNull() },
            scope = scope
        )
    }

    val pushState: PushStateStore by lazy { PushStateStore(appContext) }

    /** Push notifications and the installation registration. */
    val push: PushCoordinator by lazy {
        PushCoordinator(
            appContext = appContext,
            settings = settings,
            store = pushState,
            client = clientContext,
            session = accountSession,
            api = HttpPushApi(client = { ApiClient(aiConfiguration.backendBaseUrl) }, session = accountSession),
            events = events,
            features = { account.state.value.features },
            featuresLoaded = { account.state.value.featuresLoaded },
            bootstrapped = { account.state.value.bootstrapComplete },
            loadServerConfig = { account.loadServerConfig() },
            // A fresh Context each time: these are read off the main thread
            // (Firebase's service), where the shared cache below is not safe.
            localized = { LocalizedContext.wrap(appContext, settings.effectiveAppLanguage) },
            scope = scope
        )
    }

    /** A screen asked for from outside the navigation graph: a notification, the keyboard. */
    val navigation: PendingNavigation by lazy { PendingNavigation() }

    /** Platform, version, locale and time zone — nothing that identifies a person. */
    fun deviceDescriptor(): DeviceDescriptor = DeviceDescriptor(
        deviceId = accountCredentials.deviceId,
        platform = "android",
        appVersion = BuildConfig.VERSION_NAME,
        osVersion = android.os.Build.VERSION.RELEASE.orEmpty(),
        locale = settings.effectiveAppLanguage.code,
        timezone = TimeZone.getDefault().id
    )

    val draftNormalizer: ReplyDraftNormalizer by lazy { ReplyDraftNormalizer() }

    val replyService: AIReplyService by lazy {
        AIReplyService(
            configuration = aiConfiguration,
            nameTemplate = { template, language ->
                TemplateNaming.displayName(localized(language), template)
            },
            accountTransport = { baseUrl, context ->
                AccountReplyTransport(
                    baseUrl = baseUrl,
                    session = accountSession,
                    usageCache = usageCache,
                    context = context
                )
            },
            // Canned replies exist only in debug builds, and only when the
            // developer switched them on.
            transportOverride = if (BuildConfig.DEBUG) {
                { request: AIReplyService.Request, _: ReplyPromptBuilder.Prompt ->
                    if (settings.debugMockReplies) DebugReplyMock(request.instruction) else null
                }
            } else {
                null
            }
        )
    }

    /**
     * A Context whose resources resolve in [language].
     *
     * Cached: building one is a Configuration copy and a resource-table lookup,
     * and the keyboard asks for the same two languages over and over.
     */
    fun localized(language: AppLanguage): Context =
        appLanguageContexts.getOrPut(language) { LocalizedContext.wrap(appContext, language) }

    fun localized(language: KeyboardLanguage): Context =
        keyboardLanguageContexts.getOrPut(language) { LocalizedContext.wrap(appContext, language) }

    /** Product strings in the app's chosen language. */
    fun strings(language: AppLanguage): AppStrings =
        stringsCache.getOrPut(language) { AppStrings(localized(language)) }

    private val appLanguageContexts = HashMap<AppLanguage, Context>()
    private val keyboardLanguageContexts = HashMap<KeyboardLanguage, Context>()
    private val stringsCache = HashMap<AppLanguage, AppStrings>()
}
