package kz.yerek.aireply.billing

import android.app.Activity
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Deferred
import kotlinx.coroutines.async
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
    val price: String,
    /** A subscription's plan on the server. */
    val planId: String? = null,
    /** A subscription's period: `month` or `year`. */
    val billingPeriod: String? = null,
    val entitlementKind: String? = null,
    val dailyLimit: Int = 0,
    val monthlyLimit: Int = 0,
    /** The server's price and Play's differ: the sale waits until they agree. */
    val hasPriceMismatch: Boolean = false,
    /** A subscription whose replies stay with the account after it ends. */
    val creditsNeverExpire: Boolean = false
) {
    enum class Kind { PACK, PASS, SUBSCRIPTION }
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
    RETRY_LATER,
    /** Restore found paid purchases and the server confirmed them. */
    RESTORED,
    /** Restore found nothing to hand over. */
    NOTHING_TO_RESTORE
}

data class BillingState(
    val phase: Phase = Phase.IDLE,
    /** In the server's order. */
    val products: List<OfferedProduct> = emptyList(),
    /** The product whose sheet is open or whose purchase the server is checking. */
    val busyProductId: String? = null,
    /** Products with a payment Play holds as pending. */
    val pendingProductIds: Set<String> = emptySet(),
    val notice: PurchaseNotice? = null,
    /** «Восстановить покупки» is running. */
    val restoring: Boolean = false,
    /** Base, for an account on its fourth or fifth pack that has not turned it down. */
    val recommendation: IapRecommendationDto? = null
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
    val subscriptions: List<OfferedProduct> get() = products.filter { it.kind == OfferedProduct.Kind.SUBSCRIPTION }
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
 * prices, and only while purchases are on sale ([isOffered]). Handing over
 * what was already paid ([canDeliver]) goes on after sales close: a payment
 * Play held as pending may clear days later, and a purchase left unconsumed
 * is refunded by Play after 3 days.
 */
class BillingRepository(
    private val store: StoreClient,
    private val api: PurchaseApi,
    /** Signed in, and `features.iap_android` is true (or the debug catalogue): products and «Купить». */
    private val isOffered: () -> Boolean,
    /** Signed in, and the server knows Play purchases (`features.iap_android` sent, even false). */
    private val canDeliver: () -> Boolean,
    /** The balance the server sent with a grant. */
    private val onUsage: (UsageDto) -> Unit,
    /** A grant without a balance in the answer: read it again. */
    private val refreshUsage: suspend () -> Unit,
    /** A subscription was granted: the answer has the balance, not the plan, so the account is read again. */
    private val refreshAccount: suspend () -> Unit,
    private val recommendations: RecommendationStore,
    private val scope: CoroutineScope
) {

    private val _state = MutableStateFlow(BillingState())
    val state: StateFlow<BillingState> = _state.asStateFlow()

    @Volatile
    private var accountToken: String? = null

    /** What Play held the last time it was asked: a running subscription is what a new one replaces. */
    @Volatile
    private var held: List<StorePurchase> = emptyList()

    private val lock = Any()
    /** The request for each token on its way to the server: a second caller for one purchase waits for it. */
    private val inFlight = HashMap<Pair<Int, String>, Deferred<Delivery>>()
    /** Moves on with each [reset]: an account never reads the answer to another account's request. */
    private var accountGeneration = 0
    /** Tokens the server finished in this process (granted, or refused for good). */
    private val finished = HashSet<String>()

    init {
        store.setListener(::onStoreUpdate)
    }

    /** The purchase screen opened: the catalogue, then anything Play still holds. */
    suspend fun load() {
        if (!isOffered()) {
            _state.value = BillingState(phase = BillingState.Phase.OFF)
            // Nothing on sale, but what was paid meanwhile is still handed over.
            reconcile()
            return
        }
        _state.update { it.copy(phase = BillingState.Phase.LOADING, notice = null) }
        val catalog = try {
            api.catalog()
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (failure: ApiException) {
            if (failure.error is ApiError.PurchasesUnavailable) {
                closeSales()
            } else {
                _state.update { it.copy(phase = BillingState.Phase.UNAVAILABLE) }
            }
            return
        }
        accountToken = catalog.accountToken.ifBlank { null }
        val known = catalog.products.filter { product ->
            product.storeProductId.isNotBlank() && kindOf(product) != null && isSellable(product)
        }
        if (accountToken == null || !store.connect()) {
            _state.update { it.copy(phase = BillingState.Phase.UNAVAILABLE) }
            return
        }
        val prices = store.products(
            oneTime = known.filter { kindOf(it) != OfferedProduct.Kind.SUBSCRIPTION }.map { it.storeProductId }.distinct(),
            subscriptions = known.filter { kindOf(it) == OfferedProduct.Kind.SUBSCRIPTION }.map { it.storeProductId }.distinct()
        )
        // Known before anything can be tapped: a subscription bought without it runs alongside the current one.
        val current = prices?.let { store.purchases() }
        if (prices == null || current == null) {
            _state.update { it.copy(phase = BillingState.Phase.UNAVAILABLE) }
            return
        }
        held = current
        val byId = prices.associateBy { it.productId }
        val offered = known.mapNotNull { product -> offered(product, byId[product.storeProductId]) }
        val recommendation = catalog.recommendation?.takeIf { stillRecommended(it, catalog.accountToken) }
        _state.update { it.copy(phase = BillingState.Phase.READY, products = offered, recommendation = recommendation) }
        reconcile()
    }

    /** The server closed sales: nothing stays on offer, however recently it was loaded. */
    private fun closeSales() {
        accountToken = null
        _state.update { it.copy(phase = BillingState.Phase.OFF, products = emptyList(), recommendation = null) }
    }

    /** Only around the fourth and fifth pack, and not once this account has said no. */
    private fun stillRecommended(recommendation: IapRecommendationDto, account: String): Boolean =
        recommendation.purchaseCount in 4..5 && recommendation.spentMinor > 0 &&
            !recommendations.isRecommendationDismissed(account, recommendation.id)

    /** What the server sells that this build understands: anything else is not offered. */
    private fun isSellable(product: IapProductDto): Boolean = when (kindOf(product)) {
        OfferedProduct.Kind.SUBSCRIPTION -> !product.planId.isNullOrBlank() &&
            (product.billingPeriod == "month" || product.billingPeriod == "year") &&
            when (product.entitlementKind) {
                "unlimited" -> true
                "daily" -> product.dailyLimit > 0
                "monthly" -> product.monthlyLimit > 0
                else -> false
            }
        OfferedProduct.Kind.PASS -> product.units > 0 && product.durationHours > 0
        OfferedProduct.Kind.PACK -> product.units > 0
        null -> false
    }

    /** The server's terms and Play's price; a product Play does not price, or prices for another period, is not offered. */
    private fun offered(product: IapProductDto, store: StoreProduct?): OfferedProduct? {
        val price = store?.formattedPrice?.takeIf(String::isNotBlank) ?: return null
        val kind = kindOf(product) ?: return null
        if (kind == OfferedProduct.Kind.SUBSCRIPTION && store.billingPeriod != product.billingPeriod) return null
        return OfferedProduct(
            code = product.code,
            kind = kind,
            units = product.units,
            durationHours = product.durationHours.coerceAtLeast(0),
            storeProductId = product.storeProductId,
            name = product.name.ifBlank { product.code },
            price = price,
            planId = product.planId,
            billingPeriod = product.billingPeriod,
            entitlementKind = product.entitlementKind,
            dailyLimit = product.dailyLimit,
            monthlyLimit = product.monthlyLimit,
            hasPriceMismatch = hasPriceMismatch(product, store),
            creditsNeverExpire = product.creditsNeverExpire
        )
    }

    /** Compared only where both sides name the same currency: a buyer abroad pays in their own. */
    private fun hasPriceMismatch(product: IapProductDto, store: StoreProduct): Boolean {
        val server = product.price?.takeIf { it > 0 } ?: return false
        val micros = store.priceMicros ?: return false
        if (product.currency.isNullOrBlank() || product.currency != store.currency) return false
        return micros != server * MICROS_PER_MINOR
    }

    /** «Купить»: Play's sheet for [product]. The outcome arrives through the store's listener. */
    fun buy(activity: Activity, product: OfferedProduct) {
        val token = accountToken ?: return
        if (_state.value.busyProductId != null || product.hasPriceMismatch) return
        _state.update { it.copy(busyProductId = product.storeProductId, notice = null) }
        val replacing = if (product.kind == OfferedProduct.Kind.SUBSCRIPTION) {
            held.firstOrNull {
                it.isSubscription && product.storeProductId !in it.productIds && it.state == StorePurchaseState.PURCHASED &&
                    // Bought by another AI Reply account on this phone: not this one's to replace.
                    (it.accountToken == null || it.accountToken == token)
            }?.purchaseToken
        } else {
            null
        }
        store.launch(activity, product.storeProductId, token, replacing)?.let { onStoreUpdate(StoreUpdate.Failed(it)) }
    }

    /**
     * «Восстановить покупки»: everything Play holds goes to the server, and the
     * screen says what came of it. Purchases are also handed over by themselves
     * on every return to the app; this is the explicit way for someone who
     * expects to see it happen.
     */
    suspend fun restore() {
        if (!canDeliver() || _state.value.restoring) return
        _state.update { it.copy(restoring = true, notice = null) }
        try {
            if (!store.connect()) {
                _state.update { it.copy(notice = PurchaseNotice.UNAVAILABLE) }
                return
            }
            val purchases = store.purchases()
            if (purchases == null) {
                _state.update { it.copy(notice = PurchaseNotice.UNAVAILABLE) }
                return
            }
            held = purchases
            val paid = purchases.filter { it.state == StorePurchaseState.PURCHASED }
            synchronized(lock) { finished -= paid.map { it.purchaseToken }.toSet() }
            var restored = false
            paid.forEach { purchase -> if (verify(purchase, Origin.RESTORE)) restored = true }
            refreshUsage()
            _state.update { state ->
                state.copy(
                    notice = when {
                        restored -> PurchaseNotice.RESTORED
                        paid.isEmpty() -> PurchaseNotice.NOTHING_TO_RESTORE
                        else -> state.notice ?: PurchaseNotice.RETRY_LATER
                    }
                )
            }
        } finally {
            _state.update { it.copy(restoring = false) }
        }
    }

    /** Back in the app, or the flags read again: a purchase paid meanwhile, or never handed over, goes to the server now. */
    fun resume() {
        // Switched off while the plans are open: [load] does not run again for a screen already up.
        if (!isOffered() && _state.value.products.isNotEmpty()) closeSales()
        if (!canDeliver()) return
        scope.launch { reconcile() }
    }

    fun clearNotice() {
        _state.update { it.copy(notice = null) }
    }

    /** «Продолжить без подписки»: this account is not offered this one again. */
    fun dismissRecommendation() {
        val account = accountToken ?: return
        val id = _state.value.recommendation?.id ?: return
        recommendations.dismissRecommendation(account, id)
        _state.update { it.copy(recommendation = null) }
    }

    /** Another account may sign in next: nothing of this one's is kept. */
    fun reset() {
        accountToken = null
        held = emptyList()
        synchronized(lock) {
            finished.clear()
            accountGeneration++
        }
        _state.value = BillingState()
    }

    /**
     * Every unconsumed purchase Play holds for this account goes to the server;
     * pending ones are shown. Runs with sales closed too ([canDeliver]).
     * [tapped] is a product Play just refused as already owned: whoever tapped
     * hears what came of that one.
     */
    suspend fun reconcile(tapped: String? = null) {
        if (!canDeliver()) return
        if (!store.connect()) return
        val current = store.purchases() ?: return
        held = current
        _state.update { state ->
            state.copy(pendingProductIds = current.filter { it.state == StorePurchaseState.PENDING }.flatMap { it.productIds }.toSet())
        }
        val paid = current.filter { it.state == StorePurchaseState.PURCHASED }
        if (paid.isEmpty()) return
        // With sales closed the catalogue is refused, so the token stays unknown and
        // every purchase goes: the server refuses another account's and keeps nothing.
        if (accountToken == null && isOffered()) {
            accountToken = runCatching { api.catalog().accountToken.ifBlank { null } }
                .onFailure { if (it is CancellationException) throw it }
                .getOrNull()
        }
        val mine = accountToken
        paid
            // Bought by another AI Reply account on this phone: that account hands it over.
            .filter { mine == null || it.accountToken == null || it.accountToken == mine }
            .forEach { verify(it, if (tapped in it.productIds) Origin.SHEET else Origin.RETURN) }
    }

    internal fun onStoreUpdate(update: StoreUpdate) {
        when (update) {
            is StoreUpdate.Purchases -> {
                update.purchases.forEach { purchase ->
                    when (purchase.state) {
                        StorePurchaseState.PURCHASED -> scope.launch { verify(purchase, Origin.SHEET) }
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
                val tapped = _state.value.busyProductId
                _state.update {
                    it.copy(
                        busyProductId = null,
                        notice = when (update.error) {
                            // An earlier purchase of it is not finished yet: hand it over now.
                            // Another account's stays with that account, so nothing else would be said.
                            StoreError.ALREADY_OWNED -> if (heldByAnotherAccount(it.busyProductId)) PurchaseNotice.OTHER_ACCOUNT else it.notice
                            StoreError.UNAVAILABLE -> PurchaseNotice.UNAVAILABLE
                            StoreError.FAILED -> PurchaseNotice.FAILED
                        }
                    )
                }
                if (update.error == StoreError.ALREADY_OWNED) scope.launch { reconcile(tapped) }
            }
        }
    }

    private fun heldByAnotherAccount(productId: String?): Boolean {
        val mine = accountToken ?: return false
        return held.any { productId in it.productIds && it.accountToken != null && it.accountToken != mine }
    }

    /**
     * One purchase to the server, one request at a time: a caller that finds it
     * already on its way waits for that request and reads the same answer.
     * [origin] decides what that answer tells the screen.
     */
    private suspend fun verify(purchase: StorePurchase, origin: Origin): Boolean {
        val token = purchase.purchaseToken
        val productId = purchase.productIds.firstOrNull() ?: return false
        val request = synchronized(lock) {
            if (token in finished) return false
            val key = accountGeneration to token
            inFlight.getOrPut(key) { scope.async(start = CoroutineStart.LAZY) { deliver(purchase, productId, key) } }
        }
        var notice: PurchaseNotice? = null
        var granted = false
        try {
            val delivery = request.await()
            granted = delivery == Delivery.GRANTED || delivery == Delivery.ALREADY_GRANTED
            notice = noticeFor(delivery, purchase, origin)
            // Before the buttons come back: they are drawn from the plan.
            if (purchase.isSubscription && notice == PurchaseNotice.GRANTED) refreshAccount()
        } finally {
            _state.update { state ->
                state.copy(
                    busyProductId = if (state.busyProductId in purchase.productIds) null else state.busyProductId,
                    notice = notice ?: state.notice
                )
            }
        }
        return granted
    }

    /** What [delivery] tells this caller. Someone who just tapped hears all of it. */
    private fun noticeFor(delivery: Delivery, purchase: StorePurchase, origin: Origin): PurchaseNotice? = when {
        origin == Origin.SHEET -> delivery.notice
        // A repeat found on return, or by restore, is not news: restore sums up on its own.
        delivery == Delivery.ALREADY_GRANTED -> null
        // Restoring it will never work, and the one asking is told why.
        delivery == Delivery.ACCOUNT_MISMATCH && origin == Origin.RESTORE -> PurchaseNotice.OTHER_ACCOUNT
        // Found on return, bound to another account: not this one's to report.
        delivery == Delivery.ACCOUNT_MISMATCH -> null
        // A running subscription is sent on every return: one that could not be checked was not bought just now.
        delivery == Delivery.LATER && origin == Origin.RETURN && purchase.isSubscription -> null
        else -> delivery.notice
    }

    /** The one request for [purchase]. It finishes even if the caller that started it has gone. */
    private suspend fun deliver(purchase: StorePurchase, productId: String, key: Pair<Int, String>): Delivery {
        val token = purchase.purchaseToken
        try {
            val result = api.verify(productId, token)
            return when {
                result.isGranted -> {
                    // A running subscription is checked again on the next return: it can renew or end.
                    if (!purchase.isSubscription) synchronized(lock) { finished += token }
                    _state.update { it.copy(pendingProductIds = it.pendingProductIds - purchase.productIds.toSet()) }
                    val usage = result.usage?.takeIf { it.isKnown }
                    if (usage != null) onUsage(usage) else refreshUsage()
                    if (result.status == PurchaseResultDto.STATUS_GRANTED) Delivery.GRANTED else Delivery.ALREADY_GRANTED
                }
                result.status == PurchaseResultDto.STATUS_PENDING -> {
                    _state.update { it.copy(pendingProductIds = it.pendingProductIds + purchase.productIds) }
                    Delivery.PENDING
                }
                else -> Delivery.LATER
            }
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (failure: ApiException) {
            ReplyLog.warn(failure) { "purchase not granted (HTTP ${failure.httpStatus ?: "none"})" }
            return when (val error = failure.error) {
                is ApiError.PurchaseInvalid -> {
                    synchronized(lock) { finished += token }
                    if (error.reason == REASON_ACCOUNT) Delivery.ACCOUNT_MISMATCH else Delivery.REFUSED
                }
                ApiError.PurchaseOtherAccount -> {
                    synchronized(lock) { finished += token }
                    Delivery.OTHER_ACCOUNT
                }
                // Sales closed on the server: hide them; the purchase stays with Play, unconsumed.
                ApiError.PurchasesUnavailable -> {
                    closeSales()
                    Delivery.LATER
                }
                else -> Delivery.LATER
            }
        } finally {
            synchronized(lock) { inFlight -= key }
        }
    }

    private fun kindOf(product: IapProductDto): OfferedProduct.Kind? = when (product.kind) {
        "pack" -> OfferedProduct.Kind.PACK
        "pass" -> OfferedProduct.Kind.PASS
        "subscription" -> OfferedProduct.Kind.SUBSCRIPTION
        else -> null
    }

    /** Who is handing a purchase over. */
    private enum class Origin {
        /** Play's answer to a tap: the user just paid or already owns it, so even a repeat (`already_granted`) is news worth saying. */
        SHEET,
        /** The routine check on every return to the app. */
        RETURN,
        /** «Восстановить покупки»: someone is waiting to hear what came of it. */
        RESTORE
    }

    /** How handing one purchase to the server ended, and what someone who just paid is told. */
    private enum class Delivery(val notice: PurchaseNotice) {
        GRANTED(PurchaseNotice.GRANTED),
        ALREADY_GRANTED(PurchaseNotice.GRANTED),
        PENDING(PurchaseNotice.PENDING),
        /** The server could not finish it now; it is sent again on the next return. */
        LATER(PurchaseNotice.RETRY_LATER),
        REFUSED(PurchaseNotice.NOT_CONFIRMED),
        /** Play's obfuscated account id is not this account's token. */
        ACCOUNT_MISMATCH(PurchaseNotice.NOT_CONFIRMED),
        OTHER_ACCOUNT(PurchaseNotice.OTHER_ACCOUNT)
    }

    private companion object {
        /** `PURCHASE_INVALID` `details.reason`: Play's obfuscated account id is not this account's token. */
        const val REASON_ACCOUNT = "account_mismatch"

        /** One minor unit of a currency in Play's micros: 1 ₸ = 100 тиын = 1 000 000 micros. */
        const val MICROS_PER_MINOR = 10_000L
    }
}
