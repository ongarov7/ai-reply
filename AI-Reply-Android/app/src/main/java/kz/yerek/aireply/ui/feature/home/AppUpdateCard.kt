package kz.yerek.aireply.ui.feature.home

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Download
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import kotlinx.coroutines.launch
import kz.yerek.aireply.BuildConfig
import kz.yerek.aireply.R
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.design.AppCard
import kz.yerek.aireply.ui.design.AppSection
import kz.yerek.aireply.ui.design.LocalExtraColors
import kz.yerek.aireply.ui.design.PrimaryButton
import kz.yerek.aireply.ui.design.SecondaryButton
import kz.yerek.aireply.ui.design.Spacing
import kz.yerek.aireply.update.AppUpdateController

/** «Доступно обновление» on Home: checked at most once a day. */
@Composable
internal fun AppUpdateCard() {
    val services = LocalServices.current
    val context = LocalContext.current
    val updates = services.updates
    val state by updates.state.collectAsStateWithLifecycle()
    val lifecycle = LocalLifecycleOwner.current.lifecycle

    LaunchedEffect(lifecycle) {
        lifecycle.repeatOnLifecycle(Lifecycle.State.RESUMED) { updates.checkIfDue() }
    }

    if (!state.showsCard) return

    AppCard {
        Column(verticalArrangement = Arrangement.spacedBy(Spacing.s)) {
            Row(horizontalArrangement = Arrangement.spacedBy(Spacing.s), verticalAlignment = Alignment.Top) {
                Icon(
                    Icons.Outlined.Download,
                    contentDescription = null,
                    tint = MaterialTheme.colorScheme.primary,
                    modifier = Modifier.padding(top = 2.dp).size(22.dp)
                )
                Column(verticalArrangement = Arrangement.spacedBy(Spacing.xxs)) {
                    Text(stringResource(R.string.updates_title), style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.SemiBold)
                    Text(
                        stringResource(R.string.updates_body),
                        style = MaterialTheme.typography.bodySmall,
                        color = LocalExtraColors.current.textSecondary
                    )
                }
            }
            Row(horizontalArrangement = Arrangement.spacedBy(Spacing.s), verticalAlignment = Alignment.CenterVertically) {
                PrimaryButton(stringResource(R.string.updates_update), modifier = Modifier.weight(1f)) { openStore(context, updates) }
                SecondaryButton(stringResource(R.string.updates_later), modifier = Modifier.weight(1f), onClick = updates::remindLater)
            }
            if (state.status == AppUpdateController.Status.OPEN_FAILED) Footnote(stringResource(R.string.updates_open_failed))
        }
    }
}

/** «О приложении»: the version, a check on demand, and the way to the update. */
@Composable
internal fun AppUpdateSettingsSection() {
    val services = LocalServices.current
    val context = LocalContext.current
    val updates = services.updates
    val state by updates.state.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    val checking = state.status == AppUpdateController.Status.CHECKING

    AppSection(stringResource(R.string.updates_about)) {
        AppCard {
            Column(verticalArrangement = Arrangement.spacedBy(Spacing.xs)) {
                Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                    Text(stringResource(R.string.updates_version), style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f))
                    Text(
                        "${BuildConfig.VERSION_NAME} (${BuildConfig.VERSION_CODE})",
                        style = MaterialTheme.typography.bodyMedium,
                        color = LocalExtraColors.current.textSecondary
                    )
                }
                TextButton(
                    onClick = { scope.launch { updates.checkNow() } },
                    enabled = !checking,
                    modifier = Modifier.fillMaxWidth()
                ) {
                    if (checking) CircularProgressIndicator(modifier = Modifier.padding(end = Spacing.xs).size(16.dp), strokeWidth = 2.dp)
                    Text(stringResource(if (checking) R.string.updates_checking else R.string.updates_check))
                }
                if (state.offeredVersion != null) {
                    PrimaryButton(stringResource(R.string.updates_update)) { openStore(context, updates) }
                    Footnote(stringResource(R.string.updates_body))
                }
                statusText(state.status)?.let { Footnote(stringResource(it)) }
            }
        }
    }
}

private fun statusText(status: AppUpdateController.Status): Int? = when (status) {
    AppUpdateController.Status.CURRENT -> R.string.updates_current
    AppUpdateController.Status.OFFLINE -> R.string.updates_offline
    AppUpdateController.Status.UNAVAILABLE -> R.string.updates_unavailable
    AppUpdateController.Status.OPEN_FAILED -> R.string.updates_open_failed
    AppUpdateController.Status.IDLE, AppUpdateController.Status.CHECKING -> null
}

/** The app's page on Google Play; the browser's copy of it where the Play app is missing. */
private fun openStore(context: Context, updates: AppUpdateController) {
    val pkg = context.packageName
    val play = Intent(Intent.ACTION_VIEW, Uri.parse("market://details?id=$pkg")).setPackage("com.android.vending")
    val web = Intent(Intent.ACTION_VIEW, Uri.parse("https://play.google.com/store/apps/details?id=$pkg"))
    for (intent in listOf(play, web)) {
        try {
            context.startActivity(intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
            return
        } catch (_: ActivityNotFoundException) {
            continue
        }
    }
    updates.storeDidNotOpen()
}
