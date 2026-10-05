package kz.yerek.aireply.keyboard.voice

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import kz.yerek.aireply.keyboard.input.KeyboardTextFieldState
import kz.yerek.aireply.platform.ReplyLog
import kz.yerek.aireply.voice.MicPermission
import kz.yerek.aireply.voice.SpeechRecognitionClient
import kz.yerek.aireply.voice.VoiceState

/** Where dictated words go: one of the panel's fields, and its character limit. */
class DictationTarget(val field: KeyboardTextFieldState, val limit: Int?)

/** The microphone permission, as the keyboard can see and ask for it. */
interface MicAccess {
    fun isGranted(): Boolean

    /** Shows the system dialog and waits for the answer; null when it could not be shown. */
    suspend fun request(): MicPermission.Outcome?

    /** The app's system settings, where a refused permission can be turned back on. */
    fun openSettings()
}

/** Dictation stopped at the field's limit: only what fitted went in. */
data class DictationNotice(val limit: Int)

/** What a screen reader should say about the microphone. */
sealed interface VoiceAnnouncement {
    /** The microphone opened. */
    data object Listening : VoiceAnnouncement

    /** The microphone closed: stopped, finished or cancelled. */
    data object Stopped : VoiceAnnouncement

    /** A sentence is now on the status line: a failure, the permission, the limit. */
    data class Message(val state: VoiceState, val notice: DictationNotice?) : VoiceAnnouncement
}

/**
 * The keyboard's dictation, shared by the reply composer and Create.
 *
 * WHAT IT GUARANTEES:
 *  * Words go into the field the recording was started for, at its caret,
 *    with clean spacing and never past its limit ([DictationInsertion]) - and
 *    only while that field is still on screen ([isLive]). Words for a panel
 *    that has gone are dropped, not kept somewhere.
 *  * Nothing is sent anywhere because of dictation. It fills a field; the
 *    user still taps Write or Reply.
 *  * One recogniser, created on first use and released whenever the keyboard
 *    goes away ([release]), so the microphone is never held across apps.
 *  * Every way out ends the recording: Stop and a layout switch keep what was
 *    heard ([stop]); closing, New, another persona discard it ([cancel]).
 *
 * The permission dialog hides the keyboard while it is up. A recording asked
 * for that way starts when the keyboard is back - if that happens soon and the
 * field is still there - so "tap the mic, allow" ends in the mic listening.
 */
