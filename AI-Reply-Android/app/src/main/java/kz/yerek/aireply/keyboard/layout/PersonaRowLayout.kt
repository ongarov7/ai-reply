package kz.yerek.aireply.keyboard.layout

import kotlin.math.floor
import kotlin.math.max
import kotlin.math.min

/**
 * The persona row above the keys, start to end:
 *
 *     [✨] [Friend] [Client] [Colleague] [Partner] [your own…] [🎙]
 *
 * ✨ (Create: write a new message with AI) is pinned at the leading edge, so
 * no number of personas can push it off the row or cover it. 🎙 (voice input
 * into the app's field), where it is offered, is pinned at the trailing edge
 * the same way. Everything between belongs to the chips - and, while a word
 * is being typed, to the suggestion strip, which takes exactly the chips' place.
 *
 * A built-in chip carries its recipient's icon when the whole set fits with
 * the icons; otherwise the icons go before anything scrolls ([chipFit]). The
 * icons never change the chips' height or move ✨ and 🎙.
 *
 * Plain numbers in dp, so JVM tests can pin the row; `PersonaRow` in
 * ReplyPanel.kt draws exactly this.
 */
object PersonaRowLayout {

    /** What the row holds, in the order it is drawn and read by TalkBack. */
    enum class Slot { CREATE, CHIPS, MIC }

    /** The row without 🎙: a password field, or no voice input offered. */
    val order: List<Slot> = listOf(Slot.CREATE, Slot.CHIPS)

    fun order(hasMic: Boolean): List<Slot> = if (hasMic) order + Slot.MIC else order

    /** The row: the pills and two dp above and below them. */
    const val HEIGHT = 48f

    /** ✨, the chips and 🎙: as tall as the row's touch targets need. */
    const val PILL_HEIGHT = 44f

    /** ✨ and 🎙 are a touch rounder than the chips beside them. */
    const val CREATE_RADIUS = 16f
    const val CHIP_RADIUS = 15f

    /** Between the row's content and the keyboard's edges. */
    const val SIDE_PADDING = 8f

    /** A recognizable ✨ AI pill, with a native-size interactive height. */
    const val CREATE_WIDTH = 64f

    /** The microphone keeps a separate, square touch target. */
    const val MIC_SIZE = 44f

    /** Between ✨ and the chips (or the suggestions), and between them and 🎙. */
    const val SLOT_GAP = 6f

    const val CHIP_GAP = 6f

    /** No chip is narrower than a touch target. */
    const val CHIP_MIN_WIDTH = 44f

    /** Horizontal chip padding, roomiest first: the first at which the whole set fits wins. */
    val CHIP_PADDINGS = listOf(14f, 11f, 8f)

    /**
     * The last padding before labels scroll. Icons keep their roomier
     * padding; adding the wider AI action never squeezes either below it.
     */
    const val TIGHT_PADDING = 6f

    /** The most a chip is widened to fill a row with room to spare. */
    const val CHIP_MAX_STRETCH = 28f

    /** A recipient icon in a chip, and the space between it and the label. */
    const val ICON_SIZE = 14f
    const val ICON_GAP = 4f

    /** What an icon adds to a chip's width. */
    const val ICON_WIDTH = ICON_SIZE + ICON_GAP

    /** Where the chips (and the suggestions) start, from the row's leading edge. */
    const val CHIPS_START = SIDE_PADDING + CREATE_WIDTH + SLOT_GAP

    /** The width the chips - or the suggestions - get in a row [rowWidth] wide, with or without 🎙. */
    fun chipsWidth(rowWidth: Float, hasMic: Boolean = false): Float =
        max(0f, rowWidth - CHIPS_START - SIDE_PADDING - if (hasMic) SLOT_GAP + MIC_SIZE else 0f)

    /** How the chips are drawn: their widths, and whether the built-ins show their icons. */
    data class ChipFit(val widths: List<Float>, val showsIcons: Boolean)

