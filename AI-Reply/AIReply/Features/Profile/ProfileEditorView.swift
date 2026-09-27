import SwiftUI

/// "About me" - the context that is true of the user in every conversation.
///
/// It asks normal questions. There is no prompt field, no system-message box
/// and no mention of a model anywhere on this screen: the user says what they
/// do and what must never be promised, and `ReplyPromptBuilder` turns that into
/// context. That separation is deliberate - it means the prompt can be improved
/// without asking anyone to rewrite their answers.
///
/// Dictation happens HERE, in the containing app, because an iOS keyboard
/// extension cannot reliably capture audio and this project does not pretend
/// otherwise.
struct ProfileEditorView: View {

    @Environment(ReplyConfigurationModel.self) private var model
    @Environment(AppSettings.self) private var settings

    @State private var role: String = ""
    @State private var about: String = ""
    @State private var business: BusinessContext = .empty
    @State private var tone: ReplyTone = .natural
    @State private var replyLanguage: ReplyLanguagePreference?
    @State private var isDictating = false
    @State private var suggestion: VoiceConfigurationParser.Suggestion?
    @FocusState private var isFocused: Bool

    var body: some View {
        Form {
            Section {
                TextField("profile.role.placeholder", text: $role, axis: .vertical)
                    .onChange(of: role) { _, value in
                        role = String(value.prefix(UserProfile.maximumRoleCharacters))
                    }
            } header: {
                Text("profile.role")
            }

            Section {
                TextField("profile.offering.placeholder", text: offeringBinding, axis: .vertical)
                TextField("profile.summary.placeholder", text: summaryBinding, axis: .vertical)
                    .lineLimit(2...5)
            } header: {
                Text("profile.business")
            } footer: {
                Text("profile.business.footer")
            }

            RulesEditor(rules: rulesBinding, footerKey: "profile.rules.footer")

            Section {
                TextEditor(text: $about)
                    .frame(minHeight: 130)
                    .focused($isFocused)
                    .onChange(of: about) { _, newValue in
                        // Clamp as the user types rather than silently
                        // truncating on save, so the counter never lies.
                        if newValue.unicodeScalars.count > UserProfile.maximumDescriptionCharacters {
                            about = String(newValue.prefix(UserProfile.maximumDescriptionCharacters))
                        }
                    }

                HStack {
                    Text(counterText)
                        .font(.caption)
                        .foregroundStyle(isNearLimit ? Color.orange : .secondary)
                        .monospacedDigit()
                    Spacer()
                    Button {
                        isFocused = false
                        isDictating = true
                    } label: {
                        Label("profile.dictate", systemImage: "mic.fill")
                            .font(.subheadline)
                    }
                }
            } header: {
                Text("profile.about")
            } footer: {
                Text("profile.description.footer")
            }

            Section("profile.tone") {
                TonePicker(selection: $tone)
            }

            Section {
                Picker("profile.replyLanguage", selection: $replyLanguage) {
                    Text("profile.replyLanguage.auto").tag(ReplyLanguagePreference?.none)
                    ForEach(ReplyLanguagePreference.allCases) { language in
                        Text(verbatim: language.nativeName).tag(ReplyLanguagePreference?.some(language))
                    }
                }
            } footer: {
                Text("profile.replyLanguage.footer")
            }
        }
        .navigationTitle("profile.title")
        .navigationBarTitleDisplayMode(.inline)
        .onAppear(perform: loadExisting)
        // Saved on the way out rather than on every keystroke: the store writes
        // a file the keyboard reads, and rewriting it per character would be
        // pointless disk traffic.
        .onDisappear(perform: save)
        .sheet(isPresented: $isDictating) {
            DictationSheet(language: settings.effectiveLanguage) { transcript in
                acceptDictation(transcript)
            }
        }
        .sheet(item: $suggestion) { value in
            VoiceSuggestionSheet(suggestion: value, existingHours: model.profile.workingHours) { confirmed in
                apply(confirmed)
            }
        }
    }

    // MARK: Bindings

    private var offeringBinding: Binding<String> {
        Binding(get: { business.offering }, set: { business.setOffering($0) })
    }

    private var summaryBinding: Binding<String> {
        Binding(get: { business.summary }, set: { business.setSummary($0) })
    }

    private var rulesBinding: Binding<BusinessContext> {
        Binding(get: { business }, set: { business = $0 })
    }

    private var counterText: String {
        String(
            format: settings.localized("profile.counter"),
            about.unicodeScalars.count,
            UserProfile.maximumDescriptionCharacters
        )
    }

    private var isNearLimit: Bool {
        about.unicodeScalars.count > UserProfile.maximumDescriptionCharacters - 80
    }

    // MARK: State

    private func loadExisting() {
        let profile = model.profile
        role = profile.role
        about = profile.descriptionText
        business = profile.business
        tone = profile.preferredTone
        replyLanguage = profile.replyLanguage
    }

    private func save() {
        model.updateProfile {
            $0.setRole(role)
            $0.setDescription(about)
            $0.business = business
            $0.preferredTone = tone
            $0.replyLanguage = replyLanguage
        }
    }

    /// What the user said is kept verbatim; what the app recognised in it is
    /// offered separately and applied only on confirmation.
    private func acceptDictation(_ transcript: String) {
        let addition = transcript.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !addition.isEmpty else { return }
        let separator = about.isEmpty || about.hasSuffix(" ") ? "" : " "
        about = String((about + separator + addition).prefix(UserProfile.maximumDescriptionCharacters))

        let parsed = VoiceConfigurationParser.parse(addition, language: settings.effectiveLanguage)
        if !parsed.isEmpty { suggestion = parsed }
    }

    private func apply(_ confirmed: VoiceConfigurationParser.Suggestion) {
        if let updated = confirmed.workingHours(basedOn: model.profile.workingHours) {
            model.updateProfile { $0.workingHours = updated }
        }
        for rule in confirmed.rules { business.addRule(rule) }
    }
}

// MARK: - Rules

/// The "things the assistant must never promise" list, shared by the profile
/// and every template editor.
///
/// A list of short lines rather than one paragraph, because that is what the
/// model follows most reliably and what the user can edit one item at a time.
struct RulesEditor: View {

    @Binding var rules: BusinessContext
    var footerKey: LocalizedStringKey

    var body: some View {
        Section {
            ForEach(Array(rules.rules.enumerated()), id: \.offset) { index, _ in
                TextField(
                    "profile.rules.placeholder",
                    text: Binding(
                        get: { index < rules.rules.count ? rules.rules[index] : "" },
                        set: { rules.setRule($0, at: index) }
                    ),
                    axis: .vertical
                )
            }
            .onDelete { offsets in
                rules.removeRules(at: offsets)
            }

            if rules.rules.count < BusinessContext.maximumRules {
                Button {
                    rules.addRule()
                } label: {
                    Label("profile.rules.add", systemImage: "plus")
                }
            }
        } header: {
            Text("profile.rules")
        } footer: {
            Text(footerKey)
        }
    }
}
