package kz.yerek.aireply

import kz.yerek.aireply.keyboard.layout.PersonaRowLayout
import kz.yerek.aireply.keyboard.layout.PersonaRowLayout.Slot
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The row above the keys: ✨ at the leading edge, the personas (or, while a
 * word is typed, the suggestions) in everything to its right. There is no
 * "+" any more.
 *
 * Пернетақтаның жоғарғы жолы: ✨ сол жақта, персоналар оның оң жағында.
 */
class PersonaRowLayoutTest {

    private val widths = listOf(320f, 360f, 393f, 411f, 480f)

    /** Rough 14 sp semibold label widths of Друг, Клиент, Бизнес, Работа. */
    private val builtIns = listOf(34f, 50f, 53f, 54f)

    @Test
    fun `create comes first, then the chips, and nothing else`() {
        assertEquals(listOf(Slot.CREATE, Slot.CHIPS), PersonaRowLayout.order)
    }

    @Test
    fun `the row keeps its height`() {
        assertEquals(44f, PersonaRowLayout.HEIGHT)
    }

    @Test
    fun `the chips start right of create and run to the trailing edge`() {
        val createEnd = PersonaRowLayout.SIDE_PADDING + PersonaRowLayout.CREATE_SIZE
        assertTrue(PersonaRowLayout.CHIPS_START > createEnd)
        for (width in widths) {
            val end = PersonaRowLayout.CHIPS_START + PersonaRowLayout.chipsWidth(width)
            assertEquals("w=$width", width - PersonaRowLayout.SIDE_PADDING, end, 0.001f)
        }
    }

    @Test
    fun `the four built-ins fill the chips area without scrolling`() {
        for (width in widths.filter { it >= 360f }) {
            val available = PersonaRowLayout.chipsWidth(width)
            val chips = PersonaRowLayout.chipWidths(builtIns, available)
            val used = chips.sum() + PersonaRowLayout.CHIP_GAP * (chips.size - 1)
            assertTrue("w=$width used=$used of $available", used <= available)
            // Stretched evenly by whole dp: short of the edge by less than a dp per chip.
            assertTrue("w=$width used=$used of $available", available - used < chips.size)
            chips.zip(builtIns).forEach { (chip, text) ->
                assertEquals("w=$width", chip - text, chips[0] - builtIns[0], 0.001f)
            }
        }
    }

    @Test
    fun `more personas than fit keep their natural width and scroll`() {
        val many = builtIns + listOf(70f, 80f, 90f)
        val available = PersonaRowLayout.chipsWidth(320f)
        val chips = PersonaRowLayout.chipWidths(many, available)
        val tightest = PersonaRowLayout.CHIP_PADDINGS.last() * 2
        assertEquals(many.map { it + tightest }, chips)
        assertTrue(chips.sum() + PersonaRowLayout.CHIP_GAP * (chips.size - 1) > available)
    }

    @Test
    fun `no personas, no chips`() {
        assertEquals(emptyList<Float>(), PersonaRowLayout.chipWidths(emptyList(), 300f))
    }
}
