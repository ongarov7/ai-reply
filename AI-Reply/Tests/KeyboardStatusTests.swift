import XCTest
@testable import AIReply

/// What the app says about its keyboard: only what the keyboard itself
/// reported - its App Group heartbeat and paste stamps, and its Darwin
/// notifications - each with the time it was seen, and the diagnostics
/// statuses of contract §3.4 derived from them.
@MainActor
final class KeyboardStatusTests: XCTestCase {

    private let now = Date(timeIntervalSince1970: 1_800_000_000)
    private var hourAgo: Date { now.addingTimeInterval(-3_600) }
    private var minuteAgo: Date { now.addingTimeInterval(-60) }
    private var monthAgo: Date { now.addingTimeInterval(-30 * 24 * 3_600) }

    private func status(
        heartbeatAt: Date? = nil,
        heartbeatFullAccess: Bool = true,
        fullAccessSignalAt: Date? = nil,
        limitedSignalAt: Date? = nil,
        pasteSucceededAt: Date? = nil,
        pasteDeniedAt: Date? = nil
    ) -> KeyboardStatus {
        KeyboardStatus.resolve(
            heartbeatAt: heartbeatAt,
            heartbeatFullAccess: heartbeatFullAccess,
            fullAccessSignalAt: fullAccessSignalAt,
            limitedSignalAt: limitedSignalAt,
            pasteSucceededAt: pasteSucceededAt,
            pasteDeniedAt: pasteDeniedAt
        )
    }

    // MARK: Statuses

    func testNothingReportedMeansNothingVerified() {
        let nothing = status()
        XCTAssertEqual(nothing, .none)
        XCTAssertEqual(nothing.keyboardSetupStatus, .notVerified)
        XCTAssertEqual(nothing.keyboardSelectedStatus, .unsupported)
        XCTAssertEqual(nothing.fullAccessStatus, .notVerified)
        XCTAssertEqual(nothing.clipboardStatus, .unknown)
        XCTAssertNil(nothing.lastKeyboardCheckAt)
        XCTAssertFalse(nothing.isEnabled)
        XCTAssertTrue(nothing.isStale(now: now), "nothing seen is never current")
    }

    /// A fresh heartbeat: the keyboard is set up and had Full Access an hour
    /// ago - said with the time, and current enough to be shown as done.
    func testFreshHeartbeatVerifiesSetupAndFullAccess() {
        let fresh = status(heartbeatAt: hourAgo)
        XCTAssertEqual(fresh.keyboardSetupStatus, .verifiedEnabled)
        XCTAssertEqual(fresh.fullAccessStatus, .verifiedEnabled)
        XCTAssertEqual(fresh.fullAccessVerifiedAt, hourAgo)
        XCTAssertEqual(fresh.lastKeyboardCheckAt, hourAgo)
        XCTAssertFalse(fresh.isStale(now: now))
        XCTAssertTrue(fresh.isEnabled)
    }

    /// A month-old heartbeat still says what was seen then, with its date,
    /// but is history: stale, so no screen may present it as current.
    func testOldHeartbeatIsStale() {
        let stale = status(heartbeatAt: monthAgo)
        XCTAssertEqual(stale.fullAccessStatus, .verifiedEnabled)
        XCTAssertEqual(stale.lastKeyboardCheckAt, monthAgo)
        XCTAssertTrue(stale.isStale(now: now))
        XCTAssertFalse(status(heartbeatAt: now.addingTimeInterval(-KeyboardStatus.freshness + 60)).isStale(now: now))
    }

    /// Full Access turned off after it was verified: the stored heartbeat
    /// still says yes, but the newer sighting without it wins - and the
    /// keyboard was seen, so it is still set up.
    func testLimitedAfterVerifiedIsObservedDenied() {
        let limited = status(heartbeatAt: hourAgo, limitedSignalAt: minuteAgo)
        XCTAssertEqual(limited.fullAccessStatus, .observedDenied)
        XCTAssertEqual(limited.keyboardSetupStatus, .verifiedEnabled)
        XCTAssertEqual(limited.lastKeyboardCheckAt, minuteAgo)
        XCTAssertEqual(limited.fullAccessVerifiedAt, hourAgo, "the earlier verification is kept, with its date")

        // ...and switched back on: the newer one wins again.
        let again = status(heartbeatAt: hourAgo, fullAccessSignalAt: now, limitedSignalAt: minuteAgo)
        XCTAssertEqual(again.fullAccessStatus, .verifiedEnabled)
        XCTAssertEqual(status(heartbeatAt: minuteAgo, limitedSignalAt: hourAgo).fullAccessStatus, .verifiedEnabled)
    }

