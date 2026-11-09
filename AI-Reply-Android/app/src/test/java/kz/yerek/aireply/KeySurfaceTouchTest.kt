package kz.yerek.aireply

import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.core.lang.KeyboardPlane
import kz.yerek.aireply.keyboard.KeyboardKey
import kz.yerek.aireply.keyboard.layout.KeyboardGeometry
import kz.yerek.aireply.keyboard.layout.KeyboardLayout
import kz.yerek.aireply.keyboard.layout.KeyboardSizing
import kz.yerek.aireply.keyboard.layout.LaidOutPanel
import kz.yerek.aireply.keyboard.layout.PageLayout
import kz.yerek.aireply.keyboard.layout.PageOptions
import kz.yerek.aireply.keyboard.layout.PanelKind
import kz.yerek.aireply.keyboard.ui.KeySurfaceController
import kz.yerek.aireply.keyboard.ui.KeySurfaceListener
import kz.yerek.aireply.keyboard.ui.LayoutPicker
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Touches on the emoji page: the grid and its bar are the panels' ground, so
 * a finger that goes down on ABC and slides there types nothing - it is not
 * "near" ⌫, whatever the distances say.
 *
 * Эмодзи торына не санаттар жолағына сырғыған саусақ ештеңе термейді.
 */
class KeySurfaceTouchTest {

    private class Recorder : KeySurfaceListener {
        val events = mutableListOf<String>()
        val downs = mutableListOf<KeyboardKey>()
        override fun onKeyDown(key: KeyboardKey) { downs += key }
        override fun onKeyCommit(key: KeyboardKey) { events += "commit $key" }
        override fun onTextCommit(text: String) { events += "text $text" }
        override fun onDelete(word: Boolean) { events += "delete" }
        override fun onMoveCursor(offset: Int) { events += "move $offset" }
        override fun onShowInputMethodPicker() { events += "picker" }
        override fun onLanguagePicked(language: KeyboardLanguage) { events += "language $language" }
    }

    private val screens = listOf(
        KeyboardSizing(320f, isLandscape = false, density = 2f),
        KeyboardSizing(411f, isLandscape = false, density = 2.625f),
        KeyboardSizing(923f, isLandscape = true, density = 2.625f)
    )

    private fun page(language: KeyboardLanguage, plane: KeyboardPlane, sizing: KeyboardSizing): PageLayout {
        val area = sizing.keyAreaHeight(KeyboardLayout.maximumRowCount(KeyboardLanguage.CYCLE_ORDER))
        return KeyboardGeometry.layout(KeyboardLayout.page(language, plane, PageOptions(showsEmojiKey = true)), sizing, area)
    }

    private fun PageLayout.panel(kind: PanelKind): LaidOutPanel = panels.single { it.kind == kind }
    private fun PageLayout.key(key: KeyboardKey) = keys.single { it.key == key }

    private fun controller(layout: PageLayout): Pair<KeySurfaceController, Recorder> {
        val recorder = Recorder()
        return KeySurfaceController(recorder).also { it.layout = layout } to recorder
    }

    @Test
    fun `no point of the grid or the bar is a key`() {
        for (sizing in screens) for (language in KeyboardLanguage.entries) {
            val layout = page(language, KeyboardPlane.EMOJI, sizing)
            for (kind in PanelKind.entries) {
                val hit = layout.panel(kind).hitFrame
                var y = hit.top + 0.5f
                while (y < hit.bottom) {
                    var x = hit.left + 0.5f
                    while (x < hit.right) {
                        val label = "$language w=${sizing.width} $kind at ($x, $y)"
                        assertNull(label, layout.keyIndex(x, y))
                        assertEquals(label, kind, layout.panelAt(x, y)?.kind)
                        x += 7.3f
                    }
                    y += 5.1f
                }
            }
        }
    }

