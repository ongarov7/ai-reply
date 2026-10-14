package kz.yerek.aireply.keyboard.voice

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.ai.AIReplyException
import kz.yerek.aireply.core.text.codePointLength
import kz.yerek.aireply.keyboard.input.KeyboardTextFieldState
import kz.yerek.aireply.platform.ReplyLog
import kz.yerek.aireply.voice.MicPermission
import kz.yerek.aireply.voice.RecordedAudio
import kz.yerek.aireply.voice.RecorderFailure
import kz.yerek.aireply.voice.TranscriptionPurpose
import kz.yerek.aireply.voice.TranscriptionRequest
import kz.yerek.aireply.voice.VoiceRecorder
import kz.yerek.aireply.voice.VoiceTranscriber
import java.util.UUID

/** Where a recording's words go. */
sealed interface DictationTarget {

    /** The instruction of an AI panel - the reply composer's or Create's - with its character limit. */
    class Instruction(val field: KeyboardTextFieldState, val limit: Int?) : DictationTarget

    /** The app's own text field, through the InputConnection. */
    class Host(val field: HostDictation) : DictationTarget

    val purpose: TranscriptionPurpose
        get() = if (this is Instruction) TranscriptionPurpose.INSTRUCTION else TranscriptionPurpose.DICTATION
}

/** The app's text field, as dictation needs it; the keyboard service implements it over its InputConnection. */
interface HostDictation {
    /** Still the field the recording was made for, with the keyboard on screen over it. */
    val isLive: Boolean

    /** Puts [text] in at the caret, spaced and capitalised the way typing would; false when it could not. */
    fun insert(text: String): Boolean
}

/** The microphone permission, as the keyboard can see and ask for it. */
interface MicAccess {
    fun isGranted(): Boolean

    /** Shows the system dialog and waits for the answer; null when it could not be shown. */
    suspend fun request(): MicPermission.Outcome?

    /** The app's system settings, where a refused permission can be turned back on. */
    fun openSettings()
}

/**
 * Where a recording is. One session at a time; CANCELED is where a session
 * ends when it is thrown away, and every late callback for it is ignored.
 */
enum class DictationPhase {
    /** No recording. */
    IDLE,

    /** The microphone is opening; nothing captured yet, the clock does not run. */
    STARTING,

    /** Capturing. Only here does the 40-second allowance get used. */
    RECORDING,

    /** The microphone is closed, the audio and the remaining time are kept. */
    PAUSED,

    /** Capture ended; the audio is being written into one file. */
    FINALIZING,

    /** The file is with the server (OpenAI gpt-transcribe). Cancel still works. */
    TRANSCRIBING,

    /** The connection failed once; one automatic retry is waiting. */
    RECOVERING,

    /** The words are in: in the instruction, with Undo - or, for the app's field the keyboard left meanwhile, waiting for Insert. */
    REVIEWING,

    /** The recording or the transcription failed; the audio is kept for Try again. */
    ERROR,

    /** Thrown away. Never published: the controller is back to [IDLE]. */
    CANCELED
}

/** A sentence the voice line or bar shows; not part of a recording's controls. */
sealed interface DictationMessage {
    data object PermissionRequired : DictationMessage
    data object PermissionDenied : DictationMessage

    /** The microphone stopped or would not start. */
    data class Microphone(val failure: RecorderFailure) : DictationMessage

    /** Less than [DictationController.MIN_RECORDING_MS] of audio. */
    data object TooShort : DictationMessage

    /** The recording was transcribed and held no words. */
    data object NothingHeard : DictationMessage

    /** Transcription failed, or could not be asked for (sign-in, consent, today's limit). */
    data class Failed(val error: AIReplyError) : DictationMessage

    /** The instruction is longer than its limit with the dictated words in it: nothing was cut. */
    data class TooLong(val limit: Int) : DictationMessage
}

/** What a screen reader should hear, and when a light haptic is due. Never the words. */
sealed interface DictationEvent {
    /** The microphone is capturing. */
    data object Recording : DictationEvent

    data object Paused : DictationEvent

    /** The recording went to be recognised. */
    data object Recognizing : DictationEvent

    /** The words landed. */
    data object Inserted : DictationEvent

    /** Thrown away. */
    data object Cancelled : DictationEvent

    /** [secondsLeft] seconds of the allowance are left: 10, then 5, then 0 (the limit). */
    data class TimeLeft(val secondsLeft: Int) : DictationEvent

