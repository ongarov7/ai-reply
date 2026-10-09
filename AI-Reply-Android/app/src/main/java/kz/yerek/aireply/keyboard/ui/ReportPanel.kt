package kz.yerek.aireply.keyboard.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CheckBox
import androidx.compose.material.icons.filled.CheckBoxOutlineBlank
import androidx.compose.material.icons.outlined.Flag
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kz.yerek.aireply.R
import kz.yerek.aireply.ai.AIReportDraft
import kz.yerek.aireply.ai.AIReportReason
import kz.yerek.aireply.ai.AppStrings
import kz.yerek.aireply.keyboard.KeyboardTheme

/** Report under a result: open or close the panel, send. */
class ReportActions(
    /** The flag in the header: opens the panel for the text on screen, or closes it. */
    val onToggle: () -> Unit,
    val onSend: () -> Unit,
    val onCancel: () -> Unit
) {
    companion object {
        val NONE = ReportActions(onToggle = {}, onSend = {}, onCancel = {})
    }
}

/**
 * The flag in a panel's header while a result is on screen; an accent disc
 * while the report is open, like Edit while editing.
 */
@Composable
internal fun ReportFlag(open: Boolean, strings: AppStrings, theme: KeyboardTheme, onClick: () -> Unit) {
    CircleIcon(
        icon = Icons.Outlined.Flag,
        description = strings[R.string.report_action],
        theme = theme,
        size = 28.dp,
        iconSize = 15.dp,
        filled = false,
        selected = open,
        tint = theme.secondaryText,
        onClick = onClick
    )
}

/**
 * Report, inside the keyboard: it takes the place of the reply and its
 * buttons, under the same header, because an input method has no business
 * opening a window over the chat. A reason, the text on or off (on until the
 * user says otherwise), Send or Cancel. The thanks shows for a moment, then
 * the reply comes back exactly as it was.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun ReportPanel(
    draft: AIReportDraft,
    strings: AppStrings,
    theme: KeyboardTheme,
    actions: ReportActions
) {
    val sending = draft.status == AIReportDraft.Status.SENDING
    val sent = draft.status == AIReportDraft.Status.SENT
    val editable = !sending && !sent

    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Text(
            text = strings[draft.mode.title],
            fontSize = 13.sp,
            fontWeight = FontWeight.SemiBold,
            color = theme.primaryText,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.padding(horizontal = 2.dp).semantics { heading() }
        )

        FlowRow(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(6.dp),
            verticalArrangement = Arrangement.spacedBy(6.dp)
        ) {
            AIReportReason.entries.forEach { reason ->
                val selected = draft.reason == reason
                Box(
                    modifier = Modifier
                        .height(28.dp)
                        .clip(RoundedCornerShape(14.dp))
                        .background(if (selected) theme.accent else theme.fieldBackground)
                        .clickable(enabled = editable, role = Role.RadioButton) { draft.reason = reason }
                        .semantics { this.selected = selected }
                        .padding(horizontal = 11.dp),
                    contentAlignment = Alignment.Center
                ) {
                    Text(
                        text = strings[reason.label],
                        fontSize = 12.5.sp,
                        fontWeight = FontWeight.Medium,
                        color = if (selected) Color.White else theme.primaryText,
                        maxLines = 1
                    )
                }
            }
        }

        Row(
            modifier = Modifier
                .fillMaxWidth()
                .heightIn(min = 28.dp)
                .toggleable(
                    value = draft.includeText,
                    enabled = editable,
                    role = Role.Checkbox,
                    onValueChange = { draft.includeText = it }
                )
                .padding(horizontal = 2.dp),
            verticalAlignment = Alignment.CenterVertically
        ) {
            Icon(
                if (draft.includeText) Icons.Filled.CheckBox else Icons.Filled.CheckBoxOutlineBlank,
                contentDescription = null,
                tint = if (draft.includeText) theme.accent else theme.secondaryText,
                modifier = Modifier.size(18.dp)
            )
            Spacer(Modifier.width(6.dp))
            Text(
                text = strings[draft.mode.includeText],
                fontSize = 12.5.sp,
                color = theme.primaryText,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis
            )
        }

        val status = when (draft.status) {
            AIReportDraft.Status.SENT -> strings[draft.mode.thanks]
            AIReportDraft.Status.FAILED -> strings[R.string.report_failed]
            else -> null
        }
        if (status != null) {
            Text(
                text = status,
                fontSize = 12.sp,
                fontWeight = FontWeight.Medium,
                color = if (sent) theme.secondaryText else theme.destructive,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.fillMaxWidth().padding(horizontal = 2.dp)
            )
        }

        Row(
            modifier = Modifier.fillMaxWidth().height(ROW_HEIGHT),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(6.dp)
        ) {
            ActionPill(
                label = strings[if (sent) R.string.common_done else R.string.common_cancel],
                enabled = true,
                theme = theme,
                modifier = Modifier.weight(1f),
                quiet = true,
                onClick = actions.onCancel
            )
            ActionPill(
                label = strings[R.string.report_send],
                enabled = draft.canSend,
                theme = theme,
                modifier = Modifier.weight(1f),
                busy = sending,
                onClick = actions.onSend
            )
        }
    }
}
