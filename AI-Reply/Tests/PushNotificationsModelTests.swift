import XCTest
@testable import AIReply

/// The notification UI's rules: when the soft card appears, what asking for
/// permission does, and a category switch that goes back when the server
/// refuses it.
///
/// Хабарлама күйі: карточка, рұқсат сұрау, санаттардың қайтарылуы.
final class PushNotificationsModelTests: XCTestCase {

    // MARK: Fakes

    private final class FakeAuthorizer: NotificationAuthorizing, @unchecked Sendable {
        var permission: NotificationPermission = .notDetermined
        var answer = true
        var answerStatus: NotificationPermission = .authorized
        private(set) var requests = 0

        func currentPermission() async -> NotificationPermission { permission }

        func requestPermission() async {
            requests += 1
            permission = answer ? answerStatus : .denied
        }
    }

    private final class FakePreferences: NotificationPreferencesService, @unchecked Sendable {
        var stored = NotificationPreferences.defaults
        var failSaves = false
        private(set) var saves: [[String: Bool]] = []

        func load() async throws -> NotificationPreferences { stored }

        func save(_ changes: [String: Bool]) async throws -> NotificationPreferences {
            saves.append(changes)
            if failSaves { throw APIError.offline }
            for (key, value) in changes { stored.values[key] = value }
            return stored
        }
    }

    private var suiteName = ""
    private var defaults: UserDefaults!

    override func setUp() {
        super.setUp()
        suiteName = "PushNotificationsModelTests.\(UUID().uuidString)"
        defaults = UserDefaults(suiteName: suiteName)
    }

    override func tearDown() {
        defaults.removePersistentDomain(forName: suiteName)
        super.tearDown()
    }

    @MainActor
    private func model(authorizer: FakeAuthorizer = FakeAuthorizer(),
                       preferences: FakePreferences = FakePreferences(),
                       buildSupportsPush: Bool = true) -> PushNotificationsModel {
        PushNotificationsModel(authorizer: authorizer, preferencesService: preferences,
                               store: NotificationSettingsStore(defaults: defaults),
                               buildSupportsPush: buildSupportsPush)
    }

    // MARK: Card

    @MainActor
    func testTheCardNeedsAnAccountAPushCapableBuildAndServerAndNoAnswerYet() async {
        let authorizer = FakeAuthorizer()
        let model = model(authorizer: authorizer)
        await model.refreshPermission()
        XCTAssertFalse(model.showsPermissionCard, "signed out")

        model.setSignedIn(true)
        XCTAssertFalse(model.showsPermissionCard, "the server has not said it can send pushes")

        model.setServerDeliversPush(true)
        XCTAssertTrue(model.showsPermissionCard)

        authorizer.permission = .denied
        await model.refreshPermission()
        XCTAssertFalse(model.showsPermissionCard, "iOS has an answer already")

        let debugBuild = self.model(buildSupportsPush: false)
        debugBuild.setSignedIn(true)
        debugBuild.setServerDeliversPush(true)
        await debugBuild.refreshPermission()
        XCTAssertFalse(debugBuild.showsPermissionCard, "a build without Firebase never asks")
        XCTAssertFalse(debugBuild.showsNotificationSettings)
    }

    /// `AppServices` learns at launch whether Firebase is configured.
    @MainActor
    func testTheBuildCanBecomePushCapableAtLaunch() async {
        let model = model(buildSupportsPush: false)
        model.setSignedIn(true)
        model.setServerDeliversPush(true)
        await model.refreshPermission()
        XCTAssertFalse(model.isPushAvailable)
        model.setBuildSupportsPush(true)
        XCTAssertTrue(model.isPushAvailable)
        XCTAssertTrue(model.showsPermissionCard)
    }

    @MainActor
    func testAnOlderServerShowsNothing() async {
        let model = model()
        model.setSignedIn(true)
        await model.refreshPermission()
        XCTAssertFalse(model.showsNotificationSettings, "the server has not answered yet")
        model.setServerDeliversPush(false)
        XCTAssertFalse(model.showsNotificationSettings, "a server without FCM")
        XCTAssertFalse(model.showsPermissionCard)
    }