    /**
     * The chips for labels [textWidths] wide, [hasIcon] saying which chips have
     * an icon at all (a custom template has none), in [available] dp:
     *
     *  1. with icons, at the roomiest of [CHIP_PADDINGS] at which the whole set fits;
     *  2. otherwise without icons, at the roomiest padding that fits, [TIGHT_PADDING] last;
     *  3. otherwise the labels' natural widths at the last of [CHIP_PADDINGS]: the chips scroll.
     *
     * Whatever fits is then widened evenly to fill the row, as [chipWidths] does.
     */
    fun chipFit(textWidths: List<Float>, hasIcon: List<Boolean>, available: Float): ChipFit {
        if (textWidths.isEmpty()) return ChipFit(emptyList(), showsIcons = false)
        if (hasIcon.any { it }) {
            val withIcons = textWidths.mapIndexed { index, width ->
                if (hasIcon.getOrElse(index) { false }) width + ICON_WIDTH else width
            }
            fitting(withIcons, available, CHIP_PADDINGS)?.let { return ChipFit(it, showsIcons = true) }
        }
        return ChipFit(chipWidths(textWidths, available), showsIcons = false)
    }

    /**
     * Chip widths for labels [textWidths] wide in [available] dp: the roomiest
     * padding at which the whole set fits, then every chip widened by the same
     * whole number of dp (at most [CHIP_MAX_STRETCH]) to fill the space. When
     * even [TIGHT_PADDING] does not fit, the natural widths at the last of
     * [CHIP_PADDINGS]: the chips scroll, and need not be cramped to do it.
     */
    fun chipWidths(textWidths: List<Float>, available: Float): List<Float> {
        if (textWidths.isEmpty()) return emptyList()
        return fitting(textWidths, available, CHIP_PADDINGS + TIGHT_PADDING)
            ?: textWidths.map { max(CHIP_MIN_WIDTH, it + CHIP_PADDINGS.last() * 2) }
    }

    /** The set at the roomiest of [paddings] that fits, stretched to fill; null when none fits. */
    private fun fitting(contentWidths: List<Float>, available: Float, paddings: List<Float>): List<Float>? {
        val gaps = CHIP_GAP * (contentWidths.size - 1)
        val padding = paddings.firstOrNull { p ->
            contentWidths.sumOf { max(CHIP_MIN_WIDTH, it + p * 2).toDouble() } + gaps <= available
        } ?: return null
        val natural = contentWidths.map { max(CHIP_MIN_WIDTH, it + padding * 2) }
        val used = natural.sum() + gaps
        val extra = floor(min((available - used) / natural.size, CHIP_MAX_STRETCH))
        return natural.map { it + extra }
    }
}

/**
 * The reply controls under a result: Back, Regenerate and Edit on the left,
 * Insert on the right, the version pager between them - or, where that does
 * not fit, the pager and Insert on a second row. The pager's room is reserved
 * even for the first version, so a new one never changes the panel's height.
 *
 * Plain numbers in dp, so JVM tests can pin it.
 */
data class ResultActionsLayout(
    val back: Box,
    val regenerate: Box,
    val edit: Box,
    val previous: Box,
    val version: Box,
    val next: Box,
    val insert: Box,
    val height: Float
) {
    companion object {
        const val TARGET = 44f
        const val GAP = 6f
        private const val VERSION_WIDTH = 34f
        private const val MIN_REGENERATE = 112f
        private const val MIN_INSERT = 96f

        fun of(width: Float, regenerateWidth: Float, insertWidth: Float): ResultActionsLayout {
            val row = max(0f, width)
            val pager = TARGET * 2 + VERSION_WIDTH
            val regenerate = min(max(MIN_REGENERATE, regenerateWidth), max(TARGET, row - TARGET * 2 - GAP * 2))
            val insert = min(row, max(MIN_INSERT, insertWidth))
            val first = TARGET * 2 + regenerate + GAP * 2
            val oneRow = first + GAP + pager + GAP + insert <= row
            val second = if (oneRow) 0f else TARGET + GAP
            val backBox = Box(0f, 0f, TARGET, TARGET)
            val regenerateBox = Box(backBox.right + GAP, 0f, backBox.right + GAP + regenerate, TARGET)
            val editBox = Box(regenerateBox.right + GAP, 0f, regenerateBox.right + GAP + TARGET, TARGET)
            val insertBox = Box(row - insert, second, row, second + TARGET)
            val pagerLeft = if (oneRow) editBox.right + GAP + max(0f, (insertBox.left - GAP - editBox.right - GAP - pager) / 2f) else 0f
            val previousBox = Box(pagerLeft, second, pagerLeft + TARGET, second + TARGET)
            val versionBox = Box(previousBox.right, second, previousBox.right + VERSION_WIDTH, second + TARGET)
            val nextBox = Box(versionBox.right, second, versionBox.right + TARGET, second + TARGET)
            return ResultActionsLayout(backBox, regenerateBox, editBox, previousBox, versionBox, nextBox, insertBox, second + TARGET)
        }
    }
}
