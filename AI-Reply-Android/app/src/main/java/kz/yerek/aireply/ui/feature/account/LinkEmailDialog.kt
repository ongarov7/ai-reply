package kz.yerek.aireply.ui.feature.account

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.AlertDialog
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
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kz.yerek.aireply.R
import kz.yerek.aireply.data.account.EmailAddress
import kz.yerek.aireply.data.account.OtpCode
import kz.yerek.aireply.data.account.ResendCountdown
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.design.Spacing

/**
 * "Add e-mail for sign-in", for accounts opened with a phone number.
 *
 * Телефонмен ашылған тіркелгіге пошта қосу: кейін сол поштамен кіруге болады.
 *
 * Phone sign-in is no longer offered, so an account that only has a number
 * would be unreachable after signing out. Proving an address once, while still
 * signed in, keeps the account — its plan, quota and history — usable.
 */
@Composable
fun LinkEmailDialog(onDismiss: () -> Unit) {
    val services = LocalServices.current
    val account = services.account
    val scope = rememberCoroutineScope()
    val language = services.settings.effectiveAppLanguage.code

    var email by rememberSaveable { mutableStateOf("") }
    var code by rememberSaveable { mutableStateOf("") }
    var sentTo by rememberSaveable { mutableStateOf<String?>(null) }
    var resendAt by rememberSaveable { mutableLongStateOf(0L) }
    var errorMessage by remember { mutableStateOf<Int?>(null) }
    var busy by remember { mutableStateOf(false) }
    var now by remember { mutableLongStateOf(System.currentTimeMillis()) }
    val fieldDescription = stringResource(R.string.account_code_field)

    LaunchedEffect(resendAt) {
        now = System.currentTimeMillis()
        while (now < resendAt) {
            delay(250)
            now = System.currentTimeMillis()
        }
    }

    fun requestCode(address: String) {
        if (busy || !EmailAddress.isPlausible(address)) return
        busy = true
        errorMessage = null
        scope.launch {
            try {
                resendAt = account.requestLinkEmailCode(address, language)
                sentTo = EmailAddress.normalized(address)
                code = ""
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (failure: Throwable) {
                errorMessage = AccountController.messageFor(failure)
            } finally {
                busy = false
            }
        }
    }

    fun verify(value: String) {
        val address = sentTo ?: return
        if (busy || value.length != OtpCode.LENGTH) return
        busy = true
        errorMessage = null
        scope.launch {
            try {
                account.verifyLinkEmailCode(address, value)
                onDismiss()
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (failure: Throwable) {
                errorMessage = AccountController.messageFor(failure)
                if (AccountController.codeIsSpent(failure)) code = ""
            } finally {
                busy = false
            }
        }
    }

    val address = sentTo
    AlertDialog(
        onDismissRequest = { if (!busy) onDismiss() },
        title = { Text(stringResource(R.string.settings_account_add_email)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(Spacing.s)) {
                if (address == null) {
                    Text(
                        stringResource(R.string.settings_account_add_email_body),
                        style = MaterialTheme.typography.bodyMedium
                    )
                    OutlinedTextField(
                        value = email,
                        onValueChange = { email = it.trimStart() },
                        label = { Text(stringResource(R.string.account_email_placeholder)) },
                        singleLine = true,
                        isError = errorMessage != null,
                        keyboardOptions = KeyboardOptions(
                            keyboardType = KeyboardType.Email,
                            imeAction = ImeAction.Go,
                            autoCorrectEnabled = false
                        ),
                        keyboardActions = KeyboardActions(onGo = { requestCode(email) }),
                        modifier = Modifier.fillMaxWidth()
                    )
                } else {
                    Text(
                        stringResource(R.string.account_code_sent_to, address),
                        style = MaterialTheme.typography.bodyMedium
                    )
                    OutlinedTextField(
                        value = code,
                        onValueChange = { value ->
                            val digits = OtpCode.sanitize(value)
                            code = digits
                            if (digits.length == OtpCode.LENGTH) verify(digits)
                        },
                        singleLine = true,
                        isError = errorMessage != null,
                        textStyle = TextStyle(fontSize = 24.sp, letterSpacing = 6.sp, textAlign = TextAlign.Center),
                        keyboardOptions = KeyboardOptions(
                            keyboardType = KeyboardType.NumberPassword,
                            imeAction = ImeAction.Done,
                            autoCorrectEnabled = false
                        ),
                        keyboardActions = KeyboardActions(onDone = { verify(code) }),
                        modifier = Modifier
                            .fillMaxWidth()
                            .semantics { contentDescription = fieldDescription }
                    )
                    val secondsLeft = ResendCountdown.secondsRemaining(resendAt, now)
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.SpaceBetween,
                        verticalAlignment = Alignment.CenterVertically
                    ) {
                        TextButton(
                            enabled = secondsLeft == 0 && !busy,
                            onClick = { requestCode(address) }
                        ) {
                            Text(
                                if (secondsLeft > 0) {
                                    stringResource(R.string.account_code_resend_in, secondsLeft)
                                } else {
                                    stringResource(R.string.account_code_resend)
                                }
                            )
                        }
                        TextButton(
                            enabled = !busy,
                            onClick = {
                                sentTo = null
                                code = ""
                                errorMessage = null
                            }
                        ) { Text(stringResource(R.string.account_code_change_email)) }
                    }
                }
                errorMessage?.let { message ->
                    Text(
                        stringResource(message),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.error
                    )
                }
            }
        },
        confirmButton = {
            if (address == null) {
                TextButton(
                    enabled = !busy && EmailAddress.isPlausible(email),
                    onClick = { requestCode(email) }
                ) { Text(stringResource(R.string.account_continue)) }
            } else {
                TextButton(
                    enabled = !busy && code.length == OtpCode.LENGTH,
                    onClick = { verify(code) }
                ) { Text(stringResource(R.string.account_code_confirm)) }
            }
        },
        dismissButton = {
            TextButton(enabled = !busy, onClick = onDismiss) {
                Text(stringResource(R.string.common_cancel))
            }
        }
    )
}
