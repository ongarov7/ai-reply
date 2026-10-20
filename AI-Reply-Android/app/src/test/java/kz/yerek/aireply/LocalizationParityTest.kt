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

    /** Every `strings*.xml` of a folder, so a new file cannot slip past these checks. */
    private fun files(folder: String): List<File> =
        File(base, folder).listFiles { file -> file.name.startsWith("strings") && file.extension == "xml" }
            ?.sortedBy { it.name }
            .orEmpty()

    private fun strings(folder: String): Map<String, String> {
        val result = LinkedHashMap<String, String>()
        files(folder).forEach { file ->
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

    /** `<plurals>`: name → quantity → text. */
    private fun plurals(folder: String): Map<String, Map<String, String>> {
        val result = LinkedHashMap<String, Map<String, String>>()
        files(folder).forEach { file ->
            val document = DocumentBuilderFactory.newInstance().newDocumentBuilder().parse(file)
            val nodes = document.getElementsByTagName("plurals")
            for (index in 0 until nodes.length) {
                val element = nodes.item(index) as org.w3c.dom.Element
                val items = element.getElementsByTagName("item")
                result[element.getAttribute("name")] = (0 until items.length).associate { item ->
                    val node = items.item(item) as org.w3c.dom.Element
                    node.getAttribute("quantity") to node.textContent
                }
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
            "templates_title", "hours_title", "android_setup_title", "onboarding_welcome_headline",
            "onboarding_tutorial_title", "onboarding_connect_title", "permissions_title", "paste_help_title",
            "compose_title", "balance_welcome_title", "exhausted_body", "purchase_title", "support_title",
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
     * Plurals: Russian and Kazakh have every one, each language has the
     * quantities its grammar asks for (Russian one / few / many), and every
     * quantity keeps the English specifiers - a count written as a plain
     * string reads «1 ответов».
     */
    @Test
    fun `plurals are complete in every language`() {
        val englishPlurals = plurals("values")
        assertTrue("expected the reply counts", "subscription_per_day" in englishPlurals)
        val required = mapOf(
            "values" to setOf("one", "other"),
            "values-ru" to setOf("one", "few", "many", "other"),
            "values-kk" to setOf("one", "other"),
            "values-uz" to setOf("one", "other")
        )
        val specifier = Regex("""%(\d+\$)?[sdf]""")
        required.forEach { (folder, quantities) ->
            val table = plurals(folder)
            if (folder != "values-uz") {
                assertEquals("$folder misses plurals", emptySet<String>(), englishPlurals.keys - table.keys)
            }
            assertEquals("$folder adds plurals", emptySet<String>(), table.keys - englishPlurals.keys)
            table.forEach { (name, items) ->
                assertTrue("$name ($folder): ${items.keys}", items.keys.containsAll(quantities))
                val expected = specifier.findAll(englishPlurals.getValue(name).getValue("other")).map { it.value }.toSet()
                items.forEach { (quantity, text) ->
                    assertEquals("$name/$quantity ($folder)", expected, specifier.findAll(text).map { it.value }.toSet())
                }
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

    /** Write with AI produces a message of the user's own: its report never calls it a reply. */
    @Test
    fun `the compose report speaks of a message`() {
        val keys = listOf("report_title_compose", "report_include_text_compose", "report_thanks_compose")
        val reply = mapOf("en" to "reply", "ru" to "ответ", "kk" to "жауап", "uz" to "javob")
        val message = mapOf("en" to "message", "ru" to "сообщени", "kk" to "хабарлама", "uz" to "xabar")
        mapOf("en" to english, "ru" to russian, "kk" to kazakh, "uz" to uzbek).forEach { (language, table) ->
            keys.forEach { key ->
                val text = table.getValue(key).lowercase()
                assertTrue("$key ($language): $text", text.contains(message.getValue(language)))
                assertTrue("$key ($language): $text", !text.contains(reply.getValue(language)))
            }
        }
    }

    /**
     * The consent names everything that leaves: the selection too, not only
     * what was copied.
     */
    @Test
    fun `the AI disclosure covers selected text`() {
        val selected = mapOf("en" to "selected", "ru" to "выделенное", "kk" to "белгіленген", "uz" to "belgilangan")
        mapOf("en" to english, "ru" to russian, "kk" to kazakh, "uz" to uzbek).forEach { (language, table) ->
            listOf("legal_consent_ai_disclosure", "settings_privacy_body").forEach { key ->
                val text = table.getValue(key)
                assertTrue("$key ($language): $text", text.contains(selected.getValue(language)))
            }
        }
    }

    /** Kazakh Settings says «тіркелгі» for the account; the deletion strings sit in the same section. */
    @Test
    fun `kazakh uses one word for the account`() {
        listOf(
            "settings_account_delete", "settings_account_delete_title", "settings_account_delete_body",
            "account_deleted", "account_delete_failed", "settings_withdraw_consent_body"
        ).forEach { key ->
            val text = kazakh.getValue(key)
            assertTrue("$key: $text", text.contains("ркелгі") && !text.contains("ккаунт"))
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
