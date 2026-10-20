import Foundation
import Observation

/// A product the screen may offer: what the server sells, at the price the
/// App Store shows. Only products both know become one.
struct PurchaseOffer: Identifiable, Equatable, Sendable {

    enum Kind: String, Sendable {
        /// Replies that never expire.
        case pack
        /// Replies for a number of hours from the purchase.
        case pass
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

    var id: String { storeProductID }

    /// The server's products that the App Store also knows, in the server's
    /// order. A product either side does not know is not offered.
    static func match(server: [PurchaseAPI.Product], store: [StoreProduct]) -> [PurchaseOffer] {
        let prices = Dictionary(store.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        return server.compactMap { product in
            guard let kind = Kind(rawValue: product.kind), product.units > 0,
                  !product.storeProductID.isEmpty, let storeProduct = prices[product.storeProductID] else { return nil }
            if kind == .pass, product.durationHours <= 0 { return nil }
            let name = product.name.trimmingCharacters(in: .whitespacesAndNewlines)
            return PurchaseOffer(code: product.code, kind: kind, units: product.units,
                                 durationHours: product.durationHours, storeProductID: product.storeProductID,
                                 name: name.isEmpty ? storeProduct.displayName : name,
                                 displayPrice: storeProduct.displayPrice)
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
        case granted
        /// Waiting for approval, Ask to Buy for example.
        case pending
        case cancelled
        case failed(Failure)

        var isBusy: Bool {
            switch self {
            case .purchasing, .verifying: return true
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

    /// A grant arrived: the new balance, or nil when the server could not
    /// read it (then the account is refreshed).
    @ObservationIgnored var onGrant: (@MainActor (AccountAPI.Usage?) -> Void)?

    @ObservationIgnored private let storeKit: StoreKitClient
    @ObservationIgnored private let backend: PurchaseBackend
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

    init(storeKit: StoreKitClient = AppStoreClient(), backend: PurchaseBackend = BackendPurchaseService()) {
        self.storeKit = storeKit
        self.backend = backend
    }

    var canMakePayments: Bool { storeKit.canMakePayments }

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
        }
        let enabled = isSignedIn && features?.iapIOS == true
        if enabled != isEnabled {
            isEnabled = enabled
            if !enabled {
                offers = []
                catalogueState = .idle
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
        for transaction in await storeKit.unfinishedTransactions() where !refused.contains(transaction.id) {
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
        let delivery = await deliver(transaction)
        // An Ask to Buy approval.
        if delivery == .granted, status == .pending {
            status = .granted
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
        if let inFlight = sending[transaction.id] { return await inFlight.value }
        let request = Task { () -> Delivery in
            let delivery = await submit(transaction)
            // Still on the main actor: gone before anyone reads the answer.
            sending[transaction.id] = nil
            return delivery
        }
        sending[transaction.id] = request
        return await request.value
    }

    private func submit(_ transaction: StoreTransaction) async -> Delivery {
        do {
            let verdict = try await backend.submit(signedTransaction: transaction.signedTransaction)
            guard verdict.isGranted else { return .failed(.willRetry) }
            refused.remove(transaction.id)
            await transaction.finish()
            onGrant?(verdict.usage)
            return .granted
        } catch APIError.purchaseInvalid(let reason) {
            ReplyLog.event("store: transaction refused: \(reason ?? "no reason")")
            if Self.finishesAfterRefusal(reason) {
                await transaction.finish()
            } else {
                refused.insert(transaction.id)
            }
            return .failed(.notConfirmed)
        } catch APIError.purchaseBelongsToAnotherAccount {
            refused.insert(transaction.id)
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
        catalogueState = .loading
        do {
            let catalogue = try await backend.catalogue()
            guard let token = UUID(uuidString: catalogue.accountToken) else {
                offers = []
                catalogueState = .unavailable
                return
            }
            accountToken = token
            let ids = catalogue.products.map(\.storeProductID).filter { !$0.isEmpty }
            let products = ids.isEmpty ? [] : try await storeKit.products(for: ids)
            offers = PurchaseOffer.match(server: catalogue.products, store: products)
            catalogueState = offers.isEmpty ? .unavailable : .loaded
        } catch APIError.purchasesUnavailable {
            offers = []
            catalogueState = .unavailable
        } catch {
            catalogueState = offers.isEmpty ? .failed : .loaded
        }
    }

    // MARK: Purchase

    func purchase(_ offer: PurchaseOffer) async {
        guard isEnabled, !status.isBusy else { return }
        guard let accountToken else {
            status = .failed(.unavailable)
            return
        }
        status = .purchasing(productID: offer.storeProductID)
        let result: StorePurchaseResult
        do {
            result = try await storeKit.purchase(productID: offer.storeProductID, appAccountToken: accountToken)
        } catch {
            ReplyLog.event("store: purchase failed: \(error)")
            status = .failed(.store)
            return
        }
        switch result {
        case .purchased(let transaction):
            status = .verifying(productID: offer.storeProductID)
            switch await deliver(transaction) {
            case .granted:              status = .granted
            case .failed(let failure):  status = .failed(failure)
            }
        case .pending:
            status = .pending
        case .cancelled:
            status = .cancelled
        case .unverified:
            status = .failed(.store)
        }
    }

    /// The outcome was read; the screen shows the products again.
    func dismissStatus() {
        guard !status.isBusy else { return }
        status = .idle
    }
}
