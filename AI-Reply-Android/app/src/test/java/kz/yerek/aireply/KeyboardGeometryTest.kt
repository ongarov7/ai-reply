package kz.yerek.aireply

import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.core.lang.KeyboardPlane
import kz.yerek.aireply.keyboard.KeyboardKey
import kz.yerek.aireply.keyboard.layout.KeyboardGeometry
import kz.yerek.aireply.keyboard.layout.KeyboardLayout
import kz.yerek.aireply.keyboard.layout.KeyboardSizing
import kz.yerek.aireply.keyboard.layout.PageLayout
import kz.yerek.aireply.keyboard.layout.PageOptions
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** Key frames and hit boxes: no dead zones, one height, sensible sizes. */
class KeyboardGeometryTest {

    private val widths = listOf(320f, 360f, 393f, 411f, 480f)

    private fun layout(language: KeyboardLanguage, plane: KeyboardPlane, width: Float): PageLayout {
        val sizing = KeyboardSizing(width, isLandscape = false, density = 2.625f)
        val area = sizing.keyAreaHeight(KeyboardLayout.maximumRowCount(KeyboardLanguage.CYCLE_ORDER))
        return KeyboardGeometry.layout(KeyboardLayout.page(language, plane, PageOptions()), sizing, area)
    }

    /** Every point of the key area belongs to exactly one key. */
    @Test
    fun `hit boxes tile the whole area with no gaps and no overlaps`() {
        for (width in widths) for (language in KeyboardLanguage.entries) for (plane in KeyboardPlane.entries) {
            val page = layout(language, plane, width)
            var y = 0.5f
            while (y < page.height) {
                var x = 0.5f
                while (x < page.width) {
                    val owners = page.keys.count { it.hitFrame.contains(x, y) }
                    assertEquals("$language $plane w=$width at ($x, $y)", 1, owners)
                    x += 1.7f
                }
                y += 1.3f
            }
        }
    }

    @Test
    fun `a tap between two keys types the nearer one`() {
        val page = layout(KeyboardLanguage.RUSSIAN, KeyboardPlane.LETTERS, 393f)
        val row = page.keys.filter { it.row == 1 }
        val left = row[3]
        val right = row[4]
        val gapMiddle = (left.frame.right + right.frame.left) / 2f
        val y = left.frame.centerY
        assertEquals(left.key, page.keys[page.keyIndex(gapMiddle - 0.6f, y)!!].key)
        assertEquals(right.key, page.keys[page.keyIndex(gapMiddle + 0.6f, y)!!].key)
    }

    @Test
    fun `every page of every layout has the same height`() {
        val heights = KeyboardLanguage.entries.flatMap { language ->
            KeyboardPlane.entries.map { plane -> layout(language, plane, 393f).height }
        }.toSet()
        assertEquals(1, heights.size)
    }

    @Test
    fun `keys are comfortably large`() {
        for (width in widths) {
            val kazakh = layout(KeyboardLanguage.KAZAKH, KeyboardPlane.LETTERS, width)
            val letter = kazakh.keys.first { it.key == KeyboardKey.Character("й") }
            assertTrue("height at $width: ${letter.frame.height}", letter.frame.height >= 37f)
            assertTrue("width at $width: ${letter.frame.width}", letter.frame.width >= 24f)
            // Four-row pages get the spare height as taller keys, not a gap.
            val english = layout(KeyboardLanguage.ENGLISH, KeyboardPlane.LETTERS, width)
            val q = english.keys.first { it.key == KeyboardKey.Character("q") }
            assertTrue(q.frame.height > letter.frame.height)
        }
    }

    @Test
    fun `the kazakh top row is centred with keys the size of the ones below`() {
        val page = layout(KeyboardLanguage.KAZAKH, KeyboardPlane.LETTERS, 393f)
        val top = page.keys.filter { it.row == 0 }
        val below = page.keys.first { it.row == 1 }
        assertEquals(9, top.size)
        assertEquals(below.frame.width, top.first().frame.width, 1f)
        val leftMargin = top.first().frame.left
        val rightMargin = page.width - top.last().frame.right
        assertEquals(leftMargin, rightMargin, 1.5f)
    }

    @Test
    fun `a touch that slides off the page still finds a key`() {
        val page = layout(KeyboardLanguage.ENGLISH, KeyboardPlane.LETTERS, 393f)
        assertNotNull(page.keyIndex(-20f, 10f))
        assertNotNull(page.keyIndex(10f, page.height + 30f))
    }
}
