package kz.yerek.aireply.keyboard.voice

import androidx.annotation.StringRes
import kz.yerek.aireply.R
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.voice.RecognitionLanguage
import kz.yerek.aireply.voice.VoiceFailure
import kz.yerek.aireply.voice.VoiceState

/** A status-line sentence: a string resource and, for the limit, its number. */
data class VoiceMessage(@StringRes val id: Int, val argument: Int? = null)

/**
 * What the dictation status line says, as a value - the same for the reply
 * composer and Create, and for the screen reader.
 */
object VoiceStatusText {

    /** The sentence for [voice], or for [notice] once the microphone is idle; null for nothing. */
    fun message(voice: VoiceState, notice: DictationNotice?): VoiceMessage? = when (voice) {
        VoiceState.Starting, is VoiceState.Listening -> VoiceMessage(R.string.voice_kb_listening)
        VoiceState.Processing -> VoiceMessage(R.string.voice_kb_processing)
        VoiceState.PermissionRequired -> VoiceMessage(R.string.voice_kb_permission_needed)
        VoiceState.PermissionDenied -> VoiceMessage(R.string.voice_kb_permission_denied)
        is VoiceState.Failed -> VoiceMessage(failure(voice))
        VoiceState.Idle, is VoiceState.Done -> notice?.let { VoiceMessage(R.string.kb_compose_err_too_long, it.limit) }
    }

    @StringRes
    private fun failure(failed: VoiceState.Failed): Int = when (failed.reason) {
        VoiceFailure.NO_SPEECH -> R.string.voice_kb_no_speech
        VoiceFailure.NETWORK -> R.string.kb_err_offline
        VoiceFailure.LANGUAGE_UNAVAILABLE -> when (RecognitionLanguage.layoutFor(failed.languageTag)) {
            KeyboardLanguage.KAZAKH -> R.string.voice_kb_language_unavailable_kk
            KeyboardLanguage.RUSSIAN -> R.string.voice_kb_language_unavailable_ru
            KeyboardLanguage.ENGLISH -> R.string.voice_kb_language_unavailable_en
            null -> R.string.voice_kb_unavailable
        }
        VoiceFailure.UNAVAILABLE -> R.string.voice_kb_unavailable
        VoiceFailure.BUSY -> R.string.voice_kb_busy
        VoiceFailure.GENERIC -> R.string.voice_kb_failed
    }

    /**
     * The end of a running transcript that fits the line: the newest words
     * matter most while the user speaks. Cuts only between words, and marks
     * the cut with "…". [fits] measures a candidate.
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
