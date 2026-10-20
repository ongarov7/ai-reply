package kz.yerek.aireply.ui.feature.onboarding

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import kz.yerek.aireply.R
import kz.yerek.aireply.domain.model.RelationshipKind
import kz.yerek.aireply.core.lang.TemplateNaming
import kz.yerek.aireply.ui.common.AppClipboard
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.common.RecipientGrid
import kz.yerek.aireply.ui.common.RecipientOption
import kz.yerek.aireply.ui.common.RelationshipIcons
import kz.yerek.aireply.ui.design.AppCard
import kz.yerek.aireply.ui.design.LocalExtraColors
import kz.yerek.aireply.ui.design.Spacing

/*
 * «Как пользоваться»: copy, choose who it is for, the reply. Everything is
 * local - the replies are examples from the resources, no request is made and
 * no reply is spent. Copy is the one real action: it puts the text on the
 * clipboard, on the user's tap only.
 */

/** 1 of 3: a message to copy, with a Copy button that really copies. */
@Composable
internal fun ColumnScope.TutorialCopyStep(copied: Boolean, onCopied: () -> Unit) {
    val context = LocalContext.current
    val message = stringResource(R.string.onboarding_demo_message)

    StepHeader(stringResource(R.string.onboarding_copy_title), stringResource(R.string.onboarding_copy_prompt))
    AppCard {
        Column(verticalArrangement = Arrangement.spacedBy(Spacing.s)) {
            IncomingBubble(message)
            CopyButton(copied = copied) { if (AppClipboard.copy(context, message)) onCopied() }
            // Android 13+ confirms a copy with its own panel; below that, this line does.
            if (copied && !AppClipboard.systemConfirmsCopy) {
                Text(
                    stringResource(R.string.onboarding_copied_note),
                    style = MaterialTheme.typography.bodySmall,
                    color = LocalExtraColors.current.textSecondary,
                    modifier = Modifier.semantics { liveRegion = LiveRegionMode.Polite }
                )
            }
        }
    }
    Footnote(stringResource(R.string.onboarding_copy_footnote))
}

/** 2 of 3: the four recipients, as the keyboard names them. */
@Composable
internal fun ColumnScope.TutorialRecipientStep(selected: RelationshipKind?, onSelect: (RelationshipKind) -> Unit) {
    StepHeader(stringResource(R.string.onboarding_recipient_title), stringResource(R.string.onboarding_recipient_prompt))
    BuiltInRecipients(selected, onSelect)
    Footnote(stringResource(R.string.onboarding_recipient_note))
}

/**
 * 3 of 3: the same message, the recipients, and the reply for the one chosen
 * - it changes as the choice does. Its Copy copies the example.
 */
@Composable
internal fun ColumnScope.TutorialReplyStep(
    selected: RelationshipKind,
    onSelect: (RelationshipKind) -> Unit,
    copied: Boolean,
    onCopied: () -> Unit
) {
    val context = LocalContext.current
    val reply = stringResource(OnboardingExamples.demoReply(selected))

    StepHeader(stringResource(R.string.onboarding_try_title), stringResource(R.string.onboarding_try_prompt))
    AppCard {
        Column(verticalArrangement = Arrangement.spacedBy(Spacing.xs)) {
            BubbleLabel(stringResource(R.string.onboarding_message_label))
            IncomingBubble(stringResource(R.string.onboarding_demo_message))
        }
    }
    BuiltInRecipients(selected, onSelect)
    AppCard(modifier = Modifier.semantics { liveRegion = LiveRegionMode.Polite }) {
        Column(verticalArrangement = Arrangement.spacedBy(Spacing.xs)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                BubbleLabel(stringResource(R.string.onboarding_reply_variant), accent = true)
                Spacer(Modifier.weight(1f))
                Icon(
                    Icons.Filled.CheckCircle,
                    contentDescription = null,
                    tint = LocalExtraColors.current.success,
                    modifier = Modifier.size(16.dp)
                )
                Text(
                    stringResource(R.string.onboarding_reply_ready),
                    style = MaterialTheme.typography.labelMedium,
                    color = LocalExtraColors.current.textSecondary,
                    modifier = Modifier.padding(start = Spacing.xxs)
                )
            }
            ReplyBubble(reply)
            Row(modifier = Modifier.fillMaxWidth()) {
                Spacer(Modifier.weight(1f))
                TextButton(onClick = { if (AppClipboard.copy(context, reply)) onCopied() }) {
                    Icon(
                        if (copied) Icons.Filled.Check else Icons.Outlined.ContentCopy,
                        contentDescription = null,
                        modifier = Modifier.size(18.dp)
                    )
                    Text(
                        stringResource(if (copied) R.string.compose_copied else R.string.common_copy),
                        modifier = Modifier.padding(start = Spacing.xxs)
                    )
                }
            }
        }
    }
    Row(
        horizontalArrangement = Arrangement.spacedBy(Spacing.s),
        verticalAlignment = Alignment.Top
    ) {
        Icon(
            Icons.Filled.CheckCircle,
            contentDescription = null,
            tint = LocalExtraColors.current.success,
            modifier = Modifier.size(24.dp)
        )
        Column(verticalArrangement = Arrangement.spacedBy(Spacing.xxs)) {
            Text(stringResource(R.string.onboarding_success), style = MaterialTheme.typography.titleLarge)
            Footnote(stringResource(R.string.onboarding_success_body))
        }
    }
}

@Composable
private fun CopyButton(copied: Boolean, onClick: () -> Unit) {
    OutlinedButton(onClick = onClick) {
        Icon(
            if (copied) Icons.Filled.Check else Icons.Outlined.ContentCopy,
            contentDescription = null,
            modifier = Modifier.size(18.dp)
        )
        Text(
            stringResource(if (copied) R.string.compose_copied else R.string.common_copy),
            modifier = Modifier.padding(start = Spacing.xs)
        )
    }
}

/** Другу, Клиенту, Коллеге, Партнёру - the built-ins in their new-install order, with icons. */
@Composable
private fun BuiltInRecipients(selected: RelationshipKind?, onSelect: (RelationshipKind) -> Unit) {
    RecipientGrid(
        options = RelationshipKind.builtIns.map { kind ->
            RecipientOption(
                id = kind.raw,
                label = stringResource(TemplateNaming.dativeResource(kind) ?: TemplateNaming.resource(kind)),
                icon = RelationshipIcons.of(kind)
            )
        },
        selectedId = selected?.raw,
        onSelect = { raw -> RelationshipKind.fromRaw(raw)?.let(onSelect) }
    )
}
