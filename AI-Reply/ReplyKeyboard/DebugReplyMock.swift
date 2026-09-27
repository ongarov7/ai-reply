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

        Self.lock.lock()
        let index = Self.served % Self.replies.count
        Self.served += 1
        Self.lock.unlock()
        return GeneratedReply(text: Self.replies[index], detectedLanguage: nil)
    }
}
#endif
