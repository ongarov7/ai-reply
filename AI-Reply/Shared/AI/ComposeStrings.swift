import Foundation

/// The words of the keyboard's "Create" mode, by APP language - the same rule
/// as `AIReplyStrings`: product text follows the language the user picked in
/// the app, never the keyboard layout they happen to type on.
struct ComposeStrings: Sendable {

    /// VoiceOver for the toolbar's ✨ button next to "+", which is an icon only.
    let createButtonAccessibility: String
    /// Header of the Create panel.
    let title: String
    /// Placeholder of the instruction field: what to type, with an example.
    let placeholder: String
    /// The primary button: write the message.
    let write: String
    /// Clears the instruction and every version.
    let newDraft: String
    let newDraftAccessibility: String
    /// VoiceOver for Back on the result: return to the instruction.
    let editRequest: String
    /// VoiceOver for the result field.
    let draftTitle: String

    // Errors
    let noInstruction: String
    /// Without Full Access the keyboard has no network.
    let fullAccessRequired: String
    /// Takes the server's limit.
    let instructionTooLongFormat: String

    /// One-tap additions to the instruction: occasions and tones. Optional;
    /// the free-form instruction is the interface.
    let intents: [QuickIntent]

    func instructionTooLong(limit: Int) -> String {
        String(format: instructionTooLongFormat, limit)
    }

    static func forLanguage(_ language: AppLanguage) -> ComposeStrings {
        switch language {
        case .english: return .english
        case .russian: return .russian
        case .kazakh:  return .kazakh
        case .uzbek:   return .uzbek
        }
    }

    private static let english = ComposeStrings(
        createButtonAccessibility: "Write a new message with AI",
        title: "Write with AI",
        placeholder: "Describe the message, e.g. “Congratulate our director on his 55th birthday”",
        write: "Write",
        newDraft: "New",
        newDraftAccessibility: "Start over",
        editRequest: "Edit request",
        draftTitle: "Your message",
        noInstruction: "Describe what to write first.",
        fullAccessRequired: "Turn on Allow Full Access for this keyboard in iOS Settings: writing with AI needs the internet.",
        instructionTooLongFormat: "The request is too long. Use up to %d characters.",
        intents: [
            QuickIntent(id: "congratulate", label: "Congratulate", phrase: "Write a congratulation."),
            QuickIntent(id: "short", label: "Short", phrase: "Keep it short."),
            QuickIntent(id: "formal", label: "Formal", phrase: "Make it formal."),
            QuickIntent(id: "friendly", label: "Friendly", phrase: "Warm and friendly."),
            QuickIntent(id: "emoji", label: "Add emoji", phrase: "Add a few fitting emoji."),
            QuickIntent(id: "thanks", label: "Thank you", phrase: "Write a thank-you message."),
            QuickIntent(id: "decline", label: "Decline politely", phrase: "Politely decline.")
        ]
    )

    private static let russian = ComposeStrings(
        createButtonAccessibility: "Написать новое сообщение с AI",
        title: "Написать с AI",
        placeholder: "Опишите сообщение, например: «Поздравь директора с 55-летием»",
        write: "Написать",
        newDraft: "Новое",
        newDraftAccessibility: "Начать заново",
        editRequest: "Изменить запрос",
        draftTitle: "Ваше сообщение",
        noInstruction: "Сначала опишите, что написать.",
        fullAccessRequired: "Включите полный доступ для клавиатуры в настройках iOS: для работы AI нужен интернет.",
        instructionTooLongFormat: "Запрос слишком длинный. Не более %d символов.",
        intents: [
            QuickIntent(id: "congratulate", label: "Поздравить", phrase: "Напиши поздравление."),
            QuickIntent(id: "short", label: "Коротко", phrase: "Коротко."),
            QuickIntent(id: "formal", label: "Официально", phrase: "Официальным тоном."),
            QuickIntent(id: "friendly", label: "Дружелюбно", phrase: "Тепло и дружелюбно."),
            QuickIntent(id: "emoji", label: "С эмодзи", phrase: "Добавь несколько уместных эмодзи."),
            QuickIntent(id: "thanks", label: "Поблагодарить", phrase: "Напиши благодарность."),
            QuickIntent(id: "decline", label: "Вежливый отказ", phrase: "Напиши вежливый отказ.")
        ]
    )

    private static let kazakh = ComposeStrings(
        createButtonAccessibility: "AI арқылы жаңа хабарлама жазу",
        title: "AI-мен жазу",
        placeholder: "Хабарламаны сипаттаңыз, мысалы: «Директорды 55 жасқа толуымен құттықта»",
        write: "Жазу",
        newDraft: "Жаңа",
        newDraftAccessibility: "Басынан бастау",
        editRequest: "Сұранысты өзгерту",
        draftTitle: "Сіздің хабарламаңыз",
        noInstruction: "Алдымен не жазу керегін сипаттаңыз.",
        fullAccessRequired: "iOS баптауларында пернетақтаға толық рұқсат беріңіз: AI-ға интернет керек.",
        instructionTooLongFormat: "Сұраныс тым ұзын. %d таңбадан аспасын.",
        intents: [
            QuickIntent(id: "congratulate", label: "Құттықтау", phrase: "Құттықтау жаз."),
            QuickIntent(id: "short", label: "Қысқа", phrase: "Қысқа жаз."),
            QuickIntent(id: "formal", label: "Ресми", phrase: "Ресми үнмен жаз."),
            QuickIntent(id: "friendly", label: "Жылы", phrase: "Жылы, достық үнмен жаз."),
            QuickIntent(id: "emoji", label: "Эмодзимен", phrase: "Бірнеше орынды эмодзи қос."),
            QuickIntent(id: "thanks", label: "Алғыс айту", phrase: "Алғыс хат жаз."),
            QuickIntent(id: "decline", label: "Сыпайы бас тарту", phrase: "Сыпайы түрде бас тарт.")
        ]
    )

    private static let uzbek = ComposeStrings(
        createButtonAccessibility: "AI bilan yangi xabar yozish",
        title: "AI bilan yozish",
        placeholder: "Xabarni tasvirlang, masalan: «Direktorni 55 yoshi bilan tabrikla»",
        write: "Yozish",
        newDraft: "Yangi",
        newDraftAccessibility: "Boshidan boshlash",
        editRequest: "So‘rovni o‘zgartirish",
        draftTitle: "Xabaringiz",
        noInstruction: "Avval nima yozishni tasvirlang.",
        fullAccessRequired: "iOS sozlamalarida klaviaturaga to‘liq ruxsat bering: AI uchun internet kerak.",
        instructionTooLongFormat: "So‘rov juda uzun. %d belgigacha yozing.",
        intents: [
            QuickIntent(id: "congratulate", label: "Tabriklash", phrase: "Tabrik yoz."),
            QuickIntent(id: "short", label: "Qisqa", phrase: "Qisqa yoz."),
            QuickIntent(id: "formal", label: "Rasmiy", phrase: "Rasmiy ohangda yoz."),
            QuickIntent(id: "friendly", label: "Do‘stona", phrase: "Iliq va do‘stona yoz."),
            QuickIntent(id: "emoji", label: "Emoji bilan", phrase: "Bir nechta mos emoji qo‘sh."),
            QuickIntent(id: "thanks", label: "Minnatdorchilik", phrase: "Minnatdorchilik xabarini yoz."),
            QuickIntent(id: "decline", label: "Muloyim rad", phrase: "Muloyim rad javobini yoz.")
        ]
    )
}
