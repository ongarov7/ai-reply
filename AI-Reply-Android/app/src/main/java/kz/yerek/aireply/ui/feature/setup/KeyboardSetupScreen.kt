package kz.yerek.aireply.ui.feature.setup

import android.Manifest
import android.speech.SpeechRecognizer
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import android.content.Context
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import kz.yerek.aireply.R
import kz.yerek.aireply.keyboard.input.KeyboardStatus
import kz.yerek.aireply.ui.common.AppScreen
import kz.yerek.aireply.ui.common.ChecklistRow
import kz.yerek.aireply.ui.common.ChecklistState
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.common.rememberKeyboardStatus
import kz.yerek.aireply.ui.design.AppCard
import kz.yerek.aireply.ui.design.AppSection
import kz.yerek.aireply.ui.design.LocalExtraColors
import kz.yerek.aireply.ui.design.ReadableColumn
import kz.yerek.aireply.ui.design.PrimaryButton
import kz.yerek.aireply.ui.design.SecondaryButton
import kz.yerek.aireply.ui.design.Spacing
import kz.yerek.aireply.ui.design.StepRow
import kz.yerek.aireply.voice.MicPermission

/**
 * The keyboard setup guide.
 *
 * HONESTY WAS THE DESIGN CONSTRAINT ON iOS, and it produced a screen with a
 * "we cannot tell yet" state, a paragraph explaining why iOS will not say
 * whether its own keyboard is enabled, and a button that can only open the
 * app's own settings page because no public URL reaches the keyboard list.
 *
 * None of that is true here. Android answers both questions and provides an
 * intent that lands in exactly the right place, so this screen is shorter,
 * the checklist is a fact, and the two buttons actually do the two things.
 * The privacy section is kept word for word, because that part of the promise
 * is identical on both platforms.
 */
@Composable
fun KeyboardSetupScreen(onBack: (() -> Unit)? = null, showsTitle: Boolean = true) {
    val context = LocalContext.current
    val status by rememberKeyboardStatus()
    var microphoneGranted by remember { mutableStateOf(MicPermission.isGranted(context)) }
    val speechAvailable = remember { SpeechRecognizer.isRecognitionAvailable(context) }
    val microphoneLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) { granted -> microphoneGranted = granted }

    val body: @Composable () -> Unit = {
        ReadableColumn {
            Footnote(stringResource(R.string.android_setup_subtitle))

            AppSection(stringResource(R.string.setup_checklist_title)) {
                AppCard {
                    Column(verticalArrangement = Arrangement.spacedBy(Spacing.s)) {
                        ChecklistRow(
                            title = stringResource(R.string.android_checklist_enabled),
                            state = if (status.isEnabled) ChecklistState.DONE else ChecklistState.MISSING,
                            statusLabel = stringResource(
                                if (status.isEnabled) R.string.setup_state_done else R.string.setup_state_missing
                            )
                        )
                        ChecklistRow(
                            title = stringResource(R.string.android_checklist_selected),
                            state = if (status.isSelected) ChecklistState.DONE else ChecklistState.MISSING,
                            statusLabel = stringResource(
                                if (status.isSelected) R.string.setup_state_done else R.string.setup_state_missing
                            )
                        )
                        ChecklistRow(
                            title = stringResource(R.string.setup_checklist_microphone),
                            state = if (microphoneGranted) ChecklistState.DONE else ChecklistState.MISSING,
                            statusLabel = stringResource(
                                if (microphoneGranted) R.string.setup_state_done else R.string.setup_state_missing
                            )
                        )
                        ChecklistRow(
                            title = stringResource(R.string.setup_checklist_speech),
                            state = if (speechAvailable) ChecklistState.DONE else ChecklistState.MISSING,
                            statusLabel = stringResource(
                                if (speechAvailable) R.string.setup_state_done else R.string.setup_state_unavailable
                            )
                        )
                        Footnote(stringResource(R.string.android_checklist_footer))
                    }
                }
            }

            AppSection(stringResource(R.string.setup_voice_title)) {
                AppCard {
                    Footnote(stringResource(R.string.setup_voice_body))
                }
                if (!microphoneGranted) {
                    SecondaryButton(
                        text = stringResource(R.string.setup_voice_enable),
                        enabled = speechAvailable,
                        onClick = { microphoneLauncher.launch(Manifest.permission.RECORD_AUDIO) }
                    )
                }
            }

            AppSection(stringResource(R.string.setup_steps_title)) {
                AppCard {
                    Column(verticalArrangement = Arrangement.spacedBy(Spacing.s)) {
                        StepRow(1, stringResource(R.string.android_step_enable))
                        StepRow(2, stringResource(R.string.android_step_select))
                        StepRow(3, stringResource(R.string.android_step_test))
                    }
                }
                KeyboardSetupAction(status, context)
            }

            AppSection(stringResource(R.string.setup_paste_title)) {
                AppCard {
                    Text(
                        stringResource(R.string.android_howitworks_clipboard),
                        style = MaterialTheme.typography.bodyLarge
                    )
                }
            }

            AppSection(stringResource(R.string.setup_privacy_title)) {
                AppCard {
                    Column(verticalArrangement = Arrangement.spacedBy(Spacing.s)) {
                        PrivacyPoint(stringResource(R.string.setup_privacy_clipboard))
                        PrivacyPoint(stringResource(R.string.setup_privacy_explicit))
                        PrivacyPoint(stringResource(R.string.android_privacy_sensitive))
                        PrivacyPoint(stringResource(R.string.setup_privacy_nokeyinkeyboard))
                        PrivacyPoint(stringResource(R.string.setup_privacy_securefields))
                    }
                }
            }
        }
    }

    if (showsTitle) {
        AppScreen(title = stringResource(R.string.android_setup_title), onBack = onBack) { body() }
    } else {
        body()
    }
}

@Composable
private fun PrivacyPoint(text: String) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(Spacing.s),
        verticalAlignment = Alignment.Top
    ) {
        Icon(
            Icons.Filled.Lock,
            contentDescription = null,
            tint = MaterialTheme.colorScheme.primary,
            modifier = Modifier.size(16.dp)
        )
        Text(
            text,
            style = MaterialTheme.typography.bodySmall,
            color = LocalExtraColors.current.textSecondary
        )
    }
}

/**
 * The single setup action for the step that is still missing: turn the
 * keyboard on in system settings, or - once it is on - pick it. Nothing when
 * both are done. Android reports both states reliably, and the screen
 * re-reads them on every resume, so returning from Settings updates this.
 */
@Composable
fun KeyboardSetupAction(status: KeyboardStatus.Snapshot, context: Context) {
    when {
        !status.isEnabled -> PrimaryButton(
            text = stringResource(R.string.android_open_keyboard_settings),
            onClick = { KeyboardStatus.openKeyboardSettings(context) }
        )
        !status.isSelected -> PrimaryButton(
            text = stringResource(R.string.android_switch_keyboard),
            onClick = { KeyboardStatus.showKeyboardPicker(context) }
        )
    }
}
