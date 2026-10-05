import Foundation

/// Every user-visible string the AI reply flow needs, keyed by the APP's
/// interface language.
///
/// WHICH LANGUAGE, AND WHY. There are two languages in this product and they
/// answer different questions:
///
/// * the KEYBOARD LAYOUT decides what the character keys type, and what the
///   space and return keys are called - that is `KeyboardLabels`;
/// * the APP LANGUAGE decides what the PRODUCT says - template chips, Insert,
///   Regenerate, statuses and errors - which is this type.
///
/// They are deliberately not the same. A Kazakh user typing on the English
/// layout to write a Russian reply must still see "Кірістіру", because the
/// product is theirs and the layout is just a keyboard. Keying these strings to
/// the layout is exactly the bug this file used to have: the app was in Kazakh
/// and the chips read Friend / Client / Business / Work.
///
/// Resolved from a table rather than `NSLocalizedString` because the keyboard
/// extension has to show the language the user PICKED IN THE APP, which iOS
/// bundle lookup knows nothing about.
/// A one-tap phrasing of a reply INSTRUCTION.
///
/// Quick intents answer "how should I reply", which is a different question
/// from the one `ReplyTemplate` answers ("who am I replying to"). They are not
/// stored per user and never become saved configuration: tapping one writes its
/// `phrase` into the instruction field, where it can be edited, combined with
/// another intent, or deleted. The source message is never touched.
///
/// Phrases never carry the sender's gender («согласен» / «согласна»): the
/// same pill serves everyone, and the reply's own wording follows the
/// profile instead.
struct QuickIntent: Sendable, Equatable, Identifiable {
    let id: String
    /// What the pill says. Short enough to fit a keyboard-width row.
    let label: String
    /// What is written into the instruction field, in the app's language.
    let phrase: String
}

struct AIReplyStrings: Sendable {

    // Actions
    let generate: String
    let regenerate: String
    let insert: String
    let cancel: String
    let replaceExisting: String
    let appendToExisting: String
    let keepTyping: String
    /// Explicit, user-initiated clipboard read inside the composer.
    let pasteMessage: String
    let clearSource: String
    let back: String
    let editReply: String
    /// Shown on the primary button after a failure, so one tap retries with
    /// everything the user typed still there.
    let retry: String
    /// Title of the overflow menu that holds the quick intents which did not
    /// fit the row at this width in this language.
    let moreActions: String
    /// Expand / collapse the quoted source message.
    let showFullMessage: String
    let hideFullMessage: String
    /// Cancels a generation that is still running.
    let stop: String
    /// Leaves edit mode on the reply.
    let doneEditing: String
    /// Moving between the versions Regenerate produced.
    let previousVersion: String
    let nextVersion: String
    /// VoiceOver: "Version 2 of 3". Takes the position and the count.
    let versionPositionFormat: String
    /// VoiceOver for the character counter: takes the count and the limit.
    let characterCountFormat: String

    // Status
    let generating: String
    let draftTitle: String
    let sourceTitle: String
    let chooseTemplate: String
    /// Caption over the quoted source message.
    let copiedMessage: String
    /// Placeholder of the reply-instruction field. This is the sentence that
    /// tells the user the field is for what THEY want said, not for the reply.
    let instructionPlaceholder: String

    // Errors
    let noSourceMessage: String
    /// Takes the limit, which the server publishes and an administrator can
    /// change: the sentence must never name a number the server no longer uses.
    let messageTooLongFormat: String
    /// Without Full Access a keyboard can neither read the clipboard nor reach
    /// the network, so this covers both - and is said before any request.
    let fullAccessRequired: String
    let notConfigured: String
    let offline: String
    let timedOut: String
    let authenticationFailed: String
    let rateLimited: String
    let emptyResponse: String
    let serviceUnavailable: String
    /// Backend mode only: the session is gone, and a keyboard extension cannot
    /// sign anybody in, so it says where to do it.
    let signInRequired: String
    /// Backend mode only: the plan's daily generations are spent.
    let quotaExhausted: String

    // Host-field conflict
    let hostFieldNotEmpty: String

    // Quick intents
    let quickIntents: [QuickIntent]

    /// Which table this is.
    let appLanguage: AppLanguage

    /// The "Create" mode: writing a new message from a description.
    var compose: ComposeStrings { ComposeStrings.forLanguage(appLanguage) }

    /// The suggestion strip and the instruction suggestion.
    var typing: TypingStrings { TypingStrings.forLanguage(appLanguage) }

