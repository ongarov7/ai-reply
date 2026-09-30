import SwiftUI

/// Step one of e-mail sign-in: the address.
///
/// Пошта мекенжайы: оған 4 таңбалы код жіберіледі.
struct EmailSignInView: View {

    @Environment(AppSettings.self) private var settings
    @Environment(AccountModel.self) private var account

    @State private var email = ""
    @FocusState private var isFocused: Bool

    var body: some View {
        AuthScreen {
            VStack(alignment: .leading, spacing: DS.Spacing.l) {
                VStack(alignment: .leading, spacing: DS.Spacing.s) {
                    Text("account.email.title").font(.title.weight(.semibold))
                    Text("account.email.subtitle")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }

                EmailField(text: $email, isFocused: $isFocused, hasError: account.errorKey != nil,
                           onSubmit: submit)

                if let errorKey = account.errorKey {
                    AuthErrorLabel(key: errorKey)
                }

                Button(action: submit) {
                    if account.isBusy {
                        ProgressView().tint(.white)
                    } else {
                        Text("account.continue")
                    }
                }
                .buttonStyle(.dsPrimary)
                .disabled(account.isBusy || !EmailAddress.isPlausible(email))
                .accessibilityIdentifier("auth.email.continue")

                Button("common.back") { account.cancelEmailSignIn() }
                    .font(.subheadline)
                    .disabled(account.isBusy)
            }
        }
        .onAppear {
            if email.isEmpty { email = account.pendingEmail }
            isFocused = true
        }
    }

    private func submit() {
        guard EmailAddress.isPlausible(email), !account.isBusy else { return }
        isFocused = false
        Task { await account.requestEmailCode(email: email, locale: settings.effectiveLanguage.rawValue) }
    }
}
