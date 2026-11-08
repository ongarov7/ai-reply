package kz.yerek.aireply.ui.feature.home

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.selection.toggleable
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.NotificationsActive
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.launch
import kz.yerek.aireply.R
import kz.yerek.aireply.push.NotificationPermission
import kz.yerek.aireply.push.NotificationPreferencesRepository
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.design.AppCard
import kz.yerek.aireply.ui.design.LocalExtraColors
import kz.yerek.aireply.ui.design.PrimaryButton
import kz.yerek.aireply.ui.design.SecondaryButton
import kz.yerek.aireply.ui.design.Spacing
import kz.yerek.aireply.ui.feature.settings.rememberNotificationPermissionRequest

/**
 * The soft ask for notifications on Home, with the account's own choice about
 * pack offers.
 *
 * Хабарламаға жұмсақ шақыру: іске қосқанда емес, кіргеннен кейін ғана.
 *
 * Never at first launch: Home comes after sign-in. Shown in a build with
 * Firebase, against a server that can deliver, where the system dialog can
 * still appear or where notifications are allowed but this account has not
 * opted in to offers — see [kz.yerek.aireply.push.PushUiState.showsPrompt].
 * Leaving the switch off never grants offers; a save that fails keeps the card
 * and says so, and the system dialog waits for it. Once answered («Не сейчас»,
 * «Готово», or the dialog) the card stays hidden for this account (Settings ▸
 * Notifications stays available).
 */
@Composable
fun NotificationPromptCard(modifier: Modifier = Modifier) {
    val services = LocalServices.current
    val push = services.push
    val ui by push.ui.collectAsStateWithLifecycle()
    val account by services.account.state.collectAsStateWithLifecycle()
    val categories by push.preferences.state.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val requestPermission = rememberNotificationPermissionRequest(fromPrompt = true)

    var receiveOffers by rememberSaveable { mutableStateOf(false) }
    var saving by remember { mutableStateOf(false) }
    var saveFailed by remember { mutableStateOf(false) }

    val offersOn = categories.isOn(NotificationPreferencesRepository.MARKETING)
    // A switch moves before the server answers: that must not take the card away yet.
    val shows = account.isSignedIn && (saving || ui.showsPrompt(offersOn))

    LaunchedEffect(shows) {
        if (shows && ui.hasCategories && !categories.isLoaded && !categories.loading) push.preferences.load()
    }

    if (!shows) return

    fun complete() {
        scope.launch {
            saving = true
            saveFailed = false
            val saved = !receiveOffers || offersOn ||
                push.preferences.set(NotificationPreferencesRepository.MARKETING, true)
            saving = false
            if (!saved) {
                saveFailed = true
                return@launch
            }
            when {
                ui.canAskSystem -> requestPermission()
                ui.canPost -> push.dismissPrompt()
                // Debug-forced on an older Android, or blocked for good.
                else -> NotificationPermission.openSystemSettings(context)
            }
        }
    }

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
                        stringResource(R.string.push_card_title),
                        style = MaterialTheme.typography.titleSmall
                    )
                    Text(
                        stringResource(R.string.push_card_body),
                        style = MaterialTheme.typography.bodyMedium,
                        color = LocalExtraColors.current.textSecondary
                    )
                }
            }
            if (ui.hasCategories && (!offersOn || saving)) {
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .toggleable(
                            value = receiveOffers,
                            enabled = !saving,
                            role = Role.Switch,
                            onValueChange = { receiveOffers = it }
                        ),
                    horizontalArrangement = Arrangement.spacedBy(Spacing.s),
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    Text(
                        stringResource(R.string.push_card_offers),
                        style = MaterialTheme.typography.bodyMedium,
                        modifier = Modifier.weight(1f)
                    )
                    Switch(checked = receiveOffers, onCheckedChange = null, enabled = !saving)
                }
                Text(
                    stringResource(R.string.push_card_offers_optional),
                    style = MaterialTheme.typography.bodySmall,
                    color = LocalExtraColors.current.textSecondary
                )
            }
            if (saveFailed) {
                Text(
                    stringResource(R.string.push_card_save_failed),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.error
                )
            }
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(Spacing.s),
                verticalAlignment = Alignment.CenterVertically
            ) {
                SecondaryButton(
                    text = stringResource(R.string.push_prompt_later),
                    modifier = Modifier.weight(1f),
                    enabled = !saving,
                    onClick = push::dismissPrompt
                )
                PrimaryButton(
                    text = stringResource(if (ui.canPost) R.string.common_done else R.string.push_prompt_enable),
                    modifier = Modifier.weight(1f),
                    enabled = !saving && !categories.loading,
                    onClick = { complete() }
                )
            }
        }
    }
}
