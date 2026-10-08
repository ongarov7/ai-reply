import SwiftUI

struct SettingsView: View {

    /// A part of Settings a notification link can open directly.
    enum Focus: Hashable {
        case notifications
    }

    /// Scrolled into view when the screen appears.
    var focus: Focus?

    @Environment(AppSettings.self) private var settings
    @Environment(ReplyConfigurationModel.self) private var model
    @Environment(AccountModel.self) private var account
    @Environment(KeyboardStatusMonitor.self) private var keyboard
    @Environment(PushNotificationsModel.self) private var notifications
    @Environment(\.openURL) private var openURL

    @State private var isShowingTutorial = false
    @State private var isConfirmingWithdrawal = false
    /// Why the withdrawal did not reach the server, shown in an alert.
    @State private var withdrawalFailureKey: String?

    var body: some View {
        ScrollViewReader { proxy in
            form
                .task {
                    guard let focus else { return }
                    // After the push animation, so the row exists to scroll to.
                    // Centred rather than at the top, where the section's header
                    // would sit under the navigation bar.
                    try? await Task.sleep(for: .milliseconds(350))
                    withAnimation { proxy.scrollTo(focus, anchor: .center) }
                }
        }
        .navigationTitle("settings.title")
        .navigationBarTitleDisplayMode(.inline)
        // The app re-reads it on every return to the foreground as well.
        .onAppear { keyboard.refresh() }
        .fullScreenCover(isPresented: $isShowingTutorial) {
            OnboardingView(flow: OnboardingFlow(mode: .tutorial, asksGender: false))
        }
    }

    private var form: some View {
        Form {
            AccountSettingsSection()

            if notifications.showsNotificationSettings {
                NotificationSettingsSection()
            }

            Section {
                NavigationLink { ProfileEditorView() } label: {
                    Label("home.profile.edit", systemImage: "person.text.rectangle")
                }
                NavigationLink { TemplateListView() } label: {
                    Label("home.profile.templates", systemImage: "text.bubble")
                }
                NavigationLink { WorkingHoursView() } label: {
                    Label("home.profile.hours", systemImage: "clock")
                }
            } header: {
                Text("home.profile.title")
            }

            Section {
                KeyboardStatusRow(kind: .keyboard, status: keyboard.status)
                KeyboardStatusRow(kind: .fullAccess, status: keyboard.status)
                NavigationLink { KeyboardSetupView() } label: {
                    Label("settings.setup.guide", systemImage: "keyboard")
                }
            } header: {
                Text("settings.setup")
            } footer: {
                Text("settings.setup.footer")
            }

            Section {
                ForEach(KeyboardLanguage.cycleOrder, id: \.self) { language in
                    Toggle(language.nativeName, isOn: layoutBinding(language))
                        .disabled(settings.keyboardLanguages == [language])
                }
                Toggle("settings.keyboard.haptics", isOn: hapticsBinding)
            } header: {
                Text("settings.keyboard")
            } footer: {
                Text("settings.keyboard.footer")
            }

            Section {
                Toggle("settings.smartCorrection", isOn: smartCorrectionBinding)
            } footer: {
                Text("settings.smartCorrection.footer")
            }

            Section("settings.appearance") {
                Picker("settings.appearance", selection: appearanceBinding) {
                    ForEach(AppearancePreference.allCases, id: \.rawValue) { option in
                        Text(option.titleKey).tag(option)
                    }
                }
                .pickerStyle(.segmented)
                .labelsHidden()
            }

            Section {
                Picker("settings.language", selection: languageBinding) {
                    Text("common.system").tag(AppLanguage?.none)
                    ForEach(AppLanguage.allCases) { language in
                        // Always shown in its own language: a Kazakh speaker
                        // looking for Kazakh should see "Қазақша".
                        Text(language.nativeName).tag(AppLanguage?.some(language))
                    }
                }
            } header: {
                Text("settings.language")
            } footer: {
                Text("settings.language.footer")
            }

            Section {
                Text("settings.privacy.body")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                Button("legal.terms") { openLegal(account.legalConfig.termsURL) }
                Button("legal.privacy") { openLegal(account.legalConfig.privacyURL) }
                // The AI consent can be taken back as easily as it was given;
                // the consent screen then returns until it is given again.
                Button("settings.privacy.withdraw", role: .destructive) {
                    isConfirmingWithdrawal = true
                }
                .disabled(account.isBusy)
                .confirmationDialog("settings.privacy.withdraw", isPresented: $isConfirmingWithdrawal,
                                    titleVisibility: .visible) {
                    Button("settings.privacy.withdraw", role: .destructive) {
                        Task { withdrawalFailureKey = await account.withdrawLegalConsent() }
                    }
                    Button("common.cancel", role: .cancel) {}
                } message: {
                    Text("settings.privacy.withdraw.body")
                }
                .alert(Text(LocalizedStringKey(withdrawalFailureKey ?? "account.error.generic")),
                       isPresented: withdrawalFailureBinding) {
                    Button("common.done", role: .cancel) {}
                }
            } header: {
                Text("settings.privacy.title")
            }

            // The guide again, on top of everything: nothing is reset and the
            // screen underneath is still here when it closes.
            Section {
                Button("settings.tutorial") {
                    ProductEvents.track(.onboardingReopened)
                    isShowingTutorial = true
                }
            } footer: {
                Text("settings.tutorial.footer")
            }

            Section {
                Button {
                    openLegal(account.legalConfig.resolvedSupportURL)
                } label: {
                    Label("settings.support", systemImage: "questionmark.circle")
                }
            }

            if !AppGroup.isAvailable || !model.isPersistent {
                Section {
                    Label("settings.storage.unavailable", systemImage: "exclamationmark.triangle")
                        .font(.footnote)
                        .foregroundStyle(Color.orange)
                }
            }
        }
    }

    private var withdrawalFailureBinding: Binding<Bool> {
        Binding(get: { withdrawalFailureKey != nil }, set: { if !$0 { withdrawalFailureKey = nil } })
    }

    private var appearanceBinding: Binding<AppearancePreference> {
        Binding(get: { settings.appearance }, set: { settings.setAppearance($0) })
    }

    private func layoutBinding(_ language: KeyboardLanguage) -> Binding<Bool> {
        Binding(
            get: { settings.keyboardLanguages.contains(language) },
            set: { settings.setKeyboardLanguage(language, enabled: $0) }
        )
    }

    private var hapticsBinding: Binding<Bool> {
        Binding(get: { settings.keyboardHaptics }, set: { settings.setKeyboardHaptics($0) })
    }

    private var smartCorrectionBinding: Binding<Bool> {
        Binding(
            get: { settings.smartCorrection },
            set: { enabled in
                settings.setSmartCorrection(enabled)
                ProductEvents.track(enabled ? .autocorrectEnabled : .autocorrectDisabled)
            }
        )
    }

    private var languageBinding: Binding<AppLanguage?> {
        Binding(get: { settings.language }, set: { language in
            settings.setLanguage(language)
            // Also the language of the account's notifications.
            PreferredLanguageSync(account: account, settings: settings).languageChosen()
        })
    }

    private func openLegal(_ rawURL: String) {
        guard var components = URLComponents(string: rawURL) else { return }
        components.queryItems = [URLQueryItem(name: "lang", value: settings.effectiveLanguage.rawValue)]
        if let url = components.url { openURL(url) }
    }
}
