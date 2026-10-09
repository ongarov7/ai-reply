package kz.yerek.aireply.ui.feature.compose

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Checkbox
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import kz.yerek.aireply.R
import kz.yerek.aireply.ai.AIReportDraft
import kz.yerek.aireply.ai.AIReportReason
import kz.yerek.aireply.ui.design.Spacing
import kz.yerek.aireply.ui.feature.profile.SelectableRow

/**
 * The app's Report dialog: the same choices as the keyboard's inline panel -
 * a reason, the reply text on or off, Send. The thanks shows in it for a
 * moment before it closes by itself.
 */
@Composable
internal fun ReportDialog(draft: AIReportDraft, onSend: () -> Unit, onDismiss: () -> Unit) {
    val sent = draft.status == AIReportDraft.Status.SENT
    val sending = draft.status == AIReportDraft.Status.SENDING || sent
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.report_title)) },
        text = {
            Column(
                modifier = Modifier.verticalScroll(rememberScrollState()),
                verticalArrangement = Arrangement.spacedBy(Spacing.xxs)
            ) {
                AIReportReason.entries.forEach { reason ->
                    SelectableRow(stringResource(reason.label), selected = draft.reason == reason) {
                        if (!sending) draft.reason = reason
                    }
                }
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .toggleable(
                            value = draft.includeText,
                            enabled = !sending,
                            role = Role.Checkbox,
                            onValueChange = { draft.includeText = it }
                        ),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(Spacing.xs)
                ) {
                    Checkbox(checked = draft.includeText, onCheckedChange = null, enabled = !sending)
                    Text(stringResource(R.string.report_include_text), style = MaterialTheme.typography.bodyMedium)
                }
                when (draft.status) {
                    AIReportDraft.Status.SENT -> Text(
                        stringResource(R.string.report_thanks),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.primary
                    )
                    AIReportDraft.Status.FAILED -> Text(
                        stringResource(R.string.report_failed),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.error
                    )
                    else -> Unit
                }
            }
        },
        confirmButton = {
            TextButton(onClick = onSend, enabled = draft.canSend) {
                Text(stringResource(R.string.report_send))
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) {
                Text(stringResource(if (sent) R.string.common_done else R.string.common_cancel))
            }
        }
    )
}
