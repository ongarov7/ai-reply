package kz.yerek.aireply

import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.core.lang.KeyboardPlane
import kz.yerek.aireply.keyboard.KeyboardKey
import kz.yerek.aireply.keyboard.layout.FieldKind
import kz.yerek.aireply.keyboard.layout.KeyboardLabels
import kz.yerek.aireply.keyboard.layout.KeyboardLayout
import kz.yerek.aireply.keyboard.layout.PageOptions
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** The layouts, pinned to the national standards and Gboard. */
class KeyboardLayoutTest {

    private fun page(language: KeyboardLanguage, plane: KeyboardPlane = KeyboardPlane.LETTERS, options: PageOptions = PageOptions()) =
        KeyboardLayout.page(language, plane, options)

    @Test
    fun `english is standard 10-9-7 qwerty`() {
        val rows = page(KeyboardLanguage.ENGLISH).characterRows
        assertEquals(listOf(10, 9, 7), rows.take(3).map { it.size })
        assertEquals("qwertyuiop", rows[0].joinToString(""))
    }

    /** ё and ъ are long presses on е and ь, as on Gboard - not keys of their own. */
    @Test
    fun `russian is 11-11-9 with yo and hard sign behind long presses`() {
        val rows = KeyboardLayout.RUSSIAN_LETTERS
        assertEquals(listOf(11, 11, 9), rows.map { it.size })
        assertFalse(rows.flatten().contains("ё"))
        assertFalse(rows.flatten().contains("ъ"))
        assertEquals(listOf("ё", "5"), KeyboardLayout.page(KeyboardLanguage.RUSSIAN, KeyboardPlane.LETTERS, PageOptions())
            .rows[0].slots.first { (it.key as? KeyboardKey.Character)?.value == "е" }.alternates)
        assertEquals(listOf("ъ"), KeyboardLayout.alternates("ь", KeyboardLanguage.RUSSIAN))
    }

    @Test
    fun `kazakh puts its nine letters on top in the standard order`() {
        val rows = KeyboardLayout.KAZAKH_LETTERS
        assertEquals(4, rows.size)
        assertEquals(listOf("ә", "і", "ң", "ғ", "ү", "ұ", "қ", "ө", "һ"), rows.first())
        assertEquals(KeyboardLayout.RUSSIAN_LETTERS, rows.drop(1))
    }

    @Test
    fun `every letter of the kazakh alphabet can be typed`() {
        val alphabet = "аәбвгғдеёжзийкқлмнңоөпрстуұүфхһцчшщъыіьэюя"
        val page = page(KeyboardLanguage.KAZAKH)
        val typeable = page.characterRows.flatten().toSet() +
            page.rows.flatMap { row -> row.slots.flatMap { it.alternates } }
        alphabet.forEach { letter -> assertTrue("missing $letter", letter.toString() in typeable) }
    }

    @Test
    fun `digits ride on the qwerty and ytsuken rows as long presses, with nothing drawn in the corner`() {
        fun digits(row: kz.yerek.aireply.keyboard.layout.KeyRow) = row.slots.mapNotNull { slot -> slot.alternates.lastOrNull()?.takeIf { it.single().isDigit() } }
        val english = page(KeyboardLanguage.ENGLISH).rows[0]
        assertEquals("1234567890".map { it.toString() }, digits(english))
        // Kazakh: on the ЙЦУКЕН row under the Kazakh letters.
        val kazakh = page(KeyboardLanguage.KAZAKH)
        assertEquals("1234567890".map { it.toString() }, digits(kazakh.rows[1]))
        assertTrue(digits(kazakh.rows[0]).isEmpty())
        for (language in KeyboardLanguage.entries) {
            assertTrue("$language", page(language).rows.all { row -> row.slots.all { it.hint == null } })
        }
    }

    @Test
    fun `the tenge is on the currency key for kazakh and russian, the dollar for english`() {
        assertTrue(page(KeyboardLanguage.KAZAKH, KeyboardPlane.NUMBERS).characterRows.flatten().contains("₸"))
        assertTrue(page(KeyboardLanguage.RUSSIAN, KeyboardPlane.NUMBERS).characterRows.flatten().contains("₸"))
        assertTrue(page(KeyboardLanguage.ENGLISH, KeyboardPlane.NUMBERS).characterRows.flatten().contains("$"))
        assertEquals(listOf("₽", "$", "€", "£", "¥"), KeyboardLayout.alternates("₸", KeyboardLanguage.KAZAKH))
    }

    @Test
    fun `numbers and symbols are the same pages as on ios`() {
        val numbers = page(KeyboardLanguage.ENGLISH, KeyboardPlane.NUMBERS).characterRows
        assertEquals("1234567890", numbers[0].joinToString(""))
        assertEquals("-/:;()\$&@\"", numbers[1].joinToString(""))
        assertEquals(".,?!'", numbers[2].joinToString(""))
        val symbols = page(KeyboardLanguage.ENGLISH, KeyboardPlane.SYMBOLS).characterRows
        assertEquals("[]{}#%^*+=", symbols[0].joinToString(""))
        assertEquals("_\\|~<>€£¥•", symbols[1].joinToString(""))
        assertEquals(".,?!'", symbols[2].joinToString(""))
        // Tenge on the Cyrillic layouts' 123 page, the dollar one page on.
        assertTrue("₸" in page(KeyboardLanguage.RUSSIAN, KeyboardPlane.NUMBERS).characterRows[1])
        assertTrue("$" in page(KeyboardLanguage.RUSSIAN, KeyboardPlane.SYMBOLS).characterRows[1])
    }

