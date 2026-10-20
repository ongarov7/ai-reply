import SwiftUI
import UIKit

/// The pieces every keyboard-setup screen shares: Home's keyboard card, the
/// permissions section, Settings and «Подключи AI Reply» say the same thing
/// the same way.

/// THE setup action: opens AI Reply's own page in iOS Settings, the one page
/// an app may open - and for a keyboard app the page that lists its keyboard
/// and the Full Access switch. Nothing can turn either on for the user.
struct OpenKeyboardSettingsButton: View {
    var body: some View {
        Button(action: Self.openSystemSettings) {
            Label("permissions.openSettings", systemImage: "keyboard")
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
