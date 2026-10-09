import XCTest
@testable import AIReply

/// The push plumbing that has no network in it: whether the build may ask
/// for a token at all, which Firebase options are good enough to configure
/// with, the id that names the install, and the one request that carries it.
///
/// Push баптауы, Firebase параметрлері және орнату идентификаторы.
final class PushEnvironmentTests: XCTestCase {

    // MARK: Build flag

    func testOnlyABuildWithTheFlagAsksForATokenAtAll() {
        XCTAssertTrue(PushBuildConfiguration.isEnabled(info: ["AIReplyPushNotifications": "YES"]))
        XCTAssertFalse(PushBuildConfiguration.isEnabled(info: ["AIReplyPushNotifications": "NO"]))
        XCTAssertFalse(PushBuildConfiguration.isEnabled(info: [:]))
        XCTAssertFalse(PushBuildConfiguration.isEnabled(info: ["AIReplyPushNotifications": "$(AIREPLY_PUSH_NOTIFICATIONS)"]))
    }

    /// Both configurations sign with `aps-environment`, so both ask for push;
    /// Firebase still stays off until it has options.
    func testThisBuildAsksForPush() {
        XCTAssertTrue(PushBuildConfiguration.isEnabledInThisBuild)
    }

    /// The tests run inside the app, which must never configure Firebase there.
    @MainActor
    func testTheTestHostNeverConfiguresFirebase() {
        XCTAssertFalse(FirebasePush.isConfigured)
    }

    // MARK: Firebase options

    /// The shape Firebase checks (39 URL-safe characters, starting with "A"),
    /// and plainly not a real key.
    private let apiKey = "A" + String(repeating: "test_key-", count: 4) + "xx"
    private let appID = "1:123456789012:ios:0a1b2c3d4e5f6a7b8c9d0e"

    private func info(appID: String? = nil, senderID: String? = "123456789012",
                      apiKey: String? = nil, projectID: String? = "ai-reply-push") -> [String: Any] {
        var info: [String: Any] = [:]
        info["AIReplyFirebaseAppID"] = appID ?? self.appID
        info["AIReplyFirebaseSenderID"] = senderID
        info["AIReplyFirebaseAPIKey"] = apiKey ?? self.apiKey
        info["AIReplyFirebaseProjectID"] = projectID
        return info
    }

    func testTheFixtureKeyHasTheShapeFirebaseChecks() {
        XCTAssertEqual(apiKey.count, 39)
        XCTAssertTrue(FirebasePush.isAPIKey(apiKey))
    }

    func testAllFourValuesConfigureFirebase() throws {
        let options = try XCTUnwrap(FirebasePush.options(info: info()))
        XCTAssertEqual(options, FirebasePush.Options(googleAppID: appID, gcmSenderID: "123456789012",
                                                     apiKey: apiKey, projectID: "ai-reply-push"))
    }

    /// Empty build settings - the default - leave push out instead of letting
    /// the SDK raise an exception at launch.
    func testEmptyOrMissingValuesLeavePushOut() {
        XCTAssertNil(FirebasePush.options(info: [:]))
        XCTAssertNil(FirebasePush.options(info: [
            "AIReplyFirebaseAppID": "", "AIReplyFirebaseSenderID": "",
            "AIReplyFirebaseAPIKey": "", "AIReplyFirebaseProjectID": ""
        ]))
        XCTAssertNil(FirebasePush.options(info: [
            "AIReplyFirebaseAppID": "$(FIREBASE_GOOGLE_APP_ID)", "AIReplyFirebaseSenderID": "$(FIREBASE_GCM_SENDER_ID)",
            "AIReplyFirebaseAPIKey": "$(FIREBASE_API_KEY)", "AIReplyFirebaseProjectID": "$(FIREBASE_PROJECT_ID)"
        ]), "an unexpanded build setting")
        XCTAssertNil(FirebasePush.options(info: info(senderID: nil)))
        XCTAssertNil(FirebasePush.options(info: info(projectID: "")))
        XCTAssertNil(FirebasePush.options(info: info(projectID: "   ")))
    }

