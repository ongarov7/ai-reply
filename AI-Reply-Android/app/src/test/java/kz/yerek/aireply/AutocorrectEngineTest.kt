package kz.yerek.aireply

import kotlinx.coroutines.runBlocking
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.keyboard.autocorrect.AutocorrectAnalysis
import kz.yerek.aireply.keyboard.autocorrect.AutocorrectDictionaries
import kz.yerek.aireply.keyboard.autocorrect.AutocorrectEngine
import kz.yerek.aireply.keyboard.autocorrect.DictionarySource
import kz.yerek.aireply.keyboard.autocorrect.Suggestion
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.FileNotFoundException
import java.io.IOException

/**
 * The engine on the shipped dictionaries: every case of DESIGN §7.8 (the
 * iOS engine has the same expectations), case transfer, the suggestion strip
 * and the time a lookup takes.
 *
 * Автотүзету: §7.8 жағдайлары, бас әріп, ұсыныстар жолағы, жылдамдық.
 */
class AutocorrectEngineTest {

    private val engine = AutocorrectTestDictionaries.engine()

    private val english = KeyboardLanguage.ENGLISH
    private val russian = KeyboardLanguage.RUSSIAN
    private val kazakh = KeyboardLanguage.KAZAKH

    private fun corrected(word: String, language: KeyboardLanguage, before: String = ""): String? =
        engine.correction(word, before, language)

    private fun strip(word: String, language: KeyboardLanguage): List<String> =
        engine.analyze(word, "", language).suggestions.map { it.text }

    // --------------------------------------------------------------- Russian

    @Test
    fun `Russian typos are corrected`() {
        assertEquals("сегодня", corrected("сегодян", russian))
        assertEquals("сегодня", corrected("сеггодня", russian))
        assertEquals("привет", corrected("превет", russian))
        assertEquals("neighbouring key", "привет", corrected("прмвет", russian))
        val thanks = engine.analyze("спасиб", "", russian)
        assertTrue(thanks.correction == "спасибо" || thanks.suggestions.first().text == "спасибо")
    }

    /**
     * "превт" is 1.7 from "привет" (the missing и plus the swapped ве): within
     * the widened reach of a five-letter word (DESIGN §10.2), far past what is
     * applied by itself. A dozen nearer or more frequent words (право, прет,
     * крест…) still score better under the §7.4 weights, so привет is a
     * candidate but not one of the strip's three - the same on iOS (see
     * tools/dictionaries/autocorrect_parity.json).
     */
    @Test
    fun `превт is left alone rather than turned into the wrong word`() {
        assertNull(corrected("превт", russian))
        assertTrue(engine.candidates("превт", russian, limit = 20).any { it.word == "привет" && it.distance == 1.7 })
    }

    /** DESIGN §10.1: the regenerated lists no longer count these as words. */
    @Test
    fun `misspellings the corpus used to count as words are corrected`() {
        assertEquals("извини", corrected("извени", russian))
        assertEquals("здравствуйте", corrected("здраствуйте", russian))
        assertEquals("their", corrected("thier", english))
        assertEquals("receive", corrected("recieve", english))
        assertEquals("здесь", engine.analyze("сдесь", "", russian).suggestions.first().text)
    }

    @Test
    fun `Russian words are left alone`() {
        assertNull(corrected("привет", russian))
        assertNull("ё written as е", corrected("еще", russian))
        assertNull(corrected("созвониться", russian))
        assertNull(corrected("ПРИВЕТ", russian))
        assertNull(corrected("@превт", russian))
        assertNull(corrected("превт", russian, before = "@"))
        assertNull(corrected("превт123", russian))
        assertNull(corrected("https://site.ru", russian))
        assertNull("Kazakh typed on the Russian layout", corrected("рахмет", russian))
        assertNull(corrected("калайсын", russian))
    }

    // ---------------------------------------------------------------- Kazakh

    @Test
    fun `a plain spelling is kept but the Kazakh one is offered first`() {
        val analysis = engine.analyze("кайда", "", kazakh)
        assertNull(analysis.correction)
        assertEquals(Suggestion("қайда", Suggestion.Kind.HINT), analysis.suggestions.first())
        assertEquals("Қайда", engine.analyze("Кайда", "", kazakh).suggestions.first().text)
        assertNull("only on the Kazakh layout", engine.analyze("кайда", "", russian).suggestions.firstOrNull { it.kind == Suggestion.Kind.HINT })
    }

    /**
     * Regression: a Russian word the Kazakh list lacks got a Kazakh hint in
     * the first slot (был → біл, куда → құда, они → өңі). A word the Russian
     * list has is meant as written.
     */
    @Test
    fun `a listed Russian word gets no Kazakh hint`() {
        val wrong = setOf("біл", "Біл", "құда", "өңі", "іх", "тұт", "көму")
        listOf("был", "Был", "куда", "они", "их", "тут", "кому").forEach { word ->
            val analysis = engine.analyze(word, "", kazakh)
            assertNull(word, analysis.correction)
            assertTrue("$word: ${analysis.suggestions}", analysis.suggestions.none { it.kind == Suggestion.Kind.HINT || it.text in wrong })
        }
    }

