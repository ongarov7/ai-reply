package kz.yerek.aireply.keyboard.ui

import androidx.compose.animation.Crossfade
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.AutoAwesome
import androidx.compose.material.icons.filled.Cancel
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.ChevronLeft
import androidx.compose.material.icons.filled.ChevronRight
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.ContentPaste
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material.icons.filled.ExpandLess
import androidx.compose.material.icons.filled.ExpandMore
import androidx.compose.material.icons.filled.Mic
import androidx.compose.material.icons.filled.MoreHoriz
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Stop
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.layout.Layout
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Constraints
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kz.yerek.aireply.R
import kz.yerek.aireply.ai.AIReplyService
import kz.yerek.aireply.ai.AppStrings
import kz.yerek.aireply.domain.model.TemplateSummary
import kz.yerek.aireply.keyboard.KeyboardTheme
import kz.yerek.aireply.keyboard.autocorrect.Suggestion
import kz.yerek.aireply.keyboard.reply.ReplyComposerFlow
import kz.yerek.aireply.keyboard.reply.ReplySession
import kz.yerek.aireply.voice.VoiceFailure
import kz.yerek.aireply.voice.VoiceState

/** Which of the composer's fields the keys are editing. */
enum class PanelFocus { SOURCE, INSTRUCTION, DRAFT }

/** A one-tap intent that writes into the instruction. */
data class QuickIntent(val id: String, val label: String, val phrase: String)

/** Everything the composer draws, gathered by the service. */
class ComposerModel(
    val personaName: String,
    val session: ReplySession,
    val focus: PanelFocus,
    val sourceExpanded: Boolean,
    val sourceLimit: Int,
    val voice: VoiceState,
    val intents: List<QuickIntent>,
    /** The tallest the reply field may be, from the screen the keyboard is on. */
    val maxFieldLines: Int,
    /** Suggestions and the polish chip, shown in the intents' place. */
    val assist: TypingAssist = TypingAssist.NONE
)

class ComposerActions(
    /** Back to the persona row, session kept. */
    val onPersona: () -> Unit,
    /** Discard everything. */
    val onClose: () -> Unit,
    val onPaste: () -> Unit,
    val onToggleSource: () -> Unit,
    val onClearSource: () -> Unit,
    /** A tap inside a field: focus it, caret at [Int]. */
    val onFieldTap: (PanelFocus, Int) -> Unit,
    /** Reply, Stop or Try again. */
    val onPrimary: () -> Unit,
    val onIntent: (QuickIntent) -> Unit,
    val onBack: () -> Unit,
    /** Regenerate, or Stop while regenerating. */
    val onRegenerate: () -> Unit,
    val onEdit: () -> Unit,
    val onInsert: () -> Unit,
    val onPreviousVersion: () -> Unit,
    val onNextVersion: () -> Unit,
    val onConflict: (ReplyComposerFlow.ConflictChoice) -> Unit,
    val onMic: () -> Unit,
    val assist: TypingAssistActions
)

// ----------------------------------------------------------------- persona row

/**
 * The strip above the keys while no reply is being written: one chip per
 * persona, the last one used marked, then "+" (create a persona in the app)
 * and "✨" (Create: write a new message with AI).
 *
 * ADAPTIVE: "+" and "✨" are pinned at the end, so neither is ever cut off.
 * The personas share the rest of the row - stretched evenly when they fit,
 * with tighter padding when space is short, and scrolling (with a fade, never
 * a clipped label) only when even that does not fit.
 *
 * While a word is being typed and there is something to suggest, the word
 * suggestions take the personas' place - "+" and "✨" stay exactly where they
 * are, and the row keeps its height.
 */