class DictationController(
    private val scope: CoroutineScope,
    private val newClient: () -> SpeechRecognitionClient,
    private val mic: MicAccess,
    private val clock: () -> Long
) {
    /** What the status line and the mic button show. */
    var state: VoiceState by mutableStateOf(VoiceState.Idle)
        private set

    /** The limit was reached by the last dictation. Cleared by the next edit. */
    var notice: DictationNotice? by mutableStateOf(null)
        private set

    /** Whether [field] is still the one on screen to dictate into. */
    var isLive: (KeyboardTextFieldState) -> Boolean = { true }

    /** Words were put into [field]. */
    var onInserted: (KeyboardTextFieldState) -> Unit = {}

    var onAnnounce: (VoiceAnnouncement) -> Unit = {}

    /** Recording or finishing: Write and Reply wait for the words. */
    val isActive: Boolean get() = state.isActive

    private class Pending(val target: DictationTarget, val languageTag: String, val until: Long)

    private var client: SpeechRecognitionClient? = null
    private var observer: Job? = null
    private var permissionJob: Job? = null
    private var target: DictationTarget? = null
    private var pending: Pending? = null
    private var shown = false
    private var askedAt = Long.MIN_VALUE / 2

    // ------------------------------------------------------------ user taps

    /**
     * The microphone button: starts listening for [target] in [languageTag],
     * or - while listening - stops and keeps what was heard.
     */
    fun toggle(target: DictationTarget, languageTag: String) {
        when (state) {
            is VoiceState.Starting, is VoiceState.Listening -> {
                client?.stop()
                sync()
                return
            }
            // The words are on their way; one more tap must not lose them.
            is VoiceState.Processing -> return
            else -> Unit
        }
        notice = null
        when {
            mic.isGranted() -> begin(target, languageTag)
            // Refused for good: only Settings can change that.
            state is VoiceState.PermissionDenied -> mic.openSettings()
            else -> askForMicrophone(target, languageTag)
        }
    }

    /** Stop listening and keep what was heard: a layout switch, a rotation. */
    fun stop() {
        pending = null
        if (state.isActive) {
            client?.stop()
            sync()
        }
    }

    /** Stop listening and drop it: the panel closed, New, another persona. */
    fun cancel() {
        pending = null
        permissionJob?.cancel()
        permissionJob = null
        target = null
        notice = null
        client?.cancel()
        publish(VoiceState.Idle)
    }

    /** The layout changed: a recording stops (keeping its words); a message about the old layout goes. */
    fun layoutChanged() {
        if (state.isActive) stop() else clearMessage()
    }

    /** The user typed, tapped an intent or edited a field: old messages no longer apply. */
    fun userEdited() {
        clearMessage()
    }

    // ------------------------------------------------------------ lifecycle

    /** The keyboard is on screen: a recording the permission dialog interrupted may start now. */
    fun shown() {
        shown = true
        startPending()
    }

    /**
     * The keyboard went away: the microphone is released at once and anything
     * not yet delivered is dropped. A permission answer still on its way is
     * kept - the dialog itself is what hid the keyboard.
     */
    fun release() {
        shown = false
        target = null
        notice = null
        observer?.cancel()
        observer = null
        client?.release()
        client = null
        state = VoiceState.Idle
    }

    // ------------------------------------------------------------- internals

    private fun begin(target: DictationTarget, languageTag: String) {
        pending = null
        this.target = target
        val recognizer = client ?: newClient().also {
            client = it
            observe(it)
        }
        recognizer.reset()
        recognizer.start(languageTag)
        sync()
    }

    private fun askForMicrophone(target: DictationTarget, languageTag: String) {
        // One dialog per tap: a second tap while it is opening asks nothing.
        val now = clock()
        if (permissionJob?.isActive == true && now - askedAt < PERMISSION_DEBOUNCE_MS) return
        askedAt = now
        permissionJob?.cancel()
        pending = null
        permissionJob = scope.launch {
            when (mic.request()) {
                MicPermission.Outcome.GRANTED -> {
                    ReplyLog.event { "voice: microphone allowed" }
                    publish(VoiceState.Idle)
                    pending = Pending(target, languageTag, clock() + PENDING_START_MS)
                    startPending()
                }
                MicPermission.Outcome.DENIED -> publish(VoiceState.PermissionRequired)
                MicPermission.Outcome.PERMANENTLY_DENIED -> publish(VoiceState.PermissionDenied)
                null -> publish(VoiceState.PermissionRequired)
            }
        }
    }

    private fun startPending() {
        val waiting = pending ?: return
        if (!shown) return
        pending = null
        if (clock() > waiting.until || !isLive(waiting.target.field) || !mic.isGranted()) return
        begin(waiting.target, waiting.languageTag)
    }

    private fun observe(recognizer: SpeechRecognitionClient) {
        observer?.cancel()
        observer = scope.launch {
            recognizer.state.collect { consume(it) }
        }
    }

    /** Takes the recogniser's state now, rather than when the collector next runs. */
    private fun sync() {
        client?.let { consume(it.state.value) }
    }

    private fun consume(next: VoiceState) {
        when (next) {
            is VoiceState.Done -> {
                // Reset first: whichever of the collector and [sync] comes
                // second then sees Idle, and the words go in exactly once.
                client?.reset()
                publish(VoiceState.Idle)
                deliver(next.text)
            }
            // The recogniser idling says nothing about a permission answer.
            VoiceState.Idle -> if (state !is VoiceState.PermissionRequired && state !is VoiceState.PermissionDenied) {
                publish(VoiceState.Idle)
            }
            is VoiceState.Failed -> {
                target = null
                publish(next)
            }
            else -> publish(next)
        }
    }

    private fun deliver(text: String) {
        val into = target ?: return
        target = null
        // The panel it was meant for is gone: the words go too.
        if (!isLive(into.field)) {
            ReplyLog.event { "voice: dropped, the field is gone" }
            return
        }
        val result = DictationInsertion.insert(into.field, text, into.limit)
        ReplyLog.event { "voice: $result" }
        when (result) {
            DictationInsertion.Result.INSERTED -> onInserted(into.field)
            DictationInsertion.Result.TRIMMED -> {
                onInserted(into.field)
                showLimit(into.limit)
            }
            DictationInsertion.Result.NOTHING_FIT -> showLimit(into.limit)
            DictationInsertion.Result.EMPTY -> Unit
        }
    }

    private fun showLimit(limit: Int?) {
        if (limit == null) return
        val shownNotice = DictationNotice(limit)
        notice = shownNotice
        onAnnounce(VoiceAnnouncement.Message(state, shownNotice))
    }

    private fun clearMessage() {
        notice = null
        if (state is VoiceState.Failed || state is VoiceState.PermissionRequired || state is VoiceState.PermissionDenied) {
            client?.reset()
            state = VoiceState.Idle
        }
    }

    private fun publish(next: VoiceState) {
        val previous = state
        if (previous == next) return
        state = next
        when {
            !previous.isActive && next.isActive -> onAnnounce(VoiceAnnouncement.Listening)
            previous.isActive && (next is VoiceState.Idle || next is VoiceState.Done) -> onAnnounce(VoiceAnnouncement.Stopped)
        }
        when (next) {
            is VoiceState.Failed, VoiceState.PermissionRequired, VoiceState.PermissionDenied ->
                onAnnounce(VoiceAnnouncement.Message(next, null))
            else -> Unit
        }
    }

    companion object {
        /** How soon after "Allow" the keyboard must be back for the recording to start by itself. */
        const val PENDING_START_MS = 4_000L

        /** A second mic tap this soon after asking does not ask again. */
        const val PERMISSION_DEBOUNCE_MS = 1_500L
    }
}
