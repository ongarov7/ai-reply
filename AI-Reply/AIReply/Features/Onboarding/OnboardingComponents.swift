import SwiftUI

/// The pieces every onboarding page shares: the page itself with its buttons
/// pinned at the bottom, chat bubbles, the tutorial's header and dots.
///
/// Онбординг беттерінің ортақ бөліктері.

/// A scrolling page on the grouped background, its buttons pinned under it.
///
/// Each time `reveal` changes, the view marked `.onboardingRevealed()`
/// scrolls fully into view: a change below the fold of a small screen is
/// seen where it happens.
struct OnboardingPage<Content: View, Buttons: View>: View {
    var reveal: AnyHashable? = nil
    @ViewBuilder var content: Content
    @ViewBuilder var buttons: Buttons

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                content
                    .padding(.horizontal, DS.Spacing.l)
                    .padding(.vertical, DS.Spacing.m)
                    .frame(maxWidth: DS.Layout.readableWidth, alignment: .leading)
                    .frame(maxWidth: .infinity)
            }
            .scrollDismissesKeyboard(.interactively)
            .background(Color.dsBackground)
            .safeAreaInset(edge: .bottom) {
                VStack(spacing: DS.Spacing.xxs) { buttons }
                    .padding(.horizontal, DS.Spacing.l)
                    .padding(.top, DS.Spacing.s)
                    .padding(.bottom, DS.Spacing.xs)
                    .frame(maxWidth: DS.Layout.readableWidth)
                    .frame(maxWidth: .infinity)
                    .background(Color.dsBackground)
            }
            .onChange(of: reveal) {
                withAnimation(reduceMotion ? nil : .easeOut(duration: 0.25)) {
                    proxy.scrollTo(OnboardingReveal.id, anchor: .bottom)
                }
            }
        }
    }
}

/// The one view on an `OnboardingPage` that `reveal` scrolls to.
private enum OnboardingReveal {
    static let id = "onboarding.reveal"
}

extension View {
    /// Marks the view an `OnboardingPage` scrolls into view when its
    /// `reveal` changes.
    func onboardingRevealed() -> some View {
        id(OnboardingReveal.id)
    }
}

/// A step's heading and the sentence under it.
struct OnboardingStepTitle: View {
    let heading: LocalizedStringKey
    let prompt: LocalizedStringKey

    init(_ heading: LocalizedStringKey, _ prompt: LocalizedStringKey) {
        self.heading = heading
        self.prompt = prompt
    }

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.xs) {
            Text(heading)
                .font(.title.weight(.bold))
                .accessibilityAddTraits(.isHeader)
            Text(prompt)
                .font(.body)
                .foregroundStyle(.secondary)
        }
    }
}

/// One message of an example chat: the other person's in grey with an
/// avatar, a suggested reply in light blue on the right.
struct OnboardingBubble: View {

    enum Side {
        case incoming
        case reply
    }

    let text: String
    let side: Side

    var body: some View {
        HStack(alignment: .bottom, spacing: DS.Spacing.xs) {
            if side == .incoming {
                DSSymbolDisc(symbol: "person.fill", size: 32)
            } else {
                Spacer(minLength: DS.Spacing.xl)
            }
            Text(verbatim: text)
                .font(.body)
                .foregroundStyle(.primary)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.horizontal, DS.Spacing.s)
                .padding(.vertical, DS.Spacing.xs)
                .background(
                    RoundedRectangle(cornerRadius: 18, style: .continuous)
                        .fill(side == .incoming ? Color(.systemGray5) : Color.accentColor.opacity(0.15))
                )
            if side == .incoming { Spacer(minLength: DS.Spacing.xl) }
        }
        .frame(maxWidth: .infinity, alignment: side == .incoming ? .leading : .trailing)
    }
}

/// "1 из 3" under the tutorial's buttons; the header says it in words.
struct OnboardingPageDots: View {
    let position: Int
    let count: Int

    var body: some View {
        HStack(spacing: DS.Spacing.xs) {
            ForEach(1...count, id: \.self) { index in
                Circle()
                    .fill(index == position ? Color.accentColor : Color(.tertiaryLabel))
                    .frame(width: 7, height: 7)
            }
        }
        .frame(minHeight: DS.Spacing.l)
        .accessibilityHidden(true)
    }
}

extension View {
    /// «Как пользоваться · 1 из 3» in the navigation bar.
    func onboardingTutorialHeader(position: Int, count: Int) -> some View {
        navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .principal) {
                    Text("onboarding.tutorial.header \(position) \(count)")
                        .font(.headline)
                        .lineLimit(1)
                        .minimumScaleFactor(0.75)
                        .accessibilityAddTraits(.isHeader)
                }
            }
    }
}
