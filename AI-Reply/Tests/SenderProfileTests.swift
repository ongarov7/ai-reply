import XCTest
@testable import AIReply

/// The sender's grammatical gender and the keyboard layout: what a reply
/// request carries, to which server, and how the choice is kept in step with
/// the server.
@MainActor
final class SenderProfileTests: XCTestCase {

    // MARK: Reply request

    private func context(profile: AccountReplyTransport.Profile?) -> AccountReplyTransport.RequestContext {
        AccountReplyTransport.RequestContext(
            message: "Ты завтра свободен?",
            instruction: "Скажи, что вечером свободен",
            templateID: "friend",
            templateName: "Друг",
            templateRelationship: "friend",
            templateTone: .friendly,
            templateInstructions: "",
            templateReplyLength: .short,
            templateEmojiPolicy: .allowed,
            templateWorkingHoursBehaviour: .ignore,
            templateBusiness: nil,
            appLanguage: "ru",
            inputLanguage: "ru",
            business: nil,
            profile: profile
        )
    }

    private func json(_ body: AccountReplyTransport.Body) throws -> [String: Any] {
        try XCTUnwrap(JSONSerialization.jsonObject(with: JSONEncoder().encode(body)) as? [String: Any])
    }

    /// An older server rejects unknown fields outright - and the user would be
    /// told their message is too long. Nothing new goes without the flag.
    func testOlderServerGetsNoSenderFields() throws {
        var profile = UserProfile.empty
        profile.grammaticalGender = .male
        let body = AccountReplyTransport.body(
            for: context(profile: AIReplyService.profile(from: profile)),
            serverSupportsPreferences: true,
            serverSupportsSenderProfile: false
        )
        let sent = try json(body)
        XCTAssertNil(sent["input_language"])
        XCTAssertNil((sent["profile"] as? [String: Any])?["grammatical_gender"])
    }

    func testCurrentServerGetsGenderAndLayout() throws {
        var profile = UserProfile.empty
        profile.grammaticalGender = .female
        let body = AccountReplyTransport.body(
            for: context(profile: AIReplyService.profile(from: profile)),
            serverSupportsPreferences: true,
            serverSupportsSenderProfile: true
        )
        let sent = try json(body)
        XCTAssertEqual(sent["input_language"] as? String, "ru")
        XCTAssertEqual((sent["profile"] as? [String: Any])?["grammatical_gender"] as? String, "female")
        XCTAssertEqual(sent["source_text"] as? String, "Ты завтра свободен?")
    }

    /// A gender alone is still a profile worth sending; "never asked" is not.
    func testGenderAloneKeepsTheProfileBlock() {
        var profile = UserProfile.empty
        XCTAssertNil(AIReplyService.profile(from: profile))
        profile.grammaticalGender = .unspecified
        XCTAssertEqual(AIReplyService.profile(from: profile)?.grammaticalGender, .unspecified)
    }