@Composable
fun PersonaRow(
    chips: List<TemplateSummary>,
    languageCode: String,
    selectedId: String?,
    notice: String?,
    theme: KeyboardTheme,
    strings: AppStrings,
    onSelect: (String) -> Unit,
    onAdd: () -> Unit,
    onCreate: () -> Unit,
    modifier: Modifier = Modifier,
    suggestions: List<Suggestion> = emptyList(),
    onPick: (Suggestion) -> Unit = {}
) {
    Box(
        modifier = modifier
            .fillMaxWidth()
            .height(PERSONA_ROW_HEIGHT)
            .padding(horizontal = 6.dp),
        contentAlignment = Alignment.CenterStart
    ) {
        if (notice != null) {
            // A notice replaces the row rather than displacing it, so it never
            // moves the keys.
            Text(
                text = notice,
                color = theme.secondaryText,
                fontSize = 12.sp,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.fillMaxWidth().padding(horizontal = 4.dp)
            )
            return@Box
        }
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically
        ) {
            // Fades only when the strip comes or goes, never between keystrokes.
            Crossfade(
                targetState = suggestions.isNotEmpty(),
                animationSpec = tween(SUGGESTIONS_FADE_MS),
                modifier = Modifier.weight(1f),
                label = "suggestions"
            ) { typing ->
                if (typing) {
                    SuggestionStrip(suggestions, theme, strings, onPick, Modifier.fillMaxWidth().height(PERSONA_ROW_HEIGHT - 8.dp))
                } else {
                    PersonaChips(
                        chips = chips,
                        languageCode = languageCode,
                        selectedId = selectedId,
                        theme = theme,
                        onSelect = onSelect
                    )
                }
            }
            Spacer(Modifier.width(6.dp))
            CircleIcon(
                icon = Icons.Filled.Add,
                description = strings[R.string.kb_add_template],
                theme = theme,
                size = 30.dp,
                iconSize = 18.dp,
                onClick = onAdd
            )
            Spacer(Modifier.width(6.dp))
            // Same disc as "+"; only the glyph carries the accent.
            CircleIcon(
                icon = Icons.Filled.AutoAwesome,
                description = strings[R.string.kb_compose_open],
                theme = theme,
                size = 30.dp,
                iconSize = 17.dp,
                tint = theme.accent,
                onClick = onCreate
            )
        }
    }
}

@Composable
private fun PersonaChips(
    chips: List<TemplateSummary>,
    languageCode: String,
    selectedId: String?,
    theme: KeyboardTheme,
    onSelect: (String) -> Unit,
    modifier: Modifier = Modifier
) {
    val scroll = rememberScrollState()
    val measurer = rememberTextMeasurer()
    val density = LocalDensity.current
    val names = chips.map { it.names[languageCode] ?: it.id }
    BoxWithConstraints(modifier) {
        // A little under the real width, so pixel rounding never leaves a
        // set that fits one pixel short of the edge (and fading for nothing).
        val available = maxWidth - 2.dp
        // Text widths, then the roomiest padding at which the whole set fits.
        val widths = remember(names, available) {
            val text = names.map { name ->
                with(density) {
                    measurer.measure(name, TextStyle(fontSize = 14.sp, fontWeight = FontWeight.SemiBold)).size.width.toDp()
                }
            }
            val gaps = CHIP_GAP * (names.size - 1).coerceAtLeast(0)
            val padding = CHIP_PADDINGS.firstOrNull { p -> text.sumOf { (it + p * 2).value.toDouble() }.dp + gaps <= available }
                ?: CHIP_PADDINGS.last()
            val natural = text.map { it + padding * 2 }
            val used = natural.sumOf { it.value.toDouble() }.dp + gaps
            if (used <= available && natural.isNotEmpty()) {
                val extra = ((available - used) / natural.size).coerceAtMost(CHIP_MAX_STRETCH).value.toInt().dp
                natural.map { it + extra }
            } else {
                natural
            }
        }
        Box(Modifier.fillMaxWidth()) {
            Row(
                modifier = Modifier.fillMaxWidth().horizontalScroll(scroll),
                horizontalArrangement = Arrangement.spacedBy(CHIP_GAP),
                verticalAlignment = Alignment.CenterVertically
            ) {
                chips.forEachIndexed { index, chip ->
                    val selected = chip.id == selectedId
                    val name = names[index]
                    Box(
                        modifier = Modifier
                            .height(30.dp)
                            .width(widths.getOrElse(index) { 64.dp })
                            .clip(RoundedCornerShape(15.dp))
                            .background(if (selected) theme.accent else theme.fieldBackground)
                            .clickable(role = Role.Button) { onSelect(chip.id) }
                            .semantics { contentDescription = name },
                        contentAlignment = Alignment.Center
                    ) {
                        Text(
                            text = name,
                            fontSize = 14.sp,
                            fontWeight = if (selected) FontWeight.SemiBold else FontWeight.Medium,
                            color = if (selected) Color.White else theme.primaryText,
                            maxLines = 1
                        )
                    }
                }
            }
            // Fades the row out under "+" while there is more to scroll to.
            if (scroll.canScrollForward) {
                Box(
                    Modifier
                        .align(Alignment.CenterEnd)
                        .width(24.dp)
                        .height(30.dp)
                        .background(Brush.horizontalGradient(listOf(Color.Transparent, theme.background)))
                )
            }
        }
    }
}

