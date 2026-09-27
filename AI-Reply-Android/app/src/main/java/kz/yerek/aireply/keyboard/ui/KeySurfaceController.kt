package kz.yerek.aireply.keyboard.ui

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.keyboard.KeyboardKey
import kz.yerek.aireply.keyboard.layout.LaidOutKey
import kz.yerek.aireply.keyboard.layout.PageLayout
import kotlin.math.abs
import kotlin.math.floor

/** What the keys do. The service implements it; the surface only reports. */
interface KeySurfaceListener {
    /** A finger landed on a key: click and vibrate, nothing else. */
    fun onKeyDown(key: KeyboardKey)

    /** A key was typed or tapped: characters, space, return, shift, planes, layout, globe. */
    fun onKeyCommit(key: KeyboardKey)

    /** A long-press alternate was chosen: typed exactly as shown. */
    fun onTextCommit(text: String)

    /** Delete, once or while held; [word] once it has been held a while. */
    fun onDelete(word: Boolean)

    /** Space-bar trackpad: move the caret by characters. */
    fun onMoveCursor(offset: Int)

    /** Held globe or held space: the system keyboard picker. */
    fun onShowInputMethodPicker()

    /** Held language key, then a layout picked from the row. */
    fun onLanguagePicked(language: KeyboardLanguage)
}

/**
 * Touch handling for the whole key area, independent of Compose.
 *
 * ONE SURFACE, NO DEAD ZONES. Every touch is resolved against the page's hit
 * boxes, which tile the keyboard completely: a finger in a gap between keys,
 * in a row gap or on the very edge still types the nearest key.
 *
 * The behaviour is Gboard's:
 *  * characters commit on RELEASE, so sliding to the neighbouring key before
 *    lifting corrects a miss;
 *  * a second finger landing commits the first one at once (rollover), so
 *    two-thumb typing never drops or reorders letters;
 *  * delete fires on press, repeats while held, and switches to whole words;
 *  * holding a key with alternates opens them - slide along, lift to type;
 *  * dragging along space moves the caret; holding space still opens the
 *    keyboard picker;
 *  * holding the language key lists the layouts that are on.
 *
 * Coordinates are dp, in the page's own frame. Times come from the caller,
 * so the logic is deterministic.
 */
class KeySurfaceController(private val listener: KeySurfaceListener) {

    var layout: PageLayout? = null
        set(value) {
            if (field != value) {
                // A new page (shift does not make one) ends every touch on the old one.
                tracks.clear()
                visual = TouchVisual()
            }
            field = value
        }

    /** Languages the picker lists, in cycle order. */
    var languages: List<KeyboardLanguage> = KeyboardLanguage.CYCLE_ORDER

    /** Uppercase alternates while shift is on. */
    var isShifted: Boolean = false

    /** True while a request runs: touches still click, but nothing is picked up. */
    var isDimmed: Boolean = false

    /** Everything the surface draws that a touch changes. Read in the draw phase only. */
    var visual by mutableStateOf(TouchVisual())
        private set

    private enum class Mode { NORMAL, ALTERNATES, TRACKPAD, DELETE, DONE }

    private class Track(
        val id: Long,
        var keyIndex: Int,
        val downX: Float,
        val downY: Float,
        val downTime: Long,
        var mode: Mode = Mode.NORMAL,
        var lastX: Float = downX,
        var trackpadAnchor: Float = downX,
        var repeats: Int = 0,
        var nextRepeatAt: Long = Long.MAX_VALUE,
        var longPressAt: Long = Long.MAX_VALUE,
        var alternates: List<String> = emptyList(),
        var panel: AlternatesPanel? = null
    )

    private val tracks = LinkedHashMap<Long, Track>()

    // ---------------------------------------------------------------- events

