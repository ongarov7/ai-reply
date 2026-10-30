import SwiftUI

/// Current access and the full reply balance, as confirmed by the server.
///
/// Тариф пен күндік квота: шешімді әрқашан сервер қабылдайды.
///
/// The numbers on this screen come from the server on every appearance. The app
/// keeps a cached copy only so the keyboard can render instantly; it is never
/// the source of truth, and a stale cache can only ever be pessimistic.
///
struct SubscriptionView: View {

    @Environment(AppSettings.self) private var settings
    @Environment(AccountModel.self) private var account

    var body: some View {
        ScrollView {
            usageCard
                .padding(.horizontal, DS.Spacing.l)
                .padding(.vertical, DS.Spacing.l)
                .frame(maxWidth: DS.Layout.readableWidth, alignment: .leading)
                .frame(maxWidth: .infinity, alignment: .center)
        }
        .background(Color.dsBackground)
        .navigationTitle("subscription.title")
        .navigationBarTitleDisplayMode(.inline)
        .task {
            await account.refresh()
        }
    }

    // MARK: Pieces

    private var usageCard: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.s) {
            HStack {
                VStack(alignment: .leading, spacing: DS.Spacing.xxs) {
                    Text("subscription.current").font(.caption).foregroundStyle(.secondary)
                    Text(verbatim: planName)
                        .font(.title3.weight(.semibold))
                }
                Spacer()
                VStack(alignment: .trailing, spacing: DS.Spacing.xxs) {
                    // Everything usable today, free and purchased replies
                    // included. A dash until the server has answered.
                    Text(verbatim: usageIsKnown ? (account.usage.unlimited ? "∞" : "\(account.usage.availableNow)") : "—")
                        .font(.largeTitle.weight(.semibold).monospacedDigit())
                        .foregroundStyle(usageIsKnown ? Color.primary : Color.secondary)
                    Text(account.usage.unlimited ? LocalizedStringKey("purchase.unlimited") : LocalizedStringKey("subscription.remaining"))
                        .font(.caption).foregroundStyle(.secondary)
                }
            }

            // The plan's own day: its allowance and what was used of it.
            if !account.usage.unlimited && account.usage.dailyLimit > 0 {
                ProgressView(value: progress)
                    .tint(Color.accentColor)
                Text("subscription.usedToday \(account.usage.usedToday) \(account.usage.dailyLimit)")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }

            if !balance.extraRows.isEmpty {
                Divider()
                BalanceRows(rows: balance.extraRows)
            }

            if let expiry = account.subscription?.expiresAt, !expiry.isEmpty {
                Label(LocalizedStringKey("subscription.validUntil \(formatted(expiry))"), systemImage: "calendar")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
        .dsCard()
    }

    // MARK: Values

    private var planName: String {
        account.subscription?.plan.localizedName(settings.effectiveLanguage.rawValue) ?? "—"
    }

    /// The server's numbers have arrived.
    private var usageIsKnown: Bool { account.usage.isKnown }

    private var balance: BalanceSummary { BalanceSummary.make(account.usage) }

    private var progress: Double {
        guard account.usage.dailyLimit > 0 else { return 0 }
        return min(1, Double(account.usage.usedToday) / Double(account.usage.dailyLimit))
    }

    /// The server sends ISO-8601; the user gets their own locale's date.
    private func formatted(_ iso: String) -> String {
        let parser = ISO8601DateFormatter()
        parser.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        let date = parser.date(from: iso) ?? ISO8601DateFormatter().date(from: iso)
        guard let date else { return iso }
        return date.formatted(.dateTime.day().month().year().locale(settings.locale))
    }
}
