package kz.yerek.aireply

import kotlinx.coroutines.runBlocking
import kz.yerek.aireply.ai.AILimits
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.data.account.AccountObserver
import kz.yerek.aireply.data.account.AccountService
import kz.yerek.aireply.data.account.AccountUsageCache
import kz.yerek.aireply.data.account.ApiClient
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

    private fun controller() = AccountController(
        service = service,
        credentials = credentials,
        usageCache = usage,
        legalConsentStore = legal,
        observer = observer,
        learnedWords = learned
    )

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
        assertTrue(learned.values.isNotEmpty())
        assertTrue(usage.current().isKnown)
        assertTrue(observer.signedOut.isEmpty())
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
