import SwiftUI

/// «Чат с оператором»: messages as in Messages, the operator always named
/// «Поддержка AI Reply», never said to be online.
///
/// «Чат с оператором»: хабарламалар, уақыты, жіберу күйі.
///
/// Reached from Settings, `aireply://support` and the `support_reply` push.
/// It reads what arrived every few seconds only while it is on screen and
/// the app is in front, and marks operator replies read as they are shown.
struct SupportChatView: View {

    @Environment(SupportInbox.self) private var inbox
    @State private var model: SupportChatModel?

    var body: some View {
        Group {
            if let model {
                SupportChatContent(model: model)
            } else {
                Color(.systemBackground)
            }
        }
        .navigationTitle("support.title")
        .navigationBarTitleDisplayMode(.inline)
        .onAppear {
            if model == nil { model = SupportChatModel(transport: inbox.transport, inbox: inbox) }
        }
    }
}

private struct SupportChatContent: View {

    @Bindable var model: SupportChatModel

    @Environment(SupportInbox.self) private var inbox
    @Environment(AppSettings.self) private var settings
    @Environment(\.scenePhase) private var scenePhase
    @FocusState private var isComposing: Bool

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(spacing: DS.Spacing.xxs) {
                    content
                }
                .padding(.horizontal, DS.Spacing.m)
                .padding(.vertical, DS.Spacing.s)
                .frame(maxWidth: DS.Layout.readableWidth)
                .frame(maxWidth: .infinity)
            }
            .defaultScrollAnchor(.bottom)
            .scrollDismissesKeyboard(.interactively)
            .overlay {
                if model.isEmpty { emptyState }
            }
            .onChange(of: model.items.last?.id) { _, last in
                guard let last else { return }
                withAnimation(.easeOut(duration: 0.2)) { proxy.scrollTo(last, anchor: .bottom) }
            }
            .onChange(of: scrollTarget) { _, target in
                guard let target else { return }
                proxy.scrollTo(target, anchor: .top)
                scrollTarget = nil
            }
        }
        .background(Color(.systemBackground))
        .safeAreaInset(edge: .bottom) { composer }
        .task {
            model.isActive = scenePhase == .active
            await model.load()
            await model.pollWhileVisible()
        }
        .onAppear {
            inbox.isChatVisible = true
            inbox.onReplyWhileVisible = { Task { await model.poll() } }
        }
        .onDisappear {
            inbox.isChatVisible = false
            inbox.onReplyWhileVisible = nil
        }
        .onChange(of: scenePhase) { _, phase in
            model.isActive = phase == .active
            if phase == .active { Task { await model.poll() } }
        }
    }

    // MARK: Messages

    @ViewBuilder
    private var content: some View {
        switch model.loadState {
        case .idle, .loading:
            ProgressView()
                .frame(maxWidth: .infinity)
                .padding(.top, DS.Spacing.xl)
        case .failed:
            VStack(spacing: DS.Spacing.s) {
                Text("support.error.load").foregroundStyle(.secondary)
                Button("common.retry") { Task { await model.load() } }
            }
            .frame(maxWidth: .infinity)
            .padding(.top, DS.Spacing.xl)
        case .loaded:
            // An empty chat says hello in the middle of the screen (the
            // overlay below), not at the bottom where messages keep.
            if !model.items.isEmpty {
                if model.hasOlder {
                    ProgressView()
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, DS.Spacing.xs)
                        .onAppear {
                            let first = model.items.first?.id
                            Task {
                                await model.loadOlder()
                                // Stay on the message that was on top.
                                if let first { scrollTarget = first }
                            }
                        }
                }
                ForEach(Array(model.items.enumerated()), id: \.element.id) { index, item in
                    let previous = index > 0 ? model.items[index - 1] : nil
                    if previous.map({ !Calendar.current.isDate($0.date, inSameDayAs: item.date) }) ?? true {
                        dayHeader(item.date)
                    }
                    if !item.isFromUser, previous?.isFromUser != false {
                        Text("support.operator")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(.leading, DS.Spacing.s)
                            .padding(.top, DS.Spacing.xs)
                    }
                    SupportBubble(item: item, timeFormat: timeFormat) {
                        Task { await model.retry(item.id) }
                    }
                    .id(item.id)
                }
            }
        }
    }

    /// Scrolls back to the message that was on top before older ones came.
    @State private var scrollTarget: String?

    private var emptyState: some View {
        VStack(spacing: DS.Spacing.s) {
            Image(systemName: "bubble.left.and.text.bubble.right")
                .font(.system(size: 36))
                .foregroundStyle(Color.accentColor)
                .accessibilityHidden(true)
            Text("support.operator")
                .font(.headline)
            Text("support.empty")
                .font(.body)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(DS.Spacing.l)
        .frame(maxWidth: DS.Layout.readableWidth)
    }

    private func dayHeader(_ date: Date) -> some View {
        Group {
            if Calendar.current.isDateInToday(date) {
                Text("support.today")
            } else {
                Text(date, format: .dateTime.day().month(.wide).locale(settings.locale))
            }
        }
        .font(.caption.weight(.medium))
        .foregroundStyle(.secondary)
        .frame(maxWidth: .infinity)
        .padding(.vertical, DS.Spacing.xs)
    }

    private var timeFormat: Date.FormatStyle {
        Date.FormatStyle.dateTime.hour().minute().locale(settings.locale)
    }

    // MARK: Composer

    private var composer: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.xxs) {
            if let key = model.sendErrorKey {
                Text(LocalizedStringKey(key))
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            HStack(alignment: .bottom, spacing: DS.Spacing.xs) {
                TextField("support.placeholder", text: $model.draft, axis: .vertical)
                    .lineLimit(1...5)
                    .focused($isComposing)
                    .padding(.horizontal, DS.Spacing.s)
                    .padding(.vertical, DS.Spacing.xs)
                    .background(
                        RoundedRectangle(cornerRadius: 18, style: .continuous)
                            .fill(Color(.secondarySystemBackground))
                    )
                Button {
                    Task { await model.send() }
                } label: {
                    Image(systemName: "arrow.up.circle.fill")
                        .font(.system(size: 32))
                        .foregroundStyle(model.canSend ? Color.accentColor : Color.secondary)
                }
                .disabled(!model.canSend)
                .accessibilityLabel(Text("support.send"))
                .frame(minWidth: DS.Layout.minimumTouchTarget, minHeight: DS.Layout.minimumTouchTarget)
            }
            if model.trimmedDraft.count > SupportChatModel.maximumLength - 200 {
                Text(verbatim: "\(model.trimmedDraft.count) / \(SupportChatModel.maximumLength)")
                    .font(.caption.monospacedDigit())
                    .foregroundStyle(model.trimmedDraft.count > SupportChatModel.maximumLength ? Color.red : .secondary)
            }
        }
        .padding(.horizontal, DS.Spacing.m)
        .padding(.vertical, DS.Spacing.xs)
        .frame(maxWidth: DS.Layout.readableWidth)
        .frame(maxWidth: .infinity)
        .background(.bar)
    }
}

