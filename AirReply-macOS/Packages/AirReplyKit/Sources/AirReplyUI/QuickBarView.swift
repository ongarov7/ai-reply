import AppKit
import SwiftUI
import AirReplyCore
import AirReplyVoice

/// The quick bar: one input row, an optional copied message, and a card for the
/// reply, a problem or the consent step.
public struct QuickBarView: View {
    @Bindable var model: QuickBarModel
    let pricingURL: URL
    let legal: () -> ServerConfig.Legal?
    /// Reports the height the content needs, so the panel can follow it.
    var onHeightChange: (CGFloat) -> Void

    public init(model: QuickBarModel, pricingURL: URL, legal: @escaping () -> ServerConfig.Legal?,
                onHeightChange: @escaping (CGFloat) -> Void) {
        self.model = model
        self.pricingURL = pricingURL
        self.legal = legal
        self.onHeightChange = onHeightChange
    }

    public var body: some View {
        VStack(spacing: BarMetrics.gap) {
            if model.growsUpward {
                secondaryCard
                bar
            } else {
                bar
                secondaryCard
            }
        }
        .frame(width: BarMetrics.width)
        .padding(BarMetrics.shadowInset)
        .fixedSize(horizontal: false, vertical: true)
        .background(
            GeometryReader { proxy in
                Color.clear.preference(key: HeightKey.self, value: proxy.size.height)
            }
        )
        .onPreferenceChange(HeightKey.self) { onHeightChange($0) }
        .animation(.snappy(duration: 0.22), value: model.phase)
        .animation(.snappy(duration: 0.22), value: model.issue)
        .animation(.snappy(duration: 0.22), value: model.mode)
    }

    private var bar: some View {
        BarCard {
            VStack(alignment: .leading, spacing: 0) {
                if !model.auth.isSignedIn {
                    SignInRow(model: model)
                } else {
                    if model.copiedText != nil, model.voice.phase == .idle {
                        CopiedRow(model: model)
                    }
                    switch model.voice.phase {
                    case .recording, .requestingPermission:
                        RecordingRow(voice: model.voice, onCancel: model.cancelVoice, onStop: model.toggleVoice)
                    case .transcribing:
                        TranscribingRow(onCancel: model.cancelVoice)
                    default:
                        InputRow(model: model)
                    }
                }
            }
        }
    }

    @ViewBuilder
    private var secondaryCard: some View {
        if model.auth.isSignedIn && model.needsConsent {
            BarCard { ConsentCard(model: model, legal: legal()) }
                .transition(.opacity.combined(with: .move(edge: model.growsUpward ? .bottom : .top)))
        } else if let issue = model.issue {
            BarCard { IssueCard(issue: issue, model: model, pricingURL: pricingURL) }
                .transition(.opacity)
        } else if model.phase == .result {
            BarCard { ResultCard(model: model) }
                .transition(.opacity.combined(with: .move(edge: model.growsUpward ? .bottom : .top)))
        }
    }
}

private struct HeightKey: PreferenceKey {
    static let defaultValue: CGFloat = 0
    static func reduce(value: inout CGFloat, nextValue: () -> CGFloat) { value = max(value, nextValue()) }
}

// MARK: - Input

private struct BrandIcon: View {
    var body: some View {
        Image(nsImage: BrandGlyph.image)
            .renderingMode(.template)
            .resizable()
            .foregroundStyle(Color.accentColor)
            .frame(width: 22, height: 22)
            .accessibilityHidden(true)
    }
}

private struct InputRow: View {
    @Bindable var model: QuickBarModel
    @FocusState private var focused: Bool

    var body: some View {
        HStack(alignment: .center, spacing: 10) {
            BrandIcon()
            TextField(model.copiedText == nil ? String(localized: "bar.placeholder.compose")
                                              : String(localized: "bar.placeholder.reply"),
                      text: $model.draft, axis: .vertical)
                .textFieldStyle(.plain)
                .font(.system(size: 17))
                .lineLimit(1...5)
                .focused($focused)
                .onSubmit(model.submit)
                .disabled(model.phase == .generating)
                .accessibilityLabel(Text("bar.field.accessibility"))
            if model.copiedText == nil, model.draftLength > model.instructionLimit * 3 / 4 {
                CharacterCounter(count: model.draftLength, limit: model.instructionLimit)
            }
            if model.voiceAvailable {
                IconButton(symbol: "mic", label: "bar.mic", action: model.toggleVoice)
                    .keyboardShortcut("d", modifiers: .command)
                    .disabled(model.phase == .generating)
            }
            SendButton(isWorking: model.phase == .generating, isEnabled: model.canSubmit, action: model.submit)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 14)
        .onAppear { focused = true }
        .onChange(of: model.focusRequest) { focused = true }
    }
}

