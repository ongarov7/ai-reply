package kz.yerek.aireply.keyboard.reply

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kz.yerek.aireply.ai.AILimits
import kz.yerek.aireply.ai.AIReplyException
import kz.yerek.aireply.ai.PolishService
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.core.text.codePointLength
import kz.yerek.aireply.keyboard.input.KeyboardTextFieldState
import kz.yerek.aireply.platform.ReplyLog

/**
 * After the user pauses while writing an instruction, offers a cleaner version
 * of it on a chip (DESIGN §5.5, §6.5): typos and punctuation fixed, same words
 * and meaning. Taking it is one tap, and Undo stays for a few seconds.
 *
 * Нұсқау жазып болған соң: түзетілген нұсқа ұсынылады, бір түртумен қабылданады.
 *
 * RULES. Only an instruction is ever sent - never the copied message, never a
 * reply. A request needs a pause of [pauseMs], a real sentence
 * ([PolishService.qualifies]), text that changed since the last request, and
 * [isAllowed] (the setting, the server flag, no reply being written). An edit
 * cancels whatever was pending, and a late answer for older text is dropped
 * (the same ticket pattern as the reply controllers). Failures are never
 * shown; after [MAX_FAILURES] in a row the keyboard stops asking until it is
 * shown again ([reset]). A suggestion longer than the instruction may be
 * ([limit]) is never offered or taken: the request would only be refused.
 * The chip belongs to its instruction and goes when typing moves to another
 * field ([focusMoved]).
 */
class InstructionPolish(
    private val scope: CoroutineScope,
    private val service: PolishService,
    private val pauseMs: Long = PAUSE_MS,
    private val undoMs: Long = UNDO_MS
) {

    /** A cleaner version of [field]'s text, [original], offered on the chip. */
    class Offer(val field: KeyboardTextFieldState, val original: String, val text: String)

    /** The user took an offer: Undo puts [previous] back while [field] still holds [applied]. */
    class Taken(val field: KeyboardTextFieldState, val previous: String, val applied: String)

    var offer: Offer? by mutableStateOf(null)
        private set

    var taken: Taken? by mutableStateOf(null)
        private set

    /** Read when the pause ends: the setting is on, the server offers it, no reply is being written. */
    var isAllowed: () -> Boolean = { false }

    /** The layout on screen, sent as a language hint. */
    var inputLanguage: () -> KeyboardLanguage? = { null }

    /** The instruction's character limit, the one the server enforces on Reply and Write. */
    var limit: () -> Int = { AILimits.current.instructionCharacters }

    private var job: Job? = null
    private var undoJob: Job? = null

    /** The field the pending request is for; null when nothing is pending. */
    private var asking: KeyboardTextFieldState? = null

    /** Bumped by every edit, so an answer for older text never lands. */
    private var ticket = 0
    private var failures = 0

    /** The trimmed text last sent, or last settled by the user: not asked about again. */
    private var settled: String? = null

    /** The user edited the instruction [field]: the chip goes, and a pause asks again. */
    fun edited(field: KeyboardTextFieldState) {
        dismiss()
        if (failures >= MAX_FAILURES) return
        val mine = ticket
        asking = field
        job = scope.launch {
            delay(pauseMs)
            val text = field.text.trim()
            val maxLength = limit()
            if (text == settled || !PolishService.qualifies(text, maxLength) || !isAllowed() || !service.isAvailable) return@launch
            settled = text
            val polished = try {
                service.polish(PolishService.Request(text, inputLanguage()), maxLength)
            } catch (failure: AIReplyException) {
                // Never shown: the user asked for nothing. Counted, so a dead
                // connection does not cost a request after every pause.
                failures++
                ReplyLog.event { "polish failed: ${failure.error::class.simpleName}, $failures in a row" }
                return@launch
            }
            failures = 0
            if (mine != ticket || polished == null || field.text.trim() != text) return@launch
            asking = null
            offer = Offer(field, field.text, polished)
        }
    }

    /** The polished text to show while [field] is the one being typed in: only its own offer. */
    fun offerFor(field: KeyboardTextFieldState?): String? = offer?.takeIf { field != null && it.field === field }?.text

    /** Whether Undo shows while [field] is the one being typed in. */
    fun canUndo(field: KeyboardTextFieldState?): Boolean = field != null && taken?.field === field

    /**
     * Typing moved to [field] (null: no field). An offer, its Undo and a
     * request still pending all belong to one instruction; typing anywhere
     * else - the copied message, a reply - drops them.
     */
    fun focusMoved(field: KeyboardTextFieldState?) {
        val owner = offer?.field ?: taken?.field ?: asking ?: return
        if (owner !== field) dismiss()
    }

    /**
     * Takes the offer: the instruction becomes the polished text with the
     * caret at the end, and Undo shows for [undoMs]. Returns the field that
     * changed, or null when the offer no longer fits the text.
     */
    fun accept(): KeyboardTextFieldState? {
        val current = offer ?: return null
        dismiss()
        if (current.field.text != current.original || current.text.codePointLength() > limit()) return null
        current.field.set(current.text)
        settled = current.text.trim()
        taken = Taken(current.field, current.original, current.text)
        undoJob = scope.launch {
            delay(undoMs)
            taken = null
        }
        return current.field
    }

    /** Puts the instruction back as the user wrote it. Returns the field that changed, or null. */
    fun undo(): KeyboardTextFieldState? {
        val current = taken ?: return null
        dismiss()
        if (current.field.text != current.applied) return null
        current.field.set(current.previous)
        // The user said no to this suggestion: do not offer it again.
        settled = current.previous.trim()
        return current.field
    }

    /** Nothing pending, nothing shown: a reply started, the panel closed, the keyboard went away. */
    fun dismiss() {
        ticket++
        job?.cancel()
        job = null
        asking = null
        undoJob?.cancel()
        undoJob = null
        offer = null
        taken = null
    }

    /** The keyboard is shown again: failures are forgiven. */
    fun reset() {
        dismiss()
        failures = 0
        settled = null
    }

    companion object {
        /** How long the user has to stop typing before a suggestion is asked for. */
        const val PAUSE_MS = 1_400L

        /** How long Undo stays after a suggestion was taken. */
        const val UNDO_MS = 5_000L

        /** Failures in a row after which the keyboard stops asking. */
        const val MAX_FAILURES = 2
    }
}
