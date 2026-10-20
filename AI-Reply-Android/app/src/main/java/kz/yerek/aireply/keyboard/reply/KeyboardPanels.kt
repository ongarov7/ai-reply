package kz.yerek.aireply.keyboard.reply

import kz.yerek.aireply.ai.AILimits
import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.core.text.codePointLength
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
 *  * Voice input records for the instruction of the panel on screen while
 *    that instruction is being written; its words are recognised with the
 *    layout on screen (and the user's other layouts) as hints - the same for
 *    the reply composer and Create.
 *  * Write and Reply wait while a recording is on or being recognised: the
 *    words belong in the request, and a request is never started for the
 *    user. An instruction past its limit waits to be shortened - dictated
 *    words are never cut to fit.
 *  * Closing a panel, New, another persona or leaving Create for a reply drop
 *    a recording; the keyboard going away pauses one (the microphone closes
 *    at once) and a request already sent runs on.
 *
 * Pure keyboard state, no Android: the service supplies the clipboard read
 * and the layout.
 */
class KeyboardPanels(
    val replies: ReplySessionController,
    val compose: ComposeSessionController,
    val dictation: DictationController,
    private val layout: () -> KeyboardLanguage,
    private val instructionLimit: () -> Int = { AILimits.current.instructionCharacters },
    /** The user's layouts, the languages they write in: hints for recognising speech. */
    private val layouts: () -> Collection<KeyboardLanguage> = { KeyboardLanguage.CYCLE_ORDER }
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

        /**
         * The copied text is still being read, off the main thread: Create
         * stays as it is, and [read]'s result goes to [replyToCopied] again.
         */
        class Reading(val read: ContextTextProvider.Result.Deferred) : CopiedReply

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

    /** Write and Reply are available: no recording is on or being recognised. */
    val canStartRequest: Boolean get() = !dictation.holdsRequests

    /** Languages to recognise speech in, the layout on screen first. */
    val languageHints: List<String> get() = RecognitionLanguage.hints(layout(), layouts())

    // ------------------------------------------------------------ microphone

    /** The mic button, in either panel. */
    fun microphoneTapped() {
        val field = dictationField ?: return
        dictation.start(DictationTarget.Instruction(field, instructionLimit()), languageHints)
    }

    /** ҚАЗ / РУС / ENG changed: a recording not yet sent is recognised with the new layout first. */
    fun layoutChanged() = dictation.layoutChanged(languageHints)

    // -------------------------------------------------------------- requests

    /**
     * Write / Reply, Stop, Try again. Never starts a request while a recording
     * is on or being recognised, nor with an instruction past its limit.
     */
    fun primaryTapped() {
        when {
            compose.isActive -> when {
                compose.flow.isGenerating -> compose.stop()
                canStartRequest && fitsLimit(compose.session?.instruction?.text) -> {
                    dictation.requestStarted()
                    compose.generate()
                }
            }
            replies.isComposing -> when {
                replies.flow.isGenerating -> replies.stop()
                canStartRequest && fitsLimit(replies.session?.instruction?.text) -> {
                    dictation.requestStarted()
                    replies.generate()
                }
            }
        }
    }

    /** An instruction is checked here, against the limit on screen - never cut to fit. */
    private fun fitsLimit(instruction: String?): Boolean =
        instruction == null || instruction.codePointLength() <= instructionLimit()

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
     *
     * Returns the read still to be done when the copied text is behind
     * another app's provider: the composer opens at once with no message, and
     * the caller hands the result to [lateSource] when it comes.
     */
    fun openReply(
        template: ReplyTemplate,
        acquire: () -> ContextTextProvider.Result
    ): ContextTextProvider.Result.Deferred? {
        dictation.cancel()
        compose.clear()
        if (replies.session != null && replies.isSuspended) {
            replies.resume(template)
            return null
        }
        when (val result = acquire()) {
            is ContextTextProvider.Result.Success ->
                replies.open(template, result.context.text, result.context.source, null)
            is ContextTextProvider.Result.Failure ->
                replies.open(template, "", null, result.error, result.miss)
            is ContextTextProvider.Result.Deferred -> {
                replies.open(template, "", null, null)
                return result
            }
        }
        return null
    }

    /**
     * A clipboard read that finished off the main thread, for the reply
     * [session] it was started for (by a persona or by Paste). It lands only
     * while that session is the one on screen; text arriving for a message
     * field the user has typed into meanwhile is dropped - their words win.
     */
    fun lateSource(session: ReplySession, result: ContextTextProvider.Result, replacing: Boolean) {
        if (replies.session !== session || !replies.isComposing) return
        if (!replacing && session.source.text.isNotBlank()) return
        when (result) {
            is ContextTextProvider.Result.Success -> replies.replaceSource(result.context.text)
            is ContextTextProvider.Result.Failure -> replies.showError(result.error, result.miss)
            is ContextTextProvider.Result.Deferred -> Unit
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
        if (created.flow.stage != ReplyComposerFlow.Stage.Composing || dictation.holdsRequests) return CopiedReply.Refused

        return when (val result = acquire()) {
            is ContextTextProvider.Result.Failure -> {
                compose.showError(result.error, result.miss)
                CopiedReply.NothingCopied(result.error)
            }
            is ContextTextProvider.Result.Deferred -> CopiedReply.Reading(result)
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

    /** The keyboard went away: the microphone closes, the panels are kept for a while. */
    fun keyboardHidden(now: Long) {
        dictation.hidden()
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
