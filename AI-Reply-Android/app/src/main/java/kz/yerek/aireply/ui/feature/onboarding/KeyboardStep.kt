package kz.yerek.aireply.ui.feature.onboarding

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import kz.yerek.aireply.R
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.common.keyboardWarningNote
import kz.yerek.aireply.ui.common.rememberKeyboardStatus
import kz.yerek.aireply.ui.design.AppCard
import kz.yerek.aireply.ui.design.Spacing
import kz.yerek.aireply.ui.design.StepRow
import kz.yerek.aireply.ui.feature.setup.KeyboardSetupAction
import kz.yerek.aireply.ui.feature.setup.KeyboardStatusRows

/**
 * Turn the keyboard on and pick it, with the system's answer shown live.
 *
 * The action is the same single button Home and the setup guide use; the
 * numbered lines only say what the two system screens will ask. The field at
 * the end lets the user switch to AI Reply right here and see it type.
 */
@Composable
internal fun ColumnScope.KeyboardStep(onSkipTutorial: () -> Unit) {
    val context = LocalContext.current
    val status by rememberKeyboardStatus()
    var sample by rememberSaveable { mutableStateOf("") }

    StepHeader(
        stringResource(R.string.onboarding_keyboard_title),
        stringResource(R.string.onboarding_keyboard_prompt)
    )
    AppCard { KeyboardStatusRows(status) }
    AppCard {
        Column(verticalArrangement = Arrangement.spacedBy(Spacing.s)) {
            StepRow(1, stringResource(R.string.onboarding_keyboard_step_enable))
            StepRow(2, stringResource(R.string.onboarding_keyboard_step_confirm))
            StepRow(3, stringResource(R.string.onboarding_keyboard_step_choose))
        }
    }
    KeyboardSetupAction(status, context)
    Footnote(keyboardWarningNote())
    AppCard {
        Column(verticalArrangement = Arrangement.spacedBy(Spacing.s)) {
            Footnote(stringResource(R.string.onboarding_test_prompt))
            OutlinedTextField(
                value = sample,
                onValueChange = { sample = it },
                placeholder = { Text(stringResource(R.string.onboarding_test_placeholder)) },
                modifier = Modifier.fillMaxWidth()
            )
            Footnote(stringResource(R.string.onboarding_switch_note))
        }
    }
    SkipTutorialButton(onSkipTutorial)
}
