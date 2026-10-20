package kz.yerek.aireply.billing

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
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
    @SerialName("account_token") val accountToken: String = ""
)

@Serializable
data class IapProductDto(
    val code: String = "",
    /** `pack` (never expires) or `pass` ([units] for [durationHours]). */
    val kind: String = "",
    val units: Int = 0,
    @SerialName("duration_hours") val durationHours: Int = 0,
    /** The Play product id. */
    @SerialName("store_product_id") val storeProductId: String = "",
    /** In the account's language. */
    val name: String = ""
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
