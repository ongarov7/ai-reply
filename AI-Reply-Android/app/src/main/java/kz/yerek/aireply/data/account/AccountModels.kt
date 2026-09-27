package kz.yerek.aireply.data.account

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

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
    @SerialName("onboarding_completed") val onboardingCompleted: Boolean = false
) {
    /** What the user recognises themselves by. */
    val identifier: String get() = phone ?: email.orEmpty()
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
    @SerialName("onboarding_completed") val onboardingCompleted: Boolean = false
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

/** The OTP challenge. The code itself never travels back to the client. */
@Serializable
data class ChallengeDto(
    val kind: String = "phone",
    @SerialName("masked_identifier") val maskedIdentifier: String = "",
    val channel: String = "",
    @SerialName("expires_in") val expiresIn: Int = 0
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
            termsUrl = "https://api.meily.kz/offer",
            privacyUrl = "https://api.meily.kz/privacy"
        )
    }
}

/** A country the backend will accept a phone number from. */
@Serializable
data class CountryDto(
    val iso: String,
    @SerialName("dial_code") val dialCode: String,
    val name: String = "",
    val example: String = ""
) {
    /** 🇰🇿 from "KZ", without shipping a flag asset per country. */
    val flag: String
        get() = iso.uppercase().map { Character.toChars(0x1F1E6 - 'A'.code + it.code).concatToString() }
            .joinToString("")
}

/** Non-secret server configuration the client is allowed to know. */
@Serializable
data class ServerConfigDto(
    val locales: List<String> = emptyList(),
    val timezone: String = "",
    /** Set by the administrator; the server enforces it on every request. Null on old servers. */
    @SerialName("max_source_characters") val maxSourceCharacters: Int? = null,
    @SerialName("max_instruction_length") val maxInstructionLength: Int? = null,
    @SerialName("payment_mode") val paymentMode: String = "",
    val countries: List<CountryDto> = emptyList(),
    val legal: LegalConfigDto? = null
)

/** The reply endpoint's response. */
@Serializable
data class ReplyResponseDto(
    val reply: String,
    @SerialName("detected_language") val detectedLanguage: String? = null,
    val usage: UsageDto = UsageDto.UNKNOWN
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
    /** Which request field was refused, e.g. `source_text`. */
    val field: String? = null,
    /** The limit that field has, when it was refused for length. */
    @SerialName("max_characters") val maxCharacters: Int? = null
)
