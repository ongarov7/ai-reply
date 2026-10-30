import Foundation
import Observation

/// This iOS product shows account access and balances without selling access.
/// Server feature flags cannot enable a storefront in this app.
enum IOSProductPolicy {
    static let allowsNewPurchases = false
    /// Explicit product choice for the internal prototype. It is not changed
    /// by remote flags, receipts, reviewer accounts or browser state.
    static let showsWebsiteAccountLink = true
}

/// A product the screen may offer: what the server sells, at the price the
/// App Store shows. Only products both know become one.
struct PurchaseOffer: Identifiable, Equatable, Sendable {

    enum Kind: String, Sendable {
        /// Replies that never expire.
        case pack
        /// Replies for a number of hours from the purchase.
        case pass
        case subscription
    }

    let code: String
    let kind: Kind
    let units: Int
    let durationHours: Int
    let storeProductID: String
    /// The server's name, in the account's language; the App Store's when
    /// the server has none.
    let name: String
    /// `Product.displayPrice`. Never from the server, never written in the app.
    let displayPrice: String
    var planID: String? = nil
    var billingPeriod: String? = nil
    var entitlementKind: String? = nil
    var dailyLimit = 0
    var hasPriceMismatch = false

    var id: String { storeProductID }

    /// The server's products that the App Store also knows, in the server's
    /// order. A product either side does not know is not offered.
    static func match(server: [PurchaseAPI.Product], store: [StoreProduct]) -> [PurchaseOffer] {
        let prices = Dictionary(store.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        return server.compactMap { product in
            guard let kind = Kind(rawValue: product.kind),
                  !product.storeProductID.isEmpty, let storeProduct = prices[product.storeProductID] else { return nil }
            if kind == .subscription {
                guard storeProduct.kind == .subscription, product.planID != nil,
                      product.billingPeriod == "month" || product.billingPeriod == "year",
                      storeProduct.billingPeriod == product.billingPeriod,
                      product.entitlementKind == "unlimited" || (product.entitlementKind == "daily" && product.dailyLimit > 0)
                else { return nil }
            } else {
                guard product.units > 0 else { return nil }
                if kind == .pack, storeProduct.kind != .consumable { return nil }
                if kind == .pass, storeProduct.kind != .consumable && storeProduct.kind != .nonRenewing { return nil }
            }
            if kind == .pass, product.durationHours <= 0 { return nil }
            let name = product.name.trimmingCharacters(in: .whitespacesAndNewlines)
            let mismatch: Bool
            if let amount = product.price, let currency = product.currency, let storePrice = storeProduct.price,
               let storeCurrency = storeProduct.currency, currency == storeCurrency {
                mismatch = Decimal(amount) / 100 != storePrice
            } else { mismatch = false }
            return PurchaseOffer(code: product.code, kind: kind, units: product.units,
                                 durationHours: product.durationHours, storeProductID: product.storeProductID,
                                 name: name.isEmpty ? storeProduct.displayName : name,
                                 displayPrice: storeProduct.displayPrice, planID: product.planID,
                                 billingPeriod: product.billingPeriod, entitlementKind: product.entitlementKind,
                                 dailyLimit: product.dailyLimit, hasPriceMismatch: mismatch)
        }
    }
}

/// Reply packs and the pass through StoreKit 2 (contract §3.8).
///
/// Жауап пакеттері мен рұқсаттама: App Store арқылы, растауы серверде.
///
/// - The catalogue is the server's products (only while `features.iap_ios`
///   is on) that `Product.products(for:)` also returns, at the App Store's
///   price.
/// - A purchase carries the account's `appAccountToken`; the verified
///   transaction goes to the server, and `finish()` is called only after
///   the server granted it (200), or said the store revoked it. Anything
///   else leaves it unfinished: it comes back (`Transaction.unfinished`) and
///   is sent again, which the server answers `already_granted` at worst. A
///   refusal is sent again at the next launch, not on every return to the
///   app.
/// - The same transaction is never sent twice at once: the sheet, the
///   update listener and a return to the app share one request and its
///   answer.
/// - `Transaction.updates` is followed from launch: Ask to Buy approvals
///   and purchases made elsewhere arrive there. Transactions that arrive
///   before an account can receive them wait.
@MainActor
@Observable
final class PurchaseStore {

