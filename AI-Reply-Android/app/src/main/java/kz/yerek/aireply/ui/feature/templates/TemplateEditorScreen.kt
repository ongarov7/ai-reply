package kz.yerek.aireply.ui.feature.templates

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Mic
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import kz.yerek.aireply.R
import kz.yerek.aireply.core.lang.TemplateNaming
import kz.yerek.aireply.core.text.clampToCodePoints
import kz.yerek.aireply.core.text.codePointLength
import kz.yerek.aireply.domain.model.BusinessContext
import kz.yerek.aireply.domain.model.EmojiPolicy
import kz.yerek.aireply.domain.model.ReplyLength
import kz.yerek.aireply.domain.model.ReplyTemplate
import kz.yerek.aireply.domain.model.ReplyTone
import kz.yerek.aireply.domain.model.WorkingHoursBehaviour
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.common.AppScreen
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.design.AppCard
import kz.yerek.aireply.ui.design.AppSection
import kz.yerek.aireply.ui.design.LocalExtraColors
import kz.yerek.aireply.ui.design.ReadableColumn
import kz.yerek.aireply.ui.design.Spacing
import kz.yerek.aireply.ui.feature.hours.QuickHoursEditor
import kz.yerek.aireply.ui.feature.profile.RulesEditor
import kz.yerek.aireply.ui.feature.profile.SelectableRow
import kz.yerek.aireply.ui.feature.profile.toneResource
import kz.yerek.aireply.ui.feature.voice.DictationSheet
import kz.yerek.aireply.ui.feature.voice.VoiceSuggestionSheet
import kz.yerek.aireply.voice.VoiceConfigurationParser

/**
 * One template's settings.
 *
 * The sections follow the iOS editor exactly: basic, business, working hours,
 * rules, style, instructions — and business, hours and rules are hidden for a
 * Friend template, which has no use for any of them.
 */
