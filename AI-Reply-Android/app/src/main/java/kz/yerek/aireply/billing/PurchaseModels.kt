package kz.yerek.aireply.billing

import kotlinx.serialization.KSerializer
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.nullable
import kotlinx.serialization.descriptors.SerialDescriptor
import kotlinx.serialization.encoding.Decoder
import kotlinx.serialization.encoding.Encoder
import kotlinx.serialization.json.JsonDecoder
import kz.yerek.aireply.data.account.UsageDto

/**
 * `GET /api/v1/iap/products?platform=android`: what an administrator put on
 * sale, and the account token every purchase is bound to.
 *
 * Сатылымдағы өнімдер: бағасы серверде емес, Google Play-де.
 */
@Serializable
data class IapCatalogDto(
    val products: List<IapProductDto> = emptyList(),
    /** A stable UUID for this account; sent as Play's obfuscated account id, exactly as received. */
    @SerialName("account_token") val accountToken: String = "",
    /** Base, for an account that keeps buying packs; null when there is nothing to recommend. */
    @Serializable(with = OptionalRecommendationSerializer::class)
    val recommendation: IapRecommendationDto? = null
)

/** What the server knows of an account's pack purchases. All of it or none: see [OptionalRecommendationSerializer]. */
@Serializable
data class IapRecommendationDto(
    /** Names this recommendation, so turning it down does not turn down a later one. */
    val id: String,
    @SerialName("purchase_count") val purchaseCount: Int,
    @SerialName("spent_minor") val spentMinor: Long,
    val currency: String,
    @SerialName("base_price_minor") val basePriceMinor: Long,
    /** The Play product id of the Base plan. */
    @SerialName("base_store_product_id") val baseStoreProductId: String
)

/** An extra on the catalogue: an answer this build cannot read costs the card, never the products. */
internal object OptionalRecommendationSerializer : KSerializer<IapRecommendationDto?> {
    private val strict = IapRecommendationDto.serializer().nullable

    override val descriptor: SerialDescriptor = strict.descriptor

    override fun deserialize(decoder: Decoder): IapRecommendationDto? {
        val input = decoder as? JsonDecoder ?: return strict.deserialize(decoder)
        val element = input.decodeJsonElement()
        return runCatching { input.json.decodeFromJsonElement(strict, element) }.getOrNull()
    }

    override fun serialize(encoder: Encoder, value: IapRecommendationDto?) = strict.serialize(encoder, value)
}

@Serializable
data class IapProductDto(
    val code: String = "",
    /** `pack` (never expires), `pass` ([units] for [durationHours]) or `subscription`. */
    val kind: String = "",
    val units: Int = 0,
    @SerialName("duration_hours") val durationHours: Int = 0,
    /** The Play product id. */
    @SerialName("store_product_id") val storeProductId: String = "",
    /** In the account's language. */
    val name: String = "",
    /** A subscription's plan on the server. */
    @SerialName("plan_id") val planId: String? = null,
    /** A subscription's period: `month` or `year`. */
    @SerialName("billing_period") val billingPeriod: String? = null,
    /** A subscription's allowance: `daily`, `monthly` or `unlimited`. */
    @SerialName("entitlement_kind") val entitlementKind: String? = null,
    @SerialName("daily_limit") val dailyLimit: Int = 0,
    @SerialName("monthly_limit") val monthlyLimit: Int = 0,
    /** The server's price in minor units, to compare with Play's; never shown. */
    val price: Long? = null,
    val currency: String? = null,
    /** A subscription whose replies stay with the account after it ends. */
    @SerialName("credits_never_expire") val creditsNeverExpire: Boolean = false
)

/** `POST /api/v1/iap/google/purchases`. The server refuses any other field. */
@Serializable
data class GooglePurchaseRequest(
    @SerialName("product_id") val productId: String,
    @SerialName("purchase_token") val purchaseToken: String
)

/** 200 `granted | already_granted` with the new balance, or 202 `pending`. */
@Serializable
data class PurchaseResultDto(
    val status: String = "",
    val usage: UsageDto? = null
) {
    val isGranted: Boolean get() = status == STATUS_GRANTED || status == STATUS_ALREADY_GRANTED

    companion object {
        const val STATUS_GRANTED = "granted"
        const val STATUS_ALREADY_GRANTED = "already_granted"
        const val STATUS_PENDING = "pending"
    }
}

/** The two purchase calls, over the account's session. Throws [kz.yerek.aireply.data.account.ApiException]. */
interface PurchaseApi {
    suspend fun catalog(): IapCatalogDto
    suspend fun verify(productId: String, purchaseToken: String): PurchaseResultDto
}

/** The recommendations an account turned down, on this phone. */
interface RecommendationStore {
    fun isRecommendationDismissed(account: String, id: String): Boolean
    fun dismissRecommendation(account: String, id: String)
}
