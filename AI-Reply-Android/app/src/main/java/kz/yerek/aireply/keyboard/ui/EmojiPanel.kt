package kz.yerek.aireply.keyboard.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyGridState
import androidx.compose.foundation.lazy.grid.LazyHorizontalGrid
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicText
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.EmojiEmotions
import androidx.compose.material.icons.outlined.EmojiEvents
import androidx.compose.material.icons.outlined.EmojiFlags
import androidx.compose.material.icons.outlined.EmojiFoodBeverage
import androidx.compose.material.icons.outlined.EmojiNature
import androidx.compose.material.icons.outlined.EmojiObjects
import androidx.compose.material.icons.outlined.EmojiSymbols
import androidx.compose.material.icons.outlined.EmojiTransportation
import androidx.compose.material.icons.outlined.Schedule
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.Stable
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.EmojiSupportMatch
import androidx.compose.ui.text.PlatformTextStyle
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch
import kz.yerek.aireply.keyboard.KeyboardTheme
import kz.yerek.aireply.keyboard.emoji.EmojiCatalog
import kz.yerek.aireply.keyboard.emoji.EmojiGridLayout
import kz.yerek.aireply.keyboard.emoji.EmojiGridMetrics
import kz.yerek.aireply.keyboard.emoji.EmojiPageModel
import kz.yerek.aireply.keyboard.emoji.EmojiTab
import kz.yerek.aireply.keyboard.layout.KeyboardLabels
import kz.yerek.aireply.keyboard.layout.LaidOutPanel

/**
 * What the emoji page shows, kept by the keyboard service for as long as it
 * runs: the catalog (once it is read, off the main thread), the page built
 * from it with the recent emoji, and the grid's scroll.
 *
 * The recent emoji are taken when the page OPENS ([opened]), not after every
 * tap: an emoji just typed must not jump to the front under the finger that
 * is about to tap the next one.
 */
@Stable
class EmojiPanelState {

    val grid = LazyGridState()

    var catalog: EmojiCatalog? by mutableStateOf(null)
        private set

    var page: EmojiPageModel by mutableStateOf(EmojiPageModel.EMPTY)
        private set

    /** Counts the page's openings: each one starts at the top. */
    var openings by mutableIntStateOf(0)
        private set

    private var recents: List<String> = emptyList()

    /** The tab of the first emoji on screen. */
    val selectedTab: EmojiTab? by derivedStateOf { page.tabAt(grid.firstVisibleItemIndex) }

    fun catalogLoaded(loaded: EmojiCatalog) {
        catalog = loaded
        rebuild()
    }

    fun opened(recent: List<String>) {
        recents = recent
        rebuild()
        openings++
    }

    private fun rebuild() {
        page = catalog?.let { EmojiPageModel.of(it, recents) } ?: EmojiPageModel.EMPTY
    }
}

/**
 * The emoji grid: every category in one sideways scroll, a few rows tall, the
 * name of the section at the leading edge above it, the tabs below following
 * along. Its box is the panel's whole touch area, so a tap between two emoji
 * never reaches the keys underneath. The names are in the layout's language
 * ([KeyboardLabels]), like the ABC key below.
 *
 * The emoji are drawn by the system font, exactly as the app they go into
 * will draw them - never swapped for EmojiCompat's - and only the ones that
 * font has ([kz.yerek.aireply.keyboard.emoji.PaintEmojiSupport]).
 */
@Composable
internal fun EmojiGrid(
    state: EmojiPanelState,
    panel: LaidOutPanel,
    theme: KeyboardTheme,
    labels: KeyboardLabels,
    dimmed: Boolean,
    onEmoji: (String) -> Unit
) {
    val page = state.page
    LaunchedEffect(state.openings) {
        if (state.grid.firstVisibleItemIndex != 0 || state.grid.firstVisibleItemScrollOffset != 0) state.grid.scrollToItem(0)
    }
    val density = LocalDensity.current.density
    Box(Modifier.fillMaxSize()) {
        if (page.isEmpty) {
            // Read once per process, in the background: a moment at most.
            if (state.catalog == null) {
                CircularProgressIndicator(
                    modifier = Modifier.align(Alignment.Center).size(20.dp),
                    strokeWidth = 2.dp,
                    color = theme.secondaryText
                )
            }
            return@Box
        }
        val inset = (panel.frame.left - panel.hitFrame.left).coerceAtLeast(0f)
        val metrics = EmojiGridLayout.metrics(
            width = panel.hitFrame.width,
            height = panel.hitFrame.height,
            sideInset = inset,
            topInset = (panel.frame.top - panel.hitFrame.top).coerceAtLeast(0f),
            density = density
        )
        metrics.titleTop?.let { top ->
            state.selectedTab?.let { tab ->
                Text(
                    text = labels.emojiTab(tab).uppercase(),
                    fontSize = 12.sp,
                    fontWeight = FontWeight.SemiBold,
                    color = theme.secondaryText,
                    maxLines = 1,
                    modifier = Modifier
                        .padding(start = (inset + 6f).dp, top = top.dp)
                        .semantics { heading() }
                )
            }
        }
        LazyHorizontalGrid(
            rows = GridCells.Fixed(metrics.rows),
            state = state.grid,
            contentPadding = PaddingValues(horizontal = metrics.contentInset.dp),
            modifier = Modifier
                .fillMaxWidth()
                .padding(top = metrics.gridTop.dp)
                .height((metrics.cellHeight * metrics.rows).dp)
                .alpha(if (dimmed) 0.45f else 1f)
        ) {
            items(
                count = page.items.size,
                key = { page.items[it].key },
                span = { index -> GridItemSpan(if (page.items[index] is EmojiPageModel.Header) maxLineSpan else 1) },
                contentType = { index -> if (page.items[index] is EmojiPageModel.Header) 0 else 1 }
            ) { index ->
                when (val item = page.items[index]) {
                    // No header in a sideways grid: the gap between two sections.
                    is EmojiPageModel.Header -> Spacer(Modifier.width(if (index == 0) 0.dp else EmojiGridLayout.SECTION_GAP.dp))
                    is EmojiPageModel.Cell -> EmojiCell(item.emoji, metrics, enabled = !dimmed, onEmoji = onEmoji)
                }
            }
        }
    }
}

