package kz.yerek.aireply.ui.feature.home

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.NotificationsActive
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kz.yerek.aireply.R
import kz.yerek.aireply.push.NotificationPermission
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.design.AppCard
import kz.yerek.aireply.ui.design.LocalExtraColors
import kz.yerek.aireply.ui.design.PrimaryButton
import kz.yerek.aireply.ui.design.SecondaryButton
import kz.yerek.aireply.ui.design.Spacing
import kz.yerek.aireply.ui.feature.settings.rememberNotificationPermissionRequest

/**
 * The soft ask for notifications on Home.
 *
 * Хабарламаға жұмсақ шақыру: іске қосқанда емес, кіргеннен кейін ғана.
 *
 * Never at first launch: Home comes after sign-in. Shown only on Android 13+
 * where the system dialog can still appear, in a build with Firebase, against a
 * server that can deliver — see [kz.yerek.aireply.push.PushUiState.showsPrompt].
 * "Turn on" shows the system dialog; "Not now", or a denial, hides the card for
 * good (Settings ▸ Notifications stays available).
 */
@Composable
fun NotificationPromptCard(modifier: Modifier = Modifier) {
    val services = LocalServices.current
    val push = services.push
    val ui by push.ui.collectAsStateWithLifecycle()
    val account by services.account.state.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val requestPermission = rememberNotificationPermissionRequest(fromPrompt = true)

    if (!account.isSignedIn || !ui.showsPrompt) return

    AppCard(modifier = modifier) {
        Column(verticalArrangement = Arrangement.spacedBy(Spacing.s)) {
            Row(
                horizontalArrangement = Arrangement.spacedBy(Spacing.s),
                verticalAlignment = Alignment.Top
            ) {
                Icon(
                    Icons.Outlined.NotificationsActive,
                    contentDescription = null,
                    tint = MaterialTheme.colorScheme.primary,
                    modifier = Modifier.size(22.dp)
                )
                Column(verticalArrangement = Arrangement.spacedBy(Spacing.xxs)) {
                    Text(
                        stringResource(R.string.push_prompt_title),
                        style = MaterialTheme.typography.titleSmall
                    )
                    Text(
                        stringResource(R.string.push_prompt_body),
                        style = MaterialTheme.typography.bodyMedium,
                        color = LocalExtraColors.current.textSecondary
                    )
                }
            }
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(Spacing.s),
                verticalAlignment = Alignment.CenterVertically
            ) {
                SecondaryButton(
                    text = stringResource(R.string.push_prompt_later),
                    modifier = Modifier.weight(1f),
                    onClick = push::dismissPrompt
                )
                PrimaryButton(
                    text = stringResource(R.string.push_prompt_enable),
                    modifier = Modifier.weight(1f),
                    onClick = {
                        if (ui.canAskSystem) {
                            requestPermission()
                        } else {
                            // Debug-forced on an older Android, or blocked for good.
                            NotificationPermission.openSystemSettings(context)
                        }
                    }
                )
            }
        }
    }
}
