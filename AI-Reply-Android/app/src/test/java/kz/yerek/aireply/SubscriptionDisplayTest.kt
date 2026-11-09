package kz.yerek.aireply

import kotlinx.serialization.json.Json
import kz.yerek.aireply.billing.IapRecommendationDto
import kz.yerek.aireply.billing.OfferedProduct
import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.data.account.AccountUsageCache
import kz.yerek.aireply.data.account.PlanDto
import kz.yerek.aireply.data.account.SubscriptionDto
import kz.yerek.aireply.data.account.UsageDto
import kz.yerek.aireply.ui.feature.account.UsageSummary
import kz.yerek.aireply.ui.feature.account.formatExpiry
import kz.yerek.aireply.ui.feature.account.formatSpend
import kz.yerek.aireply.ui.feature.account.recommendedOffer
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle

/**
 * The plan screen without Play Billing: no price and no Choose in a release
 * build, and the end date in the app's language.
 *
 * Тариф экраны: релизде баға мен «Таңдау» жоқ, күн қолданба тілінде.
 */
class SubscriptionDisplayTest {

    private val json = Json { ignoreUnknownKeys = true }

    @Test
    fun `purchasable is off unless the server says so`() {
        val older = json.decodeFromString(PlanDto.serializer(), """{"id":"p","code":"pro"}""")
        assertFalse(older.purchasable)
        val current = json.decodeFromString(PlanDto.serializer(), """{"id":"p","code":"pro","purchasable":true}""")
        assertTrue(current.purchasable)
    }

    private val baseRecommendation = IapRecommendationDto("base_after_pack_4", 4, 396_000, "KZT", 99_000, "play.base.month")
    private val balance = UsageDto(resetsAt = "x", remainingTotal = 3)

    private fun baseOffer(
        id: String = "play.base.month",
        period: String = "month",
        kind: String = "monthly",
        monthly: Int = 1000,
        mismatch: Boolean = false
    ) = OfferedProduct(
        code = "base_monthly", kind = OfferedProduct.Kind.SUBSCRIPTION, units = 0, durationHours = 0, storeProductId = id, name = "Base",
        price = "990 ₸", planId = "base", billingPeriod = period, entitlementKind = kind, monthlyLimit = monthly, hasPriceMismatch = mismatch
    )

    @Test
    fun `the recommendation points at the monthly Base that can be bought`() {
        val offer = baseOffer()
        val pro = baseOffer(id = "play.pro.month", kind = "unlimited", monthly = 0)
        assertEquals(offer, recommendedOffer(baseRecommendation, listOf(pro, offer), balance, null))
        assertNull("not the yearly one", recommendedOffer(baseRecommendation, listOf(baseOffer(period = "year")), balance, null))
        assertNull("not a price being updated", recommendedOffer(baseRecommendation, listOf(baseOffer(mismatch = true)), balance, null))
        assertNull("not without an allowance to name", recommendedOffer(baseRecommendation, listOf(baseOffer(monthly = 0)), balance, null))
        assertNull("not another product", recommendedOffer(baseRecommendation, listOf(pro), balance, null))
    }

    @Test
    fun `only an account without a paid plan is recommended Base`() {
        val offers = listOf(baseOffer())
        fun on(status: String, free: Boolean) = SubscriptionDto(status = status, plan = PlanDto(id = "p", code = "c", isFree = free))
        assertNull(recommendedOffer(baseRecommendation, offers, balance, on("active", free = false)))
        assertNull(recommendedOffer(baseRecommendation, offers, UsageDto(unlimited = true, resetsAt = "x"), null))
        assertEquals(offers.single(), recommendedOffer(baseRecommendation, offers, balance, on("active", free = true)))
        assertEquals(offers.single(), recommendedOffer(baseRecommendation, offers, balance, on("expired", free = false)))
    }

    @Test
    fun `what was spent is written with the tenge sign in every language`() {
        AppLanguage.entries.forEach { language ->
            val text = formatSpend(396_000, "KZT", language.locale)
            assertTrue(text, text.contains("₸"))
            assertEquals(text, "396000", text.filter(Char::isDigit))
        }
    }

