import XCTest
@testable import AIReply

/// Which existing pipeline an instruction takes, and what it carries there.
final class WatchVoiceRoutingTests: XCTestCase {

    private let limits = WatchVoiceRouter.Limits(replyInstruction: 280, composeInstruction: 400)
    private let copied = ReplyContext(text: "Можем встретиться завтра?", source: .clipboard)

    private func command(_ instruction: String = "Скажи, что занят", spoken: WatchVoiceLanguage = .russian,
                         options: WatchVoiceOptions = WatchVoiceOptions()) -> WatchVoiceCommand {
        WatchVoiceCommand(envelope: .command(instruction: instruction, language: spoken, options: options, keyboardSessionID: nil),
                          targetSessionID: nil)!
    }

    private func plan(_ command: WatchVoiceCommand, copied: Result<ReplyContext, AIReplyError>? = nil,
                      existing: String? = nil) -> WatchGenerationPlan {
        WatchVoiceRouter.plan(for: command, copied: copied, existingText: existing, limits: limits)
    }

    // MARK: Reply

    func testAReplyUsesTheCopiedMessageAndTheInstruction() throws {
        guard case .reply(let reply) = plan(command(), copied: .success(copied)) else { return XCTFail("not a reply") }
        XCTAssertEqual(reply.source, copied.text)
        XCTAssertEqual(reply.sourceKind, .clipboard)
        XCTAssertEqual(reply.instruction, "Скажи, что занят")
        XCTAssertNil(reply.length, "the persona's own length")
        XCTAssertNil(reply.responseLanguage, "the reply follows the copied message")
        XCTAssertEqual(reply.inputLanguage, .russian)
    }

    func testNothingCopiedWritesANewMessage() {
        guard case .compose(let compose) = plan(command(), copied: .failure(.noSourceMessage)) else {
            return XCTFail("not a new message")
        }
        XCTAssertEqual(compose.instruction, "Скажи, что занят")
    }

    func testARefusedPasteDoesNotBecomeANewMessage() {
        XCTAssertEqual(plan(command(), copied: .failure(.pasteDenied)), .needsCopiedMessage(instruction: "Скажи, что занят"))
    }

    func testFullAccessOffIsSaid() {
        XCTAssertEqual(plan(command(), copied: .failure(.fullAccessRequired)), .refused(.fullAccessRequired, limit: nil))
    }

    func testAnInstructionTooLongForAReplyIsRefusedNotCut() {
        let long = String(repeating: "а", count: 281)
        XCTAssertEqual(plan(command(long), copied: .success(copied)), .refused(.instructionTooLong, limit: 280))
    }

    func testAnInstructionForANewMessageHasItsOwnLimit() {
        let fine = String(repeating: "а", count: 281)
        let options = WatchVoiceOptions(mode: .newMessage)
        guard case .compose = plan(command(fine, options: options)) else { return XCTFail("a new message takes 400") }
        let tooLong = String(repeating: "а", count: 401)
        XCTAssertEqual(plan(command(tooLong, options: options)), .refused(.instructionTooLong, limit: 400))
    }

    // MARK: Options

    func testLengthOnAReply() {
        func length(_ value: WatchVoiceLength) -> WatchReplyPlan? {
            if case .reply(let reply) = plan(command(options: WatchVoiceOptions(length: value)), copied: .success(copied)) {
                return reply
            }
            return nil
        }
        XCTAssertEqual(length(.short)?.length, .short)
        XCTAssertEqual(length(.medium)?.length, .medium)
        XCTAssertEqual(length(.long)?.length, .medium, "the server's longest")
        XCTAssertEqual(length(.long)?.instruction, "Скажи, что занят (Напиши подробнее и развёрнуто.)")
        XCTAssertEqual(length(.short)?.instruction, "Скажи, что занят", "already said by reply_length")
    }

    func testTheReplyLanguageIsIndependentOfTheSpokenOne() {
        let options = WatchVoiceOptions(responseLanguage: .kazakh)
        guard case .reply(let reply) = plan(command(spoken: .russian, options: options), copied: .success(copied)) else {
            return XCTFail("not a reply")
        }
        XCTAssertEqual(reply.responseLanguage, .kazakh)
        XCTAssertEqual(reply.inputLanguage, .russian)
    }