@Composable
fun TemplateEditorScreen(templateId: String, onBack: () -> Unit) {
    val services = LocalServices.current
    val context = LocalContext.current
    val language = services.settings.effectiveAppLanguage

    val original = remember(templateId) { services.configuration.current.template(templateId) }
    if (original == null) {
        // The template was deleted while this screen was on the back stack.
        androidx.compose.runtime.LaunchedEffect(Unit) { onBack() }
        return
    }

    var draft by remember(templateId) { mutableStateOf(original) }
    var dictating by remember { mutableStateOf(false) }
    var suggestion by remember { mutableStateOf<VoiceConfigurationParser.Suggestion?>(null) }
    var confirmingDelete by remember { mutableStateOf(false) }

    val latest = rememberUpdatedState(draft)
    DisposableEffect(templateId) {
        onDispose { services.configuration.update(latest.value.withFinishedName(original)) }
    }

    val usesBusiness = draft.relationship.usesBusinessContext

    AppScreen(title = TemplateNaming.displayName(context, draft), onBack = onBack) {
        ReadableColumn {

            AppSection(stringResource(R.string.templates_section_basic)) {
                AppCard {
                    Column(verticalArrangement = Arrangement.spacedBy(Spacing.s)) {
                        OutlinedTextField(
                            value = draft.customName ?: "",
                            onValueChange = { draft = draft.withTypedName(it) },
                            label = { Text(stringResource(R.string.templates_name)) },
                            placeholder = {
                                Text(stringResource(TemplateNaming.resource(draft.relationship)))
                            },
                            singleLine = true,
                            modifier = Modifier.fillMaxWidth()
                        )
                        Row(
                            modifier = Modifier.fillMaxWidth(),
                            verticalAlignment = Alignment.CenterVertically
                        ) {
                            Text(
                                stringResource(R.string.templates_visible),
                                style = MaterialTheme.typography.bodyLarge,
                                modifier = Modifier.weight(1f)
                            )
                            Switch(
                                checked = draft.isVisible,
                                onCheckedChange = { draft = draft.copy(isVisible = it) }
                            )
                        }
                    }
                }
            }

            if (usesBusiness) {
                AppSection(stringResource(R.string.templates_section_business)) {
                    AppCard {
                        val business = draft.business ?: BusinessContext.EMPTY
                        Column(verticalArrangement = Arrangement.spacedBy(Spacing.xs)) {
                            OutlinedTextField(
                                value = business.offering,
                                onValueChange = {
                                    draft = draft.copy(business = business.withOffering(it))
                                },
                                placeholder = {
                                    Text(stringResource(R.string.templates_offering_placeholder))
                                },
                                modifier = Modifier.fillMaxWidth()
                            )
                            OutlinedTextField(
                                value = business.summary,
                                onValueChange = {
                                    draft = draft.copy(business = business.withSummary(it))
                                },
                                placeholder = {
                                    Text(stringResource(R.string.templates_summary_placeholder))
                                },
                                minLines = 2,
                                modifier = Modifier.fillMaxWidth()
                            )
                        }
                    }
                    Footnote(stringResource(R.string.templates_section_business_footer))
                }

                AppSection(stringResource(R.string.templates_section_hours)) {
                    AppCard {
                        Column(verticalArrangement = Arrangement.spacedBy(Spacing.s)) {
                            Row(
                                modifier = Modifier.fillMaxWidth(),
                                verticalAlignment = Alignment.CenterVertically
                            ) {
                                Text(
                                    stringResource(R.string.templates_hours_override),
                                    style = MaterialTheme.typography.bodyLarge,
                                    modifier = Modifier.weight(1f)
                                )
                                Switch(
                                    checked = draft.workingHoursOverride?.isEnabled == true,
                                    onCheckedChange = { enabled ->
                                        draft = draft.withHoursOverride(
                                            enabled,
                                            services.configuration.profile.workingHours
                                        )
                                    }
                                )
                            }
                            draft.workingHoursOverride?.takeIf { it.isEnabled }?.let { hours ->
                                QuickHoursEditor(
                                    hours = hours,
                                    language = language,
                                    onChange = { draft = draft.copy(workingHoursOverride = it) }
                                )
                            }
                            WorkingHoursBehaviour.entries.forEach { option ->
                                SelectableRow(
                                    label = stringResource(behaviourResource(option)),
                                    selected = draft.workingHoursBehaviour == option
                                ) { draft = draft.copy(workingHoursBehaviour = option) }
                            }
                        }
                    }
                    Footnote(stringResource(R.string.templates_section_hours_footer))
                }

                RulesEditor(
                    business = draft.business ?: BusinessContext.EMPTY,
                    onChange = { draft = draft.copy(business = it) },
                    footnote = stringResource(R.string.templates_rules_footer)
                )
            }

            AppSection(stringResource(R.string.templates_section_style)) {
                AppCard {
                    Column {
                        Footnote(stringResource(R.string.templates_tone))
                        ReplyTone.entries.forEach { option ->
                            SelectableRow(
                                label = stringResource(toneResource(option)),
                                selected = draft.tone == option
                            ) { draft = draft.copy(tone = option) }
                        }
                        Footnote(stringResource(R.string.templates_length))
                        ReplyLength.entries.forEach { option ->
                            SelectableRow(
                                label = stringResource(lengthResource(option)),
                                selected = draft.replyLength == option
                            ) { draft = draft.copy(replyLength = option) }
                        }
                        Footnote(stringResource(R.string.templates_emoji))
                        EmojiPolicy.entries.forEach { option ->
                            SelectableRow(
                                label = stringResource(emojiResource(option)),
                                selected = draft.emojiPolicy == option
                            ) { draft = draft.copy(emojiPolicy = option) }
                        }
                    }
                }
            }

            AppSection(stringResource(R.string.templates_instructions)) {
                AppCard {
                    OutlinedTextField(
                        value = draft.instructions,
                        onValueChange = { draft = draft.withInstructions(it) },
                        placeholder = {
                            Text(stringResource(R.string.templates_instructions_placeholder))
                        },
                        modifier = Modifier
                            .fillMaxWidth()
                            .height(140.dp)
                    )
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        verticalAlignment = Alignment.CenterVertically
                    ) {
                        Text(
                            stringResource(
                                R.string.profile_counter,
                                draft.instructions.codePointLength(),
                                ReplyTemplate.MAX_INSTRUCTIONS
                            ),
                            style = MaterialTheme.typography.labelSmall,
                            color = LocalExtraColors.current.textSecondary,
                            modifier = Modifier.weight(1f)
                        )
                        TextButton(onClick = { dictating = true }) {
                            Icon(
                                Icons.Filled.Mic,
                                contentDescription = null,
                                modifier = Modifier.size(18.dp)
                            )
                            Text(
                                stringResource(R.string.profile_dictate),
                                modifier = Modifier.padding(start = Spacing.xs)
                            )
                        }
                    }
                }
                Footnote(stringResource(R.string.templates_instructions_footer))
            }

            // Built-ins are never destroyed, only hidden, so a user cannot end
            // up with an empty keyboard bar and no way back to the defaults.
            if (!draft.isBuiltIn) {
                TextButton(onClick = { confirmingDelete = true }) {
                    Text(
                        stringResource(R.string.templates_delete),
                        color = MaterialTheme.colorScheme.error
                    )
                }
            }
        }
    }

    if (dictating) {
        DictationSheet(
            language = language,
            onDismiss = { dictating = false },
            onAccept = { transcript ->
                // The sentence goes into the notes verbatim. Structured values
                // are only ever suggested on top of it, so nothing the user said
                // is replaced by the app's interpretation of it.
                val addition = transcript.trim()
                if (addition.isNotEmpty()) {
                    val existing = draft.instructions
                    val separator = if (existing.isEmpty() || existing.endsWith(" ")) "" else " "
                    draft = draft.withInstructions(
                        (existing + separator + addition)
                            .clampToCodePoints(ReplyTemplate.MAX_INSTRUCTIONS)
                    )

                    val parsed = VoiceConfigurationParser.parse(addition, language)
                    if (!parsed.isEmpty) suggestion = parsed
                }
            }
        )
    }

    suggestion?.let { value ->
        VoiceSuggestionSheet(
            suggestion = value,
            existingHours = draft.workingHoursOverride ?: services.configuration.profile.workingHours,
            language = language,
            onDismiss = { suggestion = null },
            onApply = { confirmed ->
                draft = confirmed.appliedTo(draft, services.configuration.profile.workingHours)
            }
        )
    }

    if (confirmingDelete) {
        AlertDialog(
            onDismissRequest = { confirmingDelete = false },
            title = { Text(stringResource(R.string.templates_delete_confirm)) },
            confirmButton = {
                TextButton(onClick = {
                    confirmingDelete = false
                    services.configuration.delete(draft)
                    onBack()
                }) {
                    Text(
                        stringResource(R.string.common_delete),
                        color = MaterialTheme.colorScheme.error
                    )
                }
            },
            dismissButton = {
                TextButton(onClick = { confirmingDelete = false }) {
                    Text(stringResource(R.string.common_cancel))
                }
            }
        )
    }
}