    /// Firebase raises an exception for a key or id of the wrong shape; such
    /// a value is refused here instead.
    func testMalformedValuesLeavePushOut() {
        XCTAssertNil(FirebasePush.options(info: info(apiKey: "Atest_short")), "too short")
        XCTAssertNil(FirebasePush.options(info: info(apiKey: "B" + String(apiKey.dropFirst()))), "must start with A")
        XCTAssertNil(FirebasePush.options(info: info(apiKey: String(apiKey.dropLast()) + "!")), "not URL-safe")
        XCTAssertNil(FirebasePush.options(info: info(appID: "1:123:android:abc")), "an Android app id")
        XCTAssertNil(FirebasePush.options(info: info(appID: "not-an-app-id")))
        XCTAssertNil(FirebasePush.options(info: info(senderID: "12ab")))
    }

    func testOptionsAreTrimmed() throws {
        let options = try XCTUnwrap(FirebasePush.options(info: info(projectID: " ai-reply-push \n")))
        XCTAssertEqual(options.projectID, "ai-reply-push")
    }

    /// The shared client config in Config/Firebase reaches the app through
    /// the "Firebase config" build phase and passes the checks that guard
    /// `FirebaseApp.configure`. No value of it is printed.
    func testTheCommittedFirebaseConfigIsCopiedIntoTheAppAndAccepted() throws {
        let source = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
            .appendingPathComponent("Config/Firebase/GoogleService-Info.plist")
        guard FileManager.default.fileExists(atPath: source.path) else {
            throw XCTSkip("this checkout has no Config/Firebase/GoogleService-Info.plist")
        }
        let path = try XCTUnwrap(Bundle.main.path(forResource: "GoogleService-Info", ofType: "plist"),
                                 "the Firebase config phase did not copy the file into the app")
        XCTAssertTrue(FileManager.default.contentsEqual(atPath: path, andPath: source.path), "a stale copy")
        let file = try XCTUnwrap(NSDictionary(contentsOfFile: path) as? [String: Any])
        XCTAssertEqual(file["BUNDLE_ID"] as? String, Bundle.main.bundleIdentifier)
        let options = FirebasePush.validated(googleAppID: file["GOOGLE_APP_ID"] as? String,
                                             gcmSenderID: file["GCM_SENDER_ID"] as? String,
                                             apiKey: file["API_KEY"] as? String,
                                             projectID: file["PROJECT_ID"] as? String)
        XCTAssertTrue(options != nil, "push would stay off with these options")
    }

    // MARK: Installation id

    private final class MemoryIDStorage: InstallationIDStorage {
        var stored: InstallationIDLoad = .notFound
        var saves: [String] = []
        var acceptsWrites = true

        func load() -> InstallationIDLoad { stored }

        func save(_ id: String) -> Bool {
            guard acceptsWrites else { return false }
            saves.append(id)
            stored = .found(id)
            return true
        }
    }

    func testTheInstallationIDIsMadeOnceAndKept() {
        let storage = MemoryIDStorage()
        var made = 0
        let identity = InstallationIdentity(storage: storage, makeID: {
            made += 1
            return "0b7c9a52-4f5e-4d0a-9c1e-00000000000\(made)"
        })
        XCTAssertEqual(identity.id, "0b7c9a52-4f5e-4d0a-9c1e-000000000001")
        XCTAssertEqual(identity.id, "0b7c9a52-4f5e-4d0a-9c1e-000000000001")
        XCTAssertEqual(storage.saves.count, 1)

        let reopened = InstallationIdentity(storage: storage, makeID: { XCTFail("must not make another"); return "" })
        XCTAssertEqual(reopened.id, "0b7c9a52-4f5e-4d0a-9c1e-000000000001", "the next launch reads the same id")
    }

    func testTheDefaultIDIsARandomLowercaseUUID() throws {
        let storage = MemoryIDStorage()
        let id = try XCTUnwrap(InstallationIdentity(storage: storage).id)
        XCTAssertNotNil(UUID(uuidString: id))
        XCTAssertEqual(id, id.lowercased())
        XCTAssertTrue(InstallationIdentity.isValid(id))
    }