    @Test
    fun `the bottom row is identical on every layout`() {
        fun bottom(language: KeyboardLanguage) = page(language).rows.last().let { row ->
            row.unitColumns to row.slots.map { it.key?.javaClass to it.width }
        }
        assertEquals(bottom(KeyboardLanguage.ENGLISH), bottom(KeyboardLanguage.RUSSIAN))
        assertEquals(bottom(KeyboardLanguage.ENGLISH), bottom(KeyboardLanguage.KAZAKH))
    }

    @Test
    fun `the bottom row is 123, emoji, the layout, space and return, with the globe only when asked for`() {
        val options = PageOptions(showsEmojiKey = true)
        assertEquals(
            listOf(KeyboardKey.Plane(KeyboardPlane.NUMBERS), KeyboardKey.Plane(KeyboardPlane.EMOJI), KeyboardKey.Layout, KeyboardKey.Space, KeyboardKey.Return),
            page(KeyboardLanguage.KAZAKH, options = options).rows.last().slots.map { it.key }
        )
        assertEquals(
            listOf(KeyboardKey.Plane(KeyboardPlane.NUMBERS), KeyboardKey.Plane(KeyboardPlane.EMOJI), KeyboardKey.Globe, KeyboardKey.Layout, KeyboardKey.Space, KeyboardKey.Return),
            page(KeyboardLanguage.KAZAKH, options = options.copy(showsGlobeKey = true)).rows.last().slots.map { it.key }
        )
    }

    @Test
    fun `email and url fields get their own keys`() {
        val email = page(KeyboardLanguage.ENGLISH, options = PageOptions(field = FieldKind.EMAIL)).rows.last()
        assertTrue(email.slots.any { it.key == KeyboardKey.Character("@") })
        val url = page(KeyboardLanguage.ENGLISH, options = PageOptions(field = FieldKind.URL)).rows.last()
        assertTrue(url.slots.any { it.key == KeyboardKey.Character("/") })
        assertTrue(url.slots.first { it.key == KeyboardKey.Character(".") }.alternates.contains(".kz"))
    }

    @Test
    fun `the language key only appears with more than one layout`() {
        val one = page(KeyboardLanguage.ENGLISH, options = PageOptions(showsLanguageKey = false)).rows.last()
        assertFalse(one.slots.any { it.key == KeyboardKey.Layout })
        assertTrue(page(KeyboardLanguage.ENGLISH).rows.last().slots.any { it.key == KeyboardKey.Layout })
    }

    @Test
    fun `kazakh sets the height every page is laid out in`() {
        assertEquals(5, KeyboardLayout.maximumRowCount(KeyboardLanguage.CYCLE_ORDER))
        assertEquals(4, KeyboardLayout.maximumRowCount(listOf(KeyboardLanguage.ENGLISH, KeyboardLanguage.RUSSIAN)))
    }

    @Test
    fun `the language key cycles kaz rus eng among the enabled ones`() {
        assertEquals(KeyboardLanguage.RUSSIAN, KeyboardLanguage.KAZAKH.next())
        assertEquals(KeyboardLanguage.ENGLISH, KeyboardLanguage.RUSSIAN.next())
        assertEquals(KeyboardLanguage.KAZAKH, KeyboardLanguage.ENGLISH.next())
        val two = listOf(KeyboardLanguage.KAZAKH, KeyboardLanguage.ENGLISH)
        assertEquals(KeyboardLanguage.ENGLISH, KeyboardLanguage.KAZAKH.next(two))
        assertEquals(KeyboardLanguage.KAZAKH, KeyboardLanguage.ENGLISH.next(two))
        // A layout that was switched off goes to the first one that is on.
        assertEquals(KeyboardLanguage.KAZAKH, KeyboardLanguage.RUSSIAN.next(two))
    }

    @Test
    fun `captions follow the layout`() {
        assertEquals("бос орын", KeyboardLabels(KeyboardLanguage.KAZAKH).space)
        assertEquals("Пробел", KeyboardLabels(KeyboardLanguage.RUSSIAN).space)
        assertEquals("space", KeyboardLabels(KeyboardLanguage.ENGLISH).space)
        assertEquals("АӘБ", KeyboardLabels(KeyboardLanguage.KAZAKH).planeKey(KeyboardPlane.LETTERS))
        assertEquals("123", KeyboardLabels(KeyboardLanguage.RUSSIAN).planeKey(KeyboardPlane.NUMBERS))
        assertEquals("ҚАЗ", KeyboardLabels(KeyboardLanguage.KAZAKH).badge)
    }
}
