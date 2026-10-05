import XCTest
@testable import AIReply

/// The cleaner version of the user's instruction (DESIGN §2.6, §5.5): when it
/// is asked for, what goes over the wire, and how the suggestion behaves on
/// screen - it waits for a pause, never replaces newer text, offers Undo and
/// gives up after two failures.
@MainActor
final class InstructionPolishTests: XCTestCase {

    // MARK: When to ask

    func testOnlyNotesWorthPolishingAreSent() {
        let ask = { (text: String) in InstructionPolishRules.shouldRequest(text, lastRequested: nil, limit: 400) }
        XCTAssertTrue(ask("ответь ему вежливо я сегодня не могу"))
        XCTAssertTrue(ask("бүгін бос емес екенімді айт"))
        XCTAssertFalse(ask("ок"), "too short")
        XCTAssertFalse(ask("да да да"), "three words, but under twelve characters")
        XCTAssertFalse(ask("ответьвежливопожалуйста"), "one word")
        XCTAssertFalse(ask("12 34 56 78 90 12"), "no words at all")
        XCTAssertFalse(ask("   \n "))
        XCTAssertFalse(InstructionPolishRules.shouldRequest("ответь ему вежливо", lastRequested: "ответь ему вежливо", limit: 400),
                       "asked already")
        XCTAssertFalse(InstructionPolishRules.shouldRequest("ответь ему вежливо", lastRequested: nil, limit: 10), "over the limit")
    }

    // MARK: Wire

    func testWireFormat() throws {
        let descriptor = DeviceDescriptor(device_id: "d", platform: "ios", app_version: "1.2", os_version: "18.0", locale: "ru", timezone: "Asia/Almaty")
        let body = AccountPolishTransport.body(
            for: PolishService.Request(text: "бүгін бос емеспін", inputLanguage: .kazakh),
            descriptor: descriptor
        )
        let sent = try XCTUnwrap(JSONSerialization.jsonObject(with: JSONEncoder().encode(body)) as? [String: Any])
        XCTAssertEqual(Set(sent.keys), ["text", "input_language", "platform", "app_version"], "no device id, no profile, no message")
        XCTAssertEqual(sent["text"] as? String, "бүгін бос емеспін")
        XCTAssertEqual(sent["input_language"] as? String, "kk")
        XCTAssertEqual(sent["platform"] as? String, "ios")

        let plain = AccountPolishTransport.body(for: PolishService.Request(text: "x", inputLanguage: nil), descriptor: descriptor)
        let encoded = try XCTUnwrap(String(data: JSONEncoder().encode(plain), encoding: .utf8))
        XCTAssertFalse(encoded.contains("input_language"))
    }

    // MARK: Service

    func testOnlyARealChangeIsASuggestion() async throws {
        let note = "ответь ему вежливо я сегодня не могу"
        let unchanged = PolishService(transportOverride: FakePolishTransport { _ in .init(text: note, changed: false) })
        let same = PolishService(transportOverride: FakePolishTransport { _ in .init(text: "  \(note) ", changed: true) })
        let better = PolishService(transportOverride: FakePolishTransport { _ in .init(text: "Ответь ему вежливо: я сегодня не могу.", changed: true) })

        let unchangedResult = try await unchanged.polish(.init(text: note, inputLanguage: .russian))
        let sameResult = try await same.polish(.init(text: note, inputLanguage: .russian))
        let betterResult = try await better.polish(.init(text: "  \(note)", inputLanguage: .russian))
        XCTAssertNil(unchangedResult)
        XCTAssertNil(sameResult)
        XCTAssertEqual(betterResult, "Ответь ему вежливо: я сегодня не могу.")
    }

    func testTheTrimmedNoteIsSent() async throws {
        let transport = FakePolishTransport { request in .init(text: request.text, changed: false) }
        _ = try await PolishService(transportOverride: transport).polish(.init(text: "  ответь ему вежливо \n", inputLanguage: .russian))
        XCTAssertEqual(transport.requests.map(\.text), ["ответь ему вежливо"])
    }

    // MARK: On screen

    private let note = "ответь ему вежливо я сегодня не могу"
    private let polished = "Ответь ему вежливо: я сегодня не могу."

    private func makePolisher(_ transport: FakePolishTransport, undoWindow: Duration = .seconds(5)) -> InstructionPolisher {
        let polisher = InstructionPolisher(service: PolishService(transportOverride: transport), pause: .milliseconds(20), undoWindow: undoWindow)
        polisher.isEnabled = true
        return polisher
    }

    func testSuggestionAppearsAfterAPauseAndIsUsedOnATap() async throws {
        let transport = FakePolishTransport { [polished] _ in .init(text: polished, changed: true) }
        let polisher = makePolisher(transport)
        polisher.instructionDidChange(note, inputLanguage: .russian, limit: 400)
        XCTAssertEqual(polisher.chip, .none, "nothing before the pause")

        try await waitUntil { polisher.chip == .suggestion(self.polished) }
        XCTAssertEqual(transport.requests, [PolishService.Request(text: note, inputLanguage: .russian)])

        XCTAssertEqual(polisher.accept(replacing: note), polished)
        XCTAssertEqual(polisher.chip, .undo)
        // The keyboard puts the suggestion in the field; that edit keeps Undo.
        polisher.instructionDidChange(polished, inputLanguage: .russian, limit: 400)
        XCTAssertEqual(polisher.chip, .undo)

        XCTAssertEqual(polisher.undo(), note)
        XCTAssertEqual(polisher.chip, .none)
        // Putting the user's text back does not ask about it again.
        polisher.instructionDidChange(note, inputLanguage: .russian, limit: 400)
        try await Task.sleep(for: .milliseconds(80))
        XCTAssertEqual(transport.requests.count, 1)
        XCTAssertEqual(polisher.chip, .none)
    }

