package kz.yerek.aireply.ui.feature.onboarding

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kz.yerek.aireply.R
import kz.yerek.aireply.core.lang.TemplateNaming
import kz.yerek.aireply.domain.model.RelationshipKind
import kz.yerek.aireply.ui.design.LocalExtraColors
import kz.yerek.aireply.ui.design.Radius
import kz.yerek.aireply.ui.design.Spacing

/*
 * Small drawings of a messenger and of the AI Reply keyboard, for the
 * onboarding. Built from the app's colour tokens, so they follow the dark
 * theme, and from the app's own strings, so they follow the language. They
 * read no clipboard and send nothing: every tap in them is local.
 */

/** A phone screen: a chat on top, a keyboard under it. */
@Composable
internal fun MockPhone(modifier: Modifier = Modifier, content: @Composable ColumnScope.() -> Unit) {
    val shape = RoundedCornerShape(Radius.large)
    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(shape)
            .border(0.5.dp, LocalExtraColors.current.separator, shape)
            .background(MaterialTheme.colorScheme.surface),
        content = content
    )
}

/**
 * The other person's message and the message field under it.
 *
 * @param belowMessage what the messenger shows under the message: a Copy
 *   action, a "Copied" note.
 * @param field the text in the user's own message field; the placeholder
 *   when null.
 */
@Composable
internal fun MockChat(
    incoming: String,
    field: String? = null,
    belowMessage: @Composable (() -> Unit)? = null,
    fieldAction: @Composable (RowScope.() -> Unit)? = null
) {
    val extras = LocalExtraColors.current
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(Spacing.s),
        verticalArrangement = Arrangement.spacedBy(Spacing.xs)
    ) {
        Box(modifier = Modifier.fillMaxWidth(0.85f)) {
            Text(
                incoming,
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onBackground,
                modifier = Modifier
                    .clip(RoundedCornerShape(Radius.large))
                    .background(MaterialTheme.colorScheme.background)
                    .padding(horizontal = Spacing.s, vertical = Spacing.xs)
            )
        }
        belowMessage?.invoke()
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(top = Spacing.xs)
                .clip(RoundedCornerShape(Radius.large))
                .background(MaterialTheme.colorScheme.background)
                .padding(start = Spacing.s, end = Spacing.xxs),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(Spacing.xs)
        ) {
            Text(
                field ?: stringResource(R.string.onboarding_mock_field),
                style = MaterialTheme.typography.bodyMedium,
                color = if (field == null) extras.textSecondary else MaterialTheme.colorScheme.onBackground,
                modifier = Modifier
                    .weight(1f)
                    .padding(vertical = Spacing.xs)
            )
            fieldAction?.invoke(this)
        }
    }
}

/**
 * The keyboard: the persona row (or, once a persona is tapped, the reply
 * panel in its place, as on the real keyboard) above a block of keys.
 */
@Composable
internal fun MockKeyboard(
    showsPersonas: Boolean,
    selected: RelationshipKind? = null,
    onPersona: ((RelationshipKind) -> Unit)? = null,
    panel: @Composable (ColumnScope.() -> Unit)? = null
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(MaterialTheme.colorScheme.background)
            .padding(Spacing.xs),
        verticalArrangement = Arrangement.spacedBy(Spacing.xs)
    ) {
        when {
            panel != null -> Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(Radius.medium))
                    .background(MaterialTheme.colorScheme.surface)
                    .padding(Spacing.s),
                verticalArrangement = Arrangement.spacedBy(Spacing.xs),
                content = panel
            )
            showsPersonas -> PersonaRow(selected, onPersona)
        }
        MockKeys()
    }
}

