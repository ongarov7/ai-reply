package kz.yerek.aireply.ui.feature.account

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.OpenInNew
import androidx.compose.material.icons.filled.Description
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.launch
import kz.yerek.aireply.R
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.common.openWebPage
import kz.yerek.aireply.ui.design.AppMark
import kz.yerek.aireply.ui.design.AuthColumn
import kz.yerek.aireply.ui.design.PrimaryButton
import kz.yerek.aireply.ui.design.Spacing

/**
 * The terms, the privacy policy and the AI processing, accepted before
 * anything else. Two separate boxes: the documents, and the texts going to
 * OpenAI; Continue needs both. It shows again when the versions change, when
 * the consent is withdrawn in Settings, and after the account is deleted.
 *
 * Келісімсіз де шығуға және тіркелгіні жоюға болады.
 * A signed-in user who does not consent is not trapped here: Sign out and
 * Delete account sit under Continue, with Settings' own confirmations, so
 * neither needs the texts-to-OpenAI box ticked first.
 */
@Composable
fun LegalConsentScreen() {
    val services = LocalServices.current
    val state by services.account.state.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var accepted by remember { mutableStateOf(false) }
    var acceptedAi by remember { mutableStateOf(false) }
    var confirmingSignOut by remember { mutableStateOf(false) }
    var confirmingDeletion by remember { mutableStateOf(false) }

    fun open(rawUrl: String) = context.openWebPage(rawUrl, services.settings.effectiveAppLanguage.code)

    // Back here because the server refused an AI request: its newest versions first.
    LaunchedEffect(Unit) { services.account.recheckLegalVersions() }

    AuthColumn {
        Column(verticalArrangement = Arrangement.spacedBy(Spacing.s)) {
            AppMark(size = 56.dp)
            state.notice?.let { notice ->
                Text(
                    stringResource(notice),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.primary
                )
            }
            Text(stringResource(R.string.legal_consent_title), style = MaterialTheme.typography.headlineSmall)
            Footnote(stringResource(R.string.legal_consent_body))
        }

        Column(modifier = Modifier.fillMaxWidth()) {
            LegalLink(stringResource(R.string.legal_terms)) { open(state.legalConfig.termsUrl) }
            LegalLink(stringResource(R.string.legal_privacy)) { open(state.legalConfig.privacyUrl) }
        }

        // Named with the keyboard's own button labels, like the privacy note in Settings.
        Text(
            stringResource(
                R.string.legal_consent_ai_disclosure,
                stringResource(R.string.kb_generate),
                stringResource(R.string.kb_compose_write)
            ),
            style = MaterialTheme.typography.bodyMedium
        )

        Column(verticalArrangement = Arrangement.spacedBy(Spacing.s)) {
            ConsentBox(stringResource(R.string.legal_consent_checkbox), accepted) { accepted = it }
            ConsentBox(stringResource(R.string.legal_consent_ai_checkbox), acceptedAi) { acceptedAi = it }
        }

        PrimaryButton(
            text = stringResource(R.string.legal_consent_continue),
            enabled = accepted && acceptedAi && !state.busy,
            onClick = {
                scope.launch {
                    services.account.acceptLegal(services.settings.effectiveAppLanguage.code)
                }
            }
        )

        Footnote(stringResource(R.string.legal_consent_footer))

        if (state.isSignedIn) {
            AccountExits(
                state = state,
                onSignOut = { confirmingSignOut = true },
                onDelete = {
                    services.account.clearDeletionFailure()
                    confirmingDeletion = true
                }
            )
        }
    }

    if (confirmingSignOut) {
        SignOutDialog(
            onConfirm = {
                confirmingSignOut = false
                scope.launch { services.account.signOut() }
            },
            onDismiss = { confirmingSignOut = false }
        )
    }

    if (confirmingDeletion) {
        DeleteAccountDialog(
            onConfirm = {
                confirmingDeletion = false
                // On success this screen stays and says the account is gone.
                scope.launch { services.account.deleteAccount() }
            },
            onDismiss = { confirmingDeletion = false }
        )
    }
}

/** Sign out and Delete account, quieter than Continue. */
@Composable
private fun AccountExits(state: AccountController.State, onSignOut: () -> Unit, onDelete: () -> Unit) {
    // Unknown features (no answer yet) offer it, as in Settings.
    val offersDeletion = state.features?.accountDeletion ?: true
    Column(modifier = Modifier.fillMaxWidth(), horizontalAlignment = Alignment.CenterHorizontally) {
        TextButton(onClick = onSignOut, enabled = !state.busy) {
            Text(stringResource(R.string.settings_account_sign_out))
        }
        if (offersDeletion) {
            TextButton(onClick = onDelete, enabled = !state.busy) {
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(Spacing.s)
                ) {
                    if (state.deletingAccount) {
                        CircularProgressIndicator(
                            modifier = Modifier.size(16.dp),
                            strokeWidth = 2.dp,
                            color = MaterialTheme.colorScheme.error
                        )
                    }
                    Text(
                        stringResource(R.string.settings_account_delete),
                        color = MaterialTheme.colorScheme.error
                    )
                }
            }
        }
        if (state.accountDeletionFailed) {
            Text(
                stringResource(R.string.account_delete_failed),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.error
            )
        }
    }
}

@Composable
private fun ConsentBox(text: String, checked: Boolean, onChange: (Boolean) -> Unit) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        verticalAlignment = Alignment.Top,
        horizontalArrangement = Arrangement.spacedBy(Spacing.s)
    ) {
        Checkbox(checked = checked, onCheckedChange = onChange)
        Text(text, style = MaterialTheme.typography.bodyMedium)
    }
}

@Composable
private fun LegalLink(text: String, onClick: () -> Unit) {
    TextButton(onClick = onClick, modifier = Modifier.fillMaxWidth()) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(Spacing.s)
        ) {
            Icon(Icons.Filled.Description, contentDescription = null)
            Text(text, modifier = Modifier.weight(1f))
            Icon(Icons.AutoMirrored.Filled.OpenInNew, contentDescription = null)
        }
    }
}
