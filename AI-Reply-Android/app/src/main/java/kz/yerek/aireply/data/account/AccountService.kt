package kz.yerek.aireply.data.account

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kz.yerek.aireply.data.legal.StoredLegalConsent

/**
 * What the client tells the server about itself.
 *
 * Құрылғы туралы ең қажетті мәлімет қана.
 *
 * Deliberately minimal: platform, app version, OS version, locale, time zone.
 * No device name, no model identifier, no ANDROID_ID, no advertising id.
 */
@Serializable
data class DeviceDescriptor(
    @SerialName("device_id") val deviceId: String,
    val platform: String,
    @SerialName("app_version") val appVersion: String,
    @SerialName("os_version") val osVersion: String,
    val locale: String,
    val timezone: String
)

@Serializable
internal data class RefreshRequest(
    @SerialName("refresh_token") val refreshToken: String,
    val device: DeviceDescriptor
)

@Serializable
internal data class LogoutRequest(@SerialName("refresh_token") val refreshToken: String)

@Serializable
private data class EmailCodeRequest(val email: String, val locale: String)

@Serializable
private data class EmailCodeVerifyRequest(
    val email: String,
    val code: String,
    val device: DeviceDescriptor
)

@Serializable
private data class GoogleSignInRequest(
    @SerialName("id_token") val idToken: String,
    val nonce: String,
    val device: DeviceDescriptor
)

@Serializable
private data class LinkEmailVerifyRequest(val email: String, val code: String)

/** Only the fields that changed; everything else is left alone server-side. */
@Serializable
data class ProfileUpdate(
    @SerialName("display_name") val displayName: String? = null,
    val role: String? = null,
    val description: String? = null,
    @SerialName("preferred_tone") val preferredTone: String? = null,
    @SerialName("business_offering") val businessOffering: String? = null,
    @SerialName("business_summary") val businessSummary: String? = null,
    @SerialName("business_rules") val businessRules: List<String>? = null,
    val locale: String? = null,
    val timezone: String? = null,
    @SerialName("onboarding_completed") val onboardingCompleted: Boolean? = null,
    /** Sent only to a server that announced `sender_profile`. */
    @SerialName("grammatical_gender") val grammaticalGender: String? = null,
    /** Sent only to a server that announced `sender_profile`; the server keeps the highest. */
    @SerialName("onboarding_version") val onboardingVersion: Int? = null,
    /** Sent only to a server that announced `preferred_language`: kk, ru, en or uz. */
    @SerialName("preferred_language") val preferredLanguage: String? = null
)

@Serializable
private data class RegisterDeviceRequest(
    @SerialName("device_id") val deviceId: String,
    val platform: String,
    @SerialName("app_version") val appVersion: String,
    @SerialName("os_version") val osVersion: String,
    val locale: String,
    @SerialName("push_token") val pushToken: String? = null,
    @SerialName("push_enabled") val pushEnabled: Boolean
)

@Serializable
private data class CheckoutRequest(@SerialName("plan_id") val planId: String)

@Serializable
private data class LegalConsentRequest(
    @SerialName("terms_version") val termsVersion: String,
    @SerialName("privacy_version") val privacyVersion: String,
    val locale: String,
    val platform: String,
    @SerialName("app_version") val appVersion: String
)

/**
 * Every call the app makes to the AI Reply backend.
 *
 * Барлық сұраныс осы жерден өтеді: қайталанатын желі коды жоқ.
 *
 * The service holds no state. Tokens belong to [AccountSession] and the base
 * URL to the configuration, so a screen only says what it wants, not how
 * authentication works.
 */
