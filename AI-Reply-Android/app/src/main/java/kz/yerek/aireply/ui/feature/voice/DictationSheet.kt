package kz.yerek.aireply.ui.feature.voice

import android.Manifest
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Mic
import androidx.compose.material.icons.filled.Stop
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kz.yerek.aireply.R
import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.ui.design.LocalExtraColors
import kz.yerek.aireply.ui.design.Spacing
import kz.yerek.aireply.voice.AndroidSpeechRecognitionClient
import kz.yerek.aireply.voice.MicPermission
import kz.yerek.aireply.voice.VoiceFailure
import kz.yerek.aireply.voice.VoiceState

/**
 * Dictation, presented as a sheet wherever text can be typed.
 *
 * The transcript is EDITABLE, not a read-only readout. Speech recognition
 * mishears names, numbers and Kazakh endings, and the fix belongs where the user
 * is already looking rather than three screens later. Recording stops as soon as
 * they start typing, so the recogniser cannot overwrite a correction.
 *
 * Unlike the keyboard, this is an Activity context, so the permission is
 * requested directly rather than through the transparent proxy Activity.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DictationSheet(
    language: AppLanguage,
    onDismiss: () -> Unit,
    onAccept: (String) -> Unit
) {
    val context = LocalContext.current
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)

    val client = remember { AndroidSpeechRecognitionClient(context) }
    val state by client.state.collectAsStateWithLifecycle()

    var transcript by remember { mutableStateOf("") }
    var isEditing by remember { mutableStateOf(false) }

    DisposableEffect(Unit) { onDispose { client.release() } }

    val permissionLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) { granted -> if (granted) client.start(language.languageTag) }

    // Recognised text becomes the editable draft; the user owns it from there.
    LaunchedEffect(state) {
        val current = state
        if (current is VoiceState.Done) {
            val separator = if (transcript.isEmpty() || transcript.endsWith(" ")) "" else " "
            transcript = transcript + separator + current.text
            client.reset()
        }
    }

    val listening = state is VoiceState.Listening || state is VoiceState.Starting

    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = sheetState) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = Spacing.l)
                .padding(bottom = Spacing.xl),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(Spacing.m)
        ) {
            Text(
                stringResource(R.string.voice_title),
                style = MaterialTheme.typography.headlineMedium
            )

            Text(
                text = statusMessage(state),
                style = MaterialTheme.typography.bodyMedium,
                color = LocalExtraColors.current.textSecondary,
                textAlign = TextAlign.Center
            )

            OutlinedTextField(
                value = transcript,
                onValueChange = {
                    transcript = it
                    if (!isEditing) {
                        isEditing = true
                        client.stop()
                    }
                },
                placeholder = { Text(stringResource(R.string.voice_placeholder)) },
                modifier = Modifier
                    .fillMaxWidth()
                    .height(180.dp)
            )

            Box(
                modifier = Modifier
                    .size(72.dp)
                    .clip(CircleShape)
                    .background(
                        if (listening) {
                            MaterialTheme.colorScheme.error
                        } else {
                            MaterialTheme.colorScheme.primary
                        }
                    )
                    .clickable {
                        isEditing = false
                        when {
                            listening -> client.stop()
                            !MicPermission.isGranted(context) ->
                                permissionLauncher.launch(Manifest.permission.RECORD_AUDIO)
                            else -> {
                                client.reset()
                                client.start(language.languageTag)
                            }
                        }
                    },
                contentAlignment = Alignment.Center
            ) {
                Icon(
                    if (listening) Icons.Filled.Stop else Icons.Filled.Mic,
                    contentDescription = stringResource(
                        if (listening) R.string.voice_stop else R.string.voice_taptospeak
                    ),
                    tint = Color.White,
                    modifier = Modifier.size(30.dp)
                )
            }

            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween
            ) {
                TextButton(onClick = {
                    client.cancel()
                    onDismiss()
                }) { Text(stringResource(R.string.common_cancel)) }

                TextButton(
                    enabled = transcript.isNotBlank(),
                    onClick = {
                        client.stop()
                        onAccept(transcript)
                        onDismiss()
                    }
                ) { Text(stringResource(R.string.common_save)) }
            }
        }
    }
}

@Composable
private fun statusMessage(state: VoiceState): String = when (state) {
    is VoiceState.Idle -> stringResource(R.string.voice_taptospeak)
    is VoiceState.Starting -> stringResource(R.string.voice_status_requesting)
    is VoiceState.Listening -> stringResource(R.string.voice_status_recording)
    is VoiceState.Processing -> stringResource(R.string.voice_kb_processing)
    is VoiceState.PermissionRequired -> stringResource(R.string.voice_kb_permission_needed)
    is VoiceState.PermissionDenied -> stringResource(R.string.voice_status_microphonedenied)
    is VoiceState.Done -> stringResource(R.string.voice_status_stopped)
    is VoiceState.Failed -> when (state.reason) {
        VoiceFailure.NO_SPEECH -> stringResource(R.string.voice_status_nospeech)
        VoiceFailure.NETWORK -> stringResource(R.string.kb_err_offline)
        VoiceFailure.LANGUAGE_UNAVAILABLE -> stringResource(
            R.string.voice_status_languageunavailable,
            AppLanguage.systemDefault().nativeName
        )
        VoiceFailure.UNAVAILABLE -> stringResource(R.string.voice_status_unavailable)
        VoiceFailure.BUSY -> stringResource(R.string.voice_kb_busy)
        VoiceFailure.GENERIC -> stringResource(R.string.voice_kb_failed)
    }
}
