package kz.yerek.aireply

import android.content.Context
import android.content.ContextWrapper
import android.content.SharedPreferences
import kotlinx.coroutines.runBlocking
import kz.yerek.aireply.ai.AIConfiguration
import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.data.BackendEnvironment
import kz.yerek.aireply.data.account.AccountService
import kz.yerek.aireply.data.account.AccountSession
import kz.yerek.aireply.data.account.ApiClient
import kz.yerek.aireply.data.account.DeviceDescriptor
import kz.yerek.aireply.data.account.LegalConfigDto
import kz.yerek.aireply.data.legal.LegalConsentStore
import kz.yerek.aireply.data.profile.ProfileStore
import kz.yerek.aireply.data.secure.SecureCredentialStore
import kz.yerek.aireply.data.settings.DeviceStateStore
import kz.yerek.aireply.data.settings.SettingsStore
import kz.yerek.aireply.diagnostics.CrashReporter
import kz.yerek.aireply.domain.model.ReplyConfiguration
import kz.yerek.aireply.keyboard.autocorrect.PrefsLearnedWordsStore
import kz.yerek.aireply.keyboard.emoji.PrefsEmojiRecentsStore
import kz.yerek.aireply.push.InstallationIdStore
import kz.yerek.aireply.push.PushStateStore
import kz.yerek.aireply.support.FakeCredentials
import kz.yerek.aireply.support.FakeResponse
import kz.yerek.aireply.support.FakeServer
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File
import java.net.URL
import javax.xml.parsers.DocumentBuilderFactory

/** Exercises the real storage constructors with two environments on the same installation. */
class BackendEnvironmentTest {
    @get:Rule val folder = TemporaryFolder()

    private val production = BackendEnvironment.PRODUCTION
    private val sandbox = BackendEnvironment.SANDBOX

    private class StorageContext(private val directory: File) : ContextWrapper(null) {
        private val preferences = mutableMapOf<String, InMemoryPreferences>()
        override fun getApplicationContext(): Context = this
        override fun getFilesDir(): File = directory
        override fun getSharedPreferences(name: String, mode: Int): SharedPreferences =
            preferences.getOrPut(name) { InMemoryPreferences() }
    }

    @Test fun `the absent environment keeps production and unknown values fail closed`() {
        assertEquals(production, BackendEnvironment.fromName())
        assertEquals("https://ai-reply.kz", production.baseUrl)
        assertEquals("https://ai-reply.kz/sandbox", sandbox.baseUrl)
        for (invalid in listOf("", "Sandbox", "staging", "https://example.test", "sandbox/../")) {
            assertThrows(IllegalArgumentException::class.java) { BackendEnvironment.fromName(invalid) }
        }
    }

    @Test fun `every build uses its selected environment without changing the Play package`() {
        val selected = BackendEnvironment.fromName(BuildConfig.BACKEND_ENVIRONMENT)
        assertEquals(selected, BackendEnvironment.current)
        assertEquals(selected.baseUrl, BuildConfig.BACKEND_BASE_URL)
        assertEquals(selected.baseUrl, AIConfiguration(isAccountSignedIn = { true }).backendBaseUrl)
        assertEquals("kz.yerek.aireply", BuildConfig.APPLICATION_ID)
    }

    @Test fun `sandbox account fallback never migrates or clears production snapshots`() {
        val context = StorageContext(folder.root)
        val oldProductionSettings = context.getSharedPreferences("aireply_settings", 0)
        oldProductionSettings.edit()
            .putString("account.identifier", "production@example.test")
            .putString("account.deviceId", "production-device")
            .putInt("account.usage.remainingTotal", 700)
            .apply()
        val testSettings = SettingsStore(context, sandbox)
        testSettings.migrateToBackendOnly()
        assertNull(testSettings.accountIdentifier)
        assertNull(testSettings.accountDeviceId)
        assertNull(testSettings.cachedRemainingTotal)
        testSettings.accountIdentifier = "sandbox@example.test"
        testSettings.accountDeviceId = "sandbox-device"
        testSettings.cachedRemainingTotal = 30
        testSettings.appLanguage = AppLanguage.ENGLISH

        val liveSettings = SettingsStore(context, production)
        assertEquals("production@example.test", liveSettings.accountIdentifier)
        assertEquals("production-device", liveSettings.accountDeviceId)
        assertEquals(700, liveSettings.cachedRemainingTotal)
        assertNull(liveSettings.appLanguage)
        testSettings.clearAccountState()
        assertEquals(700, SettingsStore(context, production).cachedRemainingTotal)
        assertNull(SettingsStore(context, sandbox).cachedRemainingTotal)
    }

