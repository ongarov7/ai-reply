import XCTest
@testable import AIReply

final class SubscriptionDisplayTests: XCTestCase {
    private func plan(_ code: String, days: Int, kind: String, limit: Int = 0, free: Bool = false) -> AccountAPI.Plan {
        AccountAPI.Plan(id: code, code: code, name: ["en": code.hasPrefix("base") ? "Base" : "Pro"],
                        description: [:], price: 0, priceText: "", currency: "KZT",
                        dailyLimit: limit, monthlyLimit: 0, periodDays: days, isFree: free,
                        entitlementKind: kind)
    }

    private var plans: [AccountAPI.Plan] {
        [plan("free", days: 0, kind: "welcome", free: true),
         plan("base_monthly", days: 30, kind: "daily", limit: 30),
         plan("base_yearly", days: 365, kind: "daily", limit: 30),
         plan("pro_monthly", days: 30, kind: "unlimited"),
         plan("pro_yearly", days: 365, kind: "unlimited")]
    }

    func testUnmappedPlansRemainReadableWithoutDuplicatePeriods() {
        let catalogue = SubscriptionPlanCatalogue(plans: plans, offers: [])
        XCTAssertEqual(catalogue.freePlan?.code, "free")
        XCTAssertEqual(catalogue.periods, ["month", "year"])
        XCTAssertEqual(catalogue.visible(period: "month").map(\.planID), ["base_monthly", "pro_monthly"])
        XCTAssertEqual(catalogue.visible(period: "year").map(\.planID), ["base_yearly", "pro_yearly"])
        XCTAssertEqual(catalogue.visible(period: "month").map(\.allowance), [.daily(30), .unlimited])
        XCTAssertTrue(catalogue.options.allSatisfy { $0.offer == nil }, "information alone cannot open checkout")
    }

    func testMissingSelectedPeriodFallsBackToOnePeriod() {
        let yearly = plans.filter { $0.periodDays == 365 }
        let catalogue = SubscriptionPlanCatalogue(plans: yearly, offers: [])
        XCTAssertEqual(catalogue.visible(period: "month").map(\.planID), ["base_yearly", "pro_yearly"])
        XCTAssertTrue(catalogue.visible(period: "month").allSatisfy { $0.billingPeriod == "year" })
    }

    func testMatchedStoreOfferReplacesInformationWithoutLegacyCheckoutFlag() throws {
        let offer = PurchaseOffer(code: "base_monthly", kind: .subscription, units: 0, durationHours: 0,
                                  storeProductID: "base.monthly", name: "Base", displayPrice: "599 ₸",
                                  planID: "base_monthly", billingPeriod: "month", entitlementKind: "daily", dailyLimit: 30)
        XCTAssertFalse(plans[1].purchasable, "the legacy checkout flag does not govern StoreKit")
        let catalogue = SubscriptionPlanCatalogue(plans: plans, offers: [offer])
        XCTAssertEqual(catalogue.options.count, 4)
        let monthlyBase = try XCTUnwrap(catalogue.visible(period: "month").first)
        XCTAssertEqual(monthlyBase.id, offer.id)
        XCTAssertEqual(monthlyBase.offer?.displayPrice, "599 ₸")
    }

    func testZeroDailyLimitNeverBecomesZeroRepliesPerDay() {
        let invalid = plan("unknown_monthly", days: 30, kind: "daily")
        let catalogue = SubscriptionPlanCatalogue(plans: [invalid], offers: [])
        XCTAssertEqual(catalogue.options.first?.allowance, .unspecified)
    }

    func testStoreOnlySubscriptionsRemainAvailableWhilePlansAreLoading() {
        let offer = PurchaseOffer(code: "pro_yearly", kind: .subscription, units: 0, durationHours: 0,
                                  storeProductID: "pro.yearly", name: "Pro", displayPrice: "29 990 ₸",
                                  planID: "pro_yearly", billingPeriod: "year", entitlementKind: "unlimited")
        let catalogue = SubscriptionPlanCatalogue(plans: [], offers: [offer])
        XCTAssertEqual(catalogue.visible(period: "month").first?.offer, offer)
        XCTAssertEqual(catalogue.options.first?.allowance, .unlimited)
    }
}

/// Packs and the pass through StoreKit 2, with the App Store and the server
/// both imitated: what is offered, when a transaction is finished, and how
/// every outcome is told (contract §3.8).
///
/// Сатып алу: App Store мен сервер жалған, ағын толық тексеріледі.
@MainActor
final class PurchaseStoreTests: XCTestCase {

    // MARK: Fakes

    private final class FakeStoreKit: StoreKitClient, @unchecked Sendable {
        var canMakePayments = true
        var products: [StoreProduct] = []
        var result: Result<StorePurchaseResult, Error> = .success(.cancelled)
        var unfinished: [StoreTransaction] = []
        var entitlements: [StoreTransaction] = []
        var syncError: Error?
        private(set) var syncCount = 0
        private(set) var tokens: [UUID] = []
        private(set) var requestedProducts: [[String]] = []