    data class Message(val message: DictationMessage) : DictationEvent
}

/**
 * The keyboard's voice input, shared by the app's field (🎙 on the persona
 * row) and the instructions of the two AI panels.
 *
 * Пернетақтаның дауыспен енгізуі: жазу, кідірту, жалғастыру, тану.
 *
 * HOW IT WORKS. The microphone records on the phone ([VoiceRecorder]); Done
 * turns everything recorded into one file, which the AI Reply server sends to
 * OpenAI `gpt-transcribe` ([VoiceTranscriber]); the words go in at the caret.
 * A pause in speech never ends anything - only Pause, Done, Cancel or the
 * limit do, so thinking mid-sentence is safe.
 *
 * WHAT IT GUARANTEES:
 *  * At most [maxRecordingMs] of audio per recording, counted from the audio
 *    actually captured: pauses, a slow microphone and the upload use none of
 *    it. The recorder itself stops at the limit; the controller then finishes
 *    the recording the way Done does.
 *  * Pause closes the microphone and keeps the audio and the time left;
 *    Resume continues both. Once the limit is reached there is no Resume.
 *  * Cancel stops the microphone at once, abandons the request and deletes the
 *    audio. What the field held before is untouched: nothing goes into a field
 *    until the words are back.
 *  * Nothing AI is ever started by voice. Words fill a field; Reply and Write
 *    still have to be tapped. The instruction's limit is never applied by
 *    cutting words: an instruction over it stays whole, says so, and waits for
 *    the user to shorten it.
 *  * Words go only into the field the recording was made for, while it is on
 *    screen. The app's field gets them through [HostDictation]; when the
 *    keyboard left meanwhile they wait for an explicit Insert, and for another
 *    field they are dropped.
 *  * One recording and one request at a time; a repeated request carries the
 *    same id, so the server answers it from its cache instead of twice.
 *
 * Every method is called on the main thread.
 */