    static func forLanguage(_ language: AppLanguage) -> AIReplyStrings {
        switch language {
        case .english: return .english
        case .russian: return .russian
        case .kazakh:  return .kazakh
        case .uzbek:   return .uzbek
        }
    }

    /// Maps an error onto the sentence the user actually sees. Short, plain,
    /// actionable, and free of status codes, JSON and provider names.
    func message(for error: AIReplyError) -> String {
        switch error {
        case .noSourceMessage:            return noSourceMessage
        case .messageTooLong(let limit):  return messageTooLong(limit: limit)
        case .fullAccessRequired:         return fullAccessRequired
        case .notConfigured:              return notConfigured
        case .offline:                    return offline
        case .timedOut:                   return timedOut
        case .cancelled:                  return ""
        case .authenticationFailed:       return signInRequired
        case .rateLimited:                return rateLimited
        case .quotaExhausted:             return quotaExhausted
        case .emptyResponse:              return emptyResponse
        case .serviceUnavailable:         return serviceUnavailable
        case .noInstruction:              return compose.noInstruction
        case .instructionTooLong(let limit): return compose.instructionTooLong(limit: limit)
        }
    }

    func messageTooLong(limit: Int) -> String {
        String(format: messageTooLongFormat, limit)
    }

    func versionPosition(_ position: Int, of count: Int) -> String {
        String(format: versionPositionFormat, position, count)
    }

    func characterCount(_ count: Int, limit: Int) -> String {
        String(format: characterCountFormat, count, limit)
    }

    private static let english = AIReplyStrings(
        generate: "Reply",
        regenerate: "Regenerate",
        insert: "Insert",
        cancel: "Cancel",
        replaceExisting: "Replace",
        appendToExisting: "Add",
        keepTyping: "Cancel",
        pasteMessage: "Paste message",
        clearSource: "Clear message",
        back: "Back",
        editReply: "Edit reply",
        retry: "Try again",
        moreActions: "More",
        showFullMessage: "Show full message",
        hideFullMessage: "Collapse message",
        stop: "Stop",
        doneEditing: "Done",
        previousVersion: "Previous version",
        nextVersion: "Next version",
        versionPositionFormat: "Version %d of %d",
        characterCountFormat: "%d of %d characters",
        generating: "Generating…",
        draftTitle: "Your reply",
        sourceTitle: "Reply to",
        chooseTemplate: "Choose who you are replying to",
        copiedMessage: "Copied message",
        instructionPlaceholder: "How should I reply?",
        noSourceMessage: "Copy a message first",
        messageTooLongFormat: "Message is too long. Copy up to %d characters.",
        fullAccessRequired: "Turn on Full Access for AI Reply in iOS Settings to read copied text and get replies.",
        notConfigured: "Open the AI Reply app and finish setup first.",
        offline: "No internet connection.",
        timedOut: "That took too long. Try again.",
        authenticationFailed: "Your session could not be verified. Sign in again in the AI Reply app.",
        rateLimited: "Too many requests. Wait a moment and try again.",
        emptyResponse: "No reply came back. Try again.",
        serviceUnavailable: "The service is unavailable right now. Try again.",
        signInRequired: "Open the AI Reply app and sign in to keep replying.",
        quotaExhausted: "You have used today's replies. They come back tomorrow, or change your plan in the app.",
        hostFieldNotEmpty: "There is already text in this field.",
        quickIntents: [
            QuickIntent(id: "agree", label: "Agree", phrase: "Reply that I agree."),
            QuickIntent(id: "decline", label: "Decline", phrase: "Decline politely."),
            QuickIntent(id: "details", label: "Ask details", phrase: "Ask for more details."),
            QuickIntent(id: "brief", label: "Briefly", phrase: "Keep the reply short."),
            QuickIntent(id: "professional", label: "Professional", phrase: "Answer professionally."),
            QuickIntent(id: "friendly", label: "Friendly", phrase: "Answer in a warm, friendly way."),
            QuickIntent(id: "thanks", label: "Thank them", phrase: "Thank them."),
            QuickIntent(id: "reschedule", label: "Another time", phrase: "Suggest a different time.")
        ],
        appLanguage: .english
    )