    /**
     * Kazakh typed with plain letters keeps its hint, even when a Russian web
     * text once had that spelling (the Russian known filter has кайда and
     * биз): only the Russian word list takes the hint away.
     */
    @Test
    fun `Kazakh plain spellings keep their hint`() {
        mapOf("кайда" to "қайда", "калайсын" to "қалайсың", "бугин" to "бүгін", "биз" to "біз", "сиз" to "сіз").forEach { (typed, hint) ->
            val analysis = engine.analyze(typed, "", kazakh)
            assertNull(typed, analysis.correction)
            assertEquals(typed, Suggestion(hint, Suggestion.Kind.HINT), analysis.suggestions.first())
        }
    }

    /**
     * A word typed with a Kazakh letter is Kazakh: the Russian list is not
     * searched for it, so no Russian word can replace a typed Kazakh letter
     * or crowd out the Kazakh fix (the iOS engine used to: қном → гном,
     * душі → душу, үрену left alone because of арену).
     */
    @Test
    fun `a word with a Kazakh letter gets no Russian candidates`() {
        assertEquals("үйрену", corrected("үрену", kazakh))
        mapOf(
            "қном" to "гном", "душі" to "душу", "бғды" to "беды", "кағое" to "какое", "важғости" to "важности", "үрену" to "арену"
        ).forEach { (typed, russian) ->
            assertFalse("$typed → $russian", engine.candidates(typed, kazakh, limit = 10).any { it.word == russian })
        }
        listOf("қном", "душі", "бғды", "кағое", "важғости").forEach { assertNull(it, corrected(it, kazakh)) }
        assertTrue("plain letters still search Russian", engine.candidates("превет", kazakh).any { it.word == "привет" })
    }

    @Test
    fun `Kazakh typos are corrected`() {
        val analysis = engine.analyze("қалайсын", "", kazakh)
        assertTrue(analysis.correction == "қалайсың" || analysis.suggestions.first().text == "қалайсың")
    }

    @Test
    fun `a Kazakh letter is never corrected to its plain one`() {
        KeyboardLanguage.entries.filter { it != english }.forEach { language ->
            assertNull(corrected("қомпания", language))
            assertFalse(engine.candidates("қомпания", language).any { it.word == "компания" })
        }
        assertFalse(engine.candidates("қайдв", kazakh).any { it.word.startsWith("к") })
    }

    @Test
    fun `Kazakh words are left alone`() {
        assertNull(corrected("рахмет", kazakh))
        assertNull(corrected("сәлем", kazakh))
        assertNull(corrected("қалайсың", kazakh))
        assertNull("Russian typed on the Kazakh layout", corrected("привет", kazakh))
    }

    // --------------------------------------------------------------- English

    @Test
    fun `English typos are corrected`() {
        assertEquals("the", corrected("teh", english))
        assertEquals("tomorrow", corrected("tommorow", english))
        assertEquals("don't", corrected("dont", english))
        assertEquals("I'm", corrected("im", english))
        assertEquals("I", corrected("i", english))
    }

    @Test
    fun `English words are left alone`() {
        assertNull(corrected("hello", english))
        assertNull(corrected("I", english))
        assertNull(corrected("iPhone", english))
        assertNull(corrected("McDonald", english))
        assertNull(corrected("lol", english))
        assertNull("a word in its own right", corrected("its", english))
        assertNull(corrected("well", english))
    }

    @Test
    fun `misspellings are never offered`() {
        assertFalse(engine.candidates("definatly", english).any { it.word == "definately" })
        assertFalse(engine.completions("defina", english).contains("definately"))
        assertFalse(engine.candidates("tej", english).any { it.word == "teh" })
    }

    // ----------------------------------------------------------- completions

    @Test
    fun `completions start with the prefix`() {
        assertTrue(engine.completions("сего", russian).contains("сегодня"))
        assertTrue(engine.completions("tomo", english).contains("tomorrow"))
        assertTrue(engine.completions("рахм", kazakh).contains("рахмет"))
        assertTrue(engine.completions("с", russian).isEmpty())
        engine.completions("сего", russian).forEach { assertTrue(it.lowercase().startsWith("сего") && it.length > 4) }
    }

    // ---------------------------------------------------------- case transfer

    @Test
    fun `the typed case carries over`() {
        assertEquals("Сегодня", corrected("Сегодян", russian))
        assertEquals("The", corrected("Teh", english))
        assertEquals("Don't", corrected("Dont", english))
        assertEquals("I'm", corrected("Im", english))
        assertEquals("Сегодня", strip("Сего", russian).first())
        assertEquals("Москва", AutocorrectEngine.transferCase("москвв", "Москва"))
        assertEquals("Москва", AutocorrectEngine.transferCase("Москвв", "москва"))
        assertEquals("москва", AutocorrectEngine.transferCase("москвв", "москва"))
    }

    // -------------------------------------------------------- suggestion strip

