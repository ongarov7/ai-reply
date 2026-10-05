package kz.yerek.aireply

import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.keyboard.autocorrect.EditCost
import kz.yerek.aireply.keyboard.autocorrect.KazakhLetters
import kz.yerek.aireply.keyboard.autocorrect.KeyProximity
import kz.yerek.aireply.keyboard.autocorrect.TokenGuard
import kz.yerek.aireply.keyboard.autocorrect.TypedWord
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The building blocks of a correction: which tokens are never touched, which
 * keys are neighbours, and what each kind of typo costs.
 *
 * Түзету ережелері: қорғалған сөздер, көрші пернелер, қате бағасы.
 */
class AutocorrectRulesTest {

    private val english = KeyboardLanguage.ENGLISH
    private val russian = KeyboardLanguage.RUSSIAN
    private val kazakh = KeyboardLanguage.KAZAKH

    // ------------------------------------------------------------ token guard

    @Test
    fun `ordinary words are not protected`() {
        assertFalse(TokenGuard.isProtected("превт", "", russian))
        assertFalse(TokenGuard.isProtected("Превт", "Ну ", russian))
        assertFalse(TokenGuard.isProtected("қалайсын", "", kazakh))
        assertFalse(TokenGuard.isProtected("teh", "", english))
        assertFalse(TokenGuard.isProtected("don't", "", english))
        assertFalse(TokenGuard.isProtected("кое-что", "", russian))
        assertFalse("a quote is not an address", TokenGuard.isProtected("превт", "«", russian))
        assertFalse("the marker is in an earlier chunk", TokenGuard.isProtected("превт", "@user ", russian))
    }

    @Test
    fun `short tokens are protected except the English i`() {
        assertTrue(TokenGuard.isProtected("я", "", russian))
        assertTrue(TokenGuard.isProtected("a", "", english))
        assertFalse(TokenGuard.isProtected("i", "", english))
        assertFalse(TokenGuard.isProtected("I", "", english))
        assertTrue(TokenGuard.isProtected("i", "", russian))
    }

    @Test
    fun `numbers, handles and addresses are protected`() {
        assertTrue(TokenGuard.isProtected("превт123", "", russian))
        assertTrue(TokenGuard.isProtected("@превт", "", russian))
        assertTrue(TokenGuard.isProtected("превт", "@", russian))
        assertTrue(TokenGuard.isProtected("#тег", "", russian))
        assertTrue(TokenGuard.isProtected("https://site.ru", "", english))
        assertTrue(TokenGuard.isProtected("site", "https://", english))
        assertTrue(TokenGuard.isProtected("ru", "site.", english))
        assertTrue(TokenGuard.isProtected("com", "www.", english))
        assertTrue(TokenGuard.isProtected("snake_case", "", english))
        assertTrue(TokenGuard.isProtected("bc", "a=", english))
        assertTrue(TokenGuard.isProtected("tom", "jerry&", english))
        assertTrue(TokenGuard.isProtected("bin", "C:\\", english))
    }

    @Test
    fun `capitals inside a word are protected`() {
        assertTrue(TokenGuard.isProtected("ПРИВЕТ", "", russian))
        assertTrue(TokenGuard.isProtected("OK", "", english))
        assertTrue(TokenGuard.isProtected("iPhone", "", english))
        assertTrue(TokenGuard.isProtected("McDonald", "", english))
        assertFalse(TokenGuard.isProtected("Hello", "", english))
    }

    @Test
    fun `another script than the layout is protected`() {
        assertTrue(TokenGuard.isProtected("hello", "", russian))
        assertTrue(TokenGuard.isProtected("hello", "", kazakh))
        assertTrue(TokenGuard.isProtected("привет", "", english))
        assertTrue("mixed scripts", TokenGuard.isProtected("пpивет", "", russian))
    }

    // -------------------------------------------------------- key proximity