private struct CharacterCounter: View {
    let count: Int
    let limit: Int

    var body: some View {
        Text(verbatim: "\(count)/\(limit)")
            .font(.system(size: 11, weight: .medium).monospacedDigit())
            .foregroundStyle(count > limit ? Color.red : Color.secondary)
            .accessibilityLabel(Text("bar.counter \(count) \(limit)"))
    }
}

struct IconButton: View {
    let symbol: String
    let label: LocalizedStringKey
    let action: () -> Void
    @State private var hovering = false

    var body: some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(.system(size: 15, weight: .medium))
                .foregroundStyle(.secondary)
                .frame(width: 30, height: 30)
                .background(Circle().fill(Color.primary.opacity(hovering ? 0.08 : 0)))
                .contentShape(Circle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .help(Text(label))
        .accessibilityLabel(Text(label))
    }
}

private struct SendButton: View {
    let isWorking: Bool
    let isEnabled: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            ZStack {
                RoundedRectangle(cornerRadius: 8, style: .continuous)
                    .fill(Color.accentColor.opacity(isEnabled || isWorking ? 1 : 0.35))
                if isWorking {
                    ProgressView().controlSize(.small).tint(.white)
                } else {
                    Image(systemName: "arrow.up").font(.system(size: 14, weight: .semibold)).foregroundStyle(.white)
                }
            }
            .frame(width: 32, height: 32)
        }
        .buttonStyle(.plain)
        .disabled(!isEnabled)
        .help(Text("bar.send.help"))
        .accessibilityLabel(Text("bar.send"))
    }
}

// MARK: - Copied message

private struct CopiedRow: View {
    @Bindable var model: QuickBarModel

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 6) {
                Image(systemName: "arrowshape.turn.up.left").font(.system(size: 11, weight: .semibold))
                Text(verbatim: preview).lineLimit(1).truncationMode(.tail)
                Spacer(minLength: 4)
                Button(action: model.dismissCopied) {
                    Image(systemName: "xmark.circle.fill").font(.system(size: 13))
                }
                .buttonStyle(.plain)
                .help(Text("copied.dismiss"))
                .accessibilityLabel(Text("copied.dismiss"))
            }
            .font(.system(size: 12.5))
            .foregroundStyle(.secondary)
            .padding(.horizontal, 10)
            .padding(.vertical, 6)
            .background(Capsule().fill(Color.primary.opacity(0.06)))
            .accessibilityElement(children: .combine)
            .accessibilityLabel(Text("copied.accessibility \(preview)"))

            if model.sourceTooLong {
                Text("copied.too_long \(model.sourceLength) \(model.sourceLimit)")
                    .font(.system(size: 12))
                    .foregroundStyle(.orange)
            } else {
                HStack(spacing: 6) {
                    ForEach(Array(Recipient.allCases.enumerated()), id: \.element) { index, recipient in
                        RecipientChip(recipient: recipient, isLast: recipient == model.recipient,
                                      isEnabled: model.canSubmit) { model.reply(to: recipient) }
                            .keyboardShortcut(KeyEquivalent(Character("\(index + 1)")), modifiers: .command)
                    }
                }
            }
        }
        .padding(.horizontal, 14)
        .padding(.top, 12)
    }

    private var preview: String {
        let text = model.copiedText?.text ?? ""
        let firstLine = text.split(whereSeparator: \.isNewline).first.map(String.init) ?? text
        return "«" + firstLine.trimmingCharacters(in: .whitespaces) + "»"
    }
}

private struct RecipientChip: View {
    let recipient: Recipient
    let isLast: Bool
    let isEnabled: Bool
    let action: () -> Void
    @State private var hovering = false

