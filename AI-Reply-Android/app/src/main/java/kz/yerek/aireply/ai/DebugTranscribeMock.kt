package kz.yerek.aireply.ai

import kotlinx.coroutines.delay
import kz.yerek.aireply.voice.TranscriptionRequest

/**
 * DEBUG BUILDS ONLY: a stand-in for `/api/v1/ai/transcribe`, so voice input
 * can be tried on an emulator without an account or the daily voice cap.
 * Wired in next to [DebugReplyMock], under the same developer switch.
 *
 * The audio is never read. After a pause about as long as a real answer
 * (cancellable, to try Stop), it returns a fixed sentence in the language of
 * the first hint: Kazakh, Russian or English; Russian for anything else.
 */
class DebugTranscribeMock : TranscriptionTransport {

    override suspend fun transcribe(request: TranscriptionRequest): Transcript {
        delay(DELAY_MS)
        val language = when (val hint = request.languages.firstOrNull()?.trim()?.lowercase()) {
            "kk", "en" -> hint
            else -> "ru"
        }
        return Transcript(
            text = TRANSCRIPTS.getValue(language),
            languages = listOf(language),
            durationMs = request.audio.durationMs
        )
    }

    companion object {
        const val DELAY_MS = 1_200L

        private val TRANSCRIPTS = mapOf(
            "kk" to "Сәлеметсіз бе! Бүгін кездесуге келе алмаймын, кешіріңіз.",
            "ru" to "Ответь клиенту вежливо и коротко, что заказ будет готов завтра.",
            "en" to "Reply politely that I cannot attend the meeting today."
        )
    }
}
