package kz.yerek.aireply

import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kz.yerek.aireply.ai.AIConfiguration
import kz.yerek.aireply.ai.AIFeatures
import kz.yerek.aireply.ai.AILimits
import kz.yerek.aireply.ai.AIReplyService
import kz.yerek.aireply.ai.AccountReplyTransport
import kz.yerek.aireply.ai.GeneratedReply
import kz.yerek.aireply.ai.ReplyPromptBuilder
import kz.yerek.aireply.ai.ReplyTransport
import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.domain.model.ReplyConfiguration
import kz.yerek.aireply.domain.model.ReplyTemplate
import kz.yerek.aireply.ui.feature.compose.tryReplyRequest
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Before
import org.junit.Test

/**
 * The reply's language follows the copied message, never the phone's or the
 * app's language: "Try a reply" sends the message as it is and names no reply
 * language, exactly like the keyboard.
 *
 * Қазақша хабарламаға қазақша жауап: интерфейс тілі жауап тілін таңдамайды.
 */
class ReplyLanguageTest {

    private val everything = AIFeatures(
        replyPreferences = true,
        senderProfile = true,
        instructionPolish = true,
        productEvents = true
    )

    @Before
    fun startFresh() {
        AILimits.install(InMemoryPreferences())
    }

    @After
    fun forgetServerAnswers() {
        AILimits.install(InMemoryPreferences())
    }

    /** The body "Try a reply" puts on the wire for [message] in an app shown in [ui]. */
    private fun sent(message: String, ui: AppLanguage, instruction: String = ""): JsonObject = runBlocking {
        var context: AccountReplyTransport.RequestContext? = null
        val service = AIReplyService(
            configuration = AIConfiguration { true },
            nameTemplate = { template, _ -> template.id },
            accountTransport = { _, requestContext ->
                context = requestContext
                object : ReplyTransport {
                    override suspend fun generate(prompt: ReplyPromptBuilder.Prompt) = GeneratedReply("ok")
                }
            }
        )
        val template = ReplyTemplate.defaults.first()
        service.generate(tryReplyRequest(message, template, ReplyConfiguration(), ui, instruction))
        Json.parseToJsonElement(AccountReplyTransport.encode(requireNotNull(context), everything)).jsonObject
    }

    @Test
    fun `a Kazakh message goes unchanged whatever the app language`() {
        AppLanguage.entries.forEach { ui ->
            val body = sent("  Сәлем! Ертең кездесуге уақытың бар ма?\n", ui)
            assertEquals(ui.name, "Сәлем! Ертең кездесуге уақытың бар ма?", body["source_text"]!!.jsonPrimitive.content)
            assertFalse("${ui.name}: no instruction was typed", "instruction" in body.keys)
            assertFalse("${ui.name}: the app has no keyboard layout to hint with", "input_language" in body.keys)
            assertFalse("${ui.name}: Android has no reply-language preference", "reply_language" in body.toString())
            assertEquals("${ui.name}: the app language is only a hint", ui.code, body["language"]!!.jsonPrimitive.content)
        }
    }

    @Test
    fun `the instruction goes as typed, with no language rule added`() {
        val body = sent("Привет! Давно не общались. Как ты?", AppLanguage.ENGLISH, instruction = " ответь коротко ")
        assertEquals("Привет! Давно не общались. Как ты?", body["source_text"]!!.jsonPrimitive.content)
        assertEquals("ответь коротко", body["instruction"]!!.jsonPrimitive.content)
    }
}
