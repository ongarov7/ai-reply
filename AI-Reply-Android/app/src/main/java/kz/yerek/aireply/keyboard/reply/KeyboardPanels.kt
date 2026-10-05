package kz.yerek.aireply.keyboard.reply

import kz.yerek.aireply.ai.AILimits
import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.domain.model.RelationshipKind
import kz.yerek.aireply.domain.model.ReplyConfiguration
import kz.yerek.aireply.domain.model.ReplyTemplate
import kz.yerek.aireply.keyboard.input.ContextTextProvider
import kz.yerek.aireply.keyboard.input.KeyboardTextFieldState
import kz.yerek.aireply.keyboard.voice.DictationController
import kz.yerek.aireply.keyboard.voice.DictationTarget
import kz.yerek.aireply.platform.ReplyLog
import kz.yerek.aireply.voice.RecognitionLanguage

/**
 * The rules BETWEEN the two AI panels and the microphone: where dictation
 * goes, what ends it, when a request may start, and the step from Create to
 * a reply to the copied message.
 *
 *  * Dictation writes into the instruction of the panel on screen while that
 *    instruction is being written, in the language of the layout on screen -
 *    the same for the reply composer and Create.
 *  * Write and Reply wait while the microphone is on: the words being spoken
 *    belong in the request, and a request is never started for the user.
 *  * Closing a panel, New, another persona or leaving Create for a reply drop
 *    a recording; a layout switch ends it and keeps the words; the keyboard
 *    going away releases the microphone at once.
 *
 * Pure keyboard state, no Android: the service supplies the clipboard read
 * and the layout.
 */
class KeyboardPanels(
    val replies: ReplySessionController,
    val compose: ComposeSessionController,
    val dictation: DictationController,
    private val layout: () -> KeyboardLanguage,
    private val instructionLimit: () -> Int = { AILimits.current.instructionCharacters }
) {

    init {
        dictation.isLive = { field -> field === dictationField }
    }

    /** What "Reply to copied" did. */
    sealed interface CopiedReply {
        /** The reply composer is open on the copied message, with the request carried over. */
        data object Opened : CopiedReply

        /** Nothing usable was copied: Create stays, saying so. */
        data class NothingCopied(val error: AIReplyError) : CopiedReply

        /** Not now: a password field, no persona, or Create is not taking a request. */
        data object Refused : CopiedReply
    }

    /** The instruction of the panel on screen while it is being written; null otherwise. */
    val dictationField: KeyboardTextFieldState?
        get() {
            compose.session?.let { created ->
                return created.instruction.takeIf { created.flow.stage == ReplyComposerFlow.Stage.Composing }
            }
            val session = replies.session ?: return null
            if (!replies.isComposing) return null
            return session.instruction.takeIf { session.flow.stage == ReplyComposerFlow.Stage.Composing }
        }

    /** Write and Reply are available: the microphone is not running. */
    val canStartRequest: Boolean get() = !dictation.isActive

    // ------------------------------------------------------------ microphone

    /** The mic button, in either panel. */
    fun microphoneTapped() {
        val field = dictationField ?: return
        dictation.toggle(DictationTarget(field, instructionLimit()), RecognitionLanguage.tagFor(layout()))
    }

    /** ҚАЗ / РУС / ENG changed: a recording ends with what was heard. */
    fun layoutChanged() = dictation.layoutChanged()

    // -------------------------------------------------------------- requests

    /** Write / Reply, Stop, Try again. Never starts a request while the microphone is on. */
    fun primaryTapped() {
        when {
            compose.isActive -> when {
                compose.flow.isGenerating -> compose.stop()
                canStartRequest -> compose.generate()
            }
            replies.isComposing -> when {
                replies.flow.isGenerating -> replies.stop()
                canStartRequest -> replies.generate()
            }
        }
    }

    // ---------------------------------------------------------------- panels

    /** "✨": a fresh Create panel. A reply put aside is dropped. */
    fun openCreate() {
        dictation.cancel()
        replies.clear()
        compose.open()
    }

    /** New in Create: everything, a recording included, starts over. */
    fun startOver() {
        dictation.cancel()
        compose.reset()
    }

    /** The persona chip in the reply header: back to the row, the reply kept. */
    fun putReplyAside() {
        dictation.cancel()
        replies.suspend()
    }

    /**
     * A persona on the row: the composer for [template] on the message
     * [acquire] finds, or the reply put aside, resumed. Generates nothing.
     */
    fun openReply(template: ReplyTemplate, acquire: () -> ContextTextProvider.Result) {
        dictation.cancel()
        compose.clear()
        if (replies.session != null && replies.isSuspended) {
            replies.resume(template)
            return
        }
        when (val result = acquire()) {
            is ContextTextProvider.Result.Success ->
                replies.open(template, result.context.text, result.context.source, null)
            is ContextTextProvider.Result.Failure ->
                replies.open(template, "", null, result.error)
        }
    }

    /** ✕, or a finished Insert: both panels and any recording go. */
    fun closeAll() {
        dictation.cancel()
        replies.clear()
        compose.clear()
    }

    /**
     * "Reply to copied" in Create: the copied message, read now because the
     * user tapped, becomes the source of a reply by [template], and what they
     * typed or dictated in Create becomes its instruction. Nothing is
     * generated - the reply composer opens and waits for Reply.
     *
     * In a password field nothing is read at all. With nothing usable copied,
     * Create stays as it is and says "Copy a message first".
     */
    fun replyToCopied(
        template: ReplyTemplate?,
        secureField: Boolean,
        acquire: () -> ContextTextProvider.Result
    ): CopiedReply {
        val created = compose.session ?: return CopiedReply.Refused
        if (secureField || template == null) return CopiedReply.Refused
        if (created.flow.stage != ReplyComposerFlow.Stage.Composing || dictation.isActive) return CopiedReply.Refused

        return when (val result = acquire()) {
            is ContextTextProvider.Result.Failure -> {
                compose.showError(result.error)
                CopiedReply.NothingCopied(result.error)
            }
            is ContextTextProvider.Result.Success -> {
                val request = created.instruction.text
                dictation.cancel()
                compose.clear()
                replies.open(template, result.context.text, result.context.source, null)
                replies.session?.instruction?.set(request)
                ReplyLog.event { "create: reply to copied, request length ${request.length}" }
                CopiedReply.Opened
            }
        }
    }

    // ------------------------------------------------------------- lifecycle

    /** The keyboard went away: the microphone is released, the panels are kept for a while. */
    fun keyboardHidden(now: Long) {
        dictation.release()
        replies.park(now)
        compose.park(now)
    }

    /** The keyboard is back: a recent panel returns; in a password field, nothing AI does. */
    fun keyboardShown(now: Long, secureField: Boolean) {
        replies.restoreIfRecent(now)
        compose.restoreIfRecent(now)
        if (secureField) {
            dictation.cancel()
            if (replies.session != null) replies.suspend()
            // Nothing AI-written goes into a password field.
            compose.clear()
        }
        dictation.shown()
    }

    companion object {
        /**
         * The persona a reply to the copied message is written by: the last
         * one used if it is still on the row, else Friend, else the first.
         */
        fun personaForCopied(configuration: ReplyConfiguration, lastUsedId: String?): ReplyTemplate? {
            val visible = configuration.visibleTemplates
            return visible.firstOrNull { it.id == lastUsedId }
                ?: visible.firstOrNull { it.id == RelationshipKind.FRIEND.raw }
                ?: visible.firstOrNull()
                ?: configuration.template(RelationshipKind.FRIEND.raw)
        }
    }
}
