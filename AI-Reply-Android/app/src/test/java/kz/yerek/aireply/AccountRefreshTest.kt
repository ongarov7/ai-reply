package kz.yerek.aireply

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withContext
import kz.yerek.aireply.data.account.AccountObserver
import kz.yerek.aireply.data.account.AccountService
import kz.yerek.aireply.data.account.AccountUsageCache
import kz.yerek.aireply.data.account.ApiClient
import kz.yerek.aireply.data.account.DeviceDescriptor
import kz.yerek.aireply.data.legal.LegalConsentStore
import kz.yerek.aireply.data.settings.SettingsStore
import kz.yerek.aireply.support.FakeCredentials
import kz.yerek.aireply.support.FakeResponse
import kz.yerek.aireply.support.FakeServer
import kz.yerek.aireply.support.RecordedRequest
import kz.yerek.aireply.support.sessionOn
import kz.yerek.aireply.ui.feature.account.AccountController
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * What a `GET /me` answer may change: one that comes back late, after the
 * session it belonged to, changes nothing.
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

    private fun accountBody(id: String): String =
        """{"user":{"id":"$id","email":"$id@mail.kz"},"profile":{},""" +
            """"subscription":{"plan":{"id":"p1","code":"free"}},"usage":{"daily_limit":7,"remaining_today":7}}"""

    private fun sessionBody(id: String): String =
        """{"access_token":"access-2","refresh_token":"refresh-2","expires_in":900,""" +
            accountBody(id).removePrefix("{")

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
}
