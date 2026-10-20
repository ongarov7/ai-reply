package kz.yerek.aireply.keyboard.ui

import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kz.yerek.aireply.R
import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.ai.AppStrings
import kz.yerek.aireply.keyboard.KeyboardTheme
import kz.yerek.aireply.keyboard.input.ClipboardMiss

/*
 * The line under a panel's field that says what went wrong - shared by the
 * reply composer and Create, so both say it the same way.
 *
 * Two kinds of message, and they look different on purpose:
 *  * a request that FAILED (no connection, the service, a timeout) is red,
 *    and the primary button offers Try again;
 *  * a note about what was copied ("copy a message first", "this text is
 *    marked private") is not a failure of anything the user did: it is grey,
 *    and the button keeps saying Reply - trying again cannot change what is
 *    on the clipboard.
 */

/** Errors that only describe the clipboard: grey, and never "Try again". */
internal val AIReplyError.isClipboardNotice: Boolean
    get() = this == AIReplyError.NoSourceMessage || this == AIReplyError.ClipboardUnavailable

/** A failed request worth repeating as it is: the primary button says Try again. */
internal val AIReplyError.offersRetry: Boolean
    get() = !isClipboardNotice && this != AIReplyError.Cancelled

/**
 * What the notice says for [error], or null for none. A clip the copying app
 * marked private ([miss]) gets its own sentence: the user did copy something.
 */
internal fun noticeText(strings: AppStrings, error: AIReplyError?, miss: ClipboardMiss?): String? = when {
    error == null -> null
    error == AIReplyError.ClipboardUnavailable && miss == ClipboardMiss.MARKED_PRIVATE -> strings[R.string.kb_paste_private]
    else -> strings.message(error).takeIf { it.isNotEmpty() }
}

/** The notice line itself: red for a failed request, grey for a note about the clipboard. */
@Composable
internal fun NoticeLine(text: String, error: AIReplyError?, theme: KeyboardTheme) {
    Text(
        text = text,
        fontSize = 12.sp,
        fontWeight = FontWeight.Medium,
        color = if (error?.isClipboardNotice == true) theme.secondaryText else theme.destructive,
        maxLines = 2,
        overflow = TextOverflow.Ellipsis,
        modifier = Modifier.fillMaxWidth().padding(horizontal = 2.dp)
    )
}
