import XCTest
@testable import AIReply

/// The keyboard's "Create" mode: writing a new message from a description.
///
/// What these pin: the instruction is the ONLY text that leaves the device
/// (no copied message, ever); one request at a time; Regenerate asks for a new
/// version without losing the old one; New clears everything; parking keeps
/// the session but never a spinner; every server failure lands in the closed
/// error set with a translated sentence.
@MainActor
final class ComposeFlowTests: XCTestCase {

    // MARK: Test transport

    /// Records requests and answers them when told to, so a test can hold a
    /// request in flight.
    private final class ScriptedTransport: @unchecked Sendable {
        private let lock = NSLock()
        private var _requests: [ComposeService.Request] = []
        private var continuations: [CheckedContinuation<GeneratedReply, Error>] = []

        var requests: [ComposeService.Request] {
            lock.lock(); defer { lock.unlock() }
            return _requests
        }

        var pending: Int {
            lock.lock(); defer { lock.unlock() }
            return continuations.count
        }

        func transport() -> ComposeTransport { Transport(owner: self) }

        func answer(_ text: String) {
            lock.lock()
            let next = continuations.isEmpty ? nil : continuations.removeFirst()
            lock.unlock()
            next?.resume(returning: GeneratedReply(text: text, detectedLanguage: nil))
        }

        func fail(_ error: AIReplyError) {
            lock.lock()
            let next = continuations.isEmpty ? nil : continuations.removeFirst()
            lock.unlock()
            next?.resume(throwing: error)
        }

        fileprivate func record(_ request: ComposeService.Request, _ continuation: CheckedContinuation<GeneratedReply, Error>) {
            lock.lock()
            _requests.append(request)
            continuations.append(continuation)
            lock.unlock()
        }

        private struct Transport: ComposeTransport {
            let owner: ScriptedTransport
            func compose(_ request: ComposeService.Request) async throws -> GeneratedReply {
                try await withCheckedThrowingContinuation { continuation in
                    owner.record(request, continuation)
                }
            }
        }
    }

    private var scripted: ScriptedTransport!
    private var coordinator: ComposeFlowCoordinator!

    override func setUp() async throws {
        scripted = ScriptedTransport()
        let script = scripted!
        coordinator = ComposeFlowCoordinator(service: ComposeService(transportOverride: { _ in script.transport() }))
        coordinator.uiLanguage = .russian
        ComposeSessionParking.discard()
    }

    override func tearDown() async throws {
        coordinator.clear()
        ComposeSessionParking.discard()
    }

    private func waitUntil(_ condition: @escaping () -> Bool, file: StaticString = #filePath, line: UInt = #line) async {
        for _ in 0..<200 where !condition() {
            try? await Task.sleep(for: .milliseconds(5))
        }
        XCTAssertTrue(condition(), "condition not reached", file: file, line: line)
    }

    private let instruction = "Поздравь директора Сакена Бакпакбековича с 55-летием. Тепло, с эмодзи."

    // MARK: Generation

    func testWriteSendsOnlyTheInstructionAndShowsTheMessage() async {
        coordinator.open()
        coordinator.updateInstruction("  " + instruction + "\n")
        coordinator.generate()
        XCTAssertTrue(coordinator.flow.isGenerating)

        await waitUntil { self.scripted.pending == 1 }
        let request = scripted.requests.first
        XCTAssertEqual(request?.instruction, instruction, "trimmed instruction, nothing else")
        XCTAssertEqual(request?.uiLanguage, .russian)
        XCTAssertEqual(request?.isRegeneration, false)

        scripted.answer("Уважаемый Сакен Бакпакбекович!\n\nПоздравляем! 🎉")
        await waitUntil { self.coordinator.flow.stage == .result }
        XCTAssertEqual(coordinator.flow.draftText, "Уважаемый Сакен Бакпакбекович!\n\nПоздравляем! 🎉")
        XCTAssertEqual(coordinator.session?.instruction, "  " + instruction + "\n", "the field keeps what was typed")
    }