    @Test
    fun `the keys around the panels still answer, and slid-off edges find the nearest key`() {
        val layout = page(KeyboardLanguage.RUSSIAN, KeyboardPlane.EMOJI, screens[1])
        val abc = layout.key(KeyboardKey.Plane(KeyboardPlane.LETTERS))
        val delete = layout.key(KeyboardKey.Backspace)
        assertEquals(abc.key, layout.keys[layout.keyIndex(abc.frame.centerX, abc.frame.centerY)!!].key)
        assertEquals(delete.key, layout.keys[layout.keyIndex(delete.frame.centerX, delete.frame.centerY)!!].key)
        assertNull(layout.panelAt(delete.frame.centerX, delete.frame.centerY))
        // Below the page, under the bar: the nearest key, as on every page.
        assertTrue(layout.keyIndex(layout.width / 2f, layout.height + 10f) != null)
    }

    @Test
    fun `a slide from ABC into the categories bar types nothing`() {
        for (sizing in screens) for (language in KeyboardLanguage.entries) {
            val layout = page(language, KeyboardPlane.EMOJI, sizing)
            val abc = layout.key(KeyboardKey.Plane(KeyboardPlane.LETTERS)).frame
            val bar = layout.panel(PanelKind.EMOJI_CATEGORIES).frame
            val (surface, recorder) = controller(layout)
            surface.down(1, abc.centerX, abc.centerY, time = 0)
            // The right half of the bar is nearer ⌫ than anything else.
            surface.move(1, bar.left + bar.width * 0.8f, bar.centerY, time = 120)
            assertTrue("the key it left lets go", surface.visual.pressed.isEmpty())
            surface.up(1, bar.left + bar.width * 0.8f, bar.centerY, time = 300)
            assertEquals("$language w=${sizing.width}", emptyList<String>(), recorder.events)
        }
    }

    @Test
    fun `a slide from ABC up into the grid types nothing, held or not`() {
        val layout = page(KeyboardLanguage.KAZAKH, KeyboardPlane.EMOJI, screens[1])
        val abc = layout.key(KeyboardKey.Plane(KeyboardPlane.LETTERS)).frame
        val grid = layout.panel(PanelKind.EMOJI_GRID).frame
        val (surface, recorder) = controller(layout)
        surface.down(1, abc.centerX, abc.centerY, time = 0)
        surface.move(1, grid.right - 10f, grid.bottom - 20f, time = 100)
        // Resting there past a long press changes nothing.
        surface.tick(time = 2_000)
        surface.up(1, grid.right - 10f, grid.bottom - 20f, time = 2_100)
        assertEquals(emptyList<String>(), recorder.events)
    }

    @Test
    fun `lifted in a panel without a move between, nothing is typed either`() {
        val layout = page(KeyboardLanguage.ENGLISH, KeyboardPlane.EMOJI, screens[0])
        val abc = layout.key(KeyboardKey.Plane(KeyboardPlane.LETTERS)).frame
        val bar = layout.panel(PanelKind.EMOJI_CATEGORIES).frame
        val (surface, recorder) = controller(layout)
        surface.down(1, abc.centerX, abc.centerY, time = 0)
        surface.up(1, bar.right - 4f, bar.centerY, time = 80)
        assertEquals(emptyList<String>(), recorder.events)
    }

    @Test
    fun `back out of the panel onto ABC, ABC is typed`() {
        val layout = page(KeyboardLanguage.RUSSIAN, KeyboardPlane.EMOJI, screens[1])
        val abc = layout.key(KeyboardKey.Plane(KeyboardPlane.LETTERS)).frame
        val bar = layout.panel(PanelKind.EMOJI_CATEGORIES).frame
        val (surface, recorder) = controller(layout)
        surface.down(1, abc.centerX, abc.centerY, time = 0)
        surface.move(1, bar.centerX, bar.centerY, time = 100)
        surface.move(1, abc.centerX + 2f, abc.centerY, time = 200)
        surface.up(1, abc.centerX + 2f, abc.centerY, time = 260)
        assertEquals(listOf("commit ${KeyboardKey.Plane(KeyboardPlane.LETTERS)}"), recorder.events)
    }

