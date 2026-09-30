package kz.yerek.aireply.ui.feature.account

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.launch
import kz.yerek.aireply.R
import kz.yerek.aireply.data.account.EmailAddress
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.design.AuthColumn
import kz.yerek.aireply.ui.design.PrimaryButton
import kz.yerek.aireply.ui.design.Radius
import kz.yerek.aireply.ui.design.SecondaryButton
import kz.yerek.aireply.ui.design.Spacing

/**
 * Step one of e-mail sign-in: the address.
 *
 * Пошта мекенжайы: оған 4 таңбалы код жіберіледі.
 */
@Composable
fun EmailSignInScreen() {
    val services = LocalServices.current
    val account = services.account
    val state by account.state.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    val focus = remember { FocusRequester() }

    var email by rememberSaveable { mutableStateOf(state.pendingEmail) }
    val plausible = EmailAddress.isPlausible(email)

    BackHandler(enabled = !state.busy) { account.cancelEmailSignIn() }
    LaunchedEffect(Unit) { runCatching { focus.requestFocus() } }

    fun submit() {
        if (!plausible || state.busy) return
        scope.launch { account.requestEmailCode(email, services.settings.effectiveAppLanguage.code) }
    }

    AuthColumn {
        Column(verticalArrangement = Arrangement.spacedBy(Spacing.xs)) {
            Text(
                stringResource(R.string.account_email_title),
                style = MaterialTheme.typography.headlineSmall
            )
            Text(
                stringResource(R.string.account_email_subtitle),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant
            )
        }

        OutlinedTextField(
            value = email,
            onValueChange = { email = it.trimStart() },
            label = { Text(stringResource(R.string.account_email_placeholder)) },
            singleLine = true,
            isError = state.errorMessage != null,
            keyboardOptions = KeyboardOptions(
                keyboardType = KeyboardType.Email,
                imeAction = ImeAction.Go,
                autoCorrectEnabled = false
            ),
            keyboardActions = KeyboardActions(onGo = { submit() }),
            shape = RoundedCornerShape(Radius.medium),
            modifier = Modifier
                .fillMaxWidth()
                .focusRequester(focus)
        )

        state.errorMessage?.let { message ->
            Text(
                stringResource(message),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.error
            )
        }

        Column(verticalArrangement = Arrangement.spacedBy(Spacing.xs)) {
            PrimaryButton(
                text = stringResource(R.string.account_continue),
                enabled = plausible && !state.busy,
                onClick = ::submit
            )
            SecondaryButton(
                text = stringResource(R.string.common_back),
                enabled = !state.busy,
                modifier = Modifier.fillMaxWidth(),
                onClick = account::cancelEmailSignIn
            )
        }
    }
}
