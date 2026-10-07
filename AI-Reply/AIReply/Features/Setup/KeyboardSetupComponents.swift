import SwiftUI
import UIKit

/// The pieces every keyboard-setup screen shares: Home's keyboard card, the
/// setup guide, Settings and the onboarding steps say the same thing the same
/// way.

/// The steps for adding the keyboard in iOS Settings.
struct KeyboardSetupSteps: View {
    /// The short path: iOS lists a keyboard app's own keyboards on the app's
    /// page in Settings, which is the one page an app may open. The long
    /// path through General ▸ Keyboard is offered separately as a fallback.
    private let steps: [LocalizedStringKey] = [
        "home.setup.step.openApp",
        "home.setup.step.appKeyboards",
        "home.setup.step.enable",
        "home.setup.step.fullAccess"
    ]

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.s) {
            ForEach(Array(steps.enumerated()), id: \.offset) { index, step in
                DSStepRow(index: index + 1, text: step)
            }
        }
        .dsCard()
    }
}

/// THE setup action: opens AI Reply's own page in iOS Settings, the one page
/// an app may open - and for a keyboard app the page that lists its keyboard
/// and the Full Access switch. Nothing can turn either on for the user.
struct OpenKeyboardSettingsButton: View {
    var body: some View {
        Button(action: Self.openSystemSettings) {
            Label("home.keyboard.openSettings", systemImage: "keyboard")
        }
        .buttonStyle(.dsPrimary)
    }

    static func openSystemSettings() {
        guard let url = URL(string: UIApplication.openSettingsURLString) else { return }
        UIApplication.shared.open(url)
    }
}

/// One line of what the app knows about its keyboard: enabled, and Full
/// Access. Title over state, so a long translation wraps instead of clipping.
struct KeyboardStatusRow: View {

    enum Kind {
        case keyboard
        case fullAccess
    }

    let kind: Kind
    let status: KeyboardStatus

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: DS.Spacing.s) {
            Image(systemName: symbol)
                .foregroundStyle(tint)
                .frame(width: 22)
            VStack(alignment: .leading, spacing: 2) {
                Text(title).foregroundStyle(.primary)
                Text(state).font(.footnote).foregroundStyle(.secondary)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }

    private enum Reading { case yes, no, unknown }

    private var reading: Reading {
        switch kind {
        case .keyboard:
            return status.isEnabled ? .yes : .unknown
        case .fullAccess:
            switch status.fullAccess {
            case .on:      return .yes
            case .off:     return .no
            case .unknown: return .unknown
            }
        }
    }

    private var symbol: String {
        switch reading {
        case .yes:     return "checkmark.circle.fill"
        case .no:      return "exclamationmark.circle"
        case .unknown: return kind == .keyboard ? "keyboard" : "questionmark.circle"
        }
    }

    private var tint: Color {
        switch reading {
        case .yes:     return .green
        case .no:      return .orange
        case .unknown: return .secondary
        }
    }

    private var title: LocalizedStringKey {
        kind == .keyboard ? "setup.status.keyboard" : "setup.status.fullAccess"
    }

    private var state: LocalizedStringKey {
        switch (kind, reading) {
        case (.keyboard, .yes):       return "setup.status.keyboard.on"
        case (.keyboard, _):          return "setup.status.keyboard.unknown"
        case (.fullAccess, .yes):     return "setup.status.fullAccess.on"
        case (.fullAccess, .no):      return "setup.status.fullAccess.off"
        case (.fullAccess, .unknown): return "setup.status.fullAccess.unknown"
        }
    }
}

/// A field to try the keyboard in without leaving the app. Opening the AI
/// Reply keyboard here is also what lets the status above turn green.
struct KeyboardTestField: View {

    @State private var text = ""

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.s) {
            Text("onboarding.test.prompt").font(.subheadline).foregroundStyle(.secondary)
            TextField("onboarding.test.placeholder", text: $text, axis: .vertical)
                .textFieldStyle(.plain)
                .lineLimit(2...4)
                .padding(DS.Spacing.s)
                .frame(minHeight: DS.Layout.minimumTouchTarget)
                .background(
                    RoundedRectangle(cornerRadius: DS.Radius.medium, style: .continuous)
                        .fill(Color.dsBackground)
                )
            Text("onboarding.test.hint").font(.footnote).foregroundStyle(.secondary)
        }
        .dsCard()
    }
}
