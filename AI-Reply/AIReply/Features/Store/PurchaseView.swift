import SwiftUI
import StoreKit
import UIKit

struct PurchaseView: View {
    @Environment(PurchaseStore.self) private var store
    @Environment(AccountModel.self) private var account

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                PurchaseOptionsView(onSelectOffer: { proxy.scrollTo($0, anchor: .top) })
                    .padding(DS.Spacing.l)
                    .frame(maxWidth: DS.Layout.readableWidth)
                    .frame(maxWidth: .infinity)
            }
            #if DEBUG
            .onChange(of: store.offers) { _, offers in
                if let code = DebugScreen.value(after: "-AIReplyDebugOffer"),
                   let offer = offers.first(where: { $0.code == code }) {
                    Task { @MainActor in
                        await Task.yield()
                        proxy.scrollTo(offer.id, anchor: .top)
                    }
                }
            }
            #endif
        }
        .background(Color.dsBackground)
        .navigationTitle("purchase.title")
        .navigationBarTitleDisplayMode(.inline)
        .task {
            await account.refresh()
            await account.loadPlans()
            await store.loadCatalogue()
        }
        .onDisappear { store.dismissStatus() }
    }
}

/// One catalogue for the plan and purchase routes. Paid plans appear only
/// with a matched StoreKit offer. A missing selected period falls back to one period,
/// never to both monthly and yearly cards together.
struct SubscriptionPlanCatalogue {
    struct Option: Identifiable {
        enum Allowance: Equatable {
            case daily(Int), monthly(Int), unlimited, unspecified
        }

        let plan: AccountAPI.Plan?
        let offer: PurchaseOffer
        let billingPeriod: String
        let id: String

        var planID: String? { offer.planID ?? plan?.id }
        var allowance: Allowance {
            let kind = offer.entitlementKind ?? plan?.entitlementKind
            let limit = offer.dailyLimit
            if kind == "unlimited" { return .unlimited }
            if kind == "daily", limit > 0 { return .daily(limit) }
            if kind == "monthly", offer.monthlyLimit > 0 { return .monthly(offer.monthlyLimit) }
            return .unspecified
        }

        func name(_ language: String) -> String {
            offer.name
        }
    }

    let freePlan: AccountAPI.Plan?
    let options: [Option]

    init(plans: [AccountAPI.Plan], offers: [PurchaseOffer]) {
        freePlan = plans.first(where: \.isFree)
        let subscriptions = offers.filter { $0.kind == .subscription && $0.isAvailableForNewPurchase }
        var included = Set<String>()
        var result: [Option] = []
        for plan in plans where !plan.isFree {
            let period: String
            switch plan.periodDays {
            case 30: period = "month"
            case 365: period = "year"
            default: continue
            }
            guard let offer = subscriptions.first(where: { $0.planID == plan.id && $0.billingPeriod == period }) else { continue }
            included.insert(offer.id)
            result.append(Option(plan: plan, offer: offer, billingPeriod: period, id: offer.id))
        }
        for offer in subscriptions where !included.contains(offer.id) {
            guard let period = offer.billingPeriod, period == "month" || period == "year" else { continue }
            result.append(Option(plan: nil, offer: offer, billingPeriod: period, id: offer.id))
        }
        options = result
    }

    var periods: [String] {
        ["month", "year"].filter { period in options.contains { $0.billingPeriod == period } }
    }

    func visible(period: String) -> [Option] {
        let selected = periods.contains(period) ? period : periods.first
        return options.filter { $0.billingPeriod == selected }
    }
}

