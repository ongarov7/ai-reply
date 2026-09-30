package kz.yerek.aireply.ui.feature.account

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.Logout
import androidx.compose.material.icons.filled.CreditCard
import androidx.compose.material.icons.outlined.Email
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.launch
import kz.yerek.aireply.R
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.common.NavigationRow
import kz.yerek.aireply.ui.common.RowDividerIndented
import kz.yerek.aireply.ui.common.RowGroup
import kz.yerek.aireply.ui.design.AppCard
import kz.yerek.aireply.ui.design.AppSection

/**
 * The account block inside Settings.
 *
 * Баптаулардағы тіркелгі бөлімі: тариф, квота, шығу.
 *
 * Shows what the user needs to recognise their account and nothing more: the
 * e-mail it signs in with, the plan, what is left today, and the way out. An
 * account opened with a phone number is offered to add an e-mail, because
 * phone sign-in no longer exists.
 */
@Composable
fun AccountSection(onOpenSubscription: () -> Unit) {
    val services = LocalServices.current
    val account = services.account
    val state by account.state.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    var confirming by remember { mutableStateOf(false) }
    var addingEmail by remember { mutableStateOf(false) }
    val needsEmail = state.isSignedIn && state.user?.needsEmail == true

    LaunchedEffect(Unit) { account.refresh() }

    AppSection(stringResource(R.string.settings_account)) {
        if (state.isSignedIn) {
            AppCard {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    Text(
                        stringResource(R.string.settings_account_identifier),
                        style = MaterialTheme.typography.bodyMedium
                    )
                    Text(
                        account.displayIdentifier,
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant
                    )
                }
            }

            RowGroup {
                if (needsEmail) {
                    NavigationRow(
                        Icons.Outlined.Email,
                        stringResource(R.string.settings_account_add_email)
                    ) { addingEmail = true }
                    RowDividerIndented()
                }
                NavigationRow(
                    Icons.Filled.CreditCard,
                    stringResource(R.string.settings_account_plan),
                    value = planSummary(state, services.settings.effectiveAppLanguage.code)
                ) { onOpenSubscription() }
                RowDividerIndented()
                NavigationRow(
                    Icons.AutoMirrored.Filled.Logout,
                    stringResource(R.string.settings_account_sign_out)
                ) { confirming = true }
            }
        } else {
            AppCard {
                Text(
                    stringResource(R.string.settings_account_signed_out),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant
                )
            }
        }
        Footnote(
            stringResource(
                if (needsEmail) R.string.settings_account_add_email_footer else R.string.settings_account_footer
            )
        )
    }

    if (addingEmail) {
        LinkEmailDialog(onDismiss = { addingEmail = false })
    }

    if (confirming) {
        AlertDialog(
            onDismissRequest = { confirming = false },
            title = { Text(stringResource(R.string.settings_account_sign_out_confirm)) },
            confirmButton = {
                TextButton(onClick = {
                    confirming = false
                    scope.launch { account.signOut() }
                }) { Text(stringResource(R.string.settings_account_sign_out)) }
            },
            dismissButton = {
                TextButton(onClick = { confirming = false }) {
                    Text(stringResource(R.string.common_cancel))
                }
            }
        )
    }
}

private fun planSummary(state: AccountController.State, language: String): String {
    val plan = state.subscription?.plan ?: return ""
    val name = plan.localizedName(language)
    if (state.usage.dailyLimit <= 0) return name
    return "$name · ${state.usage.remainingToday}/${state.usage.dailyLimit}"
}
