package kz.yerek.aireply

import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.core.lang.KeyboardPlane
import kz.yerek.aireply.keyboard.KeyboardKey
import kz.yerek.aireply.keyboard.layout.KeyboardGeometry
import kz.yerek.aireply.keyboard.layout.KeyboardLayout
import kz.yerek.aireply.keyboard.layout.KeyboardPage
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

    // ------------------------------------------------- pads and passwords

    /** Phones from 320 dp to a small tablet, upright, and phones on their side. */
    private val screens: List<Pair<KeyboardSizing, Float>> =
        listOf(320f, 360f, 411f, 600f).map { KeyboardSizing(it, isLandscape = false, density = 2.625f) to it * 2f } +
            listOf(640f to 360f, 780f to 360f, 915f to 412f).map { (width, height) ->
                KeyboardSizing(width, isLandscape = true, density = 2.625f) to height
            }

    private val pads = listOf(KeyboardPlane.PHONE_PAD, KeyboardPlane.NUMBER_PAD, KeyboardPlane.DATE_PAD)

    /** Every page a pad or a password field can show, every number flag included. */
    private fun fieldPages(language: KeyboardLanguage): List<KeyboardPage> =
        pads.flatMap { plane ->
            listOf(false, true).flatMap { signed ->
                listOf(false, true).map { decimal ->
                    KeyboardLayout.page(language, plane, PageOptions(signed = signed, decimal = decimal))
                }
            }
        } + KeyboardLayout.page(language, KeyboardPlane.LETTERS, PageOptions(numberRow = true))

    private fun area(sizing: KeyboardSizing, screenHeight: Float, languages: List<KeyboardLanguage>) =
        sizing.keyAreaHeight(KeyboardLayout.maximumRowCount(languages), screenHeight)

    private fun assertTiles(layout: PageLayout, label: String) {
        var y = 0.5f
        while (y < layout.height) {
            var x = 0.5f
            while (x < layout.width) {
                assertEquals("$label at ($x, $y)", 1, layout.keys.count { it.hitFrame.contains(x, y) })
                x += 2.3f
            }
            y += 1.9f
        }
    }

    private fun assertNoOverlaps(layout: PageLayout, label: String) {
        layout.keys.forEachIndexed { i, a ->
            assertTrue("$label: ${a.key} is inside its touch area", a.hitFrame.left <= a.frame.left && a.frame.right <= a.hitFrame.right)
            assertTrue("$label: ${a.key} is on the page", a.frame.left >= 0f && a.frame.right <= layout.width && a.frame.bottom <= layout.height)
            layout.keys.drop(i + 1).forEach { b ->
                val overlapX = minOf(a.frame.right, b.frame.right) - maxOf(a.frame.left, b.frame.left)
                val overlapY = minOf(a.frame.bottom, b.frame.bottom) - maxOf(a.frame.top, b.frame.top)
                assertTrue("$label: ${a.key} and ${b.key} overlap", overlapX <= 0.01f || overlapY <= 0.01f)
            }
        }
    }

    @Test
    fun `the pads and a password's number row tile the area with no gaps and no overlaps`() {
        for ((sizing, screenHeight) in screens) for (language in KeyboardLanguage.entries) {
            val height = area(sizing, screenHeight, KeyboardLanguage.CYCLE_ORDER)
            fieldPages(language).forEach { page ->
                val label = "$language ${page.plane} w=${sizing.width} landscape=${sizing.isLandscape}"
                val layout = KeyboardGeometry.layout(page, sizing, height)
                assertTiles(layout, label)
                assertNoOverlaps(layout, label)
            }
        }
    }

    /**
     * The one-height promise, for every set of layouts a user can switch on:
     * every page they can meet - letters, ?123, =\<, the pads, a password's
     * letters where its number row fits - lays out inside the same area.
     */
    @Test
    fun `every page of every layout set fits the same height`() {
        val sets = (1 until (1 shl 3)).map { mask -> KeyboardLanguage.CYCLE_ORDER.filterIndexed { i, _ -> mask and (1 shl i) != 0 } }
        for ((sizing, screenHeight) in screens) for (languages in sets) {
            val height = area(sizing, screenHeight, languages)
            for (language in languages) {
                val pages = KeyboardPlane.entries.map { KeyboardLayout.page(language, it, PageOptions(signed = true, decimal = true)) } +
                    listOfNotNull(
                        KeyboardLayout.page(language, KeyboardPlane.LETTERS, PageOptions(numberRow = true))
                            .takeIf { KeyboardLayout.numberRowFits(language, languages) }
                    )
                pages.forEach { page ->
                    val label = "$languages $language ${page.plane} rows=${page.rows.size} w=${sizing.width}"
                    assertTrue(label, page.rows.size <= KeyboardLayout.maximumRowCount(languages))
                    val layout = KeyboardGeometry.layout(page, sizing, height)
                    assertEquals(label, height, layout.height, 0.001f)
                    assertTrue(label, layout.keys.all { it.frame.top >= sizing.topInset - 0.5f })
                    assertTrue(label, layout.keys.all { it.frame.bottom <= height - sizing.bottomInset + 0.5f })
                    // No page squeezes its keys below the tallest page's.
                    val tallest = KeyboardLayout.maximumRowCount(languages)
                    val floor = KeyboardGeometry.rowMetrics(tallest, height - sizing.topInset - sizing.bottomInset, sizing).key - 0.01f
                    assertTrue("$label key ${layout.rowMetrics.key}", layout.rowMetrics.key >= floor)
                }
            }
        }
    }

    @Test
    fun `pad keys are big, digits wider than the side keys`() {
        for ((sizing, screenHeight) in screens) for (plane in pads) {
            val layout = KeyboardGeometry.layout(
                KeyboardLayout.page(KeyboardLanguage.ENGLISH, plane, PageOptions()), sizing,
                area(sizing, screenHeight, KeyboardLanguage.CYCLE_ORDER)
            )
            val five = layout.keys.first { it.key == KeyboardKey.Character("5") }
            val delete = layout.keys.first { it.key == KeyboardKey.Backspace }
            val label = "$plane w=${sizing.width} landscape=${sizing.isLandscape}"
            assertTrue("$label: ${five.frame.height}", five.frame.height >= if (sizing.isLandscape) 34f else 44f)
            assertTrue("$label: ${five.frame.width}", five.frame.width >= 60f)
            assertTrue(label, five.frame.width > delete.frame.width * 1.2f)
        }
    }

    @Test
    fun `pad columns line up from the top row to the zero row`() {
        val sizing = KeyboardSizing(393f, isLandscape = false, density = 2.625f)
        val height = area(sizing, 850f, KeyboardLanguage.CYCLE_ORDER)
        val tolerance = 1f / sizing.density + 0.01f
        for (plane in pads) for (signed in listOf(false, true)) for (decimal in listOf(false, true)) {
            val layout = KeyboardGeometry.layout(
                KeyboardLayout.page(KeyboardLanguage.RUSSIAN, plane, PageOptions(signed = signed, decimal = decimal)),
                sizing, height
            )
            fun frame(value: String) = layout.keys.first { it.key == KeyboardKey.Character(value) }.frame
            val label = "$plane signed=$signed decimal=$decimal"
            // Every row has the same column edges: 1 over 4 over 7, 3 over 6 over 9.
            assertEquals(label, frame("1").left, frame("7").left, tolerance)
            assertEquals(label, frame("3").right, frame("9").right, tolerance)
            assertEquals(label, frame("2").left, frame("8").left, tolerance)
            // The zero row fills exactly the three digit columns.
            val zeroRow = layout.keys.filter { it.row == 3 && it.key is KeyboardKey.Character && it.frame.left >= frame("7").left - tolerance }
            assertEquals(label, frame("7").left, zeroRow.minOf { it.frame.left }, tolerance)
            assertEquals(label, frame("9").right, zeroRow.maxOf { it.frame.right }, tolerance)
            // Return and delete keep the right column on every row.
            val enter = layout.keys.first { it.key == KeyboardKey.Return }.frame
            val delete = layout.keys.first { it.key == KeyboardKey.Backspace }.frame
            assertEquals(label, enter.left, delete.left, tolerance)
            assertEquals(label, sizing.width - sizing.sideInset, enter.right, tolerance)
        }
    }

    @Test
    fun `a tap where a number field has no point types zero, not return`() {
        val sizing = KeyboardSizing(360f, isLandscape = false, density = 3f)
        val height = area(sizing, 760f, KeyboardLanguage.CYCLE_ORDER)
        val plain = KeyboardGeometry.layout(KeyboardLayout.page(KeyboardLanguage.KAZAKH, KeyboardPlane.NUMBER_PAD, PageOptions()), sizing, height)
        val nine = plain.keys.first { it.key == KeyboardKey.Character("9") }.frame
        val zeroY = plain.keys.first { it.key == KeyboardKey.Character("0") }.frame.centerY
        assertEquals(KeyboardKey.Character("0"), plain.keys[plain.keyIndex(nine.right - 2f, zeroY)!!].key)
        assertEquals(KeyboardKey.Character("0"), plain.keys[plain.keyIndex(nine.left + 2f, zeroY)!!].key)
    }
}
