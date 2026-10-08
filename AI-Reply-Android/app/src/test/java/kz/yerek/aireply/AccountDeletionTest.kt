package kz.yerek.aireply

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withContext
import kz.yerek.aireply.ai.AILimits
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.data.account.AccountObserver
import kz.yerek.aireply.data.account.AccountService
import kz.yerek.aireply.data.account.AccountUsageCache
import kz.yerek.aireply.data.account.ApiClient
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.DeviceDescriptor
import kz.yerek.aireply.data.account.LegalConfigDto
import kz.yerek.aireply.data.account.UsageDto
import kz.yerek.aireply.data.legal.LegalConsentStore
import kz.yerek.aireply.data.settings.SettingsStore
import kz.yerek.aireply.keyboard.autocorrect.LearnedWords
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
import java.io.IOException
import java.net.ConnectException
import java.net.SocketTimeoutException
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * Deleting the account and withdrawing the AI consent from Settings, and the
 * consent screen coming back when the server asks for it.
 *
 * Аккаунтты жою және келісімді кері қайтару: сервер мен телефон екеуі де ұмытады.
 */
class AccountDeletionTest {

    private val credentials = FakeCredentials(access = "access-1", refresh = "refresh-1")
    private val settings = SettingsStore(InMemoryPreferences())
    private val usage = AccountUsageCache(settings)
    private val legal = LegalConsentStore(InMemoryPreferences())
    private val learned = MemoryLearnedWordsStore()

    private var answer: (RecordedRequest) -> FakeResponse = { FakeResponse(500) }
    private val server = FakeServer { request -> answer(request) }

    private val service = AccountService(
        session = sessionOn(server, credentials),
        baseUrlProvider = { "https://example.test" },
        deviceDescriptor = { DeviceDescriptor("device-1", "android", "1.0", "15", "en", "Asia/Almaty") },
        clientFactory = { baseUrl -> ApiClient(baseUrl, openConnection = server::open) }
    )

    /** What the push installation would see, and the consent at that moment. */
    private class RecordingObserver(private val legal: LegalConsentStore) : AccountObserver {
        val signedOut = mutableListOf<Boolean>()
        var consentAtSignOut: Any? = "not signed out"

        override fun onSignedOut(userInitiated: Boolean) {
            signedOut += userInitiated
            consentAtSignOut = legal.current()
        }
    }

    private val observer = RecordingObserver(legal)

    private fun controller(background: CoroutineScope? = null) = AccountController(
        service = service,
        credentials = credentials,
        usageCache = usage,
        legalConsentStore = legal,
        backgroundScope = background,
        observer = observer,
        learnedWords = learned
    )

    private val refused = FakeResponse(401, FakeServer.envelope("UNAUTHORIZED"))

    /** The server's record of the current versions, as `POST /me/consents` answers. */
    private val consentRecorded = FakeResponse(
        200,
        """{"terms_version":"${LegalConfigDto.PRODUCTION.termsVersion}",""" +
            """"privacy_version":"${LegalConfigDto.PRODUCTION.privacyVersion}",""" +
            """"accepted_at":"2026-10-08T10:00:00Z","locale":"ru","platform":"android","app_version":"1.0"}"""
    )

    private val accountLoaded = FakeResponse(
        200,
        """{"user":{"id":"u1","email":"aigerim@mail.kz"},"profile":{},""" +
            """"subscription":{"plan":{"id":"p1","code":"free"}},"usage":{"daily_limit":7,"remaining_today":7}}"""
    )

    /** Everything a deleted account kept on the phone is gone, and the consent screen says so. */
    private fun assertForgotten(account: AccountController) {
        assertFalse("signed out", credentials.isSignedIn)
        assertTrue(account.state.value.phase is AccountController.Phase.SignedOut)
        assertNull("consent forgotten", legal.current())
        assertFalse(account.state.value.hasAcceptedLegal)
        assertEquals(R.string.account_deleted, account.state.value.notice)
        assertTrue("learned words gone", learned.values.isEmpty())
        assertFalse("quota cache gone", usage.current().isKnown)
        assertFalse(account.state.value.busy)
        assertFalse(account.state.value.deletingAccount)
        assertFalse(account.state.value.accountDeletionFailed)
    }

    private fun signedInWithEverything() {
        legal.accept(LegalConfigDto.PRODUCTION, "ru", "1.0")
        usage.store(UsageDto(dailyLimit = 20, usedToday = 5, remainingToday = 15))
        usage.storePlanCode("free")
        learned.write(KeyboardLanguage.RUSSIAN, LearnedWords.EMPTY.adding("сегодян").encoded())
    }

