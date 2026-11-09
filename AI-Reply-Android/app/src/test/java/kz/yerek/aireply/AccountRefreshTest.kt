package kz.yerek.aireply

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withContext
import kz.yerek.aireply.ai.AILimits
import kz.yerek.aireply.data.account.AccountObserver
import kz.yerek.aireply.data.account.AccountService
import kz.yerek.aireply.data.account.AccountUsageCache
import kz.yerek.aireply.data.account.ApiClient
import kz.yerek.aireply.data.account.DeviceDescriptor
import kz.yerek.aireply.data.account.LegalConfigDto
import kz.yerek.aireply.data.account.LegalConsentDto
import kz.yerek.aireply.data.legal.LegalConsentStore
import kz.yerek.aireply.data.settings.SettingsStore
import kz.yerek.aireply.support.FakeCredentials
import kz.yerek.aireply.support.FakeResponse
import kz.yerek.aireply.support.FakeServer
import kz.yerek.aireply.support.RecordedRequest
import kz.yerek.aireply.support.sessionOn
import kz.yerek.aireply.ui.feature.account.AccountController
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * What a `GET /me` answer may change: one that comes back late, after the
 * session it belonged to, changes nothing, and the server's record of the
 * consent decides whether the consent screen shows.
 *
 * Кешіккен `/me` жауабы шығып кеткен тіркелгіні қайтармайды.
 */
class AccountRefreshTest {

    private val credentials = FakeCredentials(access = "access-1", refresh = "refresh-1")
    private val settings = SettingsStore(InMemoryPreferences())
    private val legal = LegalConsentStore(InMemoryPreferences())

    private var answer: (RecordedRequest) -> FakeResponse = { FakeResponse(500) }
    private val server = FakeServer { request -> answer(request) }

    private val service = AccountService(
        session = sessionOn(server, credentials),
        baseUrlProvider = { "https://example.test" },
        deviceDescriptor = { DeviceDescriptor("device-1", "android", "1.0", "15", "en", "Asia/Almaty") },
        clientFactory = { baseUrl -> ApiClient(baseUrl, openConnection = server::open) }
    )

    private val loadedUsers = mutableListOf<String>()
    private val signedOut = mutableListOf<Boolean>()

    private fun controller() = AccountController(
        service = service,
        credentials = credentials,
        usageCache = AccountUsageCache(settings),
        legalConsentStore = legal,
        observer = object : AccountObserver {
            override fun onAccountLoaded(userId: String) {
                loadedUsers += userId
            }

            override fun onSignedOut(userInitiated: Boolean) {
                signedOut += userInitiated
            }
        }
    )

    private val production = LegalConfigDto.PRODUCTION

    private val configLoaded = FakeResponse(
        200,
        """{"legal":{"terms_version":"${production.termsVersion}","privacy_version":"${production.privacyVersion}",""" +
            """"terms_url":"https://ai-reply.kz/offer","privacy_url":"https://ai-reply.kz/privacy"}}"""
    )

    private val consentJson =
        """{"terms_version":"${production.termsVersion}","privacy_version":"${production.privacyVersion}",""" +
            """"accepted_at":"2026-10-08T10:00:00Z","locale":"ru","platform":"android","app_version":"1.0"}"""

    private fun accountBody(id: String, consent: String? = null): String =
        """{"user":{"id":"$id","email":"$id@mail.kz"},"profile":{},""" +
            """"subscription":{"plan":{"id":"p1","code":"free"}},"usage":{"daily_limit":7,"remaining_today":7}""" +
            (consent?.let { ""","legal_consent":$it""" } ?: "") + "}"

    private fun sessionBody(id: String, consent: String? = null): String =
        """{"access_token":"access-2","refresh_token":"refresh-2","expires_in":900,""" +
            accountBody(id, consent).removePrefix("{")

    /** The acceptance as the server confirmed it some time ago. */
    private fun consentOnThePhone() {
        legal.restore(
            LegalConsentDto(
                termsVersion = production.termsVersion,
                privacyVersion = production.privacyVersion,
                acceptedAt = "2026-10-08T10:00:00Z",
                locale = "ru",
                platform = "android",
                appVersion = "1.0"
            )
        )
    }

    @After
    fun resetLimits() {
        // A config load stores the server's limits process-wide.
        AILimits.install(InMemoryPreferences())
    }

    // ------------------------------------------------------- a late answer