    func testAMalformedStoredIDIsReplaced() {
        let storage = MemoryIDStorage()
        storage.stored = .found("not valid!")
        let identity = InstallationIdentity(storage: storage, makeID: { "0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d" })
        XCTAssertEqual(identity.id, "0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d")
    }

    func testALockedKeychainMeansNoIDYetRatherThanANewOne() {
        let storage = MemoryIDStorage()
        storage.stored = .unavailable
        let identity = InstallationIdentity(storage: storage, makeID: { "0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d" })
        XCTAssertNil(identity.id)
        XCTAssertTrue(storage.saves.isEmpty, "a second id would be a second installation on the server")

        storage.stored = .found("0b7c9a52-4f5e-4d0a-9c1e-aaaaaaaaaaaa")
        XCTAssertEqual(identity.id, "0b7c9a52-4f5e-4d0a-9c1e-aaaaaaaaaaaa")
    }

    func testAFailedWriteIsRetriedLater() {
        let storage = MemoryIDStorage()
        storage.acceptsWrites = false
        let identity = InstallationIdentity(storage: storage, makeID: { "0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d" })
        XCTAssertNil(identity.id)
        storage.acceptsWrites = true
        XCTAssertEqual(identity.id, "0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d")
    }

    /// The app's own keychain group, never the one shared with the keyboard.
    func testTheIDIsKeptInTheAppsOwnKeychainGroup() {
        XCTAssertEqual(KeychainInstallationIDStorage.applicationIdentifier(
            info: ["AIReplyAppIdentifierPrefix": "ABCDE12345."], bundleID: "kz.ai-reply.reply.keyboard.keyboard"),
            "ABCDE12345.kz.ai-reply.reply.keyboard.keyboard")
        XCTAssertNil(KeychainInstallationIDStorage.applicationIdentifier(
            info: ["AIReplyAppIdentifierPrefix": ""], bundleID: "kz.ai-reply.reply.keyboard.keyboard"),
            "an unsigned build has no prefix")
        XCTAssertNil(KeychainInstallationIDStorage.applicationIdentifier(
            info: ["AIReplyAppIdentifierPrefix": "$(AppIdentifierPrefix)"], bundleID: "kz.ai-reply.reply.keyboard.keyboard"))
        XCTAssertNil(KeychainInstallationIDStorage.applicationIdentifier(info: [:], bundleID: nil))
    }

    // MARK: Logout header

    /// Records the headers of every request and answers `{}`.
    private final class RecordingProtocol: URLProtocol {
        static var headers: [[String: String]] = []

        override class func canInit(with request: URLRequest) -> Bool { true }
        override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

        override func startLoading() {
            Self.headers.append(request.allHTTPHeaderFields ?? [:])
            let response = HTTPURLResponse(url: request.url ?? URL(fileURLWithPath: "/"), statusCode: 200,
                                           httpVersion: nil, headerFields: ["Content-Type": "application/json"])
            if let response { client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed) }
            client?.urlProtocol(self, didLoad: Data("{}".utf8))
            client?.urlProtocolDidFinishLoading(self)
        }

        override func stopLoading() {}
    }

    /// Only the logout request names the installation; every other request
    /// carries no client metadata at all.
    func testOnlyTheClientMadeForLogoutNamesTheInstallation() async throws {
        RecordingProtocol.headers = []
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [RecordingProtocol.self]
        let session = URLSession(configuration: configuration)
        let baseURL = try XCTUnwrap(URL(string: "https://example.test"))

        let logout = APIClient(baseURL: baseURL, session: session,
                               headers: ["X-Installation-ID": "0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d"])
        let _: APIClient.Empty = try await logout.post("api/v1/auth/logout", body: ["refresh_token": "r"])
        let plain = APIClient(baseURL: baseURL, session: session)
        let _: APIClient.Empty = try await plain.post("api/v1/installations", body: ["installation_id": "x"])

        XCTAssertEqual(RecordingProtocol.headers.count, 2)
        XCTAssertEqual(RecordingProtocol.headers.first?["X-Installation-ID"], "0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d")
        XCTAssertNil(RecordingProtocol.headers.last?["X-Installation-ID"])
        XCTAssertEqual(RecordingProtocol.headers.last?["Accept"], "application/json")
    }
}