    @Test
    fun `taps on ABC and delete work as before`() {
        val layout = page(KeyboardLanguage.RUSSIAN, KeyboardPlane.EMOJI, screens[1])
        val abc = layout.key(KeyboardKey.Plane(KeyboardPlane.LETTERS)).frame
        val delete = layout.key(KeyboardKey.Backspace).frame
        val (surface, recorder) = controller(layout)
        surface.down(1, delete.centerX, delete.centerY, time = 0)
        surface.up(1, delete.centerX, delete.centerY, time = 60)
        surface.down(2, abc.centerX, abc.centerY, time = 100)
        surface.up(2, abc.centerX, abc.centerY, time = 160)
        assertEquals(listOf("delete", "commit ${KeyboardKey.Plane(KeyboardPlane.LETTERS)}"), recorder.events)
    }

    @Test
    fun `slide to correct on the letters is unchanged`() {
        val layout = page(KeyboardLanguage.ENGLISH, KeyboardPlane.LETTERS, screens[1])
        assertTrue(layout.panels.isEmpty())
        val q = layout.key(KeyboardKey.Character("q")).frame
        val w = layout.key(KeyboardKey.Character("w")).frame
        val (surface, recorder) = controller(layout)
        surface.down(1, q.centerX, q.centerY, time = 0)
        surface.move(1, w.centerX, w.centerY, time = 80)
        surface.up(1, w.centerX, w.centerY, time = 120)
        // Off the bottom edge still types the nearest key.
        val space = layout.key(KeyboardKey.Space).frame
        surface.down(2, space.centerX, space.centerY, time = 200)
        surface.up(2, space.centerX, layout.height + 12f, time = 260)
        assertEquals(listOf("commit ${KeyboardKey.Character("w")}", "commit ${KeyboardKey.Space}"), recorder.events)
    }

    // --------------------------------------------------------------- dimmed

    @Test
    fun `while dimmed a key that edits text answers nothing`() {
        val layout = page(KeyboardLanguage.RUSSIAN, KeyboardPlane.LETTERS, screens[1])
        val (surface, recorder) = controller(layout)
        surface.isDimmed = true
        for (key in listOf(KeyboardKey.Character("е"), KeyboardKey.Space, KeyboardKey.Backspace, KeyboardKey.Return)) {
            val frame = layout.key(key).frame
            surface.down(1, frame.centerX, frame.centerY, time = 0)
            assertTrue("$key shows no press", surface.visual.pressed.isEmpty())
            surface.tick(time = 2_000)
            assertNull("$key opens nothing", surface.visual.panel)
            surface.move(1, frame.centerX + 40f, frame.centerY, time = 2_100)
            assertFalse("$key is no trackpad", surface.visual.trackpad)
            surface.up(1, frame.centerX + 40f, frame.centerY, time = 2_200)
        }
        assertEquals("no click, no vibration", emptyList<KeyboardKey>(), recorder.downs)
        assertEquals(emptyList<String>(), recorder.events)
    }

    @Test
    fun `while dimmed the keys that change the keyboard still press and work`() {
        val layout = page(KeyboardLanguage.RUSSIAN, KeyboardPlane.LETTERS, screens[1])
        val (surface, recorder) = controller(layout)
        surface.isDimmed = true
        val numbers = KeyboardKey.Plane(KeyboardPlane.NUMBERS)
        val frame = layout.key(numbers).frame
        surface.down(1, frame.centerX, frame.centerY, time = 0)
        assertEquals(listOf<KeyboardKey>(numbers), recorder.downs)
        assertEquals(setOf(layout.keys.indexOfFirst { it.key == numbers }), surface.visual.pressed)
        surface.up(1, frame.centerX, frame.centerY, time = 60)
        assertEquals(listOf("commit $numbers"), recorder.events)
    }

