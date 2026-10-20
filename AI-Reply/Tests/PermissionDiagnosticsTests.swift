import XCTest
@testable import AIReply

/// The permission diagnostics (contract §3.4): what the statuses say, the
/// exact body, and that a report goes out only when something changed or
/// once a day.
///
/// Рұқсат диагностикасы: күйлер, дене, өзгеріс болғанда ғана жіберу.
final class PermissionDiagnosticsTests: XCTestCase {

    private let installation = "0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d"
    private let noon = Date(timeIntervalSince1970: 1_792_497_600) // 2026-10-20 12:00 UTC

    // MARK: Statuses

    func testNothingSeenIsNotVerified() {
        let report = PermissionDiagnosticsReport.make(keyboard: .none, onboarding: .notStarted)
        XCTAssertEqual(report.keyboardSetupStatus, .notVerified)
        XCTAssertEqual(report.keyboardSelectedStatus, .unsupported, "iOS never says which keyboard is in use")
        XCTAssertEqual(report.fullAccessStatus, .notVerified)
        XCTAssertEqual(report.clipboardStatus, .unknown)
        XCTAssertEqual(report.onboardingStatus, .notStarted)
        XCTAssertEqual(report.statusSource, "app_check")
        XCTAssertNil(report.lastKeyboardCheckAt)
        XCTAssertNil(report.lastSuccessfulPasteAt)
    }

    func testTheStatusesFollowTheKeyboardsEvidence() {
        let earlier = noon.addingTimeInterval(-3600)
        let verified = KeyboardStatus.resolve(heartbeatAt: earlier, heartbeatFullAccess: true, fullAccessSignalAt: nil,
                                              limitedSignalAt: nil, pasteSucceededAt: earlier, pasteDeniedAt: nil)
        var report = PermissionDiagnosticsReport.make(keyboard: verified, onboarding: .completed)
        XCTAssertEqual(report.keyboardSetupStatus, .verifiedEnabled)
        XCTAssertEqual(report.fullAccessStatus, .verifiedEnabled)
        XCTAssertEqual(report.clipboardStatus, .verifiedEnabled)
        XCTAssertEqual(report.lastSuccessfulPasteAt, earlier)
        XCTAssertEqual(report.lastKeyboardCheckAt, earlier)

        // Seen later without Full Access, and a paste refused after the success.
        let limited = KeyboardStatus.resolve(heartbeatAt: earlier, heartbeatFullAccess: true, fullAccessSignalAt: nil,
                                             limitedSignalAt: noon, pasteSucceededAt: earlier,
                                             pasteDeniedAt: noon.addingTimeInterval(-60))
        report = PermissionDiagnosticsReport.make(keyboard: limited, onboarding: .skipped)
        XCTAssertEqual(report.fullAccessStatus, .observedDenied)
        XCTAssertEqual(report.clipboardStatus, .observedDenied)
        XCTAssertEqual(report.lastKeyboardCheckAt, noon)
        XCTAssertEqual(report.onboardingStatus, .skipped)
    }

    /// The server's strict body: these eight keys and nothing else, a
    /// missing date as null, no text of any kind.
    func testTheBodyIsExactlyTheContracts() throws {
        let status = KeyboardStatus.resolve(heartbeatAt: noon, heartbeatFullAccess: true, fullAccessSignalAt: nil,
                                            limitedSignalAt: nil, pasteSucceededAt: nil, pasteDeniedAt: nil)
        let report = PermissionDiagnosticsReport.make(keyboard: status, onboarding: .inProgress)
        let json = try XCTUnwrap(JSONSerialization.jsonObject(with: JSONEncoder().encode(report)) as? [String: Any])
        XCTAssertEqual(Set(json.keys), ["keyboard_setup_status", "keyboard_selected_status", "full_access_status",
                                        "clipboard_status", "onboarding_status", "status_source",
                                        "last_successful_paste_at", "last_keyboard_check_at"])
        XCTAssertEqual(json["keyboard_setup_status"] as? String, "verified_enabled")
        XCTAssertEqual(json["keyboard_selected_status"] as? String, "unsupported")
        XCTAssertEqual(json["full_access_status"] as? String, "verified_enabled")
        XCTAssertEqual(json["clipboard_status"] as? String, "unknown")
        XCTAssertEqual(json["onboarding_status"] as? String, "in_progress")
        XCTAssertEqual(json["status_source"] as? String, "app_check")
        XCTAssertTrue(json["last_successful_paste_at"] is NSNull)
        XCTAssertEqual(json["last_keyboard_check_at"] as? String, "2026-10-20T12:00:00Z")
    }

    // MARK: When it is sent

    private actor FakeTransport: DiagnosticsTransport {
        private(set) var sent: [(report: PermissionDiagnosticsReport, token: String?)] = []
        private var failures: [APIError] = []

        func fail(with failures: [APIError]) { self.failures = failures }

        func put(_ report: PermissionDiagnosticsReport, installationID: String, accessToken: String?) async throws {
            sent.append((report, accessToken))
            if !failures.isEmpty { throw failures.removeFirst() }
        }

        var count: Int { sent.count }
    }

    private actor FakeTokens: AccessTokenProviding {
        private(set) var refreshes = 0
        func accessToken() async throws -> String { "access" }
        func refreshAccessToken() async throws -> String {
            refreshes += 1
            return "fresh"
        }
    }

    private final class MemoryStore: DiagnosticsSyncStoring, @unchecked Sendable {
        var record: InstallationSyncRecord?
        func lastSync() -> InstallationSyncRecord? { record }
        func setLastSync(_ record: InstallationSyncRecord?) { self.record = record }
    }

