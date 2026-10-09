package kz.yerek.aireply

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonObject
import kz.yerek.aireply.ai.AIFeatures
import kz.yerek.aireply.data.account.AccountService
import kz.yerek.aireply.data.account.AccountUser
import kz.yerek.aireply.data.account.ApiClient
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.ApiException
import kz.yerek.aireply.data.account.ProfileUpdate
import kz.yerek.aireply.data.profile.PreferredLanguageSync
import kz.yerek.aireply.data.settings.DeviceStateStore
import kz.yerek.aireply.support.FakeCredentials
import kz.yerek.aireply.support.FakeResponse
import kz.yerek.aireply.support.FakeServer
import kz.yerek.aireply.support.sessionOn
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * When the account's notification language is sent, and when it never is.
 *
 * Хабарлама тілі: тек сервер белгілесе, тек өзгергенде не тіркелгіде тіл жоқ болса.
 */
class PreferredLanguageTest {

    private val device = DeviceStateStore(InMemoryPreferences())
    private val sent = mutableListOf<ProfileUpdate>()
    private var features = AIFeatures.NONE.copy(preferredLanguage = true)
    private var signedIn = true
    private var language = "kk"
    private var fail = false

    /** Launched work is queued and never run: the tests drive pushPending themselves. */
    private val parked = CoroutineScope(StandardTestDispatcher())

    private val sync = PreferredLanguageSync(
        update = { change ->
            if (fail) throw ApiException(ApiError.Offline)
            sent += change
        },
        device = device,
        isSignedIn = { signedIn },
        features = { features },
        language = { language },
        scope = parked
    )

    @Test
    fun `a server without the feature never gets the field`() = runBlocking {
        features = AIFeatures.NONE
        sync.languageChanged()
        sync.adopt("")
        sync.pushPending()
        assertTrue(sent.isEmpty())
    }

    @Test
    fun `an account without a language gets this phone's`() = runBlocking {
        sync.adopt("")
        sync.pushPending()
        assertEquals(listOf(ProfileUpdate(preferredLanguage = "kk")), sent)
        assertFalse(device.languagePendingSync)
    }

    @Test
    fun `an account whose language was set elsewhere is left alone`() = runBlocking {
        sync.adopt("ru")
        sync.pushPending()
        assertTrue(sent.isEmpty())
        assertFalse(device.languagePendingSync)
    }

    @Test
    fun `a language picked in Settings is sent even over the account's own`() = runBlocking {
        sync.adopt("ru")
        language = "uz"
        sync.languageChanged()
        sync.pushPending()
        assertEquals(listOf(ProfileUpdate(preferredLanguage = "uz")), sent)
    }

    @Test
    fun `an unsent change waits for the next foreground and the sign-in`() = runBlocking {
        signedIn = false
        sync.languageChanged()
        sync.pushPending()
        assertTrue("signed out: nothing goes", sent.isEmpty())

        signedIn = true
        fail = true
        sync.pushPending()
        assertTrue("still pending after a failure", device.languagePendingSync)

        fail = false
        sync.pushPending()
        assertEquals(listOf(ProfileUpdate(preferredLanguage = "kk")), sent)
        assertFalse(device.languagePendingSync)
    }

    @Test
    fun `signing out drops what the account never received`() = runBlocking {
        signedIn = false
        sync.languageChanged()
        device.accountSignedOut()
        signedIn = true
        sync.pushPending()
        assertTrue(sent.isEmpty())
    }

    // ------------------------------------------------------------------ wire

    @Test
    fun `the field goes out alone, and a null one never goes out at all`() = runBlocking {
        val server = FakeServer { FakeResponse(200, "{}") }
        val service = AccountService(
            session = sessionOn(server, FakeCredentials(access = "a1", refresh = "r1")),
            baseUrlProvider = { "https://example.test" },
            deviceDescriptor = { error("not used") },
            clientFactory = { baseUrl -> ApiClient(baseUrl, openConnection = server::open) }
        )

        service.updateProfile(ProfileUpdate(grammaticalGender = "female"))
        service.updateProfile(ProfileUpdate(preferredLanguage = "kk"))

        val (gender, preferred) = server.requests.map { Json.parseToJsonElement(it.body).jsonObject }
        assertEquals(setOf("grammatical_gender"), gender.keys)
        assertEquals(setOf("preferred_language"), preferred.keys)
        assertEquals("\"kk\"", preferred["preferred_language"].toString())
        assertTrue(server.requests.all { it.path == "/api/v1/me" && it.method == "POST" })
    }

    @Test
    fun `the account's language is read, and an older server's absence is not an error`() {
        val json = Json { ignoreUnknownKeys = true }
        val current = json.decodeFromString(
            AccountUser.serializer(),
            """{"id":"u1","preferred_language":"uz"}"""
        )
        assertEquals("uz", current.preferredLanguage)
        val older = json.decodeFromString(
            AccountUser.serializer(),
            """{"id":"u1"}"""
        )
        assertNull(older.preferredLanguage)
    }
}
