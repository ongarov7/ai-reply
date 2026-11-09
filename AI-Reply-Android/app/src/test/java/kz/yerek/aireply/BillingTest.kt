package kz.yerek.aireply

import android.app.Activity
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kz.yerek.aireply.billing.BillingRepository
import kz.yerek.aireply.billing.BillingState
import kz.yerek.aireply.billing.IapCatalogDto
import kz.yerek.aireply.billing.IapProductDto
import kz.yerek.aireply.billing.OfferedProduct
import kz.yerek.aireply.billing.PurchaseApi
import kz.yerek.aireply.billing.PurchaseNotice
import kz.yerek.aireply.billing.PurchaseResultDto
import kz.yerek.aireply.billing.StoreClient
import kz.yerek.aireply.billing.StoreError
import kz.yerek.aireply.billing.StoreProduct
import kz.yerek.aireply.billing.StorePurchase
import kz.yerek.aireply.billing.StorePurchaseState
import kz.yerek.aireply.billing.StoreUpdate
import kz.yerek.aireply.data.account.AccountService
import kz.yerek.aireply.data.account.ApiClient
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.ApiException
import kz.yerek.aireply.data.account.DeviceDescriptor
import kz.yerek.aireply.data.account.UsageDto
import kz.yerek.aireply.support.FakeCredentials
import kz.yerek.aireply.support.FakeResponse
import kz.yerek.aireply.support.FakeServer
import kz.yerek.aireply.support.sessionOn
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Buying replies through Google Play (contract §3.8): a fake Play and a fake
 * server. The client binds the purchase to the account, hands every purchase
 * to the server, never consumes one, and says honestly what happened.
 *
 * Google Play арқылы сатып алу: жалған дүкен және жалған сервер.
 */
class BillingTest {

    private class FakeStore : StoreClient {
        private var onUpdate: (StoreUpdate) -> Unit = {}
        var reachable = true
        var connects = 0
        val prices = mutableMapOf("play.pack_20" to "990 ₸", "play.pass_24h" to "590 ₸")
        val periods = mutableMapOf<String, String>()
        val micros = mutableMapOf<String, Long>()
        var replaced: String? = null
        val held = mutableListOf<StorePurchase>()
        val launched = mutableListOf<Pair<String, String>>()
        var launchError: StoreError? = null

        override fun setListener(listener: (StoreUpdate) -> Unit) {
            onUpdate = listener
        }

        override suspend fun connect(): Boolean {
            connects++
            return reachable
        }

        override suspend fun products(oneTime: List<String>, subscriptions: List<String>) =
            (oneTime + subscriptions).mapNotNull { id -> prices[id]?.let { StoreProduct(id, it, periods[id], micros[id], "KZT".takeIf { _ -> id in micros }) } }

        var answersPurchases = true
        var onPurchases: () -> Unit = {}

        override suspend fun purchases(): List<StorePurchase>? {
            onPurchases()
            return held.toList().takeIf { answersPurchases }
        }

        override fun launch(activity: Activity, productId: String, accountToken: String, replacing: String?): StoreError? {
            launched += productId to accountToken
            replaced = replacing
            return launchError
        }

        fun reports(vararg purchases: StorePurchase) = onUpdate(StoreUpdate.Purchases(purchases.toList()))

        fun answers(update: StoreUpdate) = onUpdate(update)
    }

    /** The server's purchase endpoints as scripted answers. */
    private class FakeApi : PurchaseApi {
        var catalogError: ApiError? = null
        var catalogs = 0
        var catalog: IapCatalogDto = CATALOG
        val verified = mutableListOf<Pair<String, String>>()
        var answer: (String) -> PurchaseResultDto = { PurchaseResultDto(PurchaseResultDto.STATUS_GRANTED, USAGE) }
        var gate: CompletableDeferred<Unit>? = null

        override suspend fun catalog(): IapCatalogDto {
            catalogs++
            catalogError?.let { throw ApiException(it, 403) }
            return catalog
        }

