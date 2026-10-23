import SwiftUI

/// «Подключи AI Reply»: adding the keyboard in iOS Settings, why Full Access
/// is needed, and a field that shows whether the keyboard really appears.
///
/// Пернетақтаны қосу: iOS реттеулеріндегі қадамдар, толық рұқсат не үшін
/// керек және пернетақта шынымен ашылатынын көрсететін өріс.
///
/// Only the user can add a keyboard; the app opens its own page in Settings
/// and says the remaining taps. The steps tick themselves off as the keyboard
/// reports in (`KeyboardStatusMonitor`, re-read every second while this page
/// is up and on every return from Settings); the main button turns into
/// «Далее» once the keyboard was seen with Full Access.
struct OnboardingConnectStep: View {

    /// Nothing comes after this step: its main button finishes.
    let isLast: Bool
    let onDone: () -> Void
    let onLater: () -> Void

    @Environment(KeyboardStatusMonitor.self) private var keyboard
    @Environment(AccountModel.self) private var account
    @Environment(AppSettings.self) private var settings
    @Environment(\.scenePhase) private var scenePhase

    @FocusState private var isChecking: Bool
    @State private var isShowingHelp = false
    @State private var isShowingVideo = false
    @State private var privacyPage: AppRouter.WebPage?

    var body: some View {
        OnboardingPage {
            VStack(alignment: .leading, spacing: DS.Spacing.l) {
                VStack(spacing: DS.Spacing.s) {
                    AppMarkView(size: 72)
                    Text("onboarding.connect.title")
                        .font(.title.weight(.bold))
                        .multilineTextAlignment(.center)
                        .accessibilityAddTraits(.isHeader)
                    Text("onboarding.connect.subtitle")
                        .font(.body)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                }
                .frame(maxWidth: .infinity)

                Button { isShowingVideo = true } label: {
                    Label("tutorial.paste.title", systemImage: "play.rectangle")
                        .frame(maxWidth: .infinity, minHeight: DS.Layout.minimumTouchTarget)
                }

                KeyboardConnectSteps(showsProgress: true)
                    .dsCard()

                VStack(alignment: .leading, spacing: DS.Spacing.xs) {
                    Text("onboarding.connect.why.title")
                        .font(.headline)
                    Text("onboarding.connect.why.body")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                    Button("onboarding.connect.data", action: openPrivacy)
                        .font(.subheadline)
                        .frame(minHeight: DS.Layout.minimumTouchTarget)
                }
                .dsCard()

                VStack(alignment: .leading, spacing: DS.Spacing.s) {
                    Text("keyboardCheck.title")
                        .font(.headline)
                    KeyboardCheckField(focus: $isChecking)
                }
                .dsCard()

                Button("onboarding.connect.trouble") { isShowingHelp = true }
                    .font(.subheadline)
                    .frame(maxWidth: .infinity, minHeight: DS.Layout.minimumTouchTarget)
            }
        } buttons: {
            if keyboard.fullAccessSighting.isConfirmed {
                Button(isLast ? "common.done" : "onboarding.next", action: onDone)
                    .buttonStyle(.dsPrimary)
            } else {
                Button("permissions.openSettings", action: OpenKeyboardSettingsButton.openSystemSettings)
                    .buttonStyle(.dsPrimary)
                Button("onboarding.connect.later", action: onLater)
                    .buttonStyle(.dsText)
            }
        }
        .navigationTitle("onboarding.connect.nav")
        .navigationBarTitleDisplayMode(.inline)
        .onAppear { ProductEvents.track(.onboardingKeyboardStepViewed) }
        // The trip to iOS Settings and back, without a tap.
        .task { await keyboard.followLiveUpdates() }
        .onChange(of: scenePhase) { _, phase in
            if phase == .active { keyboard.refresh() }
        }
        .sheet(isPresented: $isShowingHelp) {
            NavigationStack {
                PermissionsView()
                    .toolbar {
                        ToolbarItem(placement: .confirmationAction) {
                            Button("common.done") { isShowingHelp = false }
                        }
                    }
            }
        }
        .sheet(isPresented: $isShowingVideo) {
            NavigationStack {
                PasteTutorialView()
                    .toolbar {
                        ToolbarItem(placement: .confirmationAction) {
                            Button("common.done") { isShowingVideo = false }
                        }
                    }
            }
        }
        .sheet(item: $privacyPage) { page in
            SafariView(url: page.url).ignoresSafeArea()
        }
    }

    /// The privacy policy, in the app's language, in the in-app browser.
    private func openPrivacy() {
        guard var components = URLComponents(string: account.legalConfig.privacyURL) else { return }
        components.queryItems = [URLQueryItem(name: "lang", value: settings.effectiveLanguage.rawValue)]
        guard let url = components.url, url.scheme == "https" else { return }
        privacyPage = AppRouter.WebPage(url: url)
    }
}