class AccountService(
    private val session: AccountSession,
    private val baseUrlProvider: () -> String?,
    private val deviceDescriptor: () -> DeviceDescriptor,
    /** How a request is sent; a test seam. */
    private val clientFactory: (baseUrl: String) -> ApiClient = { baseUrl -> ApiClient(baseUrl) }
) {

    private fun client(): ApiClient {
        val baseUrl = baseUrlProvider() ?: ApiError.InvalidRequest.raise()
        return clientFactory(baseUrl)
    }

    private val json: Json get() = jsonCodec

    // ------------------------------------------------------ public endpoints

    /** Limits, sign-in methods and current legal versions. Called before sign-in. */
    suspend fun serverConfig(): ServerConfigDto {
        val client = client()
        return decode(ServerConfigDto.serializer(), client.request("GET", "api/v1/config"))
    }

    suspend fun plans(): List<PlanDto> {
        val client = client()
        return decode(PlanListDto.serializer(), client.request("GET", "api/v1/plans")).plans
    }

    // -------------------------------------------------------------- sign-in

    /**
     * Sends a 4-digit code to [email]. The answer is the same whether or not an
     * account exists, so it says nothing about who is registered.
     */
    suspend fun requestEmailCode(email: String, locale: String): EmailChallengeDto {
        val client = client()
        val body = json.encodeToString(EmailCodeRequest.serializer(), EmailCodeRequest(email, locale))
        return decode(
            EmailChallengeDto.serializer(),
            client.request("POST", "api/v1/auth/email/otp/request", body)
        )
    }

    /** Checks the code, opening the account on first use, and stores the session. */
    suspend fun verifyEmailCode(email: String, code: String): AccountSessionDto {
        val client = client()
        val body = json.encodeToString(
            EmailCodeVerifyRequest.serializer(),
            EmailCodeVerifyRequest(email, code, deviceDescriptor())
        )
        return adopt(client.request("POST", "api/v1/auth/email/otp/verify", body))
    }

    /**
     * Trades a Google ID token for a session. The server checks the token's
     * signature, audience, expiry and [nonce] against Google's keys; the app
     * trusts none of it.
     */
    suspend fun signInWithGoogle(idToken: String, nonce: String): AccountSessionDto {
        val client = client()
        val body = json.encodeToString(
            GoogleSignInRequest.serializer(),
            GoogleSignInRequest(idToken, nonce, deviceDescriptor())
        )
        return adopt(client.request("POST", "api/v1/auth/google", body))
    }

    private fun adopt(body: String): AccountSessionDto {
        val result = decode(AccountSessionDto.serializer(), body)
        session.adopt(result)
        return result
    }

    suspend fun signOut() = session.signOut()

    // ----------------------------------------------- authenticated endpoints

    suspend fun account(): AccountDto = session.authenticated { token ->
        decode(AccountDto.serializer(), client().request("GET", "api/v1/me", token = token))
    }

    suspend fun usage(): UsageDto = session.authenticated { token ->
        decode(UsageDto.serializer(), client().request("GET", "api/v1/me/usage", token = token))
    }

    suspend fun subscription(): SubscriptionDto = session.authenticated { token ->
        decode(SubscriptionDto.serializer(), client().request("GET", "api/v1/me/subscription", token = token))
    }

    /**
     * Saves profile changes.
     *
     * POST rather than PATCH on purpose: HttpURLConnection refuses PATCH, and
     * the server exposes this alias for exactly that reason.
     */
    suspend fun updateProfile(update: ProfileUpdate): AccountProfile = session.authenticated { token ->
        val body = json.encodeToString(ProfileUpdate.serializer(), update)
        decode(AccountProfile.serializer(), client().request("POST", "api/v1/me", body, token))
    }

    /** Product events, at most 20 per call. The server refuses unknown names and values one by one. */
    suspend fun recordProductEvents(request: ProductEventsRequest): ProductEventsResultDto =
        session.authenticated { token ->
            val body = json.encodeToString(ProductEventsRequest.serializer(), request)
            decode(
                ProductEventsResultDto.serializer(),
                client().request("POST", "api/v1/analytics/events", body, token)
            )
        }

    /** Registers this device so a push token has somewhere to live later. */
    suspend fun registerDevice(pushToken: String? = null) {
        val descriptor = deviceDescriptor()
        val body = json.encodeToString(
            RegisterDeviceRequest.serializer(),
            RegisterDeviceRequest(
                deviceId = descriptor.deviceId,
                platform = descriptor.platform,
                appVersion = descriptor.appVersion,
                osVersion = descriptor.osVersion,
                locale = descriptor.locale,
                pushToken = pushToken,
                pushEnabled = pushToken != null
            )
        )
        session.authenticated { token ->
            client().request("POST", "api/v1/devices", body, token)
        }
    }

    /** Sends a code to an address the signed-in user wants to add for sign-in. */
    suspend fun requestLinkEmailCode(email: String, locale: String): EmailChallengeDto =
        session.authenticated { token ->
            val body = json.encodeToString(EmailCodeRequest.serializer(), EmailCodeRequest(email, locale))
            decode(
                EmailChallengeDto.serializer(),
                client().request("POST", "api/v1/me/email/otp/request", body, token)
            )
        }

    /** Proves the address and attaches it to this account; answers with the updated account. */
    suspend fun verifyLinkEmailCode(email: String, code: String): AccountDto =
        session.authenticated { token ->
            val body = json.encodeToString(LinkEmailVerifyRequest.serializer(), LinkEmailVerifyRequest(email, code))
            decode(AccountDto.serializer(), client().request("POST", "api/v1/me/email/otp/verify", body, token))
        }

    suspend fun recordLegalConsent(consent: StoredLegalConsent): LegalConsentDto =
        session.authenticated { token ->
            val request = LegalConsentRequest(
                termsVersion = consent.termsVersion,
                privacyVersion = consent.privacyVersion,
                locale = consent.locale,
                platform = consent.platform,
                appVersion = consent.appVersion
            )
            val body = json.encodeToString(LegalConsentRequest.serializer(), request)
            decode(
                LegalConsentDto.serializer(),
                client().request("POST", "api/v1/me/consents", body, token)
            )
        }

    /**
     * Withdraws the consent to the current terms and AI processing. AI
     * requests are refused with CONSENT_REQUIRED until [recordLegalConsent]
     * runs again; the account itself stays.
     */
    suspend fun withdrawLegalConsent() {
        session.authenticated { token ->
            client().request("DELETE", "api/v1/me/consents", token = token)
        }
    }

    /**
     * Deletes the account and everything the server keeps for it; every
     * token of it stops working. The POST alias rather than `DELETE /me`:
     * HttpURLConnection does not send a body with DELETE on every Android
     * version, and the alias takes the same (here empty) body.
     */
    suspend fun deleteAccount(): AccountDeletionDto = session.authenticated { token ->
        decode(AccountDeletionDto.serializer(), client().request("POST", "api/v1/me/delete", "{}", token))
    }

    /** A report about text the AI wrote; answers with the report's id. */
    suspend fun reportAIOutput(report: AIReportRequest): String = session.authenticated { token ->
        val body = json.encodeToString(AIReportRequest.serializer(), report)
        decode(AIReportResultDto.serializer(), client().request("POST", "api/v1/ai/reports", body, token)).id
    }

    // --------------------------------------- subscription (demo payment flow)

    suspend fun startCheckout(planId: String): CheckoutDto = session.authenticated { token ->
        val body = json.encodeToString(CheckoutRequest.serializer(), CheckoutRequest(planId))
        decode(CheckoutDto.serializer(), client().request("POST", "api/v1/payments/checkout", body, token))
    }

    /**
     * Confirms a payment. With the demo adapter this is what actually moves the
     * account onto the chosen plan; with a real acquirer the confirmation
     * arrives from the provider and this call only reads the result.
     */
    suspend fun confirmCheckout(paymentId: String): CheckoutResultDto = session.authenticated { token ->
        decode(
            CheckoutResultDto.serializer(),
            client().request("POST", "api/v1/payments/$paymentId/confirm", "{}", token)
        )
    }

    private companion object {
        val jsonCodec = Json {
            ignoreUnknownKeys = true
            encodeDefaults = false
            explicitNulls = false
        }

        /** A body we cannot parse is a malformed response, not a crash. */
        fun <T> decode(serializer: kotlinx.serialization.KSerializer<T>, body: String): T =
            runCatching { jsonCodec.decodeFromString(serializer, body) }
                .getOrElse { ApiError.MalformedResponse.raise() }
    }
}