    private static let russian = AIReplyStrings(
        generate: "Ответить",
        regenerate: "Сгенерировать заново",
        insert: "Вставить",
        cancel: "Отмена",
        replaceExisting: "Заменить",
        appendToExisting: "Добавить",
        keepTyping: "Отмена",
        pasteMessage: "Вставить сообщение",
        clearSource: "Очистить сообщение",
        back: "Назад",
        editReply: "Изменить ответ",
        retry: "Повторить",
        moreActions: "Ещё",
        showFullMessage: "Показать полностью",
        hideFullMessage: "Свернуть сообщение",
        stop: "Остановить",
        doneEditing: "Готово",
        previousVersion: "Предыдущий вариант",
        nextVersion: "Следующий вариант",
        versionPositionFormat: "Вариант %d из %d",
        characterCountFormat: "%d из %d символов",
        generating: "Создаю ответ…",
        draftTitle: "Ваш ответ",
        sourceTitle: "Ответ на",
        chooseTemplate: "Выберите, кому вы отвечаете",
        copiedMessage: "Скопированное сообщение",
        instructionPlaceholder: "Как ответить?",
        noSourceMessage: "Сначала скопируйте сообщение",
        messageTooLongFormat: "Сообщение слишком длинное. Скопируйте не более %d символов.",
        fullAccessRequired: "Включите полный доступ для AI Reply в настройках iOS, чтобы читать скопированный текст и получать ответы.",
        notConfigured: "Откройте приложение AI Reply и завершите настройку.",
        offline: "Нет подключения к интернету.",
        timedOut: "Слишком долго. Попробуйте ещё раз.",
        authenticationFailed: "Не удалось проверить сессию. Войдите снова в приложении AI Reply.",
        rateLimited: "Слишком много запросов. Подождите немного.",
        emptyResponse: "Ответ не получен. Попробуйте ещё раз.",
        serviceUnavailable: "Сервис сейчас недоступен. Попробуйте позже.",
        signInRequired: "Откройте приложение AI Reply и войдите, чтобы продолжить.",
        quotaExhausted: "Ответы на сегодня закончились. Они обновятся завтра — или смените тариф в приложении.",
        hostFieldNotEmpty: "В этом поле уже есть текст.",
        quickIntents: [
            QuickIntent(id: "agree", label: "Согласиться", phrase: "Ответь согласием."),
            QuickIntent(id: "decline", label: "Отказать", phrase: "Вежливо откажи."),
            QuickIntent(id: "details", label: "Уточнить", phrase: "Уточни детали."),
            QuickIntent(id: "brief", label: "Коротко", phrase: "Ответь коротко."),
            QuickIntent(id: "professional", label: "По-деловому", phrase: "Ответь по-деловому."),
            QuickIntent(id: "friendly", label: "Дружелюбно", phrase: "Ответь тепло и дружелюбно."),
            QuickIntent(id: "thanks", label: "Поблагодарить", phrase: "Поблагодари."),
            QuickIntent(id: "reschedule", label: "Другое время", phrase: "Предложи другое время.")
        ],
        appLanguage: .russian
    )

