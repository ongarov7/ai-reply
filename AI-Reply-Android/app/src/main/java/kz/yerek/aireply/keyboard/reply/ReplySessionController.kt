package kz.yerek.aireply.keyboard.reply

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.ai.AIReplyException
import kz.yerek.aireply.ai.AIReplyService
import kz.yerek.aireply.ai.ReplyDraftNormalizer
import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.domain.model.ReplyConfiguration
import kz.yerek.aireply.domain.model.ReplyTemplate
import kz.yerek.aireply.keyboard.input.KeyboardTextFieldState
import kz.yerek.aireply.keyboard.input.ReplyContextSource
import kz.yerek.aireply.platform.ReplyLog

/**
 * One reply being written: the message it answers, what the user wants said,
 * and every version the model produced.
 *
 *     SOURCE MESSAGE  !=  INSTRUCTION  !=  REPLY
 *
 * Only the reply can reach the host application.
 */
class ReplySession(template: ReplyTemplate, source: String, val acquiredFrom: ReplyContextSource?) {

    var template: ReplyTemplate by mutableStateOf(template)

    /** What the other person wrote. Editable, because a copy can carry junk. */
    val source = KeyboardTextFieldState(source)

    /** What this user wants the reply to do. Typed, dictated or a quick intent. */
    val instruction = KeyboardTextFieldState()

    /** The version on screen, as the keys edit it. Mirrors [flow]'s current draft. */
    val draft = KeyboardTextFieldState()

    var flow: ReplyComposerFlow by mutableStateOf(ReplyComposerFlow())
        internal set

    /** Puts the current version's text in the draft field, caret at the end. */
    internal fun showCurrentVersion() {
        val text = flow.draftText
        if (draft.text != text) draft.set(text)
    }
}

/**
 * Drives source → instruction → AI → versions → insert.
 *
 * GENERATION IS NEVER AUTOMATIC. Copying text does not start a request;
 * opening the keyboard does not; picking a persona does not. Exactly one thing
 * does: the user tapping Reply, Regenerate or Try again.
 *
 * PRIVACY. Everything lives in memory. A session the user walks away from is
 * kept for a few minutes (they went to copy one more message) and then
 * dropped; nothing is written to disk or logged.
 */
