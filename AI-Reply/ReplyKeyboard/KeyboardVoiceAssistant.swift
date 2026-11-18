import UIKit

enum KeyboardVoiceAssistantAction: Equatable {
    case pasteContext, finish, pause, stop, review, copy, regenerate
}

/// The keyboard only exchanges protected text and control requests. It never
/// captures audio, opens an app, or inserts a generated response on its own.
@MainActor
final class KeyboardVoiceAssistant {
    var onChange: ((VoiceAssistantSnapshot?) -> Void)?
    var onReview: ((VoiceAssistantSnapshot) -> Void)?
    var onRegenerate: ((VoiceAssistantSnapshot) -> Void)?
    var onInsert: ((VoiceAssistantSnapshot) -> Void)?
    var onControls: ((VoiceAssistantSnapshot) -> Void)?
    var onNotice: ((String) -> Void)?
    var readSource: (() -> Result<ReplyContext, AIReplyError>)?
    var selectedTemplateID: (() -> String?)?
    var language: (() -> AppLanguage)?
    var hasFullAccess: (() -> Bool)?

    private let store: VoiceAssistantStore
    private var observer: VoiceAssistantSignalObserver?
    private var refreshTask: Task<Void, Never>?
    private var visible = false
    private var sessionID = UUID().uuidString
    private var dismissedRevision: String?
    private var renderedRevision: String?
    private var consumedRequestIDs = Set<String>()
    private(set) var snapshot: VoiceAssistantSnapshot?

    init(store: VoiceAssistantStore = .shared) { self.store = store }

    func appear() {
        // A reused controller can reappear in another chat without a field
        // change callback. Each appearance needs a fresh insertion binding.
        sessionID = UUID().uuidString
        visible = true
        observer = VoiceAssistantSignalObserver(signals: [.snapshot]) { [weak self] _ in
            Task { @MainActor in self?.refresh() }
        }
        refresh()
    }

    func disappear() {
        visible = false
        observer = nil
        refreshTask?.cancel()
        refreshTask = nil
    }

    func fieldChanged() {
        sessionID = UUID().uuidString
        // A response for the preceding field remains available for explicit
        // review, but insertion into this field requires confirmation.
        refresh()
    }

    func refresh() {
        guard visible else { return }
        let stored = hasFullAccess?() == true ? store.readSnapshot() : nil
        let value = stored?.isVisibleInKeyboard() == true ? stored : nil
        snapshot = value
        let hidden = value.map { revision($0) == dismissedRevision || $0.requestID.map { consumedRequestIDs.contains($0) } == true } ?? false
        let shown = hidden ? nil : value
        let nextRevision = shown?.keyboardRenderRevision()
        if nextRevision != renderedRevision {
            renderedRevision = nextRevision
            onChange?(shown)
        }
        guard value != nil, refreshTask == nil else { return }
        refreshTask = Task { [weak self] in
            // A signal can be coalesced. Re-read only during a finite active
            // task, never on a keystroke or while the keyboard is dismissed.
            while !Task.isCancelled {
                let delay = self?.snapshot.flatMap { snapshot -> TimeInterval? in
                    guard snapshot.phase == .error else { return nil }
                    return max(0.01, min(2, snapshot.updatedAt.addingTimeInterval(10).timeIntervalSinceNow))
                } ?? 2
                try? await Task.sleep(for: .seconds(delay))
                guard let self, self.visible, self.store.readSnapshot()?.isVisibleInKeyboard() == true else { break }
                self.refresh()
            }
            self?.refreshTask = nil
            self?.refresh()
        }
    }

    func dismiss() {
        dismissedRevision = snapshot.map(revision)
        renderedRevision = nil
        onChange?(nil)
    }

    private func revision(_ snapshot: VoiceAssistantSnapshot) -> String {
        snapshot.sessionID + ":" + snapshot.phase.rawValue + ":" + (snapshot.requestID ?? "")
    }

    func didInsert(requestID: String?) {
        discardOriginalResult(requestID: requestID)
    }

    func discardOriginalResult(requestID: String?) {
        if let requestID { consumedRequestIDs.insert(requestID) }
        refresh()
    }

    func review() {
        guard let snapshot, snapshot.readyResponse() != nil else { return }
        onReview?(snapshot)
    }

