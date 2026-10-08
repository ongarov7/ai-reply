import SwiftUI

@main
struct AIReplyApp: App {

    /// UIKit's push callbacks (launch, APNs token) have no SwiftUI
    /// equivalent; the delegate hands them to `AppServices`.
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate

    @State private var settings: AppSettings
    @State private var configuration: ReplyConfigurationModel
    @State private var account: AccountModel
    @State private var keyboardStatus = KeyboardStatusMonitor()
    @Environment(\.scenePhase) private var scenePhase

    private let services = AppServices.shared

    init() {
        #if DEBUG
        AIConfiguration.applyDebugLaunchArguments()
        #endif
        // Sends only for a signed-in user of a server that asks for events.
        ProductEvents.sink = ProductEventReporter.shared

        let settings = AppSettings()
        let configuration = ReplyConfigurationModel()
        let account = AccountModel()
        // A gender chosen on another device is taken the moment the profile
        // arrives, before the first-run onboarding decides its steps: a
        // returning user is not asked again.
        account.didReceiveProfile = { [weak configuration, weak account] profile in
            guard let configuration, let account else { return }
            ProfileSync(configuration: configuration, account: account).adoptServerChoice(profile?.gender)
        }
        account.installationIDForSignOut = { AppServices.shared.installationIDForSignOut }
        account.didDeleteAccount = { [weak configuration] in
            configuration?.forgetAccountProfile()
            ProductEventReporter.shared.discardWaiting()
        }
        AppServices.shared.appLanguage = { settings.effectiveLanguage.rawValue }
        // A quota or plan notification opens on current numbers.
        AppServices.shared.onNotificationOpened = { [weak account] in
            Task { await account?.refresh() }
        }
        _settings = State(initialValue: settings)
        _configuration = State(initialValue: configuration)
        _account = State(initialValue: account)
    }

    var body: some Scene {
        WindowGroup {
            Group {
                #if DEBUG
                // Screenshot / inspection hook. `-AIReplyDebugScreen keyboard`
                // opens straight onto a focused field, which is the only way to
                // get the keyboard extension on screen without a human tapping;
                // the other values open one screen directly so each can be
                // reviewed in each language without stepping through the flow.
                // DEBUG only, so none of it can exist in a shipping build.
                if let screen = DebugScreen.requested {
                    DebugScreenHost(screen: screen)
                } else {
                    AccountGateView { root }
                }
                #else
                AccountGateView { root }
                #endif
            }
            // Google Sign-In's redirect back into the app.
            .onOpenURL { url in GoogleSignInProvider.handle(url) }
            .environment(settings)
            .environment(configuration)
            .environment(account)
            .environment(keyboardStatus)
            .environment(services.router)
            .environment(services.notifications)
            // A gender picked on another device arrives with /me; one picked
            // here and not yet confirmed is retried on every return.
            .onChange(of: account.profile) {
                Task { await profileSync.reconcile() }
            }
            // An account without a notification language gets this app's.
            .onChange(of: account.user) {
                Task { await languageSync.reconcile() }
            }
            // Sign-in, sign-out and the server's features decide whether and
            // how this install is registered for notifications.
            .onChange(of: accountState, initial: true) { _, state in
                services.accountDidChange(state)
            }
            // The installation carries the app's language.
            .onChange(of: settings.effectiveLanguage) {
                services.requestInstallationSync()
            }
            .onChange(of: scenePhase) { _, phase in
                services.scenePhaseDidChange(phase)
                switch phase {
                case .active:
                    keyboardStatus.refresh()
                    // The keyboard may have dropped a consent the server no longer has.
                    account.revalidateLegalConsent()
                    Task { await profileSync.reconcile() }
                    Task { await languageSync.reconcile() }
                case .background:
                    ProductEventReporter.shared.flushBeforeSuspension()
                default:
                    break
                }
            }
            // Drives both the interface language and every localized string in
            // the subtree, so switching language takes effect without a restart.
            .environment(\.locale, settings.locale)
            .preferredColorScheme(settings.colorScheme)
            .tint(.accentColor)
        }
    }

