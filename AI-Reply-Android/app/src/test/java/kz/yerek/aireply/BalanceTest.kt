package kz.yerek.aireply

import kotlinx.serialization.json.Json
import kz.yerek.aireply.data.account.AccountDto
import kz.yerek.aireply.data.account.AccountUsageCache
import kz.yerek.aireply.data.account.ApiClient
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.ReplyResponseDto
import kz.yerek.aireply.data.account.ServerConfigDto
import kz.yerek.aireply.data.account.UsageDto
import kz.yerek.aireply.data.account.WelcomeDto
import kz.yerek.aireply.data.settings.SettingsStore
import kz.yerek.aireply.ui.feature.account.BalanceSummary
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.Instant

/**
 * The balance beyond the plan (contract §3.2): what the server sends, and the
 * rows the screens make of it.
 *
 * Баланс: сервер жіберген сандар және экрандағы жолдар.
 */
class BalanceTest {

    private val json = Json { ignoreUnknownKeys = true }
    private val now = Instant.parse("2026-10-20T10:00:00Z")

    private fun usage(body: String): UsageDto = json.decodeFromString(UsageDto.serializer(), body)

    // ----------------------------------------------------------- parsing

    @Test
    fun `the whole balance is read from the usage`() {
        val usage = usage(
            """{"daily_limit":5,"used_today":5,"remaining_today":0,"monthly_limit":0,"used_month":12,
               "resets_at":"2026-10-20T19:00:00Z","timezone":"Asia/Almaty",
               "welcome":{"total":50,"remaining":35},"credits":7,
               "pass":{"expires_at":"2026-10-21T15:00:00+05:00","remaining":40},
               "remaining_total":82,"bucket":"welcome","something_new":{"a":1}}"""
        )
        assertEquals(WelcomeDto(total = 50, remaining = 35), usage.welcome)
        assertEquals(7, usage.credits)
        assertEquals("2026-10-21T15:00:00+05:00", usage.pass?.expiresAt)
        assertEquals(40, usage.pass?.remaining)
        assertEquals(82, usage.remainingTotal)
        assertEquals("welcome", usage.bucket)
        assertEquals("remaining_today keeps its plan-only meaning", 0, usage.remainingToday)
    }

    @Test
    fun `an older server sends no balance and nothing breaks`() {
        val usage = usage("""{"daily_limit":7,"used_today":1,"remaining_today":6,"resets_at":"2026-10-20T19:00:00Z"}""")
        assertNull(usage.welcome)
        assertNull(usage.credits)
        assertNull(usage.pass)
        assertNull(usage.remainingTotal)
        assertFalse("the screens keep the plan's own count", BalanceSummary.of(usage, now = now).hasBalance)

        val reply = json.decodeFromString(ReplyResponseDto.serializer(), """{"reply":"Ok"}""")
        assertFalse("a reply without usage is not a zero balance", reply.usage.isKnown)
    }

    @Test
    fun `the account answer carries it too`() {
        val account = json.decodeFromString(
            AccountDto.serializer(),
            """{"user":{"id":"u1"},"profile":{},"subscription":{"plan":{"id":"p","code":"free"}},
               "usage":{"daily_limit":5,"remaining_today":5,"resets_at":"x","welcome":{"total":50,"remaining":50},
               "credits":0,"remaining_total":55}}"""
        )
        assertEquals(55, account.usage.remainingTotal)
        assertEquals(50, account.usage.welcome?.remaining)
    }

    @Test
    fun `the new features default to off`() {
        val old = json.decodeFromString(ServerConfigDto.serializer(), """{"features":{"compose":true}}""").features!!
        assertFalse(old.welcomeBonus || old.credits || old.idempotentGeneration || old.permissionDiagnostics)
        assertFalse(old.supportChat || old.iapAndroid || old.pushReminders)
        val current = json.decodeFromString(
            ServerConfigDto.serializer(),
            """{"features":{"welcome_bonus":true,"credits":true,"idempotent_generation":true,
               "permission_diagnostics":true,"push_reminders":false,"support_chat":true,"iap_ios":true,"iap_android":true}}"""
        ).features!!
        assertTrue(current.welcomeBonus && current.credits && current.idempotentGeneration)
        assertTrue(current.permissionDiagnostics && current.supportChat && current.iapAndroid)
    }

