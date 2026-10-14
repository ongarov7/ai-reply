package kz.yerek.aireply.ai

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.ensureActive
import kz.yerek.aireply.platform.ReplyLog
import kz.yerek.aireply.voice.TranscriptionRequest
import kz.yerek.aireply.voice.VoiceTranscriber
import kotlin.coroutines.coroutineContext

/**
 * Voice input through the server: the keyboard records on the phone, the
 * finished file goes to `POST /api/v1/ai/transcribe`, and the words come back
 * for the user to read and edit.
 *
 * Дауыс серверде мәтінге айналады: мәтін тек көрсетіледі, жауап өздігінен жасалмайды.
 *
 *  * Only the recording and a few short fields are sent: never the copied
 *    message, never a draft, never the profile.
 *  * The text is what was said, in the language it was said in. Nothing here
 *    translates it or rewrites it, and nothing generates a reply from it: that
 *    takes an explicit tap elsewhere.
 *  * It fails with the same closed set of [AIReplyError]s as replies, plus
 *    [AIReplyError.VoiceLimitReached] for the daily voice cap.
 *  * Logs carry lengths, durations and outcomes only - never the words.
 */
class VoiceTranscriptionService(
    private val configuration: AIConfiguration,
    /** Builds the authenticated transport for the backend at the given URL. */
    private val accountTransport: (String) -> TranscriptionTransport,
    /** Replaces the account transport, or returns null to use it. Tests and DEBUG mocks. */
    private val transportOverride: (() -> TranscriptionTransport?)? = null
) : VoiceTranscriber {

    /** The server announced the endpoint, or a DEBUG mock stands in for it. */
    override val isOffered: Boolean
        get() = AILimits.features.voiceTranscription || transportOverride?.invoke() != null

    /**
     * What [AIConfiguration.checkReady] would throw, returned instead: sign-in
     * first, then the consent. A missing consent is handed to the app here as
     * well, so the app the user is sent to opens on the consent screen. A
     * DEBUG mock needs neither.
     */
    override fun readiness(): AIReplyError? {
        if (transportOverride?.invoke() != null) return null
        val reason = configuration.notReadyReason ?: return null
        configuration.requestFailed(reason)
        return reason
    }

    override suspend fun transcribe(request: TranscriptionRequest): String {
        val override = transportOverride?.invoke()
        if (override == null) configuration.checkReady()
        val transport = override ?: accountTransport(configuration.backendBaseUrl)

        val started = System.nanoTime()
        val transcript = try {
            val result = transport.transcribe(request)
            coroutineContext.ensureActive()
            result
        } catch (cancellation: CancellationException) {
            throw cancellation
        } catch (exception: AIReplyException) {
            configuration.requestFailed(exception.error)
            ReplyLog.event {
                "transcribe failed: ${exception.error::class.simpleName}, " +
                    "${request.audio.durationMs} ms of audio, ${elapsedMs(started)} ms"
            }
            throw exception
        } catch (throwable: Throwable) {
            throw AIReplyException(ReplyNetworking.mapError(throwable))
        }

        val text = transcript.text.trim()
        ReplyLog.event {
            "transcribed ${request.audio.durationMs} ms of audio into ${text.length} chars " +
                "in ${elapsedMs(started)} ms"
        }
        return text
    }

    private fun elapsedMs(started: Long): Long = (System.nanoTime() - started) / 1_000_000
}

/** What the server heard. */
data class Transcript(
    /** As the server returned it; empty when nothing was said. */
    val text: String,
    /** ISO 639-1 codes the server detected, most likely first. Advisory; may be empty. */
    val languages: List<String> = emptyList(),
    /** The audio the server measured, when it said. */
    val durationMs: Long? = null
)

/** Where a transcript comes from: the backend, or a test double. */
fun interface TranscriptionTransport {
    suspend fun transcribe(request: TranscriptionRequest): Transcript
}