    @Test
    fun `an account answer that arrives after sign-out changes nothing`() = runBlocking {
        val arrived = CountDownLatch(1)
        val release = CountDownLatch(1)
        answer = { request ->
            when (request.path) {
                "/api/v1/me" -> {
                    arrived.countDown()
                    release.await(5, TimeUnit.SECONDS)
                    FakeResponse(200, accountBody("u1"))
                }
                "/api/v1/auth/logout" -> FakeResponse(200, "{}")
                else -> FakeResponse(404)
            }
        }
        val account = controller()
        val refreshing = CoroutineScope(Job()).launch { account.refresh() }
        withContext(Dispatchers.IO) { assertTrue(arrived.await(5, TimeUnit.SECONDS)) }

        account.signOut()
        release.countDown()
        refreshing.join()

        assertTrue(account.state.value.phase is AccountController.Phase.SignedOut)
        assertNull("the old user is not back", account.state.value.user)
        assertNull(account.state.value.subscription)
        assertNull("nor its name", credentials.displayIdentifier)
        assertTrue("the push installation was not told", loadedUsers.isEmpty())
    }

    @Test
    fun `an account answer for a session that ended elsewhere changes nothing`() = runBlocking {
        val arrived = CountDownLatch(1)
        val release = CountDownLatch(1)
        answer = {
            arrived.countDown()
            release.await(5, TimeUnit.SECONDS)
            FakeResponse(200, accountBody("u1"))
        }
        val account = controller()
        val refreshing = CoroutineScope(Job()).launch { account.refresh() }
        withContext(Dispatchers.IO) { assertTrue(arrived.await(5, TimeUnit.SECONDS)) }

        // Another request found the refresh token revoked.
        credentials.clear()
        release.countDown()
        refreshing.join()

        assertNull(account.state.value.user)
        assertNull("its name is not written back", credentials.displayIdentifier)
        assertTrue(loadedUsers.isEmpty())
    }

    @Test
    fun `a refusal that arrives while signing out is not a second sign-out`() = runBlocking {
        val arrived = CountDownLatch(1)
        val release = CountDownLatch(1)
        val finished = CountDownLatch(1)
        answer = { request ->
            when (request.path) {
                "/api/v1/me" -> {
                    arrived.countDown()
                    release.await(5, TimeUnit.SECONDS)
                    FakeResponse(401, FakeServer.envelope("UNAUTHORIZED"))
                }
                "/api/v1/auth/logout" -> {
                    release.countDown()
                    finished.await(5, TimeUnit.SECONDS)
                    FakeResponse(200, "{}")
                }
                else -> FakeResponse(404)
            }
        }
        val account = controller()
        val refreshing = CoroutineScope(Job()).launch { account.refresh() }
        refreshing.invokeOnCompletion { finished.countDown() }
        withContext(Dispatchers.IO) { assertTrue(arrived.await(5, TimeUnit.SECONDS)) }

        account.signOut()

        assertEquals("told once, as the user's own", listOf(true), signedOut)
    }

    @Test
    fun `a late refusal for the old account does not sign out the one that came after`() = runBlocking {
        val arrived = CountDownLatch(1)
        val release = CountDownLatch(1)
        var loads = 0
        answer = { request ->
            when (request.path) {
                "/api/v1/me" -> if (++loads == 1) {
                    FakeResponse(200, accountBody("u1"))
                } else {
                    arrived.countDown()
                    release.await(5, TimeUnit.SECONDS)
                    FakeResponse(403, FakeServer.envelope("ACCOUNT_DISABLED"))
                }
                "/api/v1/auth/logout" -> FakeResponse(200, "{}")
                "/api/v1/auth/email/otp/request" -> FakeResponse(200, "{}")
                "/api/v1/auth/email/otp/verify" -> FakeResponse(200, sessionBody("u2"))
                else -> FakeResponse(404)
            }
        }
        val account = controller()
        account.refresh()
        assertEquals("u1", account.state.value.user?.id)

        val refreshing = CoroutineScope(Job()).launch { account.refresh() }
        withContext(Dispatchers.IO) { assertTrue(arrived.await(5, TimeUnit.SECONDS)) }
        account.signOut()
        account.startEmailSignIn()
        account.requestEmailCode("u2@mail.kz", "ru")
        account.verifyEmailCode("1234")
        assertEquals("u2", account.state.value.user?.id)

        release.countDown()
        refreshing.join()

        assertTrue(account.state.value.isSignedIn)
        assertEquals("u2", account.state.value.user?.id)
        assertNull(account.state.value.errorMessage)
        assertTrue("the session of the new account stays", credentials.isSignedIn)
    }