    /// The keyboard hands the coordinator the layout and the profile's gender;
    /// both reach the request.
    func testCoordinatorPassesLayoutAndGender() async {
        coordinator.inputLanguage = .kazakh
        coordinator.grammaticalGender = .male
        coordinator.open()
        coordinator.updateInstruction(instruction)
        coordinator.generate()
        await waitUntil { self.scripted.pending == 1 }
        XCTAssertEqual(scripted.requests.first?.inputLanguage, .kazakh)
        XCTAssertEqual(scripted.requests.first?.grammaticalGender, .male)
        scripted.answer("Құттықтаймын!")
    }

    func testEmptyInstructionNeverReachesTheNetwork() async {
        coordinator.open()
        coordinator.updateInstruction("   \n ")
        coordinator.generate()
        XCTAssertEqual(coordinator.flow.error, .noInstruction)
        XCTAssertEqual(coordinator.flow.stage, .composing)
        try? await Task.sleep(for: .milliseconds(30))
        XCTAssertTrue(scripted.requests.isEmpty)
    }

    func testRepeatedTapsSendOneRequest() async {
        coordinator.open()
        coordinator.updateInstruction(instruction)
        for _ in 0..<5 { coordinator.generate() }
        await waitUntil { self.scripted.pending == 1 }
        try? await Task.sleep(for: .milliseconds(30))
        XCTAssertEqual(scripted.requests.count, 1)
        scripted.answer("Один")
        await waitUntil { self.coordinator.flow.stage == .result }
    }

    func testRegenerateAsksForAnotherVersionAndKeepsTheFirst() async {
        coordinator.open()
        coordinator.updateInstruction(instruction)
        coordinator.generate()
        await waitUntil { self.scripted.pending == 1 }
        scripted.answer("Первый вариант")
        await waitUntil { self.coordinator.flow.stage == .result }

        coordinator.generate()
        await waitUntil { self.scripted.pending == 1 }
        XCTAssertEqual(scripted.requests.last?.isRegeneration, true)
        XCTAssertEqual(scripted.requests.last?.instruction, instruction, "the same instruction")
        scripted.answer("Второй вариант")
        await waitUntil { self.coordinator.flow.drafts.count == 2 }
        XCTAssertEqual(coordinator.flow.draftText, "Второй вариант")
        coordinator.showPreviousVersion()
        XCTAssertEqual(coordinator.flow.draftText, "Первый вариант")
    }

    func testFailureKeepsTheInstructionAndRecovers() async {
        coordinator.open()
        coordinator.updateInstruction(instruction)
        coordinator.generate()
        await waitUntil { self.scripted.pending == 1 }
        scripted.fail(.offline)
        await waitUntil { self.coordinator.flow.error == .offline }
        XCTAssertEqual(coordinator.flow.stage, .composing)
        XCTAssertEqual(coordinator.session?.instruction, instruction)

        coordinator.generate()
        await waitUntil { self.scripted.pending == 1 }
        XCTAssertNil(coordinator.flow.error, "retrying clears the old error")
        scripted.answer("Готово")
        await waitUntil { self.coordinator.flow.stage == .result }
    }

    func testQuotaErrorIsReportedAsSuch() async {
        coordinator.open()
        coordinator.updateInstruction(instruction)
        coordinator.generate()
        await waitUntil { self.scripted.pending == 1 }
        scripted.fail(.quotaExhausted)
        await waitUntil { self.coordinator.flow.error == .quotaExhausted }
    }

    func testStopIgnoresTheLateAnswer() async {
        coordinator.open()
        coordinator.updateInstruction(instruction)
        coordinator.generate()
        await waitUntil { self.scripted.pending == 1 }
        coordinator.cancelGeneration()
        XCTAssertEqual(coordinator.flow.stage, .composing)
        scripted.answer("Поздно")
        try? await Task.sleep(for: .milliseconds(40))
        XCTAssertTrue(coordinator.flow.drafts.isEmpty, "a stopped request must not land")
    }

    // MARK: New, Back, Insert

    func testNewClearsInstructionVersionsAndRequest() async {
        coordinator.open()
        coordinator.updateInstruction(instruction)
        coordinator.generate()
        await waitUntil { self.scripted.pending == 1 }
        scripted.answer("Текст")
        await waitUntil { self.coordinator.flow.stage == .result }

        coordinator.reset()
        XCTAssertEqual(coordinator.session, ComposeSession())
        XCTAssertFalse(coordinator.session?.hasContent ?? true)
    }

