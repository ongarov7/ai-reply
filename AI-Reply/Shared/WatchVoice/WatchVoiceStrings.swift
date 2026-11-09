import Foundation

/// Everything the keyboard says about Watch Voice, in the APP's language,
/// from a table for the same reason as `AIReplyStrings`: the extension has to
/// follow the language picked in the app, which bundle lookup knows nothing of.
struct WatchVoiceStrings: Sendable {

    let controlName: String
    let waiting: String
    let openWatchApp: String
    let installWatchApp: String
    let connectionLost: String
    let generating: String
    let instructionReceived: String
    let responseReady: String
    let generate: String
    let insertResponse: String
    let dismiss: String
    let fullAccessRequired: String
    let copiedMessageUnreadable: String
    let copiedMessageTooLong: String
    let nothingToRewrite: String
    let instructionTooLong: String
    let notSignedIn: String
    let consentRequired: String
    let offline: String
    let timedOut: String
    let rateLimited: String
    let quotaExhausted: String
    let monthlyQuotaExhausted: String
    let serviceUnavailable: String
    let requestProblem: String

    static func forLanguage(_ language: AppLanguage) -> WatchVoiceStrings {
        switch language {
        case .english: return .english
        case .russian: return .russian
        case .kazakh:  return .kazakh
        case .uzbek:   return .uzbek
        }
    }

    /// The sentence for a failure the composer is not on screen to show.
    func message(for failure: WatchVoiceFailure) -> String {
        switch failure {
        case .fullAccessRequired:       return fullAccessRequired
        case .notSignedIn:              return notSignedIn
        case .consentRequired:          return consentRequired
        case .offline:                  return offline
        case .timedOut:                 return timedOut
        case .rateLimited:              return rateLimited
        case .quotaExhausted:           return quotaExhausted
        case .monthlyQuotaExhausted:    return monthlyQuotaExhausted
        case .serviceUnavailable, .transcriptionUnavailable, .transcriptionFailed, .storageUnavailable,
             .nothingHeard, .voiceLimitReached, .microphoneDenied, .phoneUnreachable:
            return serviceUnavailable
        case .nothingToRewrite:         return nothingToRewrite
        case .copiedMessageUnreadable:  return copiedMessageUnreadable
        case .copiedMessageTooLong:     return copiedMessageTooLong
        case .instructionTooLong:       return instructionTooLong
        case .unsupportedVersion, .invalidRequest, .instructionEmpty, .expired, .audioRejected:
            return requestProblem
        }
    }

    static let english = WatchVoiceStrings(
        controlName: "Watch Voice",
        waiting: "Speak on your Apple Watch",
        openWatchApp: "Open AI Reply on Apple Watch",
        installWatchApp: "Install AI Reply on Apple Watch",
        connectionLost: "Connection to Apple Watch lost",
        generating: "Generating response…",
        instructionReceived: "Instruction from Watch",
        responseReady: "Response ready",
        generate: "Generate",
        insertResponse: "Insert Response",
        dismiss: "Dismiss",
        fullAccessRequired: "Turn on Full Access for AI Reply",
        copiedMessageUnreadable: "iOS did not allow reading the copied message",
        copiedMessageTooLong: "The copied message is too long",
        nothingToRewrite: "Nothing to rewrite: type or select text",
        instructionTooLong: "The instruction is too long",
        notSignedIn: "Sign in to AI Reply on iPhone",
        consentRequired: "Open AI Reply to accept the terms",
        offline: "No internet connection",
        timedOut: "The response took too long",
        rateLimited: "Too many requests. Try in a moment",
        quotaExhausted: "Today's replies are used up",
        monthlyQuotaExhausted: "This month's replies are used up",
        serviceUnavailable: "AI Reply is unavailable. Try again",
        requestProblem: "This instruction could not be used"
    )