    @Test
    fun `a finger sliding from a key that works onto a dimmed one wakes nothing`() {
        val layout = page(KeyboardLanguage.RUSSIAN, KeyboardPlane.LETTERS, screens[1])
        val numbers = layout.key(KeyboardKey.Plane(KeyboardPlane.NUMBERS)).frame
        val letter = layout.key(KeyboardKey.Character("е")).frame
        val space = layout.key(KeyboardKey.Space).frame
        val (surface, recorder) = controller(layout)
        surface.isDimmed = true

        surface.down(1, numbers.centerX, numbers.centerY, time = 0)
        surface.move(1, letter.centerX, letter.centerY, time = 100)
        assertTrue("the key it left lets go", surface.visual.pressed.isEmpty())
        surface.tick(time = 2_000)
        assertNull("no alternates", surface.visual.panel)
        surface.up(1, letter.centerX, letter.centerY, time = 2_100)

        surface.down(2, numbers.centerX, numbers.centerY, time = 3_000)
        surface.move(2, space.centerX, space.centerY, time = 3_100)
        surface.move(2, space.centerX + 40f, space.centerY, time = 3_200)
        assertFalse("no trackpad, no blank captions", surface.visual.trackpad)
        surface.up(2, space.centerX + 40f, space.centerY, time = 3_300)
        assertEquals(emptyList<String>(), recorder.events)

        surface.down(3, numbers.centerX, numbers.centerY, time = 4_000)
        surface.move(3, letter.centerX, letter.centerY, time = 4_100)
        surface.move(3, numbers.centerX, numbers.centerY, time = 4_200)
        surface.up(3, numbers.centerX, numbers.centerY, time = 4_300)
        assertEquals("back on its own key, it works", listOf("commit ${KeyboardKey.Plane(KeyboardPlane.NUMBERS)}"), recorder.events)
    }

    @Test
    fun `a letter resting when the keys dimmed is still typed by the next finger`() {
        val layout = page(KeyboardLanguage.ENGLISH, KeyboardPlane.LETTERS, screens[1])
        val q = layout.key(KeyboardKey.Character("q")).frame
        val w = layout.key(KeyboardKey.Character("w")).frame
        val (surface, recorder) = controller(layout)
        surface.down(1, q.centerX, q.centerY, time = 0)
        surface.isDimmed = true
        surface.down(2, w.centerX, w.centerY, time = 40)
        assertEquals(listOf("commit ${KeyboardKey.Character("q")}"), recorder.events)
        assertTrue("neither finger holds a key now", surface.visual.pressed.isEmpty())
    }

    @Test
    fun `while dimmed TalkBack gets nothing from a key that edits text`() {
        val (surface, _) = controller(page(KeyboardLanguage.ENGLISH, KeyboardPlane.NUMBERS, screens[1]))
        surface.isDimmed = true
        val activated = mutableListOf<KeyboardKey>()
        for (key in listOf(KeyboardKey.Character("'"), KeyboardKey.Space, KeyboardKey.Backspace, KeyboardKey.Return)) {
            assertFalse("$key says it did nothing", surface.activate(key) { activated += it })
        }
        assertEquals(emptyList<KeyboardKey>(), activated)
    }

    @Test
    fun `while dimmed TalkBack still works the keys that change the keyboard`() {
        val (surface, _) = controller(page(KeyboardLanguage.ENGLISH, KeyboardPlane.NUMBERS, screens[1]))
        surface.isDimmed = true
        val activated = mutableListOf<KeyboardKey>()
        val keys = listOf(
            KeyboardKey.Plane(KeyboardPlane.LETTERS), KeyboardKey.Shift, KeyboardKey.Layout, KeyboardKey.Globe
        )
        for (key in keys) assertTrue("$key", surface.activate(key) { activated += it })
        assertEquals(keys, activated)
    }

    @Test
    fun `not dimmed TalkBack types as before`() {
        val (surface, _) = controller(page(KeyboardLanguage.ENGLISH, KeyboardPlane.NUMBERS, screens[1]))
        val activated = mutableListOf<KeyboardKey>()
        assertTrue(surface.activate(KeyboardKey.Character("'")) { activated += it })
        assertTrue(surface.activate(KeyboardKey.Backspace) { activated += it })
        assertEquals(listOf(KeyboardKey.Character("'"), KeyboardKey.Backspace), activated)
    }

