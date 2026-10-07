package kz.yerek.aireply

import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.runCurrent
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kz.yerek.aireply.ai.AIConfiguration
import kz.yerek.aireply.ai.AIFeatures
import kz.yerek.aireply.ai.AIReplyService
import kz.yerek.aireply.ai.AccountReplyTransport
import kz.yerek.aireply.ai.GeneratedReply
import kz.yerek.aireply.ai.ReplyDraftNormalizer
import kz.yerek.aireply.ai.ReplyPromptBuilder
import kz.yerek.aireply.ai.ReplyTransport
import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.domain.model.ReplyConfiguration
import kz.yerek.aireply.keyboard.reply.ReplySessionController
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Test

/**
 * The customer's rule: the COPIED message decides the reply's language - a
 * message copied in Kazakh is answered in Kazakh whatever the phone, the app
 * or the keyboard layout is set to, and whatever language the instruction or a
 * quick intent is written in. The keyboard therefore sends the copied text
 * exactly as copied, never a language rule of its own, and the layout only as
 * `input_language`, the server's last hint after the message itself.
 *
 * Жауап тілін көшірілген хабарлама шешеді: қазақша хабарламаға - қазақша жауап.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class ImeReplyLanguageTest {

    private val kazakhMessage = "Сәлеметсіз бе! Ертең кездесуге уақытыңыз бар ма?"

    private val dispatcher = StandardTestDispatcher()
    private val scope = TestScope(dispatcher)
    private var sent: AccountReplyTransport.RequestContext? = null

    private val service = AIReplyService(
        configuration = AIConfiguration { true },
        nameTemplate = { template, _ -> template.id },
        accountTransport = { _, context ->
            sent = context
            object : ReplyTransport {
                override suspend fun generate(prompt: ReplyPromptBuilder.Prompt) = GeneratedReply("Иә, ертең уақытым бар.")
            }
        }
    )

    /** The keyboard in Russian, on the Russian layout, answering a message copied in Kazakh. */
    private fun replyFromRussianKeyboard(instruction: String): JsonObject {
        val controller = ReplySessionController(scope, service, ReplyDraftNormalizer()).apply {
            configuration = ReplyConfiguration.INITIAL
            uiLanguage = AppLanguage.RUSSIAN
            inputLanguage = { KeyboardLanguage.RUSSIAN }
        }
        controller.open(ReplyConfiguration.INITIAL.visibleTemplates.first(), kazakhMessage, from = null, error = null)
        controller.session!!.instruction.set(instruction)
        controller.generate()
        scope.runCurrent()

        val context = checkNotNull(sent) { "the request went out" }
        val everything = AIFeatures(replyPreferences = true, senderProfile = true, instructionPolish = true)
        return Json.parseToJsonElement(AccountReplyTransport.encode(context, everything)).jsonObject
    }

    @Test
    fun `a message copied in Kazakh is sent exactly as copied`() {
        val body = replyFromRussianKeyboard(instruction = "")
        assertEquals(kazakhMessage, body.getValue("source_text").jsonPrimitive.content)
        assertFalse("no instruction of the keyboard's own", "instruction" in body.keys)
    }

    @Test
    fun `the layout is only a hint, and nothing else names a reply language`() {
        val body = replyFromRussianKeyboard(instruction = "Ответь согласием.")
        assertEquals("the layout, for the server's last resort", "ru", body.getValue("input_language").jsonPrimitive.content)
        assertEquals("the app's language, for naming only", "ru", body.getValue("language").jsonPrimitive.content)
        assertEquals(
            "a Russian quick intent travels as typed, with no language rule appended",
            "Ответь согласием.",
            body.getValue("instruction").jsonPrimitive.content
        )
        assertFalse(body.keys.any { it.contains("reply_language") || it == "target_language" })
    }

    @Test
    fun `an older server gets no layout at all`() {
        val controller = ReplySessionController(scope, service, ReplyDraftNormalizer()).apply {
            configuration = ReplyConfiguration.INITIAL
            uiLanguage = AppLanguage.RUSSIAN
            inputLanguage = { KeyboardLanguage.RUSSIAN }
        }
        controller.open(ReplyConfiguration.INITIAL.visibleTemplates.first(), kazakhMessage, from = null, error = null)
        controller.generate()
        scope.runCurrent()
        val body = Json.parseToJsonElement(AccountReplyTransport.encode(checkNotNull(sent), AIFeatures.NONE)).jsonObject
        assertFalse("input_language" in body.keys)
        assertEquals(kazakhMessage, body.getValue("source_text").jsonPrimitive.content)
    }
}
