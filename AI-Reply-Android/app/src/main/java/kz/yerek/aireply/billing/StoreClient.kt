package kz.yerek.aireply.billing

import android.app.Activity

/**
 * The store as the app needs it: Google Play Billing behind an interface, so
 * the purchase flow is tested on the JVM with a fake and a debug build can
 * show a sample catalogue.
 *
 * Дүкен интерфейсі: Google Play Billing, сынақта — жалған дүкен.
 *
 * The client never consumes or acknowledges a purchase: the server consumes it
 * after granting the replies (contract §3.8), so nothing here can lose one.
 */
interface StoreClient {

    /** Where Play's answers to a purchase go - also for one finished outside the app (a pending payment). */
    fun setListener(listener: (StoreUpdate) -> Unit)

    /** Connects if needed; false when Play cannot be reached on this phone. */
    suspend fun connect(): Boolean

    /** The products Play knows among [ids], with Play's own prices; null when Play did not answer. */
    suspend fun products(ids: List<String>): List<StoreProduct>?

    /** One-time purchases Play still holds (not consumed yet); null when Play did not answer. */
    suspend fun purchases(): List<StorePurchase>?

    /**
     * Opens Play's purchase sheet for [productId], bound to [accountToken]
     * (`setObfuscatedAccountId`). The outcome arrives at the listener.
     */
    fun launch(activity: Activity, productId: String, accountToken: String): StoreError?
}

/** A product as Play describes it. [formattedPrice] is the only price the app ever shows. */
data class StoreProduct(val productId: String, val formattedPrice: String)

enum class StorePurchaseState { PURCHASED, PENDING, UNKNOWN }

/** A purchase Play reported. The token is a secret of sorts: never logged, only sent to the server. */
data class StorePurchase(
    val productIds: List<String>,
    val purchaseToken: String,
    val state: StorePurchaseState,
    /** The account token the purchase was bound to, when Play says. */
    val accountToken: String? = null
)

/** How a purchase sheet ended, or a purchase Play finished meanwhile. */
sealed interface StoreUpdate {
    data class Purchases(val purchases: List<StorePurchase>) : StoreUpdate
    data object Cancelled : StoreUpdate
    data class Failed(val error: StoreError) : StoreUpdate
}

enum class StoreError {
    /** Play is missing, signed out, or its service is down. */
    UNAVAILABLE,
    /** An earlier purchase of this product is not consumed yet: the server will finish it. */
    ALREADY_OWNED,
    /** Anything else Play reported. */
    FAILED
}
