package kz.yerek.aireply.voice

import kz.yerek.aireply.ai.AIReplyError

/** What the recording is for: the server words its transcription context accordingly. */
enum class TranscriptionPurpose(val wire: String) {
    /** Text for the app's own field: a message the user is writing. */
    DICTATION("dictation"),

    /** An instruction for an AI reply or for Create. */
    INSTRUCTION("instruction")
}

data class TranscriptionRequest(
    val audio: RecordedAudio,
    /**
     * The same id for every attempt with the same audio: a repeat the server
     * has already answered comes back from its cache instead of being
     * transcribed - and paid for - twice.
     */
    val requestId: String,
    /** ISO 639-1 hints, the most likely first: `kk`, `ru`, `en`, `uz`. */
    val languages: List<String>,
    val purpose: TranscriptionPurpose
)

/**
 * Turns a finished recording into text: the AI Reply server, which keeps the
 * OpenAI key, sends it to `gpt-transcribe`. The text is what was said, in the
 * language it was said in - never translated, never rewritten.
 */
interface VoiceTranscriber {

    /** The server offers transcription (`features.voice_transcription`), or a DEBUG mock stands in. */
    val isOffered: Boolean

    /** Null when a request can be made right now; otherwise what stands in the way (sign-in, consent). */
    fun readiness(): AIReplyError?

    /**
     * The words heard, trimmed; empty when nothing was said. Throws
     * [kz.yerek.aireply.ai.AIReplyException] on failure. Cancelling the
     * coroutine abandons the request.
     */
    suspend fun transcribe(request: TranscriptionRequest): String
}
