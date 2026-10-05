import SwiftUI

struct SettingsView: View {

    @Environment(AppSettings.self) private var settings
    @Environment(ReplyConfigurationModel.self) private var model
    @Environment(AccountModel.self) private var account
    @Environment(KeyboardStatusMonitor.self) private var keyboard
    @Environment(\.openURL) private var openURL

    @State private var isShowingTutorial = false

    var body: some View {
        Form {
            AccountSettingsSection()

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

            if !AppGroup.isAvailable || !model.isPersistent {
                Section {
                    Label("settings.storage.unavailable", systemImage: "exclamationmark.triangle")
                        .font(.footnote)
                        .foregroundStyle(Color.orange)
                }
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
        Binding(get: { settings.language }, set: { settings.setLanguage($0) })
    }

    private func openLegal(_ rawURL: String) {
        guard var components = URLComponents(string: rawURL) else { return }
        components.queryItems = [URLQueryItem(name: "lang", value: settings.effectiveLanguage.rawValue)]
        if let url = components.url { openURL(url) }
    }
}