        override suspend fun verify(productId: String, purchaseToken: String): PurchaseResultDto {
            verified += productId to purchaseToken
            gate?.await()
            return answer(purchaseToken)
        }
    }

    private val store = FakeStore()
    private val api = FakeApi()
    /** `features.iap_android` true: on sale. */
    private var offered = true
    /** `features.iap_android` sent at all: what was paid is handed over. */
    private var deliverable = true
    private val usages = mutableListOf<UsageDto>()
    private var refreshes = 0
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)
    private val activity = Activity()

    private val billing = BillingRepository(
        store = store,
        api = api,
        isOffered = { offered },
        canDeliver = { deliverable },
        onUsage = { usages += it },
        refreshUsage = { refreshes++ },
        scope = scope
    )

    private fun purchase(token: String, state: StorePurchaseState = StorePurchaseState.PURCHASED, account: String? = TOKEN) =
        StorePurchase(listOf("play.pack_20"), token, state, account)

    private val state: BillingState get() = billing.state.value

    // ------------------------------------------------------ subscriptions

    private fun subscription(
        id: String = "play.base.month",
        period: String = "month",
        kind: String = "monthly",
        monthly: Int = 1000,
        price: Long? = null
    ) = IapProductDto(
        code = "base", kind = "subscription", storeProductId = id, name = "Base", planId = "base", billingPeriod = period,
        entitlementKind = kind, monthlyLimit = monthly, price = price, currency = "KZT".takeIf { price != null }
    )

    private fun sells(vararg products: IapProductDto) {
        api.catalog = IapCatalogDto(products.toList(), TOKEN)
    }

    private fun sellsBaseAndPro() {
        sells(subscription(), subscription(id = "play.pro.month", kind = "unlimited", monthly = 0))
        store.prices["play.base.month"] = "990 ₸"
        store.periods["play.base.month"] = "month"
        store.prices["play.pro.month"] = "3 990 ₸"
        store.periods["play.pro.month"] = "month"
    }

    private fun runningBase(token: String = "running-token", account: String? = TOKEN) =
        StorePurchase(listOf("play.base.month"), token, StorePurchaseState.PURCHASED, account, isSubscription = true)

    @Test
    fun `a subscription is offered with Play's price and the server's allowance`() = runBlocking {
        sells(subscription())
        store.prices["play.base.month"] = "990 ₸"
        store.periods["play.base.month"] = "month"
        billing.load()
        val offer = state.subscriptions.single()
        assertEquals("990 ₸", offer.price)
        assertEquals(1000, offer.monthlyLimit)
        assertEquals("month", offer.billingPeriod)
    }

    @Test
    fun `a subscription Play sells for another period is not offered`() = runBlocking {
        sells(subscription())
        store.prices["play.base.month"] = "990 ₸"
        store.periods["play.base.month"] = "year"
        billing.load()
        assertTrue(state.subscriptions.isEmpty())
    }

    @Test
    fun `a subscription whose allowance this build cannot show is not offered`() = runBlocking {
        sells(subscription(monthly = 0))
        store.prices["play.base.month"] = "990 ₸"
        store.periods["play.base.month"] = "month"
        billing.load()
        assertTrue(state.subscriptions.isEmpty())
    }

    @Test
    fun `a price the server and Play disagree on cannot be bought`() = runBlocking {
        sells(subscription(price = 99900))
        store.prices["play.base.month"] = "1 090 ₸"
        store.periods["play.base.month"] = "month"
        store.micros["play.base.month"] = 1_090_000_000L
        billing.load()
        val offer = state.subscriptions.single()
        assertTrue(offer.hasPriceMismatch)
        billing.buy(activity, offer)
        assertTrue(store.launched.isEmpty())
    }

    @Test
    fun `a matching price is not a mismatch`() = runBlocking {
        sells(subscription(price = 99900))
        store.prices["play.base.month"] = "999 ₸"
        store.periods["play.base.month"] = "month"
        store.micros["play.base.month"] = 999_000_000L
        billing.load()
        assertFalse(state.subscriptions.single().hasPriceMismatch)
    }

    @Test
    fun `a new subscription replaces the one already running`() = runBlocking {
        sells(subscription(), subscription(id = "play.pro.month", kind = "unlimited", monthly = 0))
        store.prices["play.base.month"] = "990 ₸"
        store.periods["play.base.month"] = "month"
        store.prices["play.pro.month"] = "3 990 ₸"
        store.periods["play.pro.month"] = "month"
        store.held += StorePurchase(listOf("play.base.month"), "running-token", StorePurchaseState.PURCHASED, TOKEN, isSubscription = true)
        billing.load()
        billing.buy(activity, state.subscriptions.first { it.storeProductId == "play.pro.month" })
        assertEquals("running-token", store.replaced)
    }

    @Test
    fun `an upgrade leaves another account's running subscription alone`() = runBlocking {
        sellsBaseAndPro()
        store.held += runningBase(account = OTHER_TOKEN)
        billing.load()
        billing.buy(activity, state.subscriptions.first { it.storeProductId == "play.pro.month" })
        assertEquals(listOf("play.pro.month" to TOKEN), store.launched)
        assertNull(store.replaced)
    }

    @Test
    fun `what Play holds is known before any plan can be tapped`() = runBlocking {
        sellsBaseAndPro()
        store.held += runningBase()
        val phases = mutableListOf<BillingState.Phase>()
        store.onPurchases = { phases += state.phase }
        billing.load()
        assertEquals(BillingState.Phase.LOADING, phases.first())
        billing.buy(activity, state.subscriptions.first { it.storeProductId == "play.pro.month" })
        assertEquals("running-token", store.replaced)
    }

    @Test
    fun `Play not saying what it holds offers a retry, not a second subscription`() = runBlocking {
        sellsBaseAndPro()
        store.answersPurchases = false
        billing.load()
        assertEquals(BillingState.Phase.UNAVAILABLE, state.phase)
        assertTrue(state.subscriptions.isEmpty())
    }

    @Test
    fun `restore hands everything to the server and says what came of it`() = runBlocking {
        billing.load()
        store.held += StorePurchase(listOf("play.pack_20"), "paid-token", StorePurchaseState.PURCHASED, TOKEN, isSubscription = true)
        billing.restore()
        assertTrue(api.verified.any { it.second == "paid-token" })
        assertEquals(PurchaseNotice.RESTORED, state.notice)
        assertFalse(state.restoring)
    }

    @Test
    fun `restore during a routine re-check shares its answer`() = runBlocking {
        billing.load()
        store.held += runningBase()
        api.answer = { PurchaseResultDto(PurchaseResultDto.STATUS_ALREADY_GRANTED, USAGE) }
        api.gate = CompletableDeferred()

        billing.resume()
        val restoring = launch(start = CoroutineStart.UNDISPATCHED) { billing.restore() }
        api.gate!!.complete(Unit)
        restoring.join()

        assertEquals("one request for the two callers", 1, api.verified.size)
        assertEquals(PurchaseNotice.RESTORED, state.notice)
        assertFalse(state.restoring)
    }

    @Test
    fun `restoring another account's purchase says whose it is`() = runBlocking {
        billing.load()
        store.held += purchase("theirs", account = OTHER_TOKEN)
        api.answer = { throw ApiException(ApiError.PurchaseInvalid("account_mismatch"), 400) }

        billing.restore()

        assertEquals(PurchaseNotice.OTHER_ACCOUNT, state.notice)
        assertFalse(state.restoring)
    }

    @Test
    fun `restore with nothing held says so`() = runBlocking {
        billing.load()
        store.held.clear()
        billing.restore()
        assertEquals(PurchaseNotice.NOTHING_TO_RESTORE, state.notice)
    }

    @Test
    fun `a running subscription is sent again on each return, a pack only once`() = runBlocking {
        billing.load()
        store.held += StorePurchase(listOf("play.base.month"), "sub-token", StorePurchaseState.PURCHASED, TOKEN, isSubscription = true)
        store.held += StorePurchase(listOf("play.pack_20"), "pack-token", StorePurchaseState.PURCHASED, TOKEN)
        billing.reconcile()
        billing.reconcile()
        assertEquals(2, api.verified.count { it.second == "sub-token" })
        assertEquals(1, api.verified.count { it.second == "pack-token" })
    }

    @Test
    fun `a routine re-check the server could not answer says nothing, a pack on return still does`() = runBlocking {
        billing.load()
        store.held += runningBase()
        api.answer = { throw ApiException(ApiError.Offline, null) }

        billing.reconcile()
        assertNull("nobody bought anything just now", state.notice)

        store.held += purchase("unfinished-pack")
        billing.reconcile()
        assertEquals(PurchaseNotice.RETRY_LATER, state.notice)
    }

    @Test
    fun `restore still says when a running subscription could not be checked`() = runBlocking {
        billing.load()
        store.held += runningBase()
        api.answer = { throw ApiException(ApiError.Offline, null) }

        billing.restore()

        assertEquals(PurchaseNotice.RETRY_LATER, state.notice)
    }

    // ---------------------------------------------------------- catalogue

    @Test
    fun `only products both sides know are offered, at Play's price, in the server's order`() = runBlocking {
        billing.load()
        assertEquals(BillingState.Phase.READY, state.phase)
        assertEquals(
            listOf(
                OfferedProduct("pack_20", OfferedProduct.Kind.PACK, 20, 0, "play.pack_20", "20 ответов", "990 ₸"),
                OfferedProduct("pass_24h", OfferedProduct.Kind.PASS, 40, 24, "play.pass_24h", "Пропуск", "590 ₸")
            ),
            state.products
        )
    }

    @Test
    fun `on a server without purchases nothing is loaded and Play is never bound`() = runBlocking {
        offered = false
        deliverable = false
        store.held += purchase("token-1")
        billing.load()
        billing.resume()
        billing.reconcile()
        assertEquals(BillingState.Phase.OFF, state.phase)
        assertEquals(0, store.connects)
        assertEquals(0, api.catalogs)
        assertTrue(api.verified.isEmpty())
    }

    @Test
    fun `with sales closed nothing is offered, but a purchase already paid is still handed over`() = runBlocking {
        offered = false
        // A pending payment that cleared after the admin closed sales.
        store.held += purchase("cleared-late")
        billing.load()
        assertEquals(BillingState.Phase.OFF, state.phase)
        assertTrue(state.products.isEmpty())
        assertEquals("the catalogue is refused while closed: not asked", 0, api.catalogs)
        assertEquals(listOf("play.pack_20" to "cleared-late"), api.verified)
        assertEquals(listOf(USAGE), usages)

        store.held += purchase("on-return")
        billing.resume()
        assertEquals(listOf("cleared-late", "on-return"), api.verified.map { it.second })
    }

    @Test
    fun `with the token unknown every purchase goes, and another account's refusal is not reported`() = runBlocking {
        // The flag still says on sale, but the server closed sales meanwhile.
        api.catalogError = ApiError.PurchasesUnavailable
        store.held += purchase("mine")
        store.held += purchase("theirs", account = "11111111-1111-4111-8111-111111111111")
        api.answer = { token ->
            if (token == "theirs") throw ApiException(ApiError.PurchaseInvalid("account_mismatch"), 400)
            PurchaseResultDto(PurchaseResultDto.STATUS_ALREADY_GRANTED, USAGE)
        }
        billing.reconcile()
        assertEquals(listOf("mine", "theirs"), api.verified.map { it.second })
        assertNull("someone else's purchase is not this account's news", state.notice)

        billing.reconcile()
        assertEquals("both finished, not sent again", 2, api.verified.size)
    }

    @Test
    fun `nothing held, nothing asked of the server`() = runBlocking {
        billing.resume()
        billing.reconcile()
        assertEquals(0, api.catalogs)
        assertTrue(api.verified.isEmpty())
    }

    @Test
    fun `sales closed on the server hide purchases, a server out of reach offers a retry`() = runBlocking {
        api.catalogError = ApiError.PurchasesUnavailable
        billing.load()
        assertEquals(BillingState.Phase.OFF, state.phase)

        api.catalogError = ApiError.Offline
        billing.load()
        assertEquals(BillingState.Phase.UNAVAILABLE, state.phase)

        api.catalogError = null
        store.reachable = false
        billing.load()
        assertEquals("Play not reachable", BillingState.Phase.UNAVAILABLE, state.phase)
    }

    @Test
    fun `sales closing takes the plans already on screen away`() = runBlocking {
        billing.load()
        assertTrue(state.products.isNotEmpty())
        val offer = state.packs.single()

        api.catalogError = ApiError.PurchasesUnavailable
        billing.load()

        assertEquals(BillingState.Phase.OFF, state.phase)
        assertTrue(state.products.isEmpty())
        billing.buy(activity, offer)
        assertTrue("nothing can be bought once sales are closed", store.launched.isEmpty())
    }

    @Test
    fun `sales closing while a purchase is checked takes the plans away too`() = runBlocking {
        billing.load()
        val offer = state.packs.single()
        api.answer = { throw ApiException(ApiError.PurchasesUnavailable, 403) }

        store.reports(purchase("token-1"))

        assertEquals(BillingState.Phase.OFF, state.phase)
        assertTrue(state.products.isEmpty())
        assertEquals("the purchase stays with Play", PurchaseNotice.RETRY_LATER, state.notice)
        billing.buy(activity, offer)
        assertTrue(store.launched.isEmpty())
    }

    // ---------------------------------------------------------- purchase

    @Test
    fun `a purchase is bound to the account and granted by the server`() = runBlocking {
        billing.load()
        billing.buy(activity, state.packs.single())
        assertEquals(listOf("play.pack_20" to TOKEN), store.launched)
        assertEquals("play.pack_20", state.busyProductId)

        store.reports(purchase("token-1"))

        assertEquals(listOf("play.pack_20" to "token-1"), api.verified)
        assertEquals(listOf(USAGE), usages)
        assertEquals(PurchaseNotice.GRANTED, state.notice)
        assertNull(state.busyProductId)
    }

    @Test
    fun `a second callback for the same purchase sends nothing`() = runBlocking {
        billing.load()
        api.gate = CompletableDeferred()
        store.reports(purchase("token-1"))
        store.reports(purchase("token-1"))
        api.gate!!.complete(Unit)
        store.reports(purchase("token-1"))
        assertEquals(1, api.verified.size)
    }

    @Test
    fun `a pending payment is shown as pending and handed over once it clears`() = runBlocking {
        billing.load()
        billing.buy(activity, state.packs.single())
        store.reports(purchase("token-2", StorePurchaseState.PENDING))

        assertTrue(api.verified.isEmpty())
        assertEquals(setOf("play.pack_20"), state.pendingProductIds)
        assertEquals(PurchaseNotice.PENDING, state.notice)
        assertNull("the buttons come back", state.busyProductId)

        store.held += purchase("token-2")
        billing.reconcile()
        assertEquals(listOf("play.pack_20" to "token-2"), api.verified)
        assertEquals(emptySet<String>(), state.pendingProductIds)
    }

    @Test
    fun `the server's pending answer is pending too`() = runBlocking {
        billing.load()
        api.answer = { PurchaseResultDto(PurchaseResultDto.STATUS_PENDING) }
        store.reports(purchase("token-3"))
        assertEquals(PurchaseNotice.PENDING, state.notice)
        assertEquals(setOf("play.pack_20"), state.pendingProductIds)
    }

    @Test
    fun `on return unfinished purchases go to the server again, but not another account's`() = runBlocking {
        store.held += purchase("mine")
        store.held += purchase("theirs", account = "11111111-1111-4111-8111-111111111111")
        api.answer = { PurchaseResultDto(PurchaseResultDto.STATUS_ALREADY_GRANTED, USAGE) }
        billing.reconcile()

        assertEquals(listOf("play.pack_20" to "mine"), api.verified)
        assertNull("a repeat found on return is not news", state.notice)
        assertEquals(listOf(USAGE), usages)

        billing.reconcile()
        assertEquals("finished once, not sent again in this process", 1, api.verified.size)
    }

    @Test
    fun `a grant without a balance reads the balance again`() = runBlocking {
        billing.load()
        api.answer = { PurchaseResultDto(PurchaseResultDto.STATUS_GRANTED) }
        store.reports(purchase("token-4"))
        assertEquals(1, refreshes)
        assertTrue(usages.isEmpty())
    }

    @Test
    fun `cancellation and failures are said as they are`() = runBlocking {
        billing.load()
        billing.buy(activity, state.packs.single())
        store.answers(StoreUpdate.Cancelled)
        assertEquals(PurchaseNotice.CANCELLED, state.notice)
        assertNull(state.busyProductId)

        store.launchError = StoreError.UNAVAILABLE
        billing.buy(activity, state.packs.single())
        assertEquals(PurchaseNotice.UNAVAILABLE, state.notice)
        assertNull(state.busyProductId)

        store.answers(StoreUpdate.Failed(StoreError.FAILED))
        assertEquals(PurchaseNotice.FAILED, state.notice)
    }

    @Test
    fun `a refused purchase is not sent again, one the server could not finish is`() = runBlocking {
        billing.load()
        api.answer = { throw ApiException(ApiError.PurchaseInvalid("revoked"), 400) }
        store.reports(purchase("refused"))
        assertEquals(PurchaseNotice.NOT_CONFIRMED, state.notice)

        api.answer = { throw ApiException(ApiError.StoreUnavailable, 503) }
        store.reports(purchase("later"))
        assertEquals(PurchaseNotice.RETRY_LATER, state.notice)

        api.answer = { throw ApiException(ApiError.PurchaseOtherAccount, 409) }
        store.reports(purchase("someone-else"))
        assertEquals(PurchaseNotice.OTHER_ACCOUNT, state.notice)

        api.answer = { PurchaseResultDto(PurchaseResultDto.STATUS_GRANTED, USAGE) }
        store.held += listOf(purchase("refused"), purchase("later"), purchase("someone-else"))
        billing.reconcile()
        assertEquals(
            listOf("refused", "later", "someone-else", "later"),
            api.verified.map { it.second }
        )
    }

    @Test
    fun `an earlier purchase Play still holds is finished instead of failing`() = runBlocking {
        billing.load()
        store.held += purchase("old")
        store.launchError = StoreError.ALREADY_OWNED
        billing.buy(activity, state.packs.single())
        assertEquals(listOf("play.pack_20" to "old"), api.verified)
        assertNull(state.busyProductId)
    }

    @Test
    fun `a plan another account holds on this Google account says so`() = runBlocking {
        sellsBaseAndPro()
        store.held += runningBase(account = OTHER_TOKEN)
        billing.load()
        store.launchError = StoreError.ALREADY_OWNED

        billing.buy(activity, state.subscriptions.first { it.storeProductId == "play.base.month" })

        assertEquals(PurchaseNotice.OTHER_ACCOUNT, state.notice)
        assertNull(state.busyProductId)
        assertTrue("that account hands it over", api.verified.isEmpty())
    }

    @Test
    fun `signing out forgets the account's token`() = runBlocking {
        billing.load()
        billing.reset()
        assertEquals(BillingState(), state)
        billing.buy(activity, OfferedProduct("pack_20", OfferedProduct.Kind.PACK, 20, 0, "play.pack_20", "x", "990 ₸"))
        assertTrue("no purchase without this account's token", store.launched.isEmpty())
    }

    // ---------------------------------------------------------- the wire

    @Test
    fun `the catalogue and the hand-over are the server's documented calls`() = runBlocking {
        val server = FakeServer { request ->
            when (request.path) {
                "/api/v1/iap/products" -> FakeResponse(
                    200,
                    """{"products":[{"code":"pack_20","kind":"pack","units":20,"duration_hours":0,
                       "store_product_id":"play.pack_20","name":"20 ответов"}],"account_token":"$TOKEN"}"""
                )
                "/api/v1/iap/google/purchases" -> if (request.body.contains("pending-token")) {
                    FakeResponse(202, """{"status":"pending"}""")
                } else {
                    FakeResponse(200, """{"status":"granted","usage":{"daily_limit":5,"resets_at":"x","credits":20,"remaining_total":20}}""")
                }
                else -> FakeResponse(404)
            }
        }
        val service = AccountService(
            session = sessionOn(server, FakeCredentials(access = "a", refresh = "r")),
            baseUrlProvider = { "https://example.test" },
            deviceDescriptor = { DeviceDescriptor("device-1", "android", "1.0", "15", "en", "Asia/Almaty") },
            clientFactory = { baseUrl -> ApiClient(baseUrl, openConnection = server::open) }
        )

        val catalog = service.iapProducts()
        assertEquals(TOKEN, catalog.accountToken)
        assertEquals("play.pack_20", catalog.products.single().storeProductId)
        assertEquals("platform=android", server.requests.first().query)
        assertEquals("GET", server.requests.first().method)

        val granted = service.verifyGooglePurchase("play.pack_20", "purchase-token")
        assertTrue(granted.isGranted)
        assertEquals(20, granted.usage?.remainingTotal)
        val body = Json.parseToJsonElement(server.requests.last().body).jsonObject
        assertEquals("exactly the two documented fields", setOf("product_id", "purchase_token"), body.keys)
        assertEquals("purchase-token", body["purchase_token"]!!.jsonPrimitive.content)

        val pending = service.verifyGooglePurchase("play.pack_20", "pending-token")
        assertEquals(PurchaseResultDto.STATUS_PENDING, pending.status)
        assertFalse(pending.isGranted)
        assertFalse(
            "the client never consumes or acknowledges",
            server.requests.any { it.path.contains("consume") || it.path.contains("acknowledge") }
        )
    }

    @Test
    fun `purchase errors map to what the client does about them`() {
        fun error(code: String, details: String = "") = ApiClient.mapServerError(
            400, """{"error":{"code":"$code","message":"x"${if (details.isEmpty()) "" else ",\"details\":{$details}"}}}"""
        )
        assertEquals(ApiError.PurchasesUnavailable, error("IAP_UNAVAILABLE"))
        assertEquals(ApiError.PurchaseInvalid("environment"), error("PURCHASE_INVALID", "\"reason\":\"environment\""))
        assertEquals(ApiError.PurchaseOtherAccount, error("PURCHASE_BELONGS_TO_ANOTHER_ACCOUNT"))
        assertEquals(ApiError.StoreUnavailable, error("STORE_UNAVAILABLE"))
    }

    private companion object {
        const val TOKEN = "6f1c3c4e-2b9a-4c3e-9f0e-6a7b8c9d0e1f"
        const val OTHER_TOKEN = "11111111-1111-4111-8111-111111111111"

        val USAGE = UsageDto(dailyLimit = 5, resetsAt = "2026-10-20T19:00:00Z", credits = 20, remainingTotal = 20)

        val CATALOG = IapCatalogDto(
            products = listOf(
                IapProductDto("pack_20", "pack", 20, 0, "play.pack_20", "20 ответов"),
                // Play does not know it: not offered.
                IapProductDto("pack_100", "pack", 100, 0, "play.pack_100", "100 ответов"),
                IapProductDto("pass_24h", "pass", 40, 24, "play.pass_24h", "Пропуск"),
                // No Play id, or a kind this build does not know: not offered.
                IapProductDto("ios_only", "pack", 5, 0, "", "iOS"),
                IapProductDto("future", "bundle", 5, 0, "play.pack_20", "?")
            ),
            accountToken = TOKEN
        )
    }
}
