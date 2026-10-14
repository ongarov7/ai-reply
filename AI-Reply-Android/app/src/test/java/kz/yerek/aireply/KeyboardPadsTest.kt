package kz.yerek.aireply

import android.text.InputType
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.core.lang.KeyboardPlane
import kz.yerek.aireply.keyboard.KeyVisualStyle
import kz.yerek.aireply.keyboard.KeyboardKey
import kz.yerek.aireply.keyboard.layout.FieldKeys
import kz.yerek.aireply.keyboard.layout.FieldKind
import kz.yerek.aireply.keyboard.layout.KeyWidth
import kz.yerek.aireply.keyboard.layout.KeyboardLabels
import kz.yerek.aireply.keyboard.layout.KeyboardLayout
import kz.yerek.aireply.keyboard.layout.KeyboardPage
import kz.yerek.aireply.keyboard.layout.PageOptions
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Numbers and symbols: the pads a number, phone or date field opens on, the
 * ?123 / =\< / ABC pages and the keys between them, and a password field's
 * row of digits.
 *
 * Сандар мен таңбалар: сандық панельдер, ?123 / =\< / ABC, құпиясөз өрісі.
 */
class KeyboardPadsTest {

    private fun page(
        plane: KeyboardPlane,
        options: PageOptions = PageOptions(),
        language: KeyboardLanguage = KeyboardLanguage.KAZAKH
    ) = KeyboardLayout.page(language, plane, options)

    /** Each row's keys, written the way they read: characters as themselves, the rest by name. */
    private fun KeyboardPage.faces(): List<List<String>> = rows.map { row ->
        row.slots.mapNotNull { slot ->
            when (val key = slot.key) {
                null -> null
                is KeyboardKey.Character -> key.value
                is KeyboardKey.Plane -> "plane:${key.target}"
                else -> key.toString()
            }
        }
    }

    private val KeyboardPage.keys: List<KeyboardKey> get() = rows.flatMap { row -> row.slots.mapNotNull { it.key } }

    private val pads = listOf(KeyboardPlane.PHONE_PAD, KeyboardPlane.NUMBER_PAD, KeyboardPlane.DATE_PAD)

    // ------------------------------------------------------------------ pads

    @Test
    fun `the phone pad is a dialler with plus, brackets and dash beside it`() {
        assertEquals(
            listOf(
                listOf("+", "1", "2", "3", "plane:LETTERS"),
                listOf("(", "4", "5", "6", "Space"),
                listOf(")", "7", "8", "9", "Backspace"),
                listOf("-", "*", "0", "#", "Return")
            ),
            page(KeyboardPlane.PHONE_PAD).faces()
        )
        // A held 0 is + on every dialler, with the + drawn in its corner.
        val zero = page(KeyboardPlane.PHONE_PAD).rows[3].slots.first { it.key == KeyboardKey.Character("0") }
        assertEquals(listOf("+"), zero.alternates)
        assertEquals("+", zero.hint)
    }

    @Test
    fun `a plain number field gets digits and nothing it would refuse`() {
        val plain = page(KeyboardPlane.NUMBER_PAD)
        assertEquals(
            listOf(
                listOf("1", "2", "3", "plane:LETTERS"),
                listOf("4", "5", "6", "Space"),
                listOf("7", "8", "9", "Backspace"),
                listOf("0", "Return")
            ),
            plain.faces()
        )
        val characters = plain.characterRows.flatten()
        assertFalse("-" in characters)
        assertFalse("." in characters)
        // The digits carry no superscripts or fractions on a long press.
        assertTrue(plain.rows.flatMap { it.slots }.all { it.alternates.isEmpty() })
    }

