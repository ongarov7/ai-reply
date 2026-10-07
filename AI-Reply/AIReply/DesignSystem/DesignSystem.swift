import SwiftUI

/// A deliberately small design system.
///
/// Semantic system colours and Dynamic Type text styles already do most of the
/// work, and reproducing them behind custom names would only make the app drift
/// away from the platform. What lives here is the handful of decisions SwiftUI
/// does not make for us: vertical rhythm, corner radius, and the two button
/// shapes this app actually uses.
enum DS {

    enum Spacing {
        static let xxs: CGFloat = 4
        static let xs: CGFloat = 8
        static let s: CGFloat = 12
        static let m: CGFloat = 16
        static let l: CGFloat = 24
        static let xl: CGFloat = 32
    }

    enum Radius {
        static let small: CGFloat = 8
        static let medium: CGFloat = 12
        static let large: CGFloat = 16
    }

    enum Layout {
        /// Text stops growing past this width so lines stay readable on a
        /// Pro Max the same way they do on an SE.
        static let readableWidth: CGFloat = 560
        /// Apple's minimum comfortable hit target.
        static let minimumTouchTarget: CGFloat = 44
    }
}

// MARK: - Surfaces

extension Color {
    /// Page background.
    static var dsBackground: Color { Color(.systemGroupedBackground) }
    /// Raised surface sitting on the page background.
    static var dsSurface: Color { Color(.secondarySystemGroupedBackground) }
    /// Hairline between rows.
    static var dsSeparator: Color { Color(.separator) }
}

/// Card surface: one radius, one background, no border and no shadow.
/// Depth on iOS comes from the background contrast, not from drop shadows.
private struct DSCardModifier: ViewModifier {
    func body(content: Content) -> some View {
        content
            .padding(DS.Spacing.m)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(
                RoundedRectangle(cornerRadius: DS.Radius.large, style: .continuous)
                    .fill(Color.dsSurface)
            )
    }
}

extension View {
    func dsCard() -> some View { modifier(DSCardModifier()) }
}

/// Centers short account flows and naturally becomes scrollable when the
/// keyboard, Dynamic Type or a compact screen leaves less vertical room.
struct AuthScreen<Content: View>: View {
    @ViewBuilder let content: Content

    var body: some View {
        GeometryReader { geometry in
            ScrollView {
                content
                    .padding(DS.Spacing.l)
                    .frame(maxWidth: DS.Layout.readableWidth, alignment: .leading)
                    .frame(maxWidth: .infinity)
                    .frame(minHeight: geometry.size.height, alignment: .center)
            }
            .scrollDismissesKeyboard(.interactively)
        }
        .background(Color.dsBackground)
    }
}

// MARK: - Sections

/// A titled block of content. The title is a real section header, so VoiceOver
/// announces it as one.
struct DSSection<Content: View>: View {
    let title: LocalizedStringKey
    @ViewBuilder var content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.s) {
            Text(title)
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(.secondary)
                .accessibilityAddTraits(.isHeader)
            content
        }
    }
}

struct TonePicker: View {
    @Binding var selection: ReplyTone

    private let columns = [GridItem(.adaptive(minimum: 118), spacing: DS.Spacing.xs)]

    var body: some View {
        LazyVGrid(columns: columns, alignment: .leading, spacing: DS.Spacing.xs) {
            ForEach(ReplyTone.allCases) { tone in
                Button {
                    selection = tone
                } label: {
                    HStack(spacing: DS.Spacing.xs) {
                        Image(systemName: selection == tone ? "checkmark.circle.fill" : "circle")
                        Text(tone.titleKey)
                            .lineLimit(2)
                            .multilineTextAlignment(.leading)
                        Spacer(minLength: 0)
                    }
                    .font(.subheadline.weight(.medium))
                    .foregroundStyle(selection == tone ? Color.accentColor : Color.primary)
                    .padding(.horizontal, DS.Spacing.s)
                    .frame(maxWidth: .infinity, minHeight: DS.Layout.minimumTouchTarget, alignment: .leading)
                    .background(
                        RoundedRectangle(cornerRadius: DS.Radius.small, style: .continuous)
                            .fill(selection == tone ? Color.accentColor.opacity(0.12) : Color.dsSurface)
                    )
                }
                .buttonStyle(.plain)
            }
        }
    }
}

// MARK: - Buttons

/// Filled button for the single most important action on a screen.
///
/// The body is a nested `View` rather than inline chrome because `ButtonStyle`
/// is not itself a `View`: `@Environment` read directly on the style is never
/// updated, so a disabled button would not dim.
struct DSPrimaryButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        StyleBody(configuration: configuration)
    }

    private struct StyleBody: View {
        let configuration: ButtonStyleConfiguration
        @Environment(\.isEnabled) private var isEnabled

        var body: some View {
            configuration.label
                .font(.body.weight(.semibold))
                .foregroundStyle(Color.white)
                .frame(maxWidth: .infinity, minHeight: DS.Layout.minimumTouchTarget)
                .background(
                    RoundedRectangle(cornerRadius: DS.Radius.medium, style: .continuous)
                        .fill(Color.accentColor)
                )
                .opacity(isEnabled ? (configuration.isPressed ? 0.82 : 1) : 0.4)
        }
    }
}

/// Quiet button for everything else.
struct DSSecondaryButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        StyleBody(configuration: configuration)
    }

    private struct StyleBody: View {
        let configuration: ButtonStyleConfiguration
        @Environment(\.isEnabled) private var isEnabled

        var body: some View {
            configuration.label
                .font(.body.weight(.medium))
                .foregroundStyle(Color.accentColor)
                .frame(minHeight: DS.Layout.minimumTouchTarget)
                .padding(.horizontal, DS.Spacing.m)
                .background(
                    RoundedRectangle(cornerRadius: DS.Radius.medium, style: .continuous)
                        .fill(Color.dsSurface)
                )
                .opacity(isEnabled ? (configuration.isPressed ? 0.7 : 1) : 0.4)
        }
    }
}

extension ButtonStyle where Self == DSPrimaryButtonStyle {
    static var dsPrimary: DSPrimaryButtonStyle { DSPrimaryButtonStyle() }
}

extension ButtonStyle where Self == DSSecondaryButtonStyle {
    static var dsSecondary: DSSecondaryButtonStyle { DSSecondaryButtonStyle() }
}

// MARK: - Numbered step

/// One step of the keyboard setup instructions.
struct DSStepRow: View {
    let index: Int
    let text: LocalizedStringKey

    /// Grows with the number inside it, so large text never spills out.
    @ScaledMetric(relativeTo: .footnote) private var badgeSize: CGFloat = 22

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: DS.Spacing.s) {
            Text("\(index)")
                .font(.footnote.weight(.semibold))
                .monospacedDigit()
                .foregroundStyle(Color.dsSurface)
                .frame(width: badgeSize, height: badgeSize)
                .background(Circle().fill(Color.primary))
                .accessibilityHidden(true)
            Text(text)
                .font(.body)
                .foregroundStyle(.primary)
        }
        .accessibilityElement(children: .combine)
    }
}
