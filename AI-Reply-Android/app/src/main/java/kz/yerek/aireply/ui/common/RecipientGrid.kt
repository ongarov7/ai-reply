package kz.yerek.aireply.ui.common

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import kz.yerek.aireply.ui.design.LocalExtraColors
import kz.yerek.aireply.ui.design.Radius
import kz.yerek.aireply.ui.design.Spacing

/** One choice of «Кому отвечаем?»: a template id, its «to whom» name, its icon (none for a custom one). */
data class RecipientOption(val id: String, val label: String, val icon: ImageVector?)

/**
 * «Кому отвечаем?» as a two-column grid of cards: Другу, Клиенту, Коллеге,
 * Партнёру with their icons, then the user's own templates by name.
 *
 * Алушы торы: екі бағанды, белгішесі мен барыс септігіндегі атауы бар.
 *
 * A single-choice group for screen readers. The selected card is outlined in
 * the accent and tinted, with a check; nothing moves when it changes.
 */
@Composable
fun RecipientGrid(
    options: List<RecipientOption>,
    selectedId: String?,
    onSelect: (String) -> Unit,
    modifier: Modifier = Modifier
) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .selectableGroup(),
        verticalArrangement = Arrangement.spacedBy(Spacing.s)
    ) {
        options.chunked(2).forEach { pair ->
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .height(IntrinsicSize.Min),
                horizontalArrangement = Arrangement.spacedBy(Spacing.s)
            ) {
                pair.forEach { option ->
                    RecipientCard(
                        option = option,
                        selected = option.id == selectedId,
                        onSelect = { onSelect(option.id) },
                        modifier = Modifier
                            .weight(1f)
                            .fillMaxHeight()
                    )
                }
                if (pair.size == 1) Spacer(Modifier.weight(1f))
            }
        }
    }
}

@Composable
private fun RecipientCard(option: RecipientOption, selected: Boolean, onSelect: () -> Unit, modifier: Modifier) {
    val shape = RoundedCornerShape(Radius.medium)
    val accent = MaterialTheme.colorScheme.primary
    Box(
        modifier = modifier
            .clip(shape)
            .background(if (selected) MaterialTheme.colorScheme.primaryContainer else MaterialTheme.colorScheme.surface)
            .border(if (selected) 1.5.dp else 0.5.dp, if (selected) accent else LocalExtraColors.current.separator, shape)
            .selectable(selected = selected, role = Role.RadioButton, onClick = onSelect)
            .defaultMinSize(minHeight = 64.dp)
            .padding(horizontal = Spacing.s, vertical = Spacing.s)
    ) {
        Column(
            modifier = Modifier.align(Alignment.CenterStart).padding(end = Spacing.l),
            verticalArrangement = Arrangement.spacedBy(Spacing.xxs)
        ) {
            option.icon?.let { icon ->
                Icon(icon, contentDescription = null, tint = accent, modifier = Modifier.size(22.dp))
            }
            Text(option.label, style = MaterialTheme.typography.titleMedium, color = MaterialTheme.colorScheme.onSurface)
        }
        if (selected) {
            Icon(
                Icons.Filled.CheckCircle,
                contentDescription = null,
                tint = accent,
                modifier = Modifier.align(Alignment.TopEnd).size(18.dp)
            )
        }
    }
}
