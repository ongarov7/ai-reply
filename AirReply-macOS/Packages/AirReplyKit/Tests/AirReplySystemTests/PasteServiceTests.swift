import AppKit
import Testing
@testable import AirReplySystem

@MainActor
final class FakePasteSystem: PasteSystem {
    var isSecureInputEnabled = false
    var canPostEvents = true
    /// The target app is in front once this many pauses have passed; nil: never.
    var frontmostAfterPauses: Int? = 0
    private(set) var permissionRequests = 0
    private(set) var openedURLs: [URL] = []
    private(set) var activated: [pid_t] = []
    private(set) var pauses = 0
    private(set) var posts = 0

    func requestPostEventAccess() {
        permissionRequests += 1
    }

    func open(_ url: URL) {
        openedURLs.append(url)
    }

    func activate(_ app: NSRunningApplication) {
        activated.append(app.processIdentifier)
    }

    func isFrontmost(_ app: NSRunningApplication) -> Bool {
        frontmostAfterPauses.map { pauses >= $0 } ?? false
    }

    func pause() async {
        pauses += 1
    }

    func postCommandV() -> Bool {
        posts += 1
        return true
    }
}

/// Apps of the login session (Finder, Dock…). The test runner itself is not one: its
/// `NSRunningApplication.current` has no pid and reads as terminated.
@MainActor
private func runningApps(_ count: Int) throws -> [NSRunningApplication] {
    let apps = NSWorkspace.shared.runningApplications.filter {
        !$0.isTerminated && $0.processIdentifier > 0 && $0.processIdentifier != ProcessInfo.processInfo.processIdentifier
    }
    try #require(apps.count >= count, "needs a login session with running apps")
    return Array(apps.prefix(count))
}

@MainActor
@Suite("Paste for me")
struct PasteServiceTests {
    let system = FakePasteSystem()
    let service: PasteService
    let app: NSRunningApplication

    init() throws {
        service = PasteService(system: system)
        app = try runningApps(1)[0]
    }

    @Test func secureInputWinsOverPermission() {
        #expect(service.availability() == .available)
        system.canPostEvents = false
        #expect(service.availability() == .needsPermission)
        system.isSecureInputEnabled = true
        #expect(service.availability() == .secureInputActive)
        system.canPostEvents = true
        #expect(service.availability() == .secureInputActive)
    }

    @Test func doesNotTryWithoutPermissionOrDuringSecureInput() async {
        system.canPostEvents = false
        #expect(await service.paste(into: app) == false)
        system.canPostEvents = true
        system.isSecureInputEnabled = true
        #expect(await service.paste(into: app) == false)
        #expect(system.activated.isEmpty)
        #expect(system.posts == 0)
    }

    @Test func doesNotTryWithoutAnApp() async {
        #expect(await service.paste(into: nil) == false)
        #expect(system.posts == 0)
    }

    @Test func pastesAtOnceWhenTheAppIsInFront() async {
        #expect(await service.paste(into: app))
        #expect(system.activated == [app.processIdentifier])
        #expect(system.pauses == 0)
        #expect(system.posts == 1)
    }

    @Test func waitsForTheAppToComeForward() async {
        system.frontmostAfterPauses = 3
        #expect(await service.paste(into: app))
        // Three while it activates, one for its key window.
        #expect(system.pauses == 4)
        #expect(system.posts == 1)
    }

    @Test func givesUpAfterSixHundredMilliseconds() async {
        system.frontmostAfterPauses = nil
        #expect(await service.paste(into: app) == false)
        #expect(system.pauses == PasteService.activationChecks)
        #expect(system.posts == 0)
    }

    @Test func permissionPromptAndSettings() {
        service.requestPermission()
        service.openPrivacySettings()
        #expect(system.permissionRequests == 1)
        #expect(system.openedURLs.map(\.absoluteString)
            == ["x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"])
    }
}

@MainActor
@Suite("Previous app")
struct FrontmostAppTrackerTests {
    @MainActor
    final class Front {
        var app: NSRunningApplication?
    }

    @Test func remembersAnotherAppAndForgetsItWhenThisAppIsInFront() throws {
        let apps = try runningApps(2)
        let (other, thisApp) = (apps[0], apps[1])
        let front = Front()
        front.app = other
        let tracker = FrontmostAppTracker(frontmostApplication: { front.app }, ownProcessID: thisApp.processIdentifier, ownBundleIdentifier: nil)
        tracker.rememberFrontmostApp()
        #expect(tracker.previousApp?.processIdentifier == other.processIdentifier)
        // Our settings window in front: nothing to go back to.
        front.app = thisApp
        tracker.rememberFrontmostApp()
        #expect(tracker.previousApp == nil)
        front.app = other
        tracker.rememberFrontmostApp()
        front.app = nil
        tracker.rememberFrontmostApp()
        #expect(tracker.previousApp == nil)
    }

    @Test func anotherCopyOfThisAppCountsAsThisApp() throws {
        let other = try #require(NSWorkspace.shared.runningApplications.first { !$0.isTerminated && $0.bundleIdentifier != nil })
        let tracker = FrontmostAppTracker(frontmostApplication: { other }, ownProcessID: 0, ownBundleIdentifier: other.bundleIdentifier)
        tracker.rememberFrontmostApp()
        #expect(tracker.previousApp == nil)
    }

    @Test func liveTrackerNeverRemembersThisProcess() {
        let tracker = FrontmostAppTracker()
        tracker.rememberFrontmostApp()
        #expect(tracker.previousApp?.processIdentifier != ProcessInfo.processInfo.processIdentifier)
    }
}