/// One message: the user's on the right in the accent colour, the
/// operator's on the left in grey, the time under it, and for an unsent one
/// its state.
private struct SupportBubble: View {

    let item: SupportChatModel.Item
    let timeFormat: Date.FormatStyle
    var onRetry: () -> Void

    var body: some View {
        HStack {
            if item.isFromUser { Spacer(minLength: 48) }
            VStack(alignment: item.isFromUser ? .trailing : .leading, spacing: 2) {
                Text(verbatim: item.text)
                    .font(.body)
                    .foregroundStyle(item.isFromUser ? Color.white : Color.primary)
                    .textSelection(.enabled)
                    .padding(.horizontal, DS.Spacing.s)
                    .padding(.vertical, DS.Spacing.xs)
                    .background(
                        RoundedRectangle(cornerRadius: 18, style: .continuous)
                            .fill(item.isFromUser ? Color.accentColor : Color(.secondarySystemBackground))
                    )
                    .opacity(item.delivery == .sending ? 0.6 : 1)
                footer
                    .padding(.horizontal, DS.Spacing.xxs)
            }
            if !item.isFromUser { Spacer(minLength: 48) }
        }
        .padding(.vertical, 2)
    }

    @ViewBuilder
    private var footer: some View {
        switch item.delivery {
        case .sent:
            Text(item.date, format: timeFormat)
                .font(.caption2)
                .foregroundStyle(.secondary)
        case .sending:
            Text("support.sending")
                .font(.caption2)
                .foregroundStyle(.secondary)
        case .failed:
            Button(action: onRetry) {
                Label("support.failed", systemImage: "exclamationmark.circle")
                    .font(.caption)
            }
            .buttonStyle(.borderless)
            .foregroundStyle(Color.red)
        }
    }
}
