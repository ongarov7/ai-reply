import SwiftUI

/// «Создать ответ»: the incoming message, who it is for, the reply.
///
/// «Создать ответ»: кіріс хабарлама, кімге жауап, дайын жауап.
///
/// It exists for two reasons. It is how the microphone reaches the AI at all,
/// since a keyboard extension cannot capture audio. And it lets someone check
/// their profile and templates produce the replies they expect without
/// switching to WhatsApp to find out.
///
/// The message comes in through the system Paste button, which iOS fills on
/// the user's tap without asking - the app never reads the clipboard
/// itself - or by typing, dictating, or an example. The recipient is the
/// keyboard's own selected persona (`SharedSettings.lastTemplateID`).
struct ComposeView: View {

    @Environment(ReplyConfigurationModel.self) private var model
    @Environment(AppSettings.self) private var settings
    @Environment(AccountModel.self) private var account
    @Environment(AppRouter.self) private var router

    @State private var viewModel = ComposeViewModel()
    /// Where the exhausted sheet's choice leads, once the sheet is down.
    @State private var routeAfterSheet: AppRoute?
    @State private var isDictating = false
    @State private var isReporting = false
    @FocusState private var isFocused: Bool

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: DS.Spacing.l) {
                VStack(alignment: .leading, spacing: DS.Spacing.xs) {
                    messageCard
                    Button("compose.sample") {
                        viewModel.useSample(settings.localized("compose.sample.message"))
                    }
                    .font(.subheadline)
                    .frame(minHeight: DS.Layout.minimumTouchTarget)
                }
                recipients
                if !viewModel.reply.isEmpty { replyCard }
                if let error = viewModel.errorMessage {
                    Label(error, systemImage: "exclamationmark.triangle")
                        .font(.footnote)
                        .foregroundStyle(Color.orange)
                }
            }
            .padding(DS.Spacing.l)
            .frame(maxWidth: DS.Layout.readableWidth, alignment: .leading)
            .frame(maxWidth: .infinity)
        }
        .scrollDismissesKeyboard(.interactively)
        .background(Color.dsBackground)
        .safeAreaInset(edge: .bottom) { createBar }
        .navigationTitle("compose.title")
        .navigationBarTitleDisplayMode(.inline)
        .onAppear { viewModel.restoreRecipient(visible: model.visibleTemplates) }
        .onChange(of: model.visibleTemplates.map(\.id)) {
            viewModel.restoreRecipient(visible: model.visibleTemplates)
        }
        .onChange(of: viewModel.message) { viewModel.messageDidChange() }
        .onDisappear { viewModel.cancel() }
        .sheet(isPresented: $isDictating) {
            DictationSheet(language: settings.effectiveLanguage) { transcript in
                viewModel.appendDictated(transcript)
            }
        }
        .sheet(isPresented: $isReporting) {
            AIReportSheet(mode: .reply, text: viewModel.reply)
        }
        // The replies are spent: what can be done, over this screen, which
        // stays as it is.
        .sheet(item: exhaustedBinding, onDismiss: {
            if let route = routeAfterSheet {
                routeAfterSheet = nil
                router.path.append(route)
            }
        }) { offer in
            ExhaustedSheetView(offer: offer, gender: model.profile.grammaticalGender,
                               onBalance: { leaveSheet(for: .subscription) })
        }
        // The terms were not accepted on the server: back to the consent screen.
        .onChange(of: viewModel.failure) { _, failure in
            if failure == .consentRequired {
                Task { await account.serverRequiresConsent() }
            }
        }
    }

    // MARK: Message

    private var messageCard: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.s) {
            Text("compose.message")
                .font(.title2.weight(.bold))
                .accessibilityAddTraits(.isHeader)

            VStack(alignment: .leading, spacing: DS.Spacing.s) {
                TextEditor(text: $viewModel.message)
                    .font(.body)
                    .scrollContentBackground(.hidden)
                    .frame(minHeight: 160)
                    .focused($isFocused)
                    .accessibilityLabel(Text("compose.message"))
                    .overlay(alignment: .topLeading) {
                        if viewModel.message.isEmpty {
                            Text("compose.message.placeholder")
                                .foregroundStyle(.tertiary)
                                .padding(.top, 8)
                                .padding(.leading, 5)
                                .allowsHitTesting(false)
                        }
                    }

                ViewThatFits(in: .horizontal) {
                    HStack(spacing: DS.Spacing.s) {
                        counter
                        Spacer(minLength: DS.Spacing.xs)
                        inputButtons
                    }
                    VStack(alignment: .leading, spacing: DS.Spacing.s) {
                        counter
                        HStack(spacing: DS.Spacing.s) { inputButtons }
                    }
                    VStack(alignment: .leading, spacing: DS.Spacing.s) {
                        counter
                        inputButtons
                    }
                }

                if let key = viewModel.pasteNoticeKey {
                    Text(LocalizedStringKey(key))
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }

                if viewModel.isOverLimit {
                    Text(AIReplyStrings.forLanguage(settings.effectiveLanguage).messageTooLong(limit: viewModel.characterLimit))
                        .font(.footnote)
                        .foregroundStyle(Color.red)
                }
            }
            .dsCard()
        }
    }

    @ViewBuilder
    private var inputButtons: some View {
        PasteButton(payloadType: String.self) { strings in
            Task { @MainActor in viewModel.paste(strings.first) }
        }
        .labelStyle(.titleAndIcon)
        .buttonBorderShape(.capsule)
        .tint(Color.accentColor)

        Button {
            isFocused = false
            isDictating = true
        } label: {
            Label("profile.dictate", systemImage: "mic")
        }
        .buttonStyle(.bordered)
        .buttonBorderShape(.capsule)
    }

    private var counter: some View {
        Text(counterText)
            .font(.caption.monospacedDigit())
            .foregroundStyle(viewModel.isOverLimit ? Color.red : .secondary)
    }

    // MARK: Recipient

    private var recipients: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.s) {
            Text("compose.recipients")
                .font(.title2.weight(.bold))
                .accessibilityAddTraits(.isHeader)

            RecipientGrid(
                items: RecipientGrid.items(for: model.visibleTemplates, language: settings.effectiveLanguage),
                selection: viewModel.selectedTemplateID,
                onSelect: viewModel.selectRecipient
            )

            if !customTemplates.isEmpty {
                VStack(spacing: 0) {
                    ForEach(Array(customTemplates.enumerated()), id: \.element.id) { index, template in
                        if index > 0 { Divider().padding(.leading, 52) }
                        customRow(template)
                    }
                }
                .background(
                    RoundedRectangle(cornerRadius: DS.Radius.medium, style: .continuous).fill(Color.dsSurface)
                )
            }

            Text("compose.hint")
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
    }

    /// Templates the user made: a row each under the grid.
    private var customTemplates: [ReplyTemplate] {
        model.visibleTemplates.filter { !$0.isBuiltIn }
    }

    private func customRow(_ template: ReplyTemplate) -> some View {
        let isSelected = viewModel.selectedTemplateID == template.id
        return Button {
            viewModel.selectRecipient(template.id)
        } label: {
            HStack(spacing: DS.Spacing.s) {
                Image(systemName: template.relationship.symbolName)
                    .foregroundStyle(Color.accentColor)
                    .frame(width: 24)
                    .accessibilityHidden(true)
                Text(verbatim: template.recipientName(appLanguage: settings.effectiveLanguage))
                    .foregroundStyle(.primary)
                Spacer(minLength: 0)
                if isSelected {
                    Image(systemName: "checkmark")
                        .font(.body.weight(.semibold))
                        .foregroundStyle(Color.accentColor)
                        .accessibilityHidden(true)
                }
            }
            .padding(.horizontal, DS.Spacing.m)
            .frame(minHeight: DS.Layout.minimumTouchTarget + 4)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(isSelected ? [.isSelected] : [])
    }

    // MARK: Reply

    private var replyCard: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.s) {
            Text("compose.result")
                .font(.headline)
                .accessibilityAddTraits(.isHeader)
            Text(viewModel.reply)
                .font(.body)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)

            HStack(spacing: DS.Spacing.s) {
                Button {
                    viewModel.copyReply()
                } label: {
                    Label(viewModel.didCopy ? "compose.copied" : "common.copy",
                          systemImage: viewModel.didCopy ? "checkmark" : "doc.on.doc")
                }
                .buttonStyle(.dsOutline)

                Button {
                    generate()
                } label: {
                    Label("compose.regenerate", systemImage: "arrow.clockwise")
                        .font(.subheadline)
                }
                .buttonStyle(.borderless)
                .disabled(viewModel.isGenerating)

                Spacer(minLength: 0)

                if account.features?.aiReports == true {
                    Button {
                        isReporting = true
                    } label: {
                        Image(systemName: "flag")
                    }
                    .buttonStyle(.borderless)
                    .foregroundStyle(.secondary)
                    .accessibilityLabel(Text(verbatim: reportStrings.report))
                    .disabled(viewModel.isGenerating)
                }
            }
        }
        .dsCard()
    }

    // MARK: Create

    private var createBar: some View {
        VStack(spacing: DS.Spacing.xs) {
            if viewModel.characterCount == 0 {
                Text("compose.hint.empty")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            Button(action: generate) {
                HStack(spacing: DS.Spacing.xs) {
                    if viewModel.isGenerating { ProgressView().tint(.white) }
                    Text("compose.generate")
                }
            }
            .buttonStyle(.dsPrimary)
            .disabled(!viewModel.canGenerate)
        }
        .padding(.horizontal, DS.Spacing.l)
        .padding(.vertical, DS.Spacing.s)
        .frame(maxWidth: DS.Layout.readableWidth)
        .frame(maxWidth: .infinity)
        .background(Color.dsBackground)
    }

    private func generate() {
        isFocused = false
        viewModel.generate(configuration: model.configuration, language: settings.effectiveLanguage)
    }

    private var exhaustedBinding: Binding<ExhaustedOffer?> {
        Binding(get: { viewModel.exhausted }, set: { if $0 == nil { viewModel.dismissExhausted() } })
    }

    private func leaveSheet(for route: AppRoute) {
        routeAfterSheet = route
        viewModel.dismissExhausted()
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

    private var strings: ReportStrings { ReportStrings.forLanguage(settings.effectiveLanguage).forMode(mode) }

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
