import SwiftUI

/// "Add e-mail for sign-in", for accounts opened with a phone number.
///
/// Телефонмен ашылған тіркелгіге пошта қосу: кейін сол поштамен кіруге болады.
///
/// Phone sign-in is no longer offered, so an account that only has a number
/// would be unreachable after signing out. Proving an address once, while
/// still signed in, keeps the account — its plan, quota and history — usable.
struct LinkEmailView: View {

    @Environment(\.dismiss) private var dismiss
    @Environment(AppSettings.self) private var settings
    @Environment(AccountModel.self) private var account

    @State private var email = ""
    @State private var code = ""
    @State private var challenge: AccountModel.EmailCodeChallenge?
    @State private var errorKey: String?
    @State private var isBusy = false
    @FocusState private var emailFocused: Bool
    @FocusState private var codeFocused: Bool

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: DS.Spacing.l) {
                    if let challenge {
                        codeStep(challenge)
                    } else {
                        emailStep
                    }
                    if let errorKey {
                        AuthErrorLabel(key: errorKey)
                    }
                }
                .padding(DS.Spacing.l)
                .frame(maxWidth: DS.Layout.readableWidth, alignment: .leading)
                .frame(maxWidth: .infinity)
            }
            .background(Color.dsBackground)
            .navigationTitle("settings.account.addEmail.title")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("common.cancel") { dismiss() }
                }
            }
        }
    }

    private var emailStep: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.l) {
            Text("settings.account.addEmail.body")
                .font(.subheadline)
                .foregroundStyle(.secondary)
            EmailField(text: $email, isFocused: $emailFocused, hasError: errorKey != nil, onSubmit: requestCode)
            Button(action: requestCode) {
                if isBusy { ProgressView().tint(.white) } else { Text("account.continue") }
            }
            .buttonStyle(.dsPrimary)
            .disabled(isBusy || !EmailAddress.isPlausible(email))
        }
        .onAppear { emailFocused = true }
    }

    private func codeStep(_ challenge: AccountModel.EmailCodeChallenge) -> some View {
        VStack(alignment: .leading, spacing: DS.Spacing.l) {
            Text("account.code.sentTo \(challenge.email)")
                .font(.subheadline)
                .foregroundStyle(.secondary)
            OTPCodeField(code: $code, isFocused: $codeFocused, hasError: errorKey != nil, onComplete: verify)
            Button(action: verify) {
                if isBusy { ProgressView().tint(.white) } else { Text("account.code.confirm") }
            }
            .buttonStyle(.dsPrimary)
            .disabled(isBusy || code.count < OTPCode.length)
            HStack(alignment: .firstTextBaseline) {
                ResendCodeButton(availableAt: challenge.resendAvailableAt, isBusy: isBusy) {
                    code = ""
                    requestCode()
                }
                Spacer()
                Button("account.code.changeEmail") {
                    self.challenge = nil
                    code = ""
                    errorKey = nil
                }
            }
            .font(.subheadline)
        }
        .onAppear { codeFocused = true }
    }

    private func requestCode() {
        guard EmailAddress.isPlausible(email), !isBusy else { return }
        isBusy = true
        errorKey = nil
        Task {
            defer { isBusy = false }
            do {
                challenge = try await account.requestLinkEmailCode(
                    email: email, locale: settings.effectiveLanguage.rawValue)
            } catch {
                errorKey = AccountModel.message(for: error)
            }
        }
    }

    private func verify() {
        guard let challenge, code.count == OTPCode.length, !isBusy else { return }
        isBusy = true
        errorKey = nil
        let entered = code
        Task {
            defer { isBusy = false }
            do {
                try await account.verifyLinkEmailCode(email: challenge.email, code: entered)
                dismiss()
            } catch {
                errorKey = AccountModel.message(for: error)
                if AccountModel.codeIsSpent(error) { code = "" }
            }
        }
    }
}
