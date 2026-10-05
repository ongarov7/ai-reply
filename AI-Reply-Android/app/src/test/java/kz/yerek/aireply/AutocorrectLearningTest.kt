package kz.yerek.aireply

import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.keyboard.autocorrect.AppliedCorrection
import kz.yerek.aireply.keyboard.autocorrect.AutocorrectEngine
import kz.yerek.aireply.keyboard.autocorrect.LearnedWords
import kz.yerek.aireply.keyboard.autocorrect.LearnedWordsStore
import kz.yerek.aireply.keyboard.autocorrect.Suggestion
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Undo and learning (DESIGN §7.7): backspace right after a correction takes
 * it back for the session, and kept words are never corrected again.
 *
 * Пайдаланушы үйреткен сөздер және түзетуді болдырмау.
 */
class AutocorrectLearningTest {

    private val russian = KeyboardLanguage.RUSSIAN
    private val english = KeyboardLanguage.ENGLISH

    // ------------------------------------------------------------ the model

    @Test
    fun `the newest word comes first and the oldest drop past the cap`() {
        var words = LearnedWords.EMPTY
        for (index in 0 until LearnedWords.CAPACITY + 20) words = words.adding("слово$index")
        assertEquals(LearnedWords.CAPACITY, words.words.size)
        assertEquals("слово${LearnedWords.CAPACITY + 19}", words.words.first())
        assertTrue("слово20" in words)
        assertFalse("the oldest are gone", "слово19" in words)

        val again = words.adding("слово100")
        assertEquals("learning again moves it to the front", "слово100", again.words.first())
        assertEquals(LearnedWords.CAPACITY, again.words.size)
    }

    @Test
    fun `learned words survive a round trip through the store`() {
        val words = LearnedWords.EMPTY.adding("ерек").adding("превт")
        assertEquals(listOf("превт", "ерек"), LearnedWords.decode(words.encoded()).words)
        assertEquals(listOf("a", "b"), LearnedWords.decode("a\n\n b \na\n").words)
        assertTrue(LearnedWords.decode(null).words.isEmpty())
        assertEquals("autocorrect.learned.kk", LearnedWordsStore.key(KeyboardLanguage.KAZAKH))
    }

    // ------------------------------------------------------------ the engine

    @Test
    fun `undo puts the word back for the session and learns it`() {
        val store = MemoryLearnedWordsStore()
        val engine = AutocorrectTestDictionaries.engine(store)
        assertEquals("сегодня", engine.correction("сегодян", "", russian))

        val applied = AppliedCorrection("сегодян", "сегодня", " ", russian)
        assertEquals("the correction and its space", 8, applied.undoLength)
        engine.undo(applied, learn = true)

        assertNull(engine.correction("сегодян", "", russian))
        assertTrue(engine.isKnown("сегодян", russian))
        assertEquals("сегодян", LearnedWords.decode(store.values[russian]).words.single())
        assertEquals("other words are still corrected", "привет", engine.correction("превет", "", russian))
    }

    @Test
    fun `undo in a field without personalised learning only rejects the pair`() {
        val store = MemoryLearnedWordsStore()
        val engine = AutocorrectTestDictionaries.engine(store)
        engine.undo(AppliedCorrection("dont", "don't", " ", english), learn = false)

        assertNull(engine.correction("dont", "", english))
        assertEquals("nothing learned", 0, store.writes)
        assertEquals("the pair is per layout", "сегодня", engine.correction("сегодян", "", russian))
    }

    @Test
    fun `a kept word is learned and never corrected`() {
        val engine = AutocorrectTestDictionaries.engine()
        val analysis = engine.analyze("Сегодян", "", russian)
        assertEquals(Suggestion("Сегодян", Suggestion.Kind.TYPED), analysis.suggestions.first())

        engine.learn("Сегодян", russian)
        assertNull(engine.correction("сегодян", "", russian))
        assertNull(engine.correction("Сегодян", "", russian))
        assertTrue(engine.analyze("сегодян", "", russian).suggestions.none { it.kind == Suggestion.Kind.CORRECTION })
        assertEquals("learned per layout", "the", engine.correction("teh", "", english))
    }

    @Test
    fun `learned words come back in a new session`() {
        val store = MemoryLearnedWordsStore()
        AutocorrectTestDictionaries.engine(store).learn("превт", russian)
        val next = AutocorrectTestDictionaries.engine(store)
        assertTrue(next.isKnown("превт", russian))
        assertNull(next.correction("превт", "", russian))
    }

    @Test
    fun `learning before the layout loads keeps what was stored`() {
        val store = MemoryLearnedWordsStore()
        store.write(russian, "ерек")
        val engine = AutocorrectEngine(AutocorrectTestDictionaries.shipped, store)
        engine.learn("превт", russian)
        assertEquals(listOf("превт", "ерек"), LearnedWords.decode(store.values[russian]).words)
    }
}