    private static let kazakh = AIReplyStrings(
        generate: "Жауап беру",
        regenerate: "Қайта жасау",
        insert: "Кірістіру",
        cancel: "Бас тарту",
        replaceExisting: "Ауыстыру",
        appendToExisting: "Қосу",
        keepTyping: "Бас тарту",
        pasteMessage: "Хабарламаны қою",
        clearSource: "Хабарламаны тазалау",
        back: "Артқа",
        editReply: "Жауапты өңдеу",
        retry: "Қайталау",
        moreActions: "Тағы",
        showFullMessage: "Толық көрсету",
        hideFullMessage: "Хабарламаны жию",
        stop: "Тоқтату",
        doneEditing: "Дайын",
        previousVersion: "Алдыңғы нұсқа",
        nextVersion: "Келесі нұсқа",
        versionPositionFormat: "%d-нұсқа, барлығы %d",
        characterCountFormat: "%d / %d таңба",
        generating: "Жауап дайындалуда…",
        draftTitle: "Сіздің жауабыңыз",
        sourceTitle: "Хабарламаға жауап",
        chooseTemplate: "Кімге жауап беретініңізді таңдаңыз",
        copiedMessage: "Көшірілген хабарлама",
        instructionPlaceholder: "Қалай жауап беру керек?",
        noSourceMessage: "Алдымен хабарламаны көшіріңіз",
        messageTooLongFormat: "Хабарлама тым ұзын. %d таңбаға дейінгі мәтінді көшіріңіз.",
        fullAccessRequired: "Көшірілген мәтінді оқып, жауап алу үшін iOS баптауларында AI Reply пернетақтасына толық рұқсат беріңіз.",
        notConfigured: "AI Reply қолданбасын ашып, баптауды аяқтаңыз.",
        offline: "Интернет байланысы жоқ.",
        timedOut: "Тым ұзаққа созылды. Қайталап көріңіз.",
        authenticationFailed: "Сеанс расталмады. AI Reply қолданбасына қайта кіріңіз.",
        rateLimited: "Сұраныс тым көп. Сәл күте тұрыңыз.",
        emptyResponse: "Жауап келмеді. Қайталап көріңіз.",
        serviceUnavailable: "Қызмет қазір қолжетімсіз. Кейінірек көріңіз.",
        signInRequired: "Жалғастыру үшін AI Reply қолданбасын ашып, кіріңіз.",
        quotaExhausted: "Бүгінгі жауаптар бітті. Ертең жаңарады немесе қолданбадан тарифті ауыстырыңыз.",
        hostFieldNotEmpty: "Бұл өрісте мәтін бар.",
        quickIntents: [
            QuickIntent(id: "agree", label: "Келісу", phrase: "Келісетінімді жаз."),
            QuickIntent(id: "decline", label: "Бас тарту", phrase: "Сыпайы түрде бас тарт."),
            QuickIntent(id: "details", label: "Нақтылау", phrase: "Толығырақ сұра."),
            QuickIntent(id: "brief", label: "Қысқа", phrase: "Қысқа жауап бер."),
            QuickIntent(id: "professional", label: "Іскери", phrase: "Іскери тілмен жауап бер."),
            QuickIntent(id: "friendly", label: "Достық", phrase: "Жылы, достық үнмен жауап бер."),
            QuickIntent(id: "thanks", label: "Алғыс айту", phrase: "Алғыс айт."),
            QuickIntent(id: "reschedule", label: "Басқа уақыт", phrase: "Басқа уақыт ұсын.")
        ],
        appLanguage: .kazakh
    )

    private static let uzbek = AIReplyStrings(
        generate: "Javob berish",
        regenerate: "Qayta yaratish",
        insert: "Kiritish",
        cancel: "Bekor qilish",
        replaceExisting: "Almashtirish",
        appendToExisting: "Qo‘shish",
        keepTyping: "Bekor qilish",
        pasteMessage: "Xabarni joylash",
        clearSource: "Xabarni tozalash",
        back: "Orqaga",
        editReply: "Javobni tahrirlash",
        retry: "Qayta urinish",
        moreActions: "Yana",
        showFullMessage: "To‘liq ko‘rsatish",
        hideFullMessage: "Xabarni yig‘ish",
        stop: "To‘xtatish",
        doneEditing: "Tayyor",
        previousVersion: "Oldingi variant",
        nextVersion: "Keyingi variant",
        versionPositionFormat: "%d-variant, jami %d",
        characterCountFormat: "%d / %d belgi",
        generating: "Javob tayyorlanmoqda…",
        draftTitle: "Javobingiz",
        sourceTitle: "Xabarga javob",
        chooseTemplate: "Kimga javob berayotganingizni tanlang",
        copiedMessage: "Nusxalangan xabar",
        instructionPlaceholder: "Qanday javob beraman?",
        noSourceMessage: "Avval xabarni nusxalang",
        messageTooLongFormat: "Xabar juda uzun. %d belgigacha matnni nusxalang.",
        fullAccessRequired: "Nusxalangan matnni o‘qish va javob olish uchun iOS sozlamalarida AI Reply klaviaturasiga to‘liq ruxsat bering.",
        notConfigured: "AI Reply ilovasini ochib, sozlashni yakunlang.",
        offline: "Internet aloqasi yo‘q.",
        timedOut: "Juda uzoq davom etdi. Qayta urinib ko‘ring.",
        authenticationFailed: "Seansni tekshirib bo‘lmadi. AI Reply ilovasiga qayta kiring.",
        rateLimited: "So‘rovlar juda ko‘p. Biroz kutib qayta urinib ko‘ring.",
        emptyResponse: "Javob kelmadi. Qayta urinib ko‘ring.",
        serviceUnavailable: "Xizmat hozir mavjud emas. Keyinroq urinib ko‘ring.",
        signInRequired: "Davom etish uchun AI Reply ilovasini ochib, tizimga kiring.",
        quotaExhausted: "Bugungi javoblar tugadi. Ular ertaga yangilanadi yoki ilovada tarifni o‘zgartiring.",
        hostFieldNotEmpty: "Bu maydonda matn bor.",
        quickIntents: [
            QuickIntent(id: "agree", label: "Rozilik", phrase: "Rozi ekanimni yoz."),
            QuickIntent(id: "decline", label: "Rad etish", phrase: "Muloyim rad et."),
            QuickIntent(id: "details", label: "Aniqlashtirish", phrase: "Batafsil so‘ra."),
            QuickIntent(id: "brief", label: "Qisqa", phrase: "Qisqa javob ber."),
            QuickIntent(id: "professional", label: "Ishchan", phrase: "Ishchan uslubda javob ber."),
            QuickIntent(id: "friendly", label: "Do‘stona", phrase: "Iliq, do‘stona javob ber."),
            QuickIntent(id: "thanks", label: "Minnatdorchilik", phrase: "Minnatdorchilik bildir."),
            QuickIntent(id: "reschedule", label: "Boshqa vaqt", phrase: "Boshqa vaqt taklif qil.")
        ],
        appLanguage: .uzbek
    )
}

