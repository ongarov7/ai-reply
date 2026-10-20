import Foundation
import StoreKit

/// A product as the App Store describes it: its name and its price, in the
/// storefront's currency and format.
struct StoreProduct: Equatable, Sendable {
    let id: String
    let displayName: String
    let displayPrice: String
}

/// A verified App Store transaction, ready to be sent to the server.
///
/// `finish` tells the App Store it was delivered. It is called only after
/// the server granted it (or refused it for good): an unfinished transaction
/// comes back at every launch, which is how a grant that did not reach the
/// server is never lost.
struct StoreTransaction: Sendable {
    let id: String
    let productID: String
    /// `VerificationResult.jwsRepresentation`, which the server verifies itself.
    let signedTransaction: String
    let finish: @Sendable () async -> Void
}

/// How a purchase sheet ended.
enum StorePurchaseResult: Sendable {
    case purchased(StoreTransaction)
    /// Waiting for approval (Ask to Buy, a payment method to fix): the
    /// transaction arrives later through `transactionUpdates`.
    case pending
    case cancelled
    /// The App Store's own check of the transaction failed.
    case unverified
}

/// StoreKit behind a protocol, so the purchase flow is tested without the
/// App Store.
///
/// Тесттерде App Store-сыз жұмыс істеу үшін StoreKit хаттама артында.
protocol StoreKitClient: Sendable {
    /// Parental controls or a device policy can forbid purchases.
    var canMakePayments: Bool { get }
    func products(for ids: [String]) async throws -> [StoreProduct]
    func purchase(productID: String, appAccountToken: UUID) async throws -> StorePurchaseResult
    func unfinishedTransactions() async -> [StoreTransaction]
    func transactionUpdates() -> AsyncStream<StoreTransaction>
}

enum StoreKitClientError: Error, Equatable {
    case productUnavailable
}

/// The real App Store, through StoreKit 2.
struct AppStoreClient: StoreKitClient {

    var canMakePayments: Bool { AppStore.canMakePayments }

    func products(for ids: [String]) async throws -> [StoreProduct] {
        try await Product.products(for: ids).map {
            StoreProduct(id: $0.id, displayName: $0.displayName, displayPrice: $0.displayPrice)
        }
    }

    func purchase(productID: String, appAccountToken: UUID) async throws -> StorePurchaseResult {
        guard let product = try await Product.products(for: [productID]).first else {
            throw StoreKitClientError.productUnavailable
        }
        switch try await product.purchase(options: [.appAccountToken(appAccountToken)]) {
        case .success(let verification):
            return Self.transaction(from: verification).map(StorePurchaseResult.purchased) ?? .unverified
        case .pending:
            return .pending
        case .userCancelled:
            return .cancelled
        @unknown default:
            return .cancelled
        }
    }

    func unfinishedTransactions() async -> [StoreTransaction] {
        var transactions: [StoreTransaction] = []
        for await verification in Transaction.unfinished {
            if let transaction = Self.transaction(from: verification) { transactions.append(transaction) }
        }
        return transactions
    }

    func transactionUpdates() -> AsyncStream<StoreTransaction> {
        AsyncStream { continuation in
            let task = Task.detached(priority: .utility) {
                for await verification in Transaction.updates {
                    if let transaction = Self.transaction(from: verification) { continuation.yield(transaction) }
                }
                continuation.finish()
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }

    /// Only a transaction the App Store verified goes on to the server; an
    /// unverified one is never granted and left alone.
    private static func transaction(from verification: VerificationResult<Transaction>) -> StoreTransaction? {
        guard case .verified(let transaction) = verification else {
            ReplyLog.event("store: an unverified transaction was ignored")
            return nil
        }
        return StoreTransaction(
            id: String(transaction.id),
            productID: transaction.productID,
            signedTransaction: verification.jwsRepresentation,
            finish: { await transaction.finish() }
        )
    }
}
