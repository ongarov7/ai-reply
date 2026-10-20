package kz.yerek.aireply.keyboard.layout

import kotlin.math.floor
import kotlin.math.max

/**
 * How tall the AI panels may be, from the room the keyboard's window leaves
 * above the keys.
 *
 * A phone on its side gives the whole keyboard about 360dp. The keys keep
 * their height, so the panel is what gives way: Create's request field shows
 * fewer lines (it scrolls), never pushing the space bar off the screen. A
 * notice under the field is part of the budget, not added on top of it.
 *
 * Whatever this budget does not cover (a long reply, the copied message
 * opened up) still never pushes the keys: the service caps a panel at the
 * room above them, and the panel's fields give way first.
 *
 * Decided per window - a rotation, a new screen size, a request's outcome -
 * never per keystroke.
 */
object PanelFit {

    /**
     * Create while the request is written, without the field: padding 16,
     * header 30, the line under the field 30, the action row 34, three 6dp gaps.
     */
    const val CREATE_COMPOSING_CHROME = 128f

    /** The same with the line moved up into the header ([compact]). */
    const val CREATE_COMPACT_CHROME = 92f

    /** Line height and vertical padding of Create's request field. */
    const val CREATE_FIELD_LINE = 20f
    const val CREATE_FIELD_PADDING = 16f

    /**
     * The room above the keys, dp: a window at most [window] dp tall (null
     * where the system does not say) less the [navigationBar] under the keys
     * and the [keys] themselves. One number per window, whatever the measure
     * pass, so a panel planned with it asks for exactly what it gets.
     */
    fun roomAbove(window: Float?, navigationBar: Float, keys: Float): Float? {
        if (window == null || window.isNaN() || window.isInfinite() || window <= 0f) return null
        return (window - navigationBar.coerceAtLeast(0f) - keys).coerceAtLeast(0f)
    }

    /**
     * Lines for Create's request field: [preferred] where they fit in [room]
     * (dp above the keys) beside the [notice] under the field (dp, from
     * [createNotice]), fewer where they do not, never under one. Unknown room
     * keeps [preferred].
     */
    fun createInstructionLines(room: Float, preferred: Int, compact: Boolean = false, notice: Float = 0f): Int {
        if (room.isNaN() || room.isInfinite()) return preferred
        val chrome = if (compact) CREATE_COMPACT_CHROME else CREATE_COMPOSING_CHROME
        val field = room - chrome - CREATE_FIELD_PADDING - notice.coerceAtLeast(0f)
        val lines = floor(field / CREATE_FIELD_LINE).toInt()
        return lines.coerceIn(1, preferred.coerceAtLeast(1))
    }

    /** What a panel shows between its field and its action row after a request. */
    enum class Notice {
        NONE,

        /** One sentence, red for a failed request or grey about the clipboard. */
        LINE,

        /** «Лимит ответов исчерпан» with the button to the app. */
        LIMIT
    }

    /** The gap between a panel's rows. */
    const val PANEL_GAP = 6f

    /** A notice's text: 12sp on lines of this height - two at most, one when compact. */
    const val NOTICE_LINE = 16f
    const val NOTICE_LINES = 2

    /** The limit notice's first row: the title beside the 28dp "Open AI Reply" pill. */
    const val LIMIT_ROW = 30f

    /** The limit title: 13sp on lines of this height, two at most - one when compact. */
    const val LIMIT_TITLE_LINE = 18f

    /** Between the limit notice's row and its sentence. */
    const val LIMIT_BODY_GAP = 2f

    /** Lines a notice sentence may take: one on a phone on its side. */
    fun noticeLines(compact: Boolean): Int = if (compact) 1 else NOTICE_LINES

    /**
     * Height of [notice] under a panel's field, with the gap above it, dp.
     * Text grows with [fontScale]; the pill does not. [compact]: the sentence
     * is one line, and the limit notice its row alone (the sentence is left
     * to TalkBack).
     */
    fun noticeHeight(notice: Notice, compact: Boolean, fontScale: Float = 1f): Float {
        val scale = if (fontScale.isNaN() || fontScale <= 0f) 1f else fontScale
        return when (notice) {
            Notice.NONE -> 0f
            Notice.LINE -> PANEL_GAP + noticeLines(compact) * NOTICE_LINE * scale
            Notice.LIMIT -> PANEL_GAP + if (compact) {
                max(LIMIT_ROW, LIMIT_TITLE_LINE * scale)
            } else {
                max(LIMIT_ROW, 2 * LIMIT_TITLE_LINE * scale) + LIMIT_BODY_GAP + NOTICE_LINES * NOTICE_LINE * scale
            }
        }
    }

    /**
     * Spent replies on a phone on its side: Create says so in its header, in
     * the place of the line under the request, where the width is.
     */
    fun createLimitInHeader(notice: Notice, compact: Boolean): Boolean = compact && notice == Notice.LIMIT

    /**
     * What Create's request field gives up for [notice]: its [noticeHeight] -
     * except where the notice is in the header ([createLimitInHeader]), which
     * costs no height at all.
     */
    fun createNotice(notice: Notice, compact: Boolean, fontScale: Float = 1f): Float =
        if (createLimitInHeader(notice, compact)) 0f else noticeHeight(notice, compact, fontScale)

    /** Lines of the dictation status: two, or one on a phone on its side. */
    fun voiceLines(isLandscape: Boolean): Int = if (isLandscape) 1 else 2

    /**
     * A phone on its side: the width is there, the height is not. Create moves
     * the line under the request (or the limit notice) into its header, and a
     * notice under a field is one line.
     */
    fun compact(isLandscape: Boolean): Boolean = isLandscape
}