    @After
    fun resetLimits() {
        // A config load stores the server's limits process-wide.
        AILimits.install(InMemoryPreferences())
    }

    // -------------------------------------------------------------- deletion

    @Test
    fun `delete account goes to the POST alias with the session`() = runBlocking {
        answer = { FakeResponse(200, """{"deleted":true,"apple_token_revoked":false}""") }
        val result = service.deleteAccount()

        val request = server.requests.single()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/me/delete", request.path)
        assertEquals("Bearer access-1", request.header("Authorization"))
        assertEquals("an Android client has no Apple code to send", "{}", request.body)
        assertTrue(result.deleted)
        assertFalse(result.appleTokenRevoked)
    }

    @Test
    fun `a deleted account leaves nothing of itself on the phone`() = runBlocking {
        signedInWithEverything()
        answer = { FakeResponse(200, """{"deleted":true,"apple_token_revoked":false}""") }
        val account = controller()
        assertTrue(account.state.value.hasAcceptedLegal)

        assertTrue(account.deleteAccount())

        assertEquals(1, server.count("/api/v1/me/delete"))
        assertFalse("signed out", credentials.isSignedIn)
        assertTrue(account.state.value.phase is AccountController.Phase.SignedOut)
        assertNull("consent forgotten", legal.current())
        assertFalse(account.state.value.hasAcceptedLegal)
        assertEquals(R.string.account_deleted, account.state.value.notice)
        assertTrue("learned words gone", learned.values.isEmpty())
        assertFalse("quota cache gone", usage.current().isKnown)
        assertEquals("", usage.current().planCode)
        assertFalse(account.state.value.busy)
        assertEquals(listOf(true), observer.signedOut)
        assertNull("the consent went before the installation heard of the sign-out", observer.consentAtSignOut)
    }

    @Test
    fun `a failed deletion changes nothing on the phone`() = runBlocking {
        signedInWithEverything()
        answer = { FakeResponse(500, FakeServer.envelope("INTERNAL")) }
        val account = controller()

        assertFalse(account.deleteAccount())

        assertTrue(credentials.isSignedIn)
        assertNotNull(legal.current())
        assertTrue(account.state.value.hasAcceptedLegal)
        assertNull(account.state.value.notice)
        assertFalse(account.state.value.busy)
        assertFalse(account.state.value.deletingAccount)
        assertTrue("Settings shows the failure", account.state.value.accountDeletionFailed)
        assertTrue(learned.values.isNotEmpty())
        assertTrue(usage.current().isKnown)
        assertTrue(observer.signedOut.isEmpty())
    }

    @Test
    fun `a deletion shows while it runs and its failure outlives the screen`() = runBlocking {
        signedInWithEverything()
        val arrived = CountDownLatch(1)
        val release = CountDownLatch(1)
        lateinit var account: AccountController
        var deletingWhileSent = false
        answer = {
            deletingWhileSent = account.state.value.deletingAccount
            arrived.countDown()
            release.await(5, TimeUnit.SECONDS)
            FakeResponse(500, FakeServer.envelope("INTERNAL"))
        }
        account = controller()
        val settings = CoroutineScope(Job())

        val deletion = settings.launch { account.deleteAccount() }
        withContext(Dispatchers.IO) { assertTrue(arrived.await(5, TimeUnit.SECONDS)) }
        // The user leaves Settings while the request is out.
        settings.cancel()
        release.countDown()
        deletion.join()

        assertTrue(deletingWhileSent)
        assertFalse(account.state.value.deletingAccount)
        assertFalse(account.state.value.busy)
        assertTrue("still there when Settings comes back", account.state.value.accountDeletionFailed)
        account.clearDeletionFailure()
        assertFalse(account.state.value.accountDeletionFailed)
        assertTrue(credentials.isSignedIn)
    }

    @Test
    fun `a deletion whose answer was lost is finished by the 401 of the next try`() = runBlocking {
        signedInWithEverything()
        val account = controller()
        // The server deletes the account; its answer never arrives.
        answer = { throw SocketTimeoutException("read timed out") }
        assertFalse(account.deleteAccount())
        assertTrue(credentials.isSignedIn)
        assertTrue(account.state.value.accountDeletionFailed)

        // No account behind the token any more, nor behind the refresh token.
        answer = { refused }
        assertTrue(account.deleteAccount())

        assertForgotten(account)
        assertEquals(listOf(true), observer.signedOut)
    }

    @Test
    fun `after a lost deletion answer the next refresh finishes the deletion too`() = runBlocking {
        signedInWithEverything()
        val account = controller()
        answer = { FakeResponse(502) }
        assertFalse(account.deleteAccount())

        answer = { refused }
        account.refresh()

        assertForgotten(account)
    }