    func testUndoGoesAwayAfterAWhile() async throws {
        let transport = FakePolishTransport { [polished] _ in .init(text: polished, changed: true) }
        let polisher = makePolisher(transport, undoWindow: .milliseconds(60))
        polisher.instructionDidChange(note, inputLanguage: .russian, limit: 400)
        try await waitUntil { polisher.chip == .suggestion(self.polished) }
        _ = polisher.accept(replacing: note)
        XCTAssertEqual(polisher.chip, .undo)
        try await waitUntil { polisher.chip == .none }
        XCTAssertNil(polisher.undo())
    }

    func testAnEditHidesTheSuggestionAndDropsAnAnswerOnItsWay() async throws {
        let transport = FakePolishTransport(delay: .milliseconds(150)) { request in
            .init(text: request.text.capitalized, changed: true)
        }
        let polisher = makePolisher(transport)
        polisher.instructionDidChange(note, inputLanguage: .russian, limit: 400)
        try await waitUntil { transport.requests.count == 1 }
        let newer = note + " завтра могу"
        polisher.instructionDidChange(newer, inputLanguage: .russian, limit: 400)
        try await waitUntil { polisher.chip != .none }
        XCTAssertEqual(polisher.chip, .suggestion(newer.capitalized), "only the answer about the newest text is shown")

        polisher.instructionDidChange(newer + " ок", inputLanguage: .russian, limit: 400)
        XCTAssertEqual(polisher.chip, .none, "any edit hides it")
    }

    func testTwoFailuresInARowStopAsking() async throws {
        let transport = FakePolishTransport { _ in throw AIReplyError.serviceUnavailable }
        let polisher = makePolisher(transport)
        polisher.instructionDidChange(note, inputLanguage: .russian, limit: 400)
        try await waitUntil { transport.requests.count == 1 }
        polisher.instructionDidChange(note + " завтра", inputLanguage: .russian, limit: 400)
        try await waitUntil { transport.requests.count == 2 }
        try await Task.sleep(for: .milliseconds(30))
        polisher.instructionDidChange(note + " завтра могу", inputLanguage: .russian, limit: 400)
        try await Task.sleep(for: .milliseconds(120))
        XCTAssertEqual(transport.requests.count, 2, "stopped after two failures")
        XCTAssertEqual(polisher.chip, .none, "failures are silent")

        polisher.reset()
        polisher.instructionDidChange(note + " послезавтра", inputLanguage: .russian, limit: 400)
        try await waitUntil { transport.requests.count == 3 }
    }

    func testNothingIsAskedWhileSwitchedOff() async throws {
        let transport = FakePolishTransport { [polished] _ in .init(text: polished, changed: true) }
        let polisher = makePolisher(transport)
        polisher.isEnabled = false
        polisher.instructionDidChange(note, inputLanguage: .russian, limit: 400)
        try await Task.sleep(for: .milliseconds(120))
        XCTAssertTrue(transport.requests.isEmpty)
    }

    func testASuggestionOverTheLimitIsNotOffered() async throws {
        let transport = FakePolishTransport { request in .init(text: request.text + String(repeating: "!", count: 50), changed: true) }
        let polisher = makePolisher(transport)
        polisher.instructionDidChange(note, inputLanguage: .russian, limit: 50)
        try await waitUntil { transport.requests.count == 1 }
        try await Task.sleep(for: .milliseconds(60))
        XCTAssertEqual(polisher.chip, .none)
    }

    // MARK: Helpers

    private func waitUntil(timeout: TimeInterval = 2, _ condition: @MainActor () -> Bool) async throws {
        let deadline = Date().addingTimeInterval(timeout)
        while !condition() {
            guard Date() < deadline else {
                XCTFail("condition not met in time")
                return
            }
            try await Task.sleep(for: .milliseconds(5))
        }
    }
}

/// Answers polish requests from a closure and records them.
private final class FakePolishTransport: PolishTransport, @unchecked Sendable {

    private let lock = NSLock()
    private var recorded: [PolishService.Request] = []
    private let delay: Duration
    private let answer: @Sendable (PolishService.Request) throws -> PolishService.Result

    init(delay: Duration = .zero, answer: @escaping @Sendable (PolishService.Request) throws -> PolishService.Result) {
        self.delay = delay
        self.answer = answer
    }

    var requests: [PolishService.Request] {
        lock.lock()
        defer { lock.unlock() }
        return recorded
    }

    func polish(_ request: PolishService.Request) async throws -> PolishService.Result {
        lock.withLock { recorded.append(request) }
        if delay > .zero { try await Task.sleep(for: delay) }
        return try answer(request)
    }
}
