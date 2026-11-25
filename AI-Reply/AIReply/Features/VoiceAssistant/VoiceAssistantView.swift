import SwiftUI

/// AI Reply Voice in the app: say an instruction, and the reply waits in the
/// AI Reply keyboard. The status, live text and capture buttons come first,
/// so they stay visible at the sheet's medium height; context and setup
/// follow. Built from the app's design system, so light and dark match the
/// rest of AI Reply.
struct VoiceAssistantView: View {
    @Environment(VoiceAssistantController.self) private var voice
    @Environment(ReplyConfigurationModel.self) private var configuration
    @Environment(\.dismiss) private var dismiss
    @State private var showsAudioConsent = false
    @Environment(\.dynamicTypeSize) private var typeSize
    @ScaledMetric(relativeTo: .headline) private var badgeSize: CGFloat = 40

    private static let privacyURL = URL(string: "https://ai-reply.kz/privacy")

    var body: some View {
        @Bindable var voice = voice
        let copy = voice.copy
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: DS.Spacing.l) {
                    VStack(alignment: .leading, spacing: DS.Spacing.s) {
                        captureCard(copy)
                        if showsResponse {
                            responseCard(copy)
                        } else {
                            captureControls(copy)
                        }
                        if !voice.isBusy {
                            if #available(iOS 18, *) {
                                VoiceControlCenterHelp()
                                    .padding(.horizontal, DS.Spacing.xs)
                                    .accessibilityIdentifier("voice.controlCenterHelp")
                            }
                            setupLink
                        }
                    }
                    messageSection(copy, message: $voice.sourceMessage)
                    optionsSection(copy, templateID: $voice.templateID, language: $voice.languageCode)
                    privacySection(copy)
                }
                .padding(.horizontal, DS.Spacing.m)
                .padding(.top, DS.Spacing.xs)
                .padding(.bottom, DS.Spacing.xl)
                .frame(maxWidth: DS.Layout.readableWidth)
                .frame(maxWidth: .infinity)
                .animation(.default, value: voice.snapshot.phase)
                .animation(.default, value: voice.isStarting)
            }
            .scrollDismissesKeyboard(.interactively)
            .background(Color.dsBackground.ignoresSafeArea())
            .navigationTitle(Text(verbatim: "AI Reply Voice"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                // «Готово» belongs to dictation; the sheet itself just closes.
                ToolbarItem(placement: .cancellationAction) {
                    Button(copy.close) { dismiss() }
                        .accessibilityIdentifier("voice.close")
                }
            }
        }
        .onAppear(perform: keepTemplateVisible)
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
        .alert(copy.voiceConsentTitle, isPresented: $showsAudioConsent) {
            Button(copy.agreeToVoiceProcessing) { voice.acceptVoiceConsentAndStart() }
            Button("common.cancel", role: .cancel) {}
        } message: { Text(copy.privacy) }
    }

    // MARK: Capture

    private var showsResponse: Bool {
        voice.snapshot.phase == .responseReady && !voice.snapshot.response.isEmpty
    }

    private var showsTranscript: Bool {
        voice.isCapturing || voice.hasTranscript
    }

    private func captureCard(_ copy: VoiceAssistantCopy) -> some View {
        VStack(alignment: .leading, spacing: DS.Spacing.m) {
            HStack(alignment: .center, spacing: DS.Spacing.s) {
                statusBadge
                VStack(alignment: .leading, spacing: 2) {
                    Text(voice.isStarting ? copy.connecting : copy.status(voice.snapshot.phase))
                        .font(.headline)
                        .accessibilityIdentifier("voice.status")
                    if let detail = statusDetail(copy) {
                        Text(detail)
                            .font(.subheadline)
                            .foregroundStyle(voice.snapshot.phase == .error ? Color.primary : Color.secondary)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .accessibilityElement(children: .combine)
                if voice.isCapturing {
                    Text(verbatim: Self.clock(voice.elapsed) + " / " + Self.clock(VoiceAssistantRecordingPolicy.maximumDuration))
                        .font(.subheadline.monospacedDigit())
                        .foregroundStyle(.secondary)
                        .accessibilityLabel(copy.recordedSeconds(Int(voice.elapsed)))
                }
            }
            if showsTranscript {
                VStack(alignment: .leading, spacing: DS.Spacing.xxs) {
                    Text(copy.instruction)
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(.secondary)
                    LiveTranscript(text: voice.snapshot.transcript, placeholder: copy.speakPrompt)
                }
            }
        }
        .dsCard()
    }

    private var statusBadge: some View {
        let phase = voice.snapshot.phase
        let symbol: String
        let foreground: Color
        let fill: Color
        switch phase {
        case .listening, .speechDetected:
            symbol = "waveform"; foreground = .white; fill = .accentColor
        case .responseReady:
            symbol = "checkmark"; foreground = .green; fill = Color.green.opacity(0.15)
        case .error:
            symbol = "exclamationmark.triangle.fill"; foreground = .orange; fill = Color.orange.opacity(0.15)
        case .paused:
            symbol = "pause.fill"; foreground = .secondary; fill = Color(.tertiarySystemFill)
        default:
            symbol = "mic.fill"; foreground = .accentColor; fill = Color.accentColor.opacity(0.12)
        }
        return ZStack {
            Circle().fill(fill)
            if voice.isStarting || voice.isProcessing {
                ProgressView().tint(.accentColor)
            } else {
                Image(systemName: symbol)
                    .font(.system(size: badgeSize * 0.42, weight: .semibold))
                    .foregroundStyle(foreground)
                    .symbolEffect(.variableColor.iterative, isActive: voice.isCapturing)
            }
        }
        .frame(width: badgeSize, height: badgeSize)
        .accessibilityHidden(true)
    }

    private func statusDetail(_ copy: VoiceAssistantCopy) -> String? {
        if voice.isStarting { return nil }
        switch voice.snapshot.phase {
        case .ready, .stopped:
            guard !voice.hasTranscript else { return nil }
            return voice.hasVoiceConsent ? copy.idleHint : copy.firstUseHint
        case .transcribing, .generating: return copy.processingHint
        case .paused: return voice.canContinuePausedTurn ? copy.pausedHint : voice.snapshot.error
        case .error: return voice.snapshot.error
        case .listening, .speechDetected, .responseReady: return nil
        }
    }

    @ViewBuilder
    private func captureControls(_ copy: VoiceAssistantCopy) -> some View {
        if voice.isCapturing {
            buttonPair {
                Button { voice.cancel() } label: { pairLabel(copy.stop) }
                    .buttonStyle(VoiceSecondaryButtonStyle(expands: typeSize.isAccessibilitySize))
                    .accessibilityIdentifier("voice.cancel")
                Button { voice.finish() } label: { pairLabel(copy.generate, symbol: "checkmark") }
                    .buttonStyle(.dsPrimary)
                    .accessibilityIdentifier("voice.finish")
            }
            note(copy.returnToChat)
        } else if voice.isStarting || voice.isProcessing {
            Button(copy.stop) { voice.cancel() }
                .buttonStyle(VoiceSecondaryButtonStyle())
                .accessibilityIdentifier("voice.cancel")
        } else if voice.canContinuePausedTurn {
            buttonPair {
                Button { voice.continuePausedTurn() } label: { pairLabel(copy.resume, symbol: "mic.fill") }
                    .buttonStyle(VoiceSecondaryButtonStyle())
                Button { voice.createReplyFromPausedTurn() } label: { pairLabel(copy.createReply) }
                    .buttonStyle(.dsPrimary)
            }
            Button(copy.startOver) { voice.cancel() }
                .buttonStyle(.dsText)
        } else {
            Button(action: startDictation) {
                Label(copy.start, systemImage: "mic.fill")
            }
            .buttonStyle(.dsPrimary)
            .accessibilityIdentifier("voice.start")

            if voice.microphoneAccessDenied, let url = URL(string: UIApplication.openSettingsURLString) {
                Link(destination: url) {
                    Label("permissions.openSettings", systemImage: "gearshape")
                }
                .buttonStyle(.dsText)
                .accessibilityIdentifier("voice.microphoneSettings")
            }
        }
    }

    private func startDictation() {
        voice.start { showsAudioConsent = true }
    }

    /// Two buttons side by side, stacked at accessibility text sizes.
    private func buttonPair<Content: View>(@ViewBuilder _ content: () -> Content) -> some View {
        let layout = typeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(spacing: DS.Spacing.s))
            : AnyLayout(HStackLayout(spacing: DS.Spacing.s))
        return layout(content)
    }

    /// One line beside another button (slightly smaller before it would
    /// wrap); stacked buttons have the width to wrap instead.
    private func pairLabel(_ title: String, symbol: String? = nil) -> some View {
        Group {
            if let symbol {
                Label(title, systemImage: symbol)
            } else {
                Text(title)
            }
        }
        .lineLimit(typeSize.isAccessibilitySize ? 3 : 1)
        .minimumScaleFactor(0.8)
    }

    // MARK: Response

    private func responseCard(_ copy: VoiceAssistantCopy) -> some View {
        VStack(alignment: .leading, spacing: DS.Spacing.s) {
            VStack(alignment: .leading, spacing: DS.Spacing.s) {
                Text(copy.response)
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.secondary)
                Text(voice.snapshot.response)
                    .font(.body)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityIdentifier("voice.response")
                Divider()
                HStack(spacing: DS.Spacing.m) {
                    ShareLink(item: voice.snapshot.response) {
                        Label(copy.share, systemImage: "square.and.arrow.up")
                    }
                    Spacer(minLength: 0)
                    Button { voice.regenerate() } label: {
                        Label(copy.again, systemImage: "arrow.clockwise")
                    }
                    .disabled(voice.isBusy)
                }
                .font(.subheadline.weight(.semibold))
                .buttonStyle(.borderless)
            }
            .dsCard()
            note(copy.keyboardResult)
            Button(action: startDictation) {
                Label(copy.newInstruction, systemImage: "mic.fill")
            }
            .buttonStyle(VoiceSecondaryButtonStyle())
            .accessibilityIdentifier("voice.start")
        }
    }

    // MARK: Context

    private func messageSection(_ copy: VoiceAssistantCopy, message: Binding<String>) -> some View {
        VStack(alignment: .leading, spacing: DS.Spacing.xs) {
            HStack(alignment: .firstTextBaseline) {
                Text(copy.message)
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(.secondary)
                    .accessibilityAddTraits(.isHeader)
                Spacer(minLength: DS.Spacing.s)
                Text(copy.optional)
                    .font(.footnote)
                    .foregroundStyle(.tertiary)
            }
            .padding(.horizontal, DS.Spacing.xxs)
            VStack(alignment: .leading, spacing: DS.Spacing.s) {
                TextField(copy.optionalMessage, text: message, axis: .vertical)
                    .lineLimit(2...6)
                    .accessibilityIdentifier("voice.message")
                HStack(spacing: DS.Spacing.s) {
                    // The system Paste control: the clipboard is read only
                    // when the user taps it, never on appearance.
                    PasteButton(payloadType: String.self) { values in
                        message.wrappedValue = values.joined(separator: "\n")
                            .trimmingCharacters(in: .whitespacesAndNewlines)
                    }
                    .buttonBorderShape(.capsule)
                    .labelStyle(.titleAndIcon)
                    .controlSize(.small)
                    .tint(.accentColor)
                    Spacer(minLength: 0)
                    if !message.wrappedValue.isEmpty {
                        Button(copy.clear) { message.wrappedValue = "" }
                            .font(.subheadline)
                            .buttonStyle(.borderless)
                    }
                }
            }
            .dsCard()
            Text(copy.messageFooter)
                .font(.footnote)
                .foregroundStyle(.secondary)
                .padding(.horizontal, DS.Spacing.xxs)
        }
        .disabled(voice.isBusy)
    }

    private func optionsSection(_ copy: VoiceAssistantCopy, templateID: Binding<String?>,
                                language: Binding<String>) -> some View {
        let hasMessage = !voice.sourceMessage.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        let visible = configuration.visibleTemplates
        // No choice, or one now hidden, shows (and generates with) the first
        // visible template, exactly as the reply request falls back.
        let template = Binding<String?>(
            get: { visible.contains(where: { $0.id == templateID.wrappedValue }) ? templateID.wrappedValue : visible.first?.id },
            set: { templateID.wrappedValue = $0 })
        return VStack(alignment: .leading, spacing: DS.Spacing.xs) {
            VStack(spacing: 0) {
                // A template shapes a reply to a message; standalone text
                // has none, so the row appears once there is a message.
                if hasMessage {
                    optionRow(copy.template) {
                        Picker(copy.template, selection: template) {
                            ForEach(configuration.visibleTemplates) { template in
                                Text(template.displayName(appLanguage: SharedSettings.shared.effectiveAppLanguage))
                                    .tag(Optional(template.id))
                            }
                        }
                    }
                    Divider().padding(.leading, DS.Spacing.m)
                }
                optionRow(copy.speechLanguage) {
                    Picker(copy.speechLanguage, selection: language) {
                        Text(copy.automatic).tag("auto")
                        Text(verbatim: "Русский").tag("ru")
                        Text(verbatim: "Қазақша").tag("kk")
                        Text(verbatim: "English").tag("en")
                    }
                }
            }
            .background(RoundedRectangle(cornerRadius: DS.Radius.large, style: .continuous).fill(Color.dsSurface))
            Text(copy.languageFooter)
                .font(.footnote)
                .foregroundStyle(.secondary)
                .padding(.horizontal, DS.Spacing.xxs)
        }
        .disabled(voice.isBusy)
    }

    private func optionRow<Content: View>(_ title: String, @ViewBuilder content: () -> Content) -> some View {
        HStack(spacing: DS.Spacing.s) {
            Text(title)
                .lineLimit(2)
            Spacer(minLength: DS.Spacing.s)
            // The chosen value keeps one line; the title gives way first.
            content()
                .labelsHidden()
                .pickerStyle(.menu)
                .fixedSize()
                .layoutPriority(1)
        }
        .padding(.leading, DS.Spacing.m)
        .padding(.trailing, DS.Spacing.xxs)
        .frame(minHeight: DS.Layout.minimumTouchTarget + 4)
    }

    /// A saved template that is now hidden would leave the picker blank.
    private func keepTemplateVisible() {
        let visible = configuration.visibleTemplates
        if !visible.contains(where: { $0.id == voice.templateID }) {
            voice.templateID = visible.first?.id
        }
    }

    // MARK: Setup and privacy

    private var setupLink: some View {
        NavigationLink { VoiceTutorialView(isInVoiceScreen: true) } label: {
            HStack(spacing: DS.Spacing.s) {
                Label("tutorial.voice.title", systemImage: "play.rectangle")
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(Color.accentColor)
                    .fixedSize(horizontal: false, vertical: true)
                Spacer(minLength: DS.Spacing.s)
                Image(systemName: "chevron.right")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.tertiary)
                    .accessibilityHidden(true)
            }
            .frame(minHeight: DS.Layout.minimumTouchTarget)
            .contentShape(Rectangle())
            .padding(.horizontal, DS.Spacing.xs)
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier("voice.setupGuide")
    }

    private func privacySection(_ copy: VoiceAssistantCopy) -> some View {
        VStack(alignment: .leading, spacing: DS.Spacing.xs) {
            Text(copy.privacy)
                .font(.footnote)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
            if let url = Self.privacyURL {
                Link(copy.privacyLink, destination: url)
                    .font(.footnote.weight(.semibold))
            }
        }
        .padding(.horizontal, DS.Spacing.xxs)
    }

    private func note(_ text: String) -> some View {
        Text(text)
            .font(.footnote)
            .foregroundStyle(.secondary)
            .fixedSize(horizontal: false, vertical: true)
            .padding(.horizontal, DS.Spacing.xxs)
    }

    /// 0:07, 0:40: the same in every language, unlike "7 s".
    static func clock(_ seconds: TimeInterval) -> String {
        let whole = max(0, Int(seconds.rounded(.down)))
        return String(format: "%d:%02d", whole / 60, whole % 60)
    }
}

