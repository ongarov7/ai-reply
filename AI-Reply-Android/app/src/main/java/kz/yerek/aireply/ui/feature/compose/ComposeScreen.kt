package kz.yerek.aireply.ui.feature.compose

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.ContentPaste
import androidx.compose.material.icons.outlined.Flag
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material.icons.outlined.Lightbulb
import androidx.compose.material.icons.outlined.Mic
import androidx.compose.material3.AssistChip
import androidx.compose.material3.AssistChipDefaults
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kz.yerek.aireply.BuildConfig
import kz.yerek.aireply.R
import kz.yerek.aireply.ai.AIConfiguration
import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.ai.AIReplyException
import kz.yerek.aireply.ai.AIReplyService
import kz.yerek.aireply.ai.AIReportController
import kz.yerek.aireply.ai.AIReportMode
import kz.yerek.aireply.analytics.ProductEvent
import kz.yerek.aireply.analytics.ProductEvents
import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.core.lang.TemplateNaming
import kz.yerek.aireply.domain.model.ReplyConfiguration
import kz.yerek.aireply.domain.model.ReplyTemplate
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.common.AppClipboard
import kz.yerek.aireply.ui.common.AppScreen
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.common.RecipientGrid
import kz.yerek.aireply.ui.common.RecipientOption
import kz.yerek.aireply.ui.common.RelationshipIcons
import kz.yerek.aireply.ui.design.AppCard
import kz.yerek.aireply.ui.design.AppSection
import kz.yerek.aireply.ui.design.Layout
import kz.yerek.aireply.ui.design.LocalExtraColors
import kz.yerek.aireply.ui.design.PrimaryButton
import kz.yerek.aireply.ui.design.ReadableColumn
import kz.yerek.aireply.ui.design.Spacing
import kz.yerek.aireply.ui.feature.account.ExhaustedRules
import kz.yerek.aireply.ui.feature.account.ExhaustedSheet
import kz.yerek.aireply.ui.feature.voice.DictationSheet
import kz.yerek.aireply.ui.navigation.Routes

/**
 * The request «Создать ответ» sends: the same one the keyboard sends, minus a
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
 * The recipient «Создать ответ» starts with: the one the keyboard has
 * selected ([SettingsStore.lastTemplateId][kz.yerek.aireply.data.settings.SettingsStore.lastTemplateId])
 * while it is still shown, else the first.
 */
internal fun initialRecipient(visibleIds: List<String>, lastTemplateId: String?): String? =
    lastTemplateId?.takeIf { it in visibleIds } ?: visibleIds.firstOrNull()

