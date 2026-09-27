package kz.yerek.aireply.keyboard.layout

import kz.yerek.aireply.keyboard.KeyboardKey
import kotlin.math.max
import kotlin.math.min
import kotlin.math.roundToInt

/** Key height and the vertical gap between rows, in dp. */
data class RowMetrics(val key: Float, val gap: Float) {
    fun height(rows: Int): Float = if (rows <= 0) 0f else rows * key + (rows - 1) * gap
}

/**
 * Everything about key size that depends on the device, derived from the
 * live width of the keyboard rather than from a list of phones.
 *
 * The numbers lean slightly larger than Gboard's defaults: the complaint this
 * keyboard answers is keys too small to hit. All values are dp; [density]
 * only snaps edges to whole pixels.
 */
data class KeyboardSizing(
    val width: Float,
    val isLandscape: Boolean,
    val density: Float = 3f
) {
    val sideInset: Float get() = if (isLandscape) 4f else 3f
    val topInset: Float get() = if (isLandscape) 4f else 6f
    val bottomInset: Float get() = if (isLandscape) 3f else 4f

    /**
     * Horizontal gap between keys. The eleven-column Cyrillic grid gives up a
     * dp per gap, which buys each key almost a dp of width.
     */
    fun columnGap(columns: Int): Float {
        val dense = columns >= 11
        if (isLandscape) return if (dense) 5f else 6f
        if (width < 360f) return if (dense) 4f else 5f
        return if (dense) 5f else 6f
    }

    /** Key height and row gap a page with [rows] rows has on its own. */
    fun natural(rows: Int): RowMetrics {
        val fiveRows = rows >= 5
        if (isLandscape) return if (fiveRows) RowMetrics(30f, 5f) else RowMetrics(34f, 6f)
        return when {
            width < 360f -> if (fiveRows) RowMetrics(38f, 7f) else RowMetrics(41f, 9f)
            width < 390f -> if (fiveRows) RowMetrics(41f, 7.5f) else RowMetrics(44f, 10f)
            width < 420f -> if (fiveRows) RowMetrics(43f, 8f) else RowMetrics(46f, 11f)
            else -> if (fiveRows) RowMetrics(45f, 8.5f) else RowMetrics(48f, 11f)
        }
    }

    /**
     * Height of the key area for layouts whose largest page has
     * [maximumRows] rows. Every page is laid out into exactly this height.
     *
     * [screenHeight] caps it: a phone on its side must still show the
     * conversation, so the keys never take more than half the screen.
     */
    fun keyAreaHeight(maximumRows: Int, screenHeight: Float? = null): Float {
        val rows = max(maximumRows, 1)
        val natural = topInset + natural(rows).height(rows) + bottomInset
        val cap = screenHeight?.takeIf { it > 0f }?.let { it * if (isLandscape) 0.5f else 0.45f }
        return pixel(if (cap != null) min(natural, cap) else natural)
    }

    fun pixel(value: Float): Float = (value * density).roundToInt() / density

    companion object {
        /** Wider than tall is landscape. */
        fun isLandscape(width: Float, screenHeight: Float): Boolean =
            screenHeight > 0f && width > screenHeight
    }
}

/** An axis-aligned rectangle in dp. */
data class Box(val left: Float, val top: Float, val right: Float, val bottom: Float) {
    val width: Float get() = right - left
    val height: Float get() = bottom - top
    val centerX: Float get() = (left + right) / 2f
    val centerY: Float get() = (top + bottom) / 2f

    /** Half-open, so neighbouring hit boxes never both claim a point. */
    fun contains(x: Float, y: Float): Boolean = x >= left && x < right && y >= top && y < bottom
}

/** One key, positioned. */
data class LaidOutKey(
    val key: KeyboardKey,
    /** Where the key is drawn. */
    val frame: Box,
    /**
     * Where a touch counts as this key. The hit boxes of a page tile it
     * completely: every gap, every spacer and every edge belongs to the
     * nearest key, so there is no point a finger can land on and get nothing.
     */
    val hitFrame: Box,
    val row: Int,
    val alternates: List<String>,
    val hint: String?
)

data class PageLayout(
    val page: KeyboardPage,
    val keys: List<LaidOutKey>,
    val width: Float,
    val height: Float,
    val rowMetrics: RowMetrics
) {
    /**
     * The key under a point. Anything inside the page resolves to a key; a
     * point outside it (a touch that slid off an edge) to the nearest one.
     */
    fun keyIndex(x: Float, y: Float): Int? {
        if (keys.isEmpty()) return null
        val hit = keys.indexOfFirst { it.hitFrame.contains(x, y) }
        if (hit >= 0) return hit
        var best = -1
        var bestDistance = Float.MAX_VALUE
        keys.forEachIndexed { index, key ->
            val dx = max(max(key.frame.left - x, 0f), x - key.frame.right)
            val dy = max(max(key.frame.top - y, 0f), y - key.frame.bottom)
            val distance = dx * dx + dy * dy
            if (distance < bestDistance) {
                bestDistance = distance
                best = index
            }
        }
        return best.takeIf { it >= 0 }
    }
}

