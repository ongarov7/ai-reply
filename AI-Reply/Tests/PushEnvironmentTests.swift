import XCTest
@testable import AIReply

/// The push plumbing that has no network in it: the token's text, which APNs
/// host it belongs to, whether the build may ask for one at all, and the ids
/// that name the install and the session.
///
/// Push токені, APNs ортасы, орнату және сессия идентификаторлары.
final class PushEnvironmentTests: XCTestCase {

    // MARK: Token

    func testTokenIsLowercaseHexTwoDigitsPerByte() {
        XCTAssertEqual(PushToken.hexString(Data([0x00, 0x0f, 0xab, 0xff])), "000fabff")
        let token = PushToken.hexString(Data((0..<32).map { UInt8($0 * 7 % 256) }))
        XCTAssertEqual(token.count, 64)
        XCTAssertNotNil(token.range(of: "^[0-9a-f]{64}$", options: .regularExpression), token)
    }

    // MARK: embedded.mobileprovision

    /// A profile as iOS stores it: a signed envelope around an XML plist. The
    /// bytes around the plist stand in for the CMS signature.
    private func profile(entitlements: String) -> Data {
        var data = Data([0x30, 0x82, 0x2c, 0x5f, 0x06, 0x09, 0x2a, 0x86, 0x48, 0x86, 0xf7, 0x0d, 0x01, 0x07, 0x02, 0xa0])
        data.append(Data("""
        <?xml version="1.0" encoding="UTF-8"?>
        <!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
        <plist version="1.0">
        <dict>
            <key>AppIDName</key>
            <string>AI Reply</string>
            <key>Entitlements</key>
            <dict>
                <key>application-identifier</key>
                <string>JXM8N66QWU.kz.yerek.replykeyboard</string>
                \(entitlements)
            </dict>
            <key>Name</key>
            <string>iOS Team Provisioning Profile: kz.yerek.replykeyboard</string>
        </dict>
        </plist>
        """.utf8))
        data.append(Data([0xa0, 0x82, 0x0e, 0x3e, 0x30, 0x82, 0x04, 0x34, 0x00, 0xff]))
        return data
    }

    func testDevelopmentProfileMeansSandbox() {
        let data = profile(entitlements: """
            <key>aps-environment</key>
            <string>development</string>
            <key>get-task-allow</key>
            <true/>
            """)
        XCTAssertEqual(ProvisioningProfile.entitlements(in: data)?["aps-environment"] as? String, "development")
        XCTAssertEqual(APNsEnvironment.detect(provisioningProfile: data, isSimulator: false), .sandbox)
    }

    func testAdHocProfileMeansProduction() {
        let data = profile(entitlements: """
            <key>aps-environment</key>
            <string>production</string>
            <key>get-task-allow</key>
            <false/>
            """)
        XCTAssertEqual(APNsEnvironment.detect(provisioningProfile: data, isSimulator: false), .production)
    }

    func testNoProfileIsTheAppStoreOrTestFlight() {
        XCTAssertEqual(APNsEnvironment.detect(provisioningProfile: nil, isSimulator: false), .production)
    }

    func testTheSimulatorIsSandbox() {
        XCTAssertEqual(APNsEnvironment.detect(provisioningProfile: nil, isSimulator: true), .sandbox)
    }

    func testAProfileWithoutPushFallsBackToHowTheBuildWasSigned() {
        let debuggable = profile(entitlements: "<key>get-task-allow</key>\n<true/>")
        XCTAssertEqual(APNsEnvironment.detect(provisioningProfile: debuggable, isSimulator: false), .sandbox)
        let distribution = profile(entitlements: "<key>get-task-allow</key>\n<false/>")
        XCTAssertEqual(APNsEnvironment.detect(provisioningProfile: distribution, isSimulator: false), .production)
    }

    func testAnUnreadableProfileIsNotACrash() {
        XCTAssertNil(ProvisioningProfile.propertyList(in: Data([0x00, 0x01, 0x02])))
        XCTAssertNil(ProvisioningProfile.propertyList(in: Data("<?xml broken".utf8)))
        XCTAssertEqual(APNsEnvironment.detect(provisioningProfile: Data("garbage".utf8), isSimulator: false), .production)
    }

    // MARK: Build flag

    func testOnlyABuildWithTheFlagAsksForATokenAtAll() {
        XCTAssertTrue(PushBuildConfiguration.isEnabled(info: ["AIReplyPushNotifications": "YES"]))
        XCTAssertFalse(PushBuildConfiguration.isEnabled(info: ["AIReplyPushNotifications": "NO"]))
        XCTAssertFalse(PushBuildConfiguration.isEnabled(info: [:]))
        XCTAssertFalse(PushBuildConfiguration.isEnabled(info: ["AIReplyPushNotifications": "$(AIREPLY_PUSH_NOTIFICATIONS)"]))
    }

    /// Tests run the Debug build, which leaves push out like Sign in with Apple.
    func testThisDebugBuildLeavesPushOut() {
        #if DEBUG
        XCTAssertFalse(PushBuildConfiguration.isEnabledInThisBuild)
        #endif
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

    // MARK: Session

    @MainActor
    func testSessionStartsAtLaunchAndAfterHalfAnHourAway() {
        var now = Date(timeIntervalSince1970: 1_800_000_000)
        var made = 0
        let tracker = SessionTracker(now: { now }, makeID: {
            made += 1
            return "session-\(made)"
        })
        XCTAssertEqual(tracker.sessionID, "session-1")
        XCTAssertEqual(tracker.didBecomeActive(), .opened(coldStart: true, newSession: true))
        XCTAssertNil(tracker.didBecomeActive(), "inactive → active is not a new opening")

        now = now.addingTimeInterval(95)
        XCTAssertEqual(tracker.didEnterBackground(), .backgrounded(foregroundSeconds: 95))
        now = now.addingTimeInterval(10 * 60)
        XCTAssertEqual(tracker.didBecomeActive(), .opened(coldStart: false, newSession: false))
        XCTAssertEqual(tracker.sessionID, "session-1")

        now = now.addingTimeInterval(20)
        XCTAssertEqual(tracker.didEnterBackground(), .backgrounded(foregroundSeconds: 20))
        now = now.addingTimeInterval(SessionTracker.backgroundTimeout)
        XCTAssertEqual(tracker.didBecomeActive(), .opened(coldStart: false, newSession: true))
        XCTAssertEqual(tracker.sessionID, "session-2")
    }
}
