package kz.yerek.aireply

import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.ai.AIReplyService
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/** The message-length rule, enforced before anything costs money, at the limit the server publishes. */
class AIReplyServiceValidationTest {

    private companion object {
        const val LIMIT = 400
    }

    @Test
    fun `blank message is rejected`() {
        val result = AIReplyService.validate("   \n  ")
        assertTrue(result is AIReplyService.ValidationResult.Invalid)
        assertEquals(
            AIReplyError.NoSourceMessage,
            (result as AIReplyService.ValidationResult.Invalid).error
        )
    }

    @Test
    fun `message is trimmed, not merely accepted`() {
        val result = AIReplyService.validate("  привет  ")
        assertEquals("привет", (result as AIReplyService.ValidationResult.Valid).message)
    }

    @Test
    fun `message at the limit is accepted`() {
        val message = "a".repeat(LIMIT)
        assertTrue(AIReplyService.validate(message, LIMIT) is AIReplyService.ValidationResult.Valid)
    }

    @Test
    fun `message over the limit is rejected with the limit attached`() {
        val message = "a".repeat(LIMIT + 1)
        val result = AIReplyService.validate(message, LIMIT)
        val error = (result as AIReplyService.ValidationResult.Invalid).error
        assertEquals(AIReplyError.MessageTooLong(LIMIT), error)
    }

    /**
     * The limit counts code points, matching the iOS `unicodeScalars` count and
     * the backend's. Counting UTF-16 units instead would reject a 160-emoji
     * message that both of the others accept.
     */
    @Test
    fun `emoji count as one character each`() {
        val message = "🙂".repeat(LIMIT)
        assertTrue(AIReplyService.validate(message, LIMIT) is AIReplyService.ValidationResult.Valid)
        assertEquals(LIMIT, AIReplyService.characterCount(message))
    }

    @Test
    fun `kazakh text counts by character, not by byte`() {
        val message = "Сәлеметсіз бе"
        assertEquals(13, AIReplyService.characterCount(message))
    }

    @Test
    fun `displayed source count matches validation at unicode boundaries`() {
        listOf(50, 400, 2000).forEach { limit ->
            val family = "👨‍👩‍👧‍👦"
            val exact = "ә".repeat(limit - 7) + family
            val atBoundary = " \n$exact\t "
            assertEquals(limit, AIReplyService.characterCount(atBoundary))
            assertTrue(AIReplyService.validate(atBoundary, limit) is AIReplyService.ValidationResult.Valid)
            val over = exact + "🙂"
            assertEquals(limit + 1, AIReplyService.characterCount(over))
            assertEquals(AIReplyError.MessageTooLong(limit),
                (AIReplyService.validate(over, limit) as AIReplyService.ValidationResult.Invalid).error)
        }
    }
}