class ReplySessionController(
    private val scope: CoroutineScope,
    private val service: AIReplyService,
    private val normalizer: ReplyDraftNormalizer
) {
    var session: ReplySession? by mutableStateOf(null)
        private set

    /** The persona row is showing over a session the user has not finished. */
    var isSuspended: Boolean by mutableStateOf(false)
        private set

    /** Configuration snapshot taken when the keyboard appeared. */
    var configuration: ReplyConfiguration = ReplyConfiguration.INITIAL

    /** The APP's language: names the persona in the prompt as the chip did. */
    var uiLanguage: AppLanguage = AppLanguage.ENGLISH

    /** The layout on screen, read when Reply is tapped: a language hint for the server. */
    var inputLanguage: () -> KeyboardLanguage? = { null }

    /**
     * Called after a change the user did not trigger directly - a reply
     * arriving, a request failing - so the keyboard can follow (shift, the
     * return key).
     */
    var onAsyncChange: () -> Unit = {}

    val isComposing: Boolean get() = session != null && !isSuspended
    val flow: ReplyComposerFlow get() = session?.flow ?: ReplyComposerFlow()

    private var job: Job? = null
    /** Increments per request, so a stopped request's answer can never land. */
    private var ticket = 0
    private var parkedAt = 0L

    // ---------------------------------------------------------------- entry

    /** The user picked a persona on the row. Opens the composer; generates nothing. */
    fun open(template: ReplyTemplate, source: String, from: ReplyContextSource?, error: AIReplyError?) {
        cancelJob()
        val opened = ReplySession(template, source, from)
        // An empty clipboard is not an error worth shouting about: the
        // composer opens anyway and its placeholder says what to do.
        if (error != null && error != AIReplyError.NoSourceMessage) {
            opened.flow = opened.flow.failing(error)
        }
        session = opened
        isSuspended = false
        ReplyLog.event { "composer opened, source ${from ?: "none"}, length ${source.length}" }
    }

    /** Picked a persona while a session was suspended: everything stays, the persona changes. */
    fun resume(template: ReplyTemplate) {
        val current = session ?: return
        current.template = template
        current.flow = current.flow.resuming()
        current.showCurrentVersion()
        isSuspended = false
    }

    /** Back to the persona row with the session kept. */
    fun suspend() {
        val current = session ?: return
        cancelJob()
        current.flow = current.flow.cancellingGeneration()
        isSuspended = true
    }

    /** Explicit Paste: the only other place the clipboard is read. */
    fun replaceSource(text: String) {
        val current = session ?: return
        current.source.set(text)
        current.flow = current.flow.clearingError()
    }

    fun showError(error: AIReplyError) {
        val current = session ?: return
        current.flow = current.flow.copy(error = error.takeIf { it != AIReplyError.Cancelled })
    }

    /** The source or the instruction was edited: an old error no longer describes it. */
    fun composerEdited() {
        val current = session ?: return
        current.flow = current.flow.clearingError()
    }

    // ----------------------------------------------------------- generation

    /**
     * Reply, Regenerate and Try again. Same message, same instruction, same
     * persona; a regeneration adds a version and never replaces one.
     */
    fun generate() {
        val current = session ?: return
        if (job != null) return

        val validation = AIReplyService.validate(current.source.text)
        if (validation is AIReplyService.ValidationResult.Invalid) {
            current.flow = current.flow.failing(validation.error)
            return
        }
        val message = (validation as AIReplyService.ValidationResult.Valid).message
        val started = current.flow.startingGeneration() ?: return
        current.flow = started

        val mine = ++ticket
        val request = AIReplyService.Request(
            message = message,
            template = current.template,
            configuration = configuration,
            uiLanguage = uiLanguage,
            instruction = current.instruction.text,
            inputLanguage = inputLanguage()
        )
        // Started only once it is the job: on Main.immediate a request that
        // fails before its first suspension (no account, no consent) has
        // already finished inside launch, and assigning it afterwards would
        // leave a dead job that refuses every Try again.
        val running = scope.launch(start = CoroutineStart.LAZY) {
            try {
                val reply = service.generate(request)
                if (mine != ticket || session !== current) return@launch
                // The model's formatting is tidied once, as it arrives. What
                // the user then sees - and edits - is exactly what Insert uses.
                val text = normalizer.normalize(reply.text).ifBlank { reply.text.trim() }
                current.flow = current.flow.receiving(text)
                current.showCurrentVersion()
                ReplyLog.event { "reply received, version ${current.flow.drafts.position}" }
                onAsyncChange()
            } catch (cancellation: CancellationException) {
                throw cancellation
            } catch (exception: AIReplyException) {
                if (mine == ticket && session === current) {
                    current.flow = current.flow.failing(exception.error)
                    onAsyncChange()
                }
            } catch (throwable: Throwable) {
                if (mine == ticket && session === current) {
                    current.flow = current.flow.failing(AIReplyError.ServiceUnavailable)
                    onAsyncChange()
                }
            } finally {
                if (mine == ticket) job = null
            }
        }
        job = running
        running.start()
    }

    /** Stop. Everything typed stays; a late answer is ignored. */
    fun stop() {
        cancelJob()
        val current = session ?: return
        current.flow = current.flow.cancellingGeneration()
        current.showCurrentVersion()
    }

    // ----------------------------------------------------------- navigation

    fun back() {
        val current = session ?: return
        if (current.flow.isGenerating) cancelJob()
        current.flow = current.flow.goingBack()
    }

    /** Edit / Done on the reply. */
    fun toggleEditing() {
        val current = session ?: return
        current.flow = if (current.flow.isEditingDraft) current.flow.endingEditing() else current.flow.beginningEditing()
    }

    /** A key was pressed while the reply is on screen: it becomes editable. */
    fun beginEditingForTyping(): Boolean {
        val current = session ?: return false
        val next = current.flow.beginningEditingForTyping() ?: return false
        current.flow = next
        return true
    }

    /** The keys changed the draft field. */
    fun draftEdited() {
        val current = session ?: return
        current.flow = current.flow.editingDraft(current.draft.text)
    }

    fun showPreviousVersion() {
        val current = session ?: return
        current.flow = current.flow.showingPreviousVersion()
        current.showCurrentVersion()
    }

    fun showNextVersion() {
        val current = session ?: return
        current.flow = current.flow.showingNextVersion()
        current.showCurrentVersion()
    }

    // --------------------------------------------------------------- insert

    fun requestInsert(hostHasText: Boolean): ReplyComposerFlow.InsertDecision {
        val current = session ?: return ReplyComposerFlow.InsertDecision.NothingToInsert
        val (next, decision) = current.flow.requestingInsert(hostHasText)
        current.flow = next
        return when (decision) {
            is ReplyComposerFlow.InsertDecision.Insert -> ReplyComposerFlow.InsertDecision.Insert(decision.text.trim())
            else -> decision
        }
    }

    fun resolveConflict(choice: ReplyComposerFlow.ConflictChoice): ReplyComposerFlow.ConflictResolution {
        val current = session ?: return ReplyComposerFlow.ConflictResolution.Cancelled
        val (next, resolution) = current.flow.resolvingConflict(choice)
        current.flow = next
        return when (resolution) {
            is ReplyComposerFlow.ConflictResolution.Replace -> ReplyComposerFlow.ConflictResolution.Replace(resolution.text.trim())
            is ReplyComposerFlow.ConflictResolution.Append -> ReplyComposerFlow.ConflictResolution.Append(resolution.text.trim())
            ReplyComposerFlow.ConflictResolution.Cancelled -> resolution
        }
    }

    // ------------------------------------------------------------ lifecycle

    /** The keyboard went away with a session open: keep it for a while. */
    fun park(now: Long) {
        if (session == null) return
        stop()
        parkedAt = now
    }

    /** The keyboard came back: a recent session is shown again, an old one dropped. */
    fun restoreIfRecent(now: Long) {
        if (session == null || parkedAt == 0L) return
        if (now - parkedAt > PARK_LIFETIME_MS) clear()
        parkedAt = 0L
    }

    /** Drops everything, cancelling any request. */
    fun clear() {
        cancelJob()
        session = null
        isSuspended = false
        parkedAt = 0L
    }

    private fun cancelJob() {
        ticket++
        job?.cancel()
        job = null
    }

    private companion object {
        const val PARK_LIFETIME_MS = 10L * 60 * 1000
    }
}