    func testBackReturnsToTheInstructionWithVersionsKept() async {
        coordinator.open()
        coordinator.updateInstruction(instruction)
        coordinator.generate()
        await waitUntil { self.scripted.pending == 1 }
        scripted.answer("Текст")
        await waitUntil { self.coordinator.flow.stage == .result }

        coordinator.back()
        XCTAssertEqual(coordinator.flow.stage, .composing)
        XCTAssertEqual(coordinator.flow.drafts.count, 1)
        XCTAssertEqual(coordinator.session?.instruction, instruction)
    }

    func testInsertTakesTheEditedMessageNeverTheInstruction() async {
        coordinator.open()
        coordinator.updateInstruction(instruction)
        coordinator.generate()
        await waitUntil { self.scripted.pending == 1 }
        scripted.answer("Сәлем!")
        await waitUntil { self.coordinator.flow.stage == .result }

        coordinator.beginEditing()
        coordinator.updateDraft("Сәлем, достар! 👋 ")
        XCTAssertEqual(coordinator.requestInsert(hostHasText: false), .insert("Сәлем, достар! 👋"))

        XCTAssertEqual(coordinator.requestInsert(hostHasText: true), .askAboutExistingText)
        XCTAssertEqual(coordinator.resolveConflict(.cancel), .cancelled)
        XCTAssertEqual(coordinator.flow.stage, .editing)
    }

    // MARK: Lifecycle

    func testParkingStopsTheRequestAndKeepsTheInstruction() async {
        coordinator.open()
        coordinator.updateInstruction(instruction)
        coordinator.generate()
        await waitUntil { self.scripted.pending == 1 }

        coordinator.park()
        XCTAssertFalse(coordinator.isActive)
        scripted.answer("Поздно")

        guard let parked = ComposeSessionParking.take() else { return XCTFail("nothing parked") }
        coordinator.restore(parked)
        XCTAssertEqual(coordinator.flow.stage, .composing, "no spinner survives the keyboard closing")
        XCTAssertEqual(coordinator.session?.instruction, instruction)
        XCTAssertNil(ComposeSessionParking.take(), "a parked session is handed out once")
    }

    func testParkedSessionExpires() {
        ComposeSessionParking.park(ComposeSession(instruction: "x"), now: Date(timeIntervalSince1970: 0))
        XCTAssertNil(ComposeSessionParking.take(now: Date(timeIntervalSince1970: ComposeSessionParking.lifetime + 1)))
    }

    func testOpenDiscardsAnOldParkedSession() {
        ComposeSessionParking.park(ComposeSession(instruction: "старое"))
        coordinator.open()
        XCTAssertEqual(coordinator.session, ComposeSession())
        XCTAssertNil(ComposeSessionParking.take())
    }

    func testFullAccessErrorIsShownWithoutARequest() async {
        coordinator.open()
        coordinator.updateInstruction(instruction)
        coordinator.showError(.fullAccessRequired)
        XCTAssertEqual(coordinator.flow.error, .fullAccessRequired)
        try? await Task.sleep(for: .milliseconds(20))
        XCTAssertTrue(scripted.requests.isEmpty)
    }

    // MARK: Reply to copied

    private let copied = ReplyContext(text: "Придёшь завтра на встречу в 10?", source: .clipboard)
    private let friend = ReplyTemplate.builtIn(.friend, sortIndex: 0)

    /// The instruction typed in Create becomes the reply's instruction, the
    /// copied message its source - and nothing is written: not in Create,
    /// not in the reply. The reply composer opens in its first stage.
    func testReplyToCopiedCarriesTheInstructionAndGeneratesNothing() async {
        coordinator.open()
        coordinator.updateInstruction("  вежливо откажи\n")
        var reads = 0
        let reply = coordinator.replyToCopied(persona: friend, instructionLimit: 280) {
            reads += 1
            return .success(copied)
        }

        XCTAssertEqual(reads, 1, "the copied message is read once, on the tap")
        XCTAssertEqual(reply?.sourceMessage, copied.text)
        XCTAssertEqual(reply?.source, .clipboard)
        XCTAssertEqual(reply?.instruction, "вежливо откажи")
        XCTAssertEqual(reply?.template, friend)
        XCTAssertEqual(reply?.flow, ReplyComposerFlow(), "the reply composer's first stage: no request, no error")
        XCTAssertFalse(coordinator.isActive, "Create closes: the panel is the reply now")
        XCTAssertNil(ComposeSessionParking.take(), "and nothing of Create is kept")

        try? await Task.sleep(for: .milliseconds(30))
        XCTAssertTrue(scripted.requests.isEmpty, "nothing is written until the user taps Reply")
    }

