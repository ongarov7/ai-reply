package kz.yerek.aireply.keyboard.emoji

/**
 * The tabs under the emoji grid, in the grid's order: the recent emoji when
 * there are any, then the catalog's categories - Smileys and People under
 * one tab, as on the system keyboards, so nine tabs fit a 320 dp phone.
 */
enum class EmojiTab(val categories: List<EmojiCategory>) {
    RECENT(emptyList()),
    SMILEYS_PEOPLE(listOf(EmojiCategory.SMILEYS_EMOTION, EmojiCategory.PEOPLE_BODY)),
    ANIMALS_NATURE(listOf(EmojiCategory.ANIMALS_NATURE)),
    FOOD_DRINK(listOf(EmojiCategory.FOOD_DRINK)),
    TRAVEL_PLACES(listOf(EmojiCategory.TRAVEL_PLACES)),
    ACTIVITIES(listOf(EmojiCategory.ACTIVITIES)),
    OBJECTS(listOf(EmojiCategory.OBJECTS)),
    SYMBOLS(listOf(EmojiCategory.SYMBOLS)),
    FLAGS(listOf(EmojiCategory.FLAGS))
}

/**
 * Everything the emoji grid shows, as one flat list for the lazy grid: a
 * header per tab, then that tab's emoji. The tabs follow the scroll - the
 * tab of the first item on screen is the selected one ([tabAt]) - and a tab
 * tapped scrolls to its header ([startOf]).
 *
 * Pure, so the order, the recent emoji and the tab sync are tested on the JVM.
 *
 * Эмодзи торы: әр санаттың тақырыбы, содан соң оның эмодзилері.
 */
class EmojiPageModel private constructor(
    val items: List<Item>,
    private val starts: List<Pair<EmojiTab, Int>>
) {

    sealed interface Item {
        /** Stable across rebuilds, so the grid keeps its place. */
        val key: String
    }

    data class Header(val tab: EmojiTab) : Item {
        override val key: String get() = "header:${tab.name}"
    }

    data class Cell(val emoji: String, val tab: EmojiTab) : Item {
        override val key: String get() = "${tab.name}:$emoji"
    }

    /** The tabs there are, in order. */
    val tabs: List<EmojiTab> get() = starts.map { it.first }

    val isEmpty: Boolean get() = items.isEmpty()

    /** The index of [tab]'s header; null when the page has no such tab. */
    fun startOf(tab: EmojiTab): Int? = starts.firstOrNull { it.first == tab }?.second

    /** The tab the item at [index] belongs to. */
    fun tabAt(index: Int): EmojiTab? = starts.lastOrNull { it.second <= index }?.first ?: starts.firstOrNull()?.first

    companion object {
        val EMPTY = EmojiPageModel(emptyList(), emptyList())

        /**
         * The page for [catalog], with [recents] (newest first) on top. A
         * recent emoji this phone does not offer - one typed before an
         * update of the catalog - is left out.
         */
        fun of(catalog: EmojiCatalog, recents: List<String>): EmojiPageModel {
            val items = mutableListOf<Item>()
            val starts = mutableListOf<Pair<EmojiTab, Int>>()
            fun section(tab: EmojiTab, emoji: List<String>) {
                if (emoji.isEmpty()) return
                starts += tab to items.size
                items += Header(tab)
                emoji.mapTo(items) { Cell(it, tab) }
            }
            section(EmojiTab.RECENT, recents.filter { it in catalog.all }.distinct())
            val byCategory = catalog.groups.associateBy { it.category }
            EmojiTab.entries.filter { it != EmojiTab.RECENT }.forEach { tab ->
                section(tab, tab.categories.flatMap { category -> byCategory[category]?.entries.orEmpty().map { it.emoji } })
            }
            return EmojiPageModel(items, starts)
        }
    }
}

/**
 * Where the emoji page's grid goes: a few rows of emoji scrolling sideways,
 * section after section, the section's name above them, worked out from the
 * panel's size. Pure, so it is tested on the JVM.
 *
 * [titleTop] is null when the panel is too short for a title.
 */
data class EmojiGridMetrics(
    val rows: Int,
    val cellWidth: Float,
    val cellHeight: Float,
    val glyphSize: Float,
    val titleTop: Float?,
    val gridTop: Float,
    val contentInset: Float
)

object EmojiGridLayout {

    /** A row is never shorter than this: fewer rows instead, never fewer than [MIN_ROWS]. */
    const val MIN_CELL_HEIGHT = 34f
    const val MIN_ROWS = 3
    const val MAX_ROWS = 5

    /** Bigger glyphs only look bloated. */
    const val MAX_GLYPH = 32f

    /** Between one section's last column and the next one's first. */
    const val SECTION_GAP = 12f
    const val TITLE_HEIGHT = 16f

    /**
     * The grid for a panel [width] by [height] dp, inset by the keyboard's
     * [sideInset]; [topInset] places the section's name.
     */
    fun metrics(width: Float, height: Float, sideInset: Float, topInset: Float, density: Float): EmojiGridMetrics {
        val titleTop = maxOf(2f, topInset - 3f)
        val withTitle = titleTop + TITLE_HEIGHT + 2f
        val bottom = height - 2f
        val showsTitle = bottom - withTitle >= MIN_CELL_HEIGHT * MIN_ROWS
        val gridTop = if (showsTitle) withTitle else 2f
        val available = maxOf(1f, bottom - gridTop)
        val rows = (available / MIN_CELL_HEIGHT).toInt().coerceIn(MIN_ROWS, MAX_ROWS)
        val cellHeight = floorToPixel(available / rows, density)
        val gridWidth = maxOf(1f, width - sideInset * 2)
        val target = minOf(cellHeight * 1.15f, 46f)
        val columns = maxOf(1, kotlin.math.round(gridWidth / target).toInt())
        val cellWidth = floorToPixel(gridWidth / columns, density)
        return EmojiGridMetrics(
            rows = rows,
            cellWidth = cellWidth,
            cellHeight = cellHeight,
            glyphSize = minOf(MAX_GLYPH, kotlin.math.floor(minOf(cellWidth, cellHeight) * 0.78f)),
            titleTop = titleTop.takeIf { showsTitle },
            gridTop = gridTop + (available - cellHeight * rows) / 2f,
            contentInset = sideInset
        )
    }

    private fun floorToPixel(value: Float, density: Float): Float =
        kotlin.math.floor(value * density) / density
}