    @Test
    fun `minus only when signed, the point only when decimal`() {
        val signed = page(KeyboardPlane.NUMBER_PAD, PageOptions(signed = true))
        assertEquals(listOf("-", "0", "Return"), signed.faces().last())

        val decimal = page(KeyboardPlane.NUMBER_PAD, PageOptions(decimal = true))
        assertEquals(listOf("0", ".", "Return"), decimal.faces().last())
        // Kazakh and Russian write 3,5: the comma is a long press on the point.
        val point = decimal.rows.last().slots.first { it.key == KeyboardKey.Character(".") }
        assertEquals(listOf(","), point.alternates)
        assertEquals(",", point.hint)

        val both = page(KeyboardPlane.NUMBER_PAD, PageOptions(signed = true, decimal = true))
        assertEquals(listOf("-", "0", ".", "Return"), both.faces().last())
    }

    @Test
    fun `the zero spans the places the field has no character for`() {
        fun zero(options: PageOptions) =
            page(KeyboardPlane.NUMBER_PAD, options).rows.last().slots.first { it.key == KeyboardKey.Character("0") }.width
        assertEquals(KeyWidth.Flexible(3f, gaps = 2), zero(PageOptions()))
        assertEquals(KeyWidth.Flexible(2f, gaps = 1), zero(PageOptions(signed = true)))
        assertEquals(KeyWidth.Flexible(1f), zero(PageOptions(signed = true, decimal = true)))
    }

    @Test
    fun `the date pad has the date and time separators and space`() {
        val date = page(KeyboardPlane.DATE_PAD)
        assertEquals(listOf(".", "/", ":", "-"), date.rows.map { (it.slots.first().key as KeyboardKey.Character).value })
        assertEquals(listOf("-", "0", "Return"), date.faces().last())
        assertTrue(KeyboardKey.Space in date.keys)
    }

    @Test
    fun `every pad has delete, space, return and ABC - and no language or globe`() {
        for (plane in pads) for (language in KeyboardLanguage.entries) {
            val keys = page(plane, PageOptions(showsGlobeKey = true, showsLanguageKey = true), language).keys
            listOf(KeyboardKey.Backspace, KeyboardKey.Space, KeyboardKey.Return, KeyboardKey.Plane(KeyboardPlane.LETTERS))
                .forEach { assertEquals("$plane $language has one $it", 1, keys.count { key -> key == it }) }
            assertFalse(KeyboardKey.Layout in keys)
            assertFalse(KeyboardKey.Globe in keys)
            assertFalse(KeyboardKey.Shift in keys)
        }
    }

    @Test
    fun `every pad has the digits once, drawn like letters, and its side keys like the controls`() {
        for (plane in pads) {
            val pad = page(plane, PageOptions(signed = true, decimal = true))
            val characters = pad.characterRows.flatten()
            "0123456789".forEach { digit -> assertEquals("$plane $digit", 1, characters.count { it == digit.toString() }) }
            pad.rows.flatMap { it.slots }.forEach { slot ->
                val value = (slot.key as? KeyboardKey.Character)?.value ?: return@forEach
                if (value.single().isDigit()) assertEquals("$plane $value", KeyVisualStyle.LETTER, slot.style)
            }
            pad.rows.forEach { row ->
                val first = row.slots.first()
                if (first.key is KeyboardKey.Character && plane != KeyboardPlane.NUMBER_PAD) {
                    assertEquals("$plane side ${first.key}", KeyVisualStyle.SPECIAL, first.style)
                }
            }
        }
    }

    @Test
    fun `a pad has four rows, so it fits the height of every layout set`() {
        for (plane in pads) {
            assertEquals(4, page(plane).rows.size)
            assertEquals(4, KeyboardLayout.rowCount(KeyboardLanguage.ENGLISH, plane))
        }
        val smallest = KeyboardLayout.maximumRowCount(listOf(KeyboardLanguage.ENGLISH))
        assertTrue(pads.all { KeyboardLayout.rowCount(KeyboardLanguage.ENGLISH, it) <= smallest })
    }