    func testANewMessageAsksForLanguageAndLengthInWords() {
        let options = WatchVoiceOptions(mode: .newMessage, responseLanguage: .kazakh, length: .short)
        guard case .compose(let compose) = plan(command(options: options)) else { return XCTFail("not a new message") }
        XCTAssertEqual(compose.instruction, "Скажи, что занят (Напиши коротко.) (Напиши на казахском языке.)")
    }

    func testNotesAreInTheLanguageSpoken() {
        let options = WatchVoiceOptions(mode: .newMessage, responseLanguage: .russian, length: .short)
        guard case .compose(let compose) = plan(command("Busy today", spoken: .english, options: options)) else {
            return XCTFail("not a new message")
        }
        XCTAssertEqual(compose.instruction, "Busy today (Keep it short.) (Write it in Russian.)")
    }

    // MARK: Existing text

    func testRewriteWorksOnTheExistingText() {
        let options = WatchVoiceOptions(mode: .rewrite)
        guard case .compose(let compose) = plan(command("Вежливее", options: options), existing: "Завтра не могу") else {
            return XCTFail("not a rewrite")
        }
        XCTAssertEqual(compose.instruction, "Перепиши мой текст ниже. Вежливее\n\nМой текст: Завтра не могу")
    }

    func testShortenAndExpandSayWhatToDo() {
        for (mode, expected) in [(WatchVoiceMode.shorten, "Сократи"), (.expand, "Сделай")] {
            guard case .compose(let compose) = plan(command("Да", options: WatchVoiceOptions(mode: mode)), existing: "Текст") else {
                return XCTFail("\(mode)")
            }
            XCTAssertTrue(compose.instruction.hasPrefix(expected), compose.instruction)
        }
    }

    func testNothingToRewriteIsSaid() {
        for mode in [WatchVoiceMode.rewrite, .shorten, .expand] {
            XCTAssertEqual(plan(command(options: WatchVoiceOptions(mode: mode)), existing: "   "), .refused(.nothingToRewrite, limit: nil))
        }
    }

    func testTextTooLongToRewriteIsRefused() {
        let long = String(repeating: "б", count: 400)
        XCTAssertEqual(plan(command(options: WatchVoiceOptions(mode: .rewrite)), existing: long),
                       .refused(.instructionTooLong, limit: 400))
    }

    // MARK: Failures

    func testEveryKeyboardErrorHasAWordForTheWatch() {
        XCTAssertEqual(WatchVoiceFailure(.quotaExhausted), .quotaExhausted)
        XCTAssertEqual(WatchVoiceFailure(.monthlyQuotaExhausted), .monthlyQuotaExhausted)
        XCTAssertEqual(WatchVoiceFailure(.authenticationFailed), .notSignedIn)
        XCTAssertEqual(WatchVoiceFailure(.consentRequired), .consentRequired)
        XCTAssertEqual(WatchVoiceFailure(.offline), .offline)
        XCTAssertEqual(WatchVoiceFailure(.messageTooLong(limit: 400)), .copiedMessageTooLong)
        XCTAssertEqual(WatchVoiceFailure(.emptyResponse), .serviceUnavailable)
    }

    // MARK: One action, one charge

    func testACommandsIdentityBecomesTheServersKey() {
        let id = UUID().uuidString
        XCTAssertEqual(IdempotencyKey.forAction(actionID: id, serverSupportsKeys: true), IdempotencyKey(id))
        XCTAssertNil(IdempotencyKey.forAction(actionID: id, serverSupportsKeys: false), "never sent to a server without keys")
        XCTAssertNotNil(IdempotencyKey.forAction(actionID: nil, serverSupportsKeys: true), "a plain tap still has its own")
    }

    /// Spent replies stop a watch request as they stop a tap: same transport,
    /// same error, no special case for the watch.
    func testASpentQuotaStopsAWatchRequestLikeAnyOther() {
        let spent = APIError.dailyLimitReached(limit: 3, usedToday: 3, resetsAt: nil)
        XCTAssertEqual(AccountComposeTransport.map(spent), .quotaExhausted)
        XCTAssertEqual(AccountReplyTransport.map(spent), .quotaExhausted)
        XCTAssertEqual(WatchVoiceFailure(AccountReplyTransport.map(spent)), .quotaExhausted)
        XCTAssertFalse(WatchVoiceFailure.quotaExhausted.offersRetry)
    }
}