    // ----------------------------------------- the page changes under a finger

    private val apostrophe = KeyboardKey.Character("'")
    private val numbers = page(KeyboardLanguage.ENGLISH, KeyboardPlane.NUMBERS, screens[1])
    private val letters = page(KeyboardLanguage.ENGLISH, KeyboardPlane.LETTERS, screens[1])

    private fun PageLayout.indexOf(key: KeyboardKey) = keys.indexOfFirst { it.key == key }

    @Test
    fun `a letter down while the apostrophe is held is typed from the letters`() {
        val quote = numbers.key(apostrophe).frame
        val t = KeyboardKey.Character("t")
        val at = letters.key(t).frame
        val (surface, recorder) = controller(numbers)
        surface.down(1, quote.centerX, quote.centerY, time = 0)
        surface.down(2, at.centerX, at.centerY, time = 40)
        assertEquals(listOf("commit $apostrophe"), recorder.events)

        // The apostrophe went back to the letters; the next frame brings them.
        surface.layout = letters
        assertEquals("held on its new key", setOf(letters.indexOf(t)), surface.visual.pressed)
        assertEquals(letters.indexOf(t), surface.visual.preview)

        surface.up(1, quote.centerX, quote.centerY, time = 70)
        surface.up(2, at.centerX, at.centerY, time = 100)
        assertEquals(listOf("commit $apostrophe", "commit $t"), recorder.events)
        assertEquals("one click each", listOf(apostrophe, KeyboardKey.Character("5")), recorder.downs)
    }

    @Test
    fun `a letter down while Space goes back to the letters is typed from the letters`() {
        val space = numbers.key(KeyboardKey.Space).frame
        val q = KeyboardKey.Character("q")
        val at = letters.key(q).frame
        val (surface, recorder) = controller(numbers)
        surface.down(1, space.centerX, space.centerY, time = 0)
        surface.down(2, at.centerX, at.centerY, time = 40)
        surface.up(1, space.centerX, space.centerY, time = 70)
        surface.layout = letters
        surface.up(2, at.centerX, at.centerY, time = 100)
        assertEquals(listOf("commit ${KeyboardKey.Space}", "commit $q"), recorder.events)
    }

    @Test
    fun `Space down while the apostrophe is held is still Space`() {
        val quote = numbers.key(apostrophe).frame
        val space = letters.key(KeyboardKey.Space).frame
        val (surface, recorder) = controller(numbers)
        surface.down(1, quote.centerX, quote.centerY, time = 0)
        surface.down(2, space.centerX, space.centerY, time = 40)
        surface.layout = letters
        surface.up(2, space.centerX, space.centerY, time = 100)
        assertEquals(listOf("commit $apostrophe", "commit ${KeyboardKey.Space}"), recorder.events)
    }

    @Test
    fun `a finger that slid before the page came types from where it is now`() {
        val quote = numbers.key(apostrophe).frame
        val t = letters.key(KeyboardKey.Character("t")).frame
        val y = KeyboardKey.Character("y")
        val slidTo = letters.key(y).frame
        val (surface, recorder) = controller(numbers)
        surface.down(1, quote.centerX, quote.centerY, time = 0)
        surface.down(2, t.centerX, t.centerY, time = 40)
        surface.move(2, slidTo.centerX, slidTo.centerY, time = 60)
        surface.layout = letters
        surface.up(2, slidTo.centerX, slidTo.centerY, time = 100)
        assertEquals(listOf("commit $apostrophe", "commit $y"), recorder.events)
    }

