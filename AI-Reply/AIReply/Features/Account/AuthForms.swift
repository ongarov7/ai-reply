import SwiftUI

// MARK: - Pure rules

/// E-mail input as the app sees it. The server normalizes and validates for
/// real; this only decides when Continue can be tapped.
///
/// Пошта: бос орынсыз, кіші әріппен. Нақты тексеру — серверде.
enum EmailAddress {

    /// Trimmed and lowercased, the same rule the server applies.
    static func normalized(_ raw: String) -> String {
        raw.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
    }

    /// Looks like one address: something@domain.tld, no spaces.
    static func isPlausible(_ raw: String) -> Bool {
        let value = normalized(raw)
        guard !value.isEmpty, value.count <= 254, !value.contains(where: \.isWhitespace) else { return false }
        let parts = value.split(separator: "@", omittingEmptySubsequences: false)
        guard parts.count == 2, !parts[0].isEmpty else { return false }
        let domain = parts[1]
        return domain.contains(".") && !domain.hasPrefix(".") && !domain.hasSuffix(".") && !domain.contains("..")
    }
}

/// The e-mail code: exactly four ASCII digits.
///
/// Код — тек 4 ASCII цифр.
enum OTPCode {

    static let length = 4

    /// The first four ASCII digits of whatever was typed, pasted or autofilled:
    /// "Code: 4821" becomes "4821". Other scripts' digits ("٤٨٢١") are not
    /// accepted — the server would reject them, so the field does too.
    static func sanitize(_ raw: String) -> String {
        String(raw.filter { ("0"..."9").contains($0) }.prefix(length))
    }
}

/// The countdown next to "Resend code". Display only: the server enforces
/// the wait and says how long it is.
enum ResendCountdown {

    /// Whole seconds until a new code may be requested, rounded up; 0 once it may.
    static func secondsRemaining(until date: Date, now: Date = Date()) -> Int {
        max(0, Int(date.timeIntervalSince(now).rounded(.up)))
    }
}

// MARK: - Fields

/// The e-mail field used by sign-in and by "Add e-mail" in Settings.
struct EmailField: View {
    @Binding var text: String
    var isFocused: FocusState<Bool>.Binding
    let hasError: Bool
    let onSubmit: () -> Void

    var body: some View {
        TextField("account.email.placeholder", text: $text)
            .keyboardType(.emailAddress)
            .textContentType(.emailAddress)
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            .submitLabel(.continue)
            .onSubmit(onSubmit)
            .focused(isFocused)
            .padding(.horizontal, DS.Spacing.m)
            .frame(minHeight: 54)
            .background(
                RoundedRectangle(cornerRadius: DS.Radius.large, style: .continuous)
                    .fill(Color.dsSurface)
            )
            .overlay(
                RoundedRectangle(cornerRadius: DS.Radius.large, style: .continuous)
                    .stroke(borderColor, lineWidth: 2)
            )
            .accessibilityIdentifier("auth.email")
    }

    private var borderColor: Color {
        if hasError { return .red }
        return isFocused.wrappedValue ? .accentColor : .clear
    }
}

/// The 4-digit code field.
///
/// One real text field, so the system's one-time-code autofill (from Mail and
/// Messages) and paste both work; input is filtered to four ASCII digits and
/// `onComplete` fires when the fourth arrives.
struct OTPCodeField: View {
    @Binding var code: String
    var isFocused: FocusState<Bool>.Binding
    let hasError: Bool
    let onComplete: () -> Void

    var body: some View {
        TextField("", text: $code, prompt: Text(verbatim: "····"))
            .keyboardType(.numberPad)
            .textContentType(.oneTimeCode)
            .font(.system(size: 30, weight: .semibold, design: .rounded).monospacedDigit())
            .multilineTextAlignment(.center)
            .kerning(12)
            .focused(isFocused)
            .frame(maxWidth: .infinity)
            .frame(height: 64)
            .background(
                RoundedRectangle(cornerRadius: DS.Radius.large, style: .continuous)
                    .fill(Color.dsSurface)
            )
            .overlay(
                RoundedRectangle(cornerRadius: DS.Radius.large, style: .continuous)
                    .stroke(borderColor, lineWidth: 2)
            )
            .onChange(of: code) { _, newValue in
                let clean = OTPCode.sanitize(newValue)
                guard clean == newValue else {
                    code = clean
                    return
                }
                if clean.count == OTPCode.length { onComplete() }
            }
            .accessibilityLabel(Text("account.code.field"))
            .accessibilityHint(Text("account.code.hint"))
            .accessibilityIdentifier("auth.code")
    }

    private var borderColor: Color {
        if hasError { return .red }
        return isFocused.wrappedValue ? .accentColor : .clear
    }
}

/// "Resend code in 32s", then "Resend code".
struct ResendCodeButton: View {
    let availableAt: Date
    let isBusy: Bool
    let action: () -> Void

    var body: some View {
        TimelineView(.periodic(from: .now, by: 1)) { context in
            let remaining: Int = ResendCountdown.secondsRemaining(until: availableAt, now: context.date)
            if remaining > 0 {
                Text("account.code.resendIn \(remaining)")
                    .foregroundStyle(.secondary)
                    .monospacedDigit()
                    .accessibilityIdentifier("auth.resend.countdown")
            } else {
                Button("account.code.resend", action: action)
                    .disabled(isBusy)
                    .accessibilityIdentifier("auth.resend")
            }
        }
    }
}

/// An error under a field. Always a localization key, never a server sentence.
struct AuthErrorLabel: View {
    let key: String

    var body: some View {
        Label(LocalizedStringKey(key), systemImage: "exclamationmark.triangle")
            .font(.footnote)
            .foregroundStyle(.red)
            .fixedSize(horizontal: false, vertical: true)
            .accessibilityIdentifier("auth.error")
    }
}

// MARK: - Provider buttons

enum AuthButton {
    /// One height for the Apple, Google and e-mail buttons.
    static let height: CGFloat = 50
}

/// Label for the Google and e-mail buttons.
struct AuthButtonLabel: View {
    let title: LocalizedStringKey
    let systemImage: String

    var body: some View {
        HStack(spacing: DS.Spacing.xs) {
            Image(systemName: systemImage)
                .font(.title3)
                .accessibilityHidden(true)
            Text(title)
        }
    }
}

/// Neutral outlined button that sits under Apple's own black/white one.
struct AuthButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        StyleBody(configuration: configuration)
    }

    private struct StyleBody: View {
        let configuration: ButtonStyleConfiguration
        @Environment(\.isEnabled) private var isEnabled

        var body: some View {
            configuration.label
                .font(.body.weight(.semibold))
                .foregroundStyle(Color.primary)
                .frame(maxWidth: .infinity, minHeight: AuthButton.height)
                .background(
                    RoundedRectangle(cornerRadius: DS.Radius.medium, style: .continuous)
                        .fill(Color.dsSurface)
                )
                .overlay(
                    RoundedRectangle(cornerRadius: DS.Radius.medium, style: .continuous)
                        .strokeBorder(Color.dsSeparator, lineWidth: 1)
                )
                .contentShape(Rectangle())
                .opacity(isEnabled ? (configuration.isPressed ? 0.7 : 1) : 0.4)
        }
    }
}