/// The composer flow a command actually runs through.
@MainActor
final class WatchComposeRequestTests: XCTestCase {

    private final class Recorder: @unchecked Sendable {
        var requests: [ComposeService.Request] = []
    }

    private struct Transport: ComposeTransport {
        let recorder: Recorder
        let result: Result<String, AIReplyError>

        func compose(_ request: ComposeService.Request) async throws -> GeneratedReply {
            recorder.requests.append(request)
            return GeneratedReply(text: try result.get(), detectedLanguage: nil)
        }
    }

    private final class Listener: ComposeFlowCoordinatorDelegate {
        var onChange: (() -> Void)?
        func composeCoordinatorDidChange(_ coordinator: ComposeFlowCoordinator) { onChange?() }
    }

    private func run(result: Result<String, AIReplyError>) async -> (ComposeFlowCoordinator, Recorder, Listener) {
        let recorder = Recorder()
        let listener = Listener()
        let coordinator = ComposeFlowCoordinator(service: ComposeService(transportOverride: { _ in Transport(recorder: recorder, result: result) }))
        coordinator.delegate = listener
        let finished = expectation(description: "finished")
        listener.onChange = { [weak coordinator] in
            if coordinator?.flow.isGenerating == false { finished.fulfill() }
        }
        coordinator.startFromWatch(instruction: "Напиши, что опаздываю", inputLanguage: .russian, actionID: "command-identity")
        await fulfillment(of: [finished], timeout: 3)
        listener.onChange = nil
        return (coordinator, recorder, listener)
    }

    func testTheCommandStartsARequestWithItsIdentity() async {
        let (coordinator, recorder, _) = await run(result: .success("Опаздываю на 10 минут."))
        XCTAssertEqual(recorder.requests.map(\.instruction), ["Напиши, что опаздываю"])
        XCTAssertEqual(recorder.requests.first?.actionID, "command-identity")
        XCTAssertEqual(recorder.requests.first?.inputLanguage, .russian)
        XCTAssertEqual(coordinator.flow.stage, .result)
        coordinator.clear()
    }

    func testAnotherVersionIsANewAction() async {
        let (coordinator, recorder, listener) = await run(result: .success("Опаздываю."))
        let second = expectation(description: "second")
        listener.onChange = { [weak coordinator] in
            if coordinator?.flow.isGenerating == false { second.fulfill() }
        }
        coordinator.generate()
        await fulfillment(of: [second], timeout: 3)
        XCTAssertEqual(recorder.requests.count, 2)
        XCTAssertNil(recorder.requests[1].actionID, "a new key, a new charge: the user asked for it")
        coordinator.clear()
    }

    func testAFailureIsRecoverable() async {
        let (coordinator, _, _) = await run(result: .failure(.offline))
        XCTAssertEqual(coordinator.flow.stage, .composing)
        XCTAssertEqual(coordinator.flow.error, .offline)
        XCTAssertEqual(coordinator.session?.instruction, "Напиши, что опаздываю", "nothing lost, Retry is there")
        coordinator.clear()
    }

    func testASpentQuotaIsTheOrdinaryError() async {
        let (coordinator, _, _) = await run(result: .failure(.quotaExhausted))
        XCTAssertEqual(coordinator.flow.error, .quotaExhausted)
        coordinator.clear()
    }

    func testASavedTextComesBackWithInsert() {
        let coordinator = ComposeFlowCoordinator(service: ComposeService(transportOverride: { _ in Transport(recorder: Recorder(), result: .success("")) }))
        coordinator.showSaved(instruction: "Опоздаю", text: "Опаздываю на 10 минут.")
        XCTAssertEqual(coordinator.flow.stage, .result)
        XCTAssertEqual(coordinator.flow.draftText, "Опаздываю на 10 минут.")
        coordinator.clear()
    }
}
