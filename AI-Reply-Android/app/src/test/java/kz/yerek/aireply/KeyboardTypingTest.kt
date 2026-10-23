package kz.yerek.aireply

import kz.yerek.aireply.keyboard.input.KeyboardTextFieldState
import kz.yerek.aireply.keyboard.layout.AutoCapitalization
import kz.yerek.aireply.keyboard.layout.Capitalization
import kz.yerek.aireply.keyboard.layout.ShiftState
import kz.yerek.aireply.keyboard.layout.SpaceShortcut
import kz.yerek.aireply.keyboard.layout.PunctuationSpaceShortcut
import kz.yerek.aireply.keyboard.layout.FieldKeys
import kz.yerek.aireply.keyboard.layout.FieldKind
import kz.yerek.aireply.core.lang.KeyboardPlane
import kz.yerek.aireply.keyboard.layout.TextDeletion
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** Shift, auto-capitals, the double-space full stop, deletion and the composer's fields. */
class KeyboardTypingTest {

    // ------------------------------------------------------------------ shift

    @Test
    fun `one tap shifts one letter`() {
        val shift = ShiftState()
        shift.tap(1_000)
        assertEquals(ShiftState.Mode.ONCE, shift.mode)
        assertEquals("Ә", shift.apply("ә"))
        shift.characterTyped()
        assertEquals(ShiftState.Mode.OFF, shift.mode)
    }

    @Test
    fun `two quick taps lock caps and a third unlocks`() {
        val shift = ShiftState()
        shift.tap(1_000)
        shift.tap(1_200)
        assertEquals(ShiftState.Mode.LOCKED, shift.mode)
        shift.characterTyped()
        assertEquals(ShiftState.Mode.LOCKED, shift.mode)
        shift.tap(5_000)
        assertEquals(ShiftState.Mode.OFF, shift.mode)
    }

    @Test
    fun `two slow taps are on then off`() {
        val shift = ShiftState()
        shift.tap(1_000)
        shift.tap(2_000)
        assertEquals(ShiftState.Mode.OFF, shift.mode)
    }

    @Test
    fun `automatic shift is taken back but a manual one is not`() {
        val shift = ShiftState()
        shift.applyAutomatic(true)
        assertTrue(shift.isAutomatic)
        shift.applyAutomatic(false)
        assertEquals(ShiftState.Mode.OFF, shift.mode)

        shift.tap(10_000)
        shift.applyAutomatic(false)
        assertEquals("a shift the user set stays", ShiftState.Mode.ONCE, shift.mode)
    }

    // ------------------------------------------------------ auto-capitals

    @Test
    fun `sentence starts`() {
        assertTrue(AutoCapitalization.isSentenceStart(null))
        assertTrue(AutoCapitalization.isSentenceStart(""))
        assertTrue(AutoCapitalization.isSentenceStart("Сәлем. "))
        assertTrue(AutoCapitalization.isSentenceStart("Қалайсыз? "))
        assertTrue(AutoCapitalization.isSentenceStart("Иә!  "))
        assertTrue(AutoCapitalization.isSentenceStart("first line\n"))
        assertTrue(AutoCapitalization.isSentenceStart("He said \"Yes.\" "))
        assertFalse(AutoCapitalization.isSentenceStart("Сәлем."))
        assertFalse(AutoCapitalization.isSentenceStart("Сәлем, "))
        assertFalse(AutoCapitalization.isSentenceStart("сәлем "))
    }

    @Test
    fun `the field's capitalization mode decides`() {
        assertFalse(AutoCapitalization.shouldCapitalize("", Capitalization.NONE))
        assertTrue(AutoCapitalization.shouldCapitalize("abc", Capitalization.CHARACTERS))
        assertTrue(AutoCapitalization.shouldCapitalize("Aigul ", Capitalization.WORDS))
        assertFalse(AutoCapitalization.shouldCapitalize("Aigul", Capitalization.WORDS))
    }

    // ------------------------------------------------------- double space