    fun down(id: Long, x: Float, y: Float, time: Long) {
        val page = layout ?: return
        val index = page.keyIndex(x, y) ?: return

        // Rollover: a finger still resting on a letter is typed now, before
        // the new one, so fast two-thumb typing keeps its order.
        tracks.values.filter { it.mode == Mode.NORMAL && page.keys.getOrNull(it.keyIndex)?.key?.isCharacter == true }
            .forEach { commit(it) }

        val key = page.keys[index]
        val track = Track(id, index, x, y, time)
        tracks[id] = track
        listener.onKeyDown(key.key)

        when (key.key) {
            KeyboardKey.Backspace -> {
                track.mode = Mode.DELETE
                listener.onDelete(word = false)
                track.nextRepeatAt = time + DELETE_FIRST_REPEAT_MS
            }
            KeyboardKey.Shift -> {
                // Shift answers on press: a double tap is timed from the
                // presses, and holding it while typing a letter works.
                listener.onKeyCommit(KeyboardKey.Shift)
                track.mode = Mode.DONE
            }
            else -> {
                track.longPressAt = time + when (key.key) {
                    KeyboardKey.Space -> SPACE_HOLD_MS
                    else -> LONG_PRESS_MS
                }
            }
        }
        refreshVisual()
    }

    fun move(id: Long, x: Float, y: Float, time: Long) {
        val track = tracks[id] ?: return
        val page = layout ?: return
        track.lastX = x

        when (track.mode) {
            Mode.ALTERNATES -> {
                track.panel = track.panel?.selecting(x)
                refreshVisual()
            }
            Mode.TRACKPAD -> {
                val step = TRACKPAD_STEP_DP
                val moved = ((x - track.trackpadAnchor) / step).toInt()
                if (moved != 0) {
                    track.trackpadAnchor += moved * step
                    listener.onMoveCursor(moved)
                }
            }
            Mode.NORMAL -> {
                val current = page.keys.getOrNull(track.keyIndex)?.key
                if (current == KeyboardKey.Space && abs(x - track.downX) > TRACKPAD_START_DP) {
                    track.mode = Mode.TRACKPAD
                    track.trackpadAnchor = x
                    track.longPressAt = Long.MAX_VALUE
                    refreshVisual()
                    return
                }
                // Slide to correct: the key under the finger is the one that
                // will be typed. Sliding does not restart the long press.
                val index = page.keyIndex(x, y) ?: return
                if (index != track.keyIndex) {
                    track.keyIndex = index
                    track.longPressAt = time + LONG_PRESS_MS
                    refreshVisual()
                }
            }
            Mode.DELETE, Mode.DONE -> Unit
        }
    }

    fun up(id: Long, x: Float, y: Float, time: Long) {
        val track = tracks.remove(id) ?: return
        when (track.mode) {
            Mode.NORMAL -> {
                // Slid well off the top into the reply panel: a change of
                // mind, not a keystroke.
                if (y > -CANCEL_ABOVE_DP) commit(track)
            }
            Mode.ALTERNATES -> track.panel?.selectedText?.let { text ->
                val key = layout?.keys?.getOrNull(track.keyIndex)?.key
                if (key == KeyboardKey.Layout) {
                    languages.getOrNull(track.panel?.selected ?: -1)?.let(listener::onLanguagePicked)
                } else {
                    listener.onTextCommit(text)
                }
            }
            Mode.TRACKPAD, Mode.DELETE, Mode.DONE -> Unit
        }
        refreshVisual()
    }

    /** The system took the touch away (a gesture, a dialog): nothing is typed. */
    fun cancel(id: Long) {
        tracks.remove(id)
        refreshVisual()
    }

    fun cancelAll() {
        tracks.clear()
        refreshVisual()
    }

    /** The earliest time something happens without a touch event, or null. */
    fun nextDeadline(): Long? =
        tracks.values.flatMap { listOf(it.longPressAt, it.nextRepeatAt) }.filter { it != Long.MAX_VALUE }.minOrNull()

    /** Long presses and delete repeats that are due by [time]. */
    fun tick(time: Long) {
        val page = layout ?: return
        var changed = false
        for (track in tracks.values.toList()) {
            if (track.mode == Mode.DELETE && time >= track.nextRepeatAt) {
                val word = track.repeats >= DELETE_WORD_AFTER
                listener.onDelete(word)
                track.repeats++
                track.nextRepeatAt = time + if (word) DELETE_WORD_INTERVAL_MS else DELETE_INTERVAL_MS
            }
            if (track.mode == Mode.NORMAL && time >= track.longPressAt) {
                track.longPressAt = Long.MAX_VALUE
                val key = page.keys.getOrNull(track.keyIndex) ?: continue
                changed = true
                when (key.key) {
                    KeyboardKey.Space, KeyboardKey.Globe -> {
                        track.mode = Mode.DONE
                        listener.onShowInputMethodPicker()
                    }
                    KeyboardKey.Layout -> if (languages.size > 1) {
                        openPanel(track, key, languages.map { it.badge })
                    }
                    else -> {
                        val options = displayedAlternates(key)
                        if (options.isNotEmpty()) openPanel(track, key, options)
                    }
                }
            }
        }
        if (changed) refreshVisual()
    }

