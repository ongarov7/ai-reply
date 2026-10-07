package kz.yerek.aireply.ui.feature.compose

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ContentCopy
import androidx.compose.material.icons.filled.ContentPaste
import androidx.compose.material.icons.filled.Mic
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
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
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kz.yerek.aireply.R
import kz.yerek.aireply.ai.AIConfiguration
import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.ai.AIReplyException
import kz.yerek.aireply.ai.AIReplyService
import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.core.lang.TemplateNaming
import kz.yerek.aireply.domain.model.ReplyConfiguration
import kz.yerek.aireply.domain.model.ReplyTemplate
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.common.AppScreen
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.design.AppCard
import kz.yerek.aireply.ui.design.AppSection
import kz.yerek.aireply.ui.design.LocalExtraColors
import kz.yerek.aireply.ui.design.PrimaryButton
import kz.yerek.aireply.ui.design.ReadableColumn
import kz.yerek.aireply.ui.design.Spacing
import kz.yerek.aireply.ui.feature.profile.SelectableRow
import kz.yerek.aireply.ui.feature.voice.DictationSheet

/**
 * The request "Try a reply" sends: the same one the keyboard sends, minus a
 * keyboard layout, because there is none here.
 *
 * The message goes exactly as pasted (the service only trims it), and nothing
 * here names a reply language: the reply follows the language of the incoming
 * message, so a Kazakh message gets a Kazakh reply whatever the phone's or the
 * app's language. [uiLanguage] only names the template the way the user saw it.
 */
internal fun tryReplyRequest(
    message: String,
    template: ReplyTemplate,
    configuration: ReplyConfiguration,
    uiLanguage: AppLanguage,
    instruction: String
): AIReplyService.Request = AIReplyService.Request(
    message = message,
    template = template,
    configuration = configuration,
    uiLanguage = uiLanguage,
    instruction = instruction,
    inputLanguage = null
)

/**
 * The in-app reply flow: paste or dictate a message, say who it is from, add an
 * instruction, generate.
 *
 * It exists for two reasons. It lets someone check that their profile and
 * templates produce the replies they expect without switching to WhatsApp to
 * find out. And it calls the SAME [AIReplyService] the keyboard calls, with the
 * same profile, the same template and the same working-hours context, so what
 * is seen here is what the keyboard will produce. A separate code path would
 * drift.
 */
