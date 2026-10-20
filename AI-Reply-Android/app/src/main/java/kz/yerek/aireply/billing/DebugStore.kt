package kz.yerek.aireply.billing

import android.app.Activity
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import java.util.UUID

/**
 * DEBUG BUILDS ONLY: a sample catalogue and a purchase that completes at
 * once, without Google Play or the server, so the purchase screen can be
 * reviewed on an emulator. Chosen in Settings ▸ Developer; a release build
 * never constructs it. The prices are samples, never real ones.
 *
 * Тек жөндеу нұсқасында: үлгі өнімдер, Google Play және серверсіз.
 */
object DebugStore {

    private val catalog = IapCatalogDto(
        products = listOf(
            IapProductDto("pack_20", "pack", 20, 0, "debug.pack_20", "20 ответов"),
            IapProductDto("pack_100", "pack", 100, 0, "debug.pack_100", "100 ответов"),
            IapProductDto("pass_24h", "pass", 40, 24, "debug.pass_24h", "Пропуск на 24 часа")
        ),
        accountToken = "00000000-0000-4000-8000-000000000000"
    )

    private val prices = mapOf(
        "debug.pack_20" to "990 ₸",
        "debug.pack_100" to "3 490 ₸",
        "debug.pass_24h" to "590 ₸"
    )

    fun api(): PurchaseApi = object : PurchaseApi {
        override suspend fun catalog(): IapCatalogDto = catalog
        override suspend fun verify(productId: String, purchaseToken: String) =
            PurchaseResultDto(status = PurchaseResultDto.STATUS_GRANTED)
    }

    fun client(scope: CoroutineScope): StoreClient = object : StoreClient {
        private var listener: (StoreUpdate) -> Unit = {}

        override fun setListener(listener: (StoreUpdate) -> Unit) {
            this.listener = listener
        }

        override suspend fun connect() = true

        override suspend fun products(ids: List<String>) =
            ids.mapNotNull { id -> prices[id]?.let { StoreProduct(id, it) } }

        override suspend fun purchases() = emptyList<StorePurchase>()

        override fun launch(activity: Activity, productId: String, accountToken: String): StoreError? {
            scope.launch {
                delay(600)
                listener(
                    StoreUpdate.Purchases(
                        listOf(StorePurchase(listOf(productId), "debug-" + UUID.randomUUID(), StorePurchaseState.PURCHASED, accountToken))
                    )
                )
            }
            return null
        }
    }
}