// ------------------------------------------------------------------- composer

/**
 * The AI reply composer.
 *
 * COMPACT BY CONSTRUCTION: header, one field, one row of actions - about
 * 140dp while composing. Heights change on content EVENTS (a new version, a
 * stage change), never on a keystroke: the fields scroll instead of growing,
 * so the keys never move under the user's thumbs. The "field is not empty"
 * question takes the reply's place at the reply's height for the same reason.
 */
@Composable
fun ComposerPanel(
    model: ComposerModel,
    actions: ComposerActions,
    strings: AppStrings,
    theme: KeyboardTheme,
    modifier: Modifier = Modifier
) {
    val session = model.session
    val flow = session.flow
    val stage = flow.stage
    val generating = flow.isGenerating
    val composingLike = stage == ReplyComposerFlow.Stage.Composing ||
        flow.generationOrigin == ReplyComposerFlow.Origin.COMPOSING
    val errorMessage = flow.error?.let { strings.message(it) }?.takeIf { it.isNotEmpty() }

    Column(
        modifier = modifier
            .fillMaxWidth()
            .padding(horizontal = 5.dp, vertical = 3.dp)
            .clip(RoundedCornerShape(12.dp))
            .background(theme.panelBackground)
            .padding(horizontal = 8.dp, vertical = 5.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp)
    ) {
        Header(model, actions, strings, theme)

        if (model.sourceExpanded && !flow.isConflict) {
            SourceCard(model, actions, strings, theme)
        }

        BoxWithConstraints(Modifier.fillMaxWidth()) {
            val fieldHeight = draftFieldHeight(flow, maxWidth, model.maxFieldLines)
            when {
                flow.isConflict -> Box(
                    modifier = Modifier.fillMaxWidth().height(fieldHeight).padding(horizontal = 8.dp),
                    contentAlignment = Alignment.Center
                ) {
                    Text(
                        text = strings[R.string.kb_host_field_not_empty],
                        fontSize = 14.sp,
                        fontWeight = FontWeight.Medium,
                        color = theme.primaryText,
                        textAlign = TextAlign.Center,
                        maxLines = 3
                    )
                }

                flow.showsReply -> KeyboardTextField(
                    state = session.draft,
                    placeholder = strings[R.string.kb_draft_title],
                    textStyle = TextStyle(fontSize = 16.sp, lineHeight = LINE_HEIGHT_SP.sp),
                    textColor = theme.primaryText,
                    placeholderColor = theme.secondaryText,
                    caretColor = theme.accent,
                    isActive = stage == ReplyComposerFlow.Stage.Editing,
                    onTap = { offset -> actions.onFieldTap(PanelFocus.DRAFT, offset) },
                    accessibilityLabel = strings[R.string.kb_draft_title],
                    modifier = Modifier
                        .fillMaxWidth()
                        .height(fieldHeight)
                        .alpha(if (generating) 0.55f else 1f)
                        .fieldFrame(theme, focused = stage == ReplyComposerFlow.Stage.Editing)
                )

                else -> KeyboardTextField(
                    state = session.instruction,
                    placeholder = strings[R.string.kb_ask_placeholder],
                    textStyle = TextStyle(fontSize = 15.5.sp, lineHeight = 20.sp),
                    textColor = theme.primaryText,
                    placeholderColor = theme.secondaryText,
                    caretColor = theme.accent,
                    isActive = stage == ReplyComposerFlow.Stage.Composing && model.focus == PanelFocus.INSTRUCTION,
                    onTap = { offset -> actions.onFieldTap(PanelFocus.INSTRUCTION, offset) },
                    accessibilityLabel = strings[R.string.kb_ask_placeholder],
                    modifier = Modifier
                        .fillMaxWidth()
                        .height(INSTRUCTION_HEIGHT)
                        .alpha(if (generating) 0.6f else 1f)
                        .fieldFrame(
                            theme,
                            focused = stage == ReplyComposerFlow.Stage.Composing && model.focus == PanelFocus.INSTRUCTION
                        )
                )
            }
        }

        if (errorMessage != null && !flow.isConflict) {
            Text(
                text = errorMessage,
                fontSize = 12.sp,
                fontWeight = FontWeight.Medium,
                color = theme.destructive,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.fillMaxWidth().padding(horizontal = 2.dp)
            )
        }

        if (composingLike && !flow.isConflict) VoiceStatusLine(model.voice, strings, theme)

        when {
            flow.isConflict -> ConflictRow(actions.onConflict, strings, theme)
            flow.showsReply -> ResultRow(
                flow = flow,
                draftText = model.session.draft.text,
                strings = strings,
                theme = theme,
                backDescription = strings[R.string.kb_back],
                onBack = actions.onBack,
                onRegenerate = actions.onRegenerate,
                onEdit = actions.onEdit,
                onPreviousVersion = actions.onPreviousVersion,
                onNextVersion = actions.onNextVersion,
                onInsert = actions.onInsert
            )
            else -> ComposingRow(model, actions, strings, theme, errorMessage != null)
        }
    }
}