    @Test
    fun `English neighbours follow the staggered rows`() {
        val keys = KeyProximity.of(english)
        "qwsz".forEach { assertTrue("a~$it", keys.areAdjacent('a', it)) }
        "edx".forEach { assertFalse("a~$it", keys.areAdjacent('a', it)) }
        "asdx".forEach { assertTrue("z~$it", keys.areAdjacent('z', it)) }
        assertTrue(keys.areAdjacent('o', 'p'))
        assertFalse(keys.areAdjacent('q', 'p'))
    }

    @Test
    fun `Russian and Kazakh neighbours come from the layout rows`() {
        val russianKeys = KeyProximity.of(russian)
        "вп".forEach { assertTrue(russianKeys.areAdjacent('а', it)) }
        "уке".forEach { assertTrue("а sits under у к е", russianKeys.areAdjacent('а', it)) }
        assertFalse("е and и are two rows apart", russianKeys.areAdjacent('е', 'и'))
        assertTrue("ё is on е's key", russianKeys.areAdjacent('ё', 'к'))
        assertFalse("no Kazakh row on the Russian layout", russianKeys.areAdjacent('ә', 'ц'))

        val kazakhKeys = KeyProximity.of(kazakh)
        assertTrue(kazakhKeys.areAdjacent('ә', 'і'))
        assertTrue("the Kazakh row sits over ЙЦУКЕН", kazakhKeys.areAdjacent('ә', 'ц'))
        assertFalse(kazakhKeys.areAdjacent('ә', 'ф'))
    }

    // ---------------------------------------------------------------- costs

    private fun distance(typed: String, candidate: String, language: KeyboardLanguage, limit: Int = 100): Int =
        TypedWord(typed, KeyProximity.of(language)).distanceTo(candidate.toCharArray(), 0, candidate.length, limit)

    @Test
    fun `each kind of typo has its cost in tenths`() {
        assertEquals(0, distance("привет", "привет", russian))
        assertEquals("doubled letter", 6, distance("сеггодня", "сегодня", russian))
        assertEquals("transposition", 7, distance("сегодян", "сегодня", russian))
        assertEquals("transposition", 7, distance("teh", "the", english))
        assertEquals("neighbour key", 6, distance("hellp", "hello", english))
        assertEquals("any other key", 10, distance("hellz", "hello", english))
        assertEquals("missing letter", 10, distance("спасиб", "спасибо", russian))
        assertEquals("extra letter", 10, distance("пиривет", "привет", russian))
        assertEquals("extra letter typed twice", 6, distance("приввет", "привет", russian))
        assertEquals("е for ё", 1, distance("еще", "ещё", russian))
        assertEquals("ё for е", 1, distance("ещё", "еще", russian))
        assertEquals("two plain Kazakh letters", 6, distance("калайсын", "қалайсың", kazakh))
        assertEquals("у may stand for ұ or ү, ұ for ү", 3, distance("ұлкен", "үлкен", kazakh))
    }

    @Test
    fun `a Kazakh letter never becomes its plain one`() {
        // The substitution is forbidden; only a deletion plus an insertion
        // gets there, which a long word could afford - hence the filter.
        assertEquals(20, distance("қайда", "кайда", kazakh))
        assertEquals(20, distance("үлкен", "ұлкен", kazakh))
        assertEquals(EditCost.FORBIDDEN, EditCost.substitution('қ', 'к', KeyProximity.of(kazakh)))
        assertTrue(KazakhLetters.losesKazakhLetter("қомпания", "компания"))
        assertFalse(KazakhLetters.losesKazakhLetter("компания", "компания"))
        assertFalse(KazakhLetters.losesKazakhLetter("кайда", "қайда"))
    }

    @Test
    fun `the distance gives up past its limit`() {
        assertTrue(distance("абвгдежз", "клмнопрс", russian, limit = 20) > 20)
        assertEquals(6, distance("сеггодня", "сегодня", russian, limit = 6))
        assertEquals(10, EditCost.maxDistance(3))
        assertEquals("a missing letter plus a swap (DESIGN §10.2)", 17, EditCost.maxDistance(4))
        assertEquals(17, EditCost.maxDistance(5))
        assertEquals(20, EditCost.maxDistance(6))
        assertEquals(20, EditCost.maxDistance(7))
    }
}
