package kz.yerek.aireply.ui.feature.account

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kz.yerek.aireply.R
import kz.yerek.aireply.data.account.OtpCode
import kz.yerek.aireply.data.account.ResendCountdown
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.design.AuthColumn
import kz.yerek.aireply.ui.design.PrimaryButton
import kz.yerek.aireply.ui.design.Radius
import kz.yerek.aireply.ui.design.Spacing

/**
 * Step two of e-mail sign-in: the 4-digit code.
 *
 * Растау коды: 4 цифр. Қайта жіберу — сервер рұқсат еткенде ғана.
 *
 * @param onVerified called with `true` when the account was created just now.
 */
@Composable
fun VerifyCodeScreen(
    phase: AccountController.Phase.AwaitingCode,
    onVerified: (isNewUser: Boolean) -> Unit
) {
    val services = LocalServices.current
    val account = services.account
    val state by account.state.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    val focus = remember { FocusRequester() }

    var code by rememberSaveable(phase.email) { mutableStateOf("") }
    var now by remember { mutableLongStateOf(System.currentTimeMillis()) }
    val secondsLeft = ResendCountdown.secondsRemaining(phase.resendAvailableAt, now)
    val fieldDescription = stringResource(R.string.account_code_field)

    // Ticks only while a countdown is showing.
    LaunchedEffect(phase.resendAvailableAt) {
        now = System.currentTimeMillis()
        while (now < phase.resendAvailableAt) {
            delay(250)
            now = System.currentTimeMillis()
        }
    }
    LaunchedEffect(Unit) { runCatching { focus.requestFocus() } }
    BackHandler(enabled = !state.busy) { account.editEmail() }

    fun submit(value: String) {
        if (value.length != OtpCode.LENGTH || state.busy) return
        scope.launch {
            when (val outcome = account.verifyEmailCode(value)) {
                is AccountController.SignInOutcome.SignedIn -> onVerified(outcome.isNewUser)
                is AccountController.SignInOutcome.Failed -> if (outcome.clearCode) code = ""
                AccountController.SignInOutcome.Cancelled -> Unit
            }
        }
    }

    AuthColumn {
        Column(verticalArrangement = Arrangement.spacedBy(Spacing.xs)) {
            Text(
                stringResource(R.string.account_code_title),
                style = MaterialTheme.typography.headlineSmall
            )
            Text(
                stringResource(R.string.account_code_sent_to, phase.email),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant
            )
        }

        OutlinedTextField(
            value = code,
            onValueChange = { value ->
                val digits = OtpCode.sanitize(value)
                code = digits
                if (digits.length == OtpCode.LENGTH) submit(digits)
            },
            singleLine = true,
            isError = state.errorMessage != null,
            textStyle = TextStyle(fontSize = 28.sp, letterSpacing = 8.sp, textAlign = TextAlign.Center),
            keyboardOptions = KeyboardOptions(
                keyboardType = KeyboardType.NumberPassword,
                imeAction = ImeAction.Done,
                autoCorrectEnabled = false
            ),
            keyboardActions = KeyboardActions(onDone = { submit(code) }),
            shape = RoundedCornerShape(Radius.medium),
            modifier = Modifier
                .fillMaxWidth()
                .focusRequester(focus)
                .semantics { contentDescription = fieldDescription }
        )

        state.errorMessage?.let { message ->
            Text(
                stringResource(message),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.error
            )
        }

        PrimaryButton(
            text = stringResource(R.string.account_code_confirm),
            enabled = code.length == OtpCode.LENGTH && !state.busy,
            onClick = { submit(code) }
        )

        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically
        ) {
            TextButton(
                enabled = secondsLeft == 0 && !state.busy,
                onClick = {
                    code = ""
                    scope.launch { account.resendEmailCode(services.settings.effectiveAppLanguage.code) }
                }
            ) {
                Text(
                    if (secondsLeft > 0) {
                        stringResource(R.string.account_code_resend_in, secondsLeft)
                    } else {
                        stringResource(R.string.account_code_resend)
                    }
                )
            }
            TextButton(enabled = !state.busy, onClick = account::editEmail) {
                Text(stringResource(R.string.account_code_change_email))
            }
        }
    }
}
