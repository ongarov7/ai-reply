package kz.yerek.aireply

import kz.yerek.aireply.ai.AILimits
import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.ai.AccountReplyTransport
import kz.yerek.aireply.data.account.ApiClient
import kz.yerek.aireply.data.account.ApiError
import org.junit.Assert.assertEquals
import org.junit.Test

/** The server publishes the character limits; the app follows, 400 until told. */
class AILimitsTest {

    @Test
    fun `the fallback is 400 characters`() {
        assertEquals(400, AILimits.FALLBACK.sourceCharacters)
        assertEquals(400, AILimits.FALLBACK.instructionCharacters)
    }

    @Test
    fun `published values replace the fallback`() {
        val limits = AILimits.published(source = 600, instruction = 300)
        assertEquals(600, limits.sourceCharacters)
        assertEquals(300, limits.instructionCharacters)
    }

    @Test
    fun `absent or absurd values keep what was known`() {
        val known = AILimits(sourceCharacters = 500, instructionCharacters = 350)
        assertEquals(known, AILimits.published(source = null, instruction = null, previous = known))
        assertEquals(known, AILimits.published(source = 5, instruction = 100_000, previous = known))
    }

    @Test
    fun `a length refusal carries the server's limit`() {
        val body = """{"error":{"code":"INVALID_REQUEST","message":"too long","details":{"field":"source_text","max_characters":450}}}"""
        assertEquals(ApiError.SourceTooLong(450), ApiClient.mapServerError(400, body))
        assertEquals(AIReplyError.MessageTooLong(450), AccountReplyTransport.map(ApiError.SourceTooLong(450)))
    }

    @Test
    fun `other invalid requests stay generic`() {
        val body = """{"error":{"code":"INVALID_REQUEST","details":{"field":"language"}}}"""
        assertEquals(ApiError.InvalidRequest, ApiClient.mapServerError(400, body))
    }
}
