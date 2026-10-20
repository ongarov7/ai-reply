package kz.yerek.aireply

import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.keyboard.KeyboardAppLink
import kz.yerek.aireply.keyboard.reply.ReplyComposerFlow
import kz.yerek.aireply.keyboard.ui.isLimit
import kz.yerek.aireply.keyboard.ui.offersRetry
import kz.yerek.aireply.push.AppLink
import kz.yerek.aireply.push.AppLinks
import kz.yerek.aireply.push.PushPayload
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File
import javax.xml.parsers.DocumentBuilderFactory

/**
 * Spent replies in the keyboard (contract §3.9): «Лимит ответов исчерпан»,
 * the way to more replies through the app, and no Try again.
 *
 * Жауаптар бітті: қосымшаны ашу батырмасы бар, «Қайталау» жоқ.
 */
class LimitNoticeTest {

    @Test
    fun `a spent daily or monthly allowance is the limit, and is not retried`() {
        for (error in listOf(AIReplyError.QuotaExhausted, AIReplyError.MonthlyQuotaExhausted)) {
            assertTrue("$error", error.isLimit)
            assertFalse("$error", error.offersRetry)
        }
        assertFalse(AIReplyError.RateLimited.isLimit)
        assertTrue("waiting a moment does fix this one", AIReplyError.RateLimited.offersRetry)
    }

    @Test
    fun `a request refused for the limit keeps the instruction and the reply`() {
        val composing = ReplyComposerFlow().startingGeneration()!!.failing(AIReplyError.QuotaExhausted)
        assertEquals(ReplyComposerFlow.Stage.Composing, composing.stage)
        assertEquals(AIReplyError.QuotaExhausted, composing.error)

        val withReply = ReplyComposerFlow().startingGeneration()!!.receiving("Привет!")
            .startingGeneration()!!.failing(AIReplyError.MonthlyQuotaExhausted)
        assertEquals("the reply on screen can still be inserted", ReplyComposerFlow.Stage.Result, withReply.stage)
        assertEquals("Привет!", withReply.draftText)
    }

    @Test
    fun `Open AI Reply goes through the push link path to the purchase screen`() {
        val extras = KeyboardAppLink.extras(KeyboardAppLink.PURCHASE)
        assertEquals("aireply://purchase", extras[PushPayload.KEY_LINK])
        // MainActivity handles intents that carry a notification id.
        val payload = PushPayload.from(extras)
        assertNotNull(payload)
        assertTrue("a screen of the app, Home until the app knows purchase", payload!!.link is AppLink.Screen)
        assertEquals(AppLinks.parse("aireply://purchase"), payload.link)
        assertNull("not a notification: nothing is reported as opened", payload.openedRequest("installation-1"))
    }

    @Test
    fun `the limit copy is in every language and no longer promises tomorrow`() {
        val tomorrow = mapOf(
            "values" to "tomorrow", "values-ru" to "завтра", "values-kk" to "ертең", "values-uz" to "ertaga"
        )
        for ((folder, word) in tomorrow) {
            val strings = strings(folder)
            listOf("kb_limit_title", "kb_limit_body", "kb_limit_open").forEach { key ->
                assertTrue("$folder $key", strings[key].orEmpty().isNotBlank())
            }
            assertTrue("$folder", strings.getValue("kb_limit_body").contains("AI Reply"))
            assertFalse("$folder", strings.getValue("kb_err_quota_exhausted").lowercase().contains(word))
        }
        assertEquals("Лимит ответов исчерпан", strings("values-ru")["kb_limit_title"])
        assertEquals("Откройте AI Reply, чтобы докупить ответы или выбрать тариф.", strings("values-ru")["kb_limit_body"])
        assertEquals("Открыть AI Reply", strings("values-ru")["kb_limit_open"])
    }

    private fun strings(folder: String): Map<String, String> {
        val result = HashMap<String, String>()
        listOf("strings.xml", "strings_android.xml").forEach { name ->
            val file = File("src/main/res/$folder/$name")
            if (!file.exists()) return@forEach
            val nodes = DocumentBuilderFactory.newInstance().newDocumentBuilder().parse(file).getElementsByTagName("string")
            for (index in 0 until nodes.length) {
                val element = nodes.item(index) as org.w3c.dom.Element
                // Android's   escape, as the resource compiler reads it.
                result[element.getAttribute("name")] = element.textContent.replace("\\u00A0", " ")
            }
        }
        return result
    }
}