    @Test
    fun `a 401 with no earlier try only ends the session`() = runBlocking {
        signedInWithEverything()
        answer = { refused }
        val account = controller()

        assertFalse(account.deleteAccount())

        assertFalse("the server ended the session", credentials.isSignedIn)
        assertTrue(account.state.value.phase is AccountController.Phase.SignedOut)
        assertEquals(R.string.account_error_session_expired, account.state.value.errorMessage)
        assertNull("nothing says the account is gone", account.state.value.notice)
        assertNotNull("the account may still exist", legal.current())
        assertTrue(learned.values.isNotEmpty())
        assertTrue(usage.current().isKnown)
        assertEquals(listOf(false), observer.signedOut)
    }

    @Test
    fun `a deletion that never left the phone does not turn a later 401 into a deletion`() = runBlocking {
        signedInWithEverything()
        val account = controller()
        answer = { throw ConnectException("no network") }
        assertFalse(account.deleteAccount())

        answer = { refused }
        assertFalse(account.deleteAccount())

        assertNotNull(legal.current())
        assertTrue(learned.values.isNotEmpty())
        assertNull(account.state.value.notice)
    }

    @Test
    fun `only a failure without a clear answer may have deleted the account`() {
        listOf(ApiError.TimedOut, ApiError.Server, ApiError.ProviderTimeout, ApiError.MalformedResponse).forEach {
            assertTrue(it.toString(), AccountController.mayHaveReachedServer(it))
        }
        listOf(ApiError.Offline, ApiError.Unauthorized, ApiError.RateLimited(null), ApiError.NotFound, null).forEach {
            assertFalse(it.toString(), AccountController.mayHaveReachedServer(it))
        }
    }

    // ------------------------------------------- signed in, without consent

    @Test
    fun `after a withdrawal the account can be deleted without consenting again`() = runBlocking {
        signedInWithEverything()
        lateinit var account: AccountController
        var withdrawingWhileSent = false
        answer = { request ->
            when (request.path) {
                "/api/v1/me/consents" -> {
                    withdrawingWhileSent = account.state.value.withdrawingConsent
                    FakeResponse(200, """{"ok":true}""")
                }
                "/api/v1/me/delete" -> FakeResponse(200, """{"deleted":true}""")
                else -> FakeResponse(404)
            }
        }
        account = controller()

        assertNull(account.withdrawLegalConsent())
        assertTrue(withdrawingWhileSent)
        assertFalse(account.state.value.withdrawingConsent)
        // The consent screen is up, for an account that is still signed in.
        assertFalse(account.state.value.hasAcceptedLegal)
        assertTrue(account.state.value.isSignedIn)

        assertTrue(account.deleteAccount())

        assertEquals(
            "no consent was sent on the way",
            listOf("DELETE /api/v1/me/consents", "POST /api/v1/me/delete"),
            server.requests.map { "${it.method} ${it.path}" }
        )
        assertForgotten(account)
    }

    @Test
    fun `after a withdrawal the user can sign out without consenting again`() = runBlocking {
        signedInWithEverything()
        answer = { request ->
            when (request.path) {
                "/api/v1/me/consents" -> FakeResponse(200, """{"ok":true}""")
                "/api/v1/auth/logout" -> FakeResponse(200, "{}")
                else -> FakeResponse(404)
            }
        }
        val account = controller()
        assertNull(account.withdrawLegalConsent())

        account.signOut()

        assertFalse(credentials.isSignedIn)
        assertTrue(account.state.value.phase is AccountController.Phase.SignedOut)
        assertFalse("the consent screen stays, for a signed-out user", account.state.value.hasAcceptedLegal)
        assertEquals(0, server.requests.count { it.method == "POST" && it.path == "/api/v1/me/consents" })
        assertEquals(1, server.count("/api/v1/auth/logout"))
    }

    // ------------------------------------------------------- consent upload

    @Test
    fun `the consent upload outlives the consent screen`() = runBlocking {
        // A signed-in user back on the consent screen: no consent on the phone.
        val arrived = CountDownLatch(1)
        val release = CountDownLatch(1)
        answer = { request ->
            if (request.path == "/api/v1/me/consents") {
                arrived.countDown()
                release.await(5, TimeUnit.SECONDS)
            }
            consentRecorded
        }
        val app = CoroutineScope(SupervisorJob())
        val account = controller(background = app)
        val screen = CoroutineScope(Job())

        screen.launch { account.acceptLegal("ru") }
        withContext(Dispatchers.IO) { assertTrue(arrived.await(5, TimeUnit.SECONDS)) }
        // Continue flipped the flag, so the consent screen left and took its scope with it.
        assertTrue(account.state.value.hasAcceptedLegal)
        screen.cancel()
        release.countDown()
        app.coroutineContext[Job]!!.children.forEach { it.join() }

        assertEquals(1, server.count("/api/v1/me/consents"))
        assertFalse("the server has it", legal.current()!!.pendingSync)
        app.cancel()
    }

