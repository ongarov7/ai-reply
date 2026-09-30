import AuthenticationServices
import SwiftUI

/// The first screen of the sign-in flow: Apple, Google, e-mail.
///
/// Кіру: Apple, Google немесе пошта. Құпиясөз жоқ.
struct SignInView: View {

    @Environment(AccountModel.self) private var account
    @Environment(\.colorScheme) private var colorScheme

    /// Called with `true` when the account was created just now.
    let onSignedIn: (Bool) -> Void

    /// The raw nonce of the Apple request in flight; Apple gets its hash.
    @State private var appleNonce = SignInNonce.make()

    var body: some View {
        AuthScreen {
            VStack(alignment: .leading, spacing: DS.Spacing.l) {
                header

                VStack(spacing: DS.Spacing.s) {
                    if account.offersApple {
                        appleButton
                    }
                    if account.offersGoogle {
                        Button {
                            Task { finish(await account.signInWithGoogle()) }
                        } label: {
                            AuthButtonLabel(title: "account.continueWithGoogle", systemImage: "g.circle.fill")
                        }
                        .buttonStyle(AuthButtonStyle())
                        .accessibilityIdentifier("signIn.google")
                    }
                    Button {
                        account.startEmailSignIn()
                    } label: {
                        AuthButtonLabel(title: "account.continueWithEmail", systemImage: "envelope.fill")
                    }
                    .buttonStyle(AuthButtonStyle())
                    .accessibilityIdentifier("signIn.email")
                }
                .disabled(account.isBusy)

                if account.isBusy {
                    ProgressView()
                        .frame(maxWidth: .infinity)
                }

                if let errorKey = account.errorKey {
                    AuthErrorLabel(key: errorKey)
                }

                Text("account.legal.footer")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .task { await account.loadServerConfig() }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.s) {
            AppMarkView(size: 56)
            Text("account.signIn.title").font(.title.weight(.semibold))
            Text("account.signIn.subtitle")
                .font(.subheadline)
                .foregroundStyle(.secondary)
        }
    }

    /// Apple's own button: its look and wording are Apple's to decide, and
    /// App Review checks that they were not reinvented.
    private var appleButton: some View {
        SignInWithAppleButton(.continue) { request in
            appleNonce = SignInNonce.make()
            AppleSignIn.configure(request, rawNonce: appleNonce)
        } onCompletion: { result in
            switch result {
            case .success(let authorization):
                guard let credential = AppleSignIn.credential(from: authorization) else {
                    account.reportProviderFailure(errorCode: "missing_identity_token")
                    return
                }
                let nonce = appleNonce
                Task { finish(await account.signInWithApple(credential, rawNonce: nonce)) }
            case .failure(let error):
                if !AppleSignIn.isCancellation(error) {
                    account.reportProviderFailure(errorCode: AppleSignIn.errorCode(for: error))
                }
            }
        }
        .signInWithAppleButtonStyle(colorScheme == .dark ? .white : .black)
        .frame(height: AuthButton.height)
        .clipShape(RoundedRectangle(cornerRadius: DS.Radius.medium, style: .continuous))
        .accessibilityIdentifier("signIn.apple")
    }

    private func finish(_ outcome: AccountModel.SignInOutcome) {
        if case let .signedIn(isNewUser) = outcome {
            onSignedIn(isNewUser)
        }
    }
}