    @MainActor
    func testNotNowIsRemembered() async {
        let first = model()
        first.setSignedIn(true)
        first.setServerDeliversPush(true)
        await first.refreshPermission()
        first.dismissCard()
        XCTAssertFalse(first.showsPermissionCard)

        let nextLaunch = model()
        nextLaunch.setSignedIn(true)
        nextLaunch.setServerDeliversPush(true)
        await nextLaunch.refreshPermission()
        XCTAssertFalse(nextLaunch.showsPermissionCard)
        XCTAssertTrue(nextLaunch.showsNotificationSettings, "Settings keeps the way to turn them on")
    }

    @MainActor
    func testForcedDebugCardShowsWhateverTheState() {
        let forced = PushNotificationsModel(authorizer: FakeAuthorizer(), preferencesService: FakePreferences(),
                                            store: NotificationSettingsStore(defaults: defaults),
                                            buildSupportsPush: false, forcesPushUI: true)
        XCTAssertTrue(forced.showsPermissionCard)
        XCTAssertTrue(forced.showsNotificationSettings)
        XCTAssertTrue(forced.showsCategories)
        forced.dismissCard()
        XCTAssertFalse(forced.showsPermissionCard)
        XCTAssertFalse(NotificationSettingsStore(defaults: defaults).isPermissionCardDismissed,
                       "a forced launch does not remember the dismissal")
    }

    // MARK: Token

    /// A backup restored onto another iPhone carries these defaults but not
    /// the installation id; the old phone's token must not be registered there.
    func testATokenIsReadOnlyForTheInstallationItWasHandedTo() {
        let store = NotificationSettingsStore(defaults: defaults)
        let token = "fcm-test-token:" + String(repeating: "Ab1_-", count: 30)
        store.setPushToken(token, installationID: "0b7c9a52-0000-4000-8000-000000000001")
        XCTAssertEqual(store.pushToken(for: "0b7c9a52-0000-4000-8000-000000000001"), token)
        XCTAssertNil(store.pushToken(for: "0b7c9a52-0000-4000-8000-000000000002"), "another installation")

        store.setPushToken(nil, installationID: "0b7c9a52-0000-4000-8000-000000000001")
        XCTAssertNil(store.pushToken(for: "0b7c9a52-0000-4000-8000-000000000001"))
    }

    /// An APNs token an earlier development build kept is never sent as an
    /// FCM token: the FCM token lives under a key of its own.
    func testAnOldAPNsTokenIsNotReadAsAnFCMToken() throws {
        let old = try JSONEncoder().encode(["installationID": "0b7c9a52-0000-4000-8000-000000000001",
                                            "token": String(repeating: "ab", count: 32)])
        defaults.set(old, forKey: "push.deviceToken.v2")
        let store = NotificationSettingsStore(defaults: defaults)
        XCTAssertNil(store.pushToken(for: "0b7c9a52-0000-4000-8000-000000000001"))
    }

    // MARK: Asking

    @MainActor
    func testAllowingAsksForATokenAndRegistersAgain() async {
        let authorizer = FakeAuthorizer()
        let model = model(authorizer: authorizer)
        var tokenRequests = 0
        var changes = 0
        model.onPermissionAllowsDelivery = { tokenRequests += 1 }
        model.onStateChange = { changes += 1 }
        await model.refreshPermission()
        changes = 0

        await model.requestPermission()
        XCTAssertEqual(authorizer.requests, 1)
        XCTAssertEqual(model.permission, .authorized)
        XCTAssertEqual(tokenRequests, 1)
        XCTAssertEqual(changes, 1, "the installation is registered again with the new permission")
    }

    @MainActor
    func testDenyingAsksForNoToken() async {
        let authorizer = FakeAuthorizer()
        authorizer.answer = false
        let model = model(authorizer: authorizer)
        var tokenRequests = 0
        model.onPermissionAllowsDelivery = { tokenRequests += 1 }

        await model.requestPermission()
        XCTAssertEqual(model.permission, .denied)
        XCTAssertEqual(tokenRequests, 0)
    }