/// Paid plans and prices appear only after both the server and the App Store
/// return the product. The current plan and free allowance remain readable.
struct PurchaseOptionsView: View {
    var onSelectOffer: (String) -> Void = { _ in }
    @Environment(\.openURL) private var openURL
    @Environment(PurchaseStore.self) private var store
    @Environment(AccountModel.self) private var account
    @Environment(AppSettings.self) private var settings
    @State private var period = "month"
    @State private var management = SubscriptionManagement()
    @State private var subscriptionWindow = SubscriptionWindow()
    @State private var selectedOfferID: String?

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.l) {
            VStack(alignment: .leading, spacing: DS.Spacing.xs) {
                Text("purchase.heading").font(.title2.bold()).accessibilityAddTraits(.isHeader)
                Text("purchase.intro").font(.subheadline).foregroundStyle(.secondary)
            }

            if store.status != .idle { statusCard }
            if !store.canMakePayments, store.isEnabled {
                Label("purchase.cannotPay", systemImage: "lock").font(.footnote).foregroundStyle(.secondary)
            }

            if !store.status.isBusy, let recommendation = store.recommendation,
               let offer = recommendedOffer(recommendation) {
                recommendationCard(recommendation, offer: offer)
            }

            if let free = planCatalogue.freePlan { freeCard(free) }
            subscriptionPlans

            if store.isEnabled {
                catalogue
            } else {
                unavailable
            }

            VStack(spacing: DS.Spacing.xs) {
                Button {
                    Task { await store.restore(); await account.refresh() }
                } label: {
                    Label("purchase.restore", systemImage: "arrow.clockwise")
                }
                .disabled(!store.canRestore)
                .frame(maxWidth: .infinity, minHeight: DS.Layout.minimumTouchTarget)
                if !ProcessInfo.processInfo.isiOSAppOnMac {
                    Button("purchase.manage") {
                        Task {
                            await management.show(hasSubscription: {
                                await AppStoreClient.hasSubscriptionHistory()
                            }, present: {
                                guard let scene = subscriptionWindow.window?.windowScene,
                                      scene.activationState == .foregroundActive else {
                                    throw SubscriptionManagement.PresentationError.unavailableScene
                                }
                                try await AppStore.showManageSubscriptions(in: scene)
                            }, refresh: {
                                await store.processUnfinished()
                                await account.refresh()
                            })
                        }
                    }
                        .disabled(management.isPresenting)
                        .accessibilityIdentifier("purchase.manage")
                        .frame(maxWidth: .infinity, minHeight: DS.Layout.minimumTouchTarget)
                    Link("purchase.manage.appStore", destination: SubscriptionWindow.subscriptionsURL)
                        .font(.footnote)
                        .frame(maxWidth: .infinity, minHeight: DS.Layout.minimumTouchTarget)
                }
                Text("purchase.restore.detail")
                    .font(.caption).foregroundStyle(.secondary).multilineTextAlignment(.center)
            }

            VStack(alignment: .leading, spacing: DS.Spacing.xs) {
                Text("purchase.renewal").font(.footnote).foregroundStyle(.secondary)
                HStack(spacing: DS.Spacing.m) {
                    if let url = legalURL(account.legalConfig.termsURL) { Link("legal.terms", destination: url) }
                    if let url = legalURL(account.legalConfig.privacyURL) { Link("legal.privacy", destination: url) }
                }
                .font(.footnote)
                if !account.displayIdentifier.isEmpty {
                    Text("purchase.footer.account \(account.displayIdentifier)")
                        .font(.caption).foregroundStyle(.secondary)
                }
            }
        }
        .background(SubscriptionWindowReader(anchor: subscriptionWindow).allowsHitTesting(false))
        .alert(LocalizedStringKey(management.hasNoSubscription ? "purchase.manage.empty.title" : "purchase.manage.failed.title"), isPresented: $management.needsHelp) {
            if management.hasNoSubscription {
                Button("purchase.restore") {
                    Task { await store.restore(); await account.refresh() }
                }
            }
            Button("purchase.manage.appStore") { openURL(SubscriptionWindow.subscriptionsURL) }
            Button("common.cancel", role: .cancel) { }
        } message: {
            Text(LocalizedStringKey(management.hasNoSubscription ? "purchase.manage.empty.detail" : "purchase.manage.failed.detail"))
        }
        // Config can finish refreshing after the parent screen's task. Load
        // when sales become available instead of requiring a second visit.
        .task(id: store.isEnabled) {
            if store.isEnabled { await store.loadCatalogue() }
        }
    }

    @ViewBuilder
    private var catalogue: some View {
        switch store.catalogueState {
        case .idle, .loading:
            if store.offers.isEmpty {
                HStack { ProgressView(); Text("purchase.loading").foregroundStyle(.secondary) }.dsCard()
            } else { products }
        case .failed:
            VStack(alignment: .leading, spacing: DS.Spacing.s) {
                Text("purchase.failed.load").foregroundStyle(.secondary)
                Button("common.retry") { Task { await reloadCatalogue() } }
            }.dsCard()
        case .unavailable: unavailable
        case .loaded: products
        }
    }

    private var unavailable: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.s) {
            Label("purchase.unavailable", systemImage: "bag").foregroundStyle(.secondary)
            Button("common.retry") { Task { await reloadCatalogue() } }
        }.dsCard()
    }

    private func reloadCatalogue() async {
        await account.loadServerConfig()
        await account.refresh()
        await account.loadPlans()
        await store.loadCatalogue()
    }

    @ViewBuilder
    private var products: some View {
        if !packs.isEmpty {
            VStack(alignment: .leading, spacing: DS.Spacing.s) {
                Text("purchase.packs").font(.headline).accessibilityAddTraits(.isHeader)
                Text("purchase.packs.footer").font(.subheadline).foregroundStyle(.secondary)
                ForEach(packs) { offer in packCard(offer) }
            }
        }
        if !passes.isEmpty {
            VStack(alignment: .leading, spacing: DS.Spacing.s) {
                Text("purchase.pass").font(.headline)
                ForEach(passes) { offer in packCard(offer) }
            }
        }
    }

    @ViewBuilder
    private var subscriptionPlans: some View {
        if !planCatalogue.options.isEmpty {
            VStack(alignment: .leading, spacing: DS.Spacing.m) {
                Text("purchase.subscriptions").font(.headline).accessibilityAddTraits(.isHeader)
                if planCatalogue.periods.count > 1 {
                    Picker("purchase.period", selection: $period) {
                        Text("purchase.month").tag("month")
                        Text("purchase.year").tag("year")
                    }.pickerStyle(.segmented)
                }
                ForEach(planCatalogue.visible(period: period)) { option in
                    subscriptionCard(option).id(option.id)
                }
            }
        }
    }

    private var subscriptions: [PurchaseOffer] { store.offers.filter { $0.kind == .subscription } }
    private var packs: [PurchaseOffer] { store.offers.filter { $0.kind == .pack } }
    private var passes: [PurchaseOffer] { store.offers.filter { $0.kind == .pass } }
    private var planCatalogue: SubscriptionPlanCatalogue {
        SubscriptionPlanCatalogue(plans: account.plans, offers: subscriptions)
    }

    private func freeCard(_ plan: AccountAPI.Plan) -> some View {
        VStack(alignment: .leading, spacing: DS.Spacing.s) {
            Text(verbatim: plan.localizedName(settings.effectiveLanguage.rawValue)).font(.title3.bold())
            if let welcome = account.usage.welcome, welcome.total > 0 {
                Text("purchase.welcome.once \(welcome.total)")
                    .font(.subheadline).foregroundStyle(.secondary)
            } else {
                Text(verbatim: plan.localizedDescription(settings.effectiveLanguage.rawValue))
                    .font(.subheadline).foregroundStyle(.secondary)
            }
        }.dsCard()
    }

    private func subscriptionCard(_ option: SubscriptionPlanCatalogue.Option) -> some View {
        let current = account.subscription?.status == "active" && account.subscription?.plan.id == option.planID
        return VStack(alignment: .leading, spacing: DS.Spacing.s) {
            HStack(alignment: .top) {
                VStack(alignment: .leading, spacing: DS.Spacing.xxs) {
                    Text(verbatim: option.name(settings.effectiveLanguage.rawValue)).font(.title3.bold())
                    if option.allowance == .unlimited {
                        Label("purchase.unlimited", systemImage: "infinity")
                            .foregroundStyle(Color.accentColor).font(.subheadline)
                    } else if case .daily(let limit) = option.allowance {
                        if option.offer.creditsNeverExpire {
                            Text("subscription.dailyAccrual \(limit)").font(.subheadline).foregroundStyle(.secondary)
                        } else {
                            Text("subscription.perDay \(limit)").font(.subheadline).foregroundStyle(.secondary)
                        }
                    } else if case .monthly(let limit) = option.allowance {
                        if option.offer.creditsNeverExpire {
                            Text("subscription.monthlyAccrual \(limit)").font(.subheadline).foregroundStyle(.secondary)
                        } else {
                            Text("subscription.perMonth \(limit)").font(.subheadline).foregroundStyle(.secondary)
                        }
                    }
                }
                Spacer(minLength: DS.Spacing.s)
                if current {
                    Image(systemName: "checkmark.circle.fill").foregroundStyle(Color.accentColor)
                        .accessibilityLabel("subscription.currentBadge")
                }
            }
            Text(option.billingPeriod == "year" ? LocalizedStringKey("purchase.year") : LocalizedStringKey("purchase.month"))
                .font(.subheadline).foregroundStyle(.secondary)
            Text(verbatim: option.offer.displayPrice).font(.title2.weight(.semibold))
            Text(option.offer.billingPeriod == "year" ? LocalizedStringKey("purchase.billed.year") : LocalizedStringKey("purchase.billed.month"))
                .font(.footnote).foregroundStyle(.secondary)
            if option.offer.creditsNeverExpire {
                Text("subscription.repliesNeverExpire")
                    .font(.footnote).foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            buyButton(option.offer, current: current)
            if option.offer.hasPriceMismatch { mismatchNotice }
        }
        .dsCard()
        .overlay(RoundedRectangle(cornerRadius: DS.Radius.large).strokeBorder(current || selectedOfferID == option.id ? Color.accentColor : Color.clear, lineWidth: 1.5))
    }

    private func packCard(_ offer: PurchaseOffer) -> some View {
        VStack(alignment: .leading, spacing: DS.Spacing.s) {
            HStack(alignment: .firstTextBaseline) {
                Text(verbatim: offer.name).font(.headline)
                Spacer(minLength: DS.Spacing.s)
                Text(verbatim: offer.displayPrice).font(.headline.monospacedDigit())
            }
            Text("purchase.units \(offer.units)").font(.subheadline)
            Text(offer.kind == .pack ? LocalizedStringKey("purchase.pack.detail") : LocalizedStringKey("purchase.pass.hours \(offer.durationHours)"))
                .font(.footnote).foregroundStyle(.secondary)
            buyButton(offer)
            if offer.hasPriceMismatch { mismatchNotice }
        }.dsCard()
    }

    private var mismatchNotice: some View {
        Text("purchase.priceMismatch").font(.footnote).foregroundStyle(.secondary)
    }

    private func buyButton(_ offer: PurchaseOffer, current: Bool = false) -> some View {
        Button {
            Task {
                await store.purchase(offer)
                if store.status == .granted {
                    await account.refresh()
                    await store.loadCatalogue()
                }
            }
        } label: {
            HStack(spacing: DS.Spacing.xs) {
                if store.status == .purchasing(productID: offer.storeProductID) || store.status == .verifying(productID: offer.storeProductID) {
                    ProgressView().tint(.white)
                }
                Text(current ? LocalizedStringKey("subscription.currentBadge") :
                     (offer.kind == .subscription ? LocalizedStringKey("purchase.subscribe") : LocalizedStringKey("purchase.buyPack")))
            }
        }
        .buttonStyle(.dsPrimary)
        .disabled(current || store.status.isBusy || !store.canMakePayments || offer.hasPriceMismatch)
    }

    private func recommendedOffer(_ recommendation: PurchaseAPI.Recommendation) -> PurchaseOffer? {
        guard !account.usage.unlimited,
              !(account.subscription?.status == "active" && account.subscription?.plan.isFree == false) else { return nil }
        return subscriptions.first {
            $0.storeProductID == recommendation.baseStoreProductID && $0.billingPeriod == "month"
                && (($0.entitlementKind == "daily" && $0.dailyLimit > 0) || ($0.entitlementKind == "monthly" && $0.monthlyLimit > 0))
                && !$0.hasPriceMismatch
        }
    }

    private func recommendationCard(_ recommendation: PurchaseAPI.Recommendation, offer: PurchaseOffer) -> some View {
        VStack(alignment: .leading, spacing: DS.Spacing.s) {
            Label("purchase.recommendation.title", systemImage: "sparkles").font(.headline)
            Group {
                if offer.monthlyLimit > 0 {
                    Text("purchase.recommendation.bodyMonthly \(formattedSpend(recommendation)) \(offer.displayPrice) \(offer.monthlyLimit)")
                } else {
                    Text("purchase.recommendation.body \(formattedSpend(recommendation)) \(offer.displayPrice) \(offer.dailyLimit)")
                }
            }
                .font(.subheadline).fixedSize(horizontal: false, vertical: true)
            if offer.creditsNeverExpire {
                Text("subscription.repliesNeverExpire")
                    .font(.footnote).foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Button("purchase.recommendation.choose") {
                period = "month"
                selectedOfferID = offer.id
                Task { @MainActor in
                    await Task.yield()
                    onSelectOffer(offer.id)
                }
            }.buttonStyle(.dsOutline)
            Button("purchase.recommendation.dismiss") { store.dismissRecommendation() }
                .buttonStyle(.dsText)
        }
        .dsCard()
    }

    private func formattedSpend(_ recommendation: PurchaseAPI.Recommendation) -> String {
        let formatter = NumberFormatter()
        formatter.locale = settings.locale
        formatter.numberStyle = .currency
        formatter.currencyCode = recommendation.currency
        if recommendation.currency == "KZT" { formatter.currencySymbol = "₸" }
        let value = NSDecimalNumber(value: recommendation.spentMinor).dividing(by: 100)
        return formatter.string(from: value) ?? "\(value) \(recommendation.currency)"
    }

    private var statusCard: some View {
        HStack(alignment: .top, spacing: DS.Spacing.s) {
            if store.status.isBusy { ProgressView() }
            Text(statusKey).font(.subheadline).fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .dsCard()
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.updatesFrequently)
    }

    private var statusKey: LocalizedStringKey {
        switch store.status {
        case .idle: return ""
        case .purchasing: return "purchase.status.purchasing"
        case .verifying: return "purchase.status.verifying"
        case .granted: return "purchase.status.granted"
        case .restoring: return "purchase.status.restoring"
        case .restored: return "purchase.status.restored"
        case .nothingToRestore: return "purchase.status.noRestore"
        case .pending: return "purchase.status.pending"
        case .cancelled: return "purchase.status.cancelled"
        case .failed(let reason):
            switch reason {
            case .store: return "purchase.status.failed"
            case .notConfirmed: return "purchase.status.notConfirmed"
            case .anotherAccount: return "purchase.status.anotherAccount"
            case .willRetry: return "purchase.status.willRetry"
            case .unavailable: return "purchase.unavailable"
            }
        }
    }

    private func legalURL(_ raw: String) -> URL? {
        guard var components = URLComponents(string: raw), components.scheme == "https" else { return nil }
        components.queryItems = [URLQueryItem(name: "lang", value: settings.effectiveLanguage.rawValue)]
        guard let url = components.url, case .web = DeepLink.destination(for: url.absoluteString) else { return nil }
        return url
    }
}

/// Resolve the scene from this view, including iPhone compatibility windows on
/// iPad, so StoreKit uses the active scene without a nested SwiftUI sheet.
private final class SubscriptionWindow {
    weak var window: UIWindow?
    static let subscriptionsURL = URL(string: "https://apps.apple.com/account/subscriptions")!
}

private struct SubscriptionWindowReader: UIViewRepresentable {
    let anchor: SubscriptionWindow

    func makeUIView(context: Context) -> WindowView {
        let view = WindowView()
        view.anchor = anchor
        return view
    }

    func updateUIView(_ uiView: WindowView, context: Context) {
        uiView.anchor = anchor
        anchor.window = uiView.window
    }

    final class WindowView: UIView {
        var anchor: SubscriptionWindow?

        override func didMoveToWindow() {
            super.didMoveToWindow()
            anchor?.window = window
        }
    }
}