/** A key slot with its horizontal extent, while a row is being placed. */
private class PlacedSlot(val slot: KeySlot, val left: Float, val right: Float)

object KeyboardGeometry {

    /**
     * Row metrics for [rows] rows in [available] dp.
     *
     * A page with fewer rows than the tallest page gets the difference: the
     * gaps grow a little (never past 1.4 times their natural size) and the
     * keys take the rest. That is what keeps the keyboard's height constant
     * across ҚАЗ's five rows and everyone else's four.
     */
    fun rowMetrics(rows: Int, available: Float, sizing: KeyboardSizing): RowMetrics {
        val count = max(rows, 1)
        val natural = sizing.natural(count)
        val naturalHeight = natural.height(count)
        if (count == 1) return RowMetrics(max(available, 1f), 0f)

        if (available <= naturalHeight + 0.5f) {
            val scale = max(available, 1f) / naturalHeight
            return RowMetrics(natural.key * scale, natural.gap * scale)
        }
        val scale = available / naturalHeight
        val gap = natural.gap * min(scale, 1.4f)
        val key = (available - (count - 1) * gap) / count
        return RowMetrics(key, gap)
    }

    fun layout(page: KeyboardPage, sizing: KeyboardSizing, areaHeight: Float): PageLayout {
        val width = sizing.width
        val innerWidth = width - sizing.sideInset * 2
        val available = areaHeight - sizing.topInset - sizing.bottomInset
        val metrics = rowMetrics(page.rows.size, available, sizing)

        val keys = mutableListOf<LaidOutKey>()
        var rowTop = sizing.topInset

        // Where one row's touch band ends and the next one's begins: half the
        // row gap each side, computed ONCE per boundary so the bands meet
        // exactly, and the outer rows reach the very top and bottom.
        val tops = page.rows.indices.map { sizing.topInset + it * (metrics.key + metrics.gap) }
        val boundaries = page.rows.indices.map { index ->
            when (index) {
                page.rows.lastIndex -> areaHeight
                else -> sizing.pixel(tops[index] + metrics.key + metrics.gap / 2f)
            }
        }

        page.rows.forEachIndexed { rowIndex, row ->
            // A row on its own grid (the bottom row) also takes that grid's
            // gap, so it is identical on every layout.
            val columns = max(row.unitColumns ?: page.columns, 1)
            val gap = sizing.columnGap(columns)
            val unit = (innerWidth - (columns - 1) * gap) / columns

            // Gaps sit only between two KEYS. A spacer is itself the space
            // between its neighbours, so it never gets a gap of its own.
            var fixedWidth = 0f
            var flexibleWeight = 0f
            var gapCount = 0
            row.slots.forEachIndexed { index, slot ->
                when (val w = slot.width) {
                    is KeyWidth.Units -> fixedWidth += w.units * unit
                    is KeyWidth.Flexible -> flexibleWeight += max(w.weight, 0f)
                }
                if (index > 0 && slot.key != null && row.slots[index - 1].key != null) gapCount++
            }
            val flexibleWidth = max(0f, innerWidth - fixedWidth - gapCount * gap)

            rowTop = tops[rowIndex]
            val top = sizing.pixel(rowTop)
            val bottom = sizing.pixel(rowTop + metrics.key)
            var x = sizing.sideInset

            val placed = mutableListOf<PlacedSlot>()

            row.slots.forEachIndexed { index, slot ->
                if (index > 0 && slot.key != null && row.slots[index - 1].key != null) x += gap
                val slotWidth = when (val w = slot.width) {
                    is KeyWidth.Units -> w.units * unit
                    is KeyWidth.Flexible ->
                        if (flexibleWeight > 0f) flexibleWidth * max(w.weight, 0f) / flexibleWeight else 0f
                }
                if (slot.key != null) placed += PlacedSlot(slot, sizing.pixel(x), sizing.pixel(x + slotWidth))
                x += slotWidth
            }

            val bandTop = if (rowIndex == 0) 0f else boundaries[rowIndex - 1]
            val bandBottom = boundaries[rowIndex]

            placed.forEachIndexed { index, key ->
                // Horizontal: the midpoint to each neighbour, and the screen
                // edge for the first and last key of the row.
                val left = if (index == 0) 0f else sizing.pixel((placed[index - 1].right + key.left) / 2f)
                val right = if (index == placed.lastIndex) {
                    width
                } else {
                    sizing.pixel((key.right + placed[index + 1].left) / 2f)
                }
                keys += LaidOutKey(
                    key = key.slot.key!!,
                    frame = Box(key.left, top, key.right, bottom),
                    hitFrame = Box(left, bandTop, right, bandBottom),
                    row = rowIndex,
                    alternates = key.slot.alternates,
                    hint = key.slot.hint
                )
            }

        }

        return PageLayout(page, keys, width, areaHeight, metrics)
    }
}