/**
 * Height of the reply field: the current version's lines, between two and
 * [maxLines], measured when the VERSION changes - not on every keystroke.
 */
@Composable
internal fun draftFieldHeight(flow: ReplyComposerFlow, width: Dp, maxLines: Int, minLines: Int = 2): Dp {
    val measurer = rememberTextMeasurer()
    val density = LocalDensity.current
    val drafts = flow.drafts
    val lines = remember(drafts.index, drafts.count, drafts.current?.generated, width) {
        val innerWidth = with(density) { (width - 16.dp).roundToPx() }.coerceAtLeast(1)
        val text = flow.draftText.ifEmpty { " " }
        measurer.measure(
            text,
            TextStyle(fontSize = 16.sp, lineHeight = LINE_HEIGHT_SP.sp),
            constraints = Constraints(maxWidth = innerWidth)
        ).lineCount
    }
    val shown = lines.coerceIn(minLines, maxLines.coerceAtLeast(minLines))
    return (shown * LINE_HEIGHT_DP + 16).dp
}

internal fun Modifier.fieldFrame(theme: KeyboardTheme, focused: Boolean): Modifier = this
    .clip(RoundedCornerShape(9.dp))
    .background(theme.fieldBackground)
    .border(1.dp, if (focused) theme.accent else theme.fieldBorder, RoundedCornerShape(9.dp))

// --------------------------------------------------------------------- header

@Composable
private fun Header(model: ComposerModel, actions: ComposerActions, strings: AppStrings, theme: KeyboardTheme) {
    val flow = model.session.flow
    val source = model.session.source.text.trim()
    val hasSource = source.isNotEmpty()
    val count = AIReplyService.characterCount(model.session.source.text)
    val overLimit = count > model.sourceLimit

    Row(
        modifier = Modifier.fillMaxWidth().height(30.dp),
        verticalAlignment = Alignment.CenterVertically
    ) {
        Row(
            modifier = Modifier
                .height(28.dp)
                .widthIn(min = 64.dp, max = 150.dp)
                .clip(RoundedCornerShape(14.dp))
                .background(theme.fieldBackground)
                .clickable(enabled = !flow.isGenerating, role = Role.Button, onClick = actions.onPersona)
                .semantics { contentDescription = model.personaName }
                .padding(start = 11.dp, end = 7.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(3.dp)
        ) {
            Text(
                text = model.personaName,
                fontSize = 13.sp,
                fontWeight = FontWeight.SemiBold,
                color = theme.primaryText,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f, fill = false)
            )
            Icon(Icons.Filled.ExpandMore, null, tint = theme.primaryText, modifier = Modifier.size(16.dp))
        }

        Spacer(Modifier.width(6.dp))

        if (!model.sourceExpanded || flow.isConflict) {
            val preview = if (hasSource) source.lines().joinToString(" ") else strings[R.string.kb_paste_message]
            Row(
                modifier = Modifier
                    .weight(1f)
                    .height(26.dp)
                    .clip(RoundedCornerShape(8.dp))
                    .background(theme.quoteBackground)
                    .clickable(enabled = !flow.isGenerating && !flow.isConflict, role = Role.Button) {
                        if (hasSource) actions.onToggleSource() else actions.onPaste()
                    }
                    .semantics {
                        contentDescription = if (hasSource) {
                            "${strings[R.string.kb_copied_message]}: $preview"
                        } else {
                            strings[R.string.kb_paste_message]
                        }
                    }
                    .padding(start = 8.dp, end = 6.dp),
                verticalAlignment = Alignment.CenterVertically
            ) {
                Text(
                    text = preview,
                    fontSize = 13.sp,
                    fontWeight = if (hasSource) FontWeight.Normal else FontWeight.Medium,
                    color = if (hasSource) theme.secondaryText else theme.primaryText,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f)
                )
                Icon(
                    if (hasSource) Icons.Filled.ExpandMore else Icons.Filled.ContentPaste,
                    null,
                    tint = theme.secondaryText,
                    modifier = Modifier.size(14.dp)
                )
            }
        } else {
            Spacer(Modifier.weight(1f))
        }

        if (hasSource) {
            Text(
                text = "$count / ${model.sourceLimit}",
                fontSize = 11.sp,
                fontFamily = FontFamily.Monospace,
                color = if (overLimit) theme.destructive else theme.secondaryText,
                maxLines = 1,
                modifier = Modifier
                    .padding(start = 6.dp)
                    .semantics { contentDescription = strings.get(R.string.kb_character_count, count, model.sourceLimit) }
            )
        }

        Spacer(Modifier.width(6.dp))
        CircleIcon(
            icon = Icons.Filled.Close,
            description = strings[R.string.kb_close],
            theme = theme,
            size = 28.dp,
            iconSize = 15.dp,
            onClick = actions.onClose
        )
    }
}