    /// Signed in: the current onboarding once per device, then Home. The
    /// completed version is stored with the profile, so it survives
    /// relaunches, and an unfinished first run resumes where it stopped.
    @ViewBuilder
    private var root: some View {
        if configuration.needsOnboarding {
            OnboardingView(flow: .firstRun(profileHasGender: ProfileSync.knowsGender(
                local: configuration.profile.grammaticalGender,
                server: account.profile?.gender
            )))
        } else {
            RootNavigationView()
        }
    }

    private var profileSync: ProfileSync {
        ProfileSync(configuration: configuration, account: account)
    }

    private var languageSync: PreferredLanguageSync {
        PreferredLanguageSync(account: account, settings: settings)
    }

    private var accountState: AppServices.AccountState {
        AppServices.AccountState(
            isBootstrapComplete: account.isBootstrapComplete,
            hasAcceptedLegal: account.hasAcceptedLegal,
            isSignedIn: account.isSignedIn,
            userID: account.user?.id,
            features: account.features
        )
    }
}

#if DEBUG
/// Which screen `-AIReplyDebugScreen <name>` should open.
enum DebugScreen: String {
    case keyboard, setup, home, settings, profile, templates
    /// Settings, scrolled to its notifications section.
    case notifications
    /// The first-run onboarding, whatever was completed: from its first step,
    /// or from the one `-AIReplyDebugStep` names.
    case onboarding
    /// The sign-in flow's three screens, for review in each language.
    case signIn, email, code

    static var requested: DebugScreen? {
        let arguments = CommandLine.arguments
        guard let index = arguments.firstIndex(of: "-AIReplyDebugScreen") else { return nil }
        let value = index + 1 < arguments.count ? arguments[index + 1] : "keyboard"
        return DebugScreen(rawValue: value) ?? .keyboard
    }

    /// `-AIReplyDebugStep <step id>` next to `onboarding`: the step it opens
    /// on, so each one can be reviewed in each language without tapping there.
    static var onboardingStep: OnboardingFlow.Step? {
        let arguments = CommandLine.arguments
        guard let index = arguments.firstIndex(of: "-AIReplyDebugStep"), index + 1 < arguments.count else { return nil }
        return OnboardingFlow.Step(rawValue: arguments[index + 1])
    }
}

private struct DebugScreenHost: View {
    let screen: DebugScreen

    var body: some View {
        switch screen {
        case .keyboard:  DebugKeyboardHost()
        case .setup:     NavigationStack { KeyboardSetupView() }
        // The router's own stack, so `-AIReplyOpenLink` can be tried here.
        case .home:      RootNavigationView()
        case .settings:  NavigationStack { SettingsView() }
        case .notifications: NavigationStack { SettingsView(focus: .notifications) }
        case .profile:   NavigationStack { ProfileEditorView() }
        case .templates: NavigationStack { TemplateEditorView(templateID: "client") }
        case .onboarding:
            OnboardingView(flow: OnboardingFlow(mode: .firstRun, asksGender: true,
                                                resumingAt: DebugScreen.onboardingStep))
        case .signIn:    SignInView { _ in }
        case .email:     EmailSignInView()
        case .code:
            VerifyCodeView(challenge: .init(email: "aigerim@example.kz",
                                            resendAvailableAt: Date().addingTimeInterval(32))) { _ in }
        }
    }
}

/// A bare focused text field, so the keyboard extension can be seen and
/// photographed during development.
private struct DebugKeyboardHost: View {
    @State private var text = ""
    @FocusState private var isFocused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.m) {
            Text(verbatim: "Host field").font(.headline)
            TextField("", text: $text, axis: .vertical)
                .textFieldStyle(.roundedBorder)
                .focused($isFocused)
            Spacer()
        }
        .padding(DS.Spacing.l)
        .task {
            // A beat, so the field exists before focus is requested.
            try? await Task.sleep(for: .milliseconds(400))
            isFocused = true
        }
    }
}
#endif
