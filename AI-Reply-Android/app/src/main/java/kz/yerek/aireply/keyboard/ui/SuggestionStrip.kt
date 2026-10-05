package kz.yerek.aireply.keyboard.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.Undo
import androidx.compose.material.icons.filled.AutoAwesome
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kz.yerek.aireply.R
import kz.yerek.aireply.ai.AppStrings
import kz.yerek.aireply.keyboard.KeyboardTheme
import kz.yerek.aireply.keyboard.autocorrect.AutocorrectEngine
import kz.yerek.aireply.keyboard.autocorrect.Suggestion

/**
 * What the panel's action row shows instead of the intents while the user
 * writes: the word suggestions, a cleaner version of the instruction, or Undo
 * for one just taken.
 */
class TypingAssist(
    val suggestions: List<Suggestion> = emptyList(),
    /** A polished version of the instruction, offered on a chip. */
    val polished: String? = null,
    /** A polished version was just taken: Undo shows. */
    val canUndoPolish: Boolean = false
) {
    companion object {
        val NONE = TypingAssist()
    }
}

class TypingAssistActions(
    val onPick: (Suggestion) -> Unit,
    val onAcceptPolish: () -> Unit,
    val onUndoPolish: () -> Unit
)

/**
 * The suggestion strip, in the system keyboards' manner: three equal slots
 * divided by hairlines, plain text on the keyboard's own background. A pending
 * correction sits in the middle in bold, what was typed to its left in quotes
 * (tap: keep it). Slots never move while typing, so the eye can rest on the
 * middle one.
 */
@Composable
internal fun SuggestionStrip(
    suggestions: List<Suggestion>,
    theme: KeyboardTheme,
    strings: AppStrings,
    onPick: (Suggestion) -> Unit,
    modifier: Modifier = Modifier
) {
    Row(
        modifier = modifier.semantics { contentDescription = strings[R.string.kb_suggestions] },
        verticalAlignment = Alignment.CenterVertically
    ) {
        repeat(AutocorrectEngine.SUGGESTION_COUNT) { index ->
            if (index > 0) {
                Box(Modifier.width(1.dp).height(18.dp).background(theme.divider))
            }
            val suggestion = suggestions.getOrNull(index)
            if (suggestion == null) {
                Spacer(Modifier.weight(1f))
            } else {
                SuggestionSlot(suggestion, theme, strings, onPick, Modifier.weight(1f))
            }
        }
    }
}

@Composable
private fun SuggestionSlot(
    suggestion: Suggestion,
    theme: KeyboardTheme,
    strings: AppStrings,
    onPick: (Suggestion) -> Unit,
    modifier: Modifier
) {
    val (label, description) = when (suggestion.kind) {
        Suggestion.Kind.TYPED -> strings.get(R.string.kb_suggestion_typed, suggestion.text) to
            strings.get(R.string.kb_suggestion_keep_description, suggestion.text)
        Suggestion.Kind.CORRECTION -> suggestion.text to
            strings.get(R.string.kb_suggestion_correction_description, suggestion.text)
        else -> suggestion.text to suggestion.text
    }
    Box(
        modifier = modifier
            .fillMaxHeight()
            .clip(RoundedCornerShape(8.dp))
            .clickable(role = Role.Button) { onPick(suggestion) }
            .semantics { contentDescription = description }
            .padding(horizontal = 4.dp),
        contentAlignment = Alignment.Center
    ) {
        Text(
            text = label,
            fontSize = 16.sp,
            fontWeight = if (suggestion.kind == Suggestion.Kind.CORRECTION) FontWeight.SemiBold else FontWeight.Normal,
            color = theme.primaryText,
            textAlign = TextAlign.Center,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis
        )
    }
}

/**
 * The action row's slot while an instruction is being written: the polish
 * chip or its Undo first (they answer a pause, so they are the newest thing
 * on screen), then the word suggestions, otherwise the intents.
 */
@Composable
internal fun IntentSlot(
    intents: List<QuickIntent>,
    enabled: Boolean,
    assist: TypingAssist,
    assistActions: TypingAssistActions,
    strings: AppStrings,
    theme: KeyboardTheme,
    onIntent: (QuickIntent) -> Unit,
    modifier: Modifier = Modifier
) {
    val polished = assist.polished
    when {
        enabled && polished != null -> PolishChip(polished, strings, theme, assistActions.onAcceptPolish, modifier)
        enabled && assist.canUndoPolish -> Row(modifier, verticalAlignment = Alignment.CenterVertically) {
            AssistPill(
                text = strings[R.string.kb_polish_undo],
                description = strings[R.string.kb_polish_undo_description],
                icon = { Icon(Icons.AutoMirrored.Filled.Undo, null, tint = theme.primaryText, modifier = Modifier.size(15.dp)) },
                theme = theme,
                onClick = assistActions.onUndoPolish
            )
        }
        enabled && assist.suggestions.isNotEmpty() ->
            SuggestionStrip(assist.suggestions, theme, strings, assistActions.onPick, modifier.fillMaxHeight())
        else -> IntentRow(
            intents = intents,
            enabled = enabled,
            theme = theme,
            moreLabel = strings[R.string.kb_more_actions],
            onIntent = onIntent,
            modifier = modifier
        )
    }
}

/** "✨ <the polished instruction>": one tap puts it in the field. */
@Composable
private fun PolishChip(
    polished: String,
    strings: AppStrings,
    theme: KeyboardTheme,
    onClick: () -> Unit,
    modifier: Modifier
) {
    Row(modifier, verticalAlignment = Alignment.CenterVertically) {
        AssistPill(
            text = polished.replace('\n', ' '),
            description = strings.get(R.string.kb_polish_suggestion_description, polished),
            icon = { Icon(Icons.Filled.AutoAwesome, null, tint = theme.accent, modifier = Modifier.size(15.dp)) },
            theme = theme,
            onClick = onClick,
            modifier = Modifier.weight(1f, fill = false)
        )
    }
}

/** A pill like the intents', with a leading icon. Dimmed like them when disabled. */
@Composable
internal fun AssistPill(
    text: String,
    description: String,
    icon: @Composable () -> Unit,
    theme: KeyboardTheme,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true
) {
    Row(
        modifier = modifier
            .height(28.dp)
            .alpha(if (enabled) 1f else 0.45f)
            .clip(RoundedCornerShape(14.dp))
            .background(theme.fieldBackground)
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick)
            .semantics { contentDescription = description }
            .padding(start = 9.dp, end = 11.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(5.dp)
    ) {
        icon()
        Text(
            text = text,
            fontSize = 12.5.sp,
            fontWeight = FontWeight.Medium,
            color = theme.primaryText,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis
        )
    }
}