    /// Short instructions are instructions too, in every language.
    func testShortInstructionsAreCarriedAsTyped() {
        for typed in ["да", "Иә", "yes", "скажи, что согласен", "келісемін де", "say I agree"] {
            coordinator.open()
            coordinator.updateInstruction(typed)
            let reply = coordinator.replyToCopied(persona: friend, instructionLimit: 280) { .success(copied) }
            XCTAssertEqual(reply?.instruction, typed)
        }
    }

    /// No instruction is fine: it is then a plain reply to the copied
    /// message, as after a persona tap.
    func testReplyToCopiedWithoutAnInstruction() {
        coordinator.open()
        let reply = coordinator.replyToCopied(persona: friend, instructionLimit: 280) { .success(copied) }
        XCTAssertEqual(reply?.instruction, "")
        XCTAssertEqual(reply?.sourceMessage, copied.text)
    }

    /// Nothing copied: "Copy a message first" in Create, which stays as it
    /// was, instruction and all. Write stays Write - Retry would write a new
    /// message, which is not what was just tried.
    func testEmptyClipboardKeepsCreateAndSaysCopyFirst() async {
        coordinator.open()
        coordinator.updateInstruction("да")
        let reply = coordinator.replyToCopied(persona: friend, instructionLimit: 280) { .failure(.noSourceMessage) }

        XCTAssertNil(reply)
        XCTAssertTrue(coordinator.isActive)
        XCTAssertEqual(coordinator.session?.instruction, "да")
        XCTAssertEqual(coordinator.flow.stage, .composing)
        XCTAssertEqual(coordinator.session?.copiedMessageError, .noSourceMessage)

        let russian = AIReplyStrings.forLanguage(.russian)
        let notice = coordinator.session?.notice(russian)
        XCTAssertEqual(notice?.text, "Сначала скопируйте сообщение")
        XCTAssertEqual(notice?.offersRetry, false)

        coordinator.updateInstruction("да, приду")
        XCTAssertNil(coordinator.session?.copiedMessageError, "typing clears it")
        XCTAssertNil(coordinator.session?.notice(russian))

        try? await Task.sleep(for: .milliseconds(20))
        XCTAssertTrue(scripted.requests.isEmpty)
    }

    /// Without Full Access the clipboard cannot be read: the reply's
    /// sentence, which says exactly that - not Create's, which is about the
    /// internet.
    func testNoFullAccessExplainsItInCreate() async {
        coordinator.open()
        coordinator.updateInstruction("вежливо откажи")
        let reply = coordinator.replyToCopied(persona: friend, instructionLimit: 280) { .failure(.fullAccessRequired) }

        XCTAssertNil(reply)
        XCTAssertTrue(coordinator.isActive)
        XCTAssertEqual(coordinator.session?.instruction, "вежливо откажи")
        for language in AppLanguage.allCases {
            let strings = AIReplyStrings.forLanguage(language)
            XCTAssertEqual(coordinator.session?.notice(strings)?.text, strings.fullAccessRequired, language.rawValue)
        }

        // Write without Full Access still gets Create's own sentence.
        coordinator.showError(.fullAccessRequired)
        let english = AIReplyStrings.forLanguage(.english)
        XCTAssertNil(coordinator.session?.copiedMessageError)
        XCTAssertEqual(coordinator.session?.notice(english),
                       ComposeSession.Notice(text: english.compose.fullAccessRequired, offersRetry: true))
        try? await Task.sleep(for: .milliseconds(20))
        XCTAssertTrue(scripted.requests.isEmpty)
    }