    @Test
    fun `a limit refusal says whether more can be bought`() {
        val body = """{"error":{"code":"DAILY_LIMIT_REACHED","message":"x","details":{"daily_limit":5,"used_today":5,
            "welcome_remaining":0,"credits_remaining":0,"pass_active":false,"can_buy":true}}}"""
        assertEquals(ApiError.DailyLimitReached(5, 5, null, canBuy = true), ApiClient.mapServerError(429, body))
        val monthly = """{"error":{"code":"MONTHLY_LIMIT_REACHED","message":"x","details":{"can_buy":false}}}"""
        assertEquals(ApiError.MonthlyLimitReached(null, canBuy = false), ApiClient.mapServerError(429, monthly))
        assertEquals(
            "an older server says nothing: nothing to buy",
            ApiError.DailyLimitReached(7, 7, null, canBuy = false),
            ApiClient.mapServerError(429, """{"error":{"code":"DAILY_LIMIT_REACHED","details":{"daily_limit":7,"used_today":7}}}""")
        )
    }

    // ----------------------------------------------------------- summary

    @Test
    fun `free replies head the card while some are left`() {
        val summary = BalanceSummary.of(
            UsageDto(dailyLimit = 5, remainingToday = 0, resetsAt = "x", welcome = WelcomeDto(50, 35), credits = 0, remainingTotal = 35),
            now = now
        )
        assertTrue(summary.hasBalance)
        assertEquals(35, summary.availableNow)
        assertEquals(BalanceSummary.Part(35, 50), summary.welcomeHeadline)
        assertNull("no purchased row for zero", summary.purchased)
        assertNull(summary.pass)

        val spent = BalanceSummary.of(
            UsageDto(dailyLimit = 5, remainingToday = 2, resetsAt = "x", welcome = WelcomeDto(50, 0), remainingTotal = 2),
            now = now
        )
        assertEquals("the row stays, as a fact", BalanceSummary.Part(0, 50), spent.welcome)
        assertNull("but it no longer heads the card", spent.welcomeHeadline)
    }

    @Test
    fun `purchased replies and a running pass get their rows`() {
        val summary = BalanceSummary.of(
            UsageDto(
                dailyLimit = 5, resetsAt = "x", credits = 12, remainingTotal = 52,
                pass = kz.yerek.aireply.data.account.PassDto("2026-10-21T15:00:00+05:00", 40)
            ),
            now = now
        )
        assertEquals(12, summary.purchased)
        assertEquals(40, summary.pass?.remaining)
        assertEquals(Instant.parse("2026-10-21T10:00:00Z"), summary.pass?.until?.toInstant())
        assertNull("no welcome grant, no row", summary.welcome)
    }

    @Test
    fun `nothing is shown that is not true`() {
        val ended = BalanceSummary.of(
            UsageDto(
                resetsAt = "x", remainingTotal = -3,
                pass = kz.yerek.aireply.data.account.PassDto("2026-10-20T09:00:00Z", 5)
            ),
            now = now
        )
        assertNull("an ended pass has no row", ended.pass)
        assertEquals("a racing request never shows below zero", 0, ended.availableNow)
        assertNull(
            "an unreadable end has no row",
            BalanceSummary.of(
                UsageDto(resetsAt = "x", remainingTotal = 1, pass = kz.yerek.aireply.data.account.PassDto("soon", 1)),
                now = now
            ).pass
        )
        assertNull(
            "a grant of nothing is no grant",
            BalanceSummary.of(UsageDto(resetsAt = "x", remainingTotal = 0, welcome = WelcomeDto(0, 0)), now = now).welcome
        )
    }

    @Test
    fun `the last known total renders before the server answers`() {
        val settings = SettingsStore(InMemoryPreferences())
        val cache = AccountUsageCache(settings)
        cache.store(UsageDto(dailyLimit = 5, remainingToday = 1, resetsAt = "x", remainingTotal = 36))
        assertEquals(36, cache.current().remainingTotal)

        val before = BalanceSummary.of(UsageDto.UNKNOWN, cache.current(), now)
        assertTrue(before.hasBalance)
        assertEquals(36, before.availableNow)

        cache.store(UsageDto.UNKNOWN)
        assertEquals("an answer without usage changes nothing", 36, cache.current().remainingTotal)

        cache.store(UsageDto(dailyLimit = 5, remainingToday = 4, resetsAt = "x"))
        assertNull("an older server's answer has no total", cache.current().remainingTotal)
        assertFalse(BalanceSummary.of(UsageDto.UNKNOWN, cache.current(), now).hasBalance)
    }
}