/** The reply panel: the copied message, then the instruction or the reply. */
@Composable
internal fun MockComposer(
    source: String,
    instruction: String? = null,
    draft: String? = null,
    action: @Composable RowScope.() -> Unit
) {
    val extras = LocalExtraColors.current
    Text(
        stringResource(R.string.kb_source_title) + ": " + source,
        style = MaterialTheme.typography.labelMedium,
        color = extras.textSecondary,
        maxLines = 1,
        overflow = TextOverflow.Ellipsis
    )
    when {
        draft != null -> {
            Text(
                stringResource(R.string.kb_draft_title),
                style = MaterialTheme.typography.labelMedium,
                color = extras.textSecondary
            )
            Text(draft, style = MaterialTheme.typography.bodyMedium)
        }
        instruction != null -> Text(instruction, style = MaterialTheme.typography.bodyMedium)
        else -> Text(
            stringResource(R.string.kb_ask_placeholder),
            style = MaterialTheme.typography.bodyMedium,
            color = extras.textSecondary
        )
    }
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.End,
        verticalAlignment = Alignment.CenterVertically,
        content = action
    )
}

/**
 * A pill the size the real keyboard draws it. With [onClick] it is a real
 * button with a full 48 dp touch target; without, it only shows where to tap.
 */
@Composable
internal fun MockAction(
    text: String,
    modifier: Modifier = Modifier,
    icon: ImageVector? = null,
    onClick: (() -> Unit)? = null
) {
    val pill = Modifier
        .clip(CircleShape)
        .background(MaterialTheme.colorScheme.primary)
    Row(
        modifier = modifier
            .then(if (onClick != null) Modifier.minimumInteractiveComponentSize() else Modifier)
            .then(pill)
            .then(if (onClick != null) Modifier.clickable(role = Role.Button, onClick = onClick) else Modifier)
            .padding(horizontal = Spacing.s, vertical = Spacing.xxs + 2.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(Spacing.xxs)
    ) {
        if (icon != null) {
            Icon(
                icon,
                contentDescription = null,
                tint = MaterialTheme.colorScheme.onPrimary,
                modifier = Modifier.size(16.dp)
            )
        }
        Text(text, style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onPrimary)
    }
}

@Composable
private fun PersonaRow(selected: RelationshipKind?, onPersona: ((RelationshipKind) -> Unit)?) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .horizontalScroll(rememberScrollState()),
        horizontalArrangement = Arrangement.spacedBy(Spacing.xs)
    ) {
        RelationshipKind.builtIns.forEach { kind ->
            val isSelected = kind == selected
            val chip = Modifier
                .clip(CircleShape)
                .background(
                    if (isSelected) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.surface
                )
            Box(
                modifier = Modifier
                    .then(if (onPersona != null) Modifier.minimumInteractiveComponentSize() else Modifier)
                    .then(chip)
                    .then(
                        if (onPersona != null) {
                            Modifier.clickable(role = Role.Button) { onPersona(kind) }
                        } else {
                            Modifier
                        }
                    )
                    .padding(horizontal = Spacing.s, vertical = Spacing.xxs + 2.dp)
            ) {
                Text(
                    stringResource(TemplateNaming.resource(kind)),
                    style = MaterialTheme.typography.labelLarge,
                    color = if (isSelected) MaterialTheme.colorScheme.onPrimary else MaterialTheme.colorScheme.onSurface
                )
            }
        }
    }
}

/** Three rows of key caps. Decoration only, so screen readers skip it. */
@Composable
private fun MockKeys() {
    val key = MaterialTheme.colorScheme.surface
    Canvas(
        modifier = Modifier
            .fillMaxWidth()
            .height(KEYS_HEIGHT)
            .clearAndSetSemantics { }
    ) {
        val gap = 5.dp.toPx()
        val rowHeight = (size.height - gap * (KEY_ROWS.size - 1)) / KEY_ROWS.size
        val keyWidth = (size.width - gap * (KEY_ROWS.first() - 1)) / KEY_ROWS.first()
        KEY_ROWS.forEachIndexed { row, count ->
            val rowWidth = count * keyWidth + (count - 1) * gap
            val start = (size.width - rowWidth) / 2
            repeat(count) { index ->
                drawRoundRect(
                    color = key,
                    topLeft = Offset(start + index * (keyWidth + gap), row * (rowHeight + gap)),
                    size = Size(keyWidth, rowHeight),
                    cornerRadius = CornerRadius(4.dp.toPx())
                )
            }
        }
    }
}

private val KEY_ROWS = listOf(10, 9, 7)
private val KEYS_HEIGHT = 84.dp
