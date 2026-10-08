package kz.yerek.aireply

import kotlinx.serialization.json.Json
import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.data.account.PlanDto
import kz.yerek.aireply.ui.feature.account.canOfferPurchase
import kz.yerek.aireply.ui.feature.account.formatExpiry
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

    private fun plan(isFree: Boolean = false, purchasable: Boolean = true) = PlanDto(
        id = "plan-pro",
        code = "pro",
        isFree = isFree,
        purchasable = purchasable
    )

    @Test
    fun `a release build never offers a purchase`() {
        assertFalse(canOfferPurchase(plan(purchasable = true), debugBuild = false))
        assertFalse(canOfferPurchase(plan(purchasable = false), debugBuild = false))
    }

    @Test
    fun `a debug build offers only what the server would sell`() {
        assertTrue(canOfferPurchase(plan(purchasable = true), debugBuild = true))
        assertFalse(canOfferPurchase(plan(purchasable = false), debugBuild = true))
        assertFalse("nothing to buy in a free plan", canOfferPurchase(plan(isFree = true), debugBuild = true))
    }

    @Test
    fun `purchasable is off unless the server says so`() {
        val older = json.decodeFromString(PlanDto.serializer(), """{"id":"p","code":"pro"}""")
        assertFalse(older.purchasable)
        val current = json.decodeFromString(PlanDto.serializer(), """{"id":"p","code":"pro","purchasable":true}""")
        assertTrue(current.purchasable)
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
}