    enum CatalogueState: Equatable {
        case idle, loading, loaded
        /// Nothing to sell right now: purchases closed, or no product both
        /// sides know.
        case unavailable
        case failed
    }

    enum Failure: Equatable {
        /// The App Store sheet failed, or its transaction did not verify.
        case store
        /// The server could not confirm the purchase with the App Store.
        case notConfirmed
        /// The purchase was granted to another account.
        case anotherAccount
        /// Paid, but the server has not heard yet; sent again automatically.
        case willRetry
        /// Purchases cannot be made for this account now.
        case unavailable
    }

    enum Status: Equatable {
        case idle
        case purchasing(productID: String)
        /// Paid; the server is confirming it.
        case verifying(productID: String)
        case restoring
        case restored
        case nothingToRestore
        case granted
        /// Waiting for approval, Ask to Buy for example.
        case pending
        case cancelled
        case failed(Failure)

        var isBusy: Bool {
            switch self {
            case .purchasing, .verifying, .restoring: return true
            default: return false
            }
        }
    }

    /// How sending one transaction to the server ended.
    enum Delivery: Equatable {
        case granted
        case failed(Failure)
    }

    private(set) var offers: [PurchaseOffer] = []
    private(set) var catalogueState: CatalogueState = .idle
    private(set) var status: Status = .idle
    /// Purchases are on for iOS (`features.iap_ios`) and an account is
    /// signed in. Off, nothing about purchases is shown.
    private(set) var isEnabled = false
    private(set) var recommendation: PurchaseAPI.Recommendation?

    /// A grant arrived: the new balance, or nil when the server could not
    /// read it (then the account is refreshed).
    @ObservationIgnored var onGrant: (@MainActor (AccountAPI.Usage?) -> Void)?

    @ObservationIgnored private let storeKit: StoreKitClient
    @ObservationIgnored private let backend: PurchaseBackend
    @ObservationIgnored private let preferences: UserDefaults
    @ObservationIgnored private let allowsNewPurchases: Bool
    @ObservationIgnored private var accountToken: UUID?
    @ObservationIgnored private var userID: String?
    /// Signed in, against a server that has purchases at all (the flag is
    /// published, on or off): a transaction can be sent. One bought while
    /// sales were on is still granted after they close.
    @ObservationIgnored private var canDeliver = false
    @ObservationIgnored private var waiting: [StoreTransaction] = []
    /// The request for each transaction being sent; a second send of the
    /// same one waits for it.
    @ObservationIgnored private var sending: [String: Task<Delivery, Never>] = [:]
    /// Refused by the server this launch: not sent again on each return to
    /// the app, only at the next launch (or for another account).
    @ObservationIgnored private var refused: Set<String> = []
    @ObservationIgnored private var updatesTask: Task<Void, Never>?
    @ObservationIgnored private var pendingProductID: String?

    init(storeKit: StoreKitClient = AppStoreClient(), backend: PurchaseBackend = BackendPurchaseService(),
         preferences: UserDefaults = .standard, allowsNewPurchases: Bool = IOSProductPolicy.allowsNewPurchases) {
        self.storeKit = storeKit
        self.backend = backend
        self.preferences = preferences
        #if DEBUG
        // Exercise legacy transaction outcomes in tests without enabling a UI.
        self.allowsNewPurchases = allowsNewPurchases
        #else
        self.allowsNewPurchases = IOSProductPolicy.allowsNewPurchases
        #endif
    }

    var canMakePayments: Bool { allowsNewPurchases && storeKit.canMakePayments }
    var canRestore: Bool { canDeliver && !status.isBusy }

    // MARK: Account

    func accountDidChange(isSignedIn: Bool, userID: String?, features: AccountAPI.Features?) {
        if userID != self.userID {
            // Another account: its token and its products are not this one's.
            self.userID = userID
            accountToken = nil
            refused = []
            offers = []
            catalogueState = .idle
            status = .idle
            pendingProductID = nil
            recommendation = nil
        }
        let enabled = allowsNewPurchases && isSignedIn && features?.iapIOS == true
        if enabled != isEnabled {
            isEnabled = enabled
            if !enabled {
                offers = []
                catalogueState = .idle
                recommendation = nil
            }
        }
        canDeliver = isSignedIn && features?.iapIOS != nil
        if canDeliver, !waiting.isEmpty {
            Task { await deliverWaiting() }
        }
    }

