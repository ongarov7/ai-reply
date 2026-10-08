import SwiftUI

/// The in-app reply flow: paste or dictate a message, pick who it is from,
/// generate a reply.
///
/// It exists for two reasons. It is how the microphone reaches the AI at all,
/// since a keyboard extension cannot capture audio. And it lets someone check
/// their profile and templates produce the replies they expect without
/// switching to WhatsApp to find out.
struct ComposeView: View {

    @Environment(ReplyConfigurationModel.self) private var model
    @Environment(AppSettings.self) private var settings
    @Environment(AccountModel.self) private var account

    @State private var viewModel = ComposeViewModel()
    @State private var isDictating = false
    @State private var isReporting = false
    @FocusState private var isFocused: Bool

    var body: some View {
        Form {
            messageSection
            templateSection
            generateSection
            if !viewModel.reply.isEmpty { replySection }
        }
        .navigationTitle("compose.title")
        .navigationBarTitleDisplayMode(.inline)
        .onAppear {
            if viewModel.selectedTemplateID == nil {
                viewModel.selectedTemplateID = model.visibleTemplates.first?.id
            }
        }
        .onDisappear { viewModel.cancel() }
        .sheet(isPresented: $isDictating) {
            DictationSheet(language: settings.effectiveLanguage) { transcript in
                viewModel.appendDictated(transcript)
            }
        }
        .sheet(isPresented: $isReporting) {
            AIReportSheet(mode: .reply, text: viewModel.reply)
        }
        // The terms were not accepted on the server: back to the consent screen.
        .onChange(of: viewModel.failure) { _, failure in
            if failure == .consentRequired {
                Task { await account.serverRequiresConsent() }
            }
        }
    }

    // MARK: Sections

    private var messageSection: some View {
        Section {
            TextEditor(text: $viewModel.message)
                .frame(minHeight: 110)
                .focused($isFocused)
                .overlay(alignment: .topLeading) {
                    if viewModel.message.isEmpty {
                        Text("compose.message.placeholder")
                            .foregroundStyle(.tertiary)
                            .padding(.top, 8)
                            .padding(.leading, 5)
                            .allowsHitTesting(false)
                    }
                }

            HStack(spacing: DS.Spacing.m) {
                Text(counterText)
                    .font(.caption.monospacedDigit())
                    .foregroundStyle(viewModel.isOverLimit ? Color.red : .secondary)
                Spacer()
                Button {
                    viewModel.pasteFromClipboard()
                } label: {
                    Label("compose.paste", systemImage: "doc.on.clipboard").font(.subheadline)
                }
                Button {
                    isFocused = false
                    isDictating = true
                } label: {
                    Label("profile.dictate", systemImage: "mic.fill").font(.subheadline)
                }
            }

            if viewModel.isOverLimit {
                // The exact wording the keyboard shows, so the rule reads the
                // same wherever the user meets it.
                Text(AIReplyStrings.forLanguage(settings.effectiveLanguage).messageTooLong(limit: viewModel.characterLimit))
                    .font(.caption)
                    .foregroundStyle(Color.red)
            }
        } header: {
            Text("compose.message")
        }
    }

    private var templateSection: some View {
        Section("compose.template") {
            Picker("compose.template", selection: $viewModel.selectedTemplateID) {
                ForEach(model.visibleTemplates) { template in
                    Text(template.displayName(appLanguage: settings.effectiveLanguage))
                        .tag(String?.some(template.id))
                }
            }
            .labelsHidden()
            .pickerStyle(.segmented)
        }
    }

    private var generateSection: some View {
        Section {
            Button {
                isFocused = false
                viewModel.generate(configuration: model.configuration, language: settings.effectiveLanguage)
            } label: {
                HStack {
                    if viewModel.isGenerating { ProgressView().padding(.trailing, DS.Spacing.xs) }
                    Text(viewModel.isGenerating ? "compose.generate" : "compose.generate")
                }
                .frame(maxWidth: .infinity)
            }
            .buttonStyle(.dsPrimary)
            .disabled(!viewModel.canGenerate)
            .listRowInsets(EdgeInsets())
            .listRowBackground(Color.clear)

            if let error = viewModel.errorMessage {
                Label(error, systemImage: "exclamationmark.triangle")
                    .font(.footnote)
                    .foregroundStyle(Color.orange)
            }
        } footer: {
            Text("compose.hint")
        }
    }

