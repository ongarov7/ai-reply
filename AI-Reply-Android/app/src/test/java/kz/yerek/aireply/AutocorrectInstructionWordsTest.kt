package kz.yerek.aireply

import kz.yerek.aireply.core.lang.KeyboardLanguage
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * What people write to AI Reply is instructions - «Поздравь коллегу»,
 * «Түсіндір», "Politely decline" - and a web corpus rarely contains
 * imperatives. Smart correction must never "fix" them into other words (it
 * once turned «поздравь» into «поздравил»): the built-in intent phrases and
 * the common instruction verbs are all left as typed.
 *
 * Нұсқау сөздері ешқашан «түзетілмейді».
 */
class AutocorrectInstructionWordsTest {

    private val engine = AutocorrectTestDictionaries.engine()

    /** Every word of the quick-intent phrases a layout's users see, read from the resources. */
    private fun intentWords(folder: String): Set<String> {
        val xml = File("src/main/res/$folder/strings_android.xml").readText()
        return Regex("""<string name="kb_(?:compose_)?intent_[a-z_]+_phrase">([^<]*)</string>""")
            .findAll(xml)
            .flatMap { Regex("""\p{L}+(?:[-'’]\p{L}+)*""").findAll(it.groupValues[1]).map { word -> word.value } }
            .toSet()
    }

    private fun corrected(words: Collection<String>, language: KeyboardLanguage): List<String> =
        words.mapNotNull { word -> engine.correction(word, "", language)?.let { "$word → $it" } }

    @Test
    fun `the quick intents are never corrected`() {
        val russian = intentWords("values-ru")
        val kazakh = intentWords("values-kk")
        val english = intentWords("values")
        assertTrue("the phrases were found", russian.size > 10 && kazakh.size > 10 && english.size > 10)

        val wrong = corrected(russian, KeyboardLanguage.RUSSIAN) + corrected(kazakh, KeyboardLanguage.KAZAKH) +
            corrected(english, KeyboardLanguage.ENGLISH)
        assertTrue(wrong.joinToString(), wrong.isEmpty())
    }

    @Test
    fun `instruction verbs are never corrected`() {
        val russian = listOf(
            "поздравь", "поздравьте", "уточни", "откажи", "подтверди", "похвали", "перезвони", "договорись",
            "назначь", "отмени", "посоветуй", "намекни", "ответь", "напиши", "скажи", "поблагодари", "извинись"
        )
        val kazakh = listOf("құттықта", "куттыкта", "түсіндір", "хабарла", "нақтыла", "айт", "жаз", "раста", "жібер")
        val wrong = corrected(russian, KeyboardLanguage.RUSSIAN) + corrected(kazakh, KeyboardLanguage.KAZAKH) +
            corrected(russian, KeyboardLanguage.KAZAKH)
        assertTrue(wrong.joinToString(), wrong.isEmpty())
    }
}
