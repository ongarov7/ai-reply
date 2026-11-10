#if DEBUG
import Foundation

/// DEBUG ONLY: canned replies, so the reply flow can be exercised in the
/// Simulator without an account or a network (see
/// `AIConfiguration.debugMockReplies`). Compiled out of release builds.
///
/// Replies rotate between Kazakh, Russian and English, with an emoji and a
/// line break, so every Regenerate produces a visibly new version and the
/// insertion path gets Unicode and multi-line text. Instruction tags simulate
/// failures: `#offline`, `#quota`, `#slow` (six seconds, to try Stop).
struct DebugReplyMock: ReplyTransport {

    static var isEnabled: Bool { AIConfiguration.debugMockReplies }

    let instruction: String

    private static let lock = NSLock()
    private static var served = 0

    private static let replies = [
        "Сәлеметсіз бе! Иә, ертең сағат 15:00-ге дейін жеткізе аламыз 🚚",
        "Здравствуйте! Да, доставим завтра до 15:00.\nПодскажите, пожалуйста, адрес 🙂",
        "Hi! Yes, we can deliver tomorrow before 3 pm 👍"
    ]

    func generate(prompt: ReplyPromptBuilder.Prompt) async throws -> GeneratedReply {
        let tags = instruction.lowercased()
        try await Task.sleep(for: tags.contains("#slow") ? .seconds(6) : .milliseconds(700))
        try Task.checkCancellation()
        if tags.contains("#offline") { throw AIReplyError.offline }
        if tags.contains("#quota") { throw AIReplyError.quotaExhausted }
        // The mock microphone's sentences get an answer in their language.
        if instruction.contains("Сәрсенбі") {
            return GeneratedReply(text: "Сәлеметсіз бе! Бүгін де, ертең де бос емеспін. Сәрсенбі түстен кейін кездесейік пе?", detectedLanguage: nil)
        }
        // The watch's quick "Politely", for the screenshots.
        if instruction.contains("Ответь вежливо") {
            return GeneratedReply(text: "Здравствуйте! Да, давайте завтра в 15:00 — с удовольствием обсудим договор.", detectedLanguage: nil)
        }
        if instruction.contains("Сыпайы жауап бер") {
            return GeneratedReply(text: "Сәлеметсіз бе! Иә, ертең 15:00-де кездесіп, келісімшартты талқылайық.", detectedLanguage: nil)
        }
        if instruction.contains("среду после обеда") {
            return GeneratedReply(text: "Здравствуйте! Сегодня и завтра я занят. Давайте встретимся в среду после обеда?", detectedLanguage: nil)
        }

        Self.lock.lock()
        let index = Self.served % Self.replies.count
        Self.served += 1
        Self.lock.unlock()
        return GeneratedReply(text: Self.replies[index], detectedLanguage: nil)
    }
}
#endif
