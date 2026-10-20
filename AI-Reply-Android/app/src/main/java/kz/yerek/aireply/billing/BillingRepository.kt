package kz.yerek.aireply.billing

import android.app.Activity
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.ApiException
import kz.yerek.aireply.data.account.UsageDto
import kz.yerek.aireply.platform.ReplyLog

/** A product both sides know: the server's terms and Play's price. */
data class OfferedProduct(
    val code: String,
    val kind: Kind,
    val units: Int,
    /** A pass's length; 0 for a pack. */
    val durationHours: Int,
    val storeProductId: String,
    /** The server's name, in the account's language. */
    val name: String,
    /** Play's formatted price, the only price ever shown. */
    val price: String
) {
    enum class Kind { PACK, PASS }
}

/** What the purchase screen says after something happened. Only a real failure is an error. */
enum class PurchaseNotice {
    /** The server granted the replies. */
    GRANTED,
    /** Play holds the payment as pending; nothing is granted until it clears. */
    PENDING,
    CANCELLED,
    /** Play could not complete the purchase. */
    FAILED,
    /** Play is not reachable on this phone right now. */
    UNAVAILABLE,
    /** The server checked with Google and refused it: nothing was granted. */
    NOT_CONFIRMED,
    /** The purchase was granted to another AI Reply account. */
    OTHER_ACCOUNT,
    /** Paid, but the server could not finish it now; it is sent again on the next return. */
    RETRY_LATER
}

data class BillingState(
    val phase: Phase = Phase.IDLE,
    /** In the server's order. */
    val products: List<OfferedProduct> = emptyList(),
    /** The product whose sheet is open or whose purchase the server is checking. */
    val busyProductId: String? = null,
    /** Products with a payment Play holds as pending. */
    val pendingProductIds: Set<String> = emptySet(),
    val notice: PurchaseNotice? = null
) {
    enum class Phase {
        IDLE,
        /** Purchases are switched off (`features.iap_android`), or the server closed them: nothing is shown. */
        OFF,
        LOADING,
        READY,
        /** The server or Play could not be reached; Retry may help. */
        UNAVAILABLE
    }

    val packs: List<OfferedProduct> get() = products.filter { it.kind == OfferedProduct.Kind.PACK }
    val passes: List<OfferedProduct> get() = products.filter { it.kind == OfferedProduct.Kind.PASS }
}

/**
 * Buying replies through Google Play (contract §3.8).
 *
 * Жауап сатып алу: өнімдер серверден, баға Google Play-ден, начисление серверде.
 *
 * The server says what is on sale and binds every purchase to the account
 * (`account_token` as Play's obfuscated account id); Play says what it costs
 * and takes the payment; the server verifies the purchase token with Google,
 * grants the replies and consumes the purchase. This class never consumes or
 * acknowledges anything, so a crash between paying and granting loses
 * nothing: Play keeps the purchase, and [resume] - on every return to the app
 * - hands it to the server again, which answers `already_granted` for one it
 * has seen.
 *
 * Shown only where both sides agree: a product the server lists and Play
 * prices. Nothing here runs while purchases are off ([isEnabled]).
 */
