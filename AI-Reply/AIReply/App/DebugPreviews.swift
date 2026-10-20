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
