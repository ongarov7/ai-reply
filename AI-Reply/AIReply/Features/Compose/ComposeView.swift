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

    @State private var viewModel = ComposeViewModel()
    @State private var isDictating = false
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
        }
    }

    private var counterText: String {
        String(format: settings.localized("compose.counter"), viewModel.characterCount, viewModel.characterLimit)
    }
}