class BillingRepository(
    private val store: StoreClient,
    private val api: PurchaseApi,
    /** Signed in, and `features.iap_android` (or the debug catalogue). */
    private val isEnabled: () -> Boolean,
    /** The balance the server sent with a grant. */
    private val onUsage: (UsageDto) -> Unit,
    /** A grant without a balance in the answer: read it again. */
    private val refreshUsage: suspend () -> Unit,
    private val scope: CoroutineScope
) {

    private val _state = MutableStateFlow(BillingState())
    val state: StateFlow<BillingState> = _state.asStateFlow()

    @Volatile
    private var accountToken: String? = null

    private val lock = Any()
    /** Tokens on their way to the server: a second callback for one purchase sends nothing. */
    private val inFlight = HashSet<String>()
    /** Tokens the server finished in this process (granted, or refused for good). */
    private val finished = HashSet<String>()

    init {
        store.setListener(::onStoreUpdate)
    }

    /** The purchase screen opened: the catalogue, then anything Play still holds. */
    suspend fun load() {
        if (!isEnabled()) {
            _state.value = BillingState(phase = BillingState.Phase.OFF)
            return
        }
        _state.update { it.copy(phase = BillingState.Phase.LOADING, notice = null) }
        val catalog = try {
            api.catalog()
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (failure: ApiException) {
            val closed = failure.error is ApiError.PurchasesUnavailable
            _state.update { it.copy(phase = if (closed) BillingState.Phase.OFF else BillingState.Phase.UNAVAILABLE) }
            return
        }
        accountToken = catalog.accountToken.ifBlank { null }
        val known = catalog.products.filter { product ->
            product.storeProductId.isNotBlank() && product.units > 0 && kindOf(product) != null
        }
        if (accountToken == null || !store.connect()) {
            _state.update { it.copy(phase = BillingState.Phase.UNAVAILABLE) }
            return
        }
        val prices = store.products(known.map { it.storeProductId }.distinct())
        if (prices == null) {
            _state.update { it.copy(phase = BillingState.Phase.UNAVAILABLE) }
            return
        }
        val byId = prices.associateBy { it.productId }
        val offered = known.mapNotNull { product ->
            val price = byId[product.storeProductId]?.formattedPrice?.takeIf(String::isNotBlank) ?: return@mapNotNull null
            OfferedProduct(
                code = product.code,
                kind = kindOf(product) ?: return@mapNotNull null,
                units = product.units,
                durationHours = product.durationHours.coerceAtLeast(0),
                storeProductId = product.storeProductId,
                name = product.name.ifBlank { product.code },
                price = price
            )
        }
        _state.update { it.copy(phase = BillingState.Phase.READY, products = offered) }
        reconcile()
    }

    /** «Купить»: Play's sheet for [product]. The outcome arrives through the store's listener. */
    fun buy(activity: Activity, product: OfferedProduct) {
        val token = accountToken ?: return
        if (_state.value.busyProductId != null) return
        _state.update { it.copy(busyProductId = product.storeProductId, notice = null) }
        store.launch(activity, product.storeProductId, token)?.let { onStoreUpdate(StoreUpdate.Failed(it)) }
    }

    /** Back in the app: a purchase paid meanwhile, or never handed over, goes to the server now. */
    fun resume() {
        if (!isEnabled()) return
        scope.launch { reconcile() }
    }

    fun clearNotice() {
        _state.update { it.copy(notice = null) }
    }

    /** Another account may sign in next: nothing of this one's is kept. */
    fun reset() {
        accountToken = null
        synchronized(lock) { finished.clear() }
        _state.value = BillingState()
    }

    /** Every unconsumed purchase Play holds for this account goes to the server; pending ones are shown. */
    suspend fun reconcile() {
        if (!isEnabled()) return
        if (accountToken == null) {
            accountToken = runCatching { api.catalog().accountToken.ifBlank { null } }
                .onFailure { if (it is CancellationException) throw it }
                .getOrNull()
        }
        if (!store.connect()) return
        val held = store.purchases() ?: return
        _state.update { state ->
            state.copy(pendingProductIds = held.filter { it.state == StorePurchaseState.PENDING }.flatMap { it.productIds }.toSet())
        }
        val mine = accountToken
        held.filter { it.state == StorePurchaseState.PURCHASED }
            // Bought by another AI Reply account on this phone: that account hands it over.
            .filter { mine == null || it.accountToken == null || it.accountToken == mine }
            .forEach { verify(it, fromSheet = false) }
    }

    internal fun onStoreUpdate(update: StoreUpdate) {
        when (update) {
            is StoreUpdate.Purchases -> {
                update.purchases.forEach { purchase ->
                    when (purchase.state) {
                        StorePurchaseState.PURCHASED -> scope.launch { verify(purchase, fromSheet = true) }
                        StorePurchaseState.PENDING -> _state.update {
                            it.copy(
                                busyProductId = null,
                                pendingProductIds = it.pendingProductIds + purchase.productIds,
                                notice = PurchaseNotice.PENDING
                            )
                        }
                        StorePurchaseState.UNKNOWN -> Unit
                    }
                }
                if (update.purchases.none { it.state == StorePurchaseState.PURCHASED }) {
                    _state.update { it.copy(busyProductId = null) }
                }
            }
            StoreUpdate.Cancelled -> _state.update { it.copy(busyProductId = null, notice = PurchaseNotice.CANCELLED) }
            is StoreUpdate.Failed -> {
                _state.update {
                    it.copy(
                        busyProductId = null,
                        notice = when (update.error) {
                            // An earlier purchase of it is not finished yet: hand it over now.
                            StoreError.ALREADY_OWNED -> it.notice
                            StoreError.UNAVAILABLE -> PurchaseNotice.UNAVAILABLE
                            StoreError.FAILED -> PurchaseNotice.FAILED
                        }
                    )
                }
                if (update.error == StoreError.ALREADY_OWNED) scope.launch { reconcile() }
            }
        }
    }

    /**
     * One purchase to the server, once at a time. [fromSheet]: the user just
     * paid, so even a repeat (`already_granted`) is news worth saying.
     */
    private suspend fun verify(purchase: StorePurchase, fromSheet: Boolean) {
        val token = purchase.purchaseToken
        val productId = purchase.productIds.firstOrNull() ?: return
        synchronized(lock) {
            if (token in finished || !inFlight.add(token)) return
        }
        var notice: PurchaseNotice? = null
        try {
            val result = api.verify(productId, token)
            when {
                result.isGranted -> {
                    synchronized(lock) { finished += token }
                    _state.update { it.copy(pendingProductIds = it.pendingProductIds - purchase.productIds.toSet()) }
                    val usage = result.usage?.takeIf { it.isKnown }
                    if (usage != null) onUsage(usage) else refreshUsage()
                    if (fromSheet || result.status == PurchaseResultDto.STATUS_GRANTED) notice = PurchaseNotice.GRANTED
                }
                result.status == PurchaseResultDto.STATUS_PENDING -> {
                    _state.update { it.copy(pendingProductIds = it.pendingProductIds + purchase.productIds) }
                    notice = PurchaseNotice.PENDING
                }
                else -> notice = PurchaseNotice.RETRY_LATER
            }
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (failure: ApiException) {
            ReplyLog.warn(failure) { "purchase not granted (HTTP ${failure.httpStatus ?: "none"})" }
            notice = when (failure.error) {
                is ApiError.PurchaseInvalid -> {
                    synchronized(lock) { finished += token }
                    PurchaseNotice.NOT_CONFIRMED
                }
                ApiError.PurchaseOtherAccount -> {
                    synchronized(lock) { finished += token }
                    PurchaseNotice.OTHER_ACCOUNT
                }
                // Sales closed on the server: hide them; the purchase stays with Play, unconsumed.
                ApiError.PurchasesUnavailable -> {
                    _state.update { it.copy(phase = BillingState.Phase.OFF) }
                    PurchaseNotice.RETRY_LATER
                }
                else -> PurchaseNotice.RETRY_LATER
            }
        } finally {
            synchronized(lock) { inFlight -= token }
            _state.update { state ->
                state.copy(
                    busyProductId = if (state.busyProductId in purchase.productIds) null else state.busyProductId,
                    notice = notice ?: state.notice
                )
            }
        }
    }

    private fun kindOf(product: IapProductDto): OfferedProduct.Kind? = when (product.kind) {
        "pack" -> OfferedProduct.Kind.PACK
        "pass" -> OfferedProduct.Kind.PASS
        else -> null
    }
}
