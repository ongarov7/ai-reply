package kz.yerek.aireply

import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kz.yerek.aireply.ai.AIConfiguration
import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.ai.AIReplyService
import kz.yerek.aireply.ai.ComposeService
import kz.yerek.aireply.ai.ComposeTransport
import kz.yerek.aireply.ai.GeneratedReply
import kz.yerek.aireply.ai.ReplyDraftNormalizer
import kz.yerek.aireply.ai.ReplyPromptBuilder
import kz.yerek.aireply.ai.ReplyTransport
import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.domain.model.ReplyConfiguration
import kz.yerek.aireply.keyboard.reply.ComposeSessionController
import kz.yerek.aireply.keyboard.reply.ReplyComposerFlow
import kz.yerek.aireply.keyboard.reply.ReplySessionController
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/**
 * Try again after a request that failed at once - signed out, or no consent -
 * on the keyboard's Main.immediate, where such a failure finishes inside
 * `launch` itself. Try again must start a new request every time.
 *
 * Бірден құлаған сұраныстан кейін «Қайталау» жұмыс істеуі керек.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class SessionRetryTest {

    // Runs a coroutine until its first suspension inside launch, like Main.immediate.
    private val scope = TestScope(UnconfinedTestDispatcher())
    private var signedIn = false

    @Test
    fun `Try again on a reply starts a new request after an immediate failure`() {
        var attempts = 0
        val service = AIReplyService(
            configuration = AIConfiguration { signedIn },
            nameTemplate = { template, _ -> template.id },
            accountTransport = { _, _ -> error("not reached in this test") },
            // Counts each attempt, then lets the real readiness check refuse it
            // until the user is signed in.
            transportOverride = { _, _ ->
                attempts++
                if (!signedIn) {
                    null
                } else {
                    object : ReplyTransport {
                        override suspend fun generate(prompt: ReplyPromptBuilder.Prompt) = GeneratedReply("Готово")
                    }
                }
            }
        )
        val controller = ReplySessionController(scope, service, ReplyDraftNormalizer()).apply {
            configuration = ReplyConfiguration.INITIAL
            uiLanguage = AppLanguage.RUSSIAN
        }
        controller.open(ReplyConfiguration.INITIAL.visibleTemplates.first(), "Когда встречаемся?", from = null, error = null)

        controller.generate()
        assertEquals(1, attempts)
        assertEquals(AIReplyError.AuthenticationFailed, controller.flow.error)

        controller.generate()
        assertEquals("Try again is not dead", 2, attempts)
        assertEquals(AIReplyError.AuthenticationFailed, controller.flow.error)

        signedIn = true
        controller.generate()
        assertEquals(3, attempts)
        assertEquals(ReplyComposerFlow.Stage.Result, controller.flow.stage)
        assertNull(controller.flow.error)
        assertEquals("Готово", controller.session!!.draft.text)
    }

    @Test
    fun `Try again in Create starts a new request after an immediate failure`() {
        var attempts = 0
        val service = ComposeService(
            configuration = AIConfiguration { signedIn },
            accountTransport = { error("not reached in this test") },
            transportOverride = {
                attempts++
                if (!signedIn) null else ComposeTransport { GeneratedReply("Поздравляю!") }
            }
        )
        val controller = ComposeSessionController(scope, service, ReplyDraftNormalizer()).apply {
            uiLanguage = AppLanguage.RUSSIAN
        }
        controller.open()
        controller.session!!.instruction.set("Поздравь коллегу")
        controller.instructionEdited()

        controller.generate()
        assertEquals(1, attempts)
        assertEquals(AIReplyError.AuthenticationFailed, controller.flow.error)

        controller.generate()
        assertEquals("Try again is not dead", 2, attempts)

        signedIn = true
        controller.generate()
        assertEquals(3, attempts)
        assertEquals(ReplyComposerFlow.Stage.Result, controller.flow.stage)
        assertEquals("Поздравляю!", controller.session!!.draft.text)
    }
}
