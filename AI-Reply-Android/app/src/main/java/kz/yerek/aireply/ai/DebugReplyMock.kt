package kz.yerek.aireply.ai

import kotlinx.coroutines.delay
import java.util.concurrent.atomic.AtomicInteger

/**
 * DEBUG BUILDS ONLY: canned replies, so the reply flow can be exercised on an
 * emulator without an account, a network or the user's quota. Wired in by
 * [kz.yerek.aireply.ServiceLocator] only when `BuildConfig.DEBUG` is true AND
 * Settings ▸ Developer ▸ Mock replies is on.
 *
 * Replies rotate between Kazakh, Russian and English, with an emoji and a
 * line break, so every Regenerate produces a visibly new version and the
 * insertion path gets Unicode and multi-line text. Instruction tags simulate
 * failures: `#offline`, `#quota`, `#slow` (six seconds, to try Stop).
 */
class DebugReplyMock(private val instruction: String) : ReplyTransport {

    override suspend fun generate(prompt: ReplyPromptBuilder.Prompt): GeneratedReply {
        val tags = instruction.lowercase()
        delay(if ("#slow" in tags) 6_000L else 700L)
        if ("#offline" in tags) AIReplyError.Offline.raise()
        if ("#quota" in tags) AIReplyError.QuotaExhausted.raise()
        val index = served.getAndIncrement() % REPLIES.size
        return GeneratedReply(text = REPLIES[index])
    }

    private companion object {
        val served = AtomicInteger(0)

        val REPLIES = listOf(
            "Сәлеметсіз бе! Иә, ертең сағат 15:00-ге дейін жеткізе аламыз 🚚",
            "Здравствуйте! Да, доставим завтра до 15:00.\nПодскажите, пожалуйста, адрес 🙂",
            "Hi! Yes, we can deliver tomorrow before 3 pm 👍"
        )
    }
}