    // --------------------------------------------------------------- helpers

    private fun commit(track: Track) {
        track.mode = Mode.DONE
        val key = layout?.keys?.getOrNull(track.keyIndex)?.key ?: return
        listener.onKeyCommit(key)
    }

    private fun openPanel(track: Track, key: LaidOutKey, options: List<String>) {
        val page = layout ?: return
        track.mode = Mode.ALTERNATES
        track.alternates = options
        track.panel = AlternatesPanel.anchored(track.keyIndex, key, options, page.width).selecting(track.lastX)
    }

    private fun displayedAlternates(key: LaidOutKey): List<String> {
        val character = (key.key as? KeyboardKey.Character)?.value ?: return emptyList()
        val letter = character.firstOrNull()?.isLetter() == true
        return if (isShifted && letter) key.alternates.map { it.uppercase() } else key.alternates
    }

    private fun refreshVisual() {
        val page = layout
        val pressed = tracks.values.filter { it.mode != Mode.DONE || page?.keys?.getOrNull(it.keyIndex)?.key == KeyboardKey.Shift }
            .map { it.keyIndex }.toSet()
        // The balloon: the character under the most recent finger still typing.
        val preview = tracks.values.lastOrNull { track ->
            track.mode == Mode.NORMAL && page?.keys?.getOrNull(track.keyIndex)?.key is KeyboardKey.Character
        }?.keyIndex
        val panel = tracks.values.lastOrNull { it.mode == Mode.ALTERNATES }?.panel
        val trackpad = tracks.values.any { it.mode == Mode.TRACKPAD }
        visual = TouchVisual(pressed, if (isDimmed) null else preview, panel, trackpad)
    }

    companion object {
        const val LONG_PRESS_MS = 380L
        const val SPACE_HOLD_MS = 650L
        const val DELETE_FIRST_REPEAT_MS = 420L
        const val DELETE_INTERVAL_MS = 65L
        const val DELETE_WORD_INTERVAL_MS = 180L
        /** Characters deleted one by one before a held delete takes whole words. */
        const val DELETE_WORD_AFTER = 14
        const val TRACKPAD_START_DP = 14f
        const val TRACKPAD_STEP_DP = 9f
        const val CANCEL_ABOVE_DP = 36f
    }
}

/** What a touch changes on screen. */
data class TouchVisual(
    val pressed: Set<Int> = emptySet(),
    /** Key whose character balloon is up. */
    val preview: Int? = null,
    val panel: AlternatesPanel? = null,
    val trackpad: Boolean = false
)

/** The row of long-press alternates above a key, in dp. */
data class AlternatesPanel(
    val keyIndex: Int,
    val options: List<String>,
    val left: Float,
    val top: Float,
    val cellWidth: Float,
    val cellHeight: Float,
    val selected: Int
) {
    val right: Float get() = left + cellWidth * options.size
    val selectedText: String? get() = options.getOrNull(selected)

    fun selecting(x: Float): AlternatesPanel {
        val index = floor((x - left) / cellWidth).toInt().coerceIn(0, options.lastIndex)
        return if (index == selected) this else copy(selected = index)
    }

    companion object {
        fun anchored(keyIndex: Int, key: LaidOutKey, options: List<String>, pageWidth: Float): AlternatesPanel {
            val cellWidth = maxOf(key.frame.width, 34f)
            val cellHeight = key.frame.height + 8f
            val total = cellWidth * options.size
            // Starts over the key and grows away from the nearer edge.
            val preferred = if (key.frame.centerX > pageWidth / 2f) key.frame.right - total else key.frame.left
            val left = preferred.coerceIn(2f, maxOf(2f, pageWidth - total - 2f))
            val top = key.frame.top - cellHeight - 6f
            return AlternatesPanel(keyIndex, options, left, top, cellWidth, cellHeight, selected = 0)
        }
    }
}