/// The words of the keyboard's typing help, by APP language like every
/// product string: the suggestion strip above the keys (smart correction) and
/// the cleaner version of an instruction offered after a pause.
///
/// The strip itself shows words, not labels; these are what VoiceOver says
/// about them, the quotation marks around the typed word, and the two states
/// of the instruction suggestion.
struct TypingStrings: Sendable {

    /// The typed word in the strip, in the language's own quotation marks:
    /// “…” in English, «…» in Russian, Kazakh and Uzbek - as on Android.
    let typedFormat: String
    /// VoiceOver for the quoted word in the strip: tapping keeps it as typed.
    let keepTypedFormat: String
    /// VoiceOver for the word the next space will put in place of the typed one.
    let correctionFormat: String
    /// VoiceOver for the suggested version of the instruction.
    let polishSuggestionFormat: String
    let polishSuggestionHint: String
    /// The chip after a suggestion was used: puts the user's text back.
    let undo: String
    let undoAccessibility: String

    func typed(_ word: String) -> String { String(format: typedFormat, word) }
    func keepTyped(_ word: String) -> String { String(format: keepTypedFormat, word) }
    func correction(_ word: String) -> String { String(format: correctionFormat, word) }
    func polishSuggestion(_ text: String) -> String { String(format: polishSuggestionFormat, text) }

    static func forLanguage(_ language: AppLanguage) -> TypingStrings {
        switch language {
        case .english: return .english
        case .russian: return .russian
        case .kazakh:  return .kazakh
        case .uzbek:   return .uzbek
        }
    }

    private static let english = TypingStrings(
        typedFormat: "\u{201C}%@\u{201D}",
        keepTypedFormat: "Keep as typed: %@",
        correctionFormat: "Autocorrection: %@",
        polishSuggestionFormat: "Suggested correction: %@",
        polishSuggestionHint: "Replaces your request with this version.",
        undo: "Undo",
        undoAccessibility: "Undo the correction"
    )

    private static let russian = TypingStrings(
        typedFormat: "«%@»",
        keepTypedFormat: "Оставить как есть: %@",
        correctionFormat: "Автоисправление: %@",
        polishSuggestionFormat: "Предлагаемое исправление: %@",
        polishSuggestionHint: "Заменит ваш запрос этим вариантом.",
        undo: "Отменить",
        undoAccessibility: "Отменить исправление"
    )

    private static let kazakh = TypingStrings(
        typedFormat: "«%@»",
        keepTypedFormat: "Жазылғандай қалдыру: %@",
        correctionFormat: "Автотүзету: %@",
        polishSuggestionFormat: "Ұсынылған түзету: %@",
        polishSuggestionHint: "Сұранысыңызды осы нұсқамен ауыстырады.",
        undo: "Қайтару",
        undoAccessibility: "Түзетуді қайтару"
    )

    private static let uzbek = TypingStrings(
        typedFormat: "«%@»",
        keepTypedFormat: "Yozilganicha qoldirish: %@",
        correctionFormat: "Avtotuzatish: %@",
        polishSuggestionFormat: "Taklif etilgan tuzatish: %@",
        polishSuggestionHint: "So‘rovingizni shu variant bilan almashtiradi.",
        undo: "Bekor qilish",
        undoAccessibility: "Tuzatishni bekor qilish"
    )
}
