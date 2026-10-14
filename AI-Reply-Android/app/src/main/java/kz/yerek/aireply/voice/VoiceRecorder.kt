package kz.yerek.aireply.voice

import java.io.File

/**
 * A finished recording: ONE valid audio file of everything captured, pauses
 * left out - never several containers glued together.
 */
data class RecordedAudio(
    val file: File,
    /** `audio/mp4` (AAC in an M4A container), or `audio/wav` when no AAC encoder worked. */
    val mimeType: String,
    /** Audio in the file, pauses excluded. */
    val durationMs: Long
)

/** Why the microphone stopped or could not start. */
enum class RecorderFailure {
    /** RECORD_AUDIO is not granted, or was revoked while recording. */
    PERMISSION,

    /** A call or another app has the microphone, or Android silenced this one. */
    INTERRUPTED,

    /** The microphone opened but delivers nothing, or will not open at all. */
    UNAVAILABLE
}

/**
 * The microphone, recording on the phone: nothing leaves the device until the
 * finished file is handed to a [VoiceTranscriber].
 *
 * TIME IS AUDIO. How long the recording is comes from the audio actually
 * captured (samples), not from a timer: a pause holds no audio, a slow start
 * holds none either, and the limit can never be passed by a late callback.
 * Capture stops by itself once [start]'s `maxMs` of audio are held.
 *
 * THREADING. Every method is called on the main thread and every [Listener]
 * callback arrives there. Reading the microphone and encoding the file happen
 * on background threads.
 *
 * One recorder serves one recording; the keyboard makes a new one for the next.
 */
interface VoiceRecorder {

    interface Listener {
        /** The first audio arrived: the microphone is really capturing. */
        fun onCapturing()

        /**
         * [capturedMs] of audio are held, pauses excluded. [level] is the
         * loudness of the latest audio, 0 (silence) to 1 - measured, never made up.
         */
        fun onProgress(capturedMs: Long, level: Float)

        /** `maxMs` of audio are held: capture stopped by itself and the microphone is closed. */
        fun onLimitReached()

        /**
         * Capture failed and stopped; the microphone is closed. What was held
         * before stays, so [finish] still works.
         */
        fun onFailure(reason: RecorderFailure)
    }

    /** Audio held so far, pauses excluded. */
    val capturedMs: Long

    /**
     * Opens the microphone and captures until `maxMs` of audio are held.
     * Null when it started; the reason otherwise, and nothing is held.
     */
    fun start(maxMs: Long, listener: Listener): RecorderFailure?

    /** Stops capturing and closes the microphone; the audio stays. Idempotent. */
    fun pause()

    /** Opens the microphone again and keeps adding audio. Null when it resumed. */
    fun resume(): RecorderFailure?

    /**
     * Stops capturing and writes everything held into one file. Null when
     * nothing usable was held. The file stays until [discard].
     */
    suspend fun finish(): RecordedAudio?

    /** Stops capturing, forgets the audio and deletes any file written. Idempotent. */
    fun discard()
}
