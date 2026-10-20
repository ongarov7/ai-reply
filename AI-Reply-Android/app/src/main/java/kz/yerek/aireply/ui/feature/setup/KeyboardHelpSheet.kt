package kz.yerek.aireply.ui.feature.setup

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import kz.yerek.aireply.R
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.design.Spacing

/**
 * «Не получается?»: what usually goes wrong while turning a keyboard on in
 * Android, and the way past each. Four short answers, no diagnosis pretended.
 *
 * Пернетақтаны қосу кезіндегі жиі кедергілер және шешімі.
 *
 * @param onPasteHelp opens the paste page; null where there is nowhere to go
 *   (the first run, before the app's screens exist).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun KeyboardHelpSheet(onDismiss: () -> Unit, onPasteHelp: (() -> Unit)? = null) {
    val context = LocalContext.current
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
        containerColor = MaterialTheme.colorScheme.surface
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .verticalScroll(rememberScrollState())
                .navigationBarsPadding()
                .padding(horizontal = Spacing.l)
                .padding(bottom = Spacing.l),
            verticalArrangement = Arrangement.spacedBy(Spacing.m)
        ) {
            Text(
                stringResource(R.string.permissions_help),
                style = MaterialTheme.typography.headlineMedium,
                modifier = Modifier.semantics { heading() }
            )
            HelpPoint(R.string.keyboard_help_not_listed_title, R.string.keyboard_help_not_listed_body)
            TextButton(onClick = { openKeyboardSettings(context) }) {
                Text(stringResource(R.string.permissions_action_keyboard_settings))
            }
            HelpPoint(R.string.keyboard_help_warning_title, R.string.keyboard_help_warning_body)
            HelpPoint(R.string.keyboard_help_other_title, R.string.keyboard_help_other_body)
            HelpPoint(R.string.keyboard_help_paste_title, R.string.keyboard_help_paste_body)
            if (onPasteHelp != null) {
                TextButton(onClick = onPasteHelp) { Text(stringResource(R.string.permissions_action_paste_help)) }
            }
        }
    }
}

@Composable
private fun HelpPoint(title: Int, body: Int) {
    Column(verticalArrangement = Arrangement.spacedBy(Spacing.xxs)) {
        Text(stringResource(title), style = MaterialTheme.typography.titleMedium)
        Footnote(stringResource(body))
    }
}
