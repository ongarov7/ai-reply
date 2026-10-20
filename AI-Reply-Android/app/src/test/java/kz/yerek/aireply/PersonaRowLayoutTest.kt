package kz.yerek.aireply

import kz.yerek.aireply.keyboard.layout.PersonaRowLayout
import kz.yerek.aireply.keyboard.layout.PersonaRowLayout.Slot
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
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

    /** Rough 14 sp semibold label widths of Друг, Клиент, Коллега, Партнёр. */
    private val builtIns = listOf(34f, 50f, 58f, 60f)

    /** Дос, Клиент, Әріптес, Серіктес. */
    private val kazakh = listOf(27f, 50f, 56f, 63f)

    /** Friend, Client, Colleague, Partner. */
    private val english = listOf(45f, 41f, 68f, 52f)

    private val allIcons = listOf(true, true, true, true)

    private fun used(widths: List<Float>) = widths.sum() + PersonaRowLayout.CHIP_GAP * (widths.size - 1)

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
        for (width in widths) {
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
        assertEquals(PersonaRowLayout.ChipFit(emptyList(), false), PersonaRowLayout.chipFit(emptyList(), emptyList(), 300f))
    }

    // ---------------------------------------------------------------- icons

    @Test
    fun `icons show only where the whole set fits with them`() {
        for (labels in listOf(builtIns, kazakh, english)) {
            for (width in listOf(320f, 360f, 393f, 411f, 480f)) {
                for (mic in listOf(false, true)) {
                    val available = PersonaRowLayout.chipsWidth(width, hasMic = mic)
                    val fit = PersonaRowLayout.chipFit(labels, allIcons, available)
                    val label = "labels=$labels w=$width mic=$mic"
                    if (fit.showsIcons) {
                        assertTrue(label, used(fit.widths) <= available)
                        // Every chip has room for its icon and at least the tightest icon padding.
                        fit.widths.zip(labels).forEach { (chip, text) ->
                            assertTrue(label, chip >= text + PersonaRowLayout.ICON_WIDTH + PersonaRowLayout.CHIP_PADDINGS.last() * 2 - 0.001f)
                        }
                    } else {
                        assertEquals(label, PersonaRowLayout.chipWidths(labels, available), fit.widths)
                    }
                }
            }
        }
    }

    @Test
    fun `icons go before the chips would scroll`() {
        // 411 dp, no 🎙: the Russian set fits with its icons.
        val roomy = PersonaRowLayout.chipFit(builtIns, allIcons, PersonaRowLayout.chipsWidth(411f))
        assertTrue(roomy.showsIcons)
        // 393 dp: not with the icons, but every label fits at a comfortable padding.
        val phone = PersonaRowLayout.chipFit(builtIns, allIcons, PersonaRowLayout.chipsWidth(393f))
        assertFalse(phone.showsIcons)
        assertTrue(used(phone.widths) <= PersonaRowLayout.chipsWidth(393f))
        // 480 dp: icons at the roomiest padding, widened to fill.
        val wide = PersonaRowLayout.chipFit(builtIns, allIcons, PersonaRowLayout.chipsWidth(480f))
        assertTrue(wide.showsIcons)
        assertTrue(PersonaRowLayout.chipsWidth(480f) - used(wide.widths) < wide.widths.size)
    }

    @Test
    fun `at 360 dp with the mic the four labels still fit, without icons`() {
        for (labels in listOf(builtIns, kazakh, english)) {
            val available = PersonaRowLayout.chipsWidth(360f, hasMic = true)
            val fit = PersonaRowLayout.chipFit(labels, allIcons, available)
            assertFalse("$labels", fit.showsIcons)
            assertTrue("$labels used=${used(fit.widths)} of $available", used(fit.widths) <= available)
            fit.widths.zip(labels).forEach { (chip, text) ->
                assertTrue("$labels", chip >= text + PersonaRowLayout.TIGHT_PADDING * 2 - 0.001f)
            }
        }
    }

    @Test
    fun `at 320 dp with the mic the labels scroll at their natural width, without icons`() {
        val available = PersonaRowLayout.chipsWidth(320f, hasMic = true)
        val fit = PersonaRowLayout.chipFit(builtIns, allIcons, available)
        assertFalse(fit.showsIcons)
        assertEquals(builtIns.map { it + PersonaRowLayout.CHIP_PADDINGS.last() * 2 }, fit.widths)
        assertTrue(used(fit.widths) > available)
    }

    @Test
    fun `a custom chip has no icon and takes no icon width`() {
        val labels = builtIns + 70f
        val icons = allIcons + false
        val fit = PersonaRowLayout.chipFit(labels, icons, 600f)
        assertTrue(fit.showsIcons)
        val builtInPadding = fit.widths[0] - builtIns[0] - PersonaRowLayout.ICON_WIDTH
        assertEquals(builtInPadding, fit.widths.last() - 70f, 0.001f)
    }

    @Test
    fun `icons never change the row`() {
        assertEquals(44f, PersonaRowLayout.HEIGHT)
        assertTrue(PersonaRowLayout.ICON_SIZE < 30f)
        for (width in widths) {
            // ✨ and 🎙 keep their places whatever the chips show.
            assertEquals(width - PersonaRowLayout.SIDE_PADDING,
                PersonaRowLayout.CHIPS_START + PersonaRowLayout.chipsWidth(width, hasMic = true) +
                    PersonaRowLayout.SLOT_GAP + PersonaRowLayout.MIC_SIZE, 0.001f)
        }
    }

    @Test
    fun `the mic mirrors create at the trailing edge and the chips keep the space between`() {
        assertEquals(listOf(Slot.CREATE, Slot.CHIPS, Slot.MIC), PersonaRowLayout.order(hasMic = true))
        assertEquals(listOf(Slot.CREATE, Slot.CHIPS), PersonaRowLayout.order(hasMic = false))
        assertEquals(PersonaRowLayout.CREATE_SIZE, PersonaRowLayout.MIC_SIZE)
        for (width in listOf(320f, 360f, 411f, 600f)) {
            val withMic = PersonaRowLayout.chipsWidth(width, hasMic = true)
            val end = PersonaRowLayout.CHIPS_START + withMic + PersonaRowLayout.SLOT_GAP + PersonaRowLayout.MIC_SIZE
            assertEquals("w=$width", width - PersonaRowLayout.SIDE_PADDING, end, 0.001f)
            assertEquals(PersonaRowLayout.chipsWidth(width) - PersonaRowLayout.SLOT_GAP - PersonaRowLayout.MIC_SIZE, withMic, 0.001f)
        }
    }
}
