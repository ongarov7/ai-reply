package kz.yerek.aireply

import kz.yerek.aireply.core.lang.AppLanguage
import org.junit.Assert.assertEquals
import org.junit.Test
import java.util.Locale

class AppLanguagePreferenceTest {
    @Test
    fun `regional system locales select their own tutorial language`() {
        mapOf("kk-KZ" to AppLanguage.KAZAKH, "ru-RU" to AppLanguage.RUSSIAN,
            "en-GB" to AppLanguage.ENGLISH, "uz-Latn-UZ" to AppLanguage.UZBEK).forEach { (tag, language) ->
            assertEquals(tag, language, AppLanguage.fromPreferredLocales(listOf(Locale.forLanguageTag(tag))))
        }
    }

    @Test
    fun `the first supported phone preference wins`() {
        assertEquals(AppLanguage.KAZAKH, AppLanguage.fromPreferredLocales(
            listOf(Locale.GERMAN, Locale.forLanguageTag("kk-KZ"), Locale.forLanguageTag("ru-RU"))))
        assertEquals(AppLanguage.ENGLISH, AppLanguage.fromPreferredLocales(
            listOf(Locale.ENGLISH, Locale.forLanguageTag("uz-UZ"))))
    }

    @Test
    fun `empty and unsupported phone preferences use the English guide`() {
        assertEquals(AppLanguage.ENGLISH, AppLanguage.fromPreferredLocales(emptyList()))
        assertEquals(AppLanguage.ENGLISH, AppLanguage.fromPreferredLocales(listOf(Locale.GERMAN, Locale.JAPANESE)))
    }
}