    @Test
    fun `an acceptance the server never got goes again with the next refresh`() = runBlocking {
        val account = controller()
        // Accepted offline: the record waits on the phone.
        answer = { throw ConnectException("no network") }
        account.acceptLegal("ru")
        assertTrue(legal.current()!!.pendingSync)

        answer = { request ->
            when (request.path) {
                "/api/v1/me" -> accountLoaded
                "/api/v1/me/consents" -> consentRecorded
                else -> FakeResponse(404)
            }
        }
        account.refresh()

        assertEquals("the offline try, then this one", 2, server.count("/api/v1/me/consents"))
        assertFalse(legal.current()!!.pendingSync)
        assertTrue(account.state.value.hasAcceptedLegal)

        // Sent: the next refresh has nothing to send.
        account.refresh()
        assertEquals(2, server.count("/api/v1/me/consents"))
    }

    @Test
    fun `offline the account stays`() = runBlocking {
        signedInWithEverything()
        answer = { throw IOException("no route") }
        val account = controller()

        assertFalse(account.deleteAccount())
        assertTrue(credentials.isSignedIn)
        assertTrue(account.state.value.hasAcceptedLegal)
    }

    // ----------------------------------------------------- consent withdrawal

    @Test
    fun `withdrawing the consent tells the server, then shows the consent screen`() = runBlocking {
        signedInWithEverything()
        answer = { FakeResponse(200, """{"ok":true}""") }
        val account = controller()

        assertNull(account.withdrawLegalConsent())

        val request = server.requests.single()
        assertEquals("DELETE", request.method)
        assertEquals("/api/v1/me/consents", request.path)
        assertEquals("Bearer access-1", request.header("Authorization"))
        assertFalse(account.state.value.hasAcceptedLegal)
        assertNull(legal.current())
        assertFalse(legal.hasAcceptedLatest())
        assertTrue("the account stays", credentials.isSignedIn)
        assertTrue(learned.values.isNotEmpty())
    }

    @Test
    fun `a withdrawal the server did not get keeps the consent`() = runBlocking {
        signedInWithEverything()
        answer = { throw IOException("no route") }
        val account = controller()

        assertNotNull(account.withdrawLegalConsent())
        assertTrue(account.state.value.hasAcceptedLegal)
        assertNotNull(legal.current())
        assertFalse(account.state.value.busy)
    }

    // ------------------------------------------------------ consent required

    @Test
    fun `a refused AI request brings the consent screen back for the newest versions`() = runBlocking {
        signedInWithEverything()
        val account = controller()
        val bumped = LegalConfigDto.PRODUCTION.copy(termsVersion = "2026-11-01", privacyVersion = "2026-11-01")
        // The keyboard learned of the new versions first.
        legal.rememberConfig(bumped)

        account.consentRequired()

        assertFalse(account.state.value.hasAcceptedLegal)
        assertEquals(bumped, account.state.value.legalConfig)
        assertNull(legal.current())
    }

    @Test
    fun `the consent screen asks the server for its versions once after a refusal`() = runBlocking {
        signedInWithEverything()
        answer = {
            FakeResponse(
                200,
                """{"legal":{"terms_version":"2026-12-01","privacy_version":"2026-12-01",""" +
                    """"terms_url":"https://ai-reply.kz/offer","privacy_url":"https://ai-reply.kz/privacy"}}"""
            )
        }
        val account = controller()

        account.recheckLegalVersions()
        assertEquals("nothing to recheck before a refusal", 0, server.requests.size)

        account.consentRequired()
        account.recheckLegalVersions()
        account.recheckLegalVersions()

        assertEquals(1, server.count("/api/v1/config"))
        assertEquals("2026-12-01", account.state.value.legalConfig.termsVersion)
        assertEquals("2026-12-01", legal.latestConfig().termsVersion)
        assertFalse(account.state.value.hasAcceptedLegal)
    }

    @Test
    fun `accepting again clears the deleted notice`() = runBlocking {
        signedInWithEverything()
        answer = { FakeResponse(200, """{"deleted":true}""") }
        val account = controller()
        account.deleteAccount()

        account.acceptLegal("ru")

        assertTrue(account.state.value.hasAcceptedLegal)
        assertNull(account.state.value.notice)
        assertTrue(legal.hasAcceptedLatest())
    }
}