    /// Without Full Access the keyboard cannot write anything: the app
    /// hearing it run is enough to know it is set up, and that access is off.
    func testLimitedSignalAloneMeansSetUpWithoutFullAccess() {
        let limited = status(limitedSignalAt: hourAgo)
        XCTAssertEqual(limited.keyboardSetupStatus, .verifiedEnabled)
        XCTAssertEqual(limited.fullAccessStatus, .observedDenied)
        XCTAssertNil(limited.fullAccessVerifiedAt)
    }

    /// An older build wrote its heartbeat without Full Access too; such a
    /// stamp is a sighting without it, never a verification.
    func testOlderHeartbeatWithoutFullAccessCountsAsLimited() {
        let old = status(heartbeatAt: hourAgo, heartbeatFullAccess: false)
        XCTAssertEqual(old.fullAccessStatus, .observedDenied)
        XCTAssertEqual(old.limitedSeenAt, hourAgo)
        XCTAssertNil(old.fullAccessVerifiedAt)
    }

    /// The last paste decides the clipboard; a paste happens only with Full
    /// Access, so it also verifies that.
    func testDeniedAfterSuccessIsObservedDenied() {
        XCTAssertEqual(status(pasteSucceededAt: hourAgo).clipboardStatus, .verifiedEnabled)
        let denied = status(pasteSucceededAt: hourAgo, pasteDeniedAt: minuteAgo)
        XCTAssertEqual(denied.clipboardStatus, .observedDenied)
        XCTAssertEqual(denied.lastSuccessfulPasteAt, hourAgo)
        XCTAssertEqual(status(pasteSucceededAt: now, pasteDeniedAt: minuteAgo).clipboardStatus, .verifiedEnabled)
        XCTAssertEqual(status(pasteDeniedAt: minuteAgo).clipboardStatus, .observedDenied)

        let pasted = status(limitedSignalAt: hourAgo, pasteSucceededAt: minuteAgo)
        XCTAssertEqual(pasted.fullAccessStatus, .verifiedEnabled)
        XCTAssertEqual(pasted.lastKeyboardCheckAt, minuteAgo)
    }

    func testStatusesUseTheDiagnosticsWireValues() {
        XCTAssertEqual(PermissionEvidence.allCases.map(\.rawValue),
                       ["unknown", "not_verified", "verified_enabled", "observed_denied", "user_reported_disabled", "unsupported"])
    }

    // MARK: Monitor

    func testMonitorKeepsTheKeyboardsSignalsWithTheirTimes() {
        let shared = SharedSettings(defaults: UserDefaults(suiteName: "KeyboardStatusTests.shared.\(UUID())")!)
        let local = UserDefaults(suiteName: "KeyboardStatusTests.local.\(UUID())")!
        let monitor = KeyboardStatusMonitor(settings: shared, defaults: local, observesKeyboard: false)
        XCTAssertEqual(monitor.status, .none)

        monitor.keyboardDidAppear(hasFullAccess: false, now: hourAgo)
        XCTAssertEqual(monitor.status.fullAccessStatus, .observedDenied)
        XCTAssertEqual(monitor.status.keyboardSetupStatus, .verifiedEnabled)

        // Full Access switched on: the keyboard writes its heartbeat, then
        // says so; both are newer than the limited sighting.
        shared.markKeyboardActive(hasFullAccess: true, at: minuteAgo)
        monitor.keyboardDidAppear(hasFullAccess: true, now: minuteAgo)
        XCTAssertEqual(monitor.status.fullAccessStatus, .verifiedEnabled)
        XCTAssertEqual(monitor.status.fullAccessVerifiedAt, minuteAgo)
        XCTAssertEqual(monitor.status.limitedSeenAt, hourAgo, "the older sighting is kept, not erased")

        // A paste the keyboard recorded, then one iOS refused.
        shared.recordPaste(.succeeded, at: minuteAgo)
        shared.recordPaste(.denied, at: now)
        monitor.refresh()
        XCTAssertEqual(monitor.status.clipboardStatus, .observedDenied)
        XCTAssertEqual(monitor.status.lastSuccessfulPasteAt, minuteAgo)

        // A fresh monitor - the next launch - reads the same answer.
        let relaunched = KeyboardStatusMonitor(settings: shared, defaults: local, observesKeyboard: false)
        XCTAssertEqual(relaunched.status, monitor.status)
    }

    func testDarwinNamesMatchTheKeyboardsSide() {
        XCTAssertEqual(KeyboardPresence.name(hasFullAccess: true), "kz.ai-reply.keyboard.appeared.full")
        XCTAssertEqual(KeyboardPresence.name(hasFullAccess: false), "kz.ai-reply.keyboard.appeared.limited")
    }
}