    /// A Create instruction can be longer than a reply instruction may be.
    /// It is not cut (the user would send words they never saw): Create says
    /// how long it may be, and the clipboard is not touched.
    func testInstructionTooLongForAReplyIsRefusedBeforeReading() {
        coordinator.open()
        let long = String(repeating: "ә", count: 281)
        coordinator.updateInstruction(long)
        var reads = 0
        let reply = coordinator.replyToCopied(persona: friend, instructionLimit: 280) {
            reads += 1
            return .success(copied)
        }
        XCTAssertNil(reply)
        XCTAssertEqual(reads, 0)
        XCTAssertEqual(coordinator.session?.instruction, long)
        XCTAssertEqual(coordinator.session?.copiedMessageError, .instructionTooLong(limit: 280))
        XCTAssertTrue(coordinator.session?.notice(.forLanguage(.kazakh))?.text.contains("280") ?? false)
    }

    /// Only from the instruction, and never while a message is being written
    /// or shown: the clipboard is not read then.
    func testReplyToCopiedOnlyFromTheInstruction() async {
        var reads = 0
        let read: () -> Result<ReplyContext, AIReplyError> = {
            reads += 1
            return .success(self.copied)
        }
        XCTAssertNil(coordinator.replyToCopied(persona: friend, instructionLimit: 280, read: read), "Create is closed")

        coordinator.open()
        coordinator.updateInstruction(instruction)
        coordinator.generate()
        await waitUntil { self.scripted.pending == 1 }
        XCTAssertNil(coordinator.replyToCopied(persona: friend, instructionLimit: 280, read: read), "writing")
        scripted.answer("Текст")
        await waitUntil { self.coordinator.flow.stage == .result }
        XCTAssertNil(coordinator.replyToCopied(persona: friend, instructionLimit: 280, read: read), "a result is shown")
        XCTAssertEqual(reads, 0)
        XCTAssertEqual(coordinator.flow.drafts.count, 1, "the message written stays")
    }

    /// "Copy a message first", then the user leaves to copy it and comes
    /// back: the restored Create keeps the instruction but not the sentence,
    /// which would now say the copy failed. The same for a too-long notice.
    func testRestoredSessionDropsTheCopiedMessageError() async {
        let russian = AIReplyStrings.forLanguage(.russian)
        for (typed, error) in [("вежливо откажи", AIReplyError.noSourceMessage),
                               (String(repeating: "ә", count: 281), .instructionTooLong(limit: 280))] {
            coordinator.open()
            coordinator.updateInstruction(typed)
            _ = coordinator.replyToCopied(persona: friend, instructionLimit: 280) { .failure(.noSourceMessage) }
            XCTAssertEqual(coordinator.session?.copiedMessageError, error)

            coordinator.park()
            guard let parked = ComposeSessionParking.take() else { return XCTFail("nothing parked") }
            coordinator.restore(parked)

            XCTAssertEqual(coordinator.session?.instruction, typed, "the instruction comes back")
            XCTAssertNil(coordinator.session?.copiedMessageError)
            XCTAssertNil(coordinator.session?.notice(russian), "no stale sentence under the field")
            XCTAssertEqual(coordinator.flow.stage, .composing)
        }
        try? await Task.sleep(for: .milliseconds(20))
        XCTAssertTrue(scripted.requests.isEmpty, "restoring writes nothing")
    }

    /// Writing after a failed "Reply to copied" is a fresh attempt: the old
    /// sentence goes.
    func testWriteClearsTheCopiedMessageError() async {
        coordinator.open()
        coordinator.updateInstruction(instruction)
        _ = coordinator.replyToCopied(persona: friend, instructionLimit: 280) { .failure(.noSourceMessage) }
        coordinator.generate()
        XCTAssertNil(coordinator.session?.copiedMessageError)
        await waitUntil { self.scripted.pending == 1 }
        scripted.answer("Готово")
        await waitUntil { self.coordinator.flow.stage == .result }
    }
}

/// The reply side of "Reply to copied": who it is to, and that the
/// instruction from Create reaches the reply request - when Reply is tapped.
final class CopiedReplyHandoffTests: XCTestCase {

    private func configuration(hidden: Set<String> = [], custom: ReplyTemplate? = nil) -> ReplyConfiguration {
        var templates = ReplyTemplate.defaults
        for index in templates.indices where hidden.contains(templates[index].id) {
            templates[index].isVisible = false
        }
        if let custom { templates.append(custom) }
        return ReplyConfiguration(profile: .empty, templates: templates)
    }

