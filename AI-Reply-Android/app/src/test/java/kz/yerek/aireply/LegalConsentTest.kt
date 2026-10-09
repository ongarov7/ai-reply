package kz.yerek.aireply

import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.TestScope
import kz.yerek.aireply.ai.AIConfiguration
import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.ai.AIReplyException
import kz.yerek.aireply.ai.AIReplyService
import kz.yerek.aireply.ai.AccountReplyTransport
import kz.yerek.aireply.ai.ReplyDraftNormalizer
import kz.yerek.aireply.data.account.ApiClient
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.LegalConfigDto
import kz.yerek.aireply.data.legal.LegalConsentStore
import kz.yerek.aireply.domain.model.ReplyConfiguration
import kz.yerek.aireply.keyboard.reply.ReplySessionController
import kz.yerek.aireply.support.FakeServer
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

/**
 * No AI request without the current terms, privacy policy and AI processing
 * accepted on this phone - from the app or the keyboard - and the server's
 * CONSENT_REQUIRED sends the user back to the consent screen.
 *
 * Келісімсіз AI сұранысы жоқ: пернетақтадан да, қолданбадан да.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class LegalConsentTest {

    private val bumped = LegalConfigDto.PRODUCTION.copy(termsVersion = "2026-11-01", privacyVersion = "2026-11-01")

    // ------------------------------------------------------------- the store

    @Test
    fun `the latest versions are the built-in ones until the server says otherwise`() {
        val store = LegalConsentStore(InMemoryPreferences())
        assertEquals(LegalConfigDto.PRODUCTION, store.latestConfig())
        assertEquals("2026-10-08", store.latestConfig().termsVersion)
        assertEquals("2026-10-08", store.latestConfig().privacyVersion)
        assertFalse(store.hasAcceptedLatest())

        store.accept(LegalConfigDto.PRODUCTION, "kk", "1.0")
        assertTrue(store.hasAcceptedLatest())
    }

    @Test
    fun `new versions from the server need a new acceptance, on the next launch too`() {
        val prefs = InMemoryPreferences()
        val store = LegalConsentStore(prefs)
        store.accept(LegalConfigDto.PRODUCTION, "ru", "1.0")

        store.rememberConfig(bumped)
        assertFalse(store.hasAcceptedLatest())

        val nextLaunch = LegalConsentStore(prefs)
        assertEquals(bumped, nextLaunch.latestConfig())
        assertFalse(nextLaunch.hasAcceptedLatest())
        nextLaunch.accept(nextLaunch.latestConfig(), "ru", "1.0")
        assertTrue(nextLaunch.hasAcceptedLatest())
    }

    @Test
    fun `clearing forgets the consent but keeps the published versions`() {
        val prefs = InMemoryPreferences()
        val store = LegalConsentStore(prefs)
        store.rememberConfig(bumped)
        store.accept(bumped, "en", "1.0")

        store.clear()

        assertNull(store.current())
        assertFalse(store.hasAcceptedLatest())
        assertEquals(bumped, store.latestConfig())
        assertNull("gone from disk too", LegalConsentStore(prefs).current())
    }

    @Test
    fun `an older stored config without the new fields still reads`() {
        val prefs = InMemoryPreferences()
        prefs.edit().putString(
            "legal.latestConfig.v1",
            """{"terms_version":"2026-09-19","privacy_version":"2026-09-19","terms_url":"t","privacy_url":"p"}"""
        ).apply()
        val config = LegalConsentStore(prefs).latestConfig()
        assertEquals("2026-09-19", config.termsVersion)
        assertEquals("", config.supportUrl)
    }

    // ------------------------------------------------------- the readiness

    @Test
    fun `a request needs an account first, then the consent`() {
        var signedIn = false
        var consent = false
        var told = 0
        val configuration = AIConfiguration(
            hasLegalConsent = { consent },
            onConsentRequired = { told++ },
            isAccountSignedIn = { signedIn }
        )
        assertEquals(AIReplyError.AuthenticationFailed, configuration.notReadyReason)

        signedIn = true
        assertEquals(AIReplyError.ConsentRequired, configuration.notReadyReason)
        assertFalse(configuration.isReady)
        try {
            configuration.checkReady()
            fail("no request without the consent")
        } catch (refused: AIReplyException) {
            assertEquals(AIReplyError.ConsentRequired, refused.error)
        }
        assertEquals("the app is told, so it shows the consent screen", 1, told)

        consent = true
        assertTrue(configuration.isReady)
        configuration.checkReady()
        assertEquals(1, told)
    }

    @Test
    fun `only a missing consent is handed to the app`() {
        var told = 0
        val configuration = AIConfiguration(onConsentRequired = { told++ }, isAccountSignedIn = { true })
        configuration.requestFailed(AIReplyError.QuotaExhausted)
        configuration.requestFailed(AIReplyError.AuthenticationFailed)
        assertEquals(0, told)
        configuration.requestFailed(AIReplyError.ConsentRequired)
        assertEquals(1, told)
    }

    @Test
    fun `the keyboard refuses a reply without the consent and never reaches the network`() {
        var told = 0
        var transports = 0
        val service = AIReplyService(
            configuration = AIConfiguration(
                hasLegalConsent = { false },
                onConsentRequired = { told++ },
                isAccountSignedIn = { true }
            ),
            nameTemplate = { template, _ -> template.id },
            accountTransport = { _, _ -> transports++; error("no request without the consent") }
        )
        val scope = TestScope(UnconfinedTestDispatcher())
        val controller = ReplySessionController(scope, service, ReplyDraftNormalizer()).apply {
            configuration = ReplyConfiguration.INITIAL
        }
        controller.open(ReplyConfiguration.INITIAL.visibleTemplates.first(), "Привет! Когда встречаемся?", from = null, error = null)

        controller.generate()

        assertEquals(AIReplyError.ConsentRequired, controller.flow.error)
        assertEquals(0, transports)
        assertEquals(1, told)
    }

    // ------------------------------------------------------ the server's word

    @Test
    fun `CONSENT_REQUIRED and the monthly limit are their own errors`() {
        assertEquals(
            ApiError.ConsentRequired,
            ApiClient.mapServerError(403, FakeServer.envelope("CONSENT_REQUIRED"))
        )
        assertEquals(
            ApiError.MonthlyLimitReached("2026-11-01T00:00:00+05:00"),
            ApiClient.mapServerError(
                429,
                """{"error":{"code":"MONTHLY_LIMIT_REACHED","message":"x","details":{"resets_at":"2026-11-01T00:00:00+05:00"}}}"""
            )
        )
        assertTrue(
            "the daily limit stays the daily limit",
            ApiClient.mapServerError(429, FakeServer.envelope("DAILY_LIMIT_REACHED")) is ApiError.DailyLimitReached
        )
    }

    @Test
    fun `the keyboard shows the consent and monthly messages, not the daily one`() {
        assertEquals(AIReplyError.ConsentRequired, AccountReplyTransport.map(ApiError.ConsentRequired))
        assertEquals(AIReplyError.MonthlyQuotaExhausted, AccountReplyTransport.map(ApiError.MonthlyLimitReached(null)))
        assertEquals(AIReplyError.QuotaExhausted, AccountReplyTransport.map(ApiError.DailyLimitReached(5, 5, null)))
    }
}
