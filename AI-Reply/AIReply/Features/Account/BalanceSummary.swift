import SwiftUI

/// The account's replies as the screens show them: the server's numbers,
/// split by where they come from.
///
/// Жауап балансы: бүгін қолжетімді, сәлемдесу бонусы, сатып алынған жауаптар, рұқсаттама.
///
/// Built from `AccountAPI.Usage` alone, so it is never a guess: before the
/// server has answered nothing is shown as a number, a part the server does
/// not send has no row, and a spent part reads as a plain number, never as a
/// warning.
struct BalanceSummary: Equatable {

    enum Row: Equatable, Identifiable {
        /// What can be generated now: plan, pass, free and purchased
        /// replies together (the plan's day alone on an older server).
        case today(Int)
        case unlimited
        /// The free replies of a new account.
        case welcome(remaining: Int, total: Int)
        /// Administrator grants and purchased packs.
        case purchased(Int)
        case admin(Int)
        /// A running pass, until when and what is left of it.
        case pass(until: Date, remaining: Int)

        var id: String {
            switch self {
            case .today:     return "today"
            case .unlimited: return "unlimited"
            case .welcome:   return "welcome"
            case .purchased: return "purchased"
            case .admin:     return "admin"
            case .pass:      return "pass"
            }
        }
    }

    /// The server has answered.
    let isKnown: Bool
    /// «Бесплатные ответы: 35 из 50»: while free replies are left.
    let welcomeHeadline: AccountAPI.Usage.Welcome?
    let rows: [Row]

    static let unknown = BalanceSummary(isKnown: false, welcomeHeadline: nil, rows: [])

    static func make(_ usage: AccountAPI.Usage, now: Date = Date()) -> BalanceSummary {
        guard usage.isKnown else { return .unknown }
        var rows: [Row] = [usage.unlimited ? .unlimited : .today(usage.availableNow)]
        var headline: AccountAPI.Usage.Welcome?
        if let welcome = usage.welcome, welcome.total > 0 {
            let remaining = min(max(0, welcome.remaining), welcome.total)
            rows.append(.welcome(remaining: remaining, total: welcome.total))
            if remaining > 0 { headline = AccountAPI.Usage.Welcome(total: welcome.total, remaining: remaining) }
        }
        let purchased = usage.purchasedCredits ?? usage.credits.map { max(0, $0 - (usage.adminCredits ?? 0)) }
        if let credits = purchased, credits > 0 {
            rows.append(.purchased(credits))
        }
        if let credits = usage.adminCredits, credits > 0 { rows.append(.admin(credits)) }
        if let pass = usage.pass, let until = date(pass.expiresAt), until > now {
            rows.append(.pass(until: until, remaining: max(0, pass.remaining)))
        }
        return BalanceSummary(isKnown: true, welcomeHeadline: headline, rows: rows)
    }

    /// The rows beyond today's total: what the plan screen lists under its
    /// own number.
    var extraRows: [Row] {
        rows.filter {
            switch $0 { case .today, .unlimited: return false; default: return true }
        }
    }

    static func date(_ iso: String) -> Date? { ServerTime.date(iso) }
}

/// The server's timestamps.
enum ServerTime {
    /// RFC 3339 with or without fractions, any offset.
    static func date(_ iso: String) -> Date? {
        let parser = ISO8601DateFormatter()
        parser.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let date = parser.date(from: iso) { return date }
        parser.formatOptions = [.withInternetDateTime]
        return parser.date(from: iso)
    }
}

/// The rows of a `BalanceSummary`, label on the left and the number on the
/// right, as in Settings.
struct BalanceRows: View {

    let rows: [BalanceSummary.Row]
    /// A dash instead of today's number until the server has answered.
    var isKnown = true

    @Environment(AppSettings.self) private var settings

    var body: some View {
        VStack(spacing: 0) {
            if !isKnown {
                row(Text("balance.today"), value: Text(verbatim: "—"))
            }
            ForEach(Array(rows.enumerated()), id: \.element.id) { index, item in
                if index > 0 || !isKnown { Divider() }
                content(item)
            }
        }
    }

    @ViewBuilder
    private func content(_ item: BalanceSummary.Row) -> some View {
        switch item {
        case .today(let count):
            row(Text("balance.today"), value: Text(verbatim: "\(count)"), isTotal: true)
        case .unlimited:
            row(Text("purchase.unlimited"), value: Text(verbatim: "∞"), isTotal: true)
        case .welcome(let remaining, let total):
            row(Text("balance.welcome"), value: Text("balance.welcome.value \(remaining) \(total)"))
        case .purchased(let count):
            row(Text("balance.purchased"), value: Text(verbatim: "\(count)"))
        case .admin(let count):
            row(Text("balance.admin"), value: Text(verbatim: "\(count)"))
        case .pass(let until, let remaining):
            row(Text("balance.pass \(until, format: passFormat)"), value: Text(verbatim: "\(remaining)"))
        }
    }

    /// `isTotal`: today's number, the one that matters, in the primary colour.
    private func row(_ title: Text, value: Text, isTotal: Bool = false) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: DS.Spacing.s) {
            title
                .font(.subheadline)
                .foregroundStyle(.primary)
            Spacer(minLength: DS.Spacing.s)
            value
                .font(isTotal ? .subheadline.weight(.semibold).monospacedDigit() : .subheadline.monospacedDigit())
                .foregroundStyle(isTotal ? Color.primary : Color.secondary)
                .multilineTextAlignment(.trailing)
        }
        .padding(.vertical, DS.Spacing.xs)
        .accessibilityElement(children: .combine)
    }

    private var passFormat: Date.FormatStyle {
        Date.FormatStyle.dateTime.day().month(.abbreviated).hour().minute().locale(settings.locale)
    }
}