@Composable
private fun SourceCard(model: ComposerModel, actions: ComposerActions, strings: AppStrings, theme: KeyboardTheme) {
    val flow = model.session.flow
    val composing = flow.stage == ReplyComposerFlow.Stage.Composing
    val lines = if (flow.showsReply) 3 else 4
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .heightIn(max = (lines * 19 + 12).dp)
            .clip(RoundedCornerShape(9.dp))
            .background(theme.quoteBackground)
            .padding(start = 8.dp, top = 4.dp, bottom = 4.dp, end = 2.dp)
    ) {
        Box(
            Modifier
                .padding(vertical = 4.dp)
                .width(3.dp)
                .height((lines * 19).dp)
                .clip(RoundedCornerShape(2.dp))
                .background(theme.accent.copy(alpha = 0.75f))
        )
        KeyboardTextField(
            state = model.session.source,
            placeholder = strings[R.string.kb_err_no_source],
            textStyle = TextStyle(fontSize = 14.sp, lineHeight = 19.sp),
            textColor = theme.primaryText,
            placeholderColor = theme.secondaryText,
            caretColor = theme.accent,
            isActive = composing && model.focus == PanelFocus.SOURCE,
            onTap = { offset -> actions.onFieldTap(PanelFocus.SOURCE, offset) },
            accessibilityLabel = strings[R.string.kb_copied_message],
            contentPadding = 4.dp,
            modifier = Modifier.weight(1f).height((lines * 19 + 8).dp)
        )
        Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
            CircleIcon(Icons.Filled.ExpandLess, strings[R.string.kb_hide_full_message], theme, 26.dp, 16.dp, onClick = actions.onToggleSource)
            if (composing) {
                CircleIcon(Icons.Filled.ContentPaste, strings[R.string.kb_paste_message], theme, 26.dp, 14.dp, onClick = actions.onPaste)
            }
            if (composing && model.session.source.text.isNotBlank()) {
                CircleIcon(
                    Icons.Filled.Cancel, strings[R.string.kb_clear_source], theme, 26.dp, 16.dp,
                    filled = false, onClick = actions.onClearSource
                )
            }
        }
    }
}

// ----------------------------------------------------------------- bottom rows

@Composable
private fun ComposingRow(
    model: ComposerModel,
    actions: ComposerActions,
    strings: AppStrings,
    theme: KeyboardTheme,
    failed: Boolean
) {
    val flow = model.session.flow
    val generating = flow.isGenerating
    val hasSource = model.session.source.text.isNotBlank()
    val overLimit = AIReplyService.characterCount(model.session.source.text) > model.sourceLimit
    val canGenerate = !generating && hasSource && !overLimit

    Row(
        modifier = Modifier.fillMaxWidth().height(ROW_HEIGHT),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(6.dp)
    ) {
        IntentSlot(
            intents = model.intents,
            enabled = flow.stage == ReplyComposerFlow.Stage.Composing,
            assist = model.assist,
            assistActions = actions.assist,
            strings = strings,
            theme = theme,
            onIntent = actions.onIntent,
            modifier = Modifier.weight(1f)
        )
        MicButton(model.voice, strings, theme, enabled = !generating, onClick = actions.onMic)
        ActionPill(
            label = when {
                generating -> strings[R.string.kb_stop]
                failed -> strings[R.string.kb_retry]
                else -> strings[R.string.kb_generate]
            },
            enabled = canGenerate || generating,
            busy = generating,
            theme = theme,
            modifier = Modifier.widthIn(min = 96.dp),
            onClick = actions.onPrimary
        )
    }
}

