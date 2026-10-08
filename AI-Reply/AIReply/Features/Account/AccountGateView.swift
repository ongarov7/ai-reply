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
        // Said once, over whichever screen comes next: "your account has
        // been deleted" lands on the consent or sign-in screen.
        .alert(Text(LocalizedStringKey(account.noticeKey ?? "")), isPresented: noticeBinding) {
            Button("common.done", role: .cancel) { account.dismissNotice() }
        }
        .task {
            await account.bootstrap()
        }
    }

    private var noticeBinding: Binding<Bool> {
        Binding(get: { account.noticeKey != nil }, set: { if !$0 { account.dismissNotice() } })
    }
}

private struct LegalConsentView: View {
    @Environment(AppSettings.self) private var settings
    @Environment(AccountModel.self) private var account
    @Environment(\.openURL) private var openURL

    @State private var isAccepted = false
    /// The second, separate consent: the texts go to the AI provider.
    @State private var isAIAccepted = false
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

                // What happens to the texts, said before anyone agrees to it.
                Text("legal.consent.ai")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)

                checkbox("legal.consent.checkbox", isOn: $isAccepted)
                checkbox("legal.consent.aiCheckbox", isOn: $isAIAccepted)

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
                .disabled(!isAccepted || !isAIAccepted || isSubmitting)

                Text("legal.consent.footer")
                    .font(.caption)
                    .foregroundStyle(.secondary)

                // Signed in, nobody is held here: signing out and deleting
                // the account need no consent first (withdrawn, a new version
                // of the documents, or the server asking again).
                if account.isSignedIn {
                    VStack(alignment: .leading, spacing: 0) {
                        Divider()
                        AccountExitActions()
                    }
                    .buttonStyle(ExitActionButtonStyle())
                }
            }
        }
    }

    private func checkbox(_ title: LocalizedStringKey, isOn: Binding<Bool>) -> some View {
        Button { isOn.wrappedValue.toggle() } label: {
            HStack(alignment: .top, spacing: DS.Spacing.s) {
                Image(systemName: isOn.wrappedValue ? "checkmark.square.fill" : "square")
                    .font(.title3)
                    .foregroundStyle(isOn.wrappedValue ? Color.accentColor : Color.secondary)
                Text(title)
                    .font(.subheadline)
                    .foregroundStyle(.primary)
                    .multilineTextAlignment(.leading)
            }
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(isOn.wrappedValue ? .isSelected : [])
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

/// The quiet ways out under the consent: text only, a full touch target.
private struct ExitActionButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        StyleBody(configuration: configuration)
    }

    private struct StyleBody: View {
        let configuration: ButtonStyleConfiguration
        @Environment(\.isEnabled) private var isEnabled

        var body: some View {
            configuration.label
                .font(.subheadline.weight(.medium))
                .foregroundStyle(configuration.role == .destructive ? Color.red : Color.accentColor)
                .frame(minHeight: DS.Layout.minimumTouchTarget)
                .contentShape(Rectangle())
                .opacity(isEnabled ? (configuration.isPressed ? 0.6 : 1) : 0.4)
        }
    }
}
