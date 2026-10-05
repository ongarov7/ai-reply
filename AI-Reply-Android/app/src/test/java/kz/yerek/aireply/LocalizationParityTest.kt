package kz.yerek.aireply

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File
import javax.xml.parsers.DocumentBuilderFactory

/**
 * Russian and Kazakh cover the full catalogue. Uzbek covers every active flow;
 * less common legacy text uses Android's English fallback.
 *
 * WHY THIS IS A TEST AND NOT A REVIEW STEP. The iOS project has 249 strings in
 * one catalogue file; this one has them spread over six XML files. The failure
 * mode — a key added to English and forgotten in Kazakh — is silent at build
 * time and shows up as an English sentence in the middle of a Kazakh screen. It
 * is exactly the kind of thing a test should be holding, and it costs
 * milliseconds.
 *
 * Gradle runs unit tests with the module directory as the working directory,
 * which is what makes these relative paths stable.
 */
class LocalizationParityTest {

    private val base = File("src/main/res")

    private fun strings(folder: String): Map<String, String> {
        val result = LinkedHashMap<String, String>()
        listOf("strings.xml", "strings_android.xml").forEach { name ->
            val file = File(base, "$folder/$name")
            if (!file.exists()) return@forEach
            val document = DocumentBuilderFactory.newInstance().newDocumentBuilder().parse(file)
            val nodes = document.getElementsByTagName("string")
            for (index in 0 until nodes.length) {
                val element = nodes.item(index) as org.w3c.dom.Element
                if (element.getAttribute("translatable") == "false") continue
                result[element.getAttribute("name")] = element.textContent
            }
        }
        return result
    }

    private val english by lazy { strings("values") }
    private val russian by lazy { strings("values-ru") }
    private val kazakh by lazy { strings("values-kk") }
    private val uzbek by lazy { strings("values-uz") }

    @Test
    fun `the catalogue is not empty`() {
        assertTrue("expected the ported iOS strings", english.size > 240)
    }

    @Test
    fun `russian covers every translatable english key`() {
        val missing = english.keys - russian.keys
        assertEquals("missing Russian translations", emptySet<String>(), missing)
    }

    @Test
    fun `kazakh covers every translatable english key`() {
        val missing = english.keys - kazakh.keys
        assertEquals("missing Kazakh translations", emptySet<String>(), missing)
    }

    @Test
    fun `no translation introduces a key english does not have`() {
        assertEquals(emptySet<String>(), russian.keys - english.keys)
        assertEquals(emptySet<String>(), kazakh.keys - english.keys)
        assertEquals(emptySet<String>(), uzbek.keys - english.keys)
    }

    @Test
    fun `uzbek covers every production entry point`() {
        val required = setOf(
            "account_sign_in_title", "account_code_title", "legal_consent_title",
            "registration_title", "home_title", "settings_title", "profile_title",
            "templates_title", "hours_title", "android_setup_title", "onboarding_welcome_title",
            "onboarding_usage_title", "onboarding_done_title", "compose_title",
            "subscription_title", "kb_generate", "kb_insert", "kb_err_sign_in_required",
            "settings_privacy_body", "legal_terms", "legal_privacy"
        )
        assertEquals("missing Uzbek production strings", emptySet<String>(), required - uzbek.keys)
        assertTrue("Uzbek catalogue is unexpectedly small", uzbek.size > 200)
    }

    /**
     * A translation that drops a `%1$d` crashes `getString` at runtime, in the
     * one language the developer is least likely to be testing in.
     */
    @Test
    fun `format specifiers match across languages`() {
        val specifier = Regex("""%(\d+\$)?[sdf]""")
        english.forEach { (key, value) ->
            val expected = specifier.findAll(value).map { it.value }.toSet()
            listOf("ru" to russian, "kk" to kazakh, "uz" to uzbek).forEach { (language, table) ->
                if (language == "uz" && key !in table) return@forEach
                val actual = specifier.findAll(table[key].orEmpty()).map { it.value }.toSet()
                assertEquals("$key ($language)", expected, actual)
            }
        }
    }

    /**
     * A quick intent is a phrase the user sends as their own instruction, so it
     * must not say «согласен» for a woman or «согласна» for a man.
     */
    @Test
    fun `russian quick intents do not assume the sender's gender`() {
        val gendered = Regex(
            """(?iU)(?<!\p{L})(согласен|согласна|рад|рада|готов|готова|занят|занята|смог|смогла|должен|должна)(?!\p{L})"""
        )
        val phrases = russian.filterKeys { it.startsWith("kb_") && it.contains("intent") }
        assertTrue("expected the quick intents", phrases.size > 10)
        phrases.forEach { (key, value) ->
            assertTrue("$key: $value", gendered.find(value) == null)
        }
    }

    @Test
    fun `no translation is left as the untranslated english text`() {
        // Sanity check on the mechanical conversion: a handful of identical
        // strings is normal (proper nouns, "OK"), but a wholesale copy is not.
        val identical = english.count { (key, value) ->
            value.isNotBlank() && russian[key] == value && kazakh[key] == value
        }
        assertTrue("too many identical strings: $identical", identical < 20)
    }
}
