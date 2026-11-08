package kz.yerek.aireply.ui.feature.hours

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import kz.yerek.aireply.R
import kz.yerek.aireply.domain.model.TimeOfDay
import kz.yerek.aireply.domain.model.WorkingHours
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.common.AppScreen
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.design.AppCard
import kz.yerek.aireply.ui.design.AppSection
import kz.yerek.aireply.ui.design.LocalExtraColors
import kz.yerek.aireply.ui.design.ReadableColumn
import kz.yerek.aireply.ui.design.Spacing
import java.time.LocalDateTime

/**
 * The full per-day working-hours editor.
 *
 * TIMEZONE. Everything is evaluated against the device's own clock, and the
 * times are stored as minutes past midnight rather than as instants — so a user
 * who travels keeps the same working day rather than having it silently shift.
 * No timezone, city or coordinate is stored or sent; the model is told only
 * whether it is currently inside the user's hours and when the next window opens.
 */
@Composable
fun WorkingHoursScreen(onBack: () -> Unit) {
    val services = LocalServices.current
    val language = services.settings.effectiveAppLanguage
    var hours by remember { mutableStateOf(services.configuration.profile.workingHours) }
    var editing by remember { mutableStateOf<Editing?>(null) }

    val latest = rememberUpdatedState(hours)
    DisposableEffect(Unit) {
        onDispose {
            services.configuration.updateProfile { it.copy(workingHours = latest.value) }
        }
    }

    val context = remember(hours) { hours.context(LocalDateTime.now()) }

    AppScreen(title = stringResource(R.string.hours_title), onBack = onBack) {
        ReadableColumn {
            AppCard {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    Text(
                        stringResource(R.string.hours_enable),
                        style = MaterialTheme.typography.bodyLarge,
                        modifier = Modifier.weight(1f)
                    )
                    Switch(
                        checked = hours.isEnabled,
                        onCheckedChange = { hours = hours.copy(isEnabled = it) }
                    )
                }
            }

            if (hours.isEnabled) {
                AppSection(stringResource(R.string.hours_title)) {
                    AppCard {
                        Column {
                            WorkingHours.MONDAY_FIRST.forEach { weekday ->
                                val day = hours.schedule(weekday) ?: return@forEach
                                Row(
                                    modifier = Modifier
                                        .fillMaxWidth()
                                        .padding(vertical = Spacing.xs),
                                    verticalAlignment = Alignment.CenterVertically,
                                    horizontalArrangement = Arrangement.spacedBy(Spacing.xs)
                                ) {
                                    Text(
                                        text = weekdayName(weekday, language),
                                        style = MaterialTheme.typography.bodyLarge,
                                        fontWeight = FontWeight.Medium,
                                        modifier = Modifier.weight(1f)
                                    )

                                    if (day.isEnabled) {
                                        TimeChip(day.start.formatted) {
                                            editing = Editing(weekday, isStart = true, value = day.start)
                                        }
                                        Text("–", color = LocalExtraColors.current.textSecondary)
                                        TimeChip(day.end.formatted) {
                                            editing = Editing(weekday, isStart = false, value = day.end)
                                        }
                                    } else {
                                        Text(
                                            stringResource(R.string.hours_closed),
                                            style = MaterialTheme.typography.bodyMedium,
                                            color = LocalExtraColors.current.textSecondary
                                        )
                                    }

                                    Switch(
                                        checked = day.isEnabled,
                                        onCheckedChange = { enabled ->
                                            hours = hours.withDay(weekday) { it.copy(isEnabled = enabled) }
                                        }
                                    )
                                }

                                if (day.isEnabled && !day.isValid) {
                                    Footnote(stringResource(R.string.hours_invalid))
                                }
                            }
                        }
                    }

                    AppCard {
                        Text(
                            stringResource(
                                if (context.isWithinWorkingHours) {
                                    R.string.hours_now_inside
                                } else {
                                    R.string.hours_now_outside
                                }
                            ),
                            style = MaterialTheme.typography.bodyMedium
                        )
                    }
                }
            }

            Footnote(stringResource(R.string.hours_footer))
        }
    }

    editing?.let { target ->
        TimeOfDayDialog(
            initial = target.value,
            onDismiss = { editing = null },
            onConfirm = { value ->
                hours = hours.withDay(target.weekday) { day ->
                    if (target.isStart) day.copy(start = value) else day.copy(end = value)
                }
                editing = null
            }
        )
    }
}

private data class Editing(val weekday: Int, val isStart: Boolean, val value: TimeOfDay)
