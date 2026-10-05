package kz.yerek.aireply.keyboard.reply

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.ai.AIReplyException
import kz.yerek.aireply.ai.ComposeService
import kz.yerek.aireply.ai.ReplyDraftNormalizer
import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.keyboard.input.KeyboardTextFieldState
import kz.yerek.aireply.platform.ReplyLog

/**
 * One "Create" session: what the user asked for and the versions written.
 *
 * Deliberately its own type, not a [ReplySession] with an empty message: there
 * is no source, no persona and no clipboard origin, so nothing copied and no
 * reply can leak into it. The stages are the reply composer's
 * ([ReplyComposerFlow]), which is exactly the compose state machine too:
 *
 *     composing ──Write──▶ generating ──▶ result ──Insert──▶ host field
 *         ▲  empty: error          fail: back, error shown        │
 *         └── Back (versions kept) ◀── result ──Regenerate──▶ generating
 *     New: everything is discarded, back to an empty instruction.
 */
class ComposeSession {

    /** What the user wants written. Typed or a quick intent. */
    val instruction = KeyboardTextFieldState()

    /** The version on screen, as the keys edit it. Mirrors [flow]'s current draft. */
    val draft = KeyboardTextFieldState()

    var flow: ReplyComposerFlow by mutableStateOf(ReplyComposerFlow())
        internal set

    /** Whether "New" has anything to clear. */
    val hasContent: Boolean get() = instruction.text.isNotBlank() || !flow.drafts.isEmpty

    internal fun showCurrentVersion() {
        val text = flow.draftText
        if (draft.text != text) draft.set(text)
    }
}

/**
 * Drives instruction → AI → versions for Create, with the reply controller's
 * guarantees: nothing is generated until the user taps Write or Regenerate;
 * one request at a time; a late answer to a stopped request never lands; a
 * failure never loses the instruction. It has no access to the clipboard or
 * the host field - the only text it can send is the instruction typed here.
 */
class ComposeSessionController(
    private val scope: CoroutineScope,
    private val service: ComposeService,
    private val normalizer: ReplyDraftNormalizer
) {
    var session: ComposeSession? by mutableStateOf(null)
        private set

    /** The APP's language, for the server when the instruction's own is unclear. */
    var uiLanguage: AppLanguage = AppLanguage.ENGLISH

    /** The layout on screen, read when Write is tapped: a language hint for the server. */
    var inputLanguage: () -> KeyboardLanguage? = { null }

    /** A change the user did not trigger directly (an answer, a failure). */
    var onAsyncChange: () -> Unit = {}

    val isActive: Boolean get() = session != null
    val flow: ReplyComposerFlow get() = session?.flow ?: ReplyComposerFlow()

    private var job: Job? = null
    private var ticket = 0
    private var parkedAt = 0L

    /** Create was tapped: a fresh, empty session. It does NOT generate. */
    fun open() {
        cancelJob()
        session = ComposeSession()
        parkedAt = 0L
        ReplyLog.event { "ai_compose_opened" }
    }

    /** Write, or Regenerate from a result: the same instruction, a NEW version. */
    fun generate() {
        val current = session ?: return
        if (job != null) return

        val validation = ComposeService.validate(current.instruction.text)
        if (validation is ComposeService.ValidationResult.Invalid) {
            current.flow = current.flow.failing(validation.error)
            return
        }
        val regenerating = !current.flow.drafts.isEmpty
        current.flow = current.flow.startingGeneration() ?: return
        ReplyLog.event { if (regenerating) "ai_compose_regenerated" else "ai_compose_generate_started" }

        val mine = ++ticket
        val started = System.currentTimeMillis()
        val request = ComposeService.Request(
            instruction = current.instruction.text,
            uiLanguage = uiLanguage,
            isRegeneration = regenerating,
            inputLanguage = inputLanguage()
        )
        job = scope.launch {
            try {
                val message = service.compose(request)
                if (mine != ticket || session !== current) return@launch
                val text = normalizer.normalize(message.text).ifBlank { message.text.trim() }
                current.flow = current.flow.receiving(text)
                current.showCurrentVersion()
                ReplyLog.event { "ai_compose_generate_success, ${System.currentTimeMillis() - started} ms" }
                onAsyncChange()
            } catch (cancellation: CancellationException) {
                throw cancellation
            } catch (exception: AIReplyException) {
                if (mine == ticket && session === current) {
                    current.flow = current.flow.failing(exception.error)
                    ReplyLog.event { "ai_compose_generate_failed: ${exception.error}" }
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
    }

    /** Stop. The instruction and versions stay; a late answer is ignored. */
    fun stop() {
        cancelJob()
        val current = session ?: return
        current.flow = current.flow.cancellingGeneration()
        current.showCurrentVersion()
    }

    /** The instruction was edited: an old error no longer describes it. */
    fun instructionEdited() {
        val current = session ?: return
        current.flow = current.flow.clearingError()
    }

    /** Back to the instruction, to change it. The versions are kept. */
    fun back() {
        val current = session ?: return
        if (current.flow.isGenerating) cancelJob()
        current.flow = current.flow.goingBack()
    }

    /** New: the instruction, every version and any error are discarded. */
    fun reset() {
        if (session == null) return
        cancelJob()
        session = ComposeSession()
        ReplyLog.event { "ai_compose_reset" }
    }

    fun toggleEditing() {
        val current = session ?: return
        current.flow = if (current.flow.isEditingDraft) current.flow.endingEditing() else current.flow.beginningEditing()
    }

    fun beginEditingForTyping(): Boolean {
        val current = session ?: return false
        current.flow = current.flow.beginningEditingForTyping() ?: return false
        return true
    }

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

    /** The message on screen, edits included. Never the instruction. */
    fun requestInsert(hostHasText: Boolean): ReplyComposerFlow.InsertDecision {
        val current = session ?: return ReplyComposerFlow.InsertDecision.NothingToInsert
        val (next, decision) = current.flow.requestingInsert(hostHasText)
        current.flow = next
        return when (decision) {
            is ReplyComposerFlow.InsertDecision.Insert -> {
                ReplyLog.event { "ai_compose_inserted" }
                ReplyComposerFlow.InsertDecision.Insert(decision.text.trim())
            }
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

    /** A failure found before any request could start. */
    fun showError(error: AIReplyError) {
        val current = session ?: return
        if (current.flow.isGenerating) return
        current.flow = current.flow.failing(error)
    }

    /** The keyboard went away: stop any request, keep the rest for a while. */
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

    /** Leaving Create: every trace of the instruction and the message goes. */
    fun clear() {
        cancelJob()
        session = null
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
