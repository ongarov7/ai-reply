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