/** Back, Regenerate, Edit, versions and Insert - for a reply and for Create alike. */
@Composable
internal fun ResultRow(
    flow: ReplyComposerFlow,
    draftText: String,
    strings: AppStrings,
    theme: KeyboardTheme,
    backDescription: String,
    onBack: () -> Unit,
    onRegenerate: () -> Unit,
    onEdit: () -> Unit,
    onPreviousVersion: () -> Unit,
    onNextVersion: () -> Unit,
    onInsert: () -> Unit
) {
    val stage = flow.stage
    val editing = stage == ReplyComposerFlow.Stage.Editing
    val regenerating = flow.generationOrigin == ReplyComposerFlow.Origin.RESULT
    val drafts = flow.drafts
    val canInsert = (stage == ReplyComposerFlow.Stage.Result || editing) && draftText.isNotBlank()

    Row(
        modifier = Modifier.fillMaxWidth().height(ROW_HEIGHT),
        verticalAlignment = Alignment.CenterVertically
    ) {
        CircleIcon(Icons.AutoMirrored.Filled.ArrowBack, backDescription, theme, onClick = onBack)
        Spacer(Modifier.width(6.dp))
        CircleIcon(
            icon = Icons.Filled.Refresh,
            description = if (regenerating) strings[R.string.kb_stop] else strings[R.string.kb_regenerate],
            theme = theme,
            busy = regenerating,
            enabled = stage == ReplyComposerFlow.Stage.Result || editing || regenerating,
            onClick = onRegenerate
        )
        Spacer(Modifier.width(6.dp))
        CircleIcon(
            icon = if (editing) Icons.Filled.Check else Icons.Filled.Edit,
            description = if (editing) strings[R.string.kb_done_editing] else strings[R.string.kb_edit_reply],
            theme = theme,
            selected = editing,
            enabled = stage == ReplyComposerFlow.Stage.Result || editing,
            onClick = onEdit
        )

        Box(Modifier.weight(1f), contentAlignment = Alignment.Center) {
            if (drafts.count > 1) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    CircleIcon(
                        Icons.Filled.ChevronLeft, strings[R.string.kb_previous_version], theme, 28.dp, 20.dp,
                        filled = false, enabled = drafts.canSelectPrevious && !flow.isGenerating,
                        onClick = onPreviousVersion
                    )
                    Text(
                        text = "${drafts.position}/${drafts.count}",
                        fontSize = 12.sp,
                        fontWeight = FontWeight.SemiBold,
                        color = theme.secondaryText,
                        modifier = Modifier.semantics {
                            contentDescription = strings.get(R.string.kb_version_position, drafts.position, drafts.count)
                        }
                    )
                    CircleIcon(
                        Icons.Filled.ChevronRight, strings[R.string.kb_next_version], theme, 28.dp, 20.dp,
                        filled = false, enabled = drafts.canSelectNext && !flow.isGenerating,
                        onClick = onNextVersion
                    )
                }
            }
        }

        ActionPill(
            label = strings[R.string.kb_insert],
            enabled = canInsert,
            theme = theme,
            modifier = Modifier.widthIn(min = 96.dp),
            onClick = onInsert
        )
    }
}

@Composable
internal fun ConflictRow(
    onConflict: (ReplyComposerFlow.ConflictChoice) -> Unit,
    strings: AppStrings,
    theme: KeyboardTheme
) {
    Row(
        modifier = Modifier.fillMaxWidth().height(ROW_HEIGHT),
        horizontalArrangement = Arrangement.spacedBy(6.dp),
        verticalAlignment = Alignment.CenterVertically
    ) {
        ActionPill(strings[R.string.kb_replace_existing], true, theme, Modifier.weight(1f)) {
            onConflict(ReplyComposerFlow.ConflictChoice.REPLACE)
        }
        ActionPill(strings[R.string.kb_append_existing], true, theme, Modifier.weight(1f)) {
            onConflict(ReplyComposerFlow.ConflictChoice.APPEND)
        }
        ActionPill(strings[R.string.kb_keep_typing], true, theme, Modifier.weight(1f), quiet = true) {
            onConflict(ReplyComposerFlow.ConflictChoice.CANCEL)
        }
    }
}