    @Test fun `sandbox secret cleanup and key alias are separate from production`() {
        val context = StorageContext(folder.root)
        val livePreferences = context.getSharedPreferences("aireply_secure", 0)
        livePreferences.edit().putString("account.refresh.token", "production-ciphertext")
            .putString("openai.api.key", "production-legacy-ciphertext").apply()
        val testPreferences = context.getSharedPreferences("sandbox_aireply_secure", 0)
        testPreferences.edit().putString("openai.api.key", "test-legacy-ciphertext").apply()
        val testSecrets = SecureCredentialStore(context, sandbox)
        assertNull("no fallback to the production credential", testSecrets.refreshToken())
        testSecrets.removeLegacySecrets()
        testSecrets.clearAccountTokens()
        assertFalse(testPreferences.contains("openai.api.key"))
        assertEquals("production-ciphertext", livePreferences.getString("account.refresh.token", null))
        assertTrue(livePreferences.contains("openai.api.key"))
        assertEquals("kz.yerek.aireply.credentials", SecureCredentialStore(context, production).keyAlias)
        assertNotEquals(SecureCredentialStore(context, production).keyAlias, testSecrets.keyAlias)
    }

    @Test fun `profiles and their atomic writes survive switching back to production`() {
        val context = StorageContext(folder.root)
        val liveProfile = ReplyConfiguration.INITIAL.copy(profile = ReplyConfiguration.INITIAL.profile.copy(role = "production"))
        val testProfile = ReplyConfiguration.INITIAL.copy(profile = ReplyConfiguration.INITIAL.profile.copy(role = "sandbox"))
        assertTrue(ProfileStore(context, production).save(liveProfile))
        assertEquals("", ProfileStore(context, sandbox).load().profile.role)
        assertTrue(ProfileStore(context, sandbox).save(testProfile))
        assertEquals("production", ProfileStore(context, production).load().profile.role)
        assertEquals("sandbox", ProfileStore(context, sandbox).load().profile.role)
        assertFalse(File(folder.root, "reply-configuration.json.tmp").exists())
        assertFalse(File(folder.root, "sandbox_reply-configuration.json.tmp").exists())
    }

    @Test fun `legal versions and acceptance never cross environments`() {
        val context = StorageContext(folder.root)
        val live = LegalConsentStore(context, production)
        live.accept(LegalConfigDto.PRODUCTION, "en", "1.2.1")
        val test = LegalConsentStore(context, sandbox)
        assertNull(test.current())
        val testLegal = LegalConfigDto.PRODUCTION.copy(termsVersion = "sandbox-only")
        test.rememberConfig(testLegal)
        test.accept(testLegal, "ru", "1.2.1")
        assertEquals(LegalConfigDto.PRODUCTION, LegalConsentStore(context, production).latestConfig())
        assertTrue(LegalConsentStore(context, production).hasAcceptedLatest())
        assertEquals("sandbox-only", LegalConsentStore(context, sandbox).current()?.termsVersion)
    }

    @Test fun `installation ids and push binding remain distinct on the same package`() {
        val context = StorageContext(folder.root)
        val liveId = InstallationIdStore(production) { folder.root }.id()
        val testId = InstallationIdStore(sandbox) { folder.root }.id()
        assertNotEquals(liveId, testId)
        assertEquals(liveId, InstallationIdStore(production) { folder.root }.id())
        assertEquals(testId, InstallationIdStore(sandbox) { folder.root }.id())

        val live = PushStateStore(context, production)
        live.fcmToken = "one-package-fcm-token"
        live.accountUserId = "production-user"
        live.recordSuccess("production-binding", 100L)
        val test = PushStateStore(context, sandbox)
        assertNull(test.fcmToken)
        assertNull(test.accountUserId)
        assertNull(test.lastSyncFingerprint)
        test.fcmToken = "one-package-fcm-token"
        test.accountUserId = "sandbox-user"
        test.recordSuccess("sandbox-binding", 200L)
        assertEquals("production-user", PushStateStore(context, production).accountUserId)
        assertEquals("production-binding", PushStateStore(context, production).lastSyncFingerprint)
    }

