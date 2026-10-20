package kz.yerek.aireply.keyboard.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.OpenInNew
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kz.yerek.aireply.R
import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.ai.AppStrings
import kz.yerek.aireply.keyboard.KeyboardTheme
import kz.yerek.aireply.keyboard.input.ClipboardMiss
import kz.yerek.aireply.keyboard.layout.PanelFit
import kz.yerek.aireply.keyboard.reply.ReplyComposerFlow

/*
 * The line under a panel's field that says what went wrong - shared by the
 * reply composer and Create, so both say it the same way.
 *
 * Three kinds of message, and they look different on purpose:
 *  * a request that FAILED (no connection, the service, a timeout) is red,
 *    and the primary button offers Try again;
 *  * a note about what was copied ("copy a message first", "this text is
 *    marked private") is not a failure of anything the user did: it is grey,
 *    and the button keeps saying Reply - trying again cannot change what is
 *    on the clipboard;
 *  * spent replies ([LimitNotice]) say so and lead to the app, where more
 *    can be bought or another plan chosen. Not "come back tomorrow": a pass
 *    or a pack may be what runs out, and the plan is not the only bucket.
 *    No Try again either - the same request would be refused the same way.
 *
 * Their heights are fixed by [PanelFit]: the text sits on explicit line
 * heights, so what the budget counts is what is drawn.
 */

/** Errors that only describe the clipboard: grey, and never "Try again". */
internal val AIReplyError.isClipboardNotice: Boolean
    get() = this == AIReplyError.NoSourceMessage || this == AIReplyError.ClipboardUnavailable

/** The replies are spent, today's or this month's: the limit notice, never "Try again". */
internal val AIReplyError.isLimit: Boolean
    get() = this == AIReplyError.QuotaExhausted || this == AIReplyError.MonthlyQuotaExhausted

/** A failed request worth repeating as it is: the primary button says Try again. */
internal val AIReplyError.offersRetry: Boolean
    get() = !isClipboardNotice && !isLimit && this != AIReplyError.Cancelled

/**
 * What the notice says for [error], or null for none. A clip the copying app
 * marked private ([miss]) gets its own sentence: the user did copy something.
 */
internal fun noticeText(strings: AppStrings, error: AIReplyError?, miss: ClipboardMiss?): String? = when {
    error == null -> null
    error == AIReplyError.ClipboardUnavailable && miss == ClipboardMiss.MARKED_PRIVATE -> strings[R.string.kb_paste_private]
    else -> strings.message(error).takeIf { it.isNotEmpty() }
}

/**
 * Which notice a panel shows under its field for [flow], whose notice
 * sentence is [text]: what both panels draw and what [PanelFit] budgets.
 * The "field is not empty" question has the panel to itself.
 */
internal fun noticeKind(flow: ReplyComposerFlow, text: String?): PanelFit.Notice = when {
    flow.isConflict -> PanelFit.Notice.NONE
    flow.error?.isLimit == true -> PanelFit.Notice.LIMIT
    text != null -> PanelFit.Notice.LINE
    else -> PanelFit.Notice.NONE
}

/**
 * [notice] under a panel's field, or nothing: [LimitNotice] or a
 * [NoticeLine] saying [text]. [compact] (a phone on its side): one line of
 * text, and the limit notice its row alone.
 */
@Composable
internal fun PanelNotice(
    notice: PanelFit.Notice,
    text: String?,
    error: AIReplyError?,
    compact: Boolean,
    strings: AppStrings,
    theme: KeyboardTheme,
    onOpenApp: () -> Unit
) {
    when (notice) {
        PanelFit.Notice.LIMIT -> LimitNotice(strings, theme, compact, onOpenApp)
        PanelFit.Notice.LINE -> text?.let { NoticeLine(it, error, theme, PanelFit.noticeLines(compact)) }
        PanelFit.Notice.NONE -> Unit
    }
}

/** The notice line itself: red for a failed request, grey for a note about the clipboard. */
@Composable
internal fun NoticeLine(text: String, error: AIReplyError?, theme: KeyboardTheme, lines: Int = PanelFit.NOTICE_LINES) {
    Text(
        text = text,
        fontSize = 12.sp,
        lineHeight = PanelFit.NOTICE_LINE.sp,
        fontWeight = FontWeight.Medium,
        color = if (error?.isClipboardNotice == true) theme.secondaryText else theme.destructive,
        maxLines = lines,
        overflow = TextOverflow.Ellipsis,
        modifier = Modifier.fillMaxWidth().padding(horizontal = 2.dp)
    )
}

/**
 * «Лимит ответов исчерпан»: what happened, where more replies are, and the
 * button that goes there - AI Reply's purchase screen, or its plans while it
 * has none ([onOpenApp]). The keyboard sells nothing itself.
 *
 * [compact] (a phone on its side) keeps only the row - the title and the
 * button ([LimitRow]) - so the keys stay on screen; the sentence is still
 * read out with the title.
 *
 * Жауаптар бітті: қосымшаны ашып, жауап сатып алуға не тариф таңдауға болады.
 */
@Composable
internal fun LimitNotice(strings: AppStrings, theme: KeyboardTheme, compact: Boolean, onOpenApp: () -> Unit) {
    Column(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 2.dp),
        verticalArrangement = Arrangement.spacedBy(PanelFit.LIMIT_BODY_GAP.dp)
    ) {
        LimitRow(strings, theme, compact, onOpenApp, Modifier.heightIn(min = PanelFit.LIMIT_ROW.dp))
        if (!compact) {
            Text(
                text = strings[R.string.kb_limit_body],
                fontSize = 12.sp,
                lineHeight = PanelFit.NOTICE_LINE.sp,
                color = theme.secondaryText,
                maxLines = PanelFit.NOTICE_LINES,
                overflow = TextOverflow.Ellipsis
            )
        }
    }
}

/**
 * The limit notice's row: the title and «Открыть AI Reply». [compact]: the
 * title is one line and carries the sentence for TalkBack, as it does where
 * Create puts the row in its header on a phone on its side.
 */
@Composable
internal fun LimitRow(
    strings: AppStrings,
    theme: KeyboardTheme,
    compact: Boolean,
    onOpenApp: () -> Unit,
    modifier: Modifier = Modifier
) {
    val title = strings[R.string.kb_limit_title]
    val body = strings[R.string.kb_limit_body]
    Row(modifier = modifier, verticalAlignment = Alignment.CenterVertically) {
        Text(
            text = title,
            fontSize = 13.sp,
            lineHeight = PanelFit.LIMIT_TITLE_LINE.sp,
            fontWeight = FontWeight.SemiBold,
            color = theme.primaryText,
            maxLines = if (compact) 1 else 2,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f).semantics {
                heading()
                if (compact) contentDescription = "$title. $body"
            }
        )
        Spacer(Modifier.width(6.dp))
        AssistPill(
            text = strings[R.string.kb_limit_open],
            description = strings[R.string.kb_limit_open],
            icon = {
                Icon(Icons.AutoMirrored.Filled.OpenInNew, null, tint = theme.primaryText, modifier = Modifier.size(14.dp))
            },
            theme = theme,
            onClick = onOpenApp
        )
    }
}