    func testPersonaIsTheLastUsedOne() {
        XCTAssertEqual(configuration().personaForCopiedReply(lastUsedID: "work").id, "work")
        let custom = ReplyTemplate.custom(name: "Поставщик", sortIndex: 4)
        XCTAssertEqual(configuration(custom: custom).personaForCopiedReply(lastUsedID: custom.id), custom)
    }

    func testPersonaFallsBackToFriend() {
        XCTAssertEqual(configuration().personaForCopiedReply(lastUsedID: nil).id, "friend")
        XCTAssertEqual(configuration().personaForCopiedReply(lastUsedID: "deleted-persona").id, "friend")
        XCTAssertEqual(configuration(hidden: ["client"]).personaForCopiedReply(lastUsedID: "client").id, "friend",
                       "a persona the keyboard no longer shows is not used")
        XCTAssertEqual(ReplyConfiguration.initial.personaForCopiedReply(lastUsedID: nil).displayName(appLanguage: .kazakh), "Дос")
    }

    func testPersonaWhenFriendIsHidden() {
        XCTAssertEqual(configuration(hidden: ["friend"]).personaForCopiedReply(lastUsedID: nil).id, "client",
                       "the first persona shown")
        let none = configuration(hidden: ["friend", "client", "business", "work"])
        XCTAssertEqual(none.personaForCopiedReply(lastUsedID: nil).id, "friend")
    }

    /// Reply, tapped in the composer Create switched to: the copied message
    /// and the instruction typed in Create go out together, as from any
    /// reply.
    func testTheInstructionReachesTheReplyRequest() async throws {
        let reply = ReplySession(sourceMessage: "Ертең кездесуге келесің бе?", source: .clipboard,
                                 template: .builtIn(.friend, sortIndex: 0), instruction: "сыпайы бас тарт")
        let captured = CapturedReply()
        let service = AIReplyService(transportOverride: { request, prompt in
            captured.store(request: request, prompt: prompt)
            return FixedReplyTransport()
        })
        _ = try await service.generate(.init(
            message: reply.sourceMessage,
            template: reply.template,
            configuration: .initial,
            uiLanguage: .kazakh,
            instruction: reply.instruction,
            inputLanguage: .kazakh
        ))
        XCTAssertEqual(captured.request?.message, "Ертең кездесуге келесің бе?")
        XCTAssertEqual(captured.request?.instruction, "сыпайы бас тарт")
        XCTAssertTrue(captured.prompt?.user.contains("сыпайы бас тарт") ?? false)
    }
}

private final class CapturedReply: @unchecked Sendable {
    private let lock = NSLock()
    private var _request: AIReplyService.Request?
    private var _prompt: ReplyPromptBuilder.Prompt?

    var request: AIReplyService.Request? { lock.lock(); defer { lock.unlock() }; return _request }
    var prompt: ReplyPromptBuilder.Prompt? { lock.lock(); defer { lock.unlock() }; return _prompt }

    func store(request: AIReplyService.Request, prompt: ReplyPromptBuilder.Prompt) {
        lock.lock(); defer { lock.unlock() }
        _request = request
        _prompt = prompt
    }
}

private struct FixedReplyTransport: ReplyTransport {
    func generate(prompt: ReplyPromptBuilder.Prompt) async throws -> GeneratedReply {
        GeneratedReply(text: "Кешір, ертең келе алмаймын.", detectedLanguage: "kk")
    }
}

/// Validation, wire format and error mapping - no coordinator involved.
final class ComposeServiceTests: XCTestCase {

    func testValidationCountsCharactersAgainstTheServerLimit() {
        XCTAssertEqual(ComposeService.validate(instruction: "", limit: 400).composeFailure, .noInstruction)
        XCTAssertEqual(ComposeService.validate(instruction: " \n ", limit: 400).composeFailure, .noInstruction)
        XCTAssertNil(ComposeService.validate(instruction: String(repeating: "ә", count: 400), limit: 400).composeFailure)
        XCTAssertEqual(ComposeService.validate(instruction: String(repeating: "ә", count: 401), limit: 400).composeFailure,
                       .instructionTooLong(limit: 400))
    }

