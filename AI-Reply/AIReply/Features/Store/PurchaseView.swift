import SwiftUI
import StoreKit

struct PurchaseView: View {
    @Environment(PurchaseStore.self) private var store

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                PurchaseOptionsView(onSelectOffer: { proxy.scrollTo($0, anchor: .top) })
                    .padding(DS.Spacing.l)
                    .frame(maxWidth: DS.Layout.readableWidth)
                    .frame(maxWidth: .infinity)
            }
        }
        .background(Color.dsBackground)
        .navigationTitle("purchase.title")
        .navigationBarTitleDisplayMode(.inline)
        .task { await store.loadCatalogue() }
        .onDisappear { store.dismissStatus() }
    }
}

/// Products are shown only after both the server and the App Store return
/// them. The server determines access; Apple supplies the checkout price.
struct PurchaseOptionsView: View {
    var onSelectOffer: (String) -> Void = { _ in }
    @Environment(PurchaseStore.self) private var store
    @Environment(AccountModel.self) private var account
    @Environment(AppSettings.self) private var settings
    @State private var period = "month"
    @State private var isManaging = false
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
                    Button("purchase.manage") { isManaging = true }
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
        .manageSubscriptionsSheet(isPresented: $isManaging)
        .onChange(of: isManaging) { _, presented in
            if !presented { Task { await store.processUnfinished(); await account.refresh() } }
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
                Button("common.retry") { Task { await store.loadCatalogue() } }
            }.dsCard()
        case .unavailable: unavailable
        case .loaded: products
        }
    }

    private var unavailable: some View {
        Label("purchase.unavailable", systemImage: "bag").foregroundStyle(.secondary).dsCard()
    }

    @ViewBuilder
    private var products: some View {
        if !subscriptions.isEmpty {
            VStack(alignment: .leading, spacing: DS.Spacing.m) {
                Text("purchase.subscriptions").font(.headline).accessibilityAddTraits(.isHeader)
                if Set(subscriptions.compactMap(\.billingPeriod)).count > 1 {
                    Picker("purchase.period", selection: $period) {
                        Text("purchase.month").tag("month")
                        Text("purchase.year").tag("year")
                    }.pickerStyle(.segmented)
                }
                ForEach(visibleSubscriptions) { offer in subscriptionCard(offer).id(offer.id) }
            }
        }
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

    private var subscriptions: [PurchaseOffer] { store.offers.filter { $0.kind == .subscription } }
    private var packs: [PurchaseOffer] { store.offers.filter { $0.kind == .pack } }
    private var passes: [PurchaseOffer] { store.offers.filter { $0.kind == .pass } }
    private var visibleSubscriptions: [PurchaseOffer] {
        let matches = subscriptions.filter { $0.billingPeriod == period }
        return matches.isEmpty ? subscriptions : matches
    }

    private func subscriptionCard(_ offer: PurchaseOffer) -> some View {
        let current = account.subscription?.status == "active" && account.subscription?.plan.id == offer.planID
        return VStack(alignment: .leading, spacing: DS.Spacing.s) {
            HStack(alignment: .top) {
                VStack(alignment: .leading, spacing: DS.Spacing.xxs) {
                    Text(verbatim: offer.name).font(.title3.bold())
                    if offer.entitlementKind == "unlimited" {
                        Label("purchase.unlimited", systemImage: "infinity")
                            .foregroundStyle(Color.accentColor).font(.subheadline)
                    } else {
                        Text("subscription.perDay \(offer.dailyLimit)").font(.subheadline).foregroundStyle(.secondary)
                    }
                }
                Spacer(minLength: DS.Spacing.s)
                if current {
                    Image(systemName: "checkmark.circle.fill").foregroundStyle(Color.accentColor)
                        .accessibilityLabel("subscription.currentBadge")
                }
            }
            Text(verbatim: offer.displayPrice).font(.title2.weight(.semibold))
            Text(offer.billingPeriod == "year" ? LocalizedStringKey("purchase.billed.year") : LocalizedStringKey("purchase.billed.month"))
                .font(.footnote).foregroundStyle(.secondary)
            buyButton(offer, current: current)
            if offer.hasPriceMismatch { mismatchNotice }
        }
        .dsCard()
        .overlay(RoundedRectangle(cornerRadius: DS.Radius.large).strokeBorder(current || selectedOfferID == offer.id ? Color.accentColor : Color.clear, lineWidth: 1.5))
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
                && $0.entitlementKind == "daily" && $0.dailyLimit > 0 && !$0.hasPriceMismatch
        }
    }

    private func recommendationCard(_ recommendation: PurchaseAPI.Recommendation, offer: PurchaseOffer) -> some View {
        VStack(alignment: .leading, spacing: DS.Spacing.s) {
            Label("purchase.recommendation.title", systemImage: "sparkles").font(.headline)
            Text("purchase.recommendation.body \(formattedSpend(recommendation)) \(offer.displayPrice) \(offer.dailyLimit)")
                .font(.subheadline).fixedSize(horizontal: false, vertical: true)
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
        return components.url
    }
}
