import SwiftUI

struct VoiceAssistantView: View {
    @Environment(VoiceAssistantController.self) private var voice
    @Environment(ReplyConfigurationModel.self) private var configuration
    @Environment(\.dismiss) private var dismiss
    @State private var showsAudioConsent = false

    var body: some View {
        @Bindable var voice = voice
        let copy = voice.copy
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    HStack(spacing: 12) {
                        Image(systemName: voice.isCapturing ? "waveform" : "mic.fill")
                            .font(.system(size: 24, weight: .medium))
                            .foregroundStyle(Color.accentColor)
                            .frame(width: 40, height: 40)
                            .background(Color.accentColor.opacity(0.1), in: Circle())
                            .symbolEffect(.variableColor, isActive: voice.isCapturing)
                        Text(voice.isStarting ? copy.connecting : copy.status(voice.snapshot.phase))
                            .font(.headline)
                        Spacer(minLength: 0)
                        if voice.isCapturing {
                            Text("\(Int(voice.elapsed)) / 40 s")
                                .font(.subheadline.monospacedDigit())
                                .foregroundStyle(.secondary)
                        }
                    }
                    if let error = voice.snapshot.error {
                        Text(error).font(.subheadline).foregroundStyle(.orange)
                    }
                    if voice.isCapturing || voice.isStarting || !voice.snapshot.transcript.isEmpty {
                        liveTranscriptCard(title: copy.instruction, placeholder: copy.speakPrompt)
                    }
                    if voice.isCapturing {
                        HStack(spacing: 12) {
                            Button(copy.stop, systemImage: "xmark") { voice.stop() }
                                .buttonStyle(.bordered)
                                .frame(minHeight: 44)
                            Button { voice.finish() } label: {
                                Label(copy.generate, systemImage: "checkmark")
                                    .font(.subheadline.weight(.semibold))
                                    .frame(maxWidth: .infinity, minHeight: 44)
                            }
                            .buttonStyle(.borderedProminent)
                        }
                        Text(copy.returnToChat).font(.footnote).foregroundStyle(.secondary)
                    } else if voice.isBusy {
                        HStack {
                            ProgressView()
                            Spacer()
                            Button(copy.stop, systemImage: "xmark") { voice.stop() }.buttonStyle(.bordered)
                        }
                    } else {
                        Button {
                            if voice.hasVoiceConsent { voice.start() } else { showsAudioConsent = true }
                        } label: {
                            Label(copy.start, systemImage: "mic.fill").frame(maxWidth: .infinity).padding(.vertical, 6)
                        }
                        .buttonStyle(.borderedProminent)
                    }
                    if !voice.snapshot.response.isEmpty {
                        textCard(title: copy.response, text: voice.snapshot.response)
                        Text(copy.keyboardResult).font(.footnote).foregroundStyle(.secondary)
                        HStack {
                            ShareLink(item: voice.snapshot.response) { Label(copy.share, systemImage: "square.and.arrow.up") }
                            Spacer()
                            Button(copy.again, systemImage: "arrow.clockwise") { voice.regenerate() }.disabled(voice.isBusy)
                        }.buttonStyle(.bordered)
                    }
                    GroupBox {
                        VStack(alignment: .leading, spacing: 12) {
                            HStack {
                                Text(copy.message).font(.subheadline.bold())
                                Spacer()
                                // System paste control: no clipboard reads on appearance.
                                PasteButton(payloadType: String.self) { values in
                                    voice.sourceMessage = values.joined(separator: "\n")
                                }.labelStyle(.iconOnly)
                            }
                            TextField(copy.optionalMessage, text: $voice.sourceMessage, axis: .vertical)
                                .lineLimit(2...5)
                            Picker(copy.template, selection: $voice.templateID) {
                                ForEach(configuration.visibleTemplates) { template in
                                    Text(template.displayName(appLanguage: SharedSettings.shared.effectiveAppLanguage))
                                        .tag(Optional(template.id))
                                }
                            }
                            Picker(copy.speechLanguage, selection: $voice.languageCode) {
                                Text(copy.automatic).tag("auto")
                                Text("Русский").tag("ru")
                                Text("Қазақша").tag("kk")
                                Text("English").tag("en")
                            }
                        }
                    }.disabled(voice.isBusy)
                    Text(copy.privacy).font(.footnote).foregroundStyle(.secondary).multilineTextAlignment(.center)
                    Link(copy.privacyLink, destination: URL(string: "https://ai-reply.kz/privacy")!).font(.footnote)
                    VStack(alignment: .leading, spacing: 8) {
                        Label(copy.quickAccess, systemImage: "switch.2").font(.headline)
                        Text(copy.onboarding).font(.footnote).foregroundStyle(.secondary)
                    }.frame(maxWidth: .infinity, alignment: .leading)
                }.padding(.horizontal, 20).padding(.vertical, 16)
            }
            .navigationTitle("AI Reply Voice")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button(copy.close) { dismiss() } } }
        }
        .onAppear {
            if voice.templateID == nil { voice.templateID = configuration.visibleTemplates.first?.id }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
        .alert("AI Reply Voice", isPresented: $showsAudioConsent) {
            Button(copy.allowAndStart) { voice.start(consent: true) }
            Button(copy.notNow, role: .cancel) {}
        } message: { Text(copy.privacy) }
    }

    private func textCard(title: String, text: String) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(title).font(.caption.weight(.semibold)).foregroundStyle(.secondary)
            Text(text).textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading)
        }.padding(16).background(.quaternary.opacity(0.5), in: RoundedRectangle(cornerRadius: 18))
    }

    private func liveTranscriptCard(title: String, placeholder: String) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(title).font(.caption.weight(.semibold)).foregroundStyle(.secondary)
            ScrollViewReader { proxy in
                ScrollView {
                    VStack(alignment: .leading, spacing: 0) {
                        Text(voice.snapshot.transcript.isEmpty ? placeholder : voice.snapshot.transcript)
                            .font(.body)
                            .foregroundStyle(voice.snapshot.transcript.isEmpty ? .secondary : .primary)
                            .textSelection(.enabled)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .accessibilityIdentifier("voice.liveTranscript")
                        Color.clear.frame(height: 1).id("voice-transcript-end")
                    }
                }
                .frame(height: 72)
                .onChange(of: voice.snapshot.transcript) {
                    withAnimation(.easeOut(duration: 0.15)) {
                        proxy.scrollTo("voice-transcript-end", anchor: .bottom)
                    }
                }
            }
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.accentColor.opacity(0.07), in: RoundedRectangle(cornerRadius: 16))
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
    var close: String { text("Done", "Готово", "Дайын", "Tayyor") }
    var generate: String { text("Done · generate reply", "Готово · создать ответ", "Дайын · жауап жасау", "Tayyor · javob yaratish") }
    var speakPrompt: String { text("Speak… your words will appear here.", "Говорите… здесь появятся ваши слова.", "Сөйлеңіз… сөзіңіз осында көрсетіледі.", "Gapiring… so‘zlaringiz shu yerda ko‘rinadi.") }
    var again: String { text("Regenerate", "Другой вариант", "Басқа нұсқа", "Boshqa variant") }
    var share: String { text("Share", "Поделиться", "Бөлісу", "Ulashish") }
    var instruction: String { text("Your instruction", "Ваша инструкция", "Сіздің нұсқауыңыз", "Koʻrsatmangiz") }
    var response: String { text("Response", "Ответ", "Жауап", "Javob") }
    var message: String { text("Message context", "Сообщение для ответа", "Жауап беретін хабарлама", "Javob uchun xabar") }
    var optionalMessage: String { text("Paste a message, or leave empty to create new text", "Вставьте сообщение или оставьте пустым для нового текста", "Хабарламаны қойыңыз немесе жаңа мәтін үшін бос қалдырыңыз", "Xabar qoʻying yoki yangi matn uchun boʻsh qoldiring") }
    var template: String { text("Template", "Шаблон", "Үлгі", "Andoza") }
    var speechLanguage: String { text("Speech language", "Язык речи", "Сөйлеу тілі", "Nutq tili") }
    var automatic: String { text("Automatic · RU / KK / EN", "Автоматически · RU / KK / EN", "Автоматты · RU / KK / EN", "Avtomatik · RU / KK / EN") }
    var connecting: String { text("Preparing microphone…", "Подключаем микрофон…", "Микрофон дайындалуда…", "Mikrofon tayyorlanmoqda…") }
    var returnToChat: String { text("You can return to your chat. Speak at your pace, then tap Done here, in the keyboard or Dynamic Island. Maximum 40 seconds.", "Можно вернуться в чат. Говорите в своём темпе, затем нажмите «Готово» здесь, в клавиатуре или Dynamic Island. Не дольше 40 секунд.", "Чатқа оралуға болады. Асықпай сөйлеп, соңында осында, пернетақтада немесе Dynamic Island ішінде «Дайын» түймесін басыңыз. Ең көбі 40 секунд.", "Chatga qaytishingiz mumkin. Shoshilmay gapiring, so‘ng shu yerda, klaviatura yoki Dynamic Island’da «Tayyor»ni bosing. Eng ko‘pi 40 soniya.") }
    var keyboardResult: String { text("Open the AI Reply keyboard and tap the response to review and insert it.", "Откройте клавиатуру AI Reply и нажмите на ответ, чтобы проверить и вставить его.", "AI Reply пернетақтасын ашып, жауапты қарап, қою үшін оны басыңыз.", "AI Reply klaviaturasini ochib, javobni tekshirish va qoʻyish uchun ustiga bosing.") }
    var privacy: String { text("Your speech is sent through AI Reply to OpenAI (USA) for transcription. Your message and instruction use your usual AI Reply plan. AI Reply does not save audio. OpenAI processes data under its API data policy; temporary retention may apply. You can withdraw voice consent in Settings → Privacy.", "Ваша речь передаётся через AI Reply в OpenAI (США) для распознавания. Сообщение и инструкция обрабатываются по вашему тарифу AI Reply. AI Reply не сохраняет аудио. OpenAI обрабатывает данные по своей политике API; возможно временное хранение. Согласие на голос можно отозвать: Настройки → Конфиденциальность.", "Сөзіңіз AI Reply арқылы OpenAI-ға (АҚШ) тану үшін жіберіледі. Хабарлама мен нұсқау AI Reply тарифіңіз бойынша өңделеді. AI Reply аудионы сақтамайды. OpenAI деректерді API саясаты бойынша өңдейді; уақытша сақтау болуы мүмкін. Дауысқа келісімді Баптаулар → Құпиялық бөлімінде қайтаруға болады.", "Nutqingiz AI Reply orqali OpenAIʼga (AQSh) tanish uchun yuboriladi. Xabar va koʻrsatma AI Reply tarifingiz boʻyicha ishlanadi. AI Reply audioni saqlamaydi. OpenAI API siyosatiga koʻra maʼlumotlarni qayta ishlaydi; vaqtinchalik saqlash mumkin. Ovoz roziligini Sozlamalar → Maxfiylikda bekor qilish mumkin.") }
    var allowAndStart: String { text("Allow and start", "Разрешить и начать", "Рұқсат беріп, бастау", "Ruxsat berib boshlash") }
    var notNow: String { text("Not now", "Не сейчас", "Қазір емес", "Hozir emas") }
    var privacyLink: String { text("Privacy policy", "Политика конфиденциальности", "Құпиялық саясаты", "Maxfiylik siyosati") }
    var quickAccess: String { text("Quick access", "Быстрый запуск", "Жылдам іске қосу", "Tez ishga tushirish") }
    var onboarding: String { text("Set up voice once in AI Reply: allow microphone access and voice processing, then enable Live Activities in iPhone Settings. On iOS 18+, open Control Center → hold an empty area → Add a Control → AI Reply Voice. After setup, this control starts recording without opening the app. iOS may ask you to unlock the phone. You can also choose the AI Reply Voice shortcut for the Action Button. On iOS 17, start here or use the Open AI Reply Voice action in Shortcuts.", "Один раз настройте голос в AI Reply: разрешите микрофон и обработку голоса, затем включите Live Activities в настройках iPhone. На iOS 18+ откройте Пункт управления → удерживайте свободное место → Добавить элемент управления → AI Reply Voice. После настройки элемент запускает запись без открытия приложения. iOS может попросить разблокировать телефон. Команду AI Reply Voice можно назначить кнопке действия. На iOS 17 запускайте здесь или через действие «Открыть AI Reply Voice» в «Командах».", "AI Reply ішінде дауысты бір рет баптаңыз: микрофон мен дауысты өңдеуге рұқсат беріп, iPhone баптауларында Live Activities қосыңыз. iOS 18+ нұсқасында Басқару орталығын ашыңыз → бос жерді ұстап тұрыңыз → басқару элементін қосу → AI Reply Voice. Баптаудан кейін ол қолданбаны ашпай жазуды бастайды. iOS телефонды ашуды сұрауы мүмкін. AI Reply Voice пәрменін әрекет түймесіне тағайындауға болады. iOS 17-де осы жерден немесе «Пәрмендердегі» AI Reply Voice ашу әрекеті арқылы бастаңыз.", "AI Reply’da ovozni bir marta sozlang: mikrofon va ovozni qayta ishlashga ruxsat bering, so‘ng iPhone sozlamalarida Live Activities’ni yoqing. iOS 18+ versiyasida Boshqaruv markazini oching → bo‘sh joyni bosib turing → boshqaruv qo‘shish → AI Reply Voice. Sozlangach, element ilovani ochmasdan yozishni boshlaydi. iOS telefonni qulfdan chiqarishni so‘rashi mumkin. AI Reply Voice buyrug‘ini harakat tugmasiga ham biriktirish mumkin. iOS 17’da shu yerdan yoki Buyruqlardagi AI Reply Voice’ni ochish amali orqali boshlang.") }
    var interrupted: String { text("Recording interrupted. Start a new voice session.", "Запись прервана. Запустите новый голосовой сеанс.", "Жазу үзілді. Жаңа дауыс сеансын бастаңыз.", "Yozish toʻxtatildi. Yangi ovoz seansini boshlang.") }
    var signIn: String { text("Sign in and accept the current terms in AI Reply.", "Войдите и примите актуальные условия AI Reply.", "AI Reply-ға кіріп, қолданыстағы шарттарды қабылдаңыз.", "AI Replyʼga kirib, amaldagi shartlarni qabul qiling.") }
    var permission: String { text("Allow microphone access in iPhone Settings → AI Reply.", "Разрешите микрофон: Настройки iPhone → AI Reply.", "iPhone баптауларында → AI Reply → микрофонға рұқсат беріңіз.", "iPhone sozlamalari → AI Reply orqali mikrofonga ruxsat bering.") }
    var unavailable: String { text("Voice transcription is unavailable on this server.", "На сервере пока недоступно распознавание голоса.", "Бұл серверде дауысты тану әзірге қолжетімсіз.", "Bu serverda nutqni tanish hozircha mavjud emas.") }
    var liveActivityRequired: String { text("Enable Live Activities for AI Reply in iPhone Settings to record from Control Center.", "Включите Live Activities для AI Reply в настройках iPhone, чтобы записывать из Пункта управления.", "Басқару орталығынан жазу үшін iPhone баптауларында AI Reply Live Activities мүмкіндігін қосыңыз.", "Boshqaruv markazidan yozish uchun iPhone sozlamalarida AI Reply Live Activities imkoniyatini yoqing.") }
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
