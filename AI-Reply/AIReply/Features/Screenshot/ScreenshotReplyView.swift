import PhotosUI
import SwiftUI

/// «Ответ по скриншоту»: the latest screenshot, the conversation read from it
/// (who wrote what, which message to answer), an instruction typed or spoken,
/// the price, and the reply — which then waits in the AI Reply keyboard.
struct ScreenshotReplyView: View {

    @Environment(\.dismiss) private var dismiss
    @Environment(AppSettings.self) private var settings
    @Environment(ReplyConfigurationModel.self) private var configuration

    @State private var model = ScreenshotReplyModel()
    @State private var monitor = ScreenshotIntakeMonitor.shared
    @State private var recorder: ScreenshotInstructionRecorder?
    @State private var pickerItem: PhotosPickerItem?
    @State private var showsPicker = false
    @State private var showsCrop = false
    @State private var showsSetup = false
    @State private var showsImageConsent = false
    @State private var showsPricing = false
    @State private var editing: ScreenshotMessage?
    @State private var introSeen = ScreenshotConsent.hasSeenIntro
    @FocusState private var instructionFocused: Bool

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: DS.Spacing.l) {
                    if !introSeen {
                        introCard
                    } else if monitor.isReading || model.isWorking {
                        readingCard
                    } else if !model.hasScreenshot {
                        emptyState
                    } else {
                        screenshotHeader
                        conversationSection
                        instructionSection
                        optionsSection
                        generateSection
                        if model.reply != nil { resultSection }
                    }
                    if let notice = model.notice {
                        Text(notice)
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .accessibilityIdentifier("screenshot.notice")
                    }
                }
                .padding(DS.Spacing.m)
                .frame(maxWidth: DS.Layout.readableWidth)
                .frame(maxWidth: .infinity)
            }
            .scrollDismissesKeyboard(.interactively)
            .background(Color.dsBackground)
            .navigationTitle("screenshot.title")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("common.close") {
                        recorder?.cancel()
                        dismiss()
                    }
                }
                ToolbarItem(placement: .primaryAction) {
                    Button {
                        showsSetup = true
                    } label: {
                        Image(systemName: "questionmark.circle")
                    }
                    .accessibilityLabel(Text("screenshot.setup.title"))
                }
            }
            .photosPicker(isPresented: $showsPicker, selection: $pickerItem, matching: .screenshots,
                          preferredItemEncoding: .compatible)
            .onChange(of: pickerItem) { _, item in
                guard let item else { return }
                pickerItem = nil
                Task {
                    if let data = try? await item.loadTransferable(type: Data.self) {
                        await model.importPicked(data)
                    }
                }
            }
            .sheet(isPresented: $showsCrop) {
                if let image = model.image {
                    ScreenshotCropView(image: image) { rect in
                        Task { await model.crop(to: rect) }
                    }
                }
            }
            .sheet(isPresented: $showsSetup) { ScreenshotSetupView() }
            .sheet(isPresented: $showsImageConsent) {
                ScreenshotImageConsentView { allowed in
                    if allowed {
                        ScreenshotConsent.allowImage()
                        model.mode = .image
                    } else {
                        model.mode = .text
                    }
                    model.modeChanged()
                }
                .presentationDetents([.medium, .large])
            }
            .sheet(item: $editing) { message in
                ScreenshotMessageEditor(message: message) { text in model.setText(text, for: message) }
            }
            .alert(Text("screenshot.confirm.title"), isPresented: approvalShown, presenting: model.pendingApproval) { quote in
                Button(String(format: localized("screenshot.confirm.generate %lld"), quote.maxUnits)) {
                    model.generate(approved: quote.maxUnits)
                }
                if model.mode == .image {
                    Button("screenshot.confirm.useText") {
                        model.mode = .text
                        model.modeChanged()
                    }
                }
                Button("common.cancel", role: .cancel) {}
            } message: { quote in
                Text(confirmationMessage(quote))
            }
            .alert(Text("screenshot.insufficient.title"), isPresented: insufficientShown, presenting: model.insufficient) { _ in
                if model.mode == .image {
                    Button("screenshot.confirm.useText") {
                        model.mode = .text
                        model.modeChanged()
                    }
                }
                Button("common.done", role: .cancel) {}
            } message: { value in
                Text(String(format: localized("screenshot.insufficient.body %lld %lld"),
                            value.quote.maxUnits, value.quote.availableUnits ?? 0))
            }
        }
        .onAppear { model.appear(language: settings.effectiveLanguage) }
        .onDisappear {
            recorder?.cancel()
            model.disappear()
        }
        .onChange(of: monitor.isReading) { _, reading in
            if !reading { model.reload(keepEdits: false) }
        }
    }

    // MARK: Sections

    private var introCard: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.m) {
            Label("screenshot.intro.title", systemImage: "camera.viewfinder")
                .font(.title3.weight(.semibold))
            Text("screenshot.intro.body")
                .font(.callout)
                .fixedSize(horizontal: false, vertical: true)
            Text("screenshot.intro.privacy")
                .font(.footnote)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
            Button {
                ScreenshotConsent.markIntroSeen()
                withAnimation { introSeen = true }
            } label: {
                Text("screenshot.intro.continue").frame(maxWidth: .infinity)
            }
            .buttonStyle(.dsPrimary)
            .accessibilityIdentifier("screenshot.intro.continue")
        }
        .dsCard()
    }

    private var readingCard: some View {
        HStack(spacing: DS.Spacing.s) {
            ProgressView()
            Text("screenshot.reading")
                .font(.callout)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .dsCard()
        .accessibilityElement(children: .combine)
    }

    private var emptyState: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.m) {
            Image(systemName: "camera.viewfinder")
                .font(.system(size: 40))
                .foregroundStyle(Color.accentColor)
                .accessibilityHidden(true)
            Text("screenshot.empty.title")
                .font(.title3.weight(.semibold))
            Text("screenshot.empty.body")
                .font(.callout)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
            Button {
                showsPicker = true
            } label: {
                Label("screenshot.choose", systemImage: "photo.on.rectangle").frame(maxWidth: .infinity)
            }
            .buttonStyle(.dsPrimary)
            .accessibilityIdentifier("screenshot.choose")
            Button {
                showsSetup = true
            } label: {
                Label("screenshot.setup.button", systemImage: "switch.2").frame(maxWidth: .infinity)
            }
            .buttonStyle(.dsSecondary)
        }
        .dsCard()
    }

    private var screenshotHeader: some View {
        HStack(alignment: .top, spacing: DS.Spacing.s) {
            if let image = model.image {
                Image(uiImage: image)
                    .resizable()
                    .scaledToFill()
                    .frame(width: 64, height: 96)
                    .clipShape(RoundedRectangle(cornerRadius: DS.Radius.small, style: .continuous))
                    .overlay(RoundedRectangle(cornerRadius: DS.Radius.small, style: .continuous).stroke(Color.dsSeparator))
                    .accessibilityLabel(Text("screenshot.preview"))
            }
            VStack(alignment: .leading, spacing: DS.Spacing.xxs) {
                Text(headerTitle)
                    .font(.headline)
                    .lineLimit(2)
                Text(String(format: localized("screenshot.header.messages %lld"), model.conversation.messages.count))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                if let context = model.context {
                    Text(String(format: localized("screenshot.header.expires %@"),
                                context.expiresAt.formatted(date: .omitted, time: .shortened)))
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            Spacer(minLength: 0)
            Menu {
                Button { showsCrop = true } label: { Label("screenshot.crop", systemImage: "crop") }
                Button { showsPicker = true } label: { Label("screenshot.replace", systemImage: "photo.on.rectangle") }
                Button(role: .destructive) {
                    recorder?.cancel()
                    model.deleteScreenshot()
                } label: {
                    Label("screenshot.delete", systemImage: "trash")
                }
            } label: {
                Image(systemName: "ellipsis.circle")
                    .font(.title3)
                    .frame(minWidth: DS.Layout.minimumTouchTarget, minHeight: DS.Layout.minimumTouchTarget)
            }
            .accessibilityLabel(Text("screenshot.actions"))
            .accessibilityIdentifier("screenshot.actions")
        }
        .dsCard()
    }

    private var headerTitle: String {
        let conversation = model.conversation
        switch (conversation.appName, conversation.title) {
        case let (app?, title?): return "\(title) · \(app)"
        case let (nil, title?): return title
        case let (app?, nil): return app
        default: return localized("screenshot.header.untitled")
        }
    }

    private var conversationSection: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.s) {
            Text("screenshot.conversation.title")
                .font(.footnote.weight(.semibold))
                .foregroundStyle(.secondary)
                .textCase(.uppercase)
            if model.context?.hasNoText == true || model.conversation.messages.isEmpty {
                Text("screenshot.conversation.none")
                    .font(.callout)
                    .foregroundStyle(.secondary)
            } else {
                if model.conversation.needsReview {
                    Label("screenshot.conversation.review", systemImage: "exclamationmark.triangle")
                        .font(.footnote)
                        .foregroundStyle(Color.orange)
                        .fixedSize(horizontal: false, vertical: true)
                }
                ForEach(model.conversation.messages) { message in
                    bubble(message)
                }
                Text("screenshot.conversation.hint")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }

    private func bubble(_ message: ScreenshotMessage) -> some View {
        let isTarget = model.target?.id == message.id
        let mine = message.role == .me
        return HStack {
            if mine { Spacer(minLength: 40) }
            VStack(alignment: .leading, spacing: DS.Spacing.xxs) {
                if let sender = message.sender {
                    Text(sender).font(.caption.weight(.semibold)).foregroundStyle(Color.accentColor)
                }
                if let quote = message.quote {
                    Text(quote)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                        .padding(.leading, DS.Spacing.xs)
                        .overlay(alignment: .leading) { Rectangle().fill(Color.accentColor).frame(width: 2) }
                }
                Text(message.text)
                    .font(.callout)
                    .foregroundStyle(.primary)
                    .fixedSize(horizontal: false, vertical: true)
                HStack(spacing: DS.Spacing.xxs) {
                    Text(roleLabel(message.role))
                    if let time = message.time { Text(verbatim: "· \(time)") }
                    if message.needsReview {
                        Text("screenshot.message.check").foregroundStyle(Color.orange)
                    }
                    if isTarget {
                        Text("screenshot.message.target").foregroundStyle(Color.accentColor).fontWeight(.semibold)
                    }
                }
                .font(.caption2)
                .foregroundStyle(.secondary)
            }
            .padding(DS.Spacing.s)
            .background(
                RoundedRectangle(cornerRadius: DS.Radius.large, style: .continuous)
                    .fill(mine ? Color.accentColor.opacity(0.14) : Color.dsSurface)
            )
            .overlay(
                RoundedRectangle(cornerRadius: DS.Radius.large, style: .continuous)
                    .stroke(isTarget ? Color.accentColor : (message.role == .unknown ? Color.orange.opacity(0.7) : .clear),
                            style: StrokeStyle(lineWidth: isTarget ? 2 : 1, dash: message.role == .unknown && !isTarget ? [4, 3] : []))
            )
            if !mine { Spacer(minLength: 40) }
        }
        .contentShape(Rectangle())
        .onTapGesture { model.select(message) }
        .contextMenu {
            Button { model.select(message) } label: { Label("screenshot.message.replyToThis", systemImage: "arrowshape.turn.up.left") }
            Button { model.setRole(.other, for: message) } label: { Label("screenshot.message.fromThem", systemImage: "person") }
            Button { model.setRole(.me, for: message) } label: { Label("screenshot.message.fromMe", systemImage: "person.fill") }
            Button { editing = message } label: { Label("screenshot.message.edit", systemImage: "pencil") }
            Button(role: .destructive) { model.remove(message) } label: { Label("screenshot.message.remove", systemImage: "trash") }
        }
        .accessibilityElement(children: .combine)
        .accessibilityHint(Text("screenshot.message.accessibilityHint"))
        .accessibilityAddTraits(isTarget ? .isSelected : [])
    }

    private func roleLabel(_ role: ScreenshotSenderRole) -> String {
        switch role {
        case .me:      return localized("screenshot.role.me")
        case .other:   return localized("screenshot.role.other")
        case .unknown: return localized("screenshot.role.unknown")
        }
    }

    private var instructionSection: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.s) {
            Text("screenshot.instruction.title")
                .font(.footnote.weight(.semibold))
                .foregroundStyle(.secondary)
                .textCase(.uppercase)
            HStack(alignment: .bottom, spacing: DS.Spacing.xs) {
                TextField("screenshot.instruction.placeholder", text: $model.instruction, axis: .vertical)
                    .lineLimit(1...5)
                    .focused($instructionFocused)
                    .padding(DS.Spacing.s)
                    .background(RoundedRectangle(cornerRadius: DS.Radius.medium, style: .continuous).fill(Color.dsSurface))
                    .onChange(of: model.instruction) { _, _ in model.instructionChanged() }
                    .accessibilityIdentifier("screenshot.instruction")
                Button {
                    instructionFocused = false
                    let recorder = ScreenshotInstructionRecorder(language: settings.effectiveLanguage)
                    self.recorder = recorder
                    recorder.start()
                } label: {
                    Image(systemName: "mic.fill")
                        .font(.title3)
                        .frame(width: DS.Layout.minimumTouchTarget, height: DS.Layout.minimumTouchTarget)
                        .background(Circle().fill(Color.accentColor.opacity(0.15)))
                }
                .accessibilityLabel(Text("screenshot.voice.start"))
                .accessibilityIdentifier("screenshot.voice")
                .disabled(recorder != nil)
            }
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: DS.Spacing.xs) {
                    ForEach(["screenshot.quick.agree", "screenshot.quick.refuse", "screenshot.quick.details", "screenshot.quick.thanks"],
                            id: \.self) { key in
                        Button(LocalizedStringKey(key)) {
                            model.instruction = ScreenshotInstructionRecorder.join(model.instruction, localized(key))
                        }
                        .buttonStyle(.bordered)
                        .buttonBorderShape(.capsule)
                        .font(.footnote)
                    }
                }
            }
            if let recorder {
                ScreenshotVoicePanel(recorder: recorder) { text in
                    if let text, !text.isEmpty {
                        model.instruction = ScreenshotInstructionRecorder.join(model.instruction, text)
                        model.instructionIsVoice = true
                    }
                    self.recorder = nil
                }
            }
        }
    }

    private var optionsSection: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.s) {
            LabeledContent {
                Picker(selection: $model.templateID) {
                    ForEach(configuration.templates.filter(\.isVisible)) { template in
                        Text(template.displayName(appLanguage: settings.effectiveLanguage)).tag(template.id)
                    }
                } label: {
                    Text("screenshot.option.persona")
                }
                .labelsHidden()
            } label: {
                Label("screenshot.option.persona", systemImage: "person.crop.circle")
            }
            .onChange(of: model.templateID) { _, _ in model.refreshEstimate() }
            Divider()
            LabeledContent {
                Picker(selection: $model.replyLanguage) {
                    Text("screenshot.option.language.auto").tag(ReplyLanguagePreference?.none)
                    ForEach(ReplyLanguagePreference.allCases) { language in
                        Text(verbatim: language.nativeName).tag(ReplyLanguagePreference?.some(language))
                    }
                } label: {
                    Text("screenshot.option.language")
                }
                .labelsHidden()
            } label: {
                Label("screenshot.option.language", systemImage: "globe")
            }
            .onChange(of: model.replyLanguage) { _, _ in model.refreshEstimate() }
            if ScreenshotFeature.imageAvailable {
                Divider()
                Toggle(isOn: imageMode) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("screenshot.option.image")
                        Text("screenshot.option.image.footer")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
                .accessibilityIdentifier("screenshot.imageMode")
            }
        }
        .pickerStyle(.menu)
        .dsCard()
    }

    private var imageMode: Binding<Bool> {
        Binding(get: { model.mode == .image }, set: { on in
            if on && !ScreenshotConsent.allowsImage {
                showsImageConsent = true
                return
            }
            model.mode = on ? .image : .text
            model.modeChanged()
        })
    }

    private var generateSection: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.s) {
            HStack(spacing: DS.Spacing.xs) {
                costLabel
                Spacer(minLength: 0)
                Button {
                    showsPricing.toggle()
                } label: {
                    Image(systemName: "info.circle")
                }
                .accessibilityLabel(Text("screenshot.cost.explain"))
            }
            if showsPricing {
                Text("screenshot.cost.policy")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if model.isGenerating {
                HStack(spacing: DS.Spacing.s) {
                    ProgressView()
                    Text("screenshot.generating").font(.callout)
                    Spacer()
                    Button("common.cancel") { model.cancelGeneration() }
                }
            } else {
                Button {
                    instructionFocused = false
                    model.generate()
                } label: {
                    Label(model.reply == nil ? "screenshot.generate" : "screenshot.regenerate",
                          systemImage: model.reply == nil ? "sparkles" : "arrow.clockwise")
                        .frame(maxWidth: .infinity)
                }
                .buttonStyle(.dsPrimary)
                .disabled(!model.canGenerate || recorder != nil)
                .accessibilityIdentifier("screenshot.generate")
                if model.mode == .text && model.target == nil && !model.conversation.messages.isEmpty {
                    Text("screenshot.noTarget")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
        }
        .dsCard()
    }

    @ViewBuilder
    private var costLabel: some View {
        switch model.cost {
        case .idle:
            EmptyView()
        case .loading:
            Text("screenshot.cost.loading").font(.footnote).foregroundStyle(.secondary)
        case .failed:
            Text("screenshot.cost.unavailable").font(.footnote).foregroundStyle(.secondary)
        case .ready(let quote):
            VStack(alignment: .leading, spacing: 2) {
                Text(costText(quote)).font(.footnote.weight(.semibold))
                if let available = quote.availableUnits {
                    Text(String(format: localized("screenshot.cost.balance %lld"), available))
                        .font(.caption)
                        .foregroundStyle(quote.exceedsBalance ? Color.orange : .secondary)
                }
            }
            .accessibilityElement(children: .combine)
            .accessibilityIdentifier("screenshot.cost")
        }
    }

    private func costText(_ quote: ScreenshotQuote) -> String {
        if quote.unlimited { return localized("screenshot.cost.unlimited") }
        if quote.estimatedUnits >= quote.maxUnits {
            return String(format: localized("screenshot.cost.exact %lld"), quote.maxUnits)
        }
        return String(format: localized("screenshot.cost.range %lld %lld"), quote.estimatedUnits, quote.maxUnits)
    }

    private func confirmationMessage(_ quote: ScreenshotQuote) -> String {
        var text = String(format: localized("screenshot.confirm.body %lld %lld"), quote.estimatedUnits, quote.maxUnits)
        if let available = quote.availableUnits {
            text += "\n" + String(format: localized("screenshot.confirm.balance %lld %lld"), available,
                                  max(0, available - quote.maxUnits))
        }
        return text
    }

    private var resultSection: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.s) {
            Text("screenshot.result.title")
                .font(.footnote.weight(.semibold))
                .foregroundStyle(.secondary)
                .textCase(.uppercase)
            TextField("", text: Binding(get: { model.reply ?? "" }, set: { model.reply = $0; model.replyEdited() }),
                      axis: .vertical)
                .lineLimit(2...10)
                .padding(DS.Spacing.s)
                .background(RoundedRectangle(cornerRadius: DS.Radius.medium, style: .continuous).fill(Color.dsSurface))
                .accessibilityIdentifier("screenshot.result")
            if let charge = model.lastCharge {
                Text(chargeText(charge))
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            HStack(spacing: DS.Spacing.s) {
                Button {
                    model.copyReply()
                } label: {
                    Label("screenshot.result.copy", systemImage: "doc.on.doc").frame(maxWidth: .infinity)
                }
                .buttonStyle(.dsSecondary)
                if let url = chatURL {
                    Button {
                        UIApplication.shared.open(url)
                    } label: {
                        Label(String(format: localized("screenshot.result.backTo %@"), model.conversation.appName ?? ""),
                              systemImage: "arrowshape.turn.up.left").frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.dsSecondary)
                }
            }
            Label("screenshot.result.keyboard", systemImage: "keyboard")
                .font(.footnote)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
        }
        .dsCard()
    }

    private func chargeText(_ charge: (units: Int, refunded: Int)) -> String {
        if charge.units == 0 { return localized("screenshot.result.free") }
        var text = String(format: localized("screenshot.result.charged %lld"), charge.units)
        if charge.refunded > 0 {
            text += " " + String(format: localized("screenshot.result.refunded %lld"), charge.refunded)
        }
        return text
    }

    /// The chat app the screenshot came from, when it was recognised and
    /// opens by its public link.
    private var chatURL: URL? {
        switch model.conversation.app {
        case "whatsapp":  return URL(string: "whatsapp://")
        case "telegram":  return URL(string: "tg://")
        case "instagram": return URL(string: "instagram://")
        default:          return nil
        }
    }

    // MARK: Helpers

    private var approvalShown: Binding<Bool> {
        Binding(get: { model.pendingApproval != nil }, set: { if !$0 { model.pendingApproval = nil } })
    }

    private var insufficientShown: Binding<Bool> {
        Binding(get: { model.insufficient != nil }, set: { if !$0 { model.insufficient = nil } })
    }

    private func localized(_ key: String) -> String {
        AppSettings.localized(key, language: settings.effectiveLanguage)
    }
}