    /// Create works like a chat box: a one-word instruction is a request
    /// like any other, in every language.
    func testShortInstructionsAreAccepted() {
        for typed in ["да", "Иә", "yes", "ok", "вежливо откажи", "сыпайы бас тарт", "politely decline", "скажи, что согласен"] {
            guard case .success(let value) = ComposeService.validate(instruction: " \(typed)\n", limit: 400) else {
                return XCTFail("«\(typed)» must be accepted")
            }
            XCTAssertEqual(value, typed)
        }
    }

    func testWireFormatCarriesTheInstructionOnly() throws {
        let request = ComposeService.Request(instruction: "Күлжан апайды құттықта", uiLanguage: .kazakh, isRegeneration: true,
                                             inputLanguage: .kazakh, grammaticalGender: .female)
        let body = AccountComposeTransport.body(for: request, serverSupportsSenderProfile: false)
        XCTAssertEqual(body.instruction, "Күлжан апайды құттықта")
        XCTAssertEqual(body.language, "kk")
        XCTAssertTrue(body.regenerate)
        XCTAssertEqual(body.platform, "ios")

        let json = try JSONSerialization.jsonObject(with: JSONEncoder().encode(body)) as? [String: Any]
        XCTAssertEqual(Set(json?.keys ?? [:].keys), ["instruction", "language", "regenerate", "platform", "app_version"],
                       "no source_text, and nothing an older server does not know")
    }

    /// A server that knows the sender fields gets the layout and the gender -
    /// still never anything copied.
    func testSenderFieldsGoOnlyToAServerThatKnowsThem() throws {
        let request = ComposeService.Request(instruction: "Поздравь коллегу", uiLanguage: .english,
                                             inputLanguage: .russian, grammaticalGender: .female)
        let body = AccountComposeTransport.body(for: request, serverSupportsSenderProfile: true)
        XCTAssertEqual(body.input_language, "ru")
        XCTAssertEqual(body.profile, .init(grammatical_gender: "female"))

        let json = try JSONSerialization.jsonObject(with: JSONEncoder().encode(body)) as? [String: Any]
        XCTAssertEqual(Set(json?.keys ?? [:].keys),
                       ["instruction", "language", "regenerate", "input_language", "profile", "platform", "app_version"])
        XCTAssertEqual((json?["profile"] as? [String: Any])?["grammatical_gender"] as? String, "female")

        // Never asked: no profile block at all, the layout still goes.
        let neutral = AccountComposeTransport.body(
            for: ComposeService.Request(instruction: "Поздравь коллегу", uiLanguage: .english, inputLanguage: .kazakh),
            serverSupportsSenderProfile: true
        )
        XCTAssertNil(neutral.profile)
        XCTAssertEqual(neutral.input_language, "kk")
    }