    // --------------------------------------------------- the server's record

    @Test
    fun `a consent the server no longer has brings the consent screen back`() = runBlocking {
        consentOnThePhone()
        answer = { request ->
            when (request.path) {
                "/api/v1/config" -> configLoaded
                "/api/v1/me" -> FakeResponse(200, accountBody("u1"))
                else -> FakeResponse(404)
            }
        }
        val account = controller()
        account.loadServerConfig()
        assertTrue(account.state.value.hasAcceptedLegal)

        account.refresh()

        assertFalse("withdrawn elsewhere: asked again", account.state.value.hasAcceptedLegal)
        assertNull(legal.current())
        assertFalse("the keyboard asks the same store", legal.hasAcceptedLatest())
    }

    @Test
    fun `another account signing in is asked for its own consent`() = runBlocking {
        consentOnThePhone()
        credentials.clear()
        answer = { request ->
            when (request.path) {
                "/api/v1/config" -> configLoaded
                "/api/v1/auth/email/otp/request" -> FakeResponse(200, "{}")
                "/api/v1/auth/email/otp/verify" -> FakeResponse(200, sessionBody("u2"))
                else -> FakeResponse(404)
            }
        }
        val account = controller()
        account.loadServerConfig()
        assertTrue(account.state.value.hasAcceptedLegal)

        account.startEmailSignIn()
        account.requestEmailCode("u2@mail.kz", "ru")
        account.verifyEmailCode("1234")

        assertTrue(account.state.value.isSignedIn)
        assertFalse("the first account's consent is not this one's", account.state.value.hasAcceptedLegal)
        assertNull(legal.current())
    }

    @Test
    fun `the account's own consent keeps the screen away`() = runBlocking {
        consentOnThePhone()
        answer = { request ->
            when (request.path) {
                "/api/v1/config" -> configLoaded
                "/api/v1/me" -> FakeResponse(200, accountBody("u1", consentJson))
                else -> FakeResponse(404)
            }
        }
        val account = controller()
        account.loadServerConfig()

        account.refresh()

        assertTrue(account.state.value.hasAcceptedLegal)
        assertNotNull(legal.current())
    }

    @Test
    fun `without the server's versions a missing consent proves nothing`() = runBlocking {
        consentOnThePhone()
        answer = { request ->
            when (request.path) {
                "/api/v1/config" -> FakeResponse(500)
                "/api/v1/me" -> FakeResponse(200, accountBody("u1"))
                else -> FakeResponse(404)
            }
        }
        val account = controller()
        account.loadServerConfig()

        account.refresh()

        assertTrue(account.state.value.hasAcceptedLegal)
        assertNotNull(legal.current())
    }

    @Test
    fun `an acceptance waiting to be sent stays`() = runBlocking {
        legal.accept(production, "ru", "1.0")
        answer = { request ->
            when (request.path) {
                "/api/v1/config" -> configLoaded
                "/api/v1/me" -> FakeResponse(200, accountBody("u1"))
                else -> FakeResponse(500)
            }
        }
        val account = controller()
        account.loadServerConfig()

        account.refresh()

        assertTrue(account.state.value.hasAcceptedLegal)
        assertTrue("still to be sent", legal.current()!!.pendingSync)
    }

    @Test
    fun `an acceptance confirmed while the request was out is newer than its answer`() = runBlocking {
        consentOnThePhone()
        val arrived = CountDownLatch(1)
        val release = CountDownLatch(1)
        answer = { request ->
            when (request.path) {
                "/api/v1/config" -> configLoaded
                "/api/v1/me" -> {
                    arrived.countDown()
                    release.await(5, TimeUnit.SECONDS)
                    FakeResponse(200, accountBody("u1"))
                }
                else -> FakeResponse(200, consentJson)
            }
        }
        val account = controller()
        account.loadServerConfig()
        val refreshing = CoroutineScope(Job()).launch { account.refresh() }
        withContext(Dispatchers.IO) { assertTrue(arrived.await(5, TimeUnit.SECONDS)) }

        account.acceptLegal("kk")
        release.countDown()
        refreshing.join()

        assertTrue(account.state.value.hasAcceptedLegal)
        assertNotNull(legal.current())
    }
}
