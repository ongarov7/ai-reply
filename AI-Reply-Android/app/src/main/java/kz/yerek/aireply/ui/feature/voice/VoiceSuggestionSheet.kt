package kz.yerek.aireply.ui.feature.voice

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Checkbox
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import kz.yerek.aireply.R
import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.domain.model.WorkingHours
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.design.AppSection
import kz.yerek.aireply.ui.design.Spacing
import kz.yerek.aireply.ui.feature.hours.weekdayShortName
import kz.yerek.aireply.voice.VoiceConfigurationParser

/**
 * Shown after dictation when the app recognised settings in what was said.
 *
 * It exists so the extraction is never silent. The user is shown, in plain
 * words, exactly what the app took from the sentence and what it will change,
 * with the option to take none of it. Whatever they decide, what they said stays
 * in the field they dictated into.
 *
 * @param onApply gets only what the user ticked. Not called at all on Skip.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun VoiceSuggestionSheet(
    suggestion: VoiceConfigurationParser.Suggestion,
    existingHours: WorkingHours,
    language: AppLanguage,
    onDismiss: () -> Unit,
    onApply: (VoiceConfigurationParser.Suggestion) -> Unit
) {
    var applyHours by remember(suggestion) { mutableStateOf(suggestion.hasWorkingHours) }
    // Rules start unticked: adding a sentence to the rules the model must follow
    // is the user's decision, not a default.
    var selectedRules by remember(suggestion) { mutableStateOf(emptySet<Int>()) }
    val hasSelection = (applyHours && suggestion.hasWorkingHours) || selectedRules.isNotEmpty()

    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
        containerColor = MaterialTheme.colorScheme.surface
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .navigationBarsPadding()
                .padding(horizontal = Spacing.l)
                .padding(bottom = Spacing.m),
            verticalArrangement = Arrangement.spacedBy(Spacing.m)
        ) {
            // Only this part scrolls, so eight long rules cannot push the
            // buttons off the screen.
            Column(
                modifier = Modifier
                    .weight(1f, fill = false)
                    .verticalScroll(rememberScrollState()),
                verticalArrangement = Arrangement.spacedBy(Spacing.l)
            ) {
                Column(verticalArrangement = Arrangement.spacedBy(Spacing.xs)) {
                    Text(
                        stringResource(R.string.voice_suggestions_title),
                        style = MaterialTheme.typography.headlineMedium,
                        modifier = Modifier.semantics { heading() }
                    )
                    Footnote(stringResource(R.string.voice_suggestions_body))
                }

                if (suggestion.hasWorkingHours) {
                    AppSection(stringResource(R.string.voice_suggestions_hours)) {
                        HoursRow(
                            summary = hoursSummary(suggestion, existingHours, language),
                            isOn = applyHours,
                            onChange = { applyHours = it }
                        )
                    }
                }

                if (suggestion.rules.isNotEmpty()) {
                    AppSection(stringResource(R.string.voice_suggestions_rules)) {
                        suggestion.rules.forEachIndexed { index, rule ->
                            RuleRow(
                                rule = rule,
                                isTicked = index in selectedRules,
                                onChange = { ticked ->
                                    selectedRules = if (ticked) selectedRules + index else selectedRules - index
                                }
                            )
                        }
                        Footnote(stringResource(R.string.voice_suggestions_rules_footer))
                    }
                }
            }

            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween
            ) {
                TextButton(onClick = onDismiss) {
                    Text(stringResource(R.string.voice_suggestions_skip))
                }
                TextButton(
                    enabled = hasSelection,
                    onClick = {
                        onApply(suggestion.confirmed(applyHours, selectedRules))
                        onDismiss()
                    }
                ) { Text(stringResource(R.string.common_apply)) }
            }
        }
    }
}

@Composable
private fun HoursRow(summary: String, isOn: Boolean, onChange: (Boolean) -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .toggleable(value = isOn, role = Role.Switch, onValueChange = onChange),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(Spacing.s)
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(stringResource(R.string.hours_title), style = MaterialTheme.typography.bodyLarge)
            Footnote(summary)
        }
        Switch(checked = isOn, onCheckedChange = null)
    }
}

@Composable
private fun RuleRow(rule: String, isTicked: Boolean, onChange: (Boolean) -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .toggleable(value = isTicked, role = Role.Checkbox, onValueChange = onChange)
            .padding(vertical = Spacing.xxs),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(Spacing.s)
    ) {
        Checkbox(checked = isTicked, onCheckedChange = null)
        Text(rule, style = MaterialTheme.typography.bodyLarge, modifier = Modifier.weight(1f))
    }
}

/** "пн, вт, ср · 10:00-18:00": the days the schedule will have open, Monday first. */
internal fun hoursSummary(
    suggestion: VoiceConfigurationParser.Suggestion,
    existingHours: WorkingHours,
    language: AppLanguage
): String {
    val start = suggestion.start ?: return ""
    val end = suggestion.end ?: return ""
    val days = suggestion.weekdays ?: existingHours.days.filter { it.isEnabled }.map { it.weekday }
    val names = WorkingHours.MONDAY_FIRST.filter { it in days }.joinToString(", ") { weekdayShortName(it, language) }
    val range = "${start.formatted}-${end.formatted}"
    return if (names.isEmpty()) range else "$names · $range"
}
