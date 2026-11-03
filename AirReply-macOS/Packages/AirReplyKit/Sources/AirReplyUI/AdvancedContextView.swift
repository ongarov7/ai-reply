import SwiftUI
import AirReplyCore

/// Optional controls inside the existing floating bar. Selecting context never
/// generates a message, reads unrelated files or starts the microphone.
struct ContextToolbar: View {
    @Bindable var model: QuickBarModel

    var body: some View {
        HStack(spacing: 10) {
            Picker("bar.mode", selection: Binding(get: { model.isReplyMode }, set: { reply in
                reply ? model.selectReplyMode() : model.dismissCopied()
            })) {
                Text("bar.mode.message").tag(false)
                Text("bar.mode.reply").tag(true)
            }
            .pickerStyle(.segmented)
            .frame(width: 210)
            Menu {
                ForEach(DesktopTemplate.allCases, id: \.self) { template in
                    Button { model.selectTemplate(template) } label: {
                        if model.template == template {
                            Label(String(localized: template.labelKey), systemImage: "checkmark")
                        } else { Text(String(localized: template.labelKey)) }
                    }
                }
            } label: {
                Label(String(localized: model.template.labelKey), systemImage: "text.bubble")
                    .font(.system(size: 12.5, weight: .medium))
            }
            .menuStyle(.borderlessButton)
            .fixedSize()
            .accessibilityLabel(Text("bar.template"))
            Spacer(minLength: 0)
            Button { model.advancedExpanded.toggle() } label: {
                HStack(spacing: 5) {
                    if model.hasAdvancedContext {
                        Circle().fill(Color.accentColor).frame(width: 5, height: 5)
                    }
                    Text("bar.context.title")
                    Image(systemName: model.advancedExpanded ? "chevron.up" : "chevron.down")
                }
                .font(.system(size: 12, weight: .medium))
            }
            .buttonStyle(.plain)
            .foregroundStyle(model.advancedExpanded ? Color.accentColor : Color.secondary)
            .accessibilityValue(model.advancedExpanded ? Text("bar.context.expanded") : Text("bar.context.collapsed"))
            Button(action: model.openAccountSettings) {
                Image(systemName: "person.crop.circle")
                    .font(.system(size: 15))
            }
            .buttonStyle(.plain)
            .foregroundStyle(.secondary)
            .help(Text("settings.account"))
            .accessibilityLabel(Text("settings.account"))
        }
        .frame(height: 26)
        .padding(.horizontal, 14)
        .padding(.top, 10)
        .disabled(model.phase == .generating)
    }
}

