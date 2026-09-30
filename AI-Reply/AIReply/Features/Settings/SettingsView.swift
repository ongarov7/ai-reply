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
    @Environment(PushNotificationsModel.self) private var notifications
    @Environment(\.openURL) private var openURL

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

            DiagnosticsSettingsSection()

            Section {
                Text("settings.privacy.body")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                Button("legal.terms") { openLegal(account.legalConfig.termsURL) }
                Button("legal.privacy") { openLegal(account.legalConfig.privacyURL) }
            } header: {
                Text("settings.privacy.title")
            }

            Section {
                Button("settings.setup.restart") { model.restartOnboarding() }
            } footer: {
                Text("settings.setup.restart.footer")
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

    private var languageBinding: Binding<AppLanguage?> {
        Binding(get: { settings.language }, set: { settings.setLanguage($0) })
    }

    private func openLegal(_ rawURL: String) {
        guard var components = URLComponents(string: rawURL) else { return }
        components.queryItems = [URLQueryItem(name: "lang", value: settings.effectiveLanguage.rawValue)]
        if let url = components.url { openURL(url) }
    }
}