        func products(for ids: [String]) async throws -> [StoreProduct] {
            requestedProducts.append(ids)
            return products.filter { ids.contains($0.id) }
        }

        func purchase(productID: String, appAccountToken: UUID) async throws -> StorePurchaseResult {
            tokens.append(appAccountToken)
            return try result.get()
        }

        func sync() async throws {
            syncCount += 1
            if let syncError { throw syncError }
        }
        func currentEntitlements() async -> [StoreTransaction] { entitlements }
        func unfinishedTransactions() async -> [StoreTransaction] { unfinished }

        func transactionUpdates() -> AsyncStream<StoreTransaction> { AsyncStream { $0.finish() } }
    }

    private final class FakeBackend: PurchaseBackend, @unchecked Sendable {
        var catalogueResult: Result<PurchaseAPI.Catalogue, APIError>
        var verdicts: [Result<PurchaseAPI.Verdict, APIError>] = []
        /// While true, every submit waits before it answers.
        var holdsAnswers = false
        private(set) var submitted: [String] = []
        private(set) var catalogueCalls = 0

        init(catalogue: Result<PurchaseAPI.Catalogue, APIError>) {
            catalogueResult = catalogue
        }

        func catalogue() async throws -> PurchaseAPI.Catalogue {
            catalogueCalls += 1
            return try catalogueResult.get()
        }

        func submit(signedTransaction: String) async throws -> PurchaseAPI.Verdict {
            submitted.append(signedTransaction)
            let answer = verdicts.isEmpty ? .success(PurchaseAPI.Verdict(status: "granted", usage: nil)) : verdicts.removeFirst()
            while holdsAnswers { try await Task.sleep(for: .milliseconds(5)) }
            return try answer.get()
        }
    }

    /// Which transactions were finished, in order.
    private final class FinishLog: @unchecked Sendable {
        private(set) var finished: [String] = []
        func finish(_ id: String) { finished.append(id) }
    }

    private let token = "0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d"

    private var serverProducts: [PurchaseAPI.Product] {
        [
            PurchaseAPI.Product(code: "pack_50", kind: "pack", units: 50, durationHours: 0,
                                storeProductID: "pack.50", name: "50 ответов"),
            PurchaseAPI.Product(code: "pack_200", kind: "pack", units: 200, durationHours: 0,
                                storeProductID: "pack.200", name: "200 ответов"),
            PurchaseAPI.Product(code: "pass_24h", kind: "pass", units: 100, durationHours: 24,
                                storeProductID: "pass.24h", name: ""),
            PurchaseAPI.Product(code: "gift", kind: "gift", units: 10, durationHours: 0,
                                storeProductID: "gift.10", name: "Gift")
        ]
    }

    private func features(_ json: String) -> AccountAPI.Features? {
        try? JSONDecoder().decode(AccountAPI.Features.self, from: Data(json.utf8))
    }

    private func transaction(_ id: String, log: FinishLog) -> StoreTransaction {
        StoreTransaction(id: id, productID: "pack.50", signedTransaction: "jws-\(id)", finish: { log.finish(id) })
    }

    private func makeStore(storeKit: FakeStoreKit = FakeStoreKit(),
                           backend: FakeBackend? = nil,
                           iap: String = #"{"iap_ios": true}"#,
                           preferences: UserDefaults = .standard) -> (PurchaseStore, FakeStoreKit, FakeBackend) {
        let backend = backend ?? FakeBackend(catalogue: .success(PurchaseAPI.Catalogue(products: serverProducts,
                                                                                         accountToken: token)))
        storeKit.products = [
            StoreProduct(id: "pack.50", displayName: "Pack 50", displayPrice: "990 ₸"),
            StoreProduct(id: "pass.24h", displayName: "Pass 24 h", displayPrice: "590 ₸"),
            StoreProduct(id: "gift.10", displayName: "Gift", displayPrice: "1 ₸"),
            StoreProduct(id: "unknown", displayName: "Unknown", displayPrice: "5 ₸")
        ]
        let store = PurchaseStore(storeKit: storeKit, backend: backend, preferences: preferences)
        store.accountDidChange(isSignedIn: true, userID: "u1", features: features(iap))
        return (store, storeKit, backend)
    }

    // MARK: Catalogue