// -------------------------------------------------------------------- intents

/**
 * The one-tap intents, laid out so that a pill is either FULLY visible or not
 * on the row at all: nothing is clipped, nothing scrolls out of sight. What
 * does not fit is one tap on "…" away - it shows the next ones.
 */
@Composable
internal fun IntentRow(
    intents: List<QuickIntent>,
    enabled: Boolean,
    theme: KeyboardTheme,
    moreLabel: String,
    onIntent: (QuickIntent) -> Unit,
    modifier: Modifier = Modifier
) {
    if (intents.isEmpty()) {
        Spacer(modifier)
        return
    }
    var start by remember(intents) { mutableIntStateOf(0) }
    val pager = remember { IntentPager() }
    val ordered = intents.indices.map { intents[(it + start) % intents.size] }

    Layout(
        modifier = modifier.alpha(if (enabled) 1f else 0.45f),
        content = {
            ordered.forEach { intent ->
                Box(
                    modifier = Modifier
                        .height(28.dp)
                        .clip(RoundedCornerShape(14.dp))
                        .background(theme.fieldBackground)
                        .clickable(enabled = enabled, role = Role.Button) { onIntent(intent) }
                        .padding(horizontal = 11.dp),
                    contentAlignment = Alignment.Center
                ) {
                    Text(intent.label, fontSize = 12.5.sp, fontWeight = FontWeight.Medium, color = theme.primaryText, maxLines = 1)
                }
            }
            CircleIcon(
                Icons.Filled.MoreHoriz, moreLabel, theme, 28.dp, 16.dp, enabled = enabled
            ) {
                start = (start + pager.shown.coerceAtLeast(1)) % intents.size
            }
        }
    ) { measurables, constraints ->
        val gap = 6.dp.roundToPx()
        val loose = Constraints(maxWidth = constraints.maxWidth, maxHeight = constraints.maxHeight)
        val placeables = measurables.map { it.measure(loose) }
        val more = placeables.last()
        val pills = placeables.dropLast(1)
        val width = constraints.maxWidth

        var used = 0
        var shown = 0
        for ((index, pill) in pills.withIndex()) {
            val remaining = pills.size - index - 1
            val need = (if (shown > 0) gap else 0) + pill.width
            val reserve = if (remaining > 0) gap + more.width else 0
            if (used + need + reserve > width) break
            used += need
            shown++
        }
        pager.shown = shown
        val showsMore = shown < pills.size
        val height = placeables.maxOf { it.height }.coerceAtMost(constraints.maxHeight)
        layout(width, height) {
            var x = 0
            pills.take(shown).forEach { pill ->
                pill.placeRelative(x, (height - pill.height) / 2)
                x += pill.width + gap
            }
            if (showsMore) more.placeRelative(x, (height - more.height) / 2)
        }
    }
}

/** How many intents the last layout fitted, for "…" to page past them. */
private class IntentPager {
    var shown = 0
}

// -------------------------------------------------------------------- pieces

@Composable
internal fun ActionPill(
    label: String,
    enabled: Boolean,
    theme: KeyboardTheme,
    modifier: Modifier = Modifier,
    quiet: Boolean = false,
    busy: Boolean = false,
    icon: ImageVector? = null,
    onClick: () -> Unit
) {
    val background = when {
        quiet -> theme.fieldBackground
        enabled -> theme.accent
        else -> theme.accent.copy(alpha = 0.35f)
    }
    Row(
        modifier = modifier
            .height(32.dp)
            .clip(RoundedCornerShape(16.dp))
            .background(background)
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick)
            .padding(horizontal = 14.dp),
        horizontalArrangement = Arrangement.Center,
        verticalAlignment = Alignment.CenterVertically
    ) {
        if (busy) {
            CircularProgressIndicator(
                modifier = Modifier.size(14.dp),
                strokeWidth = 2.dp,
                color = Color.White
            )
            Spacer(Modifier.width(6.dp))
        } else if (icon != null) {
            Icon(icon, contentDescription = null, tint = if (quiet) theme.primaryText else Color.White, modifier = Modifier.size(15.dp))
            Spacer(Modifier.width(5.dp))
        }
        Text(
            text = label,
            fontSize = 15.sp,
            fontWeight = FontWeight.SemiBold,
            color = if (quiet) theme.primaryText else Color.White,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis
        )
    }
}