@Composable
private fun EmojiCell(emoji: String, metrics: EmojiGridMetrics, enabled: Boolean, onEmoji: (String) -> Unit) {
    Box(
        modifier = Modifier
            .width(metrics.cellWidth.dp)
            .height(metrics.cellHeight.dp)
            .clip(RoundedCornerShape(8.dp))
            .clickable(enabled = enabled, role = Role.Button) { onEmoji(emoji) }
            // TalkBack names an emoji itself, in the phone's language.
            .semantics { contentDescription = emoji },
        contentAlignment = Alignment.Center
    ) {
        BasicText(text = emoji, style = EMOJI_STYLE.copy(fontSize = metrics.glyphSize.sp))
    }
}

/**
 * The categories between ABC and delete: one tab each, the one on screen in
 * the accent colour. A tap scrolls the grid to its header. The row is the
 * panel's whole touch area, so a tap between two icons is still a tab.
 */
@Composable
internal fun EmojiCategoryBar(
    state: EmojiPanelState,
    panel: LaidOutPanel,
    theme: KeyboardTheme,
    labels: KeyboardLabels,
    onTab: () -> Unit
) {
    val scope = rememberCoroutineScope()
    val page = state.page
    val selected = state.selectedTab
    val inner = panel.innerPadding()
    Row(
        modifier = Modifier
            .fillMaxSize()
            .padding(top = inner.top.dp, bottom = inner.bottom.dp),
        verticalAlignment = Alignment.CenterVertically
    ) {
        page.tabs.forEach { tab ->
            val isSelected = tab == selected
            val name = labels.emojiTab(tab)
            Box(
                modifier = Modifier
                    .weight(1f)
                    .fillMaxHeight()
                    .clickable(role = Role.Tab) {
                        onTab()
                        val start = page.startOf(tab) ?: return@clickable
                        scope.launch { state.grid.scrollToItem(start) }
                    }
                    .semantics {
                        contentDescription = name
                        this.selected = isSelected
                    },
                contentAlignment = Alignment.Center
            ) {
                Icon(
                    imageVector = tabIcon(tab),
                    contentDescription = null,
                    tint = if (isSelected) theme.accent else theme.secondaryText,
                    modifier = Modifier.size(20.dp)
                )
            }
        }
    }
}

/** Where the panel's drawn frame sits inside its touch area, in dp. */
private class Insets(val start: Float, val top: Float, val end: Float, val bottom: Float)

private fun LaidOutPanel.innerPadding() = Insets(
    start = (frame.left - hitFrame.left).coerceAtLeast(0f),
    top = (frame.top - hitFrame.top).coerceAtLeast(0f),
    end = (hitFrame.right - frame.right).coerceAtLeast(0f),
    bottom = (hitFrame.bottom - frame.bottom).coerceAtLeast(0f)
)

private fun tabIcon(tab: EmojiTab): ImageVector = when (tab) {
    EmojiTab.RECENT -> Icons.Outlined.Schedule
    EmojiTab.SMILEYS_PEOPLE -> Icons.Outlined.EmojiEmotions
    EmojiTab.ANIMALS_NATURE -> Icons.Outlined.EmojiNature
    EmojiTab.FOOD_DRINK -> Icons.Outlined.EmojiFoodBeverage
    EmojiTab.TRAVEL_PLACES -> Icons.Outlined.EmojiTransportation
    EmojiTab.ACTIVITIES -> Icons.Outlined.EmojiEvents
    EmojiTab.OBJECTS -> Icons.Outlined.EmojiObjects
    EmojiTab.SYMBOLS -> Icons.Outlined.EmojiSymbols
    EmojiTab.FLAGS -> Icons.Outlined.EmojiFlags
}

/** The system font's own emoji; never EmojiCompat's replacements. */
private val EMOJI_STYLE = TextStyle(
    platformStyle = PlatformTextStyle(emojiSupportMatch = EmojiSupportMatch.None)
)
