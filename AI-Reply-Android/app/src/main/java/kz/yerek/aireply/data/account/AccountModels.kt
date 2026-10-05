package kz.yerek.aireply.data.account

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonPrimitive

/**
 * Wire models for the AI Reply backend.
 *
 * Серверден келетін тіркелгі мен тариф деректері.
 *
 * DTOs, not domain types: they mirror the JSON the server sends and nothing
 * else. What the app reasons about — a profile, a template — already exists in
 * `domain/model` and is not duplicated here. Every field name that differs from
 * Kotlin convention carries an explicit [SerialName], so a rename on either
 * side is a compile-time or test failure rather than a silent null.
 */
@Serializable
data class AccountSessionDto(
    @SerialName("access_token") val accessToken: String,
    @SerialName("refresh_token") val refreshToken: String,
    @SerialName("expires_in") val expiresIn: Int,
    @SerialName("device_id") val deviceId: String = "",
    @SerialName("is_new_user") val isNewUser: Boolean = false,
    val user: AccountUser,
    val profile: AccountProfile,
    val subscription: SubscriptionDto,
    val usage: UsageDto,
    @SerialName("legal_consent") val legalConsent: LegalConsentDto? = null
)

/** Tokens only — what /auth/refresh answers with. */
@Serializable
data class TokenPairDto(
    @SerialName("access_token") val accessToken: String,
    @SerialName("refresh_token") val refreshToken: String,
    @SerialName("expires_in") val expiresIn: Int
)

/** The account itself. No name, no contacts, no device fingerprint. */
@Serializable
data class AccountUser(
    val id: String,
    val phone: String? = null,
    val email: String? = null,
    val status: String = "active",
    val locale: String = "en",
    @SerialName("onboarding_completed") val onboardingCompleted: Boolean = false,
    /** How this account can sign in: `email`, `google`, `apple`, `phone`. Absent on older servers. */
    @SerialName("auth_providers") val authProviders: List<String>? = null
) {
    /** What the user recognises themselves by: the e-mail, else the old phone number. */
    val identifier: String get() = email?.takeIf(String::isNotEmpty) ?: phone.orEmpty()

    /**
     * An account opened with a phone number and nothing else. Phone sign-in is
     * gone, so without an e-mail it could not be reached again after signing out.
     */
    val needsEmail: Boolean get() = email.isNullOrEmpty()

    val isActive: Boolean get() = status == "active"
}

/** Server-side copy of the personalisation answers. */
@Serializable
data class AccountProfile(
    @SerialName("display_name") val displayName: String = "",
    val role: String = "",
    val description: String = "",
    @SerialName("preferred_tone") val preferredTone: String = "natural",
    @SerialName("business_offering") val businessOffering: String = "",
    @SerialName("business_summary") val businessSummary: String = "",
    @SerialName("business_rules") val businessRules: List<String> = emptyList(),
    @SerialName("onboarding_completed") val onboardingCompleted: Boolean = false,
    /**
     * `male`, `female` or `unspecified`; absent on older servers. A string,
     * not the enum, so a value this build does not know cannot fail the
     * whole account response.
     */
    @SerialName("grammatical_gender") val grammaticalGender: String? = null,
    /** The newest onboarding finished on any device. Informational only. */
    @SerialName("onboarding_version") val onboardingVersion: Int? = null
)

/** A plan as the server defines it. Limits live there, never in the app. */
@Serializable
data class PlanDto(
    val id: String,
    val code: String,
    val name: Map<String, String> = emptyMap(),
    val description: Map<String, String> = emptyMap(),
    val price: Long = 0,
    @SerialName("price_text") val priceText: String = "",
    val currency: String = "",
    @SerialName("daily_message_limit") val dailyLimit: Int = 0,
    @SerialName("monthly_message_limit") val monthlyLimit: Int = 0,
    @SerialName("period_days") val periodDays: Int = 0,
    @SerialName("is_free") val isFree: Boolean = false,
    @SerialName("sort_order") val sortOrder: Int = 0
) {
    /** Localized name with an English fallback, mirroring the server. */
    fun localizedName(language: String): String =
        name[language] ?: name["en"] ?: code

    fun localizedDescription(language: String): String =
        description[language] ?: description["en"] ?: ""
}

@Serializable
data class PlanListDto(val plans: List<PlanDto> = emptyList())

/** Which plan the account is on right now. */
@Serializable
data class SubscriptionDto(
    val id: String? = null,
    val status: String = "active",
    val plan: PlanDto,
    @SerialName("expires_at") val expiresAt: String? = null,
    val source: String? = null
)

/** The only counter the app trusts: the server's. */
@Serializable
data class UsageDto(
    @SerialName("daily_limit") val dailyLimit: Int = 0,
    @SerialName("used_today") val usedToday: Int = 0,
    @SerialName("remaining_today") val remainingToday: Int = 0,
    @SerialName("monthly_limit") val monthlyLimit: Int = 0,
    @SerialName("used_month") val usedMonth: Int = 0,
    @SerialName("resets_at") val resetsAt: String = "",
    val timezone: String = ""
) {
    companion object {
        val UNKNOWN = UsageDto()
    }
}

/** Everything /api/v1/me returns in one call. */
@Serializable
data class AccountDto(
    val user: AccountUser,
    val profile: AccountProfile,
    val subscription: SubscriptionDto,
    val usage: UsageDto,
    @SerialName("legal_consent") val legalConsent: LegalConsentDto? = null
)

/**
 * An e-mail code is on its way. The code itself never travels back to the
 * client; the defaults are the server's own, for an answer missing a field.
 */