    var body: some View {
        Button(action: action) {
            HStack(spacing: 5) {
                Image(systemName: recipient.symbol).font(.system(size: 11))
                Text(String(localized: recipient.labelKey))
            }
            .font(.system(size: 12.5, weight: .medium))
            .padding(.horizontal, 11)
            .padding(.vertical, 5)
            .foregroundStyle(isLast ? Color.accentColor : Color.primary)
            .background(Capsule().fill(isLast ? Color.accentColor.opacity(0.12) : Color.primary.opacity(hovering ? 0.10 : 0.05)))
        }
        .buttonStyle(.plain)
        .disabled(!isEnabled)
        .onHover { hovering = $0 }
        .help(Text("recipient.help"))
    }
}

// MARK: - Voice

private struct RecordingRow: View {
    let voice: VoiceController
    let onCancel: () -> Void
    let onStop: () -> Void
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        HStack(spacing: 12) {
            Circle()
                .fill(Color.red)
                .frame(width: 8, height: 8)
                .opacity(voice.phase == .recording ? 1 : 0.4)
            Waveform(levels: voice.levels, animated: !reduceMotion)
                .frame(height: 28)
                .accessibilityHidden(true)
            Text(verbatim: "\(Self.clock(voice.elapsed)) / \(Self.clock(voice.maxDuration))")
                .font(.system(size: 12, weight: .medium).monospacedDigit())
                .foregroundStyle(.secondary)
            IconButton(symbol: "xmark", label: "voice.cancel", action: onCancel)
            Button(action: onStop) {
                Image(systemName: "checkmark")
                    .font(.system(size: 13, weight: .bold))
                    .foregroundStyle(.white)
                    .frame(width: 32, height: 32)
                    .background(Circle().fill(Color.accentColor))
            }
            .buttonStyle(.plain)
            .keyboardShortcut(.return, modifiers: [])
            .help(Text("voice.stop"))
            .accessibilityLabel(Text("voice.stop"))
            .disabled(voice.phase != .recording)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
        .accessibilityElement(children: .contain)
        .accessibilityLabel(Text("voice.recording"))
    }

    static func clock(_ interval: TimeInterval) -> String {
        let seconds = max(0, Int(interval.rounded(.down)))
        return String(format: "%d:%02d", seconds / 60, seconds % 60)
    }
}

private struct Waveform: View {
    let levels: [Float]
    let animated: Bool

    var body: some View {
        GeometryReader { proxy in
            let count = max(levels.count, 1)
            let spacing: CGFloat = 3
            let barWidth = max(2, (proxy.size.width - spacing * CGFloat(count - 1)) / CGFloat(count))
            HStack(alignment: .center, spacing: spacing) {
                ForEach(Array(levels.enumerated()), id: \.offset) { _, level in
                    Capsule()
                        .fill(Color.accentColor)
                        .frame(width: barWidth, height: max(3, CGFloat(level) * proxy.size.height))
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .animation(animated ? .linear(duration: 0.08) : nil, value: levels)
        }
    }
}

private struct TranscribingRow: View {
    let onCancel: () -> Void
    @State private var shimmer = false

    var body: some View {
        HStack(spacing: 10) {
            ProgressView().controlSize(.small)
            Text("voice.transcribing")
                .font(.system(size: 15))
                .foregroundStyle(.secondary)
            Spacer()
            IconButton(symbol: "xmark", label: "voice.cancel", action: onCancel)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 14)
    }
}

// MARK: - Result

private struct ResultCard: View {
    @Bindable var model: QuickBarModel

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            ScrollView {
                Text(verbatim: model.result)
                    .font(.system(size: 15))
                    .lineSpacing(3)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            .frame(maxHeight: 260)
            .fixedSize(horizontal: false, vertical: true)

            HStack(spacing: 8) {
                BalanceCaption(balance: model.account.balance)
                Spacer()
                Button(action: model.regenerate) {
                    Label("result.regenerate", systemImage: "arrow.clockwise")
                }
                .controlSize(.large)
                .keyboardShortcut("r", modifiers: .command)
                .disabled(model.phase == .generating)
                Button(action: model.copyResult) {
                    Label(model.justCopied ? "result.copied" : "result.copy",
                          systemImage: model.justCopied ? "checkmark" : "doc.on.doc")
                        .frame(minWidth: 110)
                }
                .controlSize(.large)
                .buttonStyle(.borderedProminent)
                .keyboardShortcut(.return, modifiers: .command)
                .help(Text("result.copy.help"))
            }
        }
        .padding(16)
    }
}

struct BalanceCaption: View {
    let balance: BalanceSummary?

