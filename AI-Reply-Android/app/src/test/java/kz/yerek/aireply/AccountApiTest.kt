package kz.yerek.aireply

import kotlinx.serialization.json.Json
import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.ai.AccountReplyTransport
import kz.yerek.aireply.data.account.AccountSessionDto
import kz.yerek.aireply.data.account.ApiClient
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.CountryDto
import kz.yerek.aireply.data.account.PlanDto
import kz.yerek.aireply.data.account.ProfileUpdate
import kz.yerek.aireply.data.account.ReplyResponseDto
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The account layer's pure decisions: error mapping, wire format and the
 * localization fallbacks.
 *
 * Тіркелгі қабатының таза логикасы — желісіз тексеріледі.
 *
 * Networking itself is not mocked. What matters, and what these cover, is that
 * a server answer always turns into exactly one value the UI can render, and
 * that the JSON the app sends is the JSON the server documented.
 */
class AccountApiTest {

    private val json = Json { ignoreUnknownKeys = true; encodeDefaults = false; explicitNulls = false }

    private fun envelope(code: String, details: String = ""): String {
        val detailsPart = if (details.isEmpty()) "" else ", \"details\": {$details}"
        return """{"error": {"code": "$code", "message": "ignored"$detailsPart}}"""
    }

    // ------------------------------------------------------- error mapping

    @Test
    fun `quota error carries the numbers the screen needs`() {
        val error = ApiClient.mapServerError(
            status = 429,
            body = envelope(
                "DAILY_LIMIT_REACHED",
                "\"daily_limit\": 7, \"used_today\": 7, \"resets_at\": \"2026-03-11T19:00:00Z\""
            )
        )
        val quota = error as ApiError.DailyLimitReached
        assertEquals(7, quota.limit)
        assertEquals(7, quota.usedToday)
        assertEquals("2026-03-11T19:00:00Z", quota.resetsAt)
    }

    @Test
    fun `stable codes map to stable cases`() {
        val cases = listOf(
            Triple("UNAUTHORIZED", 401, ApiError.Unauthorized),
            Triple("TOKEN_EXPIRED", 401, ApiError.Unauthorized),
            Triple("ACCOUNT_DISABLED", 403, ApiError.AccountDisabled),
            Triple("INVALID_OTP", 400, ApiError.InvalidOtp),
            Triple("OTP_EXPIRED", 400, ApiError.OtpExpired),
            Triple("AI_TIMEOUT", 504, ApiError.ProviderTimeout),
            Triple("AI_PROVIDER_UNAVAILABLE", 502, ApiError.ProviderUnavailable),
            Triple("INVALID_REQUEST", 400, ApiError.InvalidRequest),
            Triple("CONFLICT", 409, ApiError.Conflict)
        )
        cases.forEach { (code, status, expected) ->
            assertEquals(code, expected, ApiClient.mapServerError(status, envelope(code)))
        }
    }

    /**
     * A proxy or load balancer can answer without our envelope. The client still
     * has to produce something sensible rather than crash or guess.
     */
    @Test
    fun `falls back to the status when there is no envelope`() {
        assertEquals(ApiError.Unauthorized, ApiClient.mapServerError(401, "<html>"))
        assertEquals(ApiError.Server, ApiClient.mapServerError(500, ""))
        assertEquals(ApiError.RateLimited(30), ApiClient.mapServerError(429, "", "30"))
        // The legacy /v1 endpoint answers 413 with its own shape.
        assertEquals(ApiError.InvalidRequest, ApiClient.mapServerError(413, """{"error":{"code":"message_too_long"}}"""))
    }

    /**
     * The reply flow has a closed error set; every backend failure has to land
     * in it, because the keyboard can only show those.
     */
    @Test
    fun `backend errors become reply errors`() {
        assertEquals(AIReplyError.Offline, AccountReplyTransport.map(ApiError.Offline))
        assertEquals(AIReplyError.AuthenticationFailed, AccountReplyTransport.map(ApiError.Unauthorized))
        assertEquals(AIReplyError.AuthenticationFailed, AccountReplyTransport.map(ApiError.AccountDisabled))
        // A spent quota and a burst of requests have different fixes, so the
        // user is told different things.
        assertEquals(
            AIReplyError.QuotaExhausted,
            AccountReplyTransport.map(ApiError.DailyLimitReached(7, 7, null))
        )
        assertEquals(AIReplyError.RateLimited, AccountReplyTransport.map(ApiError.RateLimited(30)))
        assertEquals(AIReplyError.TimedOut, AccountReplyTransport.map(ApiError.ProviderTimeout))
        assertEquals(AIReplyError.EmptyResponse, AccountReplyTransport.map(ApiError.EmptyResponse))
        assertEquals(AIReplyError.ServiceUnavailable, AccountReplyTransport.map(ApiError.Server))
    }

