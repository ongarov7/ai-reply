package kz.yerek.aireply.ai

import kotlinx.coroutines.delay
import java.util.concurrent.atomic.AtomicInteger

/**
 * DEBUG BUILDS ONLY: canned messages for Create, so the flow can be tried on an
 * emulator without an account. Wired in next to [DebugReplyMock], under the
 * same developer switch. Instruction tags: `#offline`, `#quota`, `#slow`.
 */
class DebugComposeMock : ComposeTransport {

    override suspend fun compose(request: ComposeService.Request): GeneratedReply {
        val tags = request.instruction.lowercase()
        delay(if ("#slow" in tags) 6_000L else 700L)
        if ("#offline" in tags) AIReplyError.Offline.raise()
        if ("#quota" in tags) AIReplyError.QuotaExhausted.raise()
        val index = served.getAndIncrement() % MESSAGES.size
        return GeneratedReply(text = MESSAGES[index])
    }

    private companion object {
        val served = AtomicInteger(0)

        val MESSAGES = listOf(
            "Уважаемый Сакен Бакпакбекович!\n\nОт всей души поздравляем Вас с 55-летним юбилеем! 🎉 " +
                "Желаем крепкого здоровья, благополучия и новых профессиональных достижений.\n\nС юбилеем! 🎂👏",
            "Құрметті Күлжан апай!\n\nТуған күніңізбен шын жүректен құттықтаймыз! 🌷 " +
                "Зор денсаулық, отбасыңызға амандық пен береке тілейміз.",
            "Hi team! Starting tomorrow the office opens at 10:00. Thanks, and see you then 👋"
        )
    }
}