    func testNativeSalesUseBuildPolicyAndStillRequireBackendAvailability() async {
        let apple = FakeStoreKit()
        let backend = FakeBackend(catalogue: .success(PurchaseAPI.Catalogue(products: serverProducts, accountToken: token)))
        let store = PurchaseStore(storeKit: apple, backend: backend)
        store.accountDidChange(isSignedIn: true, userID: "u1", features: features(#"{"iap_ios": true}"#))
        XCTAssertTrue(IOSProductPolicy.allowsNewPurchases)
        XCTAssertTrue(store.isEnabled)
        XCTAssertTrue(store.canMakePayments)
        store.accountDidChange(isSignedIn: true, userID: "u1", features: features(#"{"iap_ios": false}"#))
        XCTAssertFalse(store.isEnabled)
        await store.loadCatalogue()
        let offer = PurchaseOffer(code: "pack30", kind: .pack, units: 30, durationHours: 0,
                                  storeProductID: "kz.aireply.pack30", name: "30 replies", displayPrice: "100 ₸")
        await store.purchase(offer)
        XCTAssertTrue(store.offers.isEmpty)
        XCTAssertTrue(apple.requestedProducts.isEmpty)
        XCTAssertTrue(apple.tokens.isEmpty, "a forged offer cannot call StoreKit purchase")
        XCTAssertEqual(backend.catalogueCalls, 0)
        XCTAssertTrue(backend.submitted.isEmpty)
        XCTAssertEqual(store.status, .idle)
    }

    func testProductWithoutSalesStillConfirmsGenuineUnfinishedTransactions() async {
        let apple = FakeStoreKit()
        let log = FinishLog()
        apple.unfinished = [transaction("previous-purchase", log: log)]
        let backend = FakeBackend(catalogue: .failure(.purchasesUnavailable))
        let store = PurchaseStore(storeKit: apple, backend: backend, allowsNewPurchases: false)
        store.accountDidChange(isSignedIn: true, userID: "u1", features: features(#"{"iap_ios": true}"#))
        await store.processUnfinished()
        XCTAssertFalse(store.isEnabled)
        XCTAssertEqual(backend.submitted, ["jws-previous-purchase"])
        XCTAssertEqual(log.finished, ["previous-purchase"])
        XCTAssertEqual(backend.catalogueCalls, 0)
        XCTAssertTrue(apple.tokens.isEmpty)
    }

    func testSignOutInvalidatesLoadedOffersBeforeAnotherPurchaseCanStart() async throws {
        let (store, apple, backend) = makeStore()
        await store.loadCatalogue()
        let oldOffer = try XCTUnwrap(store.offers.first)
        store.accountDidChange(isSignedIn: false, userID: nil, features: features(#"{"iap_ios": true}"#))
        await store.purchase(oldOffer)
        XCTAssertFalse(store.isEnabled)
        XCTAssertTrue(store.offers.isEmpty)
        XCTAssertTrue(apple.tokens.isEmpty, "the cached signed-in screen cannot start a purchase after logout begins")
        XCTAssertTrue(backend.submitted.isEmpty)
    }

    func testTransactionArrivingDuringLogoutWaitsForTheAccountToReturn() async throws {
        let (store, _, backend) = makeStore()
        let log = FinishLog()
        store.accountDidChange(isSignedIn: false, userID: nil, features: features(#"{"iap_ios": true}"#))
        await store.received(transaction("logout-waiting", log: log))
        XCTAssertTrue(backend.submitted.isEmpty)
        XCTAssertTrue(log.finished.isEmpty)
        store.accountDidChange(isSignedIn: true, userID: "u1", features: features(#"{"iap_ios": true}"#))
        for _ in 0..<100 where log.finished.isEmpty { try await Task.sleep(for: .milliseconds(5)) }
        XCTAssertEqual(backend.submitted, ["jws-logout-waiting"])
        XCTAssertEqual(log.finished, ["logout-waiting"])
    }

    func testServerGrantForABeforeLogoutFinishesWithoutUpdatingLoggedOutUI() async throws {
        let (store, _, backend) = makeStore()
        let log = FinishLog()
        var grants = 0
        store.onGrant = { _ in grants += 1 }
        backend.holdsAnswers = true
        let delivery = Task { await store.received(transaction("logout-granted", log: log)) }
        for _ in 0..<100 where backend.submitted.isEmpty { try await Task.sleep(for: .milliseconds(5)) }
        XCTAssertEqual(backend.submitted, ["jws-logout-granted"])
        store.accountDidChange(isSignedIn: false, userID: nil, features: features(#"{"iap_ios": true}"#))
        backend.holdsAnswers = false
        await delivery.value
        XCTAssertEqual(log.finished, ["logout-granted"], "the server already credited A before StoreKit finish")
        XCTAssertEqual(grants, 0, "the old grant must not populate a logged-out or different account UI")
    }

    func testNothingIsShownOrAskedWhilePurchasesAreOff() async {
        let (closed, _, backend) = makeStore(iap: #"{"iap_ios": false}"#)
        XCTAssertFalse(closed.isEnabled)
        await closed.loadCatalogue()
        XCTAssertEqual(backend.catalogueCalls, 0)
        XCTAssertTrue(closed.offers.isEmpty)

        let (older, _, olderBackend) = makeStore(iap: "{}")
        XCTAssertFalse(older.isEnabled, "a server without purchases")
        await older.loadCatalogue()
        XCTAssertEqual(olderBackend.catalogueCalls, 0)

        let (signedOut, _, _) = makeStore()
        signedOut.accountDidChange(isSignedIn: false, userID: nil, features: features(#"{"iap_ios": true}"#))
        XCTAssertFalse(signedOut.isEnabled)
    }

    /// Only what both the server and the App Store know, in the server's
    /// order, at the App Store's price.
    func testOnlyProductsBothSidesKnowAreOffered() async {
        let (store, storeKit, _) = makeStore()
        await store.loadCatalogue()
        XCTAssertEqual(storeKit.requestedProducts, [["pack.50", "pack.200", "pass.24h", "gift.10"]])
        XCTAssertEqual(store.offers.map(\.storeProductID), ["pack.50", "pass.24h"])
        XCTAssertEqual(store.offers.map(\.displayPrice), ["990 ₸", "590 ₸"])
        XCTAssertEqual(store.offers.first?.name, "50 ответов", "the server's name")
        XCTAssertEqual(store.offers.last?.name, "Pass 24 h", "the App Store's when the server has none")
        XCTAssertEqual(store.offers.last?.kind, .pass)
        XCTAssertEqual(store.offers.last?.durationHours, 24)
        XCTAssertEqual(store.catalogueState, .loaded)
    }

    func testClosedSalesOrABadTokenOfferNothing() async {
        let closed = FakeBackend(catalogue: .failure(.purchasesUnavailable))
        let (store, _, _) = makeStore(backend: closed)
        await store.loadCatalogue()
        XCTAssertEqual(store.catalogueState, .unavailable)
        XCTAssertTrue(store.offers.isEmpty)

        let badToken = FakeBackend(catalogue: .success(PurchaseAPI.Catalogue(products: serverProducts,
                                                                              accountToken: "not-a-uuid")))
        let (other, _, _) = makeStore(backend: badToken)
        await other.loadCatalogue()
        XCTAssertEqual(other.catalogueState, .unavailable)

        let offline = FakeBackend(catalogue: .failure(.offline))
        let (third, _, _) = makeStore(backend: offline)
        await third.loadCatalogue()
        XCTAssertEqual(third.catalogueState, .failed)
    }

    // MARK: Purchase

    /// The account's token goes to StoreKit; the transaction is finished
    /// only after the server granted it, and the new balance is handed on.
    func testAGrantFinishesTheTransactionAfterTheServerSaysSo() async throws {
        let log = FinishLog()
        let (store, storeKit, backend) = makeStore()
        var granted: [AccountAPI.Usage?] = []
        store.onGrant = { granted.append($0) }
        await store.loadCatalogue()
        let usage = AccountAPI.Usage(dailyLimit: 7, usedToday: 7, remainingToday: 0, monthlyLimit: 0, usedMonth: 7,
                                     resetsAt: "2026-10-21T19:00:00Z", timezone: "Asia/Almaty", credits: 50,
                                     remainingTotal: 50)
        backend.verdicts = [.success(PurchaseAPI.Verdict(status: "granted", usage: usage))]
        storeKit.result = .success(.purchased(transaction("t1", log: log)))

        await store.purchase(try XCTUnwrap(store.offers.first))

        XCTAssertEqual(storeKit.tokens, [try XCTUnwrap(UUID(uuidString: token))])
        XCTAssertEqual(backend.submitted, ["jws-t1"])
        XCTAssertEqual(log.finished, ["t1"])
        XCTAssertEqual(store.status, .granted)
        XCTAssertEqual(granted, [usage])
    }

    /// Paid, but the server did not answer: the transaction stays with the
    /// App Store and is sent again from the unfinished ones.
    func testAnUnconfirmedPurchaseStaysUnfinishedAndIsSentAgain() async throws {
        let log = FinishLog()
        let (store, storeKit, backend) = makeStore()
        await store.loadCatalogue()
        let paid = transaction("t2", log: log)
        backend.verdicts = [.failure(.offline)]
        storeKit.result = .success(.purchased(paid))

        await store.purchase(try XCTUnwrap(store.offers.first))
        XCTAssertEqual(store.status, .failed(.willRetry))
        XCTAssertTrue(log.finished.isEmpty)

        storeKit.unfinished = [paid]
        backend.verdicts = [.success(PurchaseAPI.Verdict(status: "already_granted", usage: nil))]
        await store.processUnfinished()
        XCTAssertEqual(backend.submitted, ["jws-t2", "jws-t2"])
        XCTAssertEqual(log.finished, ["t2"])
    }

    /// Finished only after a grant, or a refund the store made. A refusal
    /// that only a misconfigured server gives (another bundle, a product
    /// type it does not sell) stays, to be sent again once it is fixed.
    func testOnlyARevokedRefusalIsFinished() async {
        let cases: [(APIError, PurchaseStore.Failure, Bool)] = [
            (.purchaseInvalid(reason: "revoked"), .notConfirmed, true),
            (.purchaseInvalid(reason: "product_type"), .notConfirmed, false),
            (.purchaseInvalid(reason: "bundle_mismatch"), .notConfirmed, false),
            (.purchaseInvalid(reason: "signature"), .notConfirmed, false),
            (.purchaseInvalid(reason: nil), .notConfirmed, false),
            (.purchaseBelongsToAnotherAccount, .anotherAccount, false),
            (.purchasesUnavailable, .willRetry, false),
            (.storeUnavailable, .willRetry, false),
            (.server, .willRetry, false),
            (.rateLimited(retryAfter: 5), .willRetry, false)
        ]
        for (error, failure, finishes) in cases {
            let log = FinishLog()
            let (store, _, backend) = makeStore()
            backend.verdicts = [.failure(error)]
            let delivery = await store.deliver(transaction("t", log: log))
            XCTAssertEqual(delivery, .failed(failure), "\(error)")
            XCTAssertEqual(log.finished.isEmpty, !finishes, "\(error)")
        }
    }

    func testEveryOtherOutcomeIsSaidAsItIs() async throws {
        let (store, storeKit, backend) = makeStore()
        await store.loadCatalogue()
        let offer = try XCTUnwrap(store.offers.first)

        storeKit.result = .success(.pending)
        await store.purchase(offer)
        XCTAssertEqual(store.status, .pending)

        storeKit.result = .success(.cancelled)
        await store.purchase(offer)
        XCTAssertEqual(store.status, .cancelled)

        storeKit.result = .success(.unverified)
        await store.purchase(offer)
        XCTAssertEqual(store.status, .failed(.store))

        storeKit.result = .failure(StoreKitClientError.productUnavailable)
        await store.purchase(offer)
        XCTAssertEqual(store.status, .failed(.store))
        XCTAssertTrue(backend.submitted.isEmpty, "nothing reached the server")

        store.dismissStatus()
        XCTAssertEqual(store.status, .idle)
    }

    /// Ask to Buy: the purchase waits; the approval arrives as an update and
    /// the screen says the replies are there.
    func testAnApprovalArrivesLater() async throws {
        let log = FinishLog()
        let (store, storeKit, _) = makeStore()
        await store.loadCatalogue()
        storeKit.result = .success(.pending)
        await store.purchase(try XCTUnwrap(store.offers.first))
        XCTAssertEqual(store.status, .pending)

        await store.received(transaction("t3", log: log))
        XCTAssertEqual(store.status, .granted)
        XCTAssertEqual(log.finished, ["t3"])
    }

    func testUnrelatedGrantDoesNotConfirmAPendingPurchase() async throws {
        let log = FinishLog()
        let (store, apple, _) = makeStore()
        await store.loadCatalogue()
        apple.result = .success(.pending)
        await store.purchase(try XCTUnwrap(store.offers.first))
        let unrelated = StoreTransaction(id: "older", productID: "another-product", signedTransaction: "older-jws",
                                         finish: { log.finish("older") })
        await store.received(unrelated)
        XCTAssertEqual(store.status, .pending)
        await store.received(transaction("approved", log: log))
        XCTAssertEqual(store.status, .granted)
        XCTAssertEqual(log.finished, ["older", "approved"])
    }

    func testNoTokenNoPurchase() async {
        let (store, storeKit, _) = makeStore()
        let offer = PurchaseOffer(code: "pack_50", kind: .pack, units: 50, durationHours: 0,
                                  storeProductID: "pack.50", name: "50", displayPrice: "990 ₸")
        await store.purchase(offer)
        XCTAssertEqual(store.status, .failed(.unavailable), "the catalogue was never loaded")
        XCTAssertTrue(storeKit.tokens.isEmpty)
    }

    /// Another account signed in: the last one's token and products go.
    func testAnotherAccountStartsClean() async {
        let (store, _, _) = makeStore()
        await store.loadCatalogue()
        XCTAssertFalse(store.offers.isEmpty)
        store.accountDidChange(isSignedIn: true, userID: "u2", features: features(#"{"iap_ios": true}"#))
        XCTAssertTrue(store.offers.isEmpty)
        XCTAssertEqual(store.catalogueState, .idle)
    }

    /// A refused transaction is not sent again on every return to the app;
    /// the next launch sends it again.
    func testARefusalIsSentAgainAtTheNextLaunch() async {
        let log = FinishLog()
        let paid = transaction("t6", log: log)
        let (store, storeKit, backend) = makeStore()
        storeKit.unfinished = [paid]
        backend.verdicts = [.failure(.purchaseInvalid(reason: "bundle_mismatch"))]

        await store.processUnfinished()
        await store.processUnfinished()
        XCTAssertEqual(backend.submitted, ["jws-t6"])

        let (relaunched, relaunchedStoreKit, relaunchedBackend) = makeStore()
        relaunchedStoreKit.unfinished = [paid]
        await relaunched.processUnfinished()
        XCTAssertEqual(relaunchedBackend.submitted, ["jws-t6"])
        XCTAssertEqual(log.finished, ["t6"], "granted once the server is fixed")
    }

    // MARK: The same transaction twice at once

    /// Back from the App Store sheet, the app sends what is unfinished - the
    /// sheet's own transaction among them - while the sheet sends it too.
    /// One request; when it fails, the sheet says so and can be closed.
    func testTheSheetSharesARequestAlreadyRunning() async throws {
        let log = FinishLog()
        let (store, storeKit, backend) = makeStore()
        await store.loadCatalogue()
        let offer = try XCTUnwrap(store.offers.first)
        let paid = transaction("t7", log: log)
        storeKit.unfinished = [paid]
        storeKit.result = .success(.purchased(paid))
        backend.verdicts = [.failure(.offline)]
        backend.holdsAnswers = true

        let returning = Task { await store.processUnfinished() }
        for _ in 0..<200 where backend.submitted.isEmpty { try await Task.sleep(for: .milliseconds(5)) }
        let buying = Task { await store.purchase(offer) }
        for _ in 0..<200 where store.status != .verifying(productID: "pack.50") {
            try await Task.sleep(for: .milliseconds(5))
        }
        XCTAssertEqual(store.status, .verifying(productID: "pack.50"))

        backend.holdsAnswers = false
        await returning.value
        await buying.value

        XCTAssertEqual(backend.submitted, ["jws-t7"], "one request for both")
        XCTAssertEqual(store.status, .failed(.willRetry))
        XCTAssertTrue(log.finished.isEmpty)
        store.dismissStatus()
        XCTAssertEqual(store.status, .idle)
    }

    func testASharedGrantIsToldOnce() async throws {
        let log = FinishLog()
        let (store, storeKit, backend) = makeStore()
        var grants = 0
        store.onGrant = { _ in grants += 1 }
        await store.loadCatalogue()
        let offer = try XCTUnwrap(store.offers.first)
        let paid = transaction("t8", log: log)
        storeKit.unfinished = [paid]
        storeKit.result = .success(.purchased(paid))
        backend.holdsAnswers = true

        let returning = Task { await store.processUnfinished() }
        for _ in 0..<200 where backend.submitted.isEmpty { try await Task.sleep(for: .milliseconds(5)) }
        let buying = Task { await store.purchase(offer) }
        for _ in 0..<200 where store.status != .verifying(productID: "pack.50") {
            try await Task.sleep(for: .milliseconds(5))
        }

        backend.holdsAnswers = false
        await buying.value
        await returning.value

        XCTAssertEqual(backend.submitted, ["jws-t8"])
        XCTAssertEqual(store.status, .granted)
        XCTAssertEqual(log.finished, ["t8"])
        XCTAssertEqual(grants, 1)
    }

    // MARK: Transactions outside the sheet

    /// A transaction that arrives before the account is known waits, and is
    /// sent once a signed-in account on a server with purchases is there -
    /// even when sales have closed since it was bought.
    func testTransactionsWaitForTheAccount() async throws {
        let log = FinishLog()
        let storeKit = FakeStoreKit()
        let backend = FakeBackend(catalogue: .failure(.purchasesUnavailable))
        let store = PurchaseStore(storeKit: storeKit, backend: backend)

        await store.received(transaction("t4", log: log))
        XCTAssertTrue(backend.submitted.isEmpty)

        store.accountDidChange(isSignedIn: true, userID: "u1", features: features("{}"))
        try await Task.sleep(for: .milliseconds(50))
        XCTAssertTrue(backend.submitted.isEmpty, "a server without purchases is never asked")

        store.accountDidChange(isSignedIn: true, userID: "u1", features: features(#"{"iap_ios": false}"#))
        for _ in 0..<100 where log.finished.isEmpty {
            try await Task.sleep(for: .milliseconds(10))
        }
        XCTAssertEqual(backend.submitted, ["jws-t4"])
        XCTAssertEqual(log.finished, ["t4"])
    }

    func testRestoreConfirmsSignedSubscriptionsAndDeduplicatesUnfinished() async {
        let log = FinishLog()
        let (store, apple, backend) = makeStore()
        let subscription = transaction("restored", log: log)
        apple.entitlements = [subscription]
        apple.unfinished = [subscription]
        backend.verdicts = [.success(PurchaseAPI.Verdict(status: "already_granted", usage: nil))]
        await store.restore()
        XCTAssertEqual(apple.syncCount, 1)
        XCTAssertEqual(backend.submitted, ["jws-restored"])
        XCTAssertEqual(log.finished, ["restored"])
        XCTAssertEqual(store.status, .restored)
    }

    func testRestoreDoesNotReportSuccessBeforeTheServerConfirms() async {
        let log = FinishLog()
        let (store, apple, backend) = makeStore()
        apple.entitlements = [transaction("restore-pending", log: log)]
        backend.verdicts = [.failure(.offline)]
        await store.restore()
        XCTAssertEqual(store.status, .failed(.willRetry))
        XCTAssertTrue(log.finished.isEmpty)
    }

    func testRestoreWithoutTransactionsRefreshesAccountWithoutGrantingCredits() async {
        let (store, apple, backend) = makeStore()
        var refreshes = 0
        store.onGrant = { _ in refreshes += 1 }
        await store.restore()
        XCTAssertEqual(apple.syncCount, 1)
        XCTAssertTrue(backend.submitted.isEmpty)
        XCTAssertEqual(refreshes, 1)
        XCTAssertEqual(store.status, .nothingToRestore)
    }

    func testSubscriptionsRequireMatchingStoreTypeAndPeriod() {
        let product = PurchaseAPI.Product(code: "pro_monthly", kind: "subscription", units: 0, durationHours: 0,
                                          storeProductID: "pro.month", name: "Pro", planID: "pro",
                                          billingPeriod: "month", entitlementKind: "unlimited")
        let subscription = StoreProduct(id: "pro.month", displayName: "Pro", displayPrice: "2 999 ₸",
                                        kind: .subscription, billingPeriod: "month")
        XCTAssertEqual(PurchaseOffer.match(server: [product], store: [subscription]).count, 1)
        var wrong = subscription
        wrong.kind = .consumable
        XCTAssertTrue(PurchaseOffer.match(server: [product], store: [wrong]).isEmpty)
        wrong = subscription
        wrong.billingPeriod = "year"
        XCTAssertTrue(PurchaseOffer.match(server: [product], store: [wrong]).isEmpty)
    }

    func testStorePriceIsPreservedAndMismatchDetected() throws {
        let product = PurchaseAPI.Product(code: "pack30", kind: "pack", units: 30, durationHours: 0,
                                          storeProductID: "pack30", name: "30", price: 10000, currency: "KZT")
        let apple = StoreProduct(id: "pack30", displayName: "30", displayPrice: "199 ₸", price: 199, currency: "KZT")
        let offer = try XCTUnwrap(PurchaseOffer.match(server: [product], store: [apple]).first)
        XCTAssertEqual(offer.displayPrice, "199 ₸")
        XCTAssertTrue(offer.hasPriceMismatch)
    }

    func testUnapprovedPriceCannotOpenCheckout() async throws {
        let (store, apple, backend) = makeStore()
        await store.loadCatalogue()
        var offer = try XCTUnwrap(store.offers.first)
        offer.hasPriceMismatch = true
        await store.purchase(offer)
        XCTAssertEqual(store.status, .failed(.unavailable))
        XCTAssertTrue(apple.tokens.isEmpty)
        XCTAssertTrue(backend.submitted.isEmpty)
    }

    func testRestoreRemainsAvailableAfterSalesClose() async {
        let (store, apple, _) = makeStore(iap: #"{"iap_ios": false}"#)
        XCTAssertTrue(store.canRestore)
        await store.restore()
        XCTAssertEqual(apple.syncCount, 1)
        XCTAssertEqual(store.status, .nothingToRestore)
    }

    func testRecommendationDismissalIsPerAccountAndThreshold() async throws {
        let preferences = try XCTUnwrap(UserDefaults(suiteName: "RecommendationTests.\(UUID())"))
        let recommendation = PurchaseAPI.Recommendation(id: "base_after_pack_4", purchaseCount: 4, spentMinor: 40000,
                                                       currency: "KZT", basePriceMinor: 59900, baseStoreProductID: "base.month")
        let catalogue = PurchaseAPI.Catalogue(products: serverProducts, accountToken: token, recommendation: recommendation)
        let backend = FakeBackend(catalogue: .success(catalogue))
        let (store, _, _) = makeStore(backend: backend, preferences: preferences)
        await store.loadCatalogue()
        XCTAssertEqual(store.recommendation, recommendation)
        store.dismissRecommendation()
        await store.loadCatalogue()
        XCTAssertNil(store.recommendation)
        let (relaunched, _, _) = makeStore(backend: backend, preferences: preferences)
        await relaunched.loadCatalogue()
        XCTAssertNil(relaunched.recommendation)
        relaunched.accountDidChange(isSignedIn: true, userID: "another-user", features: features(#"{"iap_ios": true}"#))
        await relaunched.loadCatalogue()
        XCTAssertEqual(relaunched.recommendation, recommendation)
    }

    func testRestoreDoesNotReuseAnotherAccountsInFlightGrant() async throws {
        let log = FinishLog()
        let (store, apple, backend) = makeStore()
        let paid = transaction("switched", log: log)
        backend.holdsAnswers = true
        backend.verdicts = [.success(PurchaseAPI.Verdict(status: "granted", usage: nil)),
                            .failure(.purchaseBelongsToAnotherAccount)]
        let firstAccount = Task { await store.deliver(paid) }
        for _ in 0..<200 where backend.submitted.isEmpty { try await Task.sleep(for: .milliseconds(5)) }
        store.accountDidChange(isSignedIn: true, userID: "another-user", features: features(#"{"iap_ios": true}"#))
        apple.entitlements = [paid]
        let restoring = Task { await store.restore() }
        for _ in 0..<200 where backend.submitted.count < 2 { try await Task.sleep(for: .milliseconds(5)) }
        XCTAssertEqual(backend.submitted.count, 2, "each account requires its own server verdict")
        backend.holdsAnswers = false
        _ = await firstAccount.value
        await restoring.value
        XCTAssertEqual(store.status, .failed(.anotherAccount))
        XCTAssertEqual(log.finished, ["switched"], "only the first account was granted")
    }

    func testMalformedOptionalRecommendationDoesNotHideLegacyProducts() throws {
        let catalogue = try JSONDecoder().decode(PurchaseAPI.Catalogue.self, from: Data("""
        {"products": [{"code": "pack", "kind": "pack", "units": 30, "store_product_id": "pack30"}],
         "account_token": "\(token)", "recommendation": {"purchase_count": "four"}}
        """.utf8))
        XCTAssertEqual(catalogue.products.first?.units, 30)
        XCTAssertNil(catalogue.recommendation)
    }

    // MARK: Wire format

    func testTheServersAnswersAreRead() throws {
        let catalogue = try JSONDecoder().decode(PurchaseAPI.Catalogue.self, from: Data("""
        {"products": [{"code": "pack_50", "kind": "pack", "units": 50, "duration_hours": 0,
                       "store_product_id": "kz.aireply.pack.50", "name": "50 ответов"}],
         "account_token": "0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d"}
        """.utf8))
        XCTAssertEqual(catalogue.products.first?.storeProductID, "kz.aireply.pack.50")
        XCTAssertEqual(catalogue.accountToken, token)

        let granted = try JSONDecoder().decode(PurchaseAPI.Verdict.self, from: Data("""
        {"status": "granted", "usage": {"daily_limit": 7, "used_today": 0, "remaining_today": 7, "monthly_limit": 0,
         "used_month": 0, "resets_at": "2026-10-21T19:00:00Z", "timezone": "Asia/Almaty", "credits": 50,
         "remaining_total": 57}}
        """.utf8))
        XCTAssertTrue(granted.isGranted)
        XCTAssertEqual(granted.usage?.credits, 50)
        let again = try JSONDecoder().decode(PurchaseAPI.Verdict.self, from: Data(#"{"status": "already_granted"}"#.utf8))
        XCTAssertTrue(again.isGranted)
        XCTAssertNil(again.usage)
        XCTAssertFalse(PurchaseAPI.Verdict(status: "pending", usage: nil).isGranted)
    }

    func testPurchaseErrorsKeepTheirMeaning() {
        func map(_ code: String, _ status: Int, details: String = "") -> APIError {
            let data = Data("""
            {"error": {"code": "\(code)", "message": "x"\(details.isEmpty ? "" : ", \"details\": {\(details)}")}}
            """.utf8)
            let response = HTTPURLResponse(url: URL(string: "https://example.test")!, statusCode: status,
                                           httpVersion: nil, headerFields: nil)!
            return APIClient.mapServerError(status: status, data: data, headers: response)
        }
        XCTAssertEqual(map("IAP_UNAVAILABLE", 403), .purchasesUnavailable, "a 403 that is not a lost session")
        XCTAssertEqual(map("PURCHASE_INVALID", 400, details: #""reason": "revoked""#), .purchaseInvalid(reason: "revoked"))
        XCTAssertEqual(map("PURCHASE_BELONGS_TO_ANOTHER_ACCOUNT", 409), .purchaseBelongsToAnotherAccount)
        XCTAssertEqual(map("STORE_UNAVAILABLE", 503), .storeUnavailable)
    }

    func testTheQueryIsNotPartOfThePath() throws {
        let client = APIClient(baseURL: try XCTUnwrap(URL(string: "https://example.test")))
        let request = try client.makeRequest(path: "api/v1/iap/products", method: "GET", body: Optional<APIClient.Empty>.none,
                                             token: nil, query: [URLQueryItem(name: "platform", value: "ios")])
        XCTAssertEqual(request.url?.absoluteString, "https://example.test/api/v1/iap/products?platform=ios")
    }
}
