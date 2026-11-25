import AVFAudio
import SwiftUI

/// «Твой личный ассистент в переписке»: what AI Reply does in one example
/// and three steps, and the two ways in.
///
/// Сәлемдесу экраны: бір мысал, үш қадам және екі жол.
struct OnboardingWelcomeStep: View {

    let samples: OnboardingSamples
    /// Reopened from Settings: «Закрыть» instead of «Позже».
    let isTutorial: Bool
    let onSetup: () -> Void
    let onTry: () -> Void
    let onLater: () -> Void

    @Environment(\.dynamicTypeSize) private var typeSize

    var body: some View {
        OnboardingPage {
            VStack(alignment: .leading, spacing: DS.Spacing.l) {
                topBar

                VStack(alignment: .leading, spacing: DS.Spacing.s) {
                    Text("onboarding.welcome.title")
                        .font(.largeTitle.weight(.bold))
                        .accessibilityAddTraits(.isHeader)
                    Text("onboarding.welcome.subtitle")
                        .font(.body)
                        .foregroundStyle(.secondary)
                }

                if !isTutorial {
                    OnboardingMicrophoneSetupCard()
                }

                example
                flow
            }
        } buttons: {
            Button("onboarding.welcome.setup", action: onSetup)
                .buttonStyle(.dsPrimary)
            Button("onboarding.welcome.tryFirst", action: onTry)
                .buttonStyle(.dsText)
        }
        .toolbar(.hidden, for: .navigationBar)
    }

    private var topBar: some View {
        HStack(spacing: DS.Spacing.xs) {
            AppMarkView(size: 30)
            Text(verbatim: "AI Reply")
                .font(.headline)
            Spacer(minLength: DS.Spacing.s)
            Button(isTutorial ? "common.close" : "onboarding.later", action: onLater)
                .font(.body)
                .frame(minHeight: DS.Layout.minimumTouchTarget)
        }
    }

    /// «Например»: a message from a friend and the reply AI Reply offers.
    private var example: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.s) {
            Text("onboarding.welcome.example")
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(.secondary)
            OnboardingBubble(text: samples.welcomeIncoming, side: .incoming)
            Text("onboarding.replyOption")
                .font(.caption)
                .foregroundStyle(.secondary)
                .frame(maxWidth: .infinity, alignment: .trailing)
                .padding(.top, DS.Spacing.xxs)
            OnboardingBubble(text: samples.welcomeReply, side: .reply)
        }
        .dsCard()
        .accessibilityElement(children: .combine)
    }

    /// Copy › choose › reply. A column at accessibility sizes.
    @ViewBuilder
    private var flow: some View {
        let steps: [(String, LocalizedStringKey)] = [
            ("doc.on.doc", "onboarding.welcome.flow.copy"),
            ("person.2", "onboarding.welcome.flow.choose"),
            ("text.bubble", "onboarding.welcome.flow.reply")
        ]
        if typeSize.isAccessibilitySize {
            VStack(alignment: .leading, spacing: DS.Spacing.s) {
                ForEach(steps, id: \.0) { symbol, text in
                    HStack(spacing: DS.Spacing.s) {
                        DSSymbolDisc(symbol: symbol)
                        Text(text).font(.subheadline)
                    }
                }
            }
        } else {
            HStack(alignment: .top, spacing: DS.Spacing.xxs) {
                ForEach(Array(steps.enumerated()), id: \.offset) { index, step in
                    if index > 0 {
                        Image(systemName: "chevron.right")
                            .font(.footnote.weight(.semibold))
                            .foregroundStyle(.tertiary)
                            .padding(.top, 14)
                            .accessibilityHidden(true)
                    }
                    VStack(spacing: DS.Spacing.xs) {
                        DSSymbolDisc(symbol: step.0)
                        Text(step.1)
                            .font(.footnote)
                            .multilineTextAlignment(.center)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    .frame(maxWidth: .infinity)
                }
            }
            .accessibilityElement(children: .combine)
        }
    }
}

private struct OnboardingMicrophoneSetupCard: View {
    private enum Status {
        case ready, requesting, granted, denied
    }

    @Environment(\.scenePhase) private var scenePhase
    @State private var status: Status? = AVAudioApplication.shared.recordPermission == .undetermined ? .ready : nil

    var body: some View {
        if let status {
            VStack(alignment: .leading, spacing: DS.Spacing.s) {
                switch status {
                case .ready, .requesting:
                    heading("onboarding.voice.microphone.title", "onboarding.voice.microphone.body", symbol: "mic")
                    Button("onboarding.voice.microphone.setup") {
                        Task { await requestPermission() }
                    }
                    .buttonStyle(.dsPrimary)
                    .disabled(status == .requesting)
                    .accessibilityIdentifier("onboarding.voice.microphone.setup")
                case .granted:
                    heading("onboarding.voice.microphone.granted.title", "onboarding.voice.microphone.granted.body",
                            symbol: "checkmark.circle")
                    if #available(iOS 18, *) {
                        VoiceControlCenterHelp()
                            .accessibilityIdentifier("onboarding.voice.controlCenterHelp")
                    }
                case .denied:
                    heading("onboarding.voice.microphone.denied.title", "onboarding.voice.microphone.denied.body",
                            symbol: "mic.slash")
                }
            }
            .dsCard()
            .accessibilityIdentifier("onboarding.voice.microphone")
            .onChange(of: scenePhase) { _, phase in
                guard phase == .active, status == .granted || status == .denied else { return }
                switch AVAudioApplication.shared.recordPermission {
                case .granted: self.status = .granted
                case .denied: self.status = .denied
                default: self.status = .ready
                }
            }
        }
    }

    private func heading(_ title: LocalizedStringKey, _ text: LocalizedStringKey, symbol: String) -> some View {
        VStack(alignment: .leading, spacing: DS.Spacing.xs) {
            Label(title, systemImage: symbol)
                .font(.headline)
                .accessibilityAddTraits(.isHeader)
            Text(text)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    @MainActor
    private func requestPermission() async {
        guard status == .ready else { return }
        status = .requesting
        let granted = await AVAudioApplication.requestRecordPermission()
        status = granted ? .granted : .denied
    }
}
