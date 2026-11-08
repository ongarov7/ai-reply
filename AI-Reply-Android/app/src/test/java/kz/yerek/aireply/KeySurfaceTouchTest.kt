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
}