    @Test
    fun `a control does not turn into the one that takes its place`() {
        val quote = numbers.key(apostrophe).frame
        val abc = numbers.key(KeyboardKey.Plane(KeyboardPlane.LETTERS)).frame
        val space = numbers.key(KeyboardKey.Space).frame
        val spaceLeft = space.left + 2f
        assertEquals(numbers.indexOf(KeyboardKey.Space), numbers.keyIndex(spaceLeft, space.centerY))
        assertEquals("the letters' language key is there", letters.indexOf(KeyboardKey.Layout), letters.keyIndex(spaceLeft, space.centerY))

        for ((x, y) in listOf(abc.centerX to abc.centerY, spaceLeft to space.centerY)) {
            val (surface, recorder) = controller(numbers)
            surface.down(1, quote.centerX, quote.centerY, time = 0)
            surface.down(2, x, y, time = 40)
            surface.layout = letters
            assertTrue("nothing held at ($x, $y)", surface.visual.pressed.isEmpty())
            surface.up(2, x, y, time = 100)
            assertEquals(listOf("commit $apostrophe"), recorder.events)
        }
    }

    @Test
    fun `a page whose panel comes under the finger types nothing`() {
        val emoji = page(KeyboardLanguage.ENGLISH, KeyboardPlane.EMOJI, screens[1])
        val emojiKey = KeyboardKey.Plane(KeyboardPlane.EMOJI)
        val smile = letters.key(emojiKey).frame
        val e = letters.key(KeyboardKey.Character("e")).frame
        val (surface, recorder) = controller(letters)
        surface.down(1, smile.centerX, smile.centerY, time = 0)
        surface.down(2, e.centerX, e.centerY, time = 40)
        surface.up(1, smile.centerX, smile.centerY, time = 70)
        surface.layout = emoji
        assertTrue(surface.visual.pressed.isEmpty())
        surface.up(2, e.centerX, e.centerY, time = 100)
        assertEquals(listOf("commit $emojiKey"), recorder.events)
    }

    @Test
    fun `a finger already over a panel does not come back to a key with the new page`() {
        val emoji = page(KeyboardLanguage.ENGLISH, KeyboardPlane.EMOJI, screens[1])
        val abc = emoji.key(KeyboardKey.Plane(KeyboardPlane.LETTERS)).frame
        val grid = emoji.panel(PanelKind.EMOJI_GRID).frame
        val (surface, recorder) = controller(emoji)
        surface.down(1, abc.centerX, abc.centerY, time = 0)
        surface.move(1, grid.centerX, grid.centerY, time = 60)
        surface.layout = letters
        surface.up(1, grid.centerX, grid.centerY, time = 100)
        assertEquals(emptyList<String>(), recorder.events)
    }

    @Test
    fun `a page that comes while the keys are dimmed wakes nothing under a resting finger`() {
        val e = letters.key(KeyboardKey.Character("e")).frame
        val (surface, recorder) = controller(letters)
        surface.down(1, e.centerX, e.centerY, time = 0)
        surface.isDimmed = true
        surface.layout = numbers
        assertTrue(surface.visual.pressed.isEmpty())
        surface.up(1, e.centerX, e.centerY, time = 100)
        assertEquals(emptyList<String>(), recorder.events)
    }

    @Test
    fun `a held delete and open alternates still end with the page`() {
        val delete = letters.key(KeyboardKey.Backspace).frame
        val e = letters.key(KeyboardKey.Character("e")).frame
        val (surface, recorder) = controller(letters)
        surface.down(1, delete.centerX, delete.centerY, time = 0)
        surface.down(2, e.centerX, e.centerY, time = 10)
        surface.tick(time = 10 + KeySurfaceController.LONG_PRESS_MS)
        assertTrue(surface.visual.panel != null)

        surface.layout = numbers
        assertNull(surface.visual.panel)
        assertTrue(surface.visual.pressed.isEmpty())
        surface.tick(time = 2_000)
        surface.up(1, delete.centerX, delete.centerY, time = 2_100)
        surface.up(2, e.centerX, e.centerY, time = 2_100)
        assertEquals(listOf("delete"), recorder.events)
    }

    // --------------------------------------------------------- layout picker

    private fun hold(surface: KeySurfaceController, layout: PageLayout) {
        val key = layout.key(KeyboardKey.Layout).frame
        surface.down(1, key.centerX, key.centerY, time = 0)
        surface.tick(time = KeySurfaceController.LONG_PRESS_MS)
    }

    private val KeyboardLanguage.row get() = KeyboardLanguage.CYCLE_ORDER.indexOf(this)