    @Test
    fun `what was spent in another currency keeps its own sign, and an unknown one its code`() {
        assertEquals("\$12.50", formatSpend(1250, "USD", AppLanguage.ENGLISH.locale))
        assertEquals("12.50 ???", formatSpend(1250, "???", AppLanguage.ENGLISH.locale))
    }

    @Test
    fun `the end date is written in the app's language, not as ISO`() {
        val zone = ZoneId.of("Asia/Almaty")
        AppLanguage.entries.forEach { language ->
            val expected = DateTimeFormatter.ofLocalizedDate(FormatStyle.MEDIUM)
                .withLocale(language.locale)
                .format(LocalDate.of(2026, 11, 8))
            assertEquals(language.code, expected, formatExpiry("2026-11-08T10:00:00Z", language.locale, zone))
        }
        val english = formatExpiry("2026-11-08T10:00:00Z", AppLanguage.ENGLISH.locale, zone)!!
        assertFalse(english.contains("2026-11-08"))
        assertTrue(english.contains("2026"))
    }

    @Test
    fun `the date is the one on this phone's calendar`() {
        // 22:30 UTC is already the next morning in Almaty.
        val formatted = formatExpiry("2026-11-08T22:30:00Z", AppLanguage.ENGLISH.locale, ZoneId.of("Asia/Almaty"))
        val expected = DateTimeFormatter.ofLocalizedDate(FormatStyle.MEDIUM)
            .withLocale(AppLanguage.ENGLISH.locale)
            .format(LocalDate.of(2026, 11, 9))
        assertEquals(expected, formatted)
    }

    @Test
    fun `a plain date reads too, and nothing readable shows nothing`() {
        val zone = ZoneId.of("Asia/Almaty")
        val expected = DateTimeFormatter.ofLocalizedDate(FormatStyle.MEDIUM)
            .withLocale(AppLanguage.RUSSIAN.locale)
            .format(LocalDate.of(2026, 12, 31))
        assertEquals(expected, formatExpiry("2026-12-31", AppLanguage.RUSSIAN.locale, zone))
        assertNull(formatExpiry("", AppLanguage.RUSSIAN.locale, zone))
        assertNull(formatExpiry("soon", AppLanguage.RUSSIAN.locale, zone))
    }

    // ------------------------------------------------------------- usage

    @Test
    fun `a daily plan shows what is left today`() {
        val summary = UsageSummary.of(UsageDto(dailyLimit = 10, usedToday = 3, remainingToday = 7))!!
        assertEquals(UsageSummary(7, 10, UsageSummary.Period.DAY), summary)
        assertEquals(3, summary.used)
        assertTrue(summary.hasReplies)
        assertFalse("spent is a fact, shown without an error", UsageSummary.of(UsageDto(dailyLimit = 5, usedToday = 5))!!.hasReplies)
    }

    @Test
    fun `a plan without a daily allowance is never 0 of 0`() {
        assertEquals(
            "counted by the month",
            UsageSummary(260, 300, UsageSummary.Period.MONTH),
            UsageSummary.of(UsageDto(monthlyLimit = 300, usedMonth = 40))
        )
        assertEquals(
            "a month overspent by a racing request still reads 0, not below",
            0,
            UsageSummary.of(UsageDto(monthlyLimit = 300, usedMonth = 310))!!.remaining
        )
        assertNull("not counted: no number at all", UsageSummary.of(UsageDto(timezone = "Asia/Almaty")))
    }

    @Test
    fun `before the server answers the cached day stands in`() {
        val cached = AccountUsageCache.Snapshot(dailyLimit = 10, usedToday = 2, remainingToday = 8, planCode = "free")
        assertEquals(UsageSummary(8, 10, UsageSummary.Period.DAY), UsageSummary.of(UsageDto.UNKNOWN, cached))
        assertNull(
            "nothing cached either",
            UsageSummary.of(UsageDto.UNKNOWN, AccountUsageCache.Snapshot(0, 0, 0, ""))
        )
        assertEquals(
            "the server's answer wins over the cache",
            UsageSummary(1, 3, UsageSummary.Period.DAY),
            UsageSummary.of(UsageDto(dailyLimit = 3, usedToday = 2, remainingToday = 1), cached)
        )
    }
}
