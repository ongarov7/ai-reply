package kz.yerek.aireply

import kz.yerek.aireply.keyboard.layout.PanelFit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Create gives way on a short window - fewer lines in the request field -
 * so the keys, the space bar included, stay on screen.
 */
class PanelFitTest {

    @Test
    fun `a tall phone keeps the roomy request field`() {
        // Portrait: about 900dp of window, 260dp of keys.
        assertEquals(4, PanelFit.createInstructionLines(room = 640f, preferred = 4))
        assertEquals(3, PanelFit.createInstructionLines(room = 640f, preferred = 3))
    }

    @Test
    fun `a phone on its side keeps the whole keyboard on screen`() {
        // Landscape on the test phone: a 359dp window, 177dp of keys, a 24dp gesture bar.
        val room = 359f - 177f - 24f
        assertTrue(PanelFit.compactCreate(isLandscape = true))
        val lines = PanelFit.createInstructionLines(room, preferred = 3, compact = true)
        assertEquals(2, lines)
        val panel = PanelFit.CREATE_COMPACT_CHROME + PanelFit.CREATE_FIELD_PADDING + lines * PanelFit.CREATE_FIELD_LINE
        assertTrue("panel $panel dp does not fit in $room dp", panel <= room)
        assertFalse(PanelFit.compactCreate(isLandscape = false))
    }

    @Test
    fun `in between it shows as many lines as fit`() {
        assertEquals(2, PanelFit.createInstructionLines(room = 128f + 16f + 2 * 20f + 5f, preferred = 4))
    }

    @Test
    fun `never under one line, and unknown room keeps the preferred size`() {
        assertEquals(1, PanelFit.createInstructionLines(room = 40f, preferred = 4))
        assertEquals(1, PanelFit.createInstructionLines(room = -10f, preferred = 4))
        assertEquals(4, PanelFit.createInstructionLines(room = Float.POSITIVE_INFINITY, preferred = 4))
        assertEquals(4, PanelFit.createInstructionLines(room = Float.NaN, preferred = 4))
    }

    @Test
    fun `the dictation status is two lines, one on a phone on its side`() {
        assertEquals(2, PanelFit.voiceLines(isLandscape = false))
        assertEquals(1, PanelFit.voiceLines(isLandscape = true))
    }
}