    func testServerInstructionErrorsAreRecognised() {
        let headers = HTTPURLResponse(url: URL(string: "https://example.test")!, statusCode: 400, httpVersion: nil, headerFields: nil)!
        let tooLong = Data(#"{"error":{"code":"INVALID_REQUEST","details":{"field":"instruction","max_characters":350}}}"#.utf8)
        let empty = Data(#"{"error":{"code":"INVALID_REQUEST","details":{"field":"instruction"}}}"#.utf8)
        XCTAssertEqual(APIClient.mapServerError(status: 400, data: tooLong, headers: headers), .instructionTooLong(limit: 350))
        XCTAssertEqual(APIClient.mapServerError(status: 400, data: empty, headers: headers), .instructionMissing)
    }

    func testBackendErrorsBecomeComposeErrors() {
        XCTAssertEqual(AccountComposeTransport.map(.instructionTooLong(limit: 350)), .instructionTooLong(limit: 350))
        XCTAssertEqual(AccountComposeTransport.map(.instructionMissing), .noInstruction)
        XCTAssertEqual(AccountComposeTransport.map(.dailyLimitReached(limit: 7, usedToday: 7, resetsAt: nil)), .quotaExhausted)
        XCTAssertEqual(AccountComposeTransport.map(.rateLimited(retryAfter: 5)), .rateLimited)
        XCTAssertEqual(AccountComposeTransport.map(.unauthorized), .authenticationFailed)
        XCTAssertEqual(AccountComposeTransport.map(.offline), .offline)
        XCTAssertEqual(AccountComposeTransport.map(.providerTimeout), .timedOut)
        XCTAssertEqual(AccountComposeTransport.map(.providerUnavailable), .serviceUnavailable)
        XCTAssertEqual(AccountComposeTransport.map(.malformedResponse), .serviceUnavailable)
        // A server without the endpoint must not be told "message too long".
        XCTAssertEqual(AccountComposeTransport.map(.notFound), .serviceUnavailable)
        XCTAssertEqual(AccountComposeTransport.map(.invalidRequest), .serviceUnavailable)
    }

    func testComposeResponseDecodes() throws {
        let data = Data("""
        {"text": "Құрметті Күлжан апай! 🌷", "detected_language": "kk",
         "usage": {"daily_limit": 7, "used_today": 3, "remaining_today": 4,
                   "monthly_limit": 0, "used_month": 0, "resets_at": "2026-03-11T19:00:00Z", "timezone": "Asia/Almaty"}}
        """.utf8)
        let decoded = try JSONDecoder().decode(AccountAPI.ComposeResponse.self, from: data)
        XCTAssertEqual(decoded.text, "Құрметті Күлжан апай! 🌷")
        XCTAssertEqual(decoded.detectedLanguage, "kk")
    }

    // MARK: Strings

    func testCreateStringsAreTranslatedInEveryLanguage() {
        let english = ComposeStrings.forLanguage(.english)
        XCTAssertEqual(english.title, "Write with AI")
        XCTAssertEqual(ComposeStrings.forLanguage(.russian).title, "Написать с AI")
        XCTAssertEqual(ComposeStrings.forLanguage(.kazakh).title, "AI-мен жазу")
        XCTAssertEqual(english.replyToCopied, "Reply to copied")
        XCTAssertEqual(ComposeStrings.forLanguage(.russian).replyToCopied, "Ответить на скопированное")
        XCTAssertEqual(ComposeStrings.forLanguage(.kazakh).replyToCopied, "Көшірілгенге жауап беру")

        for language in AppLanguage.allCases {
            let strings = ComposeStrings.forLanguage(language)
            XCTAssertEqual(AIReplyStrings.forLanguage(language).compose.title, strings.title,
                           "AIReplyStrings.compose must follow its own language")
            let all = [strings.createButtonAccessibility, strings.title, strings.placeholder,
                       strings.write, strings.newDraft, strings.newDraftAccessibility, strings.editRequest,
                       strings.draftTitle, strings.replyToCopied, strings.noInstruction, strings.fullAccessRequired,
                       strings.instructionTooLong(limit: 400)]
            for text in all { XCTAssertFalse(text.isEmpty, "empty Create string in \(language.rawValue)") }
            XCTAssertTrue(strings.instructionTooLong(limit: 350).contains("350"))
            XCTAssertEqual(strings.intents.map(\.id), english.intents.map(\.id), "\(language.rawValue) intent set")
            for intent in strings.intents {
                XCTAssertLessThanOrEqual(intent.label.count, 18, "\(intent.id) label is too long in \(language.rawValue)")
            }
            // One line above the instruction, beside nothing: short enough
            // for the narrowest keyboard.
            XCTAssertLessThanOrEqual(strings.replyToCopied.count, 26, "\(language.rawValue) replyToCopied")
            if language != .english {
                XCTAssertNotEqual(strings.placeholder, english.placeholder)
                XCTAssertNotEqual(strings.write, english.write)
                XCTAssertNotEqual(strings.replyToCopied, english.replyToCopied)
                for (own, en) in zip(strings.intents, english.intents) {
                    XCTAssertNotEqual(own.phrase, en.phrase, "\(own.id) untranslated in \(language.rawValue)")
                }
            }
        }
    }

    func testComposeErrorsHaveSentences() {
        for language in AppLanguage.allCases {
            let strings = AIReplyStrings.forLanguage(language)
            XCTAssertEqual(strings.message(for: .noInstruction), strings.compose.noInstruction)
            XCTAssertTrue(strings.message(for: .instructionTooLong(limit: 321)).contains("321"))
        }
    }
}

private extension Result where Failure == AIReplyError {
    var composeFailure: AIReplyError? {
        if case .failure(let error) = self { return error }
        return nil
    }
}
