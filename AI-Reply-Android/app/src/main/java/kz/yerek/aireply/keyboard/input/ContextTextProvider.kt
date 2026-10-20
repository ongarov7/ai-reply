package kz.yerek.aireply.keyboard.input

import android.content.ClipDescription
import android.content.ClipboardManager
import android.content.Context
import android.net.Uri
import android.text.Html
import android.view.inputmethod.InputConnection
import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.platform.ReplyLog
import java.io.InputStreamReader

enum class ReplyContextSource {
    /** Text the host exposed as a selection inside the ACTIVE EDITABLE INPUT. */
    EDITABLE_SELECTION,

    /**
     * Text the user explicitly copied, read only in direct response to the user
     * tapping a template.
     */
    CLIPBOARD
}

data class ReplyContext(val text: String, val source: ReplyContextSource)

/**
 * Why nothing usable came off the clipboard. Each case is told apart because
 * each needs its own words: "copy a message first" is wrong for someone who
 * just copied a password manager entry.
 *
 * Буферден мәтін алынбау себебі: бос, құпия деп белгіленген немесе оқылмады.
 */
enum class ClipboardMiss {
    /** A password field is focused: the clipboard is not even looked at. */
    SECURE_FIELD,

    /** Nothing was copied, or nothing that is text. */
    NOTHING_COPIED,

    /** The copying app flagged the clip as sensitive (a password manager, a bank). */
    MARKED_PRIVATE,

    /** The clip says it is text, but the text could not be read. */
    UNREADABLE
}

/**
 * Resolves "the message the user wants to reply to" using only public,
 * documented APIs.
 *
 * IMPORTANT LIMITATION, stated here so no caller can misread the intent: an
 * Android input method CANNOT read incoming message bubbles in WhatsApp,
 * Telegram, Instagram Direct or Messenger. An `InputConnection` is a view onto
 * the active editable text input only. Reading another app's chat content would
 * require an AccessibilityService, and this project deliberately contains no
 * accessibility, screen-scraping or OCR path to fake one — that is a line the
 * product does not cross, not a feature it has not got round to. The universal
 * fallback is the explicit user gesture: long press the message, Copy, then tap
 * a template.
 *
 * CLIPBOARD PRIVACY. The clipboard is touched ONLY from inside [acquire], which
 * only ever runs as the direct result of the user tapping a template, Paste or
 * Reply to copied. There is no polling, no timer, no read on appearance and no
 * background access. Android permits the active input method to read the
 * clipboard; this code is narrower than the platform allows, on purpose. The
 * one read that finishes later ([Result.Deferred]) is the same tap's: the clip
 * it opens was taken from the clipboard on that tap.
 *
 * TYPING NEVER WAITS. The text a clip carries is read at once; text only behind
 * another app's provider is read off the main thread, capped.
 */
class ContextTextProvider {

    sealed interface Result {
        data class Success(val context: ReplyContext) : Result

        /**
         * [error] is what the panels' flows know (an empty clipboard opens the
         * composer quietly; anything else is a notice); [miss] says which
         * sentence the notice is.
         */
        data class Failure(val error: AIReplyError, val miss: ClipboardMiss = missFor(error)) : Result

        /**
         * The copied text is not in the clip itself: it is HTML only, or
         * behind another app's content provider, which can be slow or
         * enormous. [read] fetches it, capped at [MAX_DEFERRED_CHARACTERS],
         * and must run OFF the main thread - the tap that asked goes on at
         * once, and the keys never wait for another app.
         */
        class Deferred(val read: () -> Result) : Result
    }

