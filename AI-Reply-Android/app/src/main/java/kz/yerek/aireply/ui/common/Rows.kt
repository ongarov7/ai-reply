package kz.yerek.aireply.ui.common

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.ErrorOutline
import androidx.compose.material.icons.filled.HelpOutline
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.dp
import kz.yerek.aireply.ui.design.LocalExtraColors
import kz.yerek.aireply.ui.design.Radius
import kz.yerek.aireply.ui.design.Spacing

/** A tappable settings row: icon, label, chevron. */
@Composable
fun NavigationRow(
    icon: ImageVector,
    title: String,
    modifier: Modifier = Modifier,
    /** Optional trailing detail, shown quietly before the chevron. */
    value: String? = null,
    /** An action that cannot be undone (Delete account): icon and label in the error colour. */
    destructive: Boolean = false,
    enabled: Boolean = true,
    onClick: () -> Unit
) {
    val accent = if (destructive) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.primary
    Row(
        modifier = modifier
            .fillMaxWidth()
            .clickable(enabled = enabled, onClick = onClick)
            .defaultMinSize(minHeight = 48.dp)
            .padding(horizontal = Spacing.m, vertical = Spacing.s),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(Spacing.s)
    ) {
        Icon(
            icon,
            contentDescription = null,
            tint = accent,
            modifier = Modifier.size(24.dp)
        )
        // Takes the room left by the value and the chevron, so a long label
        // wraps instead of pushing them off the row.
        Text(
            title,
            style = MaterialTheme.typography.bodyLarge,
            color = if (destructive) accent else Color.Unspecified,
            modifier = Modifier.weight(1f)
        )
        if (!value.isNullOrEmpty()) {
            Text(
                value,
                style = MaterialTheme.typography.bodyMedium,
                color = LocalExtraColors.current.textSecondary
            )
        }
        Icon(
            Icons.AutoMirrored.Filled.KeyboardArrowRight,
            contentDescription = null,
            tint = LocalExtraColors.current.textTertiary,
            modifier = Modifier.size(20.dp)
        )
    }
}

/** A group of [NavigationRow]s on one card. */
@Composable
fun RowGroup(modifier: Modifier = Modifier, content: @Composable () -> Unit) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(Radius.large))
            .background(MaterialTheme.colorScheme.surface)
            .padding(vertical = Spacing.xxs)
    ) { content() }
}

/**
 * One line of a status checklist.
 *
 * iOS needs a third `UNKNOWN` state because the platform will not say whether
 * its own keyboard is enabled. Android will, so [ChecklistState] has two
 * meaningful states and `UNKNOWN` exists only for a value that has not been read
 * yet.
 */
enum class ChecklistState { DONE, MISSING, UNKNOWN }

@Composable
fun ChecklistRow(
    title: String,
    state: ChecklistState,
    statusLabel: String,
    modifier: Modifier = Modifier
) {
    val extras = LocalExtraColors.current
    val (icon, tint) = when (state) {
        ChecklistState.DONE -> Icons.Filled.CheckCircle to extras.success
        ChecklistState.MISSING -> Icons.Filled.ErrorOutline to extras.warning
        ChecklistState.UNKNOWN -> Icons.Filled.HelpOutline to extras.textSecondary
    }

    Row(
        modifier = modifier.fillMaxWidth(),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(Spacing.s)
    ) {
        Icon(icon, contentDescription = null, tint = tint, modifier = Modifier.size(22.dp))
        // The title wraps at large text sizes; the status beside it never gets squeezed out.
        Text(title, style = MaterialTheme.typography.bodyLarge, modifier = Modifier.weight(1f))
        Text(
            statusLabel,
            style = MaterialTheme.typography.bodySmall,
            color = extras.textSecondary
        )
    }
}

@Composable
fun Footnote(text: String, modifier: Modifier = Modifier) {
    Text(
        text = text,
        style = MaterialTheme.typography.bodySmall,
        color = LocalExtraColors.current.textSecondary,
        modifier = modifier.fillMaxWidth()
    )
}

@Composable
fun FieldLabel(text: String, modifier: Modifier = Modifier) {
    Text(
        text = text,
        style = MaterialTheme.typography.titleSmall,
        color = LocalExtraColors.current.textSecondary,
        modifier = modifier.padding(top = Spacing.xs)
    )
}

/** Divider aligned to the text of a [NavigationRow], not to the card's edge. */
@Composable
fun RowDividerIndented(modifier: Modifier = Modifier) {
    kz.yerek.aireply.ui.design.RowDivider(modifier = modifier, startIndent = 52.dp)
}