    static let russian = WatchVoiceStrings(
        controlName: "Голос с часов",
        waiting: "Говорите на Apple Watch",
        openWatchApp: "Откройте AI Reply на Apple Watch",
        installWatchApp: "Установите AI Reply на Apple Watch",
        connectionLost: "Связь с Apple Watch потеряна",
        generating: "Создаю ответ…",
        instructionReceived: "Указание с часов",
        responseReady: "Ответ готов",
        generate: "Создать",
        insertResponse: "Вставить ответ",
        dismiss: "Убрать",
        fullAccessRequired: "Включите полный доступ для AI Reply",
        copiedMessageUnreadable: "iOS не разрешила прочитать скопированное",
        copiedMessageTooLong: "Скопированное сообщение слишком длинное",
        nothingToRewrite: "Нечего переписывать: введите или выделите текст",
        instructionTooLong: "Указание слишком длинное",
        notSignedIn: "Войдите в AI Reply на iPhone",
        consentRequired: "Откройте AI Reply и примите условия",
        offline: "Нет интернета",
        timedOut: "Ответ не пришёл вовремя",
        rateLimited: "Слишком много запросов, подождите немного",
        quotaExhausted: "Ответы на сегодня закончились",
        monthlyQuotaExhausted: "Ответы на этот месяц закончились",
        serviceUnavailable: "AI Reply недоступен, попробуйте ещё раз",
        requestProblem: "Это указание нельзя использовать"
    )

    static let kazakh = WatchVoiceStrings(
        controlName: "Сағаттан дауыс",
        waiting: "Apple Watch-та сөйлеңіз",
        openWatchApp: "Apple Watch-та AI Reply-ды ашыңыз",
        installWatchApp: "Apple Watch-қа AI Reply орнатыңыз",
        connectionLost: "Apple Watch байланысы үзілді",
        generating: "Жауап дайындалуда…",
        instructionReceived: "Сағаттан нұсқау",
        responseReady: "Жауап дайын",
        generate: "Жасау",
        insertResponse: "Жауапты кірістіру",
        dismiss: "Алып тастау",
        fullAccessRequired: "AI Reply үшін толық рұқсатты қосыңыз",
        copiedMessageUnreadable: "iOS көшірілген мәтінді оқуға рұқсат бермеді",
        copiedMessageTooLong: "Көшірілген хабарлама тым ұзын",
        nothingToRewrite: "Қайта жазатын мәтін жоқ: теріңіз не белгілеңіз",
        instructionTooLong: "Нұсқау тым ұзын",
        notSignedIn: "iPhone-да AI Reply-ға кіріңіз",
        consentRequired: "AI Reply-ды ашып, шарттарды қабылдаңыз",
        offline: "Интернет жоқ",
        timedOut: "Жауап уақытында келмеді",
        rateLimited: "Сұраныс тым көп, сәл күтіңіз",
        quotaExhausted: "Бүгінгі жауаптар бітті",
        monthlyQuotaExhausted: "Осы айдағы жауаптар бітті",
        serviceUnavailable: "AI Reply қолжетімсіз, қайталап көріңіз",
        requestProblem: "Бұл нұсқауды пайдалану мүмкін емес"
    )

    static let uzbek = WatchVoiceStrings(
        controlName: "Soatdan ovoz",
        waiting: "Apple Watch’da gapiring",
        openWatchApp: "Apple Watch’da AI Reply’ni oching",
        installWatchApp: "Apple Watch’ga AI Reply o‘rnating",
        connectionLost: "Apple Watch bilan aloqa uzildi",
        generating: "Javob tayyorlanmoqda…",
        instructionReceived: "Soatdan ko‘rsatma",
        responseReady: "Javob tayyor",
        generate: "Yaratish",
        insertResponse: "Javobni kiritish",
        dismiss: "Olib tashlash",
        fullAccessRequired: "AI Reply uchun to‘liq ruxsatni yoqing",
        copiedMessageUnreadable: "iOS nusxalangan matnni o‘qishga ruxsat bermadi",
        copiedMessageTooLong: "Nusxalangan xabar juda uzun",
        nothingToRewrite: "Qayta yoziladigan matn yo‘q: matn yozing yoki belgilang",
        instructionTooLong: "Ko‘rsatma juda uzun",
        notSignedIn: "iPhone’da AI Reply’ga kiring",
        consentRequired: "AI Reply’ni ochib, shartlarni qabul qiling",
        offline: "Internet yo‘q",
        timedOut: "Javob o‘z vaqtida kelmadi",
        rateLimited: "So‘rovlar juda ko‘p, biroz kuting",
        quotaExhausted: "Bugungi javoblar tugadi",
        monthlyQuotaExhausted: "Bu oylik javoblar tugadi",
        serviceUnavailable: "AI Reply mavjud emas, qayta urinib ko‘ring",
        requestProblem: "Bu ko‘rsatmani ishlatib bo‘lmaydi"
    )
}
