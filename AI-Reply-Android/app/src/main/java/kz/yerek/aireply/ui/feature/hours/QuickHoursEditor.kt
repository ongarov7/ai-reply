package kz.yerek.aireply.ui.feature.hours

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TimePicker
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.material3.rememberTimePickerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import kz.yerek.aireply.R
import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.domain.model.TimeOfDay
import kz.yerek.aireply.domain.model.WorkingHours
import kz.yerek.aireply.ui.design.Layout
import kz.yerek.aireply.ui.design.LocalExtraColors
import kz.yerek.aireply.ui.design.Radius
import kz.yerek.aireply.ui.design.Spacing
import java.time.format.TextStyle

/**
 * Start and end time plus a weekday row, for the common case. The full per-day
 * editor lives in Settings > Working hours.
 */
@Composable
fun QuickHoursEditor(
    hours: WorkingHours,
    language: AppLanguage,
    onChange: (WorkingHours) -> Unit,
    modifier: Modifier = Modifier
) {
    var editing by remember { mutableStateOf<QuickTime?>(null) }
    val from = stringResource(R.string.hours_from)
    val to = stringResource(R.string.hours_to)

    Column(modifier = modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(Spacing.s)) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(Spacing.xs)
        ) {
            Text(
                from,
                style = MaterialTheme.typography.bodyLarge,
                color = LocalExtraColors.current.textSecondary,
                modifier = Modifier.weight(1f)
            )
            TimeChip(hours.quickStart.formatted, description = from) { editing = QuickTime.START }
            Text(
                to,
                style = MaterialTheme.typography.bodyLarge,
                color = LocalExtraColors.current.textSecondary
            )
            TimeChip(hours.quickEnd.formatted, description = to) { editing = QuickTime.END }
        }

        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(Spacing.xxs)
        ) {
            WorkingHours.MONDAY_FIRST.forEach { weekday ->
                WeekdayToggle(
                    symbol = weekdaySymbol(weekday, language),
                    description = weekdayFullName(weekday, language),
                    isOpen = hours.schedule(weekday)?.isEnabled == true,
                    onToggle = { onChange(hours.togglingDay(weekday)) },
                    modifier = Modifier.weight(1f)
                )
            }
        }
    }

    editing?.let { target ->
        TimeOfDayDialog(
            initial = if (target == QuickTime.START) hours.quickStart else hours.quickEnd,
            onDismiss = { editing = null },
            onConfirm = { value ->
                onChange(if (target == QuickTime.START) hours.withQuickStart(value) else hours.withQuickEnd(value))
                editing = null
            }
        )
    }
}

private enum class QuickTime { START, END }

@Composable
private fun WeekdayToggle(
    symbol: String,
    description: String,
    isOpen: Boolean,
    onToggle: () -> Unit,
    modifier: Modifier = Modifier
) {
    Box(
        modifier = modifier
            .defaultMinSize(minHeight = Layout.minimumTouchTarget)
            .clip(RoundedCornerShape(Radius.small))
            .background(if (isOpen) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.background)
            .toggleable(value = isOpen, role = Role.Checkbox, onValueChange = { onToggle() })
            .semantics { contentDescription = description },
        contentAlignment = Alignment.Center
    ) {
        Text(
            symbol,
            style = MaterialTheme.typography.bodySmall,
            fontWeight = FontWeight.Medium,
            color = if (isOpen) MaterialTheme.colorScheme.onPrimary else MaterialTheme.colorScheme.onSurface
        )
    }
}

/** A tappable time in the accent colour, shared by both hours editors. */
@Composable
internal fun TimeChip(label: String, description: String? = null, onClick: () -> Unit) {
    Text(
        text = label,
        style = MaterialTheme.typography.bodyLarge,
        color = MaterialTheme.colorScheme.primary,
        modifier = Modifier
            .minimumInteractiveComponentSize()
            .then(
                if (description == null) {
                    Modifier
                } else {
                    Modifier.semantics { contentDescription = "$description $label" }
                }
            )
            .clickable(onClick = onClick)
            .padding(horizontal = Spacing.xs, vertical = Spacing.xxs)
    )
}

/** The 24-hour picker behind a [TimeChip]. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun TimeOfDayDialog(
    initial: TimeOfDay,
    onDismiss: () -> Unit,
    onConfirm: (TimeOfDay) -> Unit
) {
    val picker = rememberTimePickerState(
        initialHour = initial.hour,
        initialMinute = initial.minute,
        is24Hour = true
    )
    AlertDialog(
        onDismissRequest = onDismiss,
        text = { TimePicker(state = picker) },
        confirmButton = {
            TextButton(onClick = { onConfirm(TimeOfDay.of(picker.hour, picker.minute)) }) {
                Text(stringResource(R.string.common_done))
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) }
        }
    )
}

// Weekdays follow the APP's language, never the phone's: someone with a Russian
// phone and the app in Kazakh reads Kazakh on every other line of these screens.

/** The full editor's row label: "Пн", "Дс", "Mon". */
internal fun weekdayName(weekday: Int, language: AppLanguage): String =
    weekdayShortName(weekday, language).replaceFirstChar { it.titlecase(language.locale) }

/** The abbreviation as the locale writes it: "пн", "дс", "Mon". */
internal fun weekdayShortName(weekday: Int, language: AppLanguage): String =
    WorkingHours.dayOfWeek(weekday).getDisplayName(TextStyle.SHORT_STANDALONE, language.locale)

/** The quick editor's caption: two letters, so seven of them fit on a phone. */
internal fun weekdaySymbol(weekday: Int, language: AppLanguage): String =
    weekdayShortName(weekday, language).take(2)

/** What a screen reader says for a weekday button. */
internal fun weekdayFullName(weekday: Int, language: AppLanguage): String =
    WorkingHours.dayOfWeek(weekday).getDisplayName(TextStyle.FULL_STANDALONE, language.locale)
