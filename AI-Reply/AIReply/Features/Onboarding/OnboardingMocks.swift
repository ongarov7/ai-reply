import SwiftUI

/// A drawn sketch of a chat app with the keyboard under it, for the welcome,
/// tutorial and practice steps.
///
/// Чат пен пернетақтаның сызбасы: суреттер жоқ, жүйелік түстер, қараңғы режимде
/// де дұрыс көрінеді.
///
/// Built from system colours and text styles rather than screenshots, so it
/// follows dark mode and the app's language. The keyboard part uses the real
/// keyboard's own words (`AIReplyStrings`) and persona names, so what the user
/// learns here is what they will see. Text inside it stops growing at a size
/// the sketch can hold; the steps around it carry the full Dynamic Type range.
struct MockDevice<Chat: View, Keyboard: View>: View {
    @ViewBuilder var chat: Chat
    @ViewBuilder var keyboard: Keyboard

    var body: some View {
        VStack(spacing: 0) {
            chat
                .padding(DS.Spacing.s)
                .frame(maxWidth: .infinity, alignment: .leading)
            keyboard
                .padding(DS.Spacing.xs)
                .frame(maxWidth: .infinity)
                .background(MockStyle.keyboardBackground)
        }
        .background(Color(.systemBackground))
        .clipShape(RoundedRectangle(cornerRadius: DS.Radius.large, style: .continuous))
        .overlay(
            RoundedRectangle(cornerRadius: DS.Radius.large, style: .continuous)
                .stroke(Color.dsSeparator, lineWidth: 1)
        )
        .frame(maxWidth: 360)
        .frame(maxWidth: .infinity)
        .dynamicTypeSize(...DynamicTypeSize.xxLarge)
        // With Russian typesetting SwiftUI sometimes measures a wrapped line
        // narrower than it draws it, and the sketch's last word ends in "…".
        // Plain typesetting measures and draws the same lines.
        .typesettingLanguage(Locale.Language(identifier: "en"))
    }
}

enum MockStyle {
    static let keyboardBackground = Color(.systemGray4)
    static let key = Color.dsSurface
    static let incomingBubble = Color(.systemGray5)
}

/// The other person's message, optionally with something under it - the
/// iOS Copy menu, or a "Copied" confirmation.
struct MockIncomingBubble<Accessory: View>: View {
    let text: String
    var isHighlighted = false
    @ViewBuilder var accessory: Accessory

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.xs) {
            Text(verbatim: text)
                .font(.subheadline)
                .foregroundStyle(.primary)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.horizontal, DS.Spacing.s)
                .padding(.vertical, DS.Spacing.xs)
                .background(
                    RoundedRectangle(cornerRadius: DS.Radius.large, style: .continuous)
                        .fill(MockStyle.incomingBubble)
                )
                .overlay(
                    RoundedRectangle(cornerRadius: DS.Radius.large, style: .continuous)
                        .stroke(Color.accentColor, lineWidth: isHighlighted ? 2 : 0)
                )
            accessory
        }
        .frame(maxWidth: 280, alignment: .leading)
    }
}

extension MockIncomingBubble where Accessory == EmptyView {
    init(text: String, isHighlighted: Bool = false) {
        self.init(text: text, isHighlighted: isHighlighted) { EmptyView() }
    }
}

/// The chat's own text field: a placeholder, or the reply once it is inserted.
struct MockMessageField: View {
    var text: String?

    var body: some View {
        Group {
            if let text {
                Text(verbatim: text).foregroundStyle(.primary)
            } else {
                Text("onboarding.mock.field").foregroundStyle(.tertiary)
            }
        }
        .font(.footnote)
        .fixedSize(horizontal: false, vertical: true)
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, DS.Spacing.s)
        .padding(.vertical, DS.Spacing.xs)
        .overlay(
            RoundedRectangle(cornerRadius: DS.Radius.large, style: .continuous)
                .stroke(Color.dsSeparator, lineWidth: 1)
        )
        .padding(.top, DS.Spacing.s)
    }
}

/// The iOS long-press menu, reduced to the one item that matters here.
struct MockCopyMenu: View {
    var isHighlighted = false

    var body: some View {
        Label("common.copy", systemImage: "doc.on.doc")
            .font(.footnote.weight(.medium))
            .foregroundStyle(.primary)
            .padding(.horizontal, DS.Spacing.s)
            .frame(minHeight: 32)
            .background(
                RoundedRectangle(cornerRadius: DS.Radius.medium, style: .continuous)
                    .fill(Color.dsSurface)
                    .shadow(color: .black.opacity(0.12), radius: 6, y: 2)
            )
            .overlay(
                RoundedRectangle(cornerRadius: DS.Radius.medium, style: .continuous)
                    .stroke(Color.accentColor, lineWidth: isHighlighted ? 2 : 0)
            )
    }
}

/// "Copied" under the bubble once the message is on the clipboard.
struct MockCopiedBadge: View {
    var body: some View {
        Label("onboarding.mock.copied", systemImage: "checkmark.circle.fill")
            .font(.caption.weight(.medium))
            .foregroundStyle(.green)
    }
}

