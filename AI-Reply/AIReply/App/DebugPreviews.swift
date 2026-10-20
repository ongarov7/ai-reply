#if DEBUG
import SwiftUI

/// Sample data for the DEBUG screens (`-AIReplyDebugScreen …`), so new
/// screens can be reviewed and photographed in each language without an
/// account, a server or the App Store. None of it exists in a Release build.
///
/// Тек DEBUG: скриншоттарға арналған үлгі деректер.
enum DebugSamples {

    static let plan = AccountAPI.Plan(
        id: "free", code: "free",
        name: ["en": "Free", "ru": "Бесплатный", "kk": "Тегін", "uz": "Bepul"],
        description: [:], price: 0, priceText: "", currency: "KZT",
        dailyLimit: 7, monthlyLimit: 0, periodDays: 0, isFree: true
    )

    static let subscription = AccountAPI.Subscription(id: nil, status: "active", plan: plan, expiresAt: nil)

    /// Five of the plan's seven left, 35 of 50 free replies, 20 purchased
    /// and a pass until tomorrow afternoon.
    static var usage: AccountAPI.Usage {
        let tomorrow = Calendar.current.date(byAdding: .day, value: 1, to: Date()) ?? Date()
        let passEnd = Calendar.current.date(bySettingHour: 15, minute: 0, second: 0, of: tomorrow) ?? tomorrow
        var usage = AccountAPI.Usage(dailyLimit: 7, usedToday: 2, remainingToday: 5, monthlyLimit: 0, usedMonth: 2,
                                     resetsAt: ISO8601DateFormatter().string(from: tomorrow), timezone: "Asia/Almaty")
        usage.welcome = AccountAPI.Usage.Welcome(total: 50, remaining: 35)
        usage.credits = 20
        usage.pass = AccountAPI.Usage.Pass(expiresAt: ISO8601DateFormatter().string(from: passEnd), remaining: 40)
        usage.remainingTotal = 100
        return usage
    }
}

extension DebugSamples {

    /// Features as a server with everything new switched on would publish them.
    static var features: AccountAPI.Features? {
        try? JSONDecoder().decode(AccountAPI.Features.self, from: Data("""
        {"iap_ios": true, "support_chat": true, "credits": true, "welcome_bonus": true,
         "idempotent_generation": true, "permission_diagnostics": true}
        """.utf8))
    }

    /// Two packs and the pass, as an administrator could set them up.
    static let products = [
        PurchaseAPI.Product(code: "pack_50", kind: "pack", units: 50, durationHours: 0,
                            storeProductID: "kz.aireply.pack.50", name: localizedName(units: 50)),
        PurchaseAPI.Product(code: "pack_200", kind: "pack", units: 200, durationHours: 0,
                            storeProductID: "kz.aireply.pack.200", name: localizedName(units: 200)),
        PurchaseAPI.Product(code: "pass_24h", kind: "pass", units: 100, durationHours: 24,
                            storeProductID: "kz.aireply.pass.24h", name: localizedName(units: 0))
    ]

    private static func localizedName(units: Int) -> String {
        let language = SharedSettings.shared.effectiveAppLanguage
        switch (language, units) {
        case (.russian, 0): return "Пропуск на 24 часа"
        case (.russian, _): return "\(units) ответов"
        case (.kazakh, 0):  return "24 сағаттық рұқсаттама"
        case (.kazakh, _):  return "\(units) жауап"
        case (.uzbek, 0):   return "24 soatlik ruxsatnoma"
        case (.uzbek, _):   return "\(units) ta javob"
        case (_, 0):        return "24-hour pass"
        case (_, _):        return "\(units) replies"
        }
    }
}

/// The App Store, imitated. The prices are sample strings for the DEBUG
/// screen only: a real build shows `Product.displayPrice`.
struct DebugStoreKitClient: StoreKitClient {
    var canMakePayments: Bool { true }

    func products(for ids: [String]) async throws -> [StoreProduct] {
        let prices = ["kz.aireply.pack.50": "990 ₸", "kz.aireply.pack.200": "2 990 ₸", "kz.aireply.pass.24h": "590 ₸"]
        return ids.compactMap { id in prices[id].map { StoreProduct(id: id, displayName: id, displayPrice: $0) } }
    }

    func purchase(productID: String, appAccountToken: UUID) async throws -> StorePurchaseResult {
        try await Task.sleep(for: .milliseconds(600))
        return .pending
    }

    func unfinishedTransactions() async -> [StoreTransaction] { [] }

    func transactionUpdates() -> AsyncStream<StoreTransaction> { AsyncStream { $0.finish() } }
}

struct DebugPurchaseBackend: PurchaseBackend {
    func catalogue() async throws -> PurchaseAPI.Catalogue {
        PurchaseAPI.Catalogue(products: DebugSamples.products, accountToken: "0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d")
    }

    func submit(signedTransaction: String) async throws -> PurchaseAPI.Verdict {
        PurchaseAPI.Verdict(status: "granted", usage: DebugSamples.usage)
    }
}

/// «Докупить ответы» over the sample catalogue.
struct DebugPurchaseHost: View {
    @State private var store: PurchaseStore = {
        let store = PurchaseStore(storeKit: DebugStoreKitClient(), backend: DebugPurchaseBackend())
        store.accountDidChange(isSignedIn: true, userID: "debug", features: DebugSamples.features)
        return store
    }()

    var body: some View {
        NavigationStack { PurchaseView() }
            .environment(store)
    }
}

/// Home over the sample balance.
struct DebugBalanceHost: View {
    @Environment(AccountModel.self) private var account

    var body: some View {
        RootNavigationView()
            .onAppear {
                account.showDebugPreview(usage: DebugSamples.usage, subscription: DebugSamples.subscription)
            }
    }
}
#endif