@Serializable
data class EmailChallengeDto(
    @SerialName("masked_email") val maskedEmail: String = "",
    @SerialName("expires_in") val expiresIn: Int = 300,
    /** Seconds until the server accepts a request for a new code. */
    @SerialName("resend_after") val resendAfter: Int = 32,
    @SerialName("code_length") val codeLength: Int = 4
)

@Serializable
data class LegalConsentDto(
    @SerialName("terms_version") val termsVersion: String,
    @SerialName("privacy_version") val privacyVersion: String,
    @SerialName("accepted_at") val acceptedAt: String,
    val locale: String,
    val platform: String,
    @SerialName("app_version") val appVersion: String? = null
)

@Serializable
data class LegalConfigDto(
    @SerialName("terms_version") val termsVersion: String,
    @SerialName("privacy_version") val privacyVersion: String,
    @SerialName("terms_url") val termsUrl: String,
    @SerialName("privacy_url") val privacyUrl: String
) {
    companion object {
        val PRODUCTION = LegalConfigDto(
            termsVersion = "2026-09-19",
            privacyVersion = "2026-09-19",
            termsUrl = "https://ai-reply.kz/offer",
            privacyUrl = "https://ai-reply.kz/privacy"
        )
    }
}

/**
 * What this server can do.
 *
 * Two kinds of flag with opposite defaults. A missing SIGN-IN flag means an
 * older server: assume yes, the button is harmless. A missing REQUEST-SHAPING
 * flag means no: the server decodes bodies strictly, so a field it does not
 * know turns a reply into a 400 that the keyboard would show as "message too
 * long".
 */
@Serializable
data class ServerFeaturesDto(
    @SerialName("email_otp") val emailOtp: Boolean = true,
    @SerialName("google_sign_in") val googleSignIn: Boolean = true,
    @SerialName("apple_sign_in") val appleSignIn: Boolean = true,
    /** `profile` on `/ai/reply` (description, role, tone, business). */
    @SerialName("reply_preferences") val replyPreferences: Boolean = false,
    /** `grammatical_gender` and `input_language` on the AI requests, gender and onboarding version on `/me`. */
    @SerialName("sender_profile") val senderProfile: Boolean = false,
    /** `POST /api/v1/ai/polish` exists and is switched on. */
    @SerialName("instruction_polish") val instructionPolish: Boolean = false,
    /** `POST /api/v1/analytics/events` exists and is switched on. */
    @SerialName("product_events") val productEvents: Boolean = false
)

/** Non-secret server configuration the client is allowed to know. */
@Serializable
data class ServerConfigDto(
    val locales: List<String> = emptyList(),
    val timezone: String = "",
    /** Set by the administrator; the server enforces it on every request. Null on old servers. */
    @SerialName("max_source_characters") val maxSourceCharacters: Int? = null,
    @SerialName("max_instruction_length") val maxInstructionLength: Int? = null,
    @SerialName("payment_mode") val paymentMode: String = "",
    val features: ServerFeaturesDto? = null,
    val legal: LegalConfigDto? = null
)

/** The reply endpoint's response. */
@Serializable
data class ReplyResponseDto(
    val reply: String,
    @SerialName("detected_language") val detectedLanguage: String? = null,
    val usage: UsageDto = UsageDto.UNKNOWN
)

/** `POST /api/v1/ai/compose`: a message written from the user's instruction. */
@Serializable
data class ComposeResponseDto(
    val text: String,
    @SerialName("detected_language") val detectedLanguage: String? = null,
    val usage: UsageDto = UsageDto.UNKNOWN
)

/**
 * `POST /api/v1/analytics/events`: a batch of product events.
 *
 * Only names, short codes, small numbers and flags travel here; the server
 * keeps an allow-list of both and refuses anything else per event.
 */
@Serializable
data class ProductEventsRequest(
    val platform: String,
    @SerialName("app_version") val appVersion: String,
    val events: List<ProductEventDto>
)

@Serializable
data class ProductEventDto(
    val name: String,
    /** When it happened, RFC 3339 in UTC, to the second. */
    val ts: String,
    /** Each value is a JSON string, number or boolean. */
    val props: Map<String, JsonPrimitive>
)

@Serializable
data class ProductEventsResultDto(
    val accepted: Int = 0,
    val rejected: Int = 0
)

/** Demo checkout. A real acquirer changes this shape, not the callers. */
@Serializable
data class CheckoutDto(
    @SerialName("payment_id") val paymentId: String,
    val provider: String = "",
    val status: String = "",
    val demo: Boolean = false
)

@Serializable
data class CheckoutResultDto(
    val subscription: SubscriptionDto,
    val usage: UsageDto
)

/** The error envelope every endpoint uses. */
@Serializable
data class ErrorEnvelopeDto(val error: ErrorPayloadDto)

@Serializable
data class ErrorPayloadDto(
    val code: String = "",
    val message: String = "",
    val details: ErrorDetailsDto? = null
)

@Serializable
data class ErrorDetailsDto(
    @SerialName("daily_limit") val dailyLimit: Int? = null,
    @SerialName("used_today") val usedToday: Int? = null,
    @SerialName("resets_at") val resetsAt: String? = null,
    @SerialName("retry_after_seconds") val retryAfterSeconds: Int? = null,
    /** Wrong-code answers: how many more tries this code allows. */
    @SerialName("attempts_remaining") val attemptsRemaining: Int? = null,
    /** Which request field was refused, e.g. `source_text`. */
    val field: String? = null,
    /** The limit that field has, when it was refused for length. */
    @SerialName("max_characters") val maxCharacters: Int? = null
)
