package kz.yerek.aireply.ui.feature.account

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.launch
import kz.yerek.aireply.R
import kz.yerek.aireply.domain.model.ReplyTone
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.design.AppSection
import kz.yerek.aireply.ui.design.PrimaryButton
import kz.yerek.aireply.ui.design.ReadableColumn
import kz.yerek.aireply.ui.design.SecondaryButton
import kz.yerek.aireply.ui.design.Spacing

/**
 * The short profile step a brand-new account goes through.
 *
 * Жаңа тіркелгі: ең қажетті екі-үш сұрақ қана.
 *
 * Deliberately three fields. Templates, working hours and business rules all
 * have their own screens and are better filled in later by someone who has seen
 * a reply first.
 *
 * Shown outside the navigation scaffold, so it keeps clear of the system bars
 * and the keyboard itself, and scrolls: with the keyboard up on a small phone
 * the three fields and Finish do not fit.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun RegistrationStepScreen(onFinished: () -> Unit) {
    val services = LocalServices.current
    val account = services.account
    val state by account.state.collectAsStateWithLifecycle()
    val configuration by services.configuration.configuration.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()

    var role by remember { mutableStateOf(configuration.profile.role) }
    var description by remember { mutableStateOf(configuration.profile.descriptionText) }
    var tone by remember { mutableStateOf(configuration.profile.preferredTone) }

    ReadableColumn(
        modifier = Modifier
            .fillMaxSize()
            .windowInsetsPadding(WindowInsets.safeDrawing)
            .imePadding()
            .verticalScroll(rememberScrollState())
    ) {
        Column(verticalArrangement = Arrangement.spacedBy(Spacing.xs)) {
            Text(
                stringResource(R.string.registration_title),
                style = MaterialTheme.typography.headlineSmall
            )
            Text(
                stringResource(R.string.registration_subtitle),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant
            )
        }

        AppSection(stringResource(R.string.profile_role)) {
            OutlinedTextField(
                value = role,
                onValueChange = { role = it },
                singleLine = true,
                modifier = Modifier.fillMaxWidth()
            )
        }

        AppSection(stringResource(R.string.profile_description)) {
            OutlinedTextField(
                value = description,
                onValueChange = { description = it },
                minLines = 3,
                modifier = Modifier.fillMaxWidth()
            )
        }

        AppSection(stringResource(R.string.profile_tone)) {
            FlowRow(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(Spacing.xs),
                verticalArrangement = Arrangement.spacedBy(Spacing.xs)
            ) {
                ReplyTone.entries.forEach { option ->
                    FilterChip(
                        selected = tone == option,
                        onClick = { tone = option },
                        label = { Text(stringResource(toneLabel(option))) }
                    )
                }
            }
        }

        state.errorMessage?.let { message ->
            Text(
                stringResource(message),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.error
            )
        }

        PrimaryButton(
            text = stringResource(R.string.registration_finish),
            enabled = !state.busy,
            onClick = {
                scope.launch {
                    // The local profile is what the prompt builder reads; the
                    // server copy is what a future device restores from. Both
                    // are written, once.
                    services.configuration.updateProfile { profile ->
                        profile.copy(
                            role = role,
                            descriptionText = description,
                            preferredTone = tone
                        )
                    }
                    val saved = account.completeRegistration(
                        role = role,
                        description = description,
                        tone = tone.raw,
                        locale = services.settings.effectiveAppLanguage.code
                    )
                    if (saved) onFinished()
                }
            }
        )

        SecondaryButton(
            text = stringResource(R.string.registration_skip),
            enabled = !state.busy,
            onClick = onFinished,
            modifier = Modifier.fillMaxWidth()
        )
    }
}

private fun toneLabel(tone: ReplyTone): Int = when (tone) {
    ReplyTone.NATURAL -> R.string.tone_natural
    ReplyTone.FRIENDLY -> R.string.tone_friendly
    ReplyTone.PROFESSIONAL -> R.string.tone_professional
    ReplyTone.FORMAL -> R.string.tone_formal
    ReplyTone.SHORT -> R.string.tone_short
}