    @Test
    fun `a held language key lists the layouts by name, on the one in use`() {
        for (sizing in screens) for (language in KeyboardLanguage.entries) {
            val layout = page(language, KeyboardPlane.LETTERS, sizing)
            val (surface, _) = controller(layout)
            hold(surface, layout)
            val picker = surface.visual.picker!!
            assertNull("no row of badges", surface.visual.panel)
            assertEquals(KeyboardLanguage.CYCLE_ORDER.map { it.nativeName }, picker.options)
            assertEquals("$language w=${sizing.width}", language.row, picker.selected)
            assertTrue("above its key", picker.bottom <= layout.key(KeyboardKey.Layout).frame.top)
            assertTrue("on the page", picker.left >= 1f && picker.left + LayoutPicker.WIDTH <= layout.width - 1f)
        }
    }

    @Test
    fun `a long press with no slide keeps the layout, however the finger trembles`() {
        for (language in KeyboardLanguage.entries) {
            val layout = page(language, KeyboardPlane.LETTERS, screens[1])
            val key = layout.key(KeyboardKey.Layout).frame
            val (surface, recorder) = controller(layout)
            hold(surface, layout)
            surface.move(1, key.centerX + 4f, key.centerY - 3f, time = 500)
            surface.move(1, key.centerX - 6f, key.top + 1f, time = 520)
            surface.move(1, key.right + 30f, key.bottom - 1f, time = 540)
            assertEquals(language.row, surface.visual.picker!!.selected)
            surface.up(1, key.right + 30f, key.bottom - 1f, time = 560)
            assertEquals(listOf("language $language"), recorder.events)
        }
    }

    @Test
    fun `sliding up the column picks the row under the finger, and lifting takes it`() {
        val layout = page(KeyboardLanguage.ENGLISH, KeyboardPlane.LETTERS, screens[1])
        val (surface, recorder) = controller(layout)
        hold(surface, layout)
        val opened = surface.visual.picker!!
        val rowY = { row: Int -> opened.top + LayoutPicker.PADDING + (row + 0.5f) * LayoutPicker.ROW_HEIGHT }
        val x = opened.left + 20f
        surface.move(1, x, rowY(KeyboardLanguage.RUSSIAN.row), time = 500)
        assertEquals(KeyboardLanguage.RUSSIAN.row, surface.visual.picker!!.selected)
        surface.move(1, x, rowY(KeyboardLanguage.KAZAKH.row), time = 520)
        assertEquals(KeyboardLanguage.KAZAKH.row, surface.visual.picker!!.selected)
        surface.move(1, x, opened.top - 30f, time = 540)
        assertEquals("above the column: its top row", 0, surface.visual.picker!!.selected)
        surface.up(1, x, rowY(KeyboardLanguage.KAZAKH.row), time = 560)
        assertEquals(listOf("language ${KeyboardLanguage.KAZAKH}"), recorder.events)
        assertNull(surface.visual.picker)
    }

    @Test
    fun `sideways does not matter in the column, and a tap still takes the next layout`() {
        val layout = page(KeyboardLanguage.RUSSIAN, KeyboardPlane.LETTERS, screens[1])
        val (surface, recorder) = controller(layout)
        hold(surface, layout)
        val opened = surface.visual.picker!!
        val y = opened.top + LayoutPicker.PADDING + LayoutPicker.ROW_HEIGHT * 2.5f
        surface.move(1, layout.width - 5f, y, time = 500)
        assertEquals(2, surface.visual.picker!!.selected)
        surface.up(1, layout.width - 5f, y, time = 520)
        assertEquals(listOf("language ${KeyboardLanguage.ENGLISH}"), recorder.events)

        recorder.events.clear()
        val key = layout.key(KeyboardKey.Layout).frame
        surface.down(2, key.centerX, key.centerY, time = 1_000)
        surface.up(2, key.centerX, key.centerY, time = 1_060)
        assertEquals(listOf("commit ${KeyboardKey.Layout}"), recorder.events)
    }
}
