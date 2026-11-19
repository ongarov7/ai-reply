import Foundation

/// The keyboard's screenshot words, in the APP language (the keyboard has no
/// string catalog of its own; see `AIReplyStrings`).
struct ScreenshotKeyboardStrings {
    let language: AppLanguage

    init(_ language: AppLanguage) { self.language = language }

    private func text(_ en: String, _ ru: String, _ kk: String, _ uz: String) -> String {
        switch language {
        case .english: return en
        case .russian: return ru
        case .kazakh:  return kk
        case .uzbek:   return uz
        }
    }

    /// "Screenshot · WhatsApp · 6 messages".
    func ready(app: String?, messages: Int) -> String {
        let name = text("Screenshot", "Скриншот", "Скриншот", "Skrinshot")
        let count = text("messages: \(messages)", "сообщений: \(messages)", "хабарлама: \(messages)", "xabar: \(messages)")
        return [name, app, count].compactMap { $0 }.joined(separator: " · ")
    }

    func draftReady(_ preview: String) -> String {
        text("Reply from screenshot", "Ответ по скриншоту", "Скриншот бойынша жауап", "Skrinshot boʻyicha javob")
            + ": " + preview
    }

    var noText: String {
        text("No chat text found in the screenshot", "На скриншоте не найден текст чата",
             "Скриншоттан чат мәтіні табылмады", "Skrinshotda chat matni topilmadi")
    }
    var reply: String { text("Reply", "Ответить", "Жауап беру", "Javob berish") }
    var insert: String { text("Insert", "Вставить", "Кірістіру", "Kiritish") }
    var dismiss: String { text("Hide screenshot", "Скрыть скриншот", "Скриншотты жасыру", "Skrinshotni yashirish") }
    var fullAccess: String {
        text("Allow Full Access in the keyboard settings to answer a screenshot.",
             "Разрешите полный доступ в настройках клавиатуры, чтобы ответить по скриншоту.",
             "Скриншотқа жауап беру үшін пернетақта баптауларында толық рұқсат беріңіз.",
             "Skrinshotga javob berish uchun klaviatura sozlamalarida toʻliq ruxsat bering.")
    }

    /// The composer's line when a screenshot reply may cost more than one.
    func costConfirmation(maxUnits: Int) -> String {
        text("This screenshot reply may use up to \(maxUnits) replies (the price follows the real AI processing). Tap Try again to confirm.",
             "Этот ответ по скриншоту может списать до \(maxUnits) ответов (цена — по фактической обработке ИИ). Нажмите «Повторить», чтобы подтвердить.",
             "Бұл скриншот жауабы \(maxUnits) жауапқа дейін жұмсауы мүмкін (баға — AI-дің нақты өңдеуі бойынша). Растау үшін «Қайталау» басыңыз.",
             "Bu skrinshot javobi \(maxUnits) tagacha javob sarflashi mumkin (narx — AI’ning haqiqiy ishlovi boʻyicha). Tasdiqlash uchun «Qayta urinish»ni bosing.")
    }

    /// AI Reply Voice heard the instruction, but the reply may cost more
    /// than one: the screenshot strip asks before anything is charged.
    func voiceNeedsConfirmation(maxUnits: Int) -> String {
        text("This reply may use up to \(maxUnits) replies. Tap Reply on the screenshot strip to confirm; your instruction is kept.",
             "Этот ответ может списать до \(maxUnits) ответов. Нажмите «Ответить» на полоске скриншота, чтобы подтвердить; инструкция сохранена.",
             "Бұл жауап \(maxUnits) жауапқа дейін жұмсауы мүмкін. Растау үшін скриншот жолағында «Жауап беру» басыңыз; нұсқау сақталды.",
             "Bu javob \(maxUnits) tagacha javob sarflashi mumkin. Tasdiqlash uchun skrinshot qatorida «Javob berish»ni bosing; koʻrsatma saqlandi.")
    }

    func costTooHigh(maxUnits: Int, available: Int) -> String {
        text("This screenshot reply may need up to \(maxUnits) replies, and \(available) are left. Shorten the instruction or open AI Reply to buy more.",
             "Этому ответу по скриншоту может понадобиться до \(maxUnits) ответов, а осталось \(available). Сократите инструкцию или откройте AI Reply, чтобы докупить.",
             "Бұл скриншот жауабына \(maxUnits) жауапқа дейін қажет болуы мүмкін, ал \(available) қалды. Нұсқауды қысқартыңыз немесе AI Reply ашып, сатып алыңыз.",
             "Bu skrinshot javobiga \(maxUnits) tagacha javob kerak boʻlishi mumkin, qolgani esa \(available). Koʻrsatmani qisqartiring yoki AI Reply’ni ochib, sotib oling.")
    }
}
