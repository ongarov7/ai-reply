package kz.yerek.aireply.billing

import android.app.Activity
import android.content.Context
import com.android.billingclient.api.BillingClient
import com.android.billingclient.api.BillingClientStateListener
import com.android.billingclient.api.BillingFlowParams
import com.android.billingclient.api.BillingResult
import com.android.billingclient.api.PendingPurchasesParams
import com.android.billingclient.api.ProductDetails
import com.android.billingclient.api.Purchase
import com.android.billingclient.api.QueryProductDetailsParams
import com.android.billingclient.api.QueryPurchasesParams
import com.android.billingclient.api.queryProductDetails
import com.android.billingclient.api.queryPurchasesAsync
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kz.yerek.aireply.platform.ReplyLog
import java.util.concurrent.ConcurrentHashMap
import kotlin.coroutines.resume

/**
 * [StoreClient] over Google Play Billing 8: one-time (INAPP) products,
 * pending purchases on, automatic reconnection on. Built on first use, so a
 * phone where purchases are switched off never binds to Play.
 *
 * Google Play арқылы сатып алу: тұтыну мен растауды сервер жасайды.
 */
class PlayStoreClient(context: Context) : StoreClient {

    private val appContext = context.applicationContext

    @Volatile
    private var listener: (StoreUpdate) -> Unit = {}

    /** The last details Play gave per product id: a purchase sheet needs the object itself. */
    private val details = ConcurrentHashMap<String, ProductDetails>()

    private val connecting = Mutex()

    private val client: BillingClient by lazy {
        BillingClient.newBuilder(appContext)
            .setListener { result, purchases -> listener(update(result, purchases)) }
            .enablePendingPurchases(PendingPurchasesParams.newBuilder().enableOneTimeProducts().build())
            .enableAutoServiceReconnection()
            .build()
    }

    override fun setListener(listener: (StoreUpdate) -> Unit) {
        this.listener = listener
    }

    override suspend fun connect(): Boolean = connecting.withLock {
        if (client.isReady) return@withLock true
        suspendCancellableCoroutine { continuation ->
            client.startConnection(object : BillingClientStateListener {
                override fun onBillingSetupFinished(result: BillingResult) {
                    if (continuation.isActive) continuation.resume(result.responseCode == BillingClient.BillingResponseCode.OK)
                }

                override fun onBillingServiceDisconnected() {
                    // Reconnection is automatic; a call waiting here learns it from the next answer.
                    if (continuation.isActive) continuation.resume(false)
                }
            })
        }
    }

    override suspend fun products(ids: List<String>): List<StoreProduct>? {
        if (ids.isEmpty()) return emptyList()
        val params = QueryProductDetailsParams.newBuilder()
            .setProductList(
                ids.map { id ->
                    QueryProductDetailsParams.Product.newBuilder()
                        .setProductId(id)
                        .setProductType(BillingClient.ProductType.INAPP)
                        .build()
                }
            )
            .build()
        val result = client.queryProductDetails(params)
        if (result.billingResult.responseCode != BillingClient.BillingResponseCode.OK) {
            ReplyLog.warn(null) { "play products not loaded: ${result.billingResult.responseCode}" }
            return null
        }
        return result.productDetailsList.orEmpty().mapNotNull { product ->
            // Only Play's own price is ever shown; a product without one is not offered.
            @Suppress("DEPRECATION")
            val price = product.oneTimePurchaseOfferDetails?.formattedPrice ?: return@mapNotNull null
            details[product.productId] = product
            StoreProduct(product.productId, price)
        }
    }

    override suspend fun purchases(): List<StorePurchase>? {
        val params = QueryPurchasesParams.newBuilder().setProductType(BillingClient.ProductType.INAPP).build()
        val result = client.queryPurchasesAsync(params)
        if (result.billingResult.responseCode != BillingClient.BillingResponseCode.OK) return null
        return result.purchasesList.map(::purchase)
    }

    override fun launch(activity: Activity, productId: String, accountToken: String): StoreError? {
        val product = details[productId] ?: return StoreError.FAILED
        val params = BillingFlowParams.newBuilder()
            .setProductDetailsParamsList(
                listOf(BillingFlowParams.ProductDetailsParams.newBuilder().setProductDetails(product).build())
            )
            // Exactly as the server sent it: the server checks it against the purchase.
            .setObfuscatedAccountId(accountToken)
            .build()
        val result = client.launchBillingFlow(activity, params)
        return if (result.responseCode == BillingClient.BillingResponseCode.OK) null else error(result.responseCode)
    }

    private fun update(result: BillingResult, purchases: List<Purchase>?): StoreUpdate = when (result.responseCode) {
        BillingClient.BillingResponseCode.OK -> StoreUpdate.Purchases(purchases.orEmpty().map(::purchase))
        BillingClient.BillingResponseCode.USER_CANCELED -> StoreUpdate.Cancelled
        else -> StoreUpdate.Failed(error(result.responseCode))
    }

    private fun purchase(purchase: Purchase) = StorePurchase(
        productIds = purchase.products,
        purchaseToken = purchase.purchaseToken,
        state = when (purchase.purchaseState) {
            Purchase.PurchaseState.PURCHASED -> StorePurchaseState.PURCHASED
            Purchase.PurchaseState.PENDING -> StorePurchaseState.PENDING
            else -> StorePurchaseState.UNKNOWN
        },
        accountToken = purchase.accountIdentifiers?.obfuscatedAccountId
    )

    private fun error(code: Int): StoreError = when (code) {
        BillingClient.BillingResponseCode.ITEM_ALREADY_OWNED -> StoreError.ALREADY_OWNED
        BillingClient.BillingResponseCode.BILLING_UNAVAILABLE,
        BillingClient.BillingResponseCode.SERVICE_UNAVAILABLE,
        BillingClient.BillingResponseCode.SERVICE_DISCONNECTED,
        BillingClient.BillingResponseCode.NETWORK_ERROR -> StoreError.UNAVAILABLE
        else -> StoreError.FAILED
    }
}
