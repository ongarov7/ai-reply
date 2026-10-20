import SwiftUI

/// The exhausted sheet's decision (contract §3.9): shown when a generation
/// in the app failed because the replies are spent, and what it offers.
///
/// Лимит біткенде: қосымша жауап сатып алу (болса), тарифтер, «Позже».
struct ExhaustedOffer: Identifiable, Equatable {

    /// «Докупить ответы»: purchases are on for iOS (`features.iap_ios`) and
    /// the server said more can be bought (`details.can_buy`).
    let offersPurchase: Bool

    var id: Bool { offersPurchase }

    /// Only a refusal the server made for a spent limit (DAILY_LIMIT_REACHED
    /// or MONTHLY_LIMIT_REACHED, which leave `refusal`) opens the sheet: an
    /// expired subscription reads the same to the keyboard, but is not this.
    static func decide(failure: AIReplyError?, refusal: LimitRefusal?, purchasesEnabled: Bool) -> ExhaustedOffer? {
        guard failure == .quotaExhausted || failure == .monthlyQuotaExhausted, let refusal else { return nil }
        return ExhaustedOffer(offersPurchase: purchasesEnabled && refusal.canBuy)
    }
}

/// «Лимит ответов исчерпан»: a half-height sheet over the screen that asked,
/// which stays as it was underneath.
struct ExhaustedSheetView: View {

    let offer: ExhaustedOffer
    var onBuy: () -> Void
    var onPlans: () -> Void

    @Environment(\.dismiss) private var dismiss

    var body: some View {
        ScrollView {
            VStack(spacing: DS.Spacing.m) {
                Image(systemName: "hourglass")
                    .font(.system(size: 34, weight: .regular))
                    .foregroundStyle(Color.accentColor)
                    .accessibilityHidden(true)
                VStack(spacing: DS.Spacing.xs) {
                    Text("exhausted.title")
                        .font(.title3.weight(.semibold))
                        .multilineTextAlignment(.center)
                        .accessibilityAddTraits(.isHeader)
                    Text("exhausted.body")
                        .font(.body)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                        .fixedSize(horizontal: false, vertical: true)
                }
                VStack(spacing: DS.Spacing.xs) {
                    if offer.offersPurchase {
                        Button("exhausted.buy", action: onBuy)
                            .buttonStyle(.dsPrimary)
                        Button("exhausted.plans", action: onPlans)
                            .buttonStyle(.dsText)
                    } else {
                        Button("exhausted.plans", action: onPlans)
                            .buttonStyle(.dsPrimary)
                    }
                    Button("exhausted.later") { dismiss() }
                        .buttonStyle(.dsText)
                }
                .padding(.top, DS.Spacing.xs)
            }
            .padding(.horizontal, DS.Spacing.l)
            .padding(.top, DS.Spacing.l)
            .padding(.bottom, DS.Spacing.m)
            .frame(maxWidth: DS.Layout.readableWidth)
            .frame(maxWidth: .infinity)
        }
        .scrollBounceBehavior(.basedOnSize)
        .background(Color.dsBackground)
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
    }
}