    private var replySection: some View {
        Section("compose.result") {
            Text(viewModel.reply)
                .font(.body)
                .textSelection(.enabled)

            HStack {
                Button {
                    viewModel.copyReply()
                } label: {
                    Label(viewModel.didCopy ? "compose.copied" : "common.copy",
                          systemImage: viewModel.didCopy ? "checkmark" : "doc.on.doc")
                    .font(.subheadline)
                }
                Spacer()
                Button {
                    viewModel.generate(configuration: model.configuration, language: settings.effectiveLanguage)
                } label: {
                    Label("compose.regenerate", systemImage: "arrow.clockwise").font(.subheadline)
                }
                .disabled(viewModel.isGenerating)
            }
            // Each button its own tap target inside the row.
            .buttonStyle(.borderless)

            if account.features?.aiReports == true {
                Button {
                    isReporting = true
                } label: {
                    Label(reportStrings.report, systemImage: "flag").font(.subheadline)
                }
                .buttonStyle(.borderless)
                .foregroundStyle(.secondary)
                .disabled(viewModel.isGenerating)
            }
        }
    }

    private var reportStrings: ReportStrings { ReportStrings.forLanguage(settings.effectiveLanguage) }

    private var counterText: String {
        String(format: settings.localized("compose.counter"), viewModel.characterCount, viewModel.characterLimit)
    }
}

/// Reporting a generated reply: a reason, whether its text goes along, Send.
///
/// Жауапқа шағым: себеп, мәтінді қосу, жіберу.
///
/// The words are the keyboard's (`ReportStrings`), so both say the same.
/// Only the reason and, when the switch stays on, the reply itself are sent.
struct AIReportSheet: View {

    let mode: AIReport.Mode
    let text: String

    @Environment(AppSettings.self) private var settings
    @Environment(\.dismiss) private var dismiss

    @State private var reason: AIReport.Reason?
    @State private var includesText = true
    @State private var isSending = false
    @State private var didSend = false
    @State private var didFail = false

    private var strings: ReportStrings { ReportStrings.forLanguage(settings.effectiveLanguage) }

    var body: some View {
        NavigationStack {
            Form {
                if didSend {
                    Section {
                        Label(strings.thanks, systemImage: "checkmark.circle")
                    }
                } else {
                    Section {
                        Picker(selection: $reason) {
                            ForEach(AIReport.Reason.allCases, id: \.self) { reason in
                                Text(strings.reason(reason)).tag(AIReport.Reason?.some(reason))
                            }
                        } label: {
                            Text(strings.title)
                        }
                        .pickerStyle(.inline)
                        .labelsHidden()
                    }
                    Section {
                        Toggle(strings.includeText, isOn: $includesText)
                    }
                    Section {
                        Button {
                            Task { await send() }
                        } label: {
                            HStack {
                                Text(strings.send)
                                if isSending {
                                    Spacer()
                                    ProgressView()
                                }
                            }
                        }
                        .disabled(reason == nil || isSending)
                    } footer: {
                        if didFail {
                            Text(strings.failed).foregroundStyle(Color.red)
                        }
                    }
                }
            }
            .navigationTitle(strings.title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: didSend ? .confirmationAction : .cancellationAction) {
                    Button(didSend ? LocalizedStringKey("common.done") : LocalizedStringKey("common.cancel")) {
                        dismiss()
                    }
                }
            }
        }
        .presentationDetents([.medium, .large])
    }

    private func send() async {
        guard let reason else { return }
        isSending = true
        didFail = false
        defer { isSending = false }
        do {
            try await AccountService().reportAIOutput(
                AIReport(mode: mode, reason: reason, text: includesText ? text : nil)
            )
            didSend = true
        } catch {
            didFail = true
        }
    }
}