struct AdvancedContextView: View {
    @Bindable var model: QuickBarModel

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Divider()
            if !model.desktopContextAvailable {
                Text("bar.context.server_update")
                    .font(.system(size: 12))
                    .foregroundStyle(.orange)
            }
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    profileSelector
                    if model.editingProfile != nil { profileEditor }
                    TextField(String(localized: "bar.context.instructions"), text: $model.customInstructions, axis: .vertical)
                        .textFieldStyle(.roundedBorder)
                        .lineLimit(2...3)
                    if model.customInstructions.count > 900 {
                        Text(verbatim: "\(model.customInstructions.count)/1000")
                            .font(.caption.monospacedDigit())
                            .foregroundStyle(model.customInstructions.count > 1000 ? Color.red : Color.secondary)
                    }
                    attachmentList
                    HStack {
                        Text("bar.context.output").font(.system(size: 12.5))
                        Spacer()
                        Picker("bar.context.output", selection: $model.outputLength) {
                            Text("bar.context.standard").tag(OutputLength.standard)
                            Text("bar.context.extended").tag(OutputLength.extended)
                        }
                        .labelsHidden()
                        .pickerStyle(.segmented)
                        .frame(width: 300)
                    }
                    .frame(height: 26)
                    .disabled(!model.desktopContextAvailable)
                    Text("bar.context.privacy")
                        .font(.system(size: 11.5))
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
                .padding(.trailing, 4)
            }
            .frame(maxHeight: 245)
            if let issue = model.contextIssue {
                Text(verbatim: issue)
                    .font(.system(size: 12))
                    .foregroundStyle(.orange)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .padding(.horizontal, 14)
        .padding(.bottom, 12)
        .disabled(model.phase == .generating)
    }

    private var profileSelector: some View {
        HStack(spacing: 8) {
            Picker("bar.context.profile", selection: $model.selectedProfileID) {
                Text("bar.context.none").tag(UUID?.none)
                ForEach(model.contextProfiles.profiles) { profile in
                    Text(verbatim: profile.name).tag(Optional(profile.id))
                }
            }
            .onChange(of: model.selectedProfileID) { model.editingProfile = nil }
            Button { model.beginProfileEdit() } label: {
                Image(systemName: model.selectedProfile == nil ? "plus" : "pencil")
            }
            .help(Text("bar.context.edit"))
            if model.selectedProfile != nil {
                Button { model.selectedProfileID = nil; model.beginProfileEdit() } label: { Image(systemName: "plus") }
                    .help(Text("bar.context.new"))
            }
            Button("bar.context.clear", action: model.clearContext).buttonStyle(.link)
            if model.contextStorageBusy { ProgressView().controlSize(.small) }
        }
        .controlSize(.small)
        .disabled(model.contextStorageBusy)
    }

    @ViewBuilder
    private var profileEditor: some View {
        if let draft = Binding($model.editingProfile) {
            VStack(alignment: .leading, spacing: 8) {
                TextField(String(localized: "bar.context.profile_name"), text: draft.name)
                HStack {
                    TextField(String(localized: "bar.context.person"), text: draft.personName)
                    TextField(String(localized: "bar.context.company"), text: draft.company)
                }
                TextField(String(localized: "bar.context.occupation"), text: draft.occupation)
                HStack {
                    Picker("bar.context.purpose", selection: draft.role) {
                        Text("bar.context.personal").tag(ContextProfileRole.personal)
                        Text("bar.context.business").tag(ContextProfileRole.business)
                    }
                    TextField(String(localized: "bar.context.industry"), text: draft.industry)
                }
                TextField(String(localized: "bar.context.description"), text: draft.description, axis: .vertical).lineLimit(2...3)
                TextField(String(localized: "bar.context.style"), text: draft.communicationStyle)
                TextField(String(localized: "bar.context.products"), text: draft.products, axis: .vertical).lineLimit(1...2)
                TextField(String(localized: "bar.context.services"), text: draft.services, axis: .vertical).lineLimit(1...2)
                TextField(String(localized: "bar.context.policies"), text: draft.policies, axis: .vertical).lineLimit(1...2)
                TextField(String(localized: "bar.context.profile_instructions"), text: draft.instructions, axis: .vertical).lineLimit(2...3)
                HStack {
                    if model.selectedProfile != nil {
                        Button("bar.context.delete", role: .destructive, action: model.removeSelectedProfile)
                    }
                    Spacer()
                    Button("bar.context.cancel") { model.editingProfile = nil }
                    Button("bar.context.save", action: model.saveProfile)
                        .buttonStyle(.borderedProminent)
                        .disabled(draft.wrappedValue.name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                }
            }
            .textFieldStyle(.roundedBorder)
            .controlSize(.small)
            .disabled(model.contextStorageBusy)
            .padding(10)
            .background(RoundedRectangle(cornerRadius: 9).fill(Color.primary.opacity(0.035)))
        }
    }

    private var attachmentList: some View {
        VStack(alignment: .leading, spacing: 7) {
            HStack {
                Button(action: model.chooseDocument) { Label("bar.context.attach", systemImage: "paperclip") }
                    .controlSize(.small)
                    .disabled(model.importingDocument || model.contextStorageBusy || model.attachments.count >= 4)
                if model.importingDocument {
                    ProgressView().controlSize(.small)
                    Text("bar.context.importing").font(.caption).foregroundStyle(.secondary)
                } else {
                    Text("bar.context.formats").font(.caption).foregroundStyle(.secondary)
                }
                Spacer()
            }
            ForEach(model.attachments) { attachment in
                HStack(spacing: 7) {
                    Image(systemName: "doc.text").foregroundStyle(.secondary)
                    Text(verbatim: attachment.name).lineLimit(1).truncationMode(.middle)
                    Spacer()
                    Button { model.removeAttachment(attachment.id) } label: { Image(systemName: "xmark.circle.fill") }
                        .buttonStyle(.plain)
                        .foregroundStyle(.secondary)
                        .help(Text("bar.context.remove_attachment"))
                }
                .font(.system(size: 12))
            }
        }
    }
}
