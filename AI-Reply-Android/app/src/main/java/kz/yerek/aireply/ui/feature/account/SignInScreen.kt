package kz.yerek.aireply.ui.feature.account

import android.app.Activity
import android.content.Context
import android.content.ContextWrapper
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Email
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.LocalContentColor
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
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
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.launch
import kz.yerek.aireply.R
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.design.AppMark
import kz.yerek.aireply.ui.design.AuthColumn
import kz.yerek.aireply.ui.design.Layout
import kz.yerek.aireply.ui.design.Radius
import kz.yerek.aireply.ui.design.Spacing

/**
 * The first sign-in screen: Google or e-mail. Android has no Apple option.
 *
 * Кіру: Google немесе пошта. Құпиясөз жоқ.
 *
 * @param onSignedIn called with `true` when the account was created just now.
 */
@Composable
fun SignInScreen(onSignedIn: (isNewUser: Boolean) -> Unit) {
    val services = LocalServices.current
    val account = services.account
    val state by account.state.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    val context = LocalContext.current

    LaunchedEffect(Unit) { account.loadServerConfig() }

    AuthColumn {
        Column(verticalArrangement = Arrangement.spacedBy(Spacing.s)) {
            AppMark()
            Text(
                stringResource(R.string.account_sign_in_title),
                style = MaterialTheme.typography.headlineSmall
            )
            Text(
                stringResource(R.string.account_sign_in_subtitle),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant
            )
        }

        Column(verticalArrangement = Arrangement.spacedBy(Spacing.s)) {
            if (account.offersGoogle) {
                SignInMethodButton(
                    text = stringResource(R.string.account_continue_with_google),
                    enabled = !state.busy,
                    icon = { LetterMark("G") },
                    onClick = {
                        val activity = context.findActivity() ?: return@SignInMethodButton
                        scope.launch {
                            val outcome = account.signInWithGoogle(activity)
                            if (outcome is AccountController.SignInOutcome.SignedIn) onSignedIn(outcome.isNewUser)
                        }
                    }
                )
            }
            SignInMethodButton(
                text = stringResource(R.string.account_continue_with_email),
                enabled = !state.busy,
                icon = { Icon(Icons.Outlined.Email, contentDescription = null, modifier = Modifier.size(20.dp)) },
                onClick = account::startEmailSignIn
            )
        }

        if (state.busy) {
            CircularProgressIndicator(modifier = Modifier.align(Alignment.CenterHorizontally))
        }

        state.errorMessage?.let { message ->
            Text(
                stringResource(message),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.error
            )
        }

        Footnote(stringResource(R.string.account_legal_footer))
    }
}

/** One sign-in method: an outlined, full-width button with a leading mark. */
@Composable
private fun SignInMethodButton(
    text: String,
    enabled: Boolean,
    icon: @Composable () -> Unit,
    onClick: () -> Unit
) {
    OutlinedButton(
        onClick = onClick,
        enabled = enabled,
        shape = RoundedCornerShape(Radius.medium),
        border = BorderStroke(1.dp, MaterialTheme.colorScheme.outline),
        modifier = Modifier
            .fillMaxWidth()
            .defaultMinSize(minHeight = Layout.minimumTouchTarget + 2.dp)
    ) {
        icon()
        Spacer(Modifier.width(Spacing.s))
        Text(text, style = MaterialTheme.typography.labelLarge)
    }
}

/**
 * A plain letter in a circle. Deliberately not Google's logo artwork: the
 * official asset can replace it once it is added to the project.
 */
@Composable
private fun LetterMark(letter: String) {
    val color = LocalContentColor.current
    Box(
        contentAlignment = Alignment.Center,
        modifier = Modifier
            .size(20.dp)
            .border(1.5.dp, color, CircleShape)
    ) {
        Text(letter, color = color, fontSize = 11.sp, fontWeight = FontWeight.Bold)
    }
}

/** The Activity behind a (possibly wrapped) Context; Credential Manager needs one. */
internal tailrec fun Context.findActivity(): Activity? = when (this) {
    is Activity -> this
    is ContextWrapper -> baseContext.findActivity()
    else -> null
}
