import SwiftUI

@main
struct AIReplyApp: App {

    @State private var settings = AppSettings()
    @State private var configuration = ReplyConfigurationModel()
    @State private var account = AccountModel()

    init() {
        #if DEBUG
        AIConfiguration.applyDebugLaunchArguments()
        #endif
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
                            NavigationStack { HomeView() }
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
                        NavigationStack { HomeView() }
                    } else {
                        OnboardingView()
                    }
                }
                #endif
            }
            .environment(settings)
            .environment(configuration)
            .environment(account)
            // Drives both the interface language and every localized string in
            // the subtree, so switching language takes effect without a restart.
            .environment(\.locale, settings.locale)
            .preferredColorScheme(settings.colorScheme)
            .tint(.accentColor)
        }
    }
}

#if DEBUG
/// Which screen `-AIReplyDebugScreen <name>` should open.
enum DebugScreen: String {
    case keyboard, setup, home, settings, profile, templates

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
        case .home:      NavigationStack { HomeView() }
        case .settings:  NavigationStack { SettingsView() }
        case .profile:   NavigationStack { ProfileEditorView() }
        case .templates: NavigationStack { TemplateEditorView(templateID: "client") }
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
