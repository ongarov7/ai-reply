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
        var sync = ProfileSync(configuration: model, account: AccountModel(), defaults: defaults)
        sync.send = { _ in false }

        XCTAssertFalse(sync.hasPendingChange)
        sync.choose(.female, source: .settings)
        XCTAssertEqual(model.profile.grammaticalGender, .female)
        XCTAssertTrue(sync.hasPendingChange)
    }

    private func makeModel(_ defaults: UserDefaults) -> ReplyConfigurationModel {
        ReplyConfigurationModel(store: ProfileStore(containerURL: nil, settings: SharedSettings(defaults: defaults)))
    }

    private func freshDefaults() -> UserDefaults {
        UserDefaults(suiteName: "SenderProfileTests.\(UUID())")!
    }

    /// A returning user signs in on a new phone: the account's female answer
    /// is taken at once, and the first run is built without the question.
    func testReturningUserIsNotAskedAgain() {
        let defaults = freshDefaults()
        let model = makeModel(defaults)
        XCTAssertNil(model.profile.grammaticalGender)

        ProfileSync(configuration: model, account: AccountModel(), defaults: defaults).adoptServerChoice(.female)
        XCTAssertEqual(model.profile.grammaticalGender, .female)

        let flow = OnboardingFlow.firstRun(
            profileHasGender: ProfileSync.knowsGender(local: model.profile.grammaticalGender, server: .female),
            resume: OnboardingResumeStore(defaults: defaults)
        )
        XCTAssertFalse(flow.steps.contains(.gender))
    }

    /// Even before the local copy caught up, the account's answer is enough
    /// to leave the question out; the server's default `unspecified` is not.
    func testWhoKnowsTheGender() {
        XCTAssertTrue(ProfileSync.knowsGender(local: nil, server: .male))
        XCTAssertTrue(ProfileSync.knowsGender(local: nil, server: .female))
        XCTAssertTrue(ProfileSync.knowsGender(local: .unspecified, server: nil))
        XCTAssertFalse(ProfileSync.knowsGender(local: nil, server: .unspecified))
        XCTAssertFalse(ProfileSync.knowsGender(local: nil, server: nil))
    }

    /// Skip means neutral wording - but never erases a male or female answer
    /// the profile already holds.
    func testSkipNeverErasesAKnownGender() {
        XCTAssertNil(ProfileSync.skippedAnswer(current: .male))
        XCTAssertNil(ProfileSync.skippedAnswer(current: .female))
        XCTAssertEqual(ProfileSync.skippedAnswer(current: nil), .unspecified)
        XCTAssertEqual(ProfileSync.skippedAnswer(current: .unspecified), .unspecified)
    }

    /// A choice made here and not yet confirmed wins over the account's
    /// older value.
    func testAPendingChoiceIsNotOverwrittenByTheServer() async {
        let defaults = freshDefaults()
        let model = makeModel(defaults)
        var sync = ProfileSync(configuration: model, account: AccountModel(), defaults: defaults)
        sync.send = { _ in false } // offline
        sync.choose(.male, source: .settings)
        await ProfileSync.waitForPushes(defaults: defaults)

        sync.adoptServerChoice(.female)
        XCTAssertEqual(model.profile.grammaticalGender, .male)
        XCTAssertTrue(sync.hasPendingChange)
    }

    /// Male, then Female before the first answer came back: the server's yes
    /// to Male must not clear Female, which still has to go out - and does,
    /// after Male, never at the same time.
    func testAnOlderConfirmationNeverClearsANewerChoice() async {
        let defaults = freshDefaults()
        let model = makeModel(defaults)
        let server = HeldServer()
        var sync = ProfileSync(configuration: model, account: AccountModel(), defaults: defaults)
        sync.send = { gender in await server.receive(gender) }

        sync.choose(.male, source: .settings)
        await server.waitForRequest(count: 1)
        sync.choose(.female, source: .settings)

        server.answer(true) // Male confirmed
        await server.waitForRequest(count: 2)
        XCTAssertTrue(sync.hasPendingChange, "Female is not confirmed yet")
        sync.adoptServerChoice(.male)
        XCTAssertEqual(model.profile.grammaticalGender, .female, "the server's Male does not come back")

        server.answer(true) // Female confirmed
        await ProfileSync.waitForPushes(defaults: defaults)
        XCTAssertEqual(server.sent, [.male, .female])
        XCTAssertEqual(server.maximumInFlight, 1)
        XCTAssertFalse(sync.hasPendingChange)
        XCTAssertEqual(model.profile.grammaticalGender, .female)
    }

    func testOnlyTheLatestRevisionClearsThePendingMark() {
        let pending = PendingProfileChange(defaults: freshDefaults())
        let first = pending.markChanged()
        let second = pending.markChanged()
        pending.confirm(revision: first)
        XCTAssertTrue(pending.isPending)
        pending.confirm(revision: second)
        XCTAssertFalse(pending.isPending)
    }

    /// Sign-out drops a change the account never got: the next account on
    /// this phone keeps its own answer and is not sent the old one.
    func testSignOutDropsAnUnsentChange() async {
        let defaults = freshDefaults()
        let model = makeModel(defaults)
        var sync = ProfileSync(configuration: model, account: AccountModel(), defaults: defaults)
        sync.send = { _ in false }
        sync.choose(.female, source: .settings)
        await ProfileSync.waitForPushes(defaults: defaults)
        XCTAssertTrue(sync.hasPendingChange)

        ProfileSync.discardPendingChange(defaults: defaults)
        XCTAssertFalse(sync.hasPendingChange)
        // The next account's own choice is taken over.
        sync.adoptServerChoice(.male)
        XCTAssertEqual(model.profile.grammaticalGender, .male)
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
        var sync = ProfileSync(configuration: model, account: AccountModel(), defaults: defaults)
        sync.send = { _ in false }
        sync.choose(.male, source: .onboarding)

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

/// A server that answers each gender PATCH only when the test says so, and
/// remembers how many were open at once.
@MainActor
private final class HeldServer {
    private(set) var sent: [GrammaticalGender] = []
    private(set) var maximumInFlight = 0
    private var inFlight = 0
    private var waiting: [CheckedContinuation<Bool, Never>] = []

    func receive(_ gender: GrammaticalGender) async -> Bool {
        sent.append(gender)
        inFlight += 1
        maximumInFlight = max(maximumInFlight, inFlight)
        let confirmed = await withCheckedContinuation { waiting.append($0) }
        inFlight -= 1
        return confirmed
    }

    func answer(_ confirmed: Bool) {
        guard !waiting.isEmpty else { return XCTFail("no request is waiting") }
        waiting.removeFirst().resume(returning: confirmed)
    }

    func waitForRequest(count: Int) async {
        let deadline = Date().addingTimeInterval(2)
        while sent.count < count || waiting.isEmpty {
            guard Date() < deadline else { return XCTFail("request \(count) never came") }
            try? await Task.sleep(for: .milliseconds(5))
        }
    }
}
