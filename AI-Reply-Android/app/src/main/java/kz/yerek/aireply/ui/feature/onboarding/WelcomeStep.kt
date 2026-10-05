package kz.yerek.aireply.ui.feature.onboarding

import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import kz.yerek.aireply.R
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.design.AppMark

/** What AI Reply does, in one picture: a message in a chat, the keyboard with its personas. */
@Composable
internal fun ColumnScope.WelcomeStep(onSkipTutorial: () -> Unit) {
    AppMark(size = 56.dp)
    Text(stringResource(R.string.onboarding_welcome_title), style = MaterialTheme.typography.displaySmall)
    Footnote(stringResource(R.string.onboarding_welcome_body))
    MockPhone {
        MockChat(incoming = stringResource(R.string.onboarding_practice_incoming))
        MockKeyboard(showsPersonas = true)
    }
    Footnote(stringResource(R.string.setup_privacy_explicit))
    SkipTutorialButton(onSkipTutorial)
}