/// Recording a spoken instruction: the 40-second countdown, pause, done and
/// cancel. The words appear as they are recognised.
struct ScreenshotVoicePanel: View {
    let recorder: ScreenshotInstructionRecorder
    let onFinish: (String?) -> Void

    @Environment(AppSettings.self) private var settings

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.s) {
            HStack(spacing: DS.Spacing.xs) {
                Circle()
                    .fill(recorder.isRecording ? Color.red : Color.secondary)
                    .frame(width: 10, height: 10)
                    .accessibilityHidden(true)
                Text(recorder.isRecording ? "screenshot.voice.recording" : (recorder.isPaused ? "screenshot.voice.paused" : "screenshot.voice.ready"))
                    .font(.footnote.weight(.semibold))
                Spacer()
                TimelineView(.periodic(from: .now, by: 0.5)) { _ in
                    Text(verbatim: Self.clock(recorder.remaining))
                        .font(.footnote.monospacedDigit())
                        .foregroundStyle(recorder.remaining < 10 ? Color.red : .secondary)
                        .accessibilityLabel(Text(String(format: AppSettings.localized("screenshot.voice.remaining %lld",
                                                                                      language: settings.effectiveLanguage),
                                                        Int(recorder.remaining.rounded(.down)))))
                }
            }
            Text(recorder.text.isEmpty ? recorder.statusMessage : recorder.text)
                .font(.callout)
                .foregroundStyle(recorder.text.isEmpty ? .secondary : .primary)
                .frame(maxWidth: .infinity, alignment: .leading)
                .fixedSize(horizontal: false, vertical: true)
            HStack(spacing: DS.Spacing.s) {
                if recorder.isRecording {
                    Button {
                        recorder.pause()
                    } label: {
                        Label("screenshot.voice.pause", systemImage: "pause.fill")
                    }
                    .buttonStyle(.bordered)
                } else if !recorder.isAtLimit {
                    Button {
                        recorder.start()
                    } label: {
                        Label("screenshot.voice.resume", systemImage: "mic.fill")
                    }
                    .buttonStyle(.bordered)
                }
                Button {
                    recorder.finish()
                    onFinish(recorder.text)
                } label: {
                    Label("screenshot.voice.done", systemImage: "checkmark")
                }
                .buttonStyle(.borderedProminent)
                Button(role: .cancel) {
                    recorder.cancel()
                    onFinish(nil)
                } label: {
                    Text("common.cancel")
                }
                .buttonStyle(.bordered)
            }
            .font(.footnote)
        }
        .padding(DS.Spacing.s)
        .background(RoundedRectangle(cornerRadius: DS.Radius.medium, style: .continuous).fill(Color.dsSurface))
        .accessibilityIdentifier("screenshot.voice.panel")
    }

    static func clock(_ seconds: TimeInterval) -> String {
        let value = max(0, Int(seconds.rounded(.up)))
        return String(format: "%d:%02d", value / 60, value % 60)
    }
}