    func insert() {
        guard let snapshot, snapshot.readyResponse() != nil else { return }
        if snapshot.keyboardSessionID == sessionID {
            if let id = snapshot.requestID { consumedRequestIDs.insert(id) }
            onInsert?(snapshot)
            onChange?(nil)
        } else {
            // The existing inline composer shows the source and result. Its
            // explicit Insert is the second step for an unbound conversation.
            onReview?(snapshot)
        }
    }

    func menu() {
        refresh()
        guard hasFullAccess?() == true else {
            onNotice?(KeyboardVoiceStrings(language?() ?? .english).fullAccess)
            return
        }
        guard let snapshot, snapshot.showsMicrophoneInKeyboard() else { return }
        onControls?(snapshot)
    }

    func perform(_ action: KeyboardVoiceAssistantAction) {
        switch action {
        case .pasteContext:
            requestWithCopiedMessage()
        case .finish: send(.finish)
        case .pause: send(.pause)
        case .stop: send(.stop)
        case .review: review()
        case .copy:
            guard let snapshot, let response = snapshot.readyResponse() else { return }
            UIPasteboard.general.string = response
            onNotice?(KeyboardVoiceStrings(language?() ?? .english).copied)
        case .regenerate:
            guard let snapshot, snapshot.readyResponse() != nil else { return }
            // The extension's existing composer can generate while the main
            // app is suspended; a mailbox notification cannot wake it.
            onRegenerate?(snapshot)
        }
    }

    private func requestWithCopiedMessage() {
        // Only this explicit inline action reads the pasteboard.
        guard let result = readSource?() else { return }
        switch result {
        case .success(let source): send(.request, source: source.text)
        case .failure(let error):
            onNotice?(AIReplyStrings.forLanguage(language?() ?? .english).keyboardMessage(for: error))
        }
    }

    private func send(_ kind: VoiceAssistantCommand.Kind, source: String? = nil) {
        guard let snapshot = store.readSnapshot() else { return }
        _ = store.send(VoiceAssistantCommand(kind: kind, sessionID: snapshot.sessionID,
            keyboardSessionID: kind == .regenerate ? snapshot.keyboardSessionID : sessionID, sourceMessage: source,
            templateID: selectedTemplateID?(), requestID: snapshot.requestID))
    }


}