/// The recognized words as they arrive. Short text takes its own height;
/// longer text scrolls inside a bounded area that follows the newest words.
private struct LiveTranscript: View {
    let text: String
    let placeholder: String
    private let maximumHeight: CGFloat = 150

    var body: some View {
        ViewThatFits(in: .vertical) {
            label
            ScrollViewReader { proxy in
                ScrollView {
                    VStack(alignment: .leading, spacing: 0) {
                        label
                        Color.clear.frame(height: 1).id("voice-transcript-end")
                    }
                }
                .scrollBounceBehavior(.basedOnSize)
                .onAppear { proxy.scrollTo("voice-transcript-end", anchor: .bottom) }
                .onChange(of: text) {
                    withAnimation(.easeOut(duration: 0.15)) {
                        proxy.scrollTo("voice-transcript-end", anchor: .bottom)
                    }
                }
            }
        }
        .frame(maxHeight: maximumHeight, alignment: .top)
    }

    private var label: some View {
        Text(text.isEmpty ? placeholder : text)
            .font(.body)
            .foregroundStyle(text.isEmpty ? .secondary : .primary)
            .textSelection(.enabled)
            .frame(maxWidth: .infinity, alignment: .leading)
            .fixedSize(horizontal: false, vertical: true)
            .accessibilityIdentifier("voice.liveTranscript")
    }
}

