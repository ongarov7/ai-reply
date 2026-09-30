import SwiftUI

/// Step two of e-mail sign-in: the 4-digit code.
///
/// Растау коды: 4 цифр, 5 минут жарамды.
struct VerifyCodeView: View {

    @Environment(AppSettings.self) private var settings
    @Environment(AccountModel.self) private var account

    let challenge: AccountModel.EmailCodeChallenge
    /// Called with `true` when the account was created just now.
    let onVerified: (Bool) -> Void

    @State private var code = ""
    @FocusState private var isFocused: Bool

    var body: some View {
        AuthScreen {
            VStack(alignment: .leading, spacing: DS.Spacing.l) {
                VStack(alignment: .leading, spacing: DS.Spacing.s) {
                    Text("account.code.title").font(.title.weight(.semibold))
                    Text("account.code.sentTo \(challenge.email)")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }

                OTPCodeField(code: $code, isFocused: $isFocused, hasError: account.errorKey != nil,
                             onComplete: verify)

                if let errorKey = account.errorKey {
                    AuthErrorLabel(key: errorKey)
                }

                Button(action: verify) {
                    if account.isBusy {
                        ProgressView().tint(.white)
                    } else {
                        Text("account.code.confirm")
                    }
                }
                .buttonStyle(.dsPrimary)
                .disabled(account.isBusy || code.count < OTPCode.length)
                .accessibilityIdentifier("auth.code.confirm")

                HStack(alignment: .firstTextBaseline) {
                    ResendCodeButton(availableAt: challenge.resendAvailableAt, isBusy: account.isBusy) {
                        code = ""
                        isFocused = true
                        Task { await account.resendEmailCode(locale: settings.effectiveLanguage.rawValue) }
                    }
                    Spacer()
                    Button("account.code.changeEmail") { account.editEmail() }
                        .disabled(account.isBusy)
                }
                .font(.subheadline)
            }
        }
        .onAppear { isFocused = true }
    }

    private func verify() {
        guard code.count == OTPCode.length, !account.isBusy else { return }
        let entered = code
        Task {
            switch await account.verifyEmailCode(entered) {
            case .signedIn(let isNewUser):
                onVerified(isNewUser)
            case .failed(let clearCode):
                if clearCode { code = "" }
                isFocused = true
            case .cancelled:
                break
            }
        }
    }
}