/**
 * A round icon button. Filled ones sit on a disc; [selected] is an accent
 * disc with a white glyph (Edit while editing); [busy] shows a spinner.
 */
@Composable
internal fun CircleIcon(
    icon: ImageVector,
    description: String,
    theme: KeyboardTheme,
    size: Dp = 34.dp,
    iconSize: Dp = 18.dp,
    filled: Boolean = true,
    selected: Boolean = false,
    busy: Boolean = false,
    enabled: Boolean = true,
    tint: Color? = null,
    onClick: () -> Unit
) {
    val background = when {
        selected -> theme.accent
        filled -> theme.fieldBackground
        else -> Color.Transparent
    }
    val glyph = if (selected) Color.White else tint ?: theme.primaryText
    Box(
        modifier = Modifier
            .size(size)
            .alpha(if (enabled) 1f else 0.35f)
            .clip(CircleShape)
            .background(background)
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick)
            .semantics { contentDescription = description },
        contentAlignment = Alignment.Center
    ) {
        if (busy) {
            CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp, color = glyph)
        } else {
            Icon(icon, contentDescription = null, tint = glyph, modifier = Modifier.size(iconSize))
        }
    }
}

@Composable
private fun MicButton(
    voice: VoiceState,
    strings: AppStrings,
    theme: KeyboardTheme,
    enabled: Boolean,
    onClick: () -> Unit
) {
    val listening = voice is VoiceState.Listening || voice is VoiceState.Starting
    CircleIcon(
        icon = if (listening) Icons.Filled.Stop else Icons.Filled.Mic,
        description = if (listening) strings[R.string.voice_kb_stop] else strings[R.string.voice_kb_start],
        theme = theme,
        size = 32.dp,
        iconSize = 18.dp,
        selected = listening,
        busy = voice is VoiceState.Processing,
        enabled = enabled,
        onClick = onClick
    )
}

/**
 * One line under the instruction while the microphone is doing anything: the
 * user must never be unsure whether they are being recorded.
 */
@Composable
private fun VoiceStatusLine(voice: VoiceState, strings: AppStrings, theme: KeyboardTheme) {
    val message = when (voice) {
        is VoiceState.Starting -> strings[R.string.voice_kb_listening]
        is VoiceState.Listening -> voice.partial.ifBlank { strings[R.string.voice_kb_listening] }
        is VoiceState.Processing -> strings[R.string.voice_kb_processing]
        is VoiceState.PermissionRequired -> strings[R.string.voice_kb_permission_needed]
        is VoiceState.PermissionDenied -> strings[R.string.voice_kb_permission_denied]
        is VoiceState.Failed -> when (voice.reason) {
            VoiceFailure.NO_SPEECH -> strings[R.string.voice_kb_no_speech]
            VoiceFailure.NETWORK -> strings[R.string.kb_err_offline]
            VoiceFailure.LANGUAGE_UNAVAILABLE, VoiceFailure.UNAVAILABLE -> strings[R.string.voice_kb_unavailable]
            VoiceFailure.GENERIC -> strings[R.string.voice_kb_failed]
        }
        else -> null
    } ?: return

    Text(
        text = message,
        fontSize = 11.5.sp,
        color = if (voice is VoiceState.Listening) theme.primaryText else theme.secondaryText,
        maxLines = 2,
        overflow = TextOverflow.Ellipsis,
        modifier = Modifier.fillMaxWidth()
    )
}

val PERSONA_ROW_HEIGHT = 44.dp
internal val ROW_HEIGHT = 34.dp
private val INSTRUCTION_HEIGHT = 56.dp
internal const val LINE_HEIGHT_SP = 21
private const val LINE_HEIGHT_DP = 21
private val CHIP_GAP = 6.dp
private val CHIP_PADDINGS = listOf(14.dp, 11.dp, 8.dp)
private val CHIP_MAX_STRETCH = 28.dp
private const val SUGGESTIONS_FADE_MS = 120