    @Test
    fun `the pads say 123 and ABC returns to the layout's own letters`() {
        assertEquals("ABC", KeyboardLabels(KeyboardLanguage.ENGLISH).planeKey(KeyboardPlane.LETTERS))
        assertEquals("АБВ", KeyboardLabels(KeyboardLanguage.RUSSIAN).planeKey(KeyboardPlane.LETTERS))
        assertEquals("АӘБ", KeyboardLabels(KeyboardLanguage.KAZAKH).planeKey(KeyboardPlane.LETTERS))
        pads.forEach { assertEquals("123", KeyboardLabels(KeyboardLanguage.KAZAKH).planeKey(it)) }
    }

    // --------------------------------------------------------- ?123 and =\<

    @Test
    fun `every letters page in every field has ?123 in the bottom left`() {
        for (language in KeyboardLanguage.entries) for (field in FieldKind.entries) for (globe in listOf(false, true)) {
            for (numberRow in listOf(false, true)) {
                val letters = page(KeyboardPlane.LETTERS, PageOptions(showsGlobeKey = globe, field = field, numberRow = numberRow), language)
                assertEquals(
                    "$language $field globe=$globe",
                    KeyboardKey.Plane(KeyboardPlane.NUMBERS),
                    letters.rows.last().slots.first().key
                )
            }
        }
        assertEquals("?123", KeyboardLabels(KeyboardLanguage.ENGLISH).planeKey(KeyboardPlane.NUMBERS))
    }

    @Test
    fun `?123 leads to the symbols page and back, and both lead to ABC`() {
        for (language in KeyboardLanguage.entries) {
            val numbers = page(KeyboardPlane.NUMBERS, language = language)
            assertEquals(KeyboardKey.Plane(KeyboardPlane.SYMBOLS), numbers.rows[2].slots.first().key)
            assertEquals(KeyboardKey.Plane(KeyboardPlane.LETTERS), numbers.rows.last().slots.first().key)

            val symbols = page(KeyboardPlane.SYMBOLS, language = language)
            assertEquals(KeyboardKey.Plane(KeyboardPlane.NUMBERS), symbols.rows[2].slots.first().key)
            assertEquals(KeyboardKey.Plane(KeyboardPlane.LETTERS), symbols.rows.last().slots.first().key)

            // Delete is on both, at the end of the third row.
            assertEquals(KeyboardKey.Backspace, numbers.rows[2].slots.last().key)
            assertEquals(KeyboardKey.Backspace, symbols.rows[2].slots.last().key)
        }
        assertEquals("=\\<", KeyboardLabels(KeyboardLanguage.ENGLISH).planeKey(KeyboardPlane.SYMBOLS))
    }

    @Test
    fun `the punctuation pages have every common character, each typed as itself`() {
        for (language in KeyboardLanguage.entries) {
            // The pages' own rows; the bottom row's comma and full stop are on both.
            val numbers = page(KeyboardPlane.NUMBERS, language = language).characterRows.dropLast(1).flatten()
            val symbols = page(KeyboardPlane.SYMBOLS, language = language).characterRows.dropLast(1).flatten()
            "0123456789@#*+-/&!?()\"':;_".forEach { assertTrue("$language ?123 has $it", it.toString() in numbers) }
            "%=<>[]{}\\~|^`".forEach { assertTrue("$language =\\< has $it", it.toString() in symbols) }
            // Every printable ASCII character that is not a letter has a key of
            // its own: on ?123, on =\<, or the comma and full stop beside space.
            val bottom = listOf(",", ".")
            (33..126).map { it.toChar() }.filter { !it.isLetter() }.forEach { character ->
                assertTrue("$language '$character' has a key", character.toString() in numbers + symbols + bottom)
            }
            // One character per key, never a word: what the key shows is what it types.
            (numbers + symbols).forEach { assertEquals(it, 1, it.codePointCount(0, it.length)) }
            assertEquals("no character twice", (numbers + symbols).size, (numbers + symbols).toSet().size)
        }
    }

    // -------------------------------------------------------- password row

