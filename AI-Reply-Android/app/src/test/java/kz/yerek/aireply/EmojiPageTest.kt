package kz.yerek.aireply

import android.text.InputType
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.core.lang.KeyboardPlane
import kz.yerek.aireply.keyboard.KeyboardKey
import kz.yerek.aireply.keyboard.layout.FieldKeys
import kz.yerek.aireply.keyboard.layout.FieldKind
import kz.yerek.aireply.keyboard.layout.KeyWidth
import kz.yerek.aireply.keyboard.layout.KeyboardLabels
import kz.yerek.aireply.keyboard.layout.KeyboardLayout
import kz.yerek.aireply.keyboard.layout.PageOptions
import kz.yerek.aireply.keyboard.layout.PanelKind
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The emoji page in the page model (contract §3.10): the key right after
 * ?123 on the letters and nowhere else, the page itself, and the fields it
 * never reaches.
 *
 * Эмодзи беті: ?123-тен кейінгі перне, тек әріптер бетінде.
 */
class EmojiPageTest {

    private fun bottomKeys(language: KeyboardLanguage, plane: KeyboardPlane, options: PageOptions): List<KeyboardKey?> =
        KeyboardLayout.page(language, plane, options).rows.last().slots.map { it.key }

    @Test
    fun `the emoji key comes right after the numbers key on the letters`() {
        for (language in KeyboardLanguage.entries) for (globe in listOf(false, true)) {
            val keys = bottomKeys(language, KeyboardPlane.LETTERS, PageOptions(showsGlobeKey = globe, showsEmojiKey = true))
            assertEquals("$language", KeyboardKey.Plane(KeyboardPlane.NUMBERS), keys[0])
            assertEquals("$language", KeyboardKey.Plane(KeyboardPlane.EMOJI), keys[1])
            assertEquals("$language globe=$globe", if (globe) KeyboardKey.Globe else KeyboardKey.Character(","), keys[2])
            // A whole key, like the comma's.
            val slot = KeyboardLayout.page(language, KeyboardPlane.LETTERS, PageOptions(showsEmojiKey = true)).rows.last().slots[1]
            assertEquals(KeyWidth.Units(1f), slot.width)
            assertTrue(slot.alternates.isEmpty())
        }
    }

    @Test
    fun `without the option the letters are exactly as before`() {
        for (language in KeyboardLanguage.entries) {
            val keys = bottomKeys(language, KeyboardPlane.LETTERS, PageOptions())
            assertFalse(KeyboardKey.Plane(KeyboardPlane.EMOJI) in keys)
        }
    }

    @Test
    fun `the punctuation pages and the pads never get the emoji key`() {
        for (language in KeyboardLanguage.entries) {
            for (plane in KeyboardPlane.entries.filter { it != KeyboardPlane.LETTERS && it != KeyboardPlane.EMOJI }) {
                val with = KeyboardLayout.page(language, plane, PageOptions(showsEmojiKey = true))
                val without = KeyboardLayout.page(language, plane, PageOptions())
                assertEquals("$language $plane is unchanged", without, with)
            }
        }
    }

    @Test
    fun `the emoji page is the grid over ABC, the categories and delete`() {
        for (language in KeyboardLanguage.entries) {
            val page = KeyboardLayout.page(language, KeyboardPlane.EMOJI, PageOptions())
            assertEquals(KeyboardPlane.EMOJI, page.plane)
            assertEquals(PanelKind.EMOJI_GRID, page.panelAbove)
            assertEquals("$language", KeyboardLayout.rowCount(language, KeyboardPlane.LETTERS) - 1, page.panelRows)
            assertEquals(KeyboardLayout.rowCount(language, KeyboardPlane.LETTERS), KeyboardLayout.rowCount(language, KeyboardPlane.EMOJI))
            val bar = page.rows.single()
            assertEquals(10, bar.unitColumns)
            assertEquals(KeyboardKey.Plane(KeyboardPlane.LETTERS), bar.slots[0].key)
            assertEquals(PanelKind.EMOJI_CATEGORIES, bar.slots[1].panel)
            assertNull(bar.slots[1].key)
            assertEquals(KeyboardKey.Backspace, bar.slots[2].key)
            assertEquals(3, bar.slots.size)
        }
        assertFalse("not a pad: it has letters to go back to", KeyboardPlane.EMOJI.isPad)
    }

    @Test
    fun `the key is named in the layout's language`() {
        assertEquals("Emoji", KeyboardLabels(KeyboardLanguage.ENGLISH).planeKey(KeyboardPlane.EMOJI))
        assertEquals("Эмодзи", KeyboardLabels(KeyboardLanguage.RUSSIAN).planeKey(KeyboardPlane.EMOJI))
        assertEquals("Эмодзи", KeyboardLabels(KeyboardLanguage.KAZAKH).planeKey(KeyboardPlane.EMOJI))
    }

    // -------------------------------------------------------------- fields

    private val message = FieldKeys.of(InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_SHORT_MESSAGE)
    private val password = FieldKeys.of(InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_PASSWORD)
    private val email = FieldKeys.of(InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_EMAIL_ADDRESS)
    private val url = FieldKeys.of(InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_URI)
    private val number = FieldKeys.of(InputType.TYPE_CLASS_NUMBER)
    private val phone = FieldKeys.of(InputType.TYPE_CLASS_PHONE)

    @Test
    fun `emoji belong in plain text fields only`() {
        assertTrue(message.takesEmoji)
        assertTrue(FieldKeys().takesEmoji)
        listOf(password, email, url, number, phone).forEach { assertFalse("$it", it.takesEmoji) }
        assertEquals(FieldKind.EMAIL, email.kind)
    }

    @Test
    fun `the emoji page never carries over into a field that takes no emoji`() {
        for (next in listOf(password, email, url)) for (restarting in listOf(false, true)) {
            assertEquals("$next restarting=$restarting", KeyboardPlane.LETTERS,
                next.startPlane(KeyboardPlane.EMOJI, message, restarting, composing = false))
        }
        assertEquals(KeyboardPlane.NUMBER_PAD, number.startPlane(KeyboardPlane.EMOJI, message, restarting = true, composing = false))
        assertEquals(KeyboardPlane.PHONE_PAD, phone.startPlane(KeyboardPlane.EMOJI, message, restarting = false, composing = false))
    }

    @Test
    fun `a chat restarting its field keeps the emoji page, another field opens on letters`() {
        assertEquals(KeyboardPlane.EMOJI, message.startPlane(KeyboardPlane.EMOJI, message, restarting = true, composing = false))
        assertEquals(KeyboardPlane.LETTERS, message.startPlane(KeyboardPlane.EMOJI, message, restarting = false, composing = false))
        // Typing into the reply panel: its fields are text whatever the host is.
        assertEquals(KeyboardPlane.EMOJI, email.startPlane(KeyboardPlane.EMOJI, message, restarting = true, composing = true))
    }
}