    // MARK: Transactions

    /// Starts following `Transaction.updates` and sends what is unfinished.
    /// Once, at launch.
    func start() {
        guard updatesTask == nil else { return }
        let updates = storeKit.transactionUpdates()
        updatesTask = Task { [weak self] in
            for await transaction in updates {
                await self?.received(transaction)
            }
        }
        Task { await processUnfinished() }
    }

    /// Every transaction the App Store still holds undelivered: at launch
    /// and on each return to the app. One the server refused this launch
    /// waits for the next.
    func processUnfinished() async {
        let unfinished = await storeKit.unfinishedTransactions()
        let entitlements = await storeKit.currentEntitlements()
        var seen = Set<String>()
        for transaction in unfinished + entitlements where !refused.contains(transaction.id) && seen.insert(transaction.id).inserted {
            await received(transaction)
        }
    }

    /// A transaction that arrived outside a purchase sheet. The sheet's own,
    /// if it comes here too, is told by the sheet: both share one request.
    func received(_ transaction: StoreTransaction) async {
        guard canDeliver else {
            if !waiting.contains(where: { $0.id == transaction.id }) { waiting.append(transaction) }
            return
        }
        let receivingUserID = userID
        let delivery = await deliver(transaction)
        guard userID == receivingUserID else { return }
        // An Ask to Buy approval.
        if delivery == .granted, status == .pending, transaction.productID == pendingProductID {
            status = .granted
            pendingProductID = nil
        }
    }

    private func deliverWaiting() async {
        let transactions = waiting
        waiting = []
        for transaction in transactions {
            await received(transaction)
        }
    }

    /// Sends one transaction and finishes it when the server says so. Sent
    /// again while the first request runs, it waits for that request's answer.
    func deliver(_ transaction: StoreTransaction) async -> Delivery {
        let deliveringUserID = userID
        let key = "\(deliveringUserID ?? ""):\(transaction.id)"
        if let inFlight = sending[key] { return await inFlight.value }
        let request = Task { () -> Delivery in
            let delivery = await submit(transaction, for: deliveringUserID)
            // Still on the main actor: gone before anyone reads the answer.
            sending[key] = nil
            return delivery
        }
        sending[key] = request
        return await request.value
    }

    private func submit(_ transaction: StoreTransaction, for submittingUserID: String?) async -> Delivery {
        guard userID == submittingUserID else { return .failed(.unavailable) }
        do {
            let verdict = try await backend.submit(signedTransaction: transaction.signedTransaction)
            guard verdict.isGranted else { return .failed(.willRetry) }
            if submittingUserID == userID { refused.remove(transaction.id) }
            await transaction.finish()
            if submittingUserID == userID { onGrant?(verdict.usage) }
            return .granted
        } catch APIError.purchaseInvalid(let reason) {
            ReplyLog.event("store: transaction refused: \(reason ?? "no reason")")
            if Self.finishesAfterRefusal(reason) {
                await transaction.finish()
            } else {
                if submittingUserID == userID { refused.insert(transaction.id) }
            }
            return .failed(.notConfirmed)
        } catch APIError.purchaseBelongsToAnotherAccount {
            if submittingUserID == userID { refused.insert(transaction.id) }
            return .failed(.anotherAccount)
        } catch {
            ReplyLog.event("store: transaction not confirmed yet: \(error)")
            return .failed(.willRetry)
        }
    }

    /// The one `PURCHASE_INVALID` reason after which a transaction is
    /// finished: the store refunded or revoked it, so there is nothing to
    /// grant, ever. Every other refusal can change. StoreKit hands the app
    /// only its own transactions, so `bundle_mismatch` means the server's
    /// bundle id is wrong, and `product_type` a product set up as another
    /// type: the paid transaction stays, and is sent again once that is
    /// fixed.
    nonisolated static func finishesAfterRefusal(_ reason: String?) -> Bool {
        reason == "revoked"
    }