class DictationController(
    private val scope: CoroutineScope,
    private val newRecorder: () -> VoiceRecorder,
    private val transcriber: VoiceTranscriber,
    private val mic: MicAccess,
    /** Monotonic milliseconds, for the permission debounce and how long a hidden recording is kept. */
    private val clock: () -> Long,
    val maxRecordingMs: Long = MAX_RECORDING_MS,
    private val newRequestId: () -> String = { UUID.randomUUID().toString() }
) {
    /** Where the recording is; [DictationPhase.IDLE] without one. */
    var phase: DictationPhase by mutableStateOf(DictationPhase.IDLE)
        private set

    /** The target of the recording on screen; null while idle. */
    var target: DictationTarget? by mutableStateOf(null)
        private set

    /** Audio captured, pauses excluded. Changes many times a second: read it only where it is drawn. */
    var capturedMs: Long by mutableLongStateOf(0L)
        private set

    /** Loudness of the latest audio, 0..1, measured. Read only where it is drawn. */
    var level: Float by mutableFloatStateOf(0f)
        private set

    /** The allowance is used up: no Resume. */
    var limitReached: Boolean by mutableStateOf(false)
        private set

    /** A sentence to show. Cleared by the next edit or recording. */
    var message: DictationMessage? by mutableStateOf(null)
        private set

    /** Words for the app's field, waiting for Insert ([DictationPhase.REVIEWING] only). */
    var pendingText: String? by mutableStateOf(null)
        private set

    /** Whether [field] is the instruction on screen to dictate into; set by the panels. */
    var isLive: (KeyboardTextFieldState) -> Boolean = { true }

    /** Words were put into an instruction, or taken out again by Undo. */
    var onInstructionChanged: (KeyboardTextFieldState) -> Unit = {}

    /** For the screen reader and the haptics. */
    var onEvent: (DictationEvent) -> Unit = {}

    /** The recording's controls replace the panel's row (or the persona row): recording, paused, recognising, failed. */
    val showsControls: Boolean
        get() = phase in CONTROL_PHASES

    /** Reply and Write wait: the instruction's words are still being recorded or recognised. */
    val holdsRequests: Boolean
        get() = showsControls && target is DictationTarget.Instruction

    /** A recording of the app's field is on screen, in place of the persona row. */
    val showsHostBar: Boolean
        get() = target is DictationTarget.Host && (showsControls || phase == DictationPhase.REVIEWING)

    val canPause: Boolean get() = phase == DictationPhase.RECORDING || phase == DictationPhase.STARTING

    val canResume: Boolean get() = phase == DictationPhase.PAUSED && !limitReached

    val canFinish: Boolean get() = phase == DictationPhase.RECORDING || phase == DictationPhase.PAUSED

    /** Undo takes the dictated words back out of the instruction. */
    val canUndo: Boolean
        get() = phase == DictationPhase.REVIEWING && review?.let { it.field.text == it.after } == true

    val remainingMs: Long get() = (maxRecordingMs - capturedMs).coerceAtLeast(0L)

    // ------------------------------------------------------------- internals

    private class Session(
        val target: DictationTarget,
        var languages: List<String>,
        val recorder: VoiceRecorder,
        val requestId: String
    ) {
        var phase = DictationPhase.STARTING
        var audio: RecordedAudio? = null
        var job: Job? = null
        var retried = false
        /** The last 10 / 5 / 0 seconds announced, so each is said once. */
        var announcedLeft = Int.MAX_VALUE
    }

    /** The instruction as it was before the words went in, for Undo. */
    private class Review(val field: KeyboardTextFieldState, val text: String, val cursor: Int, val after: String)

    private class Pending(val target: DictationTarget, val languages: List<String>, val until: Long)

    private var session: Session? = null
    private var review: Review? = null
    private var pending: Pending? = null
    private var permissionJob: Job? = null
    private var askedAt = Long.MIN_VALUE / 2
    private var shown = false
    private var hiddenAt = 0L

    // ------------------------------------------------------------ user taps

    /**
     * A microphone: records for [target], with [languages] as the hints for
     * recognition. Refused for good: opens Settings. Not yet allowed: asks,
     * and starts when the keyboard is back.
     */
    fun start(target: DictationTarget, languages: List<String>) {
        if (showsControls) return
        settleReview()
        if (phase == DictationPhase.REVIEWING) cancel()
        if (!transcriber.isOffered) return
        transcriber.readiness()?.let { problem ->
            show(DictationMessage.Failed(problem))
            return
        }
        when {
            mic.isGranted() -> begin(target, languages)
            message == DictationMessage.PermissionDenied -> mic.openSettings()
            else -> askForMicrophone(target, languages)
        }
    }

    /** Closes the microphone and keeps the audio and the time left. */
    fun pause() {
        val current = session ?: return
        if (!canPause) return
        current.recorder.pause()
        level = 0f
        moveTo(current, DictationPhase.PAUSED)
        onEvent(DictationEvent.Paused)
    }

    /** Opens the microphone again, with the time that was left. */
    fun resume() {
        val current = session ?: return
        if (!canResume) return
        if (!mic.isGranted()) {
            show(DictationMessage.PermissionRequired)
            return
        }
        message = null
        val failure = current.recorder.resume()
        if (failure != null) {
            show(messageFor(failure))
            return
        }
        moveTo(current, DictationPhase.STARTING)
    }

    /** Ends the recording and sends it to be recognised. */
    fun finish() {
        val current = session ?: return
        if (!canFinish && current.phase != DictationPhase.STARTING) return
        finalize(current)
    }

    /**
     * Throws the recording away: the microphone closes, a request still
     * running is abandoned, the audio is deleted. Words already in an
     * instruction stay (that is what Undo is for). Safe to call any time.
     */
    fun cancel() {
        pending = null
        permissionJob?.cancel()
        permissionJob = null
        val current = session
        if (current != null) {
            // Words already in an instruction are the user's; anything else in flight is dropped.
            val dropped = current.phase in CONTROL_PHASES ||
                (current.phase == DictationPhase.REVIEWING && current.target is DictationTarget.Host)
            end(current)
            if (dropped) onEvent(DictationEvent.Cancelled)
        }
        review = null
        message = null
    }

    /** ERROR: send the same recording again. */
    fun retry() {
        val current = session ?: return
        if (current.phase != DictationPhase.ERROR) return
        val audio = current.audio
        message = null
        if (audio == null) {
            finalize(current)
        } else {
            current.retried = false
            run(current) { transcribe(current, audio) }
        }
    }

    /** REVIEWING for the app's field: put the words in now. */
    fun insertReviewed() {
        val current = session ?: return
        val host = current.target as? DictationTarget.Host ?: return
        val text = pendingText ?: return
        if (current.phase != DictationPhase.REVIEWING) return
        if (!host.field.isLive) return
        if (host.field.insert(text)) onEvent(DictationEvent.Inserted)
        end(current)
    }

    /** REVIEWING an instruction: the dictated words come back out, the instruction is as it was. */
    fun undo() {
        val shown = review ?: return
        if (!canUndo) return
        shown.field.set(shown.text, moveCursorToEnd = false)
        shown.field.moveCursor(shown.cursor)
        review = null
        message = null
        session?.let { end(it) }
        onInstructionChanged(shown.field)
    }

    // ------------------------------------------------------------ the keyboard

    /** A key is about to be typed: a recording in progress pauses, so the keystrokes are not recorded. */
    fun keyPressed() {
        if (canPause) pause()
    }

    /** The user edited a field, tapped an intent or took a polish: old messages go, and a review is settled. */
    fun userEdited() {
        settleReview()
        // A failed transcription keeps saying why: it is what Try again is about.
        if (phase != DictationPhase.ERROR) message = null
    }

    /** Reply or Write was tapped: the reviewed words are the user's now. */
    fun requestStarted() {
        settleReview()
    }

    /** ҚАЗ / РУС / ЕNG changed: the hints follow for a recording not yet sent; an old message goes. */
    fun layoutChanged(languages: List<String>) {
        val current = session
        if (current != null && current.phase !in SENT_PHASES) current.languages = languages
        if (current == null) message = null
    }

    // ------------------------------------------------------------ lifecycle

    /**
     * The keyboard went away: the microphone closes at once (a recording
     * pauses - nothing records with the keyboard gone), a request runs on.
     */
    fun hidden() {
        shown = false
        hiddenAt = clock()
        if (canPause) pause()
    }

    /**
     * The keyboard is back. A recording for a field that is gone, or kept
     * too long, is thrown away; one the permission dialog interrupted starts.
     */
    fun shown() {
        val wasHidden = !shown
        shown = true
        val current = session
        if (current != null) {
            val stale = wasHidden && hiddenAt != 0L && clock() - hiddenAt > HIDDEN_LIFETIME_MS
            if (stale || !targetIsLive(current.target)) {
                ReplyLog.event { "voice: dropped, the field is gone" }
                cancel()
            }
        }
        startPending()
    }

    /** The keyboard is being destroyed. */
    fun release() {
        shown = false
        cancel()
    }

    // ------------------------------------------------------------- recording

    private fun begin(target: DictationTarget, languages: List<String>) {
        pending = null
        message = null
        review = null
        val recorder = newRecorder()
        val current = Session(target, languages, recorder, newRequestId())
        session = current
        this.target = target
        capturedMs = 0L
        level = 0f
        limitReached = false
        pendingText = null
        moveTo(current, DictationPhase.STARTING)
        val failure = recorder.start(maxRecordingMs, Listener(current))
        if (failure != null && session === current) {
            end(current)
            show(messageFor(failure))
        }
    }

    private inner class Listener(private val owner: Session) : VoiceRecorder.Listener {

        private val isCurrent: Boolean get() = session === owner && owner.phase != DictationPhase.CANCELED

        override fun onCapturing() {
            if (!isCurrent || owner.phase != DictationPhase.STARTING) return
            moveTo(owner, DictationPhase.RECORDING)
            onEvent(DictationEvent.Recording)
        }

        override fun onProgress(capturedMs: Long, level: Float) {
            if (!isCurrent) return
            this@DictationController.capturedMs = capturedMs.coerceIn(0L, maxRecordingMs)
            if (owner.phase == DictationPhase.RECORDING) this@DictationController.level = level.coerceIn(0f, 1f)
            announceTimeLeft(owner)
        }

        override fun onLimitReached() {
            if (!isCurrent) return
            capturedMs = maxRecordingMs
            limitReached = true
            level = 0f
            announceTimeLeft(owner)
            if (owner.phase == DictationPhase.RECORDING || owner.phase == DictationPhase.STARTING ||
                owner.phase == DictationPhase.PAUSED
            ) {
                ReplyLog.event { "voice: the ${maxRecordingMs / 1000}s limit, finishing" }
                finalize(owner)
            }
        }

        override fun onFailure(reason: RecorderFailure) {
            if (!isCurrent) return
            ReplyLog.event { "voice: microphone stopped, $reason" }
            level = 0f
            if (capturedMs >= MIN_RECORDING_MS) {
                // What was said so far stays: Resume, Done or Cancel is the user's call.
                if (owner.phase == DictationPhase.RECORDING || owner.phase == DictationPhase.STARTING) {
                    moveTo(owner, DictationPhase.PAUSED)
                }
                show(messageFor(reason))
            } else {
                end(owner)
                show(messageFor(reason))
            }
        }
    }

    /** 10 seconds left, 5, the limit - each said once, as the audio crosses it. */
    private fun announceTimeLeft(current: Session) {
        val leftMs = maxRecordingMs - capturedMs
        val mark = when {
            leftMs <= 0L -> 0
            leftMs <= 5_000L -> 5
            leftMs <= 10_000L -> 10
            else -> return
        }
        if (mark >= current.announcedLeft) return
        current.announcedLeft = mark
        onEvent(DictationEvent.TimeLeft(mark))
    }

    // ---------------------------------------------------------- transcription

    private fun finalize(current: Session) {
        current.job?.cancel()
        level = 0f
        if (capturedMs < MIN_RECORDING_MS && !limitReached) {
            end(current)
            show(DictationMessage.TooShort)
            return
        }
        moveTo(current, DictationPhase.FINALIZING)
        onEvent(DictationEvent.Recognizing)
        run(current) {
            val audio = try {
                current.recorder.finish()
            } catch (cancellation: CancellationException) {
                throw cancellation
            } catch (failure: Exception) {
                ReplyLog.warn(failure) { "voice: the recording could not be written" }
                end(current)
                show(DictationMessage.Microphone(RecorderFailure.UNAVAILABLE))
                return@run
            }
            if (session !== current) return@run
            if (audio == null) {
                end(current)
                show(DictationMessage.TooShort)
                return@run
            }
            current.audio = audio
            transcribe(current, audio)
        }
    }

    /**
     * Sends [audio] and puts the words in; a lost connection is retried once,
     * quietly, with the same request id. Runs inside the session's one job,
     * so Cancel always reaches the request in flight.
     */
    private suspend fun transcribe(current: Session, audio: RecordedAudio) {
        while (session === current) {
            moveTo(current, DictationPhase.TRANSCRIBING)
            val error = try {
                val text = transcriber.transcribe(
                    TranscriptionRequest(audio, current.requestId, current.languages, current.target.purpose)
                )
                if (session !== current) return
                deliver(current, text)
                return
            } catch (cancellation: CancellationException) {
                throw cancellation
            } catch (failure: AIReplyException) {
                failure.error
            } catch (failure: Exception) {
                ReplyLog.warn(failure) { "voice: transcription broke" }
                AIReplyError.ServiceUnavailable
            }
            if (session !== current) return
            ReplyLog.event { "voice: transcription failed, ${error::class.simpleName}" }
            if (error !in RETRIED || current.retried) {
                moveTo(current, DictationPhase.ERROR)
                show(DictationMessage.Failed(error))
                return
            }
            current.retried = true
            moveTo(current, DictationPhase.RECOVERING)
            delay(RETRY_DELAY_MS)
        }
    }

    private fun deliver(current: Session, heard: String) {
        val text = heard.trim()
        ReplyLog.event { "voice: recognised, ${text.length} characters" }
        if (text.isEmpty()) {
            end(current)
            show(DictationMessage.NothingHeard)
            return
        }
        // The words are safe now; the audio is not needed any more.
        current.recorder.discard()
        current.audio = null
        when (val into = current.target) {
            is DictationTarget.Instruction -> {
                if (!isLive(into.field)) {
                    ReplyLog.event { "voice: dropped, the instruction is gone" }
                    end(current)
                    return
                }
                val before = into.field.text
                val cursor = into.field.cursor
                // Never cut: an instruction over the limit stays whole and says so.
                DictationInsertion.insert(into.field, text, limit = null)
                review = Review(into.field, before, cursor, into.field.text)
                moveTo(current, DictationPhase.REVIEWING)
                onInstructionChanged(into.field)
                onEvent(DictationEvent.Inserted)
                val limit = into.limit
                if (limit != null && into.field.text.codePointLength() > limit) show(DictationMessage.TooLong(limit))
            }
            is DictationTarget.Host -> {
                if (shown && into.field.isLive) {
                    if (into.field.insert(text)) onEvent(DictationEvent.Inserted)
                    end(current)
                } else {
                    // The keyboard left meanwhile: the words wait for an explicit Insert.
                    pendingText = text
                    moveTo(current, DictationPhase.REVIEWING)
                }
            }
        }
    }

    // --------------------------------------------------------------- helpers

    /**
     * The session's one job: finishing the file, the request, the retry. The
     * job is stored BEFORE it starts - on Main.immediate it may run to its
     * first suspension inside start() - so Cancel always finds it.
     */
    private fun run(current: Session, block: suspend CoroutineScope.() -> Unit) {
        current.job?.cancel()
        val job = scope.launch(start = CoroutineStart.LAZY) {
            if (session === current) block()
        }
        current.job = job
        job.start()
    }

    /** A review settles: the words are the user's, Undo goes, the session closes. */
    private fun settleReview() {
        if (phase != DictationPhase.REVIEWING || target !is DictationTarget.Instruction) return
        review = null
        session?.let { end(it) }
    }

    private fun moveTo(current: Session, next: DictationPhase) {
        current.phase = next
        if (session === current) phase = next
    }

    /** Closes [current]: microphone, request and audio all go. */
    private fun end(current: Session) {
        current.phase = DictationPhase.CANCELED
        current.job?.cancel()
        current.job = null
        current.recorder.discard()
        current.audio = null
        if (session === current) {
            session = null
            phase = DictationPhase.IDLE
            target = null
            capturedMs = 0L
            level = 0f
            limitReached = false
            pendingText = null
        }
    }

    private fun show(next: DictationMessage) {
        message = next
        onEvent(DictationEvent.Message(next))
    }

    private fun messageFor(failure: RecorderFailure): DictationMessage = when (failure) {
        RecorderFailure.PERMISSION -> DictationMessage.PermissionRequired
        else -> DictationMessage.Microphone(failure)
    }

    private fun targetIsLive(target: DictationTarget): Boolean = when (target) {
        is DictationTarget.Instruction -> isLive(target.field)
        is DictationTarget.Host -> target.field.isLive
    }

    // ------------------------------------------------------------ permission

    private fun askForMicrophone(target: DictationTarget, languages: List<String>) {
        // One dialog per tap: a second tap while it is opening asks nothing.
        val now = clock()
        if (permissionJob?.isActive == true && now - askedAt < PERMISSION_DEBOUNCE_MS) return
        askedAt = now
        permissionJob?.cancel()
        pending = null
        message = null
        permissionJob = scope.launch {
            when (mic.request()) {
                MicPermission.Outcome.GRANTED -> {
                    ReplyLog.event { "voice: microphone allowed" }
                    pending = Pending(target, languages, clock() + PENDING_START_MS)
                    startPending()
                }
                MicPermission.Outcome.DENIED, null -> show(DictationMessage.PermissionRequired)
                MicPermission.Outcome.PERMANENTLY_DENIED -> show(DictationMessage.PermissionDenied)
            }
        }
    }

    private fun startPending() {
        val waiting = pending ?: return
        if (!shown) return
        pending = null
        if (clock() > waiting.until || session != null || !targetIsLive(waiting.target) || !mic.isGranted()) return
        begin(waiting.target, waiting.languages)
    }

    companion object {
        /** The most audio one recording holds. */
        const val MAX_RECORDING_MS = 40_000L

        /** Less than this is a tap, not a recording. */
        const val MIN_RECORDING_MS = 300L

        /** How soon after "Allow" the keyboard must be back for the recording to start by itself. */
        const val PENDING_START_MS = 4_000L

        /** A second mic tap this soon after asking does not ask again. */
        const val PERMISSION_DEBOUNCE_MS = 1_500L

        /** A recording the keyboard left behind is kept this long - about as long as the panels are. */
        const val HIDDEN_LIFETIME_MS = 10L * 60 * 1000

        /** Before the one automatic retry of a failed upload. */
        const val RETRY_DELAY_MS = 1_500L

        private val CONTROL_PHASES = setOf(
            DictationPhase.STARTING, DictationPhase.RECORDING, DictationPhase.PAUSED,
            DictationPhase.FINALIZING, DictationPhase.TRANSCRIBING, DictationPhase.RECOVERING,
            DictationPhase.ERROR
        )

        /** The hints can no longer change: the audio is on its way. */
        private val SENT_PHASES = setOf(
            DictationPhase.FINALIZING, DictationPhase.TRANSCRIBING, DictationPhase.RECOVERING,
            DictationPhase.REVIEWING, DictationPhase.ERROR
        )

        /** Worth one quiet retry: the connection, not the request, failed. */
        private val RETRIED = setOf(AIReplyError.Offline, AIReplyError.TimedOut, AIReplyError.ServiceUnavailable)
    }
}
