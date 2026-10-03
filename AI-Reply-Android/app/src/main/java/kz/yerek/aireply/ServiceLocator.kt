package kz.yerek.aireply

import android.content.Context
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.Dispatchers
import kz.yerek.aireply.ai.AIConfiguration
import kz.yerek.aireply.ai.AIReplyService
import kz.yerek.aireply.ai.AccountComposeTransport
import kz.yerek.aireply.ai.AccountReplyTransport
import kz.yerek.aireply.ai.AppStrings
import kz.yerek.aireply.ai.ComposeService
import kz.yerek.aireply.ai.DebugComposeMock
import kz.yerek.aireply.ai.DebugReplyMock
import kz.yerek.aireply.ai.ReplyPromptBuilder
import kz.yerek.aireply.ai.ReplyDraftNormalizer
import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.core.lang.LocalizedContext
import kz.yerek.aireply.core.lang.TemplateNaming
import kz.yerek.aireply.data.account.AccountCredentials
import kz.yerek.aireply.data.account.AccountService
import kz.yerek.aireply.data.account.AccountSession
import kz.yerek.aireply.data.account.AccountUsageCache
import kz.yerek.aireply.data.account.DeviceDescriptor
import kz.yerek.aireply.data.account.GoogleSignInClient
import kz.yerek.aireply.data.legal.LegalConsentStore
import kz.yerek.aireply.data.profile.ConfigurationRepository
import kz.yerek.aireply.data.profile.ProfileStore
import kz.yerek.aireply.data.secure.SecureCredentialStore
import kz.yerek.aireply.data.settings.SettingsStore
import kz.yerek.aireply.ui.feature.account.AccountController
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
            backgroundScope = scope
        )
    }

    val accountService: AccountService by lazy {
        AccountService(
            session = accountSession,
            baseUrlProvider = { aiConfiguration.backendBaseUrl },
            deviceDescriptor = ::deviceDescriptor
        )
    }

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

    /** The keyboard's "Create" mode: a message from an instruction, no copied text. */
    val composeService: ComposeService by lazy {
        ComposeService(
            configuration = aiConfiguration,
            accountTransport = { baseUrl ->
                AccountComposeTransport(
                    baseUrl = baseUrl,
                    session = accountSession,
                    usageCache = usageCache,
                    appVersion = BuildConfig.VERSION_NAME
                )
            },
            transportOverride = if (BuildConfig.DEBUG) {
                { _: ComposeService.Request -> if (settings.debugMockReplies) DebugComposeMock() else null }
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
