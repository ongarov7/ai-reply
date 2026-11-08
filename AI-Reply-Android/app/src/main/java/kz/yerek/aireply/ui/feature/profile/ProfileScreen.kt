package kz.yerek.aireply.ui.feature.profile

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Mic
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
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
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import kz.yerek.aireply.R
import kz.yerek.aireply.core.text.clampToCodePoints
import kz.yerek.aireply.core.text.codePointLength
import kz.yerek.aireply.domain.model.BusinessContext
import kz.yerek.aireply.domain.model.ReplyLanguagePreference
import kz.yerek.aireply.domain.model.ReplyTone
import kz.yerek.aireply.domain.model.UserProfile
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.common.AppScreen
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.design.AppCard
import kz.yerek.aireply.ui.design.AppSection
import kz.yerek.aireply.ui.design.LocalExtraColors
import kz.yerek.aireply.ui.design.ReadableColumn
import kz.yerek.aireply.ui.design.Spacing
import kz.yerek.aireply.ui.feature.voice.DictationSheet
import kz.yerek.aireply.ui.feature.voice.VoiceSuggestionSheet
import kz.yerek.aireply.voice.VoiceConfigurationParser

/**
 * "About me" — the context that is true of the user in every conversation.
 *
 * It asks normal questions. There is no prompt field, no system-message box and
 * no mention of a model anywhere on this screen: the user says what they do and
 * what must never be promised, and `ReplyPromptBuilder` turns that into context.
 * That separation is deliberate — it means the prompt can be improved without
 * asking anyone to rewrite their answers.
 */
@Composable
fun ProfileScreen(onBack: () -> Unit) {
    val services = LocalServices.current
    val language = services.settings.effectiveAppLanguage
    val stored = remember { services.configuration.profile }

    var role by remember { mutableStateOf(stored.role) }
    var about by remember { mutableStateOf(stored.descriptionText) }
    var business by remember { mutableStateOf(stored.business) }
    var tone by remember { mutableStateOf(stored.preferredTone) }
    var replyLanguage by remember { mutableStateOf(stored.replyLanguage) }
    var dictating by remember { mutableStateOf(false) }
    var suggestion by remember { mutableStateOf<VoiceConfigurationParser.Suggestion?>(null) }

    // Saved on the way out rather than on every keystroke: the store writes a
    // file the keyboard reads, and rewriting it per character would be pointless
    // disk traffic.
    val latest = rememberUpdatedState(ProfileDraft(role, about, business, tone, replyLanguage))
    DisposableEffect(Unit) {
        onDispose {
            val draft = latest.value
            services.configuration.updateProfile { profile ->
                profile.withRole(draft.role).withDescription(draft.about).copy(
                    business = draft.business,
                    preferredTone = draft.tone,
                    replyLanguage = draft.replyLanguage
                )
            }
        }
    }

    AppScreen(title = stringResource(R.string.profile_title), onBack = onBack) {
        ReadableColumn {

            AppSection(stringResource(R.string.profile_role)) {
                AppCard {
                    OutlinedTextField(
                        value = role,
                        onValueChange = { role = it.clampToCodePoints(UserProfile.MAX_ROLE) },
                        placeholder = { Text(stringResource(R.string.profile_role_placeholder)) },
                        modifier = Modifier.fillMaxWidth()
                    )
                }
            }

            AppSection(stringResource(R.string.profile_business)) {
                AppCard {
                    Column(verticalArrangement = Arrangement.spacedBy(Spacing.xs)) {
                        OutlinedTextField(
                            value = business.offering,
                            onValueChange = { business = business.withOffering(it) },
                            label = { Text(stringResource(R.string.profile_offering)) },
                            placeholder = { Text(stringResource(R.string.profile_offering_placeholder)) },
                            modifier = Modifier.fillMaxWidth()
                        )
                        OutlinedTextField(
                            value = business.summary,
                            onValueChange = { business = business.withSummary(it) },
                            placeholder = { Text(stringResource(R.string.profile_summary_placeholder)) },
                            minLines = 2,
                            modifier = Modifier.fillMaxWidth()
                        )
                    }
                }
                Footnote(stringResource(R.string.profile_business_footer))
            }

            RulesEditor(
                business = business,
                onChange = { business = it },
                footnote = stringResource(R.string.profile_rules_footer)
            )

            AppSection(stringResource(R.string.profile_about)) {
                AppCard {
                    OutlinedTextField(
                        value = about,
                        // Clamped as the user types rather than silently
                        // truncated on save, so the counter never lies.
                        onValueChange = { about = it.clampToCodePoints(UserProfile.MAX_DESCRIPTION) },
                        placeholder = { Text(stringResource(R.string.onboarding_profile_placeholder)) },
                        modifier = Modifier
                            .fillMaxWidth()
                            .height(150.dp)
                    )
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        verticalAlignment = Alignment.CenterVertically
                    ) {
                        Text(
                            text = stringResource(
                                R.string.profile_counter,
                                about.codePointLength(),
                                UserProfile.MAX_DESCRIPTION
                            ),
                            style = MaterialTheme.typography.labelSmall,
                            color = if (about.codePointLength() > UserProfile.MAX_DESCRIPTION - 80) {
                                LocalExtraColors.current.warning
                            } else {
                                LocalExtraColors.current.textSecondary
                            }
                        )
                        FlexSpacer()
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
                Footnote(stringResource(R.string.profile_description_footer))
            }

            AppSection(stringResource(R.string.profile_tone)) {
                AppCard {
                    Column {
                        ReplyTone.entries.forEach { option ->
                            SelectableRow(
                                label = stringResource(toneResource(option)),
                                selected = tone == option
                            ) { tone = option }
                        }
                    }
                }
            }

            ReplyLanguageSection(selection = replyLanguage, onSelect = { replyLanguage = it })

            GenderSection()
        }
    }

    if (dictating) {
        DictationSheet(
            language = language,
            onDismiss = { dictating = false },
            onAccept = { transcript ->
                // What the user said is kept verbatim; what the app recognised
                // in it is offered separately and applied only on confirmation.
                val addition = transcript.trim()
                if (addition.isNotEmpty()) {
                    val separator = if (about.isEmpty() || about.endsWith(" ")) "" else " "
                    about = (about + separator + addition)
                        .clampToCodePoints(UserProfile.MAX_DESCRIPTION)

                    val parsed = VoiceConfigurationParser.parse(addition, language)
                    if (!parsed.isEmpty) suggestion = parsed
                }
            }
        )
    }

    suggestion?.let { value ->
        VoiceSuggestionSheet(
            suggestion = value,
            existingHours = services.configuration.profile.workingHours,
            language = language,
            onDismiss = { suggestion = null },
            onApply = { confirmed ->
                confirmed.workingHours(basedOn = services.configuration.profile.workingHours)?.let { updated ->
                    services.configuration.updateProfile { it.copy(workingHours = updated) }
                }
                business = confirmed.rules.fold(business, BusinessContext::addingRule)
            }
        )
    }
}

private data class ProfileDraft(
    val role: String,
    val about: String,
    val business: BusinessContext,
    val tone: ReplyTone,
    val replyLanguage: ReplyLanguagePreference?
)

fun toneResource(tone: ReplyTone): Int = when (tone) {
    ReplyTone.NATURAL -> R.string.tone_natural
    ReplyTone.FRIENDLY -> R.string.tone_friendly
    ReplyTone.PROFESSIONAL -> R.string.tone_professional
    ReplyTone.FORMAL -> R.string.tone_formal
    ReplyTone.SHORT -> R.string.tone_short
}