    @Test
    fun `a pending correction offers the typed word, the correction and one more`() {
        val suggestions = engine.analyze("сегодян", "", russian).suggestions
        assertEquals(Suggestion("сегодян", Suggestion.Kind.TYPED), suggestions[0])
        assertEquals(Suggestion("сегодня", Suggestion.Kind.CORRECTION), suggestions[1])
        assertEquals(3, suggestions.size)
        assertEquals(Suggestion("dont", Suggestion.Kind.TYPED), engine.analyze("dont", "", english).suggestions[0])
        assertEquals(
            listOf(Suggestion("i", Suggestion.Kind.TYPED), Suggestion("I", Suggestion.Kind.CORRECTION)),
            engine.analyze("i", "", english).suggestions
        )
    }

    @Test
    fun `a known word offers completions, an unknown one candidates`() {
        val known = engine.analyze("сего", "", russian)
        assertNull(known.correction)
        assertTrue(known.suggestions.all { it.kind == Suggestion.Kind.COMPLETION })
        assertTrue(known.suggestions.any { it.text == "сегодня" })

        val unknown = engine.analyze("превт", "", russian)
        assertNull(unknown.correction)
        assertEquals(Suggestion.Kind.CANDIDATE, unknown.suggestions.first().kind)
    }

    /** DESIGN §10.3: what the user is typing comes first, then what they may have meant. */
    @Test
    fun `completions come before candidates`() {
        assertEquals("рахмет", strip("рахм", kazakh).first())
        assertEquals("сегодня", strip("сего", russian).first())
        assertEquals("tomorrow", strip("tomo", english).first())
        assertEquals("the hint still goes first", Suggestion.Kind.HINT, engine.analyze("бугин", "", kazakh).suggestions.first().kind)
    }

    /** DESIGN §10.4: Russian is typed on the Kazakh layout too. */
    @Test
    fun `the Kazakh layout corrects Russian words as well`() {
        assertEquals("привет", corrected("превет", kazakh))
        assertEquals("сегодня", corrected("сегодян", kazakh))
        assertEquals("қайда", corrected("қайдв", kazakh))
        assertFalse(
            "a word with a Kazakh letter gets nothing from the Russian list",
            engine.candidates("қомпания", kazakh, limit = 10).any { it.word == "компания" }
        )
        assertTrue("Russian keeps to its own list", engine.candidates("сәлеи", russian).none { it.word == "сәлем" })
    }

    @Test
    fun `the strip holds at most three different words`() {
        listOf("сего" to russian, "превт" to russian, "tomo" to english, "кайда" to kazakh, "teh" to english).forEach { (word, language) ->
            val texts = strip(word, language)
            assertTrue(texts.size <= AutocorrectEngine.SUGGESTION_COUNT)
            assertEquals(texts.size, texts.map { it.lowercase() }.distinct().size)
        }
    }

    @Test
    fun `nothing to say about no word or a protected one`() {
        assertEquals(AutocorrectAnalysis.NONE, engine.analyze("", "", russian))
        assertEquals(AutocorrectAnalysis.NONE, engine.analyze("ПРИВЕТ", "", russian))
        assertEquals(AutocorrectAnalysis.NONE, engine.analyze("site", "https://", english))
    }

    // ---------------------------------------------------------------- loading

    @Test
    fun `nothing is suggested until the layout is loaded`() {
        val dictionaries = AutocorrectDictionaries(AutocorrectTestDictionaries.source)
        val fresh = AutocorrectEngine(dictionaries, MemoryLearnedWordsStore())
        assertFalse(fresh.isReady(russian))
        assertEquals(AutocorrectAnalysis.NONE, fresh.analyze("сегодян", "", russian))
        assertTrue(fresh.candidates("сегодян", russian).isEmpty())

        runBlocking { fresh.load(russian) }
        assertTrue(fresh.isReady(russian))
        assertFalse("other layouts load on their own", fresh.isReady(english))
        assertTrue("Russian reads the Kazakh filter too", dictionaries.filter(kazakh) != null)
        assertEquals("сегодня", fresh.correction("сегодян", "", russian))
    }

    @Test
    fun `a missing dictionary fails the load and leaves the layout quiet`() {
        val missing = DictionarySource { name -> throw FileNotFoundException(name) }
        val broken = AutocorrectEngine(AutocorrectDictionaries(missing), MemoryLearnedWordsStore())
        try {
            runBlocking { broken.load(english) }
            throw AssertionError("expected the load to fail")
        } catch (expected: IOException) {
            assertFalse(broken.isReady(english))
            assertEquals(AutocorrectAnalysis.NONE, broken.analyze("teh", "", english))
        }
    }

    // ------------------------------------------------------------ performance

    @Test
    fun `a seven-letter Russian word is looked up quickly`() {
        val word = "сегодян"
        repeat(5) { engine.candidates(word, russian) }
        val runs = 20
        val started = System.nanoTime()
        repeat(runs) { engine.analyze(word, "", russian) }
        val millis = (System.nanoTime() - started) / runs / 1_000_000.0
        println("autocorrect: candidates for a 7-letter Russian word in %.2f ms (target < 15 ms)".format(millis))
        assertTrue("took $millis ms", millis < 100)
    }
}
