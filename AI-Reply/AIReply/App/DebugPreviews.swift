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
        dailyLimit: 0, monthlyLimit: 0, periodDays: 0, isFree: true, entitlementKind: "welcome"
    )

    static let subscription = AccountAPI.Subscription(id: nil, status: "active", plan: plan, expiresAt: nil)

    /// The approved free allowance and two permanent reply packs.
    static var usage: AccountAPI.Usage {
        var usage = AccountAPI.Usage(dailyLimit: 0, usedToday: 0, remainingToday: 0, monthlyLimit: 0, usedMonth: 3,
                                     resetsAt: "", timezone: "Asia/Almaty")
        usage.welcome = AccountAPI.Usage.Welcome(total: 10, remaining: 7)
        usage.credits = 60
        usage.purchasedCredits = 60
        usage.adminCredits = 0
        usage.remainingTotal = 67
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

    // Local preview identifiers; App Store Connect mappings are supplied by the published catalog.
    static let products = [
        PurchaseAPI.Product(code: "base_monthly", kind: "subscription", units: 0, durationHours: 0,
                            storeProductID: "preview.base.monthly", name: "Base", planID: "base_monthly",
                            billingPeriod: "month", entitlementKind: "daily", dailyLimit: 30),
        PurchaseAPI.Product(code: "base_yearly", kind: "subscription", units: 0, durationHours: 0,
                            storeProductID: "preview.base.yearly", name: "Base", planID: "base_yearly",
                            billingPeriod: "year", entitlementKind: "daily", dailyLimit: 30),
        PurchaseAPI.Product(code: "pro_monthly", kind: "subscription", units: 0, durationHours: 0,
                            storeProductID: "preview.pro.monthly", name: "Pro", planID: "pro_monthly",
                            billingPeriod: "month", entitlementKind: "unlimited"),
        PurchaseAPI.Product(code: "pro_yearly", kind: "subscription", units: 0, durationHours: 0,
                            storeProductID: "preview.pro.yearly", name: "Pro", planID: "pro_yearly",
                            billingPeriod: "year", entitlementKind: "unlimited"),
        PurchaseAPI.Product(code: "pack30", kind: "pack", units: 30, durationHours: 0,
                            storeProductID: "preview.pack30", name: localizedPackName)
    ]

    private static var localizedPackName: String {
        AppSettings.localized("purchase.pack30", language: SharedSettings.shared.effectiveAppLanguage)
    }
}

/// Preview prices are excluded from Release. Checkout always uses StoreKit.
struct DebugStoreKitClient: StoreKitClient {
    var canMakePayments: Bool { true }

    func products(for ids: [String]) async throws -> [StoreProduct] {
        let prices = ["preview.base.monthly": "599 ₸", "preview.base.yearly": "5 999 ₸",
                      "preview.pro.monthly": "2 999 ₸", "preview.pro.yearly": "29 990 ₸", "preview.pack30": "100 ₸"]
        return ids.compactMap { id in
            prices[id].map { StoreProduct(id: id, displayName: id, displayPrice: $0,
                                         kind: id == "preview.pack30" ? .consumable : .subscription,
                                         billingPeriod: id.hasSuffix("monthly") ? "month" : (id.hasSuffix("yearly") ? "year" : nil)) }
        }
    }

    func purchase(productID: String, appAccountToken: UUID) async throws -> StorePurchaseResult {
        try await Task.sleep(for: .milliseconds(600))
        return .pending
    }

    func sync() async throws {}
    func currentEntitlements() async -> [StoreTransaction] { [] }
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

/// The exhausted sheet over «Создать ответ», as after a refused Reply;
/// `-AIReplyDebugGender` picks the user's gender.
struct DebugExhaustedHost: View {
    let offersPurchase: Bool
    @State private var isShowing = true

    var body: some View {
        NavigationStack { ComposeView() }
            .sheet(isPresented: $isShowing) {
                ExhaustedSheetView(offer: ExhaustedOffer(offersPurchase: offersPurchase),
                                   gender: DebugScreen.onboardingGender, onBuy: {}, onPlans: {})
            }
    }
}

/// A conversation with an operator, imitated.
struct DebugSupportTransport: SupportTransport {
    let isEmpty: Bool

    private static func stamp(daysAgo: Int, hour: Int, minute: Int) -> String {
        let day = Calendar.current.date(byAdding: .day, value: -daysAgo, to: Date()) ?? Date()
        let date = Calendar.current.date(bySettingHour: hour, minute: minute, second: 0, of: day) ?? day
        return ISO8601DateFormatter().string(from: date)
    }

    private var messages: [SupportAPI.Message] {
        guard !isEmpty else { return [] }
        let russian = SharedSettings.shared.effectiveAppLanguage == .russian
        let texts = russian ? [
            "Здравствуйте! Клавиатура не видит скопированное сообщение. Что делать?",
            "Здравствуйте! Откройте Настройки → AI Reply → «Вставка из других приложений» и выберите «Разрешить». Потом скопируйте сообщение ещё раз.",
            "Получилось, спасибо!",
            "Рады помочь. Если появятся вопросы — пишите сюда."
        ] : [
            "Hello! The keyboard does not see the message I copied. What should I do?",
            "Hello! Open Settings → AI Reply → Paste from Other Apps and choose Allow. Then copy the message again.",
            "It works now, thank you!",
            "Glad to help. Write here if anything else comes up."
        ]
        return [
            SupportAPI.Message(id: "m1", sender: "user", text: texts[0], createdAt: Self.stamp(daysAgo: 1, hour: 18, minute: 2), readAt: nil),
            SupportAPI.Message(id: "m2", sender: "operator", text: texts[1], createdAt: Self.stamp(daysAgo: 1, hour: 18, minute: 10),
                               readAt: Self.stamp(daysAgo: 1, hour: 18, minute: 12)),
            SupportAPI.Message(id: "m3", sender: "user", text: texts[2], createdAt: Self.stamp(daysAgo: 0, hour: 9, minute: 15), readAt: nil),
            SupportAPI.Message(id: "m4", sender: "operator", text: texts[3], createdAt: Self.stamp(daysAgo: 0, hour: 9, minute: 16),
                               readAt: nil)
        ]
    }

    func conversation() async throws -> SupportAPI.Conversation? {
        isEmpty ? nil : SupportAPI.Conversation(id: "c1", status: "open", unreadCount: 1)
    }

    func messages(_ cursor: SupportAPI.Cursor, limit: Int) async throws -> SupportAPI.Page {
        SupportAPI.Page(messages: cursor == .latest ? messages : [], hasMore: false)
    }

    func send(text: String, clientMessageID: String) async throws -> SupportAPI.Message {
        SupportAPI.Message(id: clientMessageID, sender: "user", text: text,
                           createdAt: ISO8601DateFormatter().string(from: Date()), readAt: nil)
    }

    func markRead(upTo messageID: String) async throws -> Int { 0 }
}

/// «Чат с оператором» over the sample conversation.
struct DebugSupportHost: View {
    @State private var inbox: SupportInbox

    init(isEmpty: Bool) {
        let inbox = SupportInbox(transport: DebugSupportTransport(isEmpty: isEmpty))
        inbox.accountDidChange(isSignedIn: true, hasAcceptedLegal: true, features: DebugSamples.features)
        _inbox = State(initialValue: inbox)
    }

    var body: some View {
        NavigationStack { SupportChatView() }
            .environment(inbox)
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