/// The quiet button next to the primary one: the same 50 pt height, so a
/// pair reads as one row.
private struct VoiceSecondaryButtonStyle: ButtonStyle {
    var expands = true

    func makeBody(configuration: Configuration) -> some View {
        StyleBody(configuration: configuration, expands: expands)
    }

    private struct StyleBody: View {
        let configuration: ButtonStyleConfiguration
        let expands: Bool
        @Environment(\.isEnabled) private var isEnabled

        var body: some View {
            configuration.label
                .font(.body.weight(.semibold))
                .foregroundStyle(Color.accentColor)
                .multilineTextAlignment(.center)
                .padding(.horizontal, DS.Spacing.m)
                .frame(maxWidth: expands ? .infinity : nil, minHeight: DS.Layout.primaryButtonHeight)
                .background(
                    RoundedRectangle(cornerRadius: DS.Radius.medium, style: .continuous)
                        .fill(Color.dsSurface)
                )
                .contentShape(RoundedRectangle(cornerRadius: DS.Radius.medium, style: .continuous))
                .opacity(isEnabled ? (configuration.isPressed ? 0.7 : 1) : 0.4)
        }
    }
}

/// Explicit app-language copy, like the existing keyboard's string tables.
struct VoiceAssistantCopy {
    let language: AppLanguage
    private func text(_ en: String, _ ru: String, _ kk: String, _ uz: String) -> String {
        switch language { case .english: en; case .russian: ru; case .kazakh: kk; case .uzbek: uz }
    }
    var start: String { text("Start dictation", "Начать диктовку", "Айтып жазуды бастау", "Aytib yozishni boshlash") }
    var stop: String { text("Cancel", "Отмена", "Болдырмау", "Bekor qilish") }
    var close: String { text("Close", "Закрыть", "Жабу", "Yopish") }
    var generate: String { text("Done · generate reply", "Готово · создать ответ", "Дайын · жауап жасау", "Tayyor · javob yaratish") }
    var resume: String { text("Continue", "Продолжить", "Жалғастыру", "Davom etish") }
    var createReply: String { text("Create reply", "Создать ответ", "Жауап жасау", "Javob yaratish") }
    var startOver: String { text("Start over", "Начать заново", "Қайта бастау", "Qaytadan boshlash") }
    var newInstruction: String { text("New instruction", "Новая инструкция", "Жаңа нұсқау", "Yangi koʻrsatma") }
    var speakPrompt: String { text("Speak… your words will appear here.", "Говорите… здесь появятся ваши слова.", "Сөйлеңіз… сөзіңіз осында көрсетіледі.", "Gapiring… so‘zlaringiz shu yerda ko‘rinadi.") }
    var firstUseHint: String { text("Tap Start dictation to set up the microphone for AI Reply Voice.", "Нажмите «Начать диктовку», чтобы настроить микрофон для AI Reply Voice.", "AI Reply Voice микрофонын баптау үшін «Айтып жазуды бастау» түймесін басыңыз.", "AI Reply Voice mikrofonini sozlash uchun «Aytib yozishni boshlash» tugmasini bosing.") }
    var idleHint: String { text("Say what to reply or write, for example: “Tell her I’ll be there at seven.”", "Скажите, что ответить или написать, например: «Напиши, что буду в семь».", "Не жауап беру не жазу керегін айтыңыз, мысалы: «Жетіде келемін деп жаз».", "Nima deb javob berish yoki yozishni ayting, masalan: «Yettida boraman deb yoz».") }
    var processingHint: String { text("You can return to your chat — the response will appear in the AI Reply keyboard.", "Можно вернуться в чат — ответ появится в клавиатуре AI Reply.", "Чатқа орала беріңіз — жауап AI Reply пернетақтасында шығады.", "Chatga qaytishingiz mumkin — javob AI Reply klaviaturasida chiqadi.") }
    var pausedHint: String { text("Continue recording, or create a reply from what you said.", "Продолжите запись или создайте ответ из сказанного.", "Жазуды жалғастырыңыз немесе айтылғаннан жауап жасаңыз.", "Yozishni davom ettiring yoki aytilganlardan javob yarating.") }
    var again: String { text("Regenerate", "Другой вариант", "Басқа нұсқа", "Boshqa variant") }
    var share: String { text("Share", "Поделиться", "Бөлісу", "Ulashish") }
    var instruction: String { text("Your instruction", "Ваша инструкция", "Сіздің нұсқауыңыз", "Koʻrsatmangiz") }
    var response: String { text("Response", "Ответ", "Жауап", "Javob") }
    var message: String { text("Message to reply to", "Сообщение для ответа", "Жауап беретін хабарлама", "Javob uchun xabar") }
    var optional: String { text("Optional", "Необязательно", "Міндетті емес", "Ixtiyoriy") }
    var optionalMessage: String { text("Paste the message you’re replying to", "Вставьте сообщение, на которое отвечаете", "Жауап беретін хабарламаны қойыңыз", "Javob beradigan xabarni qoʻying") }
    var messageFooter: String { text("Leave it empty and AI Reply writes new text from your instruction.", "Оставьте пустым — AI Reply напишет новый текст по вашей инструкции.", "Бос қалдырсаңыз, AI Reply нұсқауыңыз бойынша жаңа мәтін жазады.", "Boʻsh qoldirsangiz, AI Reply koʻrsatmangiz boʻyicha yangi matn yozadi.") }
    var clear: String { text("Clear", "Очистить", "Тазалау", "Tozalash") }
    var template: String { text("Template", "Шаблон", "Үлгі", "Andoza") }
    var speechLanguage: String { text("Speech language", "Язык речи", "Сөйлеу тілі", "Nutq tili") }
    var automatic: String { text("Automatic", "Автоматически", "Автоматты", "Avtomatik") }
    var languageFooter: String { text("Automatic recognizes Russian, Kazakh and English.", "«Автоматически» распознаёт русский, казахский и английский.", "«Автоматты» орыс, қазақ және ағылшын тілдерін таниды.", "«Avtomatik» rus, qozoq va ingliz tillarini taniydi.") }
    var connecting: String { text("Preparing microphone…", "Подключаем микрофон…", "Микрофон дайындалуда…", "Mikrofon tayyorlanmoqda…") }
    var returnToChat: String { text("You can switch to your chat and keep talking. Tap Done here, in the AI Reply keyboard or in the Dynamic Island. Up to 40 seconds.", "Можно перейти в чат и продолжать говорить. Нажмите «Готово» здесь, в клавиатуре AI Reply или в Dynamic Island. До 40 секунд.", "Чатқа өтіп, сөйлей беруге болады. «Дайын» түймесін осында, AI Reply пернетақтасында немесе Dynamic Island ішінде басыңыз. 40 секундқа дейін.", "Chatga o‘tib, gapirishda davom etishingiz mumkin. «Tayyor»ni shu yerda, AI Reply klaviaturasida yoki Dynamic Island’da bosing. 40 soniyagacha.") }
    var keyboardResult: String { text("The response is waiting in the AI Reply keyboard: open your chat and tap Insert.", "Ответ уже ждёт в клавиатуре AI Reply: откройте чат и нажмите «Вставить».", "Жауап AI Reply пернетақтасында күтіп тұр: чатты ашып, «Кірістіру» түймесін басыңыз.", "Javob AI Reply klaviaturasida kutmoqda: chatni ochib, «Kiritish»ni bosing.") }
    func recordedSeconds(_ seconds: Int) -> String {
        text("\(seconds) of 40 seconds recorded", "Записано \(seconds) из 40 секунд", "40 секундтың \(seconds) секунды жазылды", "40 soniyadan \(seconds) soniya yozildi")
    }
    var privacy: String { text("Your speech is sent through AI Reply to OpenAI (USA) for transcription. Your message and instruction use your usual AI Reply plan. AI Reply does not save audio. OpenAI processes data under its API data policy; temporary retention may apply. You can withdraw voice consent in Settings → Privacy.", "Ваша речь передаётся через AI Reply в OpenAI (США) для распознавания. Сообщение и инструкция обрабатываются по вашему тарифу AI Reply. AI Reply не сохраняет аудио. OpenAI обрабатывает данные по своей политике API; возможно временное хранение. Согласие на голос можно отозвать: Настройки → Конфиденциальность.", "Сөзіңіз AI Reply арқылы OpenAI-ға (АҚШ) тану үшін жіберіледі. Хабарлама мен нұсқау AI Reply тарифіңіз бойынша өңделеді. AI Reply аудионы сақтамайды. OpenAI деректерді API саясаты бойынша өңдейді; уақытша сақтау болуы мүмкін. Дауысқа келісімді Баптаулар → Құпиялық бөлімінде қайтаруға болады.", "Nutqingiz AI Reply orqali OpenAIʼga (AQSh) tanish uchun yuboriladi. Xabar va koʻrsatma AI Reply tarifingiz boʻyicha ishlanadi. AI Reply audioni saqlamaydi. OpenAI API siyosatiga koʻra maʼlumotlarni qayta ishlaydi; vaqtinchalik saqlash mumkin. Ovoz roziligini Sozlamalar → Maxfiylikda bekor qilish mumkin.") }
    var voiceConsentTitle: String { text("Voice processing", "Обработка голоса", "Дауысты өңдеу", "Ovozni qayta ishlash") }
    var agreeToVoiceProcessing: String { text("Agree", "Согласен", "Келісемін", "Roziman") }
    var privacyLink: String { text("Privacy policy", "Политика конфиденциальности", "Құпиялық саясаты", "Maxfiylik siyosati") }
    var controlSetupRequired: String {
        text("Open AI Reply, tap the microphone on Home, then tap Start dictation to finish setup.",
             "Откройте AI Reply, нажмите микрофон на главном экране, затем «Начать диктовку», чтобы завершить настройку.",
             "AI Reply қолданбасын ашыңыз. Баптауды аяқтау үшін басты экрандағы микрофонды, содан кейін «Айтып жазуды бастау» түймесін басыңыз.",
             "AI Reply’ni oching. Sozlashni yakunlash uchun bosh ekrandagi mikrofonni, soʻng «Aytib yozishni boshlash» tugmasini bosing.")
    }
    var interrupted: String { text("Recording interrupted. Start a new voice session.", "Запись прервана. Запустите новый голосовой сеанс.", "Жазу үзілді. Жаңа дауыс сеансын бастаңыз.", "Yozish toʻxtatildi. Yangi ovoz seansini boshlang.") }
    var signIn: String { text("Sign in and accept the current terms in AI Reply.", "Войдите и примите актуальные условия AI Reply.", "AI Reply-ға кіріп, қолданыстағы шарттарды қабылдаңыз.", "AI Replyʼga kirib, amaldagi shartlarni qabul qiling.") }
    var permission: String { text("Dictation needs microphone access. You can change access in Settings.", "Для диктовки нужен доступ к микрофону. Его можно изменить в Настройках.", "Айтып жазу үшін микрофонға рұқсат қажет. Оны Баптауларда өзгертуге болады.", "Aytib yozish uchun mikrofonga ruxsat kerak. Ruxsatni Sozlamalarda oʻzgartirishingiz mumkin.") }
    var unavailable: String { text("Voice transcription is unavailable on this server.", "На сервере пока недоступно распознавание голоса.", "Бұл серверде дауысты тану әзірге қолжетімсіз.", "Bu serverda nutqni tanish hozircha mavjud emas.") }
    var liveActivityRequired: String { text("Enable Live Activities for AI Reply in iPhone Settings to record from Control Center.", "Включите «Эфир активности» (Live Activities) для AI Reply в настройках iPhone, чтобы записывать из Пункта управления.", "Басқару орталығынан жазу үшін iPhone баптауларында AI Reply Live Activities мүмкіндігін қосыңыз.", "Boshqaruv markazidan yozish uchun iPhone sozlamalarida AI Reply Live Activities imkoniyatini yoqing.") }
    var recordingFailed: String { text("Could not complete dictation. Check the connection and try again.", "Не удалось завершить диктовку. Проверьте связь и попробуйте снова.", "Айтып жазу аяқталмады. Байланысты тексеріп, қайталаңыз.", "Aytib yozish tugamadi. Aloqani tekshirib, qayta urinib koʻring.") }
    var nothingHeard: String { text("No speech detected. Try again closer to the microphone.", "Речь не распознана. Попробуйте ближе к микрофону.", "Сөйлеу анықталмады. Микрофонға жақындап, қайталаңыз.", "Nutq aniqlanmadi. Mikrofonga yaqinroq qayta urinib koʻring.") }
    var voiceLimit: String { text("Today's dictation limit is reached. You can still type.", "Лимит диктовки на сегодня исчерпан. Можно ввести текст вручную.", "Бүгінгі айтып жазу шегі таусылды. Мәтінді қолмен енгізуге болады.", "Bugungi aytib yozish limiti tugadi. Matnni qoʻlda yozish mumkin.") }
    var offline: String { text("No internet connection.", "Нет подключения к интернету.", "Интернет байланысы жоқ.", "Internet aloqasi yoʻq.") }
    var timedOut: String { text("The request timed out. Open AI Reply and try again.", "Время ожидания истекло. Откройте AI Reply и повторите.", "Күту уақыты аяқталды. AI Reply-ды ашып, қайталаңыз.", "Kutish vaqti tugadi. AI Replyʼni ochib, qayta urinib koʻring.") }
    func status(_ phase: VoiceAssistantPhase) -> String {
        switch phase {
        case .ready, .stopped: text("Ready to dictate", "Готов к диктовке", "Айтып жазуға дайын", "Aytib yozishga tayyor")
        case .listening: text("Listening…", "Слушаю…", "Тыңдап тұрмын…", "Tinglayapman…")
        case .speechDetected: text("Speech detected", "Слышу вас", "Сөзіңізді естіп тұрмын", "Nutq eshitilmoqda")
        case .transcribing: text("Transcribing…", "Распознаю речь…", "Сөз танылуда…", "Nutq tanilmoqda…")
        case .generating: text("Generating response…", "Создаю ответ…", "Жауап жасалуда…", "Javob yaratilmoqda…")
        case .responseReady: text("Response ready", "Ответ готов", "Жауап дайын", "Javob tayyor")
        case .paused: text("Recording paused", "Запись приостановлена", "Жазу кідіртілді", "Yozish toʻxtatildi")
        case .error: text("Please try again", "Попробуйте снова", "Қайталап көріңіз", "Qayta urinib koʻring")
        }
    }
}
