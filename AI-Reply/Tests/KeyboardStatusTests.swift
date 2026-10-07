import XCTest
@testable import AIReply

/// What the app says about its keyboard: only what the keyboard itself
/// reported, through the App Group heartbeat or its Darwin notification.
@MainActor
final class KeyboardStatusTests: XCTestCase {

    private let earlier = Date(timeIntervalSince1970: 1_000)
    private let later = Date(timeIntervalSince1970: 2_000)

    func testNothingReportedMeansNothingKnown() {
        let status = KeyboardStatus.resolve(lastSeen: nil, heartbeatFullAccess: false, limitedSeenAt: nil)
        XCTAssertEqual(status, KeyboardStatus(isEnabled: false, fullAccess: .unknown))
    }

    func testHeartbeatSaysEnabledAndFullAccess() {
        XCTAssertEqual(KeyboardStatus.resolve(lastSeen: earlier, heartbeatFullAccess: true, limitedSeenAt: nil),
                       KeyboardStatus(isEnabled: true, fullAccess: .on))
        XCTAssertEqual(KeyboardStatus.resolve(lastSeen: earlier, heartbeatFullAccess: false, limitedSeenAt: nil),
                       KeyboardStatus(isEnabled: true, fullAccess: .off))
    }

    /// Without Full Access the keyboard cannot write the heartbeat at all; the
    /// app hearing it run is enough to know it is on, and that access is off.
    func testLimitedSignalAloneMeansEnabledWithoutFullAccess() {
        XCTAssertEqual(KeyboardStatus.resolve(lastSeen: nil, heartbeatFullAccess: false, limitedSeenAt: later),
                       KeyboardStatus(isEnabled: true, fullAccess: .off))
    }

    /// Full Access turned off after it was on: the stored heartbeat still
    /// says yes, but the newer signal wins.
    func testANewerLimitedSignalBeatsAnOlderHeartbeat() {
        XCTAssertEqual(KeyboardStatus.resolve(lastSeen: earlier, heartbeatFullAccess: true, limitedSeenAt: later),
                       KeyboardStatus(isEnabled: true, fullAccess: .off))
        XCTAssertEqual(KeyboardStatus.resolve(lastSeen: later, heartbeatFullAccess: true, limitedSeenAt: earlier),
                       KeyboardStatus(isEnabled: true, fullAccess: .on))
    }

    func testMonitorFollowsTheKeyboardsSignals() {
        let shared = SharedSettings(defaults: UserDefaults(suiteName: "KeyboardStatusTests.shared.\(UUID())")!)
        let local = UserDefaults(suiteName: "KeyboardStatusTests.local.\(UUID())")!
        let monitor = KeyboardStatusMonitor(settings: shared, defaults: local, observesKeyboard: false)
        XCTAssertEqual(monitor.status, KeyboardStatus(isEnabled: false, fullAccess: .unknown))

        monitor.keyboardDidAppear(hasFullAccess: false, now: later)
        XCTAssertEqual(monitor.status, KeyboardStatus(isEnabled: true, fullAccess: .off))

        // Full Access switched on: the keyboard writes its heartbeat, then
        // says so.
        shared.markKeyboardActive(hasFullAccess: true)
        monitor.keyboardDidAppear(hasFullAccess: true)
        XCTAssertEqual(monitor.status, KeyboardStatus(isEnabled: true, fullAccess: .on))

        // A fresh monitor - the next launch - reads the same answer.
        let relaunched = KeyboardStatusMonitor(settings: shared, defaults: local, observesKeyboard: false)
        XCTAssertEqual(relaunched.status, monitor.status)
    }

    func testDarwinNamesMatchTheKeyboardsSide() {
        XCTAssertEqual(KeyboardPresence.name(hasFullAccess: true), "kz.ai-reply.keyboard.appeared.full")
        XCTAssertEqual(KeyboardPresence.name(hasFullAccess: false), "kz.ai-reply.keyboard.appeared.limited")
    }
}