/// Correcting the text of one recognised message.
struct ScreenshotMessageEditor: View {
    let message: ScreenshotMessage
    let onSave: (String) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var text = ""

    var body: some View {
        NavigationStack {
            TextEditor(text: $text)
                .padding(DS.Spacing.s)
                .navigationTitle("screenshot.message.edit")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) { Button("common.cancel") { dismiss() } }
                    ToolbarItem(placement: .confirmationAction) {
                        Button("common.save") {
                            onSave(text)
                            dismiss()
                        }
                    }
                }
        }
        .onAppear { text = message.text }
        .presentationDetents([.medium])
    }
}

/// The explicit "Allow" before a screenshot itself is sent for analysis.
struct ScreenshotImageConsentView: View {
    let onDecision: (Bool) -> Void
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: DS.Spacing.m) {
                    Image(systemName: "photo.badge.checkmark")
                        .font(.system(size: 36))
                        .foregroundStyle(Color.accentColor)
                        .accessibilityHidden(true)
                    Text("screenshot.imageConsent.title").font(.title3.weight(.semibold))
                    Text("screenshot.imageConsent.body")
                        .font(.callout)
                        .fixedSize(horizontal: false, vertical: true)
                    Text("screenshot.imageConsent.cost")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                    Button {
                        onDecision(true)
                        dismiss()
                    } label: {
                        Text("screenshot.imageConsent.allow").frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.dsPrimary)
                    .accessibilityIdentifier("screenshot.imageConsent.allow")
                    Button {
                        onDecision(false)
                        dismiss()
                    } label: {
                        Text("screenshot.imageConsent.decline").frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.dsSecondary)
                }
                .padding(DS.Spacing.m)
            }
            .interactiveDismissDisabled()
        }
    }
}