    var body: some View {
        Group {
            if let balance {
                if balance.isUnlimited {
                    Text("balance.unlimited \(balance.planName)")
                } else if balance.remainingTotal <= 0 {
                    Text("balance.empty")
                } else {
                    Text("balance.remaining \(balance.remainingTotal)")
                }
            }
        }
        .font(.system(size: 12))
        .foregroundStyle(.secondary)
    }
}

// MARK: - Problems

private struct IssueCard: View {
    let issue: QuickBarIssue
    @Bindable var model: QuickBarModel
    let pricingURL: URL

    var body: some View {
        HStack(alignment: .center, spacing: 12) {
            Image(systemName: symbol)
                .font(.system(size: 15, weight: .medium))
                .foregroundStyle(tint)
            Text(message)
                .font(.system(size: 13.5))
                .fixedSize(horizontal: false, vertical: true)
            Spacer(minLength: 8)
            action
        }
        .padding(14)
    }

    private var symbol: String {
        switch issue {
        case .generation(.limitReached): "hourglass"
        case .generation(.offline), .voice(.offline): "wifi.slash"
        case .voice(.permissionDenied), .voice(.permissionRestricted): "mic.slash"
        default: "exclamationmark.circle"
        }
    }

    private var tint: Color {
        if case .generation(.limitReached) = issue { return .accentColor }
        return .orange
    }

    private var message: String {
        switch issue {
        case let .generation(error):
            switch error {
            case let .limitReached(resetsAt):
                if let resetsAt, model.account.balance?.dailyLimit ?? 0 > 0 {
                    return String(localized: "issue.limit.daily \(resetsAt.formatted(date: .omitted, time: .shortened))")
                }
                return String(localized: "issue.limit.empty")
            case let .sourceTooLong(limit): return String(localized: "issue.source_too_long \(limit)")
            case let .instructionTooLong(limit): return String(localized: "issue.instruction_too_long \(limit)")
            case .instructionMissing: return String(localized: "issue.instruction_missing")
            case .consentRequired: return String(localized: "issue.consent")
            case .accountDisabled: return String(localized: "issue.account_disabled")
            case .rateLimited: return String(localized: "issue.rate_limited")
            case .aiUnavailable: return String(localized: "issue.ai_unavailable")
            case .offline: return String(localized: "issue.offline")
            case .signedOut: return String(localized: "issue.signed_out")
            case .server: return String(localized: "issue.server")
            }
        case let .voice(error):
            switch error {
            case .permissionDenied, .permissionRestricted: return String(localized: "voice.issue.permission")
            case .noInputDevice: return String(localized: "voice.issue.no_device")
            case .tooShort: return String(localized: "voice.issue.too_short")
            case .unavailable: return String(localized: "voice.issue.unavailable")
            case .limitReached: return String(localized: "voice.issue.limit")
            case .rateLimited: return String(localized: "issue.rate_limited")
            case .offline: return String(localized: "issue.offline")
            case .recordingFailed: return String(localized: "voice.issue.recording_failed")
            case .server: return String(localized: "voice.issue.server")
            }
        case .nothingHeard: return String(localized: "voice.issue.nothing_heard")
        case .consentFailed: return String(localized: "issue.server")
        }
    }