struct KeyboardVoiceStrings {
    let language: AppLanguage
    init(_ language: AppLanguage) { self.language = language }
    private func text(_ en: String, _ ru: String, _ kk: String, _ uz: String) -> String {
        switch language { case .english: return en; case .russian: return ru; case .kazakh: return kk; case .uzbek: return uz }
    }
    var name: String { "AI Reply Voice" }
    var speak: String { text("Speak…", "Говорите…", "Айтыңыз…", "Gapiring…") }
    var pasteShort: String { text("Reply to copied message", "Ответить на скопированное", "Көшірілгенге жауап беру", "Nusxalangan xabarga javob") }
    var finishShort: String { text("Done", "Готово", "Дайын", "Tayyor") }
    var pauseShort: String { text("Pause", "Пауза", "Кідірту", "Pauza") }
    var stopShort: String { text("Cancel", "Отмена", "Бас тарту", "Bekor qilish") }
    var reviewShort: String { text("Review", "Смотреть", "Қарау", "Ko‘rish") }
    var copyShort: String { text("Copy", "Копировать", "Көшіру", "Nusxalash") }
    var againShort: String { text("Again", "Ещё", "Тағы", "Yana") }
    var copied: String { text("Response copied", "Ответ скопирован", "Жауап көшірілді", "Javob nusxalandi") }
    var finishDraftFirst: String { text("Close the current draft to review the voice response.", "Закройте текущий черновик, чтобы посмотреть голосовой ответ.", "Дауыс жауабын қарау үшін ағымдағы нобайды жабыңыз.", "Ovozli javobni ko‘rish uchun joriy qoralamani yoping.") }
    var insert: String { text("Insert", "Вставить", "Кірістіру", "Kiritish") }
    var review: String { text("Review and edit", "Посмотреть и изменить", "Көру және өзгерту", "Ko‘rish va tahrirlash") }
    var copy: String { text("Copy response", "Копировать ответ", "Жауапты көшіру", "Javobni nusxalash") }
    var regenerate: String { text("Regenerate", "Повторить генерацию", "Қайта жасау", "Qayta yaratish") }
    var cancel: String { text("Close", "Закрыть", "Жабу", "Yopish") }
    var finish: String { text("Finish and generate", "Закончить и создать ответ", "Аяқтау және жауап жасау", "Tugatish va javob yaratish") }
    var pause: String { text("Pause recording", "Приостановить запись", "Жазуды кідірту", "Yozishni pauza qilish") }
    var stop: String { text("Stop voice session", "Остановить голосовой сеанс", "Дауыс сеансын тоқтату", "Ovoz seansini to‘xtatish") }
    var useCopiedMessage: String { text("Use copied message as context", "Использовать скопированное сообщение", "Көшірілген хабарламаны пайдалану", "Nusxalangan xabardan foydalanish") }
    var startInstructions: String { text("Start AI Reply Voice from Control Center or the AI Reply app. The keyboard cannot turn on the microphone.", "Запустите AI Reply Voice из Пункта управления или приложения AI Reply. Клавиатура не может включать микрофон.", "AI Reply Voice қызметін Басқару орталығынан немесе AI Reply қолданбасынан іске қосыңыз. Пернетақта микрофонды қоса алмайды.", "AI Reply Voice’ni Boshqaruv markazi yoki AI Reply ilovasidan ishga tushiring. Klaviatura mikrofonni yoqa olmaydi.") }
    var fullAccess: String { text("Allow Full Access in iOS keyboard settings to receive voice responses. Normal typing is still available.", "Разрешите полный доступ в настройках клавиатуры iOS, чтобы получать голосовые ответы. Обычный ввод доступен.", "Дауыс жауаптарын алу үшін iOS пернетақта баптауларында толық рұқсат беріңіз. Қалыпты теру қолжетімді.", "Ovozli javoblarni olish uchun iOS klaviatura sozlamalarida to‘liq kirishga ruxsat bering. Oddiy yozish mavjud.") }
    var confirmConversation: String { text("This response may belong to a different conversation. Review it before inserting into the current chat.", "Этот ответ может относиться к другому чату. Проверьте его перед вставкой в текущий разговор.", "Бұл жауап басқа чатқа қатысты болуы мүмкін. Ағымдағы чатқа кірістірмес бұрын тексеріңіз.", "Bu javob boshqa suhbatga tegishli bo‘lishi mumkin. Joriy chatga kiritishdan oldin tekshiring.") }
    var confirmReplaceDraft: String { text("Replace the draft currently open in the keyboard with the voice response?", "Заменить текущий черновик в клавиатуре голосовым ответом?", "Пернетақтадағы ағымдағы нобайды дауыс жауабымен ауыстыру керек пе?", "Klaviaturada ochiq qoralama ovozli javob bilan almashtirilsinmi?") }
    func status(_ phase: VoiceAssistantPhase) -> String {
        switch phase {
        case .ready: return text("Microphone ready", "Микрофон готов", "Микрофон дайын", "Mikrofon tayyor")
        case .listening: return text("Listening…", "Слушаю…", "Тыңдап тұрмын…", "Tinglayapman…")
        case .speechDetected: return text("Speech detected", "Речь обнаружена", "Сөйлеу анықталды", "Nutq aniqlandi")
        case .transcribing: return text("Transcribing…", "Распознаю речь…", "Сөз танылуда…", "Nutq tanilmoqda…")
        case .generating: return text("Generating response…", "Создаю ответ…", "Жауап жасалуда…", "Javob yaratilmoqda…")
        case .responseReady: return text("Response ready", "Ответ готов", "Жауап дайын", "Javob tayyor")
        case .paused: return text("Recording paused. Resume in AI Reply.", "Запись приостановлена. Продолжите в AI Reply.", "Жазу кідіртілді. AI Reply ішінде жалғастырыңыз.", "Yozish pauzada. AI Reply’da davom eting.")
        case .stopped: return text("Recording stopped", "Запись остановлена", "Жазу тоқтатылды", "Yozish to‘xtatildi")
        case .error: return text("Voice session interrupted. Open AI Reply.", "Голосовой сеанс прерван. Откройте AI Reply.", "Дауыс сеансы үзілді. AI Reply ашыңыз.", "Ovoz seansi uzildi. AI Reply’ni oching.")
        }
    }
}