    @Test
    fun `a quick second space after a word is a full stop`() {
        assertTrue(SpaceShortcut.shouldInsertPeriod("Сәлем ", 200))
        assertFalse(SpaceShortcut.shouldInsertPeriod("Сәлем ", 900))
        assertFalse(SpaceShortcut.shouldInsertPeriod("Сәлем. ", 200))
        assertFalse(SpaceShortcut.shouldInsertPeriod("Сәлем  ", 200))
        assertFalse(SpaceShortcut.shouldInsertPeriod(" ", 200))
    }

    // -------------------------------------------------------------- delete

    @Test
    fun `word delete takes the word and the spaces before the caret`() {
        assertEquals(5, TextDeletion.wordLength("Сәлем"))
        assertEquals(6, TextDeletion.wordLength("Иә, ертең "))
        assertEquals(1, TextDeletion.wordLength("ертең,"))
        assertEquals(0, TextDeletion.wordLength(""))
        assertEquals(3, TextDeletion.wordLength("   "))
        // An emoji goes whole, never half a surrogate pair.
        assertEquals(2, TextDeletion.wordLength("ok 🚚"))
    }

    @Test
    fun `composer fields edit whole characters`() {
        val field = KeyboardTextFieldState("Жеткіземіз 🚚")
        assertTrue(field.deleteBackward())
        assertEquals("Жеткіземіз ", field.text)
        field.set("Иә, ертең болады")
        assertTrue(field.deleteWordBackward())
        assertEquals("Иә, ертең ", field.text)
        field.moveCursor(0)
        field.moveCursorBy(3)
        assertEquals(3, field.cursor)
        assertTrue(field.insert("!"))
        assertEquals("Иә,! ертең ", field.text)
    }

    @Test
    fun `an insertion past the limit is refused at the keystroke`() {
        val field = KeyboardTextFieldState("12345")
        assertFalse(field.insert("6", limit = 5))
        assertEquals("12345", field.text)
        assertTrue(field.insert("6", limit = 6))
    }
    @Test
    fun `punctuation and one space return both punctuation pages to letters in every language`() {
        for (plane in listOf(KeyboardPlane.NUMBERS, KeyboardPlane.SYMBOLS)) {
            for (text in listOf("Привет", "Сәлем", "Hello")) {
                for (punctuation in listOf(".", ",", "!", "?", ":", ";", "?!", "...", "…")) {
                    assertTrue("$plane $text$punctuation", PunctuationSpaceShortcut.returnsToLetters(plane, text + punctuation, true))
                }
            }
        }
    }

    @Test
    fun `spaces after digits symbols or an existing space preserve the selected page`() {
        for (plane in KeyboardPlane.entries) for (text in listOf("123", "12.5", "$", "_", "! ", "", " ")) {
            assertFalse("$plane $text", PunctuationSpaceShortcut.returnsToLetters(plane, text, true))
        }
        for (plane in KeyboardPlane.entries.filter { it != KeyboardPlane.NUMBERS && it != KeyboardPlane.SYMBOLS }) {
            assertFalse("$plane", PunctuationSpaceShortcut.returnsToLetters(plane, "!", true))
        }
    }

    @Test
    fun `password address and numeric fields never apply sentence shortcuts`() {
        val specialized = listOf(
            FieldKeys(kind = FieldKind.EMAIL), FieldKeys(kind = FieldKind.URL), FieldKeys(password = true),
            FieldKeys(plane = KeyboardPlane.NUMBER_PAD), FieldKeys(plane = KeyboardPlane.PHONE_PAD),
            FieldKeys(plane = KeyboardPlane.DATE_PAD)
        )
        for (field in specialized) {
            assertFalse("$field", field.takesSentenceShortcuts)
            assertFalse("$field", PunctuationSpaceShortcut.returnsToLetters(KeyboardPlane.NUMBERS, "!", field.takesSentenceShortcuts))
        }
        assertTrue(FieldKeys().takesSentenceShortcuts)
    }

}
