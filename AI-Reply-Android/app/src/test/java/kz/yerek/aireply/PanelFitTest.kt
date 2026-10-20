package kz.yerek.aireply

import kz.yerek.aireply.keyboard.layout.PanelFit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
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
        assertTrue(PanelFit.compact(isLandscape = true))
        val lines = PanelFit.createInstructionLines(room, preferred = 3, compact = true)
        assertEquals(2, lines)
        val panel = PanelFit.CREATE_COMPACT_CHROME + PanelFit.CREATE_FIELD_PADDING + lines * PanelFit.CREATE_FIELD_LINE
        assertTrue("panel $panel dp does not fit in $room dp", panel <= room)
        assertFalse(PanelFit.compact(isLandscape = false))
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

    /** The test phone on its side: a 359dp window, 177dp of keys, a 24dp gesture bar. */
    private val landscapeRoom = 359f - 177f - 24f

    private fun compactPanel(lines: Int, notice: Float) =
        PanelFit.CREATE_COMPACT_CHROME + PanelFit.CREATE_FIELD_PADDING + lines * PanelFit.CREATE_FIELD_LINE + notice

    @Test
    fun `the room is the window's less the keys, the same in every measure pass`() {
        assertEquals(landscapeRoom, PanelFit.roomAbove(window = 359f, navigationBar = 24f, keys = 177f)!!, 0.01f)
        assertEquals(0f, PanelFit.roomAbove(window = 150f, navigationBar = 24f, keys = 177f)!!, 0f)
        assertNull("no window height: the measured room decides", PanelFit.roomAbove(null, 24f, 177f))
        assertNull(PanelFit.roomAbove(Float.NaN, 24f, 177f))
        assertNull(PanelFit.roomAbove(0f, 24f, 177f))
    }

    @Test
    fun `spent replies on a phone on its side are said in Create's header, never over the space bar`() {
        assertTrue(PanelFit.createLimitInHeader(PanelFit.Notice.LIMIT, compact = true))
        assertFalse(PanelFit.createLimitInHeader(PanelFit.Notice.LIMIT, compact = false))
        assertFalse(PanelFit.createLimitInHeader(PanelFit.Notice.LINE, compact = true))
        val notice = PanelFit.createNotice(PanelFit.Notice.LIMIT, compact = true)
        assertEquals("in the line's place, it costs the field nothing", 0f, notice, 0f)
        val lines = PanelFit.createInstructionLines(landscapeRoom, preferred = 3, compact = true, notice = notice)
        assertEquals(PanelFit.createInstructionLines(landscapeRoom, preferred = 3, compact = true), lines)
        assertTrue("panel ${compactPanel(lines, notice)} dp in $landscapeRoom dp", compactPanel(lines, notice) <= landscapeRoom)
    }

    @Test
    fun `under a field on a phone on its side the limit notice is its row alone`() {
        // The reply composer, which has no header room for it.
        val notice = PanelFit.noticeHeight(PanelFit.Notice.LIMIT, compact = true)
        assertEquals(PanelFit.PANEL_GAP + PanelFit.LIMIT_ROW, notice, 0.01f)
        val line = PanelFit.noticeHeight(PanelFit.Notice.LINE, compact = false)
        assertTrue("$notice dp is no taller than an upright two-line notice ($line dp)", notice <= line)
    }

    @Test
    fun `a notice line on a phone on its side is one line, and budgeted`() {
        assertEquals(1, PanelFit.noticeLines(compact = true))
        assertEquals(PanelFit.NOTICE_LINES, PanelFit.noticeLines(compact = false))
        val notice = PanelFit.createNotice(PanelFit.Notice.LINE, compact = true)
        assertEquals(PanelFit.PANEL_GAP + PanelFit.NOTICE_LINE, notice, 0.01f)
        val lines = PanelFit.createInstructionLines(landscapeRoom, preferred = 3, compact = true, notice = notice)
        assertEquals(1, lines)
        assertTrue("panel ${compactPanel(lines, notice)} dp in $landscapeRoom dp", compactPanel(lines, notice) <= landscapeRoom)
        assertEquals(0f, PanelFit.noticeHeight(PanelFit.Notice.NONE, compact = true), 0f)
        assertEquals(0f, PanelFit.createNotice(PanelFit.Notice.NONE, compact = false), 0f)
    }

    @Test
    fun `upright the limit notice keeps its sentence and the field its lines`() {
        val notice = PanelFit.noticeHeight(PanelFit.Notice.LIMIT, compact = false)
        val sentence = PanelFit.LIMIT_BODY_GAP + PanelFit.NOTICE_LINES * PanelFit.NOTICE_LINE
        assertEquals(PanelFit.PANEL_GAP + 2 * PanelFit.LIMIT_TITLE_LINE + sentence, notice, 0.01f)
        assertEquals(4, PanelFit.createInstructionLines(room = 640f, preferred = 4, notice = notice))
    }

    @Test
    fun `larger text makes the notice taller, the pill does not`() {
        val normal = PanelFit.noticeHeight(PanelFit.Notice.LINE, compact = true)
        val large = PanelFit.noticeHeight(PanelFit.Notice.LINE, compact = true, fontScale = 1.5f)
        assertEquals(PanelFit.PANEL_GAP + 1.5f * (normal - PanelFit.PANEL_GAP), large, 0.01f)
        assertEquals(PanelFit.PANEL_GAP + PanelFit.LIMIT_ROW, PanelFit.noticeHeight(PanelFit.Notice.LIMIT, compact = true, fontScale = 1.3f), 0.01f)
        assertEquals(PanelFit.PANEL_GAP + 2f * PanelFit.LIMIT_TITLE_LINE, PanelFit.noticeHeight(PanelFit.Notice.LIMIT, compact = true, fontScale = 2f), 0.01f)
        assertEquals("nonsense scale counts as 1", normal, PanelFit.noticeHeight(PanelFit.Notice.LINE, compact = true, fontScale = Float.NaN), 0.01f)
    }

    @Test
    fun `the dictation status is two lines, one on a phone on its side`() {
        assertEquals(2, PanelFit.voiceLines(isLandscape = false))
        assertEquals(1, PanelFit.voiceLines(isLandscape = true))
    }
}