@Composable
fun ComposeScreen(onBack: () -> Unit) {
    val services = LocalServices.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val configuration by services.configuration.configuration.collectAsStateWithLifecycle()

    var message by remember { mutableStateOf("") }
    var instruction by remember { mutableStateOf("") }
    var templateId by remember { mutableStateOf(configuration.visibleTemplates.firstOrNull()?.id) }
    var reply by remember { mutableStateOf("") }
    var generating by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var copied by remember { mutableStateOf(false) }
    var dictatingMessage by remember { mutableStateOf(false) }
    var dictatingInstruction by remember { mutableStateOf(false) }
    var job by remember { mutableStateOf<Job?>(null) }

    DisposableEffect(Unit) { onDispose { job?.cancel() } }

    val count = AIReplyService.characterCount(message)
    val overLimit = count > AIConfiguration.maxMessageCharacters
    val canGenerate = !generating && !overLimit && count > 0 && templateId != null

    fun generate() {
        val template = templateId?.let { configuration.template(it) } ?: return
        // One request at a time. A second tap replaces the first rather than
        // racing it, so rapid taps cannot produce two answers or two bills.
        job?.cancel()
        error = null
        reply = ""
        generating = true

        job = scope.launch {
            try {
                val generated = services.replyService.generate(
                    tryReplyRequest(
                        message = message,
                        template = template,
                        configuration = configuration,
                        uiLanguage = services.settings.effectiveAppLanguage,
                        instruction = instruction
                    )
                )
                reply = generated.text
                generating = false
            } catch (cancellation: CancellationException) {
                throw cancellation
            } catch (exception: Throwable) {
                generating = false
                val mapped = (exception as? AIReplyException)?.error ?: AIReplyError.ServiceUnavailable
                if (mapped != AIReplyError.Cancelled) {
                    error = services.strings(services.settings.effectiveAppLanguage).message(mapped)
                }
            }
        }
    }

    AppScreen(title = stringResource(R.string.compose_title), onBack = onBack) {
        ReadableColumn {

            AppSection(stringResource(R.string.compose_message)) {
                AppCard {
                    OutlinedTextField(
                        value = message,
                        onValueChange = { message = it },
                        placeholder = { Text(stringResource(R.string.compose_message_placeholder)) },
                        isError = overLimit,
                        modifier = Modifier
                            .fillMaxWidth()
                            .height(140.dp)
                    )
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        verticalAlignment = Alignment.CenterVertically
                    ) {
                        Text(
                            stringResource(
                                R.string.compose_counter,
                                count,
                                AIConfiguration.maxMessageCharacters
                            ),
                            style = MaterialTheme.typography.labelSmall,
                            color = if (overLimit) {
                                MaterialTheme.colorScheme.error
                            } else {
                                LocalExtraColors.current.textSecondary
                            },
                            modifier = Modifier.weight(1f)
                        )
                        TextButton(onClick = {
                            readClipboard(context)?.let { message = it }
                        }) {
                            Icon(
                                Icons.Filled.ContentPaste,
                                contentDescription = null,
                                modifier = Modifier.size(18.dp)
                            )
                            Text(
                                stringResource(R.string.compose_paste),
                                modifier = Modifier.padding(start = Spacing.xxs)
                            )
                        }
                        TextButton(onClick = { dictatingMessage = true }) {
                            Icon(
                                Icons.Filled.Mic,
                                contentDescription = null,
                                modifier = Modifier.size(18.dp)
                            )
                        }
                    }
                    if (overLimit) {
                        // The exact wording the keyboard shows, so the rule
                        // reads the same wherever the user meets it.
                        Text(
                            stringResource(
                                R.string.kb_err_message_too_long,
                                AIConfiguration.maxMessageCharacters
                            ),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.error
                        )
                    }
                }
            }

            AppSection(stringResource(R.string.compose_template)) {
                AppCard {
                    Column {
                        configuration.visibleTemplates.forEach { template ->
                            SelectableRow(
                                label = TemplateNaming.displayName(context, template),
                                selected = templateId == template.id
                            ) { templateId = template.id }
                        }
                    }
                }
            }

            AppSection(stringResource(R.string.compose_instruction)) {
                AppCard {
                    OutlinedTextField(
                        value = instruction,
                        onValueChange = { instruction = it },
                        placeholder = {
                            Text(stringResource(R.string.compose_instruction_placeholder))
                        },
                        modifier = Modifier.fillMaxWidth()
                    )
                    TextButton(onClick = { dictatingInstruction = true }) {
                        Icon(
                            Icons.Filled.Mic,
                            contentDescription = null,
                            modifier = Modifier.size(18.dp)
                        )
                        Text(
                            stringResource(R.string.compose_dictate_instruction),
                            modifier = Modifier.padding(start = Spacing.xs)
                        )
                    }
                }
            }

            Column(verticalArrangement = Arrangement.spacedBy(Spacing.s)) {
                PrimaryButton(
                    text = stringResource(R.string.compose_generate),
                    onClick = { generate() },
                    enabled = canGenerate
                )
                if (generating) {
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(Spacing.xs)
                    ) {
                        CircularProgressIndicator(
                            modifier = Modifier.size(16.dp),
                            strokeWidth = 2.dp
                        )
                        Text(
                            stringResource(R.string.kb_generating),
                            style = MaterialTheme.typography.bodySmall
                        )
                    }
                }
                error?.let {
                    Text(
                        it,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.error
                    )
                }
                Footnote(stringResource(R.string.compose_hint))
            }

            if (reply.isNotEmpty()) {
                AppSection(stringResource(R.string.compose_result)) {
                    AppCard {
                        Text(reply, style = MaterialTheme.typography.bodyLarge)
                        Row(
                            modifier = Modifier.fillMaxWidth(),
                            verticalAlignment = Alignment.CenterVertically
                        ) {
                            TextButton(onClick = {
                                writeClipboard(context, reply)
                                copied = true
                            }) {
                                Icon(
                                    Icons.Filled.ContentCopy,
                                    contentDescription = null,
                                    modifier = Modifier.size(18.dp)
                                )
                                Text(
                                    stringResource(
                                        if (copied) R.string.compose_copied else R.string.common_copy
                                    ),
                                    modifier = Modifier.padding(start = Spacing.xxs)
                                )
                            }
                            Spacer(Modifier.weight(1f))
                            TextButton(onClick = { generate() }, enabled = !generating) {
                                Icon(
                                    Icons.Filled.Refresh,
                                    contentDescription = null,
                                    modifier = Modifier.size(18.dp)
                                )
                                Text(
                                    stringResource(R.string.compose_regenerate),
                                    modifier = Modifier.padding(start = Spacing.xxs)
                                )
                            }
                        }
                    }
                }
            }
        }
    }

    if (copied) {
        androidx.compose.runtime.LaunchedEffect(Unit) {
            delay(2_000)
            copied = false
        }
    }

    if (dictatingMessage) {
        DictationSheet(
            language = services.settings.effectiveAppLanguage,
            onDismiss = { dictatingMessage = false },
            onAccept = { transcript -> message = append(message, transcript) }
        )
    }

    if (dictatingInstruction) {
        DictationSheet(
            language = services.settings.effectiveAppLanguage,
            onDismiss = { dictatingInstruction = false },
            onAccept = { transcript -> instruction = append(instruction, transcript) }
        )
    }
}

private fun append(existing: String, addition: String): String {
    val trimmed = addition.trim()
    if (trimmed.isEmpty()) return existing
    val separator = if (existing.isEmpty() || existing.endsWith(" ")) "" else " "
    return existing + separator + trimmed
}

private fun readClipboard(context: Context): String? {
    val clipboard = context.getSystemService(Context.CLIPBOARD_SERVICE) as? ClipboardManager
    return runCatching {
        clipboard?.primaryClip?.takeIf { it.itemCount > 0 }
            ?.getItemAt(0)?.coerceToText(context)?.toString()
    }.getOrNull()?.takeIf { it.isNotBlank() }
}

private fun writeClipboard(context: Context, text: String) {
    val clipboard = context.getSystemService(Context.CLIPBOARD_SERVICE) as? ClipboardManager
    runCatching {
        clipboard?.setPrimaryClip(
            ClipData.newPlainText(context.getString(R.string.app_name), text)
        )
    }
}