    // MARK: Catalogue

    func loadCatalogue() async {
        guard isEnabled, catalogueState != .loading else { return }
        let loadingUserID = userID
        catalogueState = .loading
        do {
            let catalogue = try await backend.catalogue()
            guard userID == loadingUserID, isEnabled else { return }
            guard let token = UUID(uuidString: catalogue.accountToken) else {
                offers = []
                catalogueState = .unavailable
                return
            }
            accountToken = token
            let ids = catalogue.products.map(\.storeProductID).filter { !$0.isEmpty }
            let products = ids.isEmpty ? [] : try await storeKit.products(for: ids)
            guard userID == loadingUserID, isEnabled else { return }
            offers = PurchaseOffer.match(server: catalogue.products, store: products)
            recommendation = catalogue.recommendation.flatMap { value in
                guard (4...5).contains(value.purchaseCount), value.spentMinor > 0,
                      !preferences.bool(forKey: recommendationKey(value.id)) else { return nil }
                return value
            }
            catalogueState = offers.isEmpty ? .unavailable : .loaded
        } catch APIError.purchasesUnavailable {
            guard userID == loadingUserID, isEnabled else { return }
            offers = []
            catalogueState = .unavailable
            recommendation = nil
        } catch {
            guard userID == loadingUserID, isEnabled else { return }
            catalogueState = offers.isEmpty ? .failed : .loaded
        }
    }

    func dismissRecommendation() {
        guard let recommendation else { return }
        preferences.set(true, forKey: recommendationKey(recommendation.id))
        self.recommendation = nil
    }

    private func recommendationKey(_ id: String) -> String {
        "purchase.recommendation.dismissed.\(userID ?? "").\(id)"
    }

    // MARK: Purchase

    func purchase(_ offer: PurchaseOffer) async {
        guard allowsNewPurchases, isEnabled, !status.isBusy else { return }
        guard let accountToken, canMakePayments, !offer.hasPriceMismatch else {
            status = .failed(.unavailable)
            return
        }
        status = .purchasing(productID: offer.storeProductID)
        pendingProductID = nil
        let purchasingUserID = userID
        let result: StorePurchaseResult
        do {
            result = try await storeKit.purchase(productID: offer.storeProductID, appAccountToken: accountToken)
        } catch {
            guard userID == purchasingUserID else { return }
            ReplyLog.event("store: purchase failed: \(error)")
            status = .failed(.store)
            return
        }
        guard userID == purchasingUserID else { return }
        switch result {
        case .purchased(let transaction):
            status = .verifying(productID: offer.storeProductID)
            switch await deliver(transaction) {
            case .granted:              if userID == purchasingUserID { status = .granted }
            case .failed(let failure):  if userID == purchasingUserID { status = .failed(failure) }
            }
        case .pending:
            pendingProductID = offer.storeProductID
            status = .pending
        case .cancelled:
            status = .cancelled
        case .unverified:
            status = .failed(.store)
        }
    }

    /// Restores subscriptions through Apple and then confirms each signed
    /// transaction with the account server. Consumable balances come from /me.
    func restore() async {
        guard canRestore else { return }
        let restoringUserID = userID
        status = .restoring
        do {
            try await storeKit.sync()
            guard userID == restoringUserID else { return }
            let entitlements = await storeKit.currentEntitlements()
            let unfinished = await storeKit.unfinishedTransactions()
            let transactions = entitlements + unfinished
            var seen = Set<String>()
            var restored = false
            var failure: Failure?
            for transaction in transactions where seen.insert(transaction.id).inserted {
                guard userID == restoringUserID else { return }
                refused.remove(transaction.id)
                switch await deliver(transaction) {
                case .granted: restored = true
                case .failed(let reason): failure = reason
                }
            }
            guard userID == restoringUserID else { return }
            onGrant?(nil)
            status = failure.map(Status.failed) ?? (restored ? .restored : .nothingToRestore)
        } catch {
            guard userID == restoringUserID else { return }
            status = .failed(.store)
        }
    }

    /// The outcome was read; the screen shows the products again.
    func dismissStatus() {
        guard !status.isBusy else { return }
        status = .idle
        pendingProductID = nil
    }
}