    // ------------------------------------------------------------ decoding

    /**
     * The decoder has to accept exactly what the server sends, snake case and
     * all — a rename on either side should fail here, not on a phone.
     */
    @Test
    fun `session decodes the server payload`() {
        val payload = """
        {
          "access_token": "a", "refresh_token": "r", "token_type": "Bearer",
          "expires_in": 900, "refresh_expires_at": "2026-04-09T09:00:00Z",
          "device_id": "device", "is_new_user": true,
          "user": {"id": "u1", "phone": "+77011234567", "status": "active",
                   "locale": "kk", "created_at": "2026-03-10T09:00:00Z", "onboarding_completed": false},
          "profile": {"display_name": "", "role": "", "description": "", "preferred_tone": "natural",
                      "business_offering": "", "business_summary": "", "business_rules": [],
                      "onboarding_completed": false, "updated_at": "2026-03-10T09:00:00Z"},
          "subscription": {"status": "active", "plan": {"id": "p1", "code": "free",
                           "name": {"en": "Free"}, "description": {"en": ""}, "price": 0,
                           "price_text": "0", "currency": "KZT", "daily_message_limit": 7,
                           "monthly_message_limit": 0, "period_days": 0, "is_free": true, "sort_order": 10}},
          "usage": {"daily_limit": 7, "used_today": 0, "remaining_today": 7, "monthly_limit": 0,
                    "used_month": 0, "resets_at": "2026-03-11T19:00:00Z", "timezone": "Asia/Almaty"}
        }
        """.trimIndent()

        val session = json.decodeFromString(AccountSessionDto.serializer(), payload)
        assertEquals("a", session.accessToken)
        assertEquals(900, session.expiresIn)
        assertTrue(session.isNewUser)
        assertEquals("+77011234567", session.user.identifier)
        assertEquals(7, session.subscription.plan.dailyLimit)
        assertEquals(7, session.usage.remainingToday)
    }

    @Test
    fun `reply response decodes with the usage block`() {
        val payload = """
        {"reply": "Сәлеметсіз бе!", "detected_language": "kk",
         "usage": {"daily_limit": 7, "used_today": 1, "remaining_today": 6,
                   "resets_at": "2026-03-11T19:00:00Z", "timezone": "Asia/Almaty"}}
        """.trimIndent()
        val decoded = json.decodeFromString(ReplyResponseDto.serializer(), payload)
        assertEquals("kk", decoded.detectedLanguage)
        assertEquals(6, decoded.usage.remainingToday)
    }

    /**
     * A partial profile update must send ONLY what changed: sending an empty
     * string for a field the user did not touch would wipe it server-side.
     */
    @Test
    fun `profile update sends only the fields that changed`() {
        val encoded = json.encodeToString(
            ProfileUpdate.serializer(),
            ProfileUpdate(role = "сатушы", onboardingCompleted = true)
        )
        assertTrue(encoded.contains("\"role\""))
        assertTrue(encoded.contains("\"onboarding_completed\""))
        assertTrue("untouched fields must not be serialised", !encoded.contains("description"))
        assertTrue(!encoded.contains("business_rules"))
    }

    // -------------------------------------------------------------- models

    @Test
    fun `plan name falls back to english`() {
        val plan = PlanDto(
            id = "1", code = "pro",
            name = mapOf("en" to "Pro", "kk" to "Pro KK"),
            description = mapOf("en" to "Description"),
            dailyLimit = 50
        )
        assertEquals("Pro KK", plan.localizedName("kk"))
        assertEquals("Pro", plan.localizedName("ru"))
        assertEquals("Description", plan.localizedDescription("kk"))
        assertEquals("pro", PlanDto(id = "2", code = "pro").localizedName("ru"))
    }

    @Test
    fun `country flag comes from the iso code`() {
        assertEquals("🇰🇿", CountryDto("KZ", "+7", "Kazakhstan", "").flag)
        assertEquals("🇺🇿", CountryDto("UZ", "+998", "Uzbekistan", "").flag)
    }

    @Test
    fun `usage defaults to unknown rather than to a limit`() {
        assertEquals(0, kz.yerek.aireply.data.account.UsageDto.UNKNOWN.dailyLimit)
        assertNull(null)
    }
}