/**
 * The name as it is being typed, written straight into the field: a space
 * stays for the next word, and the last letter can go on the way to a new
 * name. It is made a proper name once, by [withFinishedName].
 */
internal fun ReplyTemplate.withTypedName(value: String): ReplyTemplate =
    copy(customName = value.clampToCodePoints(ReplyTemplate.MAX_NAME))

/**
 * The name to keep when the screen closes: [ReplyTemplate.withName] on what
 * was typed, so a built-in left empty goes back to its own name and one the
 * user made keeps the name it had in [original].
 */
internal fun ReplyTemplate.withFinishedName(original: ReplyTemplate): ReplyTemplate =
    copy(customName = original.customName).withName(customName.orEmpty())

private fun behaviourResource(value: WorkingHoursBehaviour): Int = when (value) {
    WorkingHoursBehaviour.IGNORE -> R.string.templates_hours_ignore
    WorkingHoursBehaviour.MENTION_WHEN_RELEVANT -> R.string.templates_hours_mentionwhenrelevant
    WorkingHoursBehaviour.ALWAYS_MENTION -> R.string.templates_hours_alwaysmention
}

private fun lengthResource(value: ReplyLength): Int = when (value) {
    ReplyLength.SHORT -> R.string.templates_length_short
    ReplyLength.MEDIUM -> R.string.templates_length_medium
}

private fun emojiResource(value: EmojiPolicy): Int = when (value) {
    EmojiPolicy.ALLOWED -> R.string.templates_emoji_allowed
    EmojiPolicy.MINIMAL -> R.string.templates_emoji_minimal
    EmojiPolicy.NONE -> R.string.templates_emoji_none
}
