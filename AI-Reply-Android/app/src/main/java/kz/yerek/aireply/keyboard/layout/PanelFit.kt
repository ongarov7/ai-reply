package kz.yerek.aireply.keyboard.layout

import kotlin.math.floor

/**
 * How tall the AI panels may be, from the room the keyboard's window leaves
 * above the keys.
 *
 * A phone on its side gives the whole keyboard about 360dp. The keys keep
 * their height, so the panel is what gives way: Create's request field shows
 * fewer lines (it scrolls), never pushing the space bar off the screen.
 *
 * Decided per window - a rotation, a new screen size - never per keystroke.
 */
object PanelFit {

    /**
     * Create while the request is written, without the field: padding 16,
     * header 30, the line under the field 30, the action row 34, three 6dp gaps.
     */
    const val CREATE_COMPOSING_CHROME = 128f

    /** The same with the line moved up into the header ([compactCreate]). */
    const val CREATE_COMPACT_CHROME = 92f

    /** Line height and vertical padding of Create's request field. */
    const val CREATE_FIELD_LINE = 20f
    const val CREATE_FIELD_PADDING = 16f

    /**
     * Lines for Create's request field: [preferred] where they fit in [room]
     * (dp above the keys), fewer where they do not, never under one. Unknown
     * room keeps [preferred].
     */
    fun createInstructionLines(room: Float, preferred: Int, compact: Boolean = false): Int {
        if (room.isNaN() || room.isInfinite()) return preferred
        val chrome = if (compact) CREATE_COMPACT_CHROME else CREATE_COMPOSING_CHROME
        val field = room - chrome - CREATE_FIELD_PADDING
        val lines = floor(field / CREATE_FIELD_LINE).toInt()
        return lines.coerceIn(1, preferred.coerceAtLeast(1))
    }

    /** Lines of the dictation status: two, or one on a phone on its side. */
    fun voiceLines(isLandscape: Boolean): Int = if (isLandscape) 1 else 2

    /**
     * On a phone on its side Create moves the line under the request into its
     * header: the width is there, the height is not.
     */
    fun compactCreate(isLandscape: Boolean): Boolean = isLandscape
}