    private final class Clock: @unchecked Sendable {
        var now: Date
        init(_ now: Date) { self.now = now }
    }

    private func report(checkedAt: Date?, onboarding: OnboardingStatus = .completed) -> PermissionDiagnosticsReport {
        let status = KeyboardStatus.resolve(heartbeatAt: checkedAt, heartbeatFullAccess: true, fullAccessSignalAt: nil,
                                            limitedSignalAt: nil, pasteSucceededAt: nil, pasteDeniedAt: nil)
        return PermissionDiagnosticsReport.make(keyboard: status, onboarding: onboarding)
    }

    func testSentOnlyWhenSomethingChangedOrOnceADay() async {
        let transport = FakeTransport()
        let clock = Clock(noon)
        let reporter = PermissionDiagnosticsReporter(transport: transport, tokens: FakeTokens(), store: MemoryStore(),
                                                     now: { clock.now })
        var outcome = await reporter.report(report(checkedAt: noon), installationID: installation,
                                            isSignedIn: false, accountID: nil)
        XCTAssertEqual(outcome, .sent)

        // The keyboard seen again an hour later, the same day: no news.
        clock.now = noon.addingTimeInterval(3600)
        outcome = await reporter.report(report(checkedAt: clock.now), installationID: installation,
                                        isSignedIn: false, accountID: nil)
        XCTAssertEqual(outcome, .unchanged)

        // A status changed: sent at once.
        outcome = await reporter.report(report(checkedAt: clock.now, onboarding: .skipped),
                                        installationID: installation, isSignedIn: false, accountID: nil)
        XCTAssertEqual(outcome, .sent)

        // Seen on another day: sent.
        clock.now = noon.addingTimeInterval(13 * 3600)
        outcome = await reporter.report(report(checkedAt: clock.now, onboarding: .skipped),
                                        installationID: installation, isSignedIn: false, accountID: nil)
        XCTAssertEqual(outcome, .sent)

        // Nothing new, but a day has passed: sent again.
        let same = report(checkedAt: clock.now, onboarding: .skipped)
        clock.now = clock.now.addingTimeInterval(PermissionDiagnosticsReporter.refreshInterval + 1)
        outcome = await reporter.report(same, installationID: installation, isSignedIn: false, accountID: nil)
        XCTAssertEqual(outcome, .sent)

        // Another account on this phone: its own report.
        outcome = await reporter.report(same, installationID: installation, isSignedIn: true, accountID: "u2")
        XCTAssertEqual(outcome, .sent)
        let count = await transport.count
        XCTAssertEqual(count, 5)
    }

    /// A refused report waits for a change; a lost one is tried again on the
    /// next foreground.
    func testFailuresAreTriedAgainOnlyWhereItHelps() async {
        let transport = FakeTransport()
        let reporter = PermissionDiagnosticsReporter(transport: transport, tokens: FakeTokens(), store: MemoryStore(),
                                                     now: { [noon] in noon })
        await transport.fail(with: [.notFound, .offline])
        let first = report(checkedAt: noon)
        var outcome = await reporter.report(first, installationID: installation, isSignedIn: false, accountID: nil)
        XCTAssertEqual(outcome, .failed(.notFound))
        outcome = await reporter.report(first, installationID: installation, isSignedIn: false, accountID: nil)
        XCTAssertEqual(outcome, .unchanged, "the same refused report is not sent again")

        let changed = report(checkedAt: noon, onboarding: .inProgress)
        outcome = await reporter.report(changed, installationID: installation, isSignedIn: false, accountID: nil)
        XCTAssertEqual(outcome, .failed(.offline))
        outcome = await reporter.report(changed, installationID: installation, isSignedIn: false, accountID: nil)
        XCTAssertEqual(outcome, .sent)
    }

    func testASessionSendsItsTokenAndRefreshesOnce() async {
        let transport = FakeTransport()
        let tokens = FakeTokens()
        let reporter = PermissionDiagnosticsReporter(transport: transport, tokens: tokens, store: MemoryStore(),
                                                     now: { [noon] in noon })
        await transport.fail(with: [.unauthorized])
        let outcome = await reporter.report(report(checkedAt: noon), installationID: installation,
                                            isSignedIn: true, accountID: "u1")
        XCTAssertEqual(outcome, .sent)
        let tokensSent = await transport.sent.map(\.token)
        XCTAssertEqual(tokensSent, ["access", "fresh"])
        let refreshes = await tokens.refreshes
        XCTAssertEqual(refreshes, 1)

        let anonymous = FakeTransport()
        let other = PermissionDiagnosticsReporter(transport: anonymous, tokens: tokens, store: MemoryStore(),
                                                  now: { [noon] in noon })
        _ = await other.report(report(checkedAt: noon), installationID: installation, isSignedIn: false, accountID: nil)
        let anonymousTokens = await anonymous.sent.map(\.token)
        XCTAssertEqual(anonymousTokens, [nil])
    }

    func testTheLastReportIsRememberedAcrossLaunches() throws {
        let defaults = try XCTUnwrap(UserDefaults(suiteName: "PermissionDiagnosticsTests.\(UUID())"))
        let store = DefaultsDiagnosticsSyncStore(defaults: defaults)
        XCTAssertNil(store.lastSync())
        store.setLastSync(InstallationSyncRecord(fingerprint: "abc", syncedAt: noon))
        XCTAssertEqual(DefaultsDiagnosticsSyncStore(defaults: defaults).lastSync()?.fingerprint, "abc")
        store.setLastSync(nil)
        XCTAssertNil(store.lastSync())
    }
}
