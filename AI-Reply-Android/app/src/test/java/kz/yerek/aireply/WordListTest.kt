package kz.yerek.aireply

import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.keyboard.autocorrect.WordList
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.IOException

/**
 * The word lists: ranks, display forms, exact and prefix lookups, on a small
 * list and on the shipped ones.
 *
 * Сөз тізімі: реті, көрсетілетін түрі, басы бойынша іздеу.
 */
class WordListTest {

    private fun list(vararg lines: String): WordList =
        WordList.read(lines.joinToString("\n", postfix = "\n").byteInputStream())

    private val small = list(
        "# a comment does not take a rank",
        "the",
        "to",
        "I",
        "Москва",
        "iPhone",
        "tomorrow",
        "tomato",
        "tom",
        "# comments may come anywhere",
        "tomorrows"
    )

    @Test
    fun `rank is the position among words`() {
        assertEquals(9, small.size)
        assertEquals(0, small.rankOf("the"))
        assertEquals(2, small.rankOf("i"))
        assertEquals(8, small.rankOf("tomorrows"))
        assertEquals(-1, small.rankOf("tomorro"))
        assertFalse("tomorrowss" in small)
        assertTrue("москва" in small)
    }

    @Test
    fun `words are matched lowercased and shown as listed`() {
        assertEquals("i", small.lowercased(2))
        assertEquals("I", small.display(2))
        assertEquals("москва", small.lowercased(3))
        assertEquals("Москва", small.display(3))
        assertEquals("iphone", small.lowercased(4))
        assertEquals("iPhone", small.display(4))
        assertEquals("tomorrow", small.display(5))
    }

    @Test
    fun `completions are longer words with the prefix, most frequent first`() {
        assertEquals(listOf(5, 6, 8), small.completions("tom", 3))
        assertEquals(listOf(5, 6), small.completions("tom", 2))
        assertEquals("the prefix itself is not a completion", listOf(5, 8), small.completions("tomo", 5))
        assertEquals(listOf(6, 8), small.completions("tom", 3) { it != 5 })
        assertEquals(emptyList<Int>(), small.completions("x", 3))
        assertEquals(emptyList<Int>(), small.completions("", 3))
    }

    @Test
    fun `a malformed list is refused`() {
        try {
            list("the", "", "to")
        } catch (expected: IOException) {
            return
        }
        throw AssertionError("an empty line is not a word")
    }

    @Test
    fun `the shipped lists read whole`() {
        val english = checkNotNull(AutocorrectTestDictionaries.shipped.wordList(KeyboardLanguage.ENGLISH))
        val russian = checkNotNull(AutocorrectTestDictionaries.shipped.wordList(KeyboardLanguage.RUSSIAN))
        val kazakh = checkNotNull(AutocorrectTestDictionaries.shipped.wordList(KeyboardLanguage.KAZAKH))
        assertEquals(50_000, english.size)
        assertEquals(80_000, russian.size)
        assertEquals(80_000, kazakh.size)

        assertEquals(0, english.rankOf("the"))
        assertEquals("I", english.display(english.rankOf("i")))
        assertEquals("Москва", russian.display(russian.rankOf("москва")))
        assertEquals("Қазақстан", kazakh.display(kazakh.rankOf("қазақстан")))
        assertTrue("сегодня" in russian)
        assertTrue("что-то" in russian)
        assertTrue("don't" in english)
    }
}