/**
 * «Создать ответ»: paste, dictate or take the example message, choose who it
 * is from, optionally say how to answer, create.
 *
 * Жауап жасау: хабарлама, алушы, қаласаңыз нұсқау — пернетақтадағыдай.
 *
 * It exists for two reasons. It lets someone check that their profile and
 * templates produce the replies they expect without switching to WhatsApp to
 * find out. And it calls the SAME [AIReplyService] the keyboard calls, with the
 * same profile, the same template and the same working-hours context, so what
 * is seen here is what the keyboard will produce. A separate code path would
 * drift. The recipient is the keyboard's too: choosing one here selects it on
 * the keyboard's row, and the other way round.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun ComposeScreen(onBack: () -> Unit, onOpen: (String) -> Unit = {}) {
    val services = LocalServices.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val configuration by services.configuration.configuration.collectAsStateWithLifecycle()
    val accountState by services.account.state.collectAsStateWithLifecycle()
    val reports = remember { AIReportController(scope, services.aiReportSender, BuildConfig.VERSION_NAME) }
    // Only on a server that takes reports.
    val canReport = accountState.features?.aiReports == true

    var message by remember { mutableStateOf("") }
    var instruction by remember { mutableStateOf("") }
    // The keyboard's selection; resolved against the shown templates below, so
    // it still applies when the configuration arrives after the first frame.
    var chosenId by remember { mutableStateOf(services.settings.lastTemplateId) }
    var reply by remember { mutableStateOf("") }
    var generating by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    // Spent replies are a fact, not a failure: a quiet line, and the sheet with the ways on.
    var errorIsLimit by remember { mutableStateOf(false) }
    var exhausted by remember { mutableStateOf<Boolean?>(null) }
    var pasteNotice by remember { mutableStateOf<PasteNotice?>(null) }
    var copied by remember { mutableStateOf(false) }
    var dictatingMessage by remember { mutableStateOf(false) }
    var dictatingInstruction by remember { mutableStateOf(false) }
    var job by remember { mutableStateOf<Job?>(null) }

    DisposableEffect(Unit) { onDispose { job?.cancel() } }

    // A template hidden or deleted meanwhile falls back to the first one shown.
    val templateId = initialRecipient(configuration.visibleTemplates.map { it.id }, chosenId)
    val count = AIReplyService.characterCount(message)
    val overLimit = count > AIConfiguration.maxMessageCharacters
    val canGenerate = !generating && !overLimit && count > 0 && templateId != null

    fun generate() {
        val template = templateId?.let { configuration.template(it) } ?: return
        // One request at a time. A second tap replaces the first rather than
        // racing it, so rapid taps cannot produce two answers or two bills.
        job?.cancel()
        error = null
        errorIsLimit = false
        reply = ""
        generating = true
        // A report is about the reply it was opened on.
        reports.close()

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
                if (ExhaustedRules.isLimit(mapped)) {
                    errorIsLimit = true
                    // The value is whether «Докупить ответы» is offered.
                    exhausted = ExhaustedRules.offersPurchase(
                        purchasesOffered = services.purchasesOffered,
                        canBuy = (exception as? AIReplyException)?.canBuy == true
                    )
                }
            }
        }
    }

    fun paste() {
        scope.launch {
            when (val outcome = ComposePaste.read(context)) {
                is PasteOutcome.Text -> {
                    message = outcome.text
                    pasteNotice = null
                    services.deviceState.notePaste(System.currentTimeMillis())
                    ProductEvents.track(ProductEvent.PASTE_SUCCEEDED)
                }
                is PasteOutcome.Notice -> pasteNotice = outcome.notice
            }
        }
    }

    AppScreen(
        title = stringResource(R.string.compose_title),
        onBack = onBack,
        bottomBar = {
            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = Spacing.l, vertical = Spacing.s),
                verticalArrangement = Arrangement.spacedBy(Spacing.xs),
                horizontalAlignment = Alignment.CenterHorizontally
            ) {
                if (message.isBlank()) {
                    Text(
                        stringResource(R.string.compose_hint_empty),
                        style = MaterialTheme.typography.bodySmall,
                        color = LocalExtraColors.current.textSecondary,
                        textAlign = TextAlign.Center,
                        modifier = Modifier.semantics { liveRegion = LiveRegionMode.Polite }
                    )
                }
                PrimaryButton(
                    text = stringResource(R.string.compose_generate),
                    onClick = { generate() },
                    enabled = canGenerate,
                    modifier = Modifier.widthIn(max = Layout.readableWidth)
                )
            }
        }
    ) {
        ReadableColumn {

            AppSection(stringResource(R.string.compose_message)) {
                AppCard {
                    OutlinedTextField(
                        value = message,
                        onValueChange = {
                            message = it
                            pasteNotice = null
                        },
                        placeholder = { Text(stringResource(R.string.compose_message_placeholder)) },
                        isError = overLimit,
                        minLines = 4,
                        supportingText = {
                            Text(
                                stringResource(R.string.compose_counter, count, AIConfiguration.maxMessageCharacters),
                                modifier = Modifier.fillMaxWidth(),
                                textAlign = TextAlign.End
                            )
                        },
                        modifier = Modifier.fillMaxWidth()
                    )
                    FlowRow(
                        horizontalArrangement = Arrangement.spacedBy(Spacing.xs),
                        modifier = Modifier.fillMaxWidth()
                    ) {
                        MessageAction(Icons.Outlined.ContentPaste, stringResource(R.string.compose_paste), ::paste)
                        MessageAction(Icons.Outlined.Mic, stringResource(R.string.compose_dictate)) {
                            dictatingMessage = true
                        }
                        MessageAction(Icons.Outlined.Lightbulb, stringResource(R.string.compose_example)) {
                            message = context.getString(R.string.compose_example_message)
                            pasteNotice = null
                        }
                    }
                    pasteNotice?.let { notice -> PasteNoticeLine(notice) { onOpen(Routes.PasteHelp) } }
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
                RecipientGrid(
                    options = configuration.visibleTemplates.map { template ->
                        RecipientOption(
                            id = template.id,
                            label = TemplateNaming.recipientName(context, template),
                            icon = RelationshipIcons.of(template.relationship)
                        )
                    },
                    selectedId = templateId,
                    onSelect = { id ->
                        chosenId = id
                        // The keyboard's row reads the same key on its next appearance.
                        services.settings.lastTemplateId = id
                    }
                )
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
                            Icons.Outlined.Mic,
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

            if (generating || error != null) {
                Column(verticalArrangement = Arrangement.spacedBy(Spacing.s)) {
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
                            color = if (errorIsLimit) LocalExtraColors.current.textSecondary else MaterialTheme.colorScheme.error
                        )
                    }
                }
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
                                copied = AppClipboard.copy(context, reply)
                            }) {
                                Icon(
                                    if (copied) Icons.Filled.Check else Icons.Outlined.ContentCopy,
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
                        // Quiet and on its own line: Copy and Regenerate keep their places.
                        if (canReport) {
                            TextButton(
                                onClick = { reports.open(AIReportMode.REPLY, reply) },
                                enabled = !generating,
                                colors = ButtonDefaults.textButtonColors(
                                    contentColor = LocalExtraColors.current.textSecondary
                                )
                            ) {
                                Icon(
                                    Icons.Outlined.Flag,
                                    contentDescription = null,
                                    modifier = Modifier.size(18.dp)
                                )
                                Text(
                                    stringResource(R.string.report_action),
                                    modifier = Modifier.padding(start = Spacing.xxs)
                                )
                            }
                        }
                    }
                }
            }

            Footnote(stringResource(R.string.compose_hint))
        }
    }

    if (copied) {
        LaunchedEffect(Unit) {
            delay(2_000)
            copied = false
        }
    }

    exhausted?.let { offersPurchase ->
        ExhaustedSheet(
            offersPurchase = offersPurchase,
            gender = configuration.profile.grammaticalGender,
            onBuy = {
                exhausted = null
                onOpen(Routes.Purchase)
            },
            onPlans = {
                exhausted = null
                onOpen(Routes.Subscription)
            },
            onDismiss = { exhausted = null }
        )
    }

    reports.draft?.let { draft ->
        ReportDialog(draft = draft, onSend = reports::send, onDismiss = reports::close)
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

/** Paste, Dictate, Try an example: chips under the message. */
@Composable
private fun MessageAction(icon: ImageVector, label: String, onClick: () -> Unit) {
    AssistChip(
        onClick = onClick,
        label = { Text(label) },
        leadingIcon = { Icon(icon, contentDescription = null, modifier = Modifier.size(AssistChipDefaults.IconSize)) },
        colors = AssistChipDefaults.assistChipColors(leadingIconContentColor = MaterialTheme.colorScheme.primary)
    )
}