    @Test fun `pending device context crash reports and learned text stay in their environment`() {
        val context = StorageContext(folder.root)
        DeviceStateStore(context, production).updateLastCheck = 99L
        assertNull(DeviceStateStore(context, sandbox).updateLastCheck)
        DeviceStateStore(context, sandbox).updateLastCheck = 111L
        assertEquals(99L, DeviceStateStore(context, production).updateLastCheck)

        val crash = CrashReporter.Pending("LiveCrash", "code.kt:1", 1, 0)
        CrashReporter.PreferencesStore(context, production).save(crash)
        assertNull(CrashReporter.PreferencesStore(context, sandbox).load())
        CrashReporter.PreferencesStore(context, sandbox).clear()
        assertEquals(crash, CrashReporter.PreferencesStore(context, production).load())

        val liveWords = PrefsLearnedWordsStore(context, production)
        liveWords.write(KeyboardLanguage.ENGLISH, "live-word")
        val testWords = PrefsLearnedWordsStore(context, sandbox)
        assertNull(testWords.read(KeyboardLanguage.ENGLISH))
        testWords.write(KeyboardLanguage.ENGLISH, "test-word")
        testWords.clear()
        assertEquals("live-word", liveWords.read(KeyboardLanguage.ENGLISH))
        PrefsEmojiRecentsStore(context, production).write("🙂")
        assertNull(PrefsEmojiRecentsStore(context, sandbox).read())
        PrefsEmojiRecentsStore(context, sandbox).write("🧪")
        assertEquals("🙂", PrefsEmojiRecentsStore(context, production).read())
    }

    @Test fun `sandbox sessions and data are excluded from both backup systems`() {
        val expectedPreferences = listOf("aireply_secure", "aireply_settings", "aireply_account", "aireply_device",
            "aireply_legal", "aireply_push", "aireply_crash_report", "aireply_autocorrect", "aireply_emoji", "tutorial_playback")
            .map { "sandbox_$it.xml" }
        for ((file, sections) in listOf(
            "backup_rules.xml" to listOf("full-backup-content"),
            "data_extraction_rules.xml" to listOf("cloud-backup", "device-transfer")
        )) {
            val document = DocumentBuilderFactory.newInstance().newDocumentBuilder().parse(File("src/main/res/xml/$file"))
            for (section in sections) {
                val nodes = document.getElementsByTagName(section).item(0).childNodes
                val excluded = (0 until nodes.length).mapNotNull { index ->
                    val node = nodes.item(index)
                    if (node.nodeName == "exclude") node.attributes.getNamedItem("domain").nodeValue to
                        node.attributes.getNamedItem("path").nodeValue else null
                }.toSet()
                for (name in expectedPreferences) assertTrue("$section omits $name", "sharedpref" to name in excluded)
                assertTrue("$section omits the test profile", "file" to "sandbox_reply-configuration.json" in excluded)
                assertTrue("$section omits an interrupted test profile write", "file" to "sandbox_reply-configuration.json.tmp" in excluded)
            }
        }
    }

    @Test fun `sandbox authentication catalogue and purchase verification never use production paths`() = runBlocking {
        val urls = mutableListOf<URL>()
        val server = FakeServer { request -> when (request.path) {
            "/sandbox/api/v1/auth/refresh" -> FakeResponse(200, """{"access_token":"fresh","refresh_token":"refresh","expires_in":900}""")
            "/sandbox/api/v1/iap/products" -> FakeResponse(200, """{"products":[],"account_token":"test-account"}""")
            "/sandbox/api/v1/iap/google/purchases" -> FakeResponse(200, """{"status":"granted"}""")
            else -> FakeResponse(404)
        } }
        val factory: (String) -> ApiClient = { baseUrl -> ApiClient(baseUrl, openConnection = { url ->
            urls += url
            server.open(url)
        }) }
        val descriptor = { DeviceDescriptor("test-device", "android", "1.2.1", "15", "en", "UTC") }
        val session = AccountSession(FakeCredentials("old", "refresh", fresh = false),
            { sandbox.baseUrl }, descriptor, clientFactory = factory)
        val service = AccountService(session = session, baseUrlProvider = { sandbox.baseUrl },
            deviceDescriptor = descriptor, clientFactory = factory)
        assertEquals("test-account", service.iapProducts().accountToken)
        assertTrue(service.verifyGooglePurchase("test.product", "test-purchase-token").isGranted)
        assertEquals(3, server.requests.size)
        assertTrue(urls.all { it.protocol == "https" && it.host == "ai-reply.kz" && it.path.startsWith("/sandbox/api/v1/") })
        assertTrue(server.requests.drop(1).all { it.header("Authorization") == "Bearer fresh" })
    }
}