    @Test
    fun `a password page gets the digits on top and drops their corner hints`() {
        for (language in listOf(KeyboardLanguage.ENGLISH, KeyboardLanguage.RUSSIAN)) {
            val plain = page(KeyboardPlane.LETTERS, language = language)
            val password = page(KeyboardPlane.LETTERS, PageOptions(numberRow = true), language)
            assertEquals(plain.rows.size + 1, password.rows.size)
            assertEquals("1234567890", password.characterRows.first().joinToString(""))
            assertEquals(10, password.rows.first().unitColumns)
            assertTrue(password.rows.all { row -> row.slots.all { it.hint == null } })
            // The letters themselves are untouched.
            assertEquals(plain.characterRows, password.characterRows.drop(1))
        }
    }

    @Test
    fun `the number row only goes where the keyboard's height has room for it`() {
        val all = KeyboardLanguage.CYCLE_ORDER
        assertTrue(KeyboardLayout.numberRowFits(KeyboardLanguage.ENGLISH, all))
        assertTrue(KeyboardLayout.numberRowFits(KeyboardLanguage.RUSSIAN, all))
        // Kazakh already has five rows; English and Russian alone are four high.
        assertFalse(KeyboardLayout.numberRowFits(KeyboardLanguage.KAZAKH, all))
        val latinCyrillic = listOf(KeyboardLanguage.ENGLISH, KeyboardLanguage.RUSSIAN)
        assertFalse(KeyboardLayout.numberRowFits(KeyboardLanguage.ENGLISH, latinCyrillic))
        assertFalse(KeyboardLayout.numberRowFits(KeyboardLanguage.RUSSIAN, latinCyrillic))
        for (language in all) {
            if (KeyboardLayout.numberRowFits(language, all)) {
                val password = page(KeyboardPlane.LETTERS, PageOptions(numberRow = true), language)
                assertTrue(password.rows.size <= KeyboardLayout.maximumRowCount(all))
            }
        }
    }

    // ------------------------------------------------------- the host field

    @Test
    fun `numbers, phones and dates open on their pads`() {
        assertEquals(KeyboardPlane.PHONE_PAD, FieldKeys.of(InputType.TYPE_CLASS_PHONE).plane)
        assertEquals(KeyboardPlane.NUMBER_PAD, FieldKeys.of(InputType.TYPE_CLASS_NUMBER).plane)
        assertEquals(
            KeyboardPlane.NUMBER_PAD,
            FieldKeys.of(InputType.TYPE_CLASS_NUMBER or InputType.TYPE_NUMBER_VARIATION_PASSWORD).plane
        )
        listOf(
            InputType.TYPE_DATETIME_VARIATION_NORMAL,
            InputType.TYPE_DATETIME_VARIATION_DATE,
            InputType.TYPE_DATETIME_VARIATION_TIME
        ).forEach { variation ->
            assertEquals(KeyboardPlane.DATE_PAD, FieldKeys.of(InputType.TYPE_CLASS_DATETIME or variation).plane)
        }
        assertTrue(FieldKeys.of(InputType.TYPE_CLASS_PHONE).isNumeric)
    }

    @Test
    fun `the number flags give the pad its minus and its point`() {
        val number = InputType.TYPE_CLASS_NUMBER
        assertEquals(FieldKeys(plane = KeyboardPlane.NUMBER_PAD), FieldKeys.of(number))
        assertTrue(FieldKeys.of(number or InputType.TYPE_NUMBER_FLAG_SIGNED).signed)
        assertFalse(FieldKeys.of(number or InputType.TYPE_NUMBER_FLAG_SIGNED).decimal)
        assertTrue(FieldKeys.of(number or InputType.TYPE_NUMBER_FLAG_DECIMAL).decimal)
        // The text class's CAP_CHARACTERS shares the signed flag's bit: a text field is never "signed".
        assertFalse(FieldKeys.of(InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_CAP_CHARACTERS).signed)
    }

