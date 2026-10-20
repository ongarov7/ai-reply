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

/** The grid's sizes, in dp and sp. */
object EmojiGridLayout {

    /** Narrower than this and an emoji is hard to hit. */
    const val MIN_CELL_WIDTH = 42f

    const val MIN_COLUMNS = 6
    const val MAX_COLUMNS = 18

    const val CELL_HEIGHT = 44f

    /** The emoji, drawn at a size that fills a cell without touching its neighbours. */
    const val EMOJI_SIZE_SP = 26f

    const val HEADER_HEIGHT = 26f

    /** Columns for a grid [width] dp wide: 7 on a 320 dp phone, 8 at 360, 9 at 393 and 411. */
    fun columns(width: Float): Int = (width / MIN_CELL_WIDTH).toInt().coerceIn(MIN_COLUMNS, MAX_COLUMNS)
}
