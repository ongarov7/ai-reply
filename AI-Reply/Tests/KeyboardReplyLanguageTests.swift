import XCTest
@testable import AIReply

/// The customer's rule, from the keyboard's side: the COPIED MESSAGE decides
/// the reply's language. A Kazakh message gets a Kazakh reply whatever the
/// phone, the app, the layout or the quick-intent phrase is in; only the
/// user's own words ("ответь на русском") or the profile's reply language may
/// change that, and the server decides it.
///
/// So the keyboard must send the message exactly as copied and add no rule
/// of its own: the layout travels only as `input_language`, a hint the server
/// uses for a message too short to tell.
final class KeyboardReplyLanguageTests: XCTestCase {

    private let kazakhMessage = "Сәлем! Ертең кездесуге уақытың бар ма?"

    /// What the keyboard puts in a request: a Russian app, the Russian
    /// layout, a Russian quick intent - and a Kazakh message.
    private func keyboardRequest() throws -> AIReplyService.Request {
        let agree = try XCTUnwrap(AIReplyStrings.forLanguage(.russian).quickIntents.first { $0.id == "agree" })
        return AIReplyService.Request(
            message: kazakhMessage,
            template: .builtIn(.friend, sortIndex: 0),
            configuration: .initial,
            uiLanguage: .russian,
            instruction: agree.phrase,
            inputLanguage: .russian
        )
    }

    func testKazakhMessageGoesAsCopiedWithNoLanguageRule() throws {
        let request = try keyboardRequest()
        let instruction = ReplyInstruction.prepare(request.instruction, appendsLanguageRule: false)
        XCTAssertEqual(instruction, request.instruction, "a server with sender_profile gets the phrase as tapped")

        let context = AccountReplyTransport.RequestContext(
            message: request.message,
            instruction: instruction,
            templateID: request.template.id,
            templateName: request.template.displayName(appLanguage: request.uiLanguage),
            templateRelationship: request.template.relationship.rawValue,
            templateTone: request.template.tone,
            templateInstructions: request.template.instructions,
            templateReplyLength: request.template.replyLength,
            templateEmojiPolicy: request.template.emojiPolicy,
            templateWorkingHoursBehaviour: request.template.workingHoursBehaviour,
            templateBusiness: request.template.effectiveBusiness,
            appLanguage: request.uiLanguage.rawValue,
            inputLanguage: request.inputLanguage?.rawValue,
            business: nil,
            profile: AIReplyService.profile(from: request.configuration.profile)
        )
        let body = AccountReplyTransport.body(for: context, serverSupportsPreferences: true, serverSupportsSenderProfile: true)
        let sent = try XCTUnwrap(JSONSerialization.jsonObject(with: JSONEncoder().encode(body)) as? [String: Any])

        XCTAssertEqual(sent["source_text"] as? String, kazakhMessage, "the message exactly as copied")
        let sentInstruction = try XCTUnwrap(sent["instruction"] as? String)
        XCTAssertFalse(sentInstruction.contains(ReplyInstruction.languageRule))
        XCTAssertEqual(sentInstruction, "Ответь согласием.")
        XCTAssertEqual(sent["input_language"] as? String, "ru", "the layout, only as a hint")
        XCTAssertNil((sent["profile"] as? [String: Any])?["reply_language"], "a profile on Auto names no language")
        // Nothing else in the request speaks about the reply's language.
        XCTAssertEqual(
            Set(sent.keys),
            ["source_text", "instruction", "language", "input_language", "template_id", "template", "platform", "app_version"]
        )
    }

    /// The same request through the service, as the keyboard runs it: the
    /// message reaches the transport unchanged and the instruction carries
    /// no language sentence for a server that resolves the language itself.
    func testServicePassesTheMessageThroughUnchanged() async throws {
        let defaults = AppGroup.defaults
        let key = "ai.features.senderProfile"
        let previous = defaults.object(forKey: key)
        defaults.set(true, forKey: key)
        defer { defaults.set(previous, forKey: key) }

        let captured = PromptBox()
        let service = AIReplyService(transportOverride: { _, prompt in
            captured.prompt = prompt
            return CannedReply()
        })
        _ = try await service.generate(try keyboardRequest())

        let prompt = try XCTUnwrap(captured.prompt)
        XCTAssertTrue(prompt.user.contains(kazakhMessage))
        XCTAssertTrue(prompt.user.contains("Ответь согласием."))
        XCTAssertFalse(prompt.user.contains(ReplyInstruction.languageRule))
    }
}

private final class PromptBox: @unchecked Sendable {
    var prompt: ReplyPromptBuilder.Prompt?
}

private struct CannedReply: ReplyTransport {
    func generate(prompt: ReplyPromptBuilder.Prompt) async throws -> GeneratedReply {
        GeneratedReply(text: "Иә, келісемін!", detectedLanguage: "kk")
    }
}