/// AI Reply's persona row, with the real built-in names in the app's
/// language. `onSelect` makes the pills tappable (practice); without it they
/// are a picture (tutorial).
struct MockPersonaRow: View {
    let language: AppLanguage
    var highlighted: RelationshipKind?
    var onSelect: ((RelationshipKind) -> Void)?

    var body: some View {
        HStack(spacing: DS.Spacing.xxs) {
            ForEach(RelationshipKind.builtIns, id: \.self) { kind in
                pill(kind)
            }
        }
    }

    @ViewBuilder
    private func pill(_ kind: RelationshipKind) -> some View {
        let name = ReplyTemplate.builtIn(kind, sortIndex: 0).displayName(appLanguage: language)
        let label = Text(verbatim: name)
            .font(.footnote.weight(.medium))
            .lineLimit(1)
            .minimumScaleFactor(0.7)
            .foregroundStyle(kind == highlighted ? Color.white : Color.primary)
            .padding(.horizontal, DS.Spacing.xs)
            .frame(maxWidth: .infinity, minHeight: 30)
            .background(Capsule().fill(kind == highlighted ? Color.accentColor : MockStyle.key))
        if let onSelect {
            Button { onSelect(kind) } label: {
                label
                    .frame(minHeight: DS.Layout.minimumTouchTarget)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
        } else {
            label
        }
    }
}

/// Three rows of blank keys and a bottom row with the globe key - enough to
/// read as a keyboard without pretending to be one.
struct MockKeys<GlobeKey: View>: View {
    @ViewBuilder var globeKey: GlobeKey

    var body: some View {
        VStack(spacing: DS.Spacing.xxs) {
            VStack(spacing: DS.Spacing.xxs) {
                ForEach([10, 9, 7], id: \.self) { count in
                    HStack(spacing: DS.Spacing.xxs) {
                        ForEach(0..<count, id: \.self) { _ in key }
                    }
                    .padding(.horizontal, count == 10 ? 0 : CGFloat(10 - count) * 6)
                }
            }
            .accessibilityHidden(true)
            // The globe slot stays visible to VoiceOver: in the practice it is
            // a real button.
            HStack(spacing: DS.Spacing.xxs) {
                globeKey
                key.accessibilityHidden(true)
            }
        }
    }

    private var key: some View {
        RoundedRectangle(cornerRadius: 4, style: .continuous)
            .fill(MockStyle.key)
            .frame(height: 22)
    }
}

extension MockKeys where GlobeKey == MockGlobeKey {
    init(highlightsGlobe: Bool = false) {
        self.init { MockGlobeKey(isHighlighted: highlightsGlobe) }
    }
}

struct MockGlobeKey: View {
    var isHighlighted = false

    var body: some View {
        Image(systemName: "globe")
            .font(.footnote)
            .foregroundStyle(isHighlighted ? Color.white : Color.primary)
            .frame(width: 36, height: 22)
            .background(
                RoundedRectangle(cornerRadius: 4, style: .continuous)
                    .fill(isHighlighted ? Color.accentColor : MockStyle.key)
            )
            .accessibilityHidden(true)
    }
}

/// The AI Reply panel that opens over the keys: the copied message, what the
/// user asked for, and the one button that matters at this moment.
struct MockComposer<Action: View>: View {
    let strings: AIReplyStrings
    let source: String
    /// The instruction typed so far; nil shows the field's placeholder.
    var instruction: String?
    /// The written reply, once there is one.
    var draft: String?
    @ViewBuilder var action: Action

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.xs) {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: strings.copiedMessage)
                    .font(.caption2.weight(.semibold))
                    .foregroundStyle(.secondary)
                Text(verbatim: source)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }
            Group {
                if let draft {
                    Text(verbatim: draft).foregroundStyle(.primary)
                } else if let instruction {
                    Text(verbatim: instruction).foregroundStyle(.primary)
                } else {
                    Text(verbatim: strings.instructionPlaceholder).foregroundStyle(.tertiary)
                }
            }
            .font(.footnote)
            .fixedSize(horizontal: false, vertical: true)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(DS.Spacing.xs)
            .background(
                RoundedRectangle(cornerRadius: DS.Radius.small, style: .continuous)
                    .fill(MockStyle.key)
            )
            HStack {
                Spacer(minLength: 0)
                action
            }
        }
    }
}

/// The composer's primary button - Reply or Insert - drawn like the
/// keyboard's own, with a 44 pt target when it is live.
struct MockActionButton: View {
    let title: String
    var action: (() -> Void)?

    var body: some View {
        if let action {
            Button(action: action) {
                label.frame(minHeight: DS.Layout.minimumTouchTarget).contentShape(Rectangle())
            }
            .buttonStyle(.plain)
        } else {
            label
        }
    }

    private var label: some View {
        Text(verbatim: title)
            .font(.footnote.weight(.semibold))
            .foregroundStyle(Color.white)
            .padding(.horizontal, DS.Spacing.m)
            .frame(minHeight: 30)
            .background(Capsule().fill(Color.accentColor))
    }
}
