package kz.yerek.aireply.ai

import kotlinx.coroutines.delay

/**
 * DEBUG BUILDS ONLY: a stand-in for `/api/v1/ai/polish`, so the suggestion
 * can be tried on an emulator without an account. Wired in next to
 * [DebugComposeMock], under the same developer switch. It only tidies spacing,
 * the first capital and the final full stop. `#offline` in the text fails.
 */
class DebugPolishMock : PolishTransport {

    override suspend fun polish(request: PolishService.Request): String? {
        delay(400L)
        if ("#offline" in request.text.lowercase()) AIReplyError.Offline.raise()
        val tidy = request.text.trim().replace(SPACES, " ").replaceFirstChar { it.uppercase() }
        val polished = if (tidy.last() in ".!?…") tidy else "$tidy."
        return polished.takeIf { it != request.text }
    }

    private companion object {
        val SPACES = Regex("\\s{2,}")
    }
}
