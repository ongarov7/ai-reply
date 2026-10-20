package kz.yerek.aireply

import kotlinx.coroutines.runBlocking
import kz.yerek.aireply.ai.AIConfiguration
import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.ai.AIReplyException
import kz.yerek.aireply.ai.AIReplyService
import kz.yerek.aireply.ai.AccountComposeTransport
import kz.yerek.aireply.ai.AccountReplyTransport
import kz.yerek.aireply.ai.ComposeService
import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.data.account.AccountUsageCache
import kz.yerek.aireply.data.account.ApiClient
import kz.yerek.aireply.data.settings.SettingsStore
import kz.yerek.aireply.domain.model.GrammaticalGender
import kz.yerek.aireply.domain.model.ReplyConfiguration
import kz.yerek.aireply.support.FakeCredentials
import kz.yerek.aireply.support.FakeResponse
import kz.yerek.aireply.support.FakeServer
import kz.yerek.aireply.support.sessionOn
import kz.yerek.aireply.ui.feature.account.ExhaustedRules
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The exhausted sheet (contract §3.9): when it shows, and when it offers
 * «Докупить ответы».
 *
 * Лимит таусылды парағы: қашан шығады, сатып алу қашан ұсынылады.
 */
class ExhaustedSheetTest {

    private fun limitBody(canBuy: Boolean) =
        """{"error":{"code":"DAILY_LIMIT_REACHED","message":"x","details":{"daily_limit":5,"used_today":5,
           "welcome_remaining":0,"credits_remaining":0,"pass_active":false,"can_buy":$canBuy}}}"""

    private fun server(body: String) = FakeServer { FakeResponse(429, body) }

    private fun replyService(server: FakeServer) = AIReplyService(
        configuration = AIConfiguration { true },
        nameTemplate = { template, _ -> template.id },
        accountTransport = { _, context ->
            AccountReplyTransport(
                baseUrl = "https://example.test",
                session = sessionOn(server, FakeCredentials("a", "r")),
                usageCache = AccountUsageCache(SettingsStore(InMemoryPreferences())),
                context = context,
                clientFactory = { url, _ -> ApiClient(url, openConnection = server::open) }
            )
        }
    )

    @Test
    fun `only spent replies open the sheet`() {
        assertTrue(ExhaustedRules.isLimit(AIReplyError.QuotaExhausted))
        assertTrue(ExhaustedRules.isLimit(AIReplyError.MonthlyQuotaExhausted))
        listOf(
            AIReplyError.RateLimited, AIReplyError.Offline, AIReplyError.ServiceUnavailable,
            AIReplyError.VoiceLimitReached, AIReplyError.ConsentRequired
        ).forEach { assertFalse(it.toString(), ExhaustedRules.isLimit(it)) }
    }

    @Test
    fun `Buy replies needs the platform's purchases and the server's can_buy`() {
        assertTrue(ExhaustedRules.offersPurchase(purchasesOffered = true, canBuy = true))
        assertFalse(ExhaustedRules.offersPurchase(purchasesOffered = false, canBuy = true))
        assertFalse(ExhaustedRules.offersPurchase(purchasesOffered = true, canBuy = false))
        assertFalse(ExhaustedRules.offersPurchase(purchasesOffered = false, canBuy = false))
    }

    @Test
    fun `the text never promises what cannot be bought, and fits the sender`() {
        assertEquals(R.string.exhausted_body_plans, ExhaustedRules.body(offersPurchase = false, GrammaticalGender.FEMALE))
        assertEquals(R.string.exhausted_body_male, ExhaustedRules.body(offersPurchase = true, GrammaticalGender.MALE))
        assertEquals(R.string.exhausted_body_female, ExhaustedRules.body(offersPurchase = true, GrammaticalGender.FEMALE))
        assertEquals(R.string.exhausted_body, ExhaustedRules.body(offersPurchase = true, null))
        assertEquals(R.string.exhausted_body, ExhaustedRules.body(offersPurchase = true, GrammaticalGender.UNSPECIFIED))
    }

    @Test
    fun `a refusal reaches the screen as spent replies, with can_buy`() = runBlocking {
        val template = ReplyConfiguration.INITIAL.visibleTemplates.first()
        val request = AIReplyService.Request("Привет", template, ReplyConfiguration.INITIAL, AppLanguage.RUSSIAN)

        val selling = runCatching { replyService(server(limitBody(canBuy = true))).generate(request) }
            .exceptionOrNull() as AIReplyException
        assertEquals(AIReplyError.QuotaExhausted, selling.error)
        assertTrue(selling.canBuy)

        val notSelling = runCatching { replyService(server(limitBody(canBuy = false))).generate(request) }
            .exceptionOrNull() as AIReplyException
        assertEquals(AIReplyError.QuotaExhausted, notSelling.error)
        assertFalse(notSelling.canBuy)
    }

    @Test
    fun `Write is refused the same way`() = runBlocking {
        val server = server("""{"error":{"code":"MONTHLY_LIMIT_REACHED","details":{"can_buy":true}}}""")
        val service = ComposeService(
            configuration = AIConfiguration { true },
            accountTransport = {
                AccountComposeTransport(
                    baseUrl = "https://example.test",
                    session = sessionOn(server, FakeCredentials("a", "r")),
                    usageCache = AccountUsageCache(SettingsStore(InMemoryPreferences())),
                    appVersion = "1.0",
                    grammaticalGender = { null },
                    clientFactory = { url, _ -> ApiClient(url, openConnection = server::open) }
                )
            }
        )
        val failure = runCatching { service.compose(ComposeService.Request("Поздравь коллегу", AppLanguage.RUSSIAN)) }
            .exceptionOrNull() as AIReplyException
        assertEquals(AIReplyError.MonthlyQuotaExhausted, failure.error)
        assertTrue(failure.canBuy)
    }
}