    @Test
    fun `text fields open on the letters, with their own bottom-row key`() {
        val text = InputType.TYPE_CLASS_TEXT
        assertEquals(FieldKeys(), FieldKeys.of(text or InputType.TYPE_TEXT_VARIATION_SHORT_MESSAGE))
        assertEquals(FieldKind.EMAIL, FieldKeys.of(text or InputType.TYPE_TEXT_VARIATION_EMAIL_ADDRESS).kind)
        assertEquals(FieldKind.EMAIL, FieldKeys.of(text or InputType.TYPE_TEXT_VARIATION_WEB_EMAIL_ADDRESS).kind)
        assertEquals(FieldKind.URL, FieldKeys.of(text or InputType.TYPE_TEXT_VARIATION_URI).kind)
        listOf(
            InputType.TYPE_TEXT_VARIATION_PASSWORD,
            InputType.TYPE_TEXT_VARIATION_VISIBLE_PASSWORD,
            InputType.TYPE_TEXT_VARIATION_WEB_PASSWORD
        ).forEach { variation ->
            val keys = FieldKeys.of(text or variation)
            assertTrue(keys.password)
            assertEquals(KeyboardPlane.LETTERS, keys.plane)
        }
        assertFalse(FieldKeys.of(text).password)
        // A terminal (TYPE_NULL) types letters.
        assertEquals(FieldKeys(), FieldKeys.of(InputType.TYPE_NULL))
        assertFalse(FieldKeys.of(text).isNumeric)
    }

    @Test
    fun `a new field opens on its own page whatever was up before`() {
        val phone = FieldKeys.of(InputType.TYPE_CLASS_PHONE)
        val chat = FieldKeys.of(InputType.TYPE_CLASS_TEXT)
        assertEquals(KeyboardPlane.PHONE_PAD, phone.startPlane(KeyboardPlane.SYMBOLS, chat, restarting = false, composing = false))
        assertEquals(KeyboardPlane.LETTERS, chat.startPlane(KeyboardPlane.PHONE_PAD, phone, restarting = false, composing = false))
        // Shown again over the same field: Gboard starts it afresh too.
        assertEquals(KeyboardPlane.PHONE_PAD, phone.startPlane(KeyboardPlane.LETTERS, phone, restarting = false, composing = false))
    }

    @Test
    fun `a restart of the same field keeps the page the user chose`() {
        val phone = FieldKeys.of(InputType.TYPE_CLASS_PHONE)
        val chat = FieldKeys.of(InputType.TYPE_CLASS_TEXT)
        // ABC on the phone pad, then the app restarts the field: still letters.
        assertEquals(KeyboardPlane.LETTERS, phone.startPlane(KeyboardPlane.LETTERS, phone, restarting = true, composing = false))
        // ?123 in a chat, the messenger restarts it after Send: still ?123.
        assertEquals(KeyboardPlane.NUMBERS, chat.startPlane(KeyboardPlane.NUMBERS, chat, restarting = true, composing = false))
        // A restart that turned the field into a number field opens its pad,
        assertEquals(KeyboardPlane.PHONE_PAD, phone.startPlane(KeyboardPlane.NUMBERS, chat, restarting = true, composing = false))
        // and one that turned it back into text leaves the pad.
        assertEquals(KeyboardPlane.LETTERS, chat.startPlane(KeyboardPlane.PHONE_PAD, phone, restarting = true, composing = false))
    }

    @Test
    fun `an open AI panel types letters, never a pad`() {
        val phone = FieldKeys.of(InputType.TYPE_CLASS_PHONE)
        val chat = FieldKeys.of(InputType.TYPE_CLASS_TEXT)
        assertEquals(KeyboardPlane.LETTERS, phone.startPlane(KeyboardPlane.LETTERS, chat, restarting = false, composing = true))
        assertEquals(KeyboardPlane.LETTERS, phone.startPlane(KeyboardPlane.PHONE_PAD, phone, restarting = true, composing = true))
        // The page picked inside the panel stays across a restart of the host.
        assertEquals(KeyboardPlane.NUMBERS, phone.startPlane(KeyboardPlane.NUMBERS, phone, restarting = true, composing = true))
    }
}
