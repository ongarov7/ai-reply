package kz.yerek.aireply.keyboard.layout

import kotlin.math.floor
import kotlin.math.max
import kotlin.math.min

/**
 * The persona row above the keys, start to end:
 *
 *     [✨] [Friend] [Client] [Business] [Work] [your own…]
 *
 * ✨ (Create: write a new message with AI) is pinned at the leading edge, so
 * no number of personas can push it off the row or cover it. Everything to
 * its right belongs to the chips - and, while a word is being typed, to the
 * suggestion strip, which takes exactly the chips' place.
 *
 * Plain numbers in dp, so JVM tests can pin the row; `PersonaRow` in
 * ReplyPanel.kt draws exactly this.
 */
object PersonaRowLayout {

    /** What the row holds, in the order it is drawn and read by TalkBack. */
    enum class Slot { CREATE, CHIPS }

    val order: List<Slot> = listOf(Slot.CREATE, Slot.CHIPS)

    const val HEIGHT = 44f

    /** Between the row's content and the keyboard's edges. */
    const val SIDE_PADDING = 6f

    /** The ✨ disc. */
    const val CREATE_SIZE = 30f

    /** Between ✨ and the chips (or the suggestions). */
    const val SLOT_GAP = 6f

    const val CHIP_GAP = 6f

    /** Horizontal chip padding, roomiest first: the first at which the whole set fits wins. */
    val CHIP_PADDINGS = listOf(14f, 11f, 8f)

    /** The most a chip is widened to fill a row with room to spare. */
    const val CHIP_MAX_STRETCH = 28f

    /** Where the chips (and the suggestions) start, from the row's leading edge. */
    const val CHIPS_START = SIDE_PADDING + CREATE_SIZE + SLOT_GAP

    /** The width the chips - or the suggestions - get in a row [rowWidth] wide. */
    fun chipsWidth(rowWidth: Float): Float = max(0f, rowWidth - CHIPS_START - SIDE_PADDING)

    /**
     * Chip widths for labels [textWidths] wide in [available] dp: the roomiest
     * padding at which the whole set fits, then every chip widened by the same
     * whole number of dp (at most [CHIP_MAX_STRETCH]) to fill the space. When
     * even the tightest padding does not fit, the natural widths: the chips
     * scroll.
     */
    fun chipWidths(textWidths: List<Float>, available: Float): List<Float> {
        if (textWidths.isEmpty()) return emptyList()
        val gaps = CHIP_GAP * (textWidths.size - 1)
        val padding = CHIP_PADDINGS.firstOrNull { p -> textWidths.sumOf { (it + p * 2).toDouble() } + gaps <= available }
            ?: CHIP_PADDINGS.last()
        val natural = textWidths.map { it + padding * 2 }
        val used = natural.sum() + gaps
        if (used > available) return natural
        val extra = floor(min((available - used) / natural.size, CHIP_MAX_STRETCH))
        return natural.map { it + extra }
    }
}
