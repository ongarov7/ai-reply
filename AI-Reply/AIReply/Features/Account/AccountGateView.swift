import SwiftUI

/// Decides whether the app shows its normal content or the sign-in flow.
///
/// Тіркелгі қажет болса — кіру экраны, әйтпесе қосымша әдеттегідей ашылады.
///
struct AccountGateView<Content: View>: View {

    @Environment(AccountModel.self) private var account

    @ViewBuilder var content: Content

    @State private var isCompletingRegistration = false

    var body: some View {
        Group {
            if !account.isBootstrapComplete {
                ProgressView()
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .background(Color.dsBackground)
            } else if !account.hasAcceptedLegal {
                LegalConsentView()
            } else if isCompletingRegistration {
                RegistrationStepView { isCompletingRegistration = false }
            } else {
                switch account.phase {
                case .signedOut:
                    SignInView { isNewUser in isCompletingRegistration = isNewUser }
                case .enteringEmail:
                    EmailSignInView()
                case let .awaitingCode(challenge):
                    VerifyCodeView(challenge: challenge) { isNewUser in
                        isCompletingRegistration = isNewUser
                    }
                case .signedIn:
                    content
                }
            }
        }
        .animation(.default, value: account.phase)
        .task {
            await account.bootstrap()
        }
    }
}

private struct LegalConsentView: View {
    @Environment(AppSettings.self) private var settings
    @Environment(AccountModel.self) private var account
    @Environment(\.openURL) private var openURL

    @State private var isAccepted = false
    @State private var isSubmitting = false

    var body: some View {
        AuthScreen {
            VStack(alignment: .leading, spacing: DS.Spacing.l) {
                VStack(alignment: .leading, spacing: DS.Spacing.s) {
                    AppMarkView(size: 56)
                    Text("legal.consent.title").font(.title.weight(.semibold))
                    Text("legal.consent.body")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }

                VStack(spacing: 0) {
                    legalLink("legal.terms", urlString: account.legalConfig.termsURL)
                    Divider().padding(.leading, 44)
                    legalLink("legal.privacy", urlString: account.legalConfig.privacyURL)
                }
                .padding(.vertical, DS.Spacing.xxs)
                .background(
                    RoundedRectangle(cornerRadius: DS.Radius.large, style: .continuous)
                        .fill(Color.dsSurface)
                )

                Button { isAccepted.toggle() } label: {
                    HStack(alignment: .top, spacing: DS.Spacing.s) {
                        Image(systemName: isAccepted ? "checkmark.square.fill" : "square")
                            .font(.title3)
                            .foregroundStyle(isAccepted ? Color.accentColor : Color.secondary)
                        Text("legal.consent.checkbox")
                            .font(.subheadline)
                            .foregroundStyle(.primary)
                            .multilineTextAlignment(.leading)
                    }
                }
                .buttonStyle(.plain)

                Button {
                    isSubmitting = true
                    Task {
                        await account.acceptLegal(locale: settings.effectiveLanguage.rawValue)
                        isSubmitting = false
                    }
                } label: {
                    if isSubmitting { ProgressView().tint(.white) }
                    else { Text("legal.consent.continue") }
                }
                .buttonStyle(.dsPrimary)
                .disabled(!isAccepted || isSubmitting)

                Text("legal.consent.footer")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
    }

    private func legalLink(_ title: LocalizedStringKey, urlString: String) -> some View {
        Button {
            guard var components = URLComponents(string: urlString) else { return }
            components.queryItems = [URLQueryItem(name: "lang", value: settings.effectiveLanguage.rawValue)]
            if let url = components.url { openURL(url) }
        } label: {
            HStack(spacing: DS.Spacing.s) {
                Image(systemName: "doc.text")
                    .frame(width: 24)
                    .foregroundStyle(Color.accentColor)
                Text(title).foregroundStyle(.primary)
                Spacer()
                Image(systemName: "arrow.up.right")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            .padding(.horizontal, DS.Spacing.m)
            .frame(minHeight: DS.Layout.minimumTouchTarget)
        }
        .buttonStyle(.plain)
    }
}
