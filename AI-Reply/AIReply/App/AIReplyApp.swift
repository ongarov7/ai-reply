import SwiftUI

@main
struct AIReplyApp: App {

    /// UIKit's push callbacks (device token, launch) have no SwiftUI
    /// equivalent; the delegate hands them to `AppServices`.
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate
    @Environment(\.scenePhase) private var scenePhase

    @State private var settings: AppSettings
    @State private var configuration = ReplyConfigurationModel()
    @State private var account: AccountModel

    private let services = AppServices.shared

    init() {
        #if DEBUG
        AIConfiguration.applyDebugLaunchArguments()
        #endif
        let settings = AppSettings()
        #if DEBUG
        if let language = DebugLaunchOptions.language {
            settings.applyLanguageForThisLaunch(language)
        }
        #endif
        let account = AccountModel()
        // Sign-out and failures inside Apple's or Google's sheet become app
        // events; the account model itself knows nothing about telemetry.
        account.onActivity = { activity in AppServices.shared.accountActivity(activity) }
        AppServices.shared.appLanguage = { settings.effectiveLanguage.rawValue }
        _settings = State(initialValue: settings)
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
                    // The account gate is transparent unless this build is
                    // pointed at our service; see AccountGateView.
                    AccountGateView {
                        if configuration.hasCompletedOnboarding {
                            RootNavigationView()
                        } else {
                            OnboardingView()
                        }
                    }
                }
                #else
                // Onboarding runs once. `hasCompletedOnboarding` is stored with
                // the profile, so it survives relaunches, and Settings can put
                // the user back through it without losing their answers.
                AccountGateView {
                    if configuration.hasCompletedOnboarding {
                        RootNavigationView()
                    } else {
                        OnboardingView()
                    }
                }
                #endif
            }
            // Google Sign-In's redirect back into the app.
            .onOpenURL { url in GoogleSignInProvider.handle(url) }
            .environment(settings)
            .environment(configuration)
            .environment(account)
            .environment(services.router)
            .environment(services.notifications)
            // Drives both the interface language and every localized string in
            // the subtree, so switching language takes effect without a restart.
            .environment(\.locale, settings.locale)
            .preferredColorScheme(settings.colorScheme)
            .tint(.accentColor)
            // Sign-in, sign-out and the server's features decide whether and
            // how this install is registered for notifications.
            .onChange(of: accountState, initial: true) { _, state in
                services.accountDidChange(state)
            }
            .onChange(of: settings.effectiveLanguage) { _, _ in
                services.requestInstallationSync()
            }
            .onChange(of: scenePhase, initial: true) { _, phase in
                services.scenePhaseDidChange(phase)
            }
        }
    }

    private var accountState: AppServices.AccountState {
        AppServices.AccountState(
            isBootstrapComplete: account.isBootstrapComplete,
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
    /// Settings, scrolled to its last sections (Diagnostics, Privacy).
    case settingsEnd
    /// The sign-in flow's three screens, for review in each language.
    case signIn, email, code

    static var requested: DebugScreen? {
        let arguments = CommandLine.arguments
        guard let index = arguments.firstIndex(of: "-AIReplyDebugScreen") else { return nil }
        let value = index + 1 < arguments.count ? arguments[index + 1] : "keyboard"
        return DebugScreen(rawValue: value) ?? .keyboard
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
        case .settingsEnd:   NavigationStack { SettingsView(focus: .end) }
        case .profile:   NavigationStack { ProfileEditorView() }
        case .templates: NavigationStack { TemplateEditorView(templateID: "client") }
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
