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

    @MainActor
    static func openSystemSettings() {
        guard let url = URL(string: UIApplication.openSettingsURLString) else { return }
        ProductEvents.track(.keyboardSettingsOpened)
        UIApplication.shared.open(url)
    }
}

/// One line of what the app knows about its keyboard: enabled, and Full
/// Access. Title over state, so a long translation wraps instead of clipping.
///
/// Says what the keyboard was seen doing and when (`KeyboardSighting`):
/// green only for a recent sighting, and then with its time; an older one is
/// "last confirmed …" in grey, never the state now.
struct KeyboardStatusRow: View {

    enum Kind {
        case keyboard
        case fullAccess
    }

    let kind: Kind
    let sighting: KeyboardSighting

    @Environment(\.locale) private var locale

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

    private var symbol: String {
        switch sighting {
        case .confirmed: return "checkmark.circle.fill"
        case .denied:    return "exclamationmark.circle"
        case .outdated:  return "clock.arrow.circlepath"
        case .unknown:   return kind == .keyboard ? "keyboard" : "questionmark.circle"
        }
    }

    private var tint: Color {
        switch sighting {
        case .confirmed:          return .green
        case .denied:             return .orange
        case .outdated, .unknown: return .secondary
        }
    }

    private var title: LocalizedStringKey {
        kind == .keyboard ? "setup.status.keyboard" : "setup.status.fullAccess"
    }

    private var state: LocalizedStringKey {
        let format = KeyboardSightingDate.style.locale(locale)
        switch (kind, sighting) {
        case (.keyboard, .confirmed(let date)):
            return "setup.status.keyboard.on \(date, format: format)"
        case (.keyboard, .outdated(let date)):
            return "setup.status.keyboard.outdated \(date, format: format)"
        case (.keyboard, _):
            return "setup.status.keyboard.unknown"
        case (.fullAccess, .confirmed(let date)):
            return "setup.status.fullAccess.on \(date, format: format)"
        case (.fullAccess, .outdated(let date)):
            return "setup.status.fullAccess.outdated \(date, format: format)"
        case (.fullAccess, .denied):
            return "setup.status.fullAccess.off"
        case (.fullAccess, .unknown):
            return "setup.status.fullAccess.unknown"
        }
    }
}

/// How a sighting's time is written: day, short month and time - "20 окт.,
/// 10:42" - in the app's language.
enum KeyboardSightingDate {
    static let style = Date.FormatStyle.dateTime.day().month(.abbreviated).hour().minute()
}

/// A field to try the keyboard in, and an honest answer: whether the AI
/// Reply keyboard really appeared in it.
///
/// Пернетақтаны тексеру өрісі: AI Reply пернетақтасы шынымен ашылғанын
/// тек оның өз белгісі бойынша айтады.
///
/// Only the keyboard's own Darwin notification, heard after the field was
/// focused, counts: it is posted every time the keyboard appears and says
/// whether it has Full Access. No signal within a few seconds means the
/// system keyboard (or another one) is up, and the field says so.
struct KeyboardCheckField: View {

    var focus: FocusState<Bool>.Binding

    @Environment(KeyboardStatusMonitor.self) private var keyboard
    @State private var text = ""
    /// When the field last got focus: only a signal after it is an answer.
    @State private var focusedAt: Date?
    @State private var waitedLong = false

    /// How long the keyboard may take to appear before "not yet" is said.
    private static let patience: Duration = .seconds(3)

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.s) {
            Text("keyboardCheck.prompt")
                .font(.subheadline)
                .foregroundStyle(.secondary)
            TextField("keyboardCheck.placeholder", text: $text, axis: .vertical)
                .lineLimit(1...3)
                .focused(focus)
                .padding(DS.Spacing.s)
                .frame(minHeight: DS.Layout.minimumTouchTarget)
                .background(
                    RoundedRectangle(cornerRadius: DS.Radius.medium, style: .continuous)
                        .fill(Color(.tertiarySystemFill))
                )
            if let result {
                Label {
                    Text(result.text).font(.footnote)
                } icon: {
                    Image(systemName: result.symbol).foregroundStyle(result.tint)
                }
                .accessibilityElement(children: .combine)
            }
        }
        .onChange(of: focus.wrappedValue) { _, focused in
            guard focused else { return }
            focusedAt = Date()
            waitedLong = false
        }
        .task(id: focusedAt) {
            guard focusedAt != nil else { return }
            try? await Task.sleep(for: Self.patience)
            guard !Task.isCancelled else { return }
            waitedLong = true
        }
    }

    private var outcome: KeyboardCheckOutcome {
        KeyboardCheckOutcome(focusedAt: focusedAt, signal: keyboard.lastSignal, waitedLong: waitedLong)
    }

    private var result: (text: LocalizedStringKey, symbol: String, tint: Color)? {
        switch outcome {
        case .idle, .waiting: return nil
        case .notYet:         return ("keyboardCheck.notYet", "keyboard", .secondary)
        case .fullAccess:     return ("keyboardCheck.full", "checkmark.circle.fill", .green)
        case .limited:        return ("keyboardCheck.limited", "exclamationmark.circle.fill", .orange)
        }
    }
}

/// What the keyboard check can say, decided without the view so it is tested.
enum KeyboardCheckOutcome: Equatable {
    /// The field was never focused.
    case idle
    /// Focused a moment ago; the keyboard may still be on its way.
    case waiting
    /// Focused for a while and the AI Reply keyboard did not announce itself.
    case notYet
    case fullAccess
    case limited

    init(focusedAt: Date?, signal: KeyboardSignal?, waitedLong: Bool) {
        guard let focusedAt else {
            self = .idle
            return
        }
        if let signal, signal.at >= focusedAt {
            self = signal.hasFullAccess ? .fullAccess : .limited
        } else {
            self = waitedLong ? .notYet : .waiting
        }
    }
}
