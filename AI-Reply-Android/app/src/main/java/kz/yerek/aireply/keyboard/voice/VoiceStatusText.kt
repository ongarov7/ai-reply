package kz.yerek.aireply.keyboard.voice

import androidx.annotation.StringRes
import kz.yerek.aireply.R
import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.voice.RecorderFailure
import java.util.Locale

/** A sentence about the recording: a string resource and its number, or one of the AI errors. */
sealed interface VoiceMessage {
    data class Text(@StringRes val id: Int, val argument: Int? = null) : VoiceMessage

    /** Localised by the same table as every other AI error. */
    data class Error(val error: AIReplyError) : VoiceMessage
}

/**
 * What the recording says, as values - the same for the app's field and the
 * two AI panels, and for the screen reader. The words recognised are never
 * part of it.
 *
 * Жазу күйінің мәтіні: панельдер мен экрандық оқу құралы үшін бірдей.
 */
object VoiceStatusText {

    /** The time left is counted down out loud from here. */
    const val COUNTDOWN_MS = 10_000L

    /** From here the countdown turns urgent. */
    const val URGENT_MS = 5_000L

    /** How long "Up to 40 seconds" stays after the microphone opens. */
    const val LIMIT_HINT_MS = 3_000L

    /** The sentence for [message]. */
    fun message(message: DictationMessage): VoiceMessage = when (message) {
        DictationMessage.PermissionRequired -> VoiceMessage.Text(R.string.voice_kb_permission_needed)
        DictationMessage.PermissionDenied -> VoiceMessage.Text(R.string.voice_kb_permission_denied)
        is DictationMessage.Microphone -> VoiceMessage.Text(
            when (message.failure) {
                RecorderFailure.PERMISSION -> R.string.voice_kb_permission_needed
                RecorderFailure.INTERRUPTED -> R.string.voice_rec_interrupted
                RecorderFailure.UNAVAILABLE -> R.string.voice_rec_mic_unavailable
            }
        )
        DictationMessage.TooShort -> VoiceMessage.Text(R.string.voice_rec_too_short)
        DictationMessage.NothingHeard -> VoiceMessage.Text(R.string.voice_kb_no_speech)
        is DictationMessage.Failed -> VoiceMessage.Error(message.error)
        is DictationMessage.TooLong -> VoiceMessage.Text(R.string.kb_compose_err_too_long, message.limit)
    }

    /**
     * The status of a recording in [phase]: what the bar or the voice line
     * says. [message] wins wherever it explains the state (why it paused, why
     * it failed); null when there is nothing to say.
     */
    fun status(
        phase: DictationPhase,
        capturedMs: Long,
        maxMs: Long,
        limitReached: Boolean,
        message: DictationMessage?,
        forHost: Boolean
    ): VoiceMessage? {
        val left = maxMs - capturedMs
        return when (phase) {
            DictationPhase.STARTING -> VoiceMessage.Text(R.string.voice_rec_limit_hint, seconds(maxMs))
            DictationPhase.RECORDING -> when {
                left <= COUNTDOWN_MS -> VoiceMessage.Text(R.string.voice_rec_left, secondsLeft(left))
                capturedMs < LIMIT_HINT_MS -> VoiceMessage.Text(R.string.voice_rec_limit_hint, seconds(maxMs))
                else -> VoiceMessage.Text(R.string.voice_rec_recording)
            }
            DictationPhase.PAUSED -> when {
                message != null -> message(message)
                limitReached -> VoiceMessage.Text(R.string.voice_rec_limit_reached)
                else -> VoiceMessage.Text(R.string.voice_rec_paused)
            }
            DictationPhase.FINALIZING, DictationPhase.TRANSCRIBING -> VoiceMessage.Text(R.string.voice_rec_recognizing)
            DictationPhase.RECOVERING -> VoiceMessage.Text(R.string.voice_rec_retrying)
            DictationPhase.ERROR -> message?.let(::message) ?: VoiceMessage.Text(R.string.voice_kb_failed)
            DictationPhase.REVIEWING -> when {
                message != null -> message(message)
                forHost -> VoiceMessage.Text(R.string.voice_rec_ready)
                else -> VoiceMessage.Text(R.string.voice_rec_added)
            }
            DictationPhase.IDLE, DictationPhase.CANCELED -> message?.let(::message)
        }
    }

    /** "00:18 / 00:40": the audio recorded and the most a recording holds. */
    fun timer(capturedMs: Long, maxMs: Long): String = "${clock(capturedMs)} / ${clock(maxMs)}"

    /** Whole seconds recorded, for the screen reader. */
    fun seconds(ms: Long): Int = (ms.coerceAtLeast(0L) / 1000).toInt()

    /** Seconds left, rounded up: "1 s left" until the very end, never "0 s left" while recording. */
    fun secondsLeft(leftMs: Long): Int = ((leftMs.coerceAtLeast(0L) + 999) / 1000).toInt()

    private fun clock(ms: Long): String {
        val total = seconds(ms)
        return String.format(Locale.ROOT, "%02d:%02d", total / 60, total % 60)
    }

    /**
     * The end of a text that fits a line: the newest words matter most. Cuts
     * only between words, and marks the cut with "…". [fits] measures a candidate.
     */
    fun tail(text: String, fits: (String) -> Boolean): String {
        if (fits(text)) return text
        val starts = text.indices.filter { it > 0 && text[it - 1] == ' ' && text[it] != ' ' }
        var low = 0
        var high = starts.size - 1
        var best = -1
        while (low <= high) {
            val middle = (low + high) / 2
            if (fits(ELLIPSIS + text.substring(starts[middle]))) {
                best = middle
                high = middle - 1
            } else {
                low = middle + 1
            }
        }
        return if (best >= 0) ELLIPSIS + text.substring(starts[best]) else ELLIPSIS + text.substring(starts.lastOrNull() ?: 0)
    }

    private const val ELLIPSIS = "…"
}