    /// A Kazakh message gets a Kazakh reply, whatever language the phone, the
    /// app or the layout is in. A profile left on Auto - which every profile
    /// starts on - names no reply language, so nothing in the request outranks
    /// the copied message; the app language rides along only as the server's
    /// last resort for a message too short to tell.
    func testAutoReplyLanguageLeavesTheLanguageToTheMessage() throws {
        XCTAssertNil(UserProfile.empty.replyLanguage, "a new profile is on Auto")
        let decoded = try JSONDecoder().decode(UserProfile.self, from: Data(#"{"role": "Дизайнер"}"#.utf8))
        XCTAssertNil(decoded.replyLanguage, "an older profile without the field is on Auto")

        var profile = UserProfile.empty
        profile.setRole("Дизайнер")
        var request = context(profile: AIReplyService.profile(from: profile))
        // Russian app, Russian layout, Russian instruction, Kazakh message.
        request.message = "Сәлем! Ертең кездесуге уақытың бар ма?"
        request.inputLanguage = "ru"
        let sent = try json(AccountReplyTransport.body(
            for: request,
            serverSupportsPreferences: true,
            serverSupportsSenderProfile: true
        ))
        XCTAssertNil((sent["profile"] as? [String: Any])?["reply_language"])
        XCTAssertEqual(sent["source_text"] as? String, "Сәлем! Ертең кездесуге уақытың бар ма?")
    }

    func testNeverAskedSendsNoGender() throws {
        var profile = UserProfile.empty
        profile.setRole("Дизайнер")
        let body = AccountReplyTransport.body(
            for: context(profile: AIReplyService.profile(from: profile)),
            serverSupportsPreferences: true,
            serverSupportsSenderProfile: true
        )
        let sent = try json(body)
        XCTAssertNotNil(sent["profile"])
        XCTAssertNil((sent["profile"] as? [String: Any])?["grammatical_gender"])
    }

    // MARK: Sync

    /// Only an actual choice made elsewhere is taken: `unspecified` is the
    /// server's default for every account and would erase the local answer.
    func testServerValueIsAdoptedOnlyWhenItIsAChoice() {
        XCTAssertNil(ProfileSync.adopted(local: .female, server: nil))
        XCTAssertNil(ProfileSync.adopted(local: .female, server: .unspecified))
        XCTAssertNil(ProfileSync.adopted(local: nil, server: .unspecified))
        XCTAssertNil(ProfileSync.adopted(local: .male, server: .male))
        XCTAssertEqual(ProfileSync.adopted(local: nil, server: .male), .male)
        XCTAssertEqual(ProfileSync.adopted(local: .unspecified, server: .female), .female)
        XCTAssertEqual(ProfileSync.adopted(local: .male, server: .female), .female)
    }

    /// A choice is the device's at once and stays pending until the server
    /// confirms it.
    func testChoosingKeepsTheValueLocallyAndMarksItPending() {
        let defaults = UserDefaults(suiteName: "SenderProfileTests.\(UUID())")!
        let settings = SharedSettings(defaults: defaults)
        let model = ReplyConfigurationModel(store: ProfileStore(containerURL: nil, settings: settings))
        let sync = ProfileSync(configuration: model, account: AccountModel(), defaults: defaults)

        XCTAssertFalse(sync.hasPendingChange)
        sync.choose(.female, source: .settings)
        XCTAssertEqual(model.profile.grammaticalGender, .female)
        XCTAssertTrue(sync.hasPendingChange)
    }

    // MARK: Events

    private final class RecordingSink: ProductEventSink {
        var names: [String] = []
        var props: [[String: ProductEventValue]] = []
        func record(_ name: String, _ props: [String: ProductEventValue]) {
            names.append(name)
            self.props.append(props)
        }
    }

    /// The gender event says where and whether it was skipped - never which.
    func testGenderEventNeverCarriesTheValue() {
        let sink = RecordingSink()
        let previous = ProductEvents.sink
        ProductEvents.sink = sink
        defer { ProductEvents.sink = previous }

        let defaults = UserDefaults(suiteName: "SenderProfileTests.\(UUID())")!
        let model = ReplyConfigurationModel(store: ProfileStore(containerURL: nil, settings: SharedSettings(defaults: defaults)))
        ProfileSync(configuration: model, account: AccountModel(), defaults: defaults).choose(.male, source: .onboarding)

        XCTAssertEqual(sink.names, ["gender_selected"])
        XCTAssertEqual(sink.props.first, ["source": .code("onboarding"), "skipped": .bool(false)])
    }

    func testOnceEventsAreRecordedOncePerInstall() {
        let sink = RecordingSink()
        let previous = ProductEvents.sink
        ProductEvents.sink = sink
        defer { ProductEvents.sink = previous }

        let defaults = UserDefaults(suiteName: "SenderProfileTests.\(UUID())")!
        ProductEvents.trackOnce(.keyboardEnabledDetected, defaults: defaults)
        ProductEvents.trackOnce(.keyboardEnabledDetected, defaults: defaults)
        XCTAssertEqual(sink.names, ["keyboard_enabled_detected"])
    }
}

/// The product events reporter: when batches go out, what they carry, and that
/// nothing goes to a signed-out user or a server that did not ask for events.
@MainActor
final class ProductEventReporterTests: XCTestCase {

    /// Records every batch; fails the sends it was told to.
    private actor StubTransport: ProductEventTransport {
        private(set) var batches: [ProductEventBatch] = []
        private var failures: [Error]

        init(failing failures: [Error] = []) {
            self.failures = failures
        }

        func send(_ batch: ProductEventBatch) async throws {
            batches.append(batch)
            if !failures.isEmpty { throw failures.removeFirst() }
        }

        /// The `version` prop of every event sent, in order.
        var sentNumbers: [Int] {
            batches.flatMap(\.events).compactMap {
                if case .int(let number)? = $0.props["version"] { return number }
                return nil
            }
        }
    }

    private let moment = Date(timeIntervalSince1970: 1_790_000_000)

    private func reporter(
        _ transport: StubTransport,
        policy: ProductEventReporter.Policy = .init(),
        allowed: Bool = true
    ) -> ProductEventReporter {
        ProductEventReporter(
            transport: transport,
            policy: policy,
            appVersion: "1.4",
            isAllowed: { allowed },
            now: { [moment] in moment }
        )
    }

    /// A policy whose timer never fires during a test.
    private var manualPolicy: ProductEventReporter.Policy {
        var policy = ProductEventReporter.Policy()
        policy.flushThreshold = 1000
        policy.interval = .seconds(3600)
        return policy
    }

    private func record(_ count: Int, into reporter: ProductEventReporter, from first: Int = 0) {
        for number in first..<(first + count) {
            reporter.record("onboarding_started", ["version": .int(number)])
        }
    }

    func testTenEventsSendABatch() async {
        let transport = StubTransport()
        let reporter = reporter(transport)

        record(9, into: reporter)
        var sent = await transport.batches
        XCTAssertTrue(sent.isEmpty, "nine wait for the timer")

        record(1, into: reporter, from: 9)
        await reporter.flush().value
        sent = await transport.batches
        XCTAssertEqual(sent.count, 1)
        XCTAssertEqual(sent.first?.events.count, 10)
        XCTAssertEqual(sent.first?.platform, "ios")
        XCTAssertEqual(sent.first?.app_version, "1.4")
        XCTAssertEqual(reporter.waitingCount, 0)
    }

    func testBatchesHoldAtMostTwentyEvents() async {
        let transport = StubTransport()
        let reporter = reporter(transport, policy: manualPolicy)
        record(45, into: reporter)
        await reporter.flush().value

        let sizes = await transport.batches.map(\.events.count)
        XCTAssertEqual(sizes, [20, 20, 5])
        let numbers = await transport.sentNumbers
        XCTAssertEqual(numbers, Array(0..<45), "in the order they happened")
    }

    func testAtMostAHundredWaitAndTheOldestGo() async {
        let transport = StubTransport()
        let reporter = reporter(transport, policy: manualPolicy)
        record(130, into: reporter)
        XCTAssertEqual(reporter.waitingCount, 100)

        await reporter.flush().value
        let numbers = await transport.sentNumbers
        XCTAssertEqual(numbers, Array(30..<130))
    }

    func testTheTimerSendsWhatWaits() async throws {
        let transport = StubTransport()
        var policy = ProductEventReporter.Policy()
        policy.interval = .milliseconds(20)
        let reporter = reporter(transport, policy: policy)
        record(1, into: reporter)

        var sent = await transport.batches
        for _ in 0..<100 where sent.isEmpty {
            try await Task.sleep(for: .milliseconds(20))
            sent = await transport.batches
        }
        XCTAssertEqual(sent.count, 1)
        XCTAssertEqual(reporter.waitingCount, 0)
    }

    /// Signed out, or a server without `product_events`: nothing is sent, and
    /// nothing is kept to be sent for someone else later.
    func testNothingGoesWhenNotAllowed() async {
        let transport = StubTransport()
        let reporter = reporter(transport, allowed: false)
        record(12, into: reporter)
        await reporter.flush().value

        let sent = await transport.batches
        XCTAssertTrue(sent.isEmpty)
        XCTAssertEqual(reporter.waitingCount, 0)
    }

    /// A network failure gets one more try with the next send, never a third.
    func testANetworkFailureIsRetriedOnce() async {
        let transport = StubTransport(failing: [APIError.offline, APIError.offline])
        let reporter = reporter(transport, policy: manualPolicy)
        record(3, into: reporter)

        await reporter.flush().value
        XCTAssertEqual(reporter.waitingCount, 3, "kept for one more try")
        await reporter.flush().value
        XCTAssertEqual(reporter.waitingCount, 0, "dropped after the second failure")

        record(1, into: reporter, from: 3)
        await reporter.flush().value
        let sizes = await transport.batches.map(\.events.count)
        XCTAssertEqual(sizes, [3, 3, 1])
    }

    /// A refusal would only repeat: dropped at once.
    func testARefusalIsNotRetried() async {
        let transport = StubTransport(failing: [APIError.invalidRequest])
        let reporter = reporter(transport, policy: manualPolicy)
        record(2, into: reporter)
        await reporter.flush().value
        XCTAssertEqual(reporter.waitingCount, 0)
    }

    func testWireFormat() throws {
        let batch = ProductEventBatch(platform: "ios", app_version: "1.4", events: [
            .init(name: "onboarding_completed", ts: moment.formatted(.iso8601),
                  props: ["version": .int(2), "skipped": .bool(false)]),
            .init(name: "onboarding_step_viewed", ts: moment.formatted(.iso8601),
                  props: ["step": .code("fullAccess")])
        ])
        let json = try XCTUnwrap(JSONSerialization.jsonObject(with: JSONEncoder().encode(batch)) as? [String: Any])
        XCTAssertEqual(Set(json.keys), ["platform", "app_version", "events"])
        let events = try XCTUnwrap(json["events"] as? [[String: Any]])
        XCTAssertEqual(events[0]["name"] as? String, "onboarding_completed")
        XCTAssertEqual(events[0]["ts"] as? String, "2026-09-21T14:13:20Z")
        let props = try XCTUnwrap(events[0]["props"] as? [String: Any])
        XCTAssertEqual(props["version"] as? Int, 2)
        XCTAssertEqual(props["skipped"] as? Bool, false)
        XCTAssertEqual((events[1]["props"] as? [String: Any])?["step"] as? String, "fullAccess")
    }

    /// Only a server that publishes the flag gets events.
    func testServerSwitchFollowsTheConfig() throws {
        let defaults = UserDefaults(suiteName: "ProductEventReporterTests.\(UUID())")!
        XCTAssertFalse(ProductEvents.serverAcceptsEvents(defaults: defaults))

        let on = try JSONDecoder().decode(AccountAPI.Features.self, from: Data(#"{"product_events": true}"#.utf8))
        ProductEvents.storeServerSupport(on, defaults: defaults)
        XCTAssertTrue(ProductEvents.serverAcceptsEvents(defaults: defaults))

        let older = try JSONDecoder().decode(AccountAPI.Features.self, from: Data("{}".utf8))
        ProductEvents.storeServerSupport(older, defaults: defaults)
        XCTAssertFalse(ProductEvents.serverAcceptsEvents(defaults: defaults))
    }
}
