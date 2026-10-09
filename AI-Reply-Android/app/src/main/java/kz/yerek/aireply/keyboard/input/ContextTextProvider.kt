package kz.yerek.aireply.keyboard.input

import android.content.ClipDescription
import android.content.ClipboardManager
import android.content.Context
import android.view.inputmethod.InputConnection
import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.platform.ReplyLog

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
 * only ever runs as the direct result of the user tapping a template. There is
 * no polling, no timer, no read on appearance and no background access. Android
 * permits the active input method to read the clipboard; this code is narrower
 * than the platform allows, on purpose.
 */
class ContextTextProvider {

    sealed interface Result {
        data class Success(val context: ReplyContext) : Result
        data class Failure(val error: AIReplyError) : Result
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
            return Result.Failure(AIReplyError.NoSourceMessage)
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
            return Result.Failure(AIReplyError.NoSourceMessage)
        }

        // Password managers and banking apps flag what they copy. Honouring the
        // flag is the Android equivalent of the iOS promise that secure fields
        // are never processed — and it is the difference between a keyboard that
        // may read the clipboard and one that reads whatever is in it. The
        // constant is API 33, but Android's copy-and-paste guidance has apps
        // set the same key on older versions too, so it is read on all of them.
        if (isMarkedSensitive { key -> description.extras?.getBoolean(key, false) }) {
            ReplyLog.event { "clipboard: refused, marked sensitive" }
            return Result.Failure(AIReplyError.NoSourceMessage)
        }

        val copied = normalised(
            runCatching {
                clipboard.primaryClip?.takeIf { it.itemCount > 0 }
                    ?.getItemAt(0)
                    ?.coerceToText(context)
                    ?.toString()
            }.getOrNull()
        )

        if (copied == null) {
            ReplyLog.event { "clipboard: no usable text" }
            return Result.Failure(AIReplyError.NoSourceMessage)
        }

        ReplyLog.event { "source: clipboard, length ${copied.length}" }
        return Result.Success(ReplyContext(copied, ReplyContextSource.CLIPBOARD))
    }

    private fun normalised(value: String?): String? = value?.trim()?.takeIf { it.isNotEmpty() }

    internal companion object {
        val TEXT_TYPES = listOf(ClipDescription.MIMETYPE_TEXT_PLAIN, ClipDescription.MIMETYPE_TEXT_HTML)

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
