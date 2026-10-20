package kz.yerek.aireply.ui.feature.onboarding

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.HelpOutline
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.outlined.Lock
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
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kz.yerek.aireply.R
import kz.yerek.aireply.data.settings.DeviceStateStore
import kz.yerek.aireply.keyboard.input.KeyboardStatus
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.common.openWebPage
import kz.yerek.aireply.ui.design.AppCard
import kz.yerek.aireply.ui.design.AppMark
import kz.yerek.aireply.ui.design.LocalExtraColors
import kz.yerek.aireply.ui.design.Spacing
import kz.yerek.aireply.ui.feature.setup.KeyboardHelpSheet

/**
 * «Подключи AI Reply»: the three things Android asks, each ticked as the
 * system reports it, why Android warns about every keyboard, and a field to
 * see the keyboard actually appear.
 *
 * Пернетақтаны қосу: жүйе айтқан күй, ескерту туралы түсінік, тексеру өрісі.
 *
 * The buttons (in the bar below) open the keyboard list or the system
 * picker; the keyboard is never switched behind the user's back. [status] is
 * re-read on resume and when the picker closes.
 */
@Composable
internal fun ColumnScope.ConnectStep(status: KeyboardStatus.Snapshot) {
    val services = LocalServices.current
    val context = LocalContext.current
    val account by services.account.state.collectAsStateWithLifecycle()
    var helping by rememberSaveable { mutableStateOf(false) }

    AppMark(size = 56.dp)
    StepHeader(
        stringResource(if (status.isReady) R.string.onboarding_connect_ready else R.string.onboarding_connect_title),
        stringResource(R.string.onboarding_connect_prompt)
    )

    AppCard {
        Column(verticalArrangement = Arrangement.spacedBy(Spacing.s)) {
            ConnectLine(1, stringResource(R.string.onboarding_connect_step_open), done = status.isEnabled)
            ConnectLine(2, stringResource(R.string.onboarding_connect_step_enable), done = status.isEnabled)
            ConnectLine(3, stringResource(R.string.onboarding_connect_step_default), done = status.isSelected)
        }
    }

    if (status.isReady) PracticeField()

    AppCard {
        Column(verticalArrangement = Arrangement.spacedBy(Spacing.xs)) {
            Row(horizontalArrangement = Arrangement.spacedBy(Spacing.xs), verticalAlignment = Alignment.CenterVertically) {
                Icon(Icons.Outlined.Lock, contentDescription = null, tint = MaterialTheme.colorScheme.primary, modifier = Modifier.size(18.dp))
                Text(stringResource(R.string.onboarding_connect_why_title), style = MaterialTheme.typography.titleMedium)
            }
            Footnote(
                stringResource(
                    R.string.onboarding_connect_why_body,
                    stringResource(R.string.kb_generate),
                    stringResource(R.string.kb_compose_write)
                )
            )
            TextButton(
                onClick = {
                    context.openWebPage(account.legalConfig.privacyUrl, services.settings.effectiveAppLanguage.code)
                },
                contentPadding = PaddingValues(horizontal = 0.dp)
            ) { Text(stringResource(R.string.onboarding_connect_privacy_link)) }
        }
    }

    TextButton(onClick = { helping = true }, modifier = Modifier.align(Alignment.CenterHorizontally)) {
        Icon(Icons.AutoMirrored.Outlined.HelpOutline, contentDescription = null, modifier = Modifier.size(18.dp))
        Text(stringResource(R.string.permissions_help), modifier = Modifier.padding(start = Spacing.xs))
    }

    if (helping) KeyboardHelpSheet(onDismiss = { helping = false })
}

/** A numbered line, its number turning into a tick once Android reports it done. */
@Composable
private fun ConnectLine(number: Int, text: String, done: Boolean) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(Spacing.s),
        verticalAlignment = Alignment.CenterVertically
    ) {
        Box(
            modifier = Modifier
                .size(26.dp)
                .clip(CircleShape)
                .background(if (done) LocalExtraColors.current.success else MaterialTheme.colorScheme.primaryContainer),
            contentAlignment = Alignment.Center
        ) {
            if (done) {
                Icon(Icons.Filled.Check, contentDescription = null, tint = MaterialTheme.colorScheme.onPrimary, modifier = Modifier.size(16.dp))
            } else {
                Text(number.toString(), style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.primary)
            }
        }
        Text(text, style = MaterialTheme.typography.bodyLarge, modifier = Modifier.weight(1f))
    }
}

/**
 * Tap, and the AI Reply keyboard should come up. The keyboard notes when it
 * is on screen ([DeviceStateStore.keyboardLastShownAt]); a note from this
 * visit, or from the last few minutes, is the proof shown here.
 */
@Composable
private fun PracticeField() {
    val deviceState = LocalServices.current.deviceState
    var sample by rememberSaveable { mutableStateOf("") }
    val openedAt = remember { System.currentTimeMillis() }
    var shownAt by remember { mutableStateOf(deviceState.keyboardLastShownAt) }
    DisposableEffect(deviceState) {
        val stop = deviceState.observe { shownAt = deviceState.keyboardLastShownAt }
        onDispose(stop)
    }
    val confirmed = shownAt?.let { it >= openedAt - DeviceStateStore.KEYBOARD_SHOWN_RESOLUTION_MS } == true

    AppCard {
        Column(verticalArrangement = Arrangement.spacedBy(Spacing.s)) {
            Footnote(stringResource(R.string.onboarding_practice_field_prompt))
            OutlinedTextField(
                value = sample,
                onValueChange = { sample = it },
                placeholder = { Text(stringResource(R.string.onboarding_practice_field_placeholder)) },
                modifier = Modifier.fillMaxWidth()
            )
            if (confirmed) {
                Row(
                    modifier = Modifier.semantics(mergeDescendants = true) { liveRegion = LiveRegionMode.Polite },
                    horizontalArrangement = Arrangement.spacedBy(Spacing.xs),
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    Icon(Icons.Filled.CheckCircle, contentDescription = null, tint = LocalExtraColors.current.success, modifier = Modifier.size(18.dp))
                    Text(stringResource(R.string.onboarding_practice_confirmed), style = MaterialTheme.typography.bodyMedium)
                }
            } else {
                Footnote(stringResource(R.string.onboarding_switch_note))
            }
        }
    }
}
