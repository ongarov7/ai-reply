import SwiftUI

/// «Докупить ответы»: reply packs and the pass, at the App Store's prices.
///
/// «Докупить ответы»: жауап пакеттері мен рұқсаттама, бағасы App Store-дан.
///
/// Reached from the exhausted sheet, the plan screen and
/// `aireply://purchase`, and only while `features.iap_ios` is on. Every
/// product says what it gives - how many replies, for how long - and every
/// outcome is said as it is: waiting for approval, cancelled, refused, or
/// paid and still being confirmed.
struct PurchaseView: View {

    @Environment(PurchaseStore.self) private var store
    @Environment(AccountModel.self) private var account

    var body: some View {
        List {
            if store.status != .idle {
                statusSection
            }
            if !store.canMakePayments, store.isEnabled {
                Section {
                    Label("purchase.cannotPay", systemImage: "lock")
                        .foregroundStyle(.secondary)
                }
            }
            if store.isEnabled {
                catalogue
            } else {
                unavailableSection
            }
            Section {
            } footer: {
                footer
            }
        }
        .listStyle(.insetGrouped)
        .navigationTitle("purchase.title")
        .navigationBarTitleDisplayMode(.inline)
        .task { await store.loadCatalogue() }
        .onDisappear { store.dismissStatus() }
    }

    // MARK: Products

    @ViewBuilder
    private var catalogue: some View {
        switch store.catalogueState {
        // Before the first answer; a reload keeps the products on screen.
        case .idle, .loading:
            if store.offers.isEmpty {
                Section {
                    HStack(spacing: DS.Spacing.s) {
                        ProgressView()
                        Text("purchase.loading").foregroundStyle(.secondary)
                    }
                }
            } else {
                offerSections
            }
        case .failed:
            Section {
                Text("purchase.failed.load").foregroundStyle(.secondary)
                Button("common.retry") { Task { await store.loadCatalogue() } }
            }
        case .unavailable:
            unavailableSection
        case .loaded:
            offerSections
        }
    }

    @ViewBuilder
    private var offerSections: some View {
        if !packs.isEmpty {
            Section {
                ForEach(packs) { offerRow($0) }
            } header: {
                Text("purchase.packs")
            } footer: {
                Text("purchase.packs.footer")
            }
        }
        if !passes.isEmpty {
            Section {
                ForEach(passes) { offerRow($0) }
            } header: {
                Text("purchase.pass")
            } footer: {
                Text("purchase.pass.footer")
            }
        }
    }

    private var packs: [PurchaseOffer] { store.offers.filter { $0.kind == .pack } }
    private var passes: [PurchaseOffer] { store.offers.filter { $0.kind == .pass } }

    private var unavailableSection: some View {
        Section {
            Label("purchase.unavailable", systemImage: "bag")
                .foregroundStyle(.secondary)
        }
    }

    private func offerRow(_ offer: PurchaseOffer) -> some View {
        HStack(alignment: .center, spacing: DS.Spacing.s) {
            Image(systemName: offer.kind == .pack ? "square.stack.3d.up" : "clock")
                .foregroundStyle(Color.accentColor)
                .frame(width: 28)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: offer.name)
                    .font(.body.weight(.semibold))
                conditions(offer)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: DS.Spacing.s)
            Button {
                Task { await store.purchase(offer) }
            } label: {
                if store.status == .purchasing(productID: offer.storeProductID)
                    || store.status == .verifying(productID: offer.storeProductID) {
                    ProgressView()
                } else {
                    Text(verbatim: offer.displayPrice)
                        .font(.subheadline.weight(.semibold).monospacedDigit())
                }
            }
            .buttonStyle(.bordered)
            .buttonBorderShape(.capsule)
            .disabled(store.status.isBusy || !store.canMakePayments)
            .accessibilityLabel(Text(verbatim: "\(offer.name), \(offer.displayPrice)"))
        }
        .padding(.vertical, DS.Spacing.xxs)
    }

    /// What the product gives: «50 ответов · не сгорают», «100 ответов · на 24 часа».
    private func conditions(_ offer: PurchaseOffer) -> Text {
        let units = Text("purchase.units \(offer.units)")
        switch offer.kind {
        case .pack: return units + Text(verbatim: " · ") + Text("purchase.pack.detail")
        case .pass: return units + Text(verbatim: " · ") + Text("purchase.pass.hours \(offer.durationHours)")
        }
    }

    // MARK: Outcome

    private var statusSection: some View {
        Section {
            switch store.status {
            case .idle:
                EmptyView()
            case .purchasing:
                HStack(spacing: DS.Spacing.s) {
                    ProgressView()
                    Text("purchase.status.purchasing")
                }
            case .verifying:
                HStack(spacing: DS.Spacing.s) {
                    ProgressView()
                    Text("purchase.status.verifying")
                }
            case .granted:
                Label("purchase.status.granted", systemImage: "checkmark.circle.fill")
                    .foregroundStyle(Color.green)
            case .pending:
                Label("purchase.status.pending", systemImage: "hourglass")
            case .cancelled:
                Label("purchase.status.cancelled", systemImage: "xmark.circle")
                    .foregroundStyle(.secondary)
            case .failed(let failure):
                Label(failureKey(failure), systemImage: "exclamationmark.triangle")
                    .foregroundStyle(failure == .willRetry ? Color.secondary : Color.red)
            }
        }
    }

    private func failureKey(_ failure: PurchaseStore.Failure) -> LocalizedStringKey {
        switch failure {
        case .store:          return "purchase.status.failed"
        case .notConfirmed:   return "purchase.status.notConfirmed"
        case .anotherAccount: return "purchase.status.anotherAccount"
        case .willRetry:      return "purchase.status.willRetry"
        case .unavailable:    return "purchase.unavailable"
        }
    }

    // MARK: Footer

    @ViewBuilder
    private var footer: some View {
        let identifier = account.displayIdentifier
        if identifier.isEmpty {
            Text("purchase.footer")
        } else {
            Text("purchase.footer.account \(identifier)")
        }
    }
}