    fun acquire(
        context: Context,
        connection: InputConnection?,
        isSecureField: Boolean
    ): Result {
        // A password field is never a conversation. Refusing here means the
        // clipboard is not even consulted while one is focused.
        if (isSecureField) {
            ReplyLog.event { "source refused: secure field" }
            return Result.Failure(AIReplyError.NoSourceMessage, ClipboardMiss.SECURE_FIELD)
        }

        // STEP 1 — a selection inside the active editable input.
        normalised(connection?.getSelectedText(0)?.toString())?.let {
            ReplyLog.event { "source: selection" }
            return Result.Success(ReplyContext(it, ReplyContextSource.EDITABLE_SELECTION))
        }

        // STEP 2 — explicitly copied text.
        val clipboard = context.getSystemService(Context.CLIPBOARD_SERVICE) as? ClipboardManager
            ?: return Result.Failure(AIReplyError.ClipboardUnavailable)

        // Browsers and some messengers copy rich text as HTML only; its text
        // is the message just the same.
        val description = clipboard.primaryClipDescription
        if (description == null || TEXT_TYPES.none(description::hasMimeType)) {
            ReplyLog.event { "clipboard: no text" }
            return Result.Failure(AIReplyError.NoSourceMessage, ClipboardMiss.NOTHING_COPIED)
        }

        // Password managers and banking apps flag what they copy. Honouring the
        // flag is the Android equivalent of the iOS promise that secure fields
        // are never processed — and it is the difference between a keyboard that
        // may read the clipboard and one that reads whatever is in it. The
        // constant is API 33, but Android's copy-and-paste guidance has apps
        // set the same key on older versions too, so it is read on all of them.
        // The user did copy something, so this is not "copy a message first".
        if (isMarkedSensitive { key -> description.extras?.getBoolean(key, false) }) {
            ReplyLog.event { "clipboard: refused, marked sensitive" }
            return Result.Failure(AIReplyError.ClipboardUnavailable, ClipboardMiss.MARKED_PRIVATE)
        }

        val item = runCatching { clipboard.primaryClip?.takeIf { it.itemCount > 0 }?.getItemAt(0) }.getOrNull()
            ?: return Result.Failure(AIReplyError.NoSourceMessage, ClipboardMiss.NOTHING_COPIED)

        // The text the copying app put in the clip: already in memory, so it
        // is read right here. Only when there is none does anything else run -
        // and that runs later, off the main thread (see [Result.Deferred]).
        val inline = runCatching { item.text?.toString() }.getOrNull()
        if (inline != null) return copiedText(normalised(inline))

        val html = runCatching { item.htmlText }.getOrNull()
        val uri = runCatching { item.uri }.getOrNull()
        if (html == null && uri == null) {
            ReplyLog.event { "clipboard: no usable text" }
            return Result.Failure(AIReplyError.NoSourceMessage, ClipboardMiss.NOTHING_COPIED)
        }
        ReplyLog.event { "clipboard: text to be read later" }
        return Result.Deferred {
            val text = if (html != null) fromHtml(html) else readText(context, uri!!)
            if (text == null) {
                ReplyLog.event { "clipboard: copied text unreadable" }
                Result.Failure(AIReplyError.ClipboardUnavailable, ClipboardMiss.UNREADABLE)
            } else {
                copiedText(normalised(text))
            }
        }
    }

    private fun copiedText(copied: String?): Result {
        if (copied == null) {
            ReplyLog.event { "clipboard: no usable text" }
            return Result.Failure(AIReplyError.NoSourceMessage, ClipboardMiss.NOTHING_COPIED)
        }
        ReplyLog.event { "source: clipboard, length ${copied.length}" }
        return Result.Success(ReplyContext(copied, ReplyContextSource.CLIPBOARD))
    }

    /** HTML-only clips: the text of the markup, capped like any other late read. */
    private fun fromHtml(html: String): String? = runCatching {
        Html.fromHtml(html.take(MAX_DEFERRED_CHARACTERS * 4), Html.FROM_HTML_MODE_COMPACT).toString()
            .take(MAX_DEFERRED_CHARACTERS)
    }.getOrNull()

    /**
     * What `ClipData.Item.coerceToText` does for a content URI - open it as
     * any text type and read it - but with a cap, and with null rather than
     * the URI itself when it is not readable text: "content://…" is not a
     * message.
     */
    private fun readText(context: Context, uri: Uri): String? = runCatching {
        context.contentResolver.openTypedAssetFileDescriptor(uri, "text/*", null)?.use { descriptor ->
            InputStreamReader(descriptor.createInputStream(), Charsets.UTF_8).use { reader ->
                val buffer = CharArray(MAX_DEFERRED_CHARACTERS)
                var filled = 0
                while (filled < buffer.size) {
                    val count = reader.read(buffer, filled, buffer.size - filled)
                    if (count < 0) break
                    filled += count
                }
                String(buffer, 0, filled)
            }
        }
    }.getOrNull()

    private fun normalised(value: String?): String? = value?.trim()?.takeIf { it.isNotEmpty() }

    internal companion object {
        val TEXT_TYPES = listOf(ClipDescription.MIMETYPE_TEXT_PLAIN, ClipDescription.MIMETYPE_TEXT_HTML)

        /**
         * The most a late read takes in. Far above the server's message limit,
         * so a long message still reaches the counter and its "too long"; low
         * enough that a huge shared file never fills the keyboard's memory.
         */
        const val MAX_DEFERRED_CHARACTERS = 20_000

        /** The miss an error stands for when a caller names only the error. */
        fun missFor(error: AIReplyError): ClipboardMiss =
            if (error == AIReplyError.ClipboardUnavailable) ClipboardMiss.UNREADABLE else ClipboardMiss.NOTHING_COPIED

        /** `ClipDescription.EXTRA_IS_SENSITIVE`, spelled out so it is usable below API 33. */
        const val EXTRA_IS_SENSITIVE = "android.content.extra.IS_SENSITIVE"

        /**
         * True when the copying app flagged the clip as sensitive, on every
         * API level. [flag] reads one boolean from the clip's extras; extras
         * that cannot be read count as no flag.
         */
        fun isMarkedSensitive(flag: (String) -> Boolean?): Boolean =
            runCatching { flag(EXTRA_IS_SENSITIVE) }.getOrNull() == true
    }
}
