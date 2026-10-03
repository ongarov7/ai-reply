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

    func testWireFormatCarriesTheInstructionOnly() throws {
        let request = ComposeService.Request(instruction: "Күлжан апайды құттықта", uiLanguage: .kazakh, isRegeneration: true)
        let body = AccountComposeTransport.body(for: request)
        XCTAssertEqual(body.instruction, "Күлжан апайды құттықта")
        XCTAssertEqual(body.language, "kk")
        XCTAssertTrue(body.regenerate)
        XCTAssertEqual(body.platform, "ios")

        let json = try JSONSerialization.jsonObject(with: JSONEncoder().encode(body)) as? [String: Any]
        XCTAssertEqual(Set(json?.keys ?? [:].keys), ["instruction", "language", "regenerate", "platform", "app_version"],
                       "no source_text, no profile: compose sends nothing copied")
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

        for language in AppLanguage.allCases {
            let strings = ComposeStrings.forLanguage(language)
            XCTAssertEqual(AIReplyStrings.forLanguage(language).compose.title, strings.title,
                           "AIReplyStrings.compose must follow its own language")
            let all = [strings.createButtonAccessibility, strings.title, strings.placeholder,
                       strings.write, strings.newDraft, strings.newDraftAccessibility, strings.editRequest,
                       strings.draftTitle, strings.noInstruction, strings.fullAccessRequired,
                       strings.instructionTooLong(limit: 400)]
            for text in all { XCTAssertFalse(text.isEmpty, "empty Create string in \(language.rawValue)") }
            XCTAssertTrue(strings.instructionTooLong(limit: 350).contains("350"))
            XCTAssertEqual(strings.intents.map(\.id), english.intents.map(\.id), "\(language.rawValue) intent set")
            for intent in strings.intents {
                XCTAssertLessThanOrEqual(intent.label.count, 18, "\(intent.id) label is too long in \(language.rawValue)")
            }
            if language != .english {
                XCTAssertNotEqual(strings.placeholder, english.placeholder)
                XCTAssertNotEqual(strings.write, english.write)
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