    @ViewBuilder
    private var action: some View {
        switch issue {
        case .generation(.limitReached):
            Button("issue.action.plans") { model.openPricing(pricingURL) }
                .buttonStyle(.borderedProminent)
        case .generation(.offline), .generation(.aiUnavailable), .generation(.server), .generation(.rateLimited):
            Button("issue.action.retry", action: model.retry)
        case .voice(.permissionDenied), .voice(.permissionRestricted):
            Button("issue.action.settings", action: model.openPrivacySettings)
        default:
            Button("issue.action.ok", action: model.clearIssue)
        }
    }
}

// MARK: - Consent

private struct ConsentCard: View {
    @Bindable var model: QuickBarModel
    let legal: ServerConfig.Legal?

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("consent.title").font(.system(size: 14, weight: .semibold))
            Text("consent.body \(legal?.aiProvider ?? "OpenAI")")
                .font(.system(size: 13))
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
            HStack(spacing: 14) {
                if let privacy = legal?.privacyURL {
                    Link("consent.privacy", destination: privacy).font(.system(size: 12.5))
                }
                if let terms = legal?.termsURL {
                    Link("consent.terms", destination: terms).font(.system(size: 12.5))
                }
                Spacer()
                Button(action: model.acceptConsent) {
                    if model.account.isConsentSaving {
                        ProgressView().controlSize(.small)
                    } else {
                        Text("consent.accept")
                    }
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.large)
                .disabled(model.account.isConsentSaving)
            }
        }
        .padding(16)
    }
}

// MARK: - Sign-in

private struct SignInRow: View {
    @Bindable var model: QuickBarModel
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 10) {
                BrandIcon()
                switch model.auth.state {
                case let .awaitingCode(challenge):
                    TextField(String(localized: "signin.code.placeholder \(challenge.maskedEmail)"), text: $model.codeInput)
                        .textFieldStyle(.plain)
                        .font(.system(size: 17).monospacedDigit())
                        .focused($focused)
                        .onSubmit(model.submitCode)
                        .onChange(of: model.codeInput) { _, value in
                            let digits = String(value.filter(\.isNumber).prefix(challenge.codeLength))
                            if digits != value { model.codeInput = digits }
                            if digits.count == challenge.codeLength { model.submitCode() }
                        }
                default:
                    TextField(String(localized: "signin.email.placeholder"), text: $model.emailInput)
                        .textFieldStyle(.plain)
                        .font(.system(size: 17))
                        .textContentType(.emailAddress)
                        .focused($focused)
                        .onSubmit(model.submitEmail)
                }
                if model.auth.isWorking {
                    ProgressView().controlSize(.small)
                }
            }
            footer
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 14)
        .onAppear { focused = true }
        .onChange(of: model.focusRequest) { focused = true }
        .onChange(of: model.auth.state) { focused = true }
    }

    @ViewBuilder
    private var footer: some View {
        HStack(spacing: 12) {
            if let error = model.auth.lastError {
                Text(Self.message(error)).foregroundStyle(.orange)
            } else if case .awaitingCode = model.auth.state {
                Text("signin.code.hint").foregroundStyle(.secondary)
            } else {
                Text("signin.hint").foregroundStyle(.secondary)
            }
            Spacer()
            if case let .awaitingCode(challenge) = model.auth.state {
                TimelineView(.periodic(from: .now, by: 1)) { context in
                    let wait = Int(challenge.resendAvailableAt.timeIntervalSince(context.date).rounded(.up))
                    Button(wait > 0 ? String(localized: "signin.resend.wait \(wait)") : String(localized: "signin.resend"),
                           action: model.resendCode)
                        .buttonStyle(.link)
                        .disabled(wait > 0 || model.auth.isWorking)
                }
                Button("signin.another_email", action: model.useAnotherEmail).buttonStyle(.link)
            }
        }
        .font(.system(size: 12))
        .padding(.leading, 34)
    }

    static func message(_ error: AuthError) -> String {
        switch error {
        case .invalidEmail: String(localized: "signin.error.email")
        case .invalidCode: String(localized: "signin.error.code")
        case .codeExpired: String(localized: "signin.error.expired")
        case .codeAlreadyUsed: String(localized: "signin.error.used")
        case .tooManyAttempts: String(localized: "signin.error.attempts")
        case let .cooldown(seconds): String(localized: "signin.error.cooldown \(seconds)")
        case .rateLimited: String(localized: "issue.rate_limited")
        case .emailDelivery: String(localized: "signin.error.delivery")
        case .accountDisabled: String(localized: "issue.account_disabled")
        case .offline: String(localized: "issue.offline")
        case .server: String(localized: "issue.server")
        }
    }
}