/** Why Paste brought nothing - informational, never an error colour - and where to read more. */
@Composable
private fun PasteNoticeLine(notice: PasteNotice, onHelp: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .semantics(mergeDescendants = true) { liveRegion = LiveRegionMode.Polite },
        horizontalArrangement = Arrangement.spacedBy(Spacing.xs),
        verticalAlignment = Alignment.Top
    ) {
        Icon(
            Icons.Outlined.Info,
            contentDescription = null,
            tint = LocalExtraColors.current.textSecondary,
            modifier = Modifier.padding(top = 2.dp).size(16.dp)
        )
        Column(modifier = Modifier.weight(1f)) {
            Text(
                stringResource(
                    when (notice) {
                        PasteNotice.EMPTY -> R.string.compose_paste_empty
                        PasteNotice.PRIVATE -> R.string.compose_paste_private
                        PasteNotice.UNREADABLE -> R.string.compose_paste_unreadable
                    }
                ),
                style = MaterialTheme.typography.bodySmall,
                color = LocalExtraColors.current.textSecondary
            )
            TextButton(onClick = onHelp, contentPadding = PaddingValues(horizontal = 0.dp)) {
                Text(stringResource(R.string.permissions_action_paste_help))
            }
        }
    }
}

private fun append(existing: String, addition: String): String {
    val trimmed = addition.trim()
    if (trimmed.isEmpty()) return existing
    val separator = if (existing.isEmpty() || existing.endsWith(" ")) "" else " "
    return existing + separator + trimmed
}
