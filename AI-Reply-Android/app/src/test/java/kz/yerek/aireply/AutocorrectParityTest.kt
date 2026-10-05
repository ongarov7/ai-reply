package kz.yerek.aireply

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.keyboard.autocorrect.Suggestion
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * The iOS and the Android keyboard must answer the same word the same way
 * (DESIGN §10.5): both test suites read one table,
 * `tools/dictionaries/autocorrect_parity.json`, and check every case on the
 * shipped dictionaries.
 *
 * Екі платформа бір сөзге бірдей жауап береді: ортақ кесте.
 */
class AutocorrectParityTest {

    private val engine = AutocorrectTestDictionaries.engine()

    /** One row of the table, in the shared vocabulary (`typed`, `correction`, `word`). */
    private data class Expectation(
        val layout: KeyboardLanguage,
        val typed: String,
        val before: String,
        val correction: String?,
        val strip: List<Pair<String, String>>
    )

    private fun expectations(): List<Expectation> {
        // Gradle runs unit tests in the module directory (AI-Reply-Android/app).
        val table = Json.parseToJsonElement(File(TABLE_PATH).readText()).jsonObject
        assertEquals("a table this test understands", "1", table.getValue("format").jsonPrimitive.content)
        return table.getValue("cases").jsonArray.map { element ->
            val case = element.jsonObject
            Expectation(
                layout = checkNotNull(KeyboardLanguage.fromCode(case.string("layout"))),
                typed = case.string("typed"),
                before = case.string("before"),
                correction = case.getValue("correction").takeUnless { it is JsonNull }?.jsonPrimitive?.content,
                strip = case.getValue("strip").jsonArray.map { item ->
                    item.jsonObject.string("kind") to item.jsonObject.string("text")
                }
            )
        }
    }

    private fun JsonObject.string(key: String): String = getValue(key).jsonPrimitive.content

    private fun Suggestion.Kind.shared(): String = when (this) {
        Suggestion.Kind.TYPED -> "typed"
        Suggestion.Kind.CORRECTION -> "correction"
        Suggestion.Kind.HINT, Suggestion.Kind.CANDIDATE, Suggestion.Kind.COMPLETION -> "word"
    }

    @Test
    fun `every case of the shared table gets the same answer`() {
        val cases = expectations()
        assertTrue("the table covers every layout", cases.map { it.layout }.toSet() == KeyboardLanguage.entries.toSet())
        val mismatches = cases.mapNotNull { case ->
            val analysis = engine.analyze(case.typed, case.before, case.layout)
            val strip = analysis.suggestions.map { it.kind.shared() to it.text }
            val correction = engine.correction(case.typed, case.before, case.layout)
            when {
                analysis.correction != case.correction || correction != case.correction ->
                    "${case.layout.code} «${case.typed}»: correction ${analysis.correction} / $correction, expected ${case.correction}"
                strip != case.strip -> "${case.layout.code} «${case.typed}»: strip $strip, expected ${case.strip}"
                else -> null
            }
        }
        assertTrue(mismatches.joinToString("\n"), mismatches.isEmpty())
    }

    private companion object {
        const val TABLE_PATH = "../../tools/dictionaries/autocorrect_parity.json"
    }
}