    @MainActor
    func testRegistrationsWaitForTheFirstPermissionReading() async {
        let model = model()
        var changes = 0
        model.onStateChange = { changes += 1 }
        XCTAssertFalse(model.hasReadPermission)
        await model.refreshPermission()
        XCTAssertTrue(model.hasReadPermission)
        XCTAssertEqual(changes, 1)
        await model.refreshPermission()
        XCTAssertEqual(changes, 1, "an unchanged permission is not a change")
    }

    @MainActor
    func testTheInAppSwitchIsSavedAndRegistered() {
        let model = model()
        var changes = 0
        model.onStateChange = { changes += 1 }
        XCTAssertTrue(model.isEnabledInApp, "on by default")
        model.setEnabledInApp(false)
        XCTAssertFalse(NotificationSettingsStore(defaults: defaults).isEnabledInApp)
        XCTAssertEqual(changes, 1)
    }

    // MARK: Categories

    @MainActor
    func testACategorySwitchIsImmediateAndSaved() async {
        let preferences = FakePreferences()
        let model = model(preferences: preferences)
        model.setSignedIn(true)
        model.setServerDeliversPush(true)
        await model.loadPreferences()

        await model.setCategory("marketing", enabled: true)
        XCTAssertEqual(preferences.saves, [["marketing": true]])
        XCTAssertTrue(model.displayedPreferences.isOn("marketing"))
        XCTAssertNil(model.failedCategory)
    }

    /// News and offers wait for the user's own yes; the rest is on.
    func testMarketingIsOffUntilSwitchedOn() {
        let defaults = NotificationPreferences.defaults
        XCTAssertFalse(defaults.isOn("marketing"))
        for category in NotificationPreferences.categories where category != "marketing" {
            XCTAssertTrue(defaults.isOn(category), category)
        }
        let unnamed = NotificationPreferences(values: [:], optional: NotificationPreferences.categories)
        XCTAssertFalse(unnamed.isOn("marketing"), "a server that does not name it")
        XCTAssertTrue(unnamed.isOn("subscription"))
    }

    @MainActor
    func testARefusedSwitchGoesBack() async {
        let preferences = FakePreferences()
        preferences.failSaves = true
        let model = model(preferences: preferences)
        model.setSignedIn(true)
        model.setServerDeliversPush(true)
        await model.loadPreferences()

        await model.setCategory("subscription", enabled: false)
        XCTAssertTrue(model.displayedPreferences.isOn("subscription"), "rolled back")
        XCTAssertEqual(model.failedCategory, "subscription")
        XCTAssertFalse(model.isSaving("subscription"))
    }

    @MainActor
    func testSecurityCannotBeSwitchedOff() async {
        let preferences = FakePreferences()
        let model = model(preferences: preferences)
        model.setSignedIn(true)
        model.setServerDeliversPush(true)
        await model.loadPreferences()

        await model.setCategory("security", enabled: false)
        XCTAssertTrue(preferences.saves.isEmpty)
        XCTAssertTrue(model.displayedPreferences.isOn("security"))
        XCTAssertFalse(model.displayedPreferences.isOptional("security"))
    }

    @MainActor
    func testSigningOutForgetsTheAccountsCategories() async {
        let model = model()
        model.setSignedIn(true)
        model.setServerDeliversPush(true)
        await model.loadPreferences()
        XCTAssertNotNil(model.preferences)
        model.setSignedIn(false)
        XCTAssertNil(model.preferences)
        XCTAssertFalse(model.showsCategories)
    }

    func testPreferencesDecodeTheServersAnswer() throws {
        let preferences = try JSONDecoder().decode(NotificationPreferences.self, from: Data("""
        {"preferences":{"account":true,"subscription":false,"security":true,"system":true,"marketing":false},
         "optional":["account","subscription","system","marketing"]}
        """.utf8))
        XCTAssertFalse(preferences.isOn("subscription"))
        XCTAssertTrue(preferences.isOn("security"))
        XCTAssertFalse(preferences.isOptional("security"))
        XCTAssertTrue(preferences.isOptional("marketing"))
    }
}
