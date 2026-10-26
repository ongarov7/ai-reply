import Foundation
import XCTest
@testable import AIReply

/// Public App Store releases only: an optional suggestion must never become a
/// downgrade, a repeated interruption, or a request sent by the test host.
@MainActor
final class AppUpdateTests: XCTestCase {

    private final class Clock {
        var date = Date(timeIntervalSince1970: 1_800_000_000)
    }

    private final class FakeFetcher: AppStoreVersionFetching {
        var result: Result<AppStoreRelease?, Error> = .success(nil)
        var holdsResponse = false
        var onRequest: (() -> Void)?
        private(set) var calls = 0
        private var pending: CheckedContinuation<AppStoreRelease?, Error>?

        func latestRelease() async throws -> AppStoreRelease? {
            calls += 1
            if holdsResponse {
                return try await withCheckedThrowingContinuation { continuation in
                    pending = continuation
                    onRequest?()
                }
            }
            onRequest?()
            return try result.get()
        }

        func finish() {
            pending?.resume(with: result)
            pending = nil
        }
    }

    private var production: AppUpdateEnvironment {
        AppUpdateEnvironment(allowsAutomaticChecks: true, isTestFlight: false, isRunningTests: false)
    }

    private func defaults() -> UserDefaults {
        let name = "AppUpdateTests.\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: name)!
        addTeardownBlock { defaults.removePersistentDomain(forName: name) }
        return defaults
    }

    private func release(_ version: String = "1.2", minimumOS: String = "17.0",
                         url: String = "https://apps.apple.com/kz/app/ai-reply/id6818570150?uo=4") -> AppStoreRelease {
        AppStoreRelease(version: version, storeURL: URL(string: url)!, minimumOSVersion: minimumOS)
    }

    private func model(_ fetcher: FakeFetcher, defaults: UserDefaults? = nil,
                       installed: String = "1.1.1", os: String = "26.4.1",
                       environment: AppUpdateEnvironment? = nil, clock: Clock? = nil) -> AppUpdateModel {
        let clock = clock ?? Clock()
        return AppUpdateModel(fetcher: fetcher, defaults: defaults ?? self.defaults(),
                              installedVersion: installed, osVersion: os,
                              environment: environment ?? production, now: { clock.date })
    }

    // MARK: Public version and identity

    func testPublicVersionsCompareNumericallyAndNormalizeTrailingZeros() throws {
        let pairs = [("1.10", "1.9"), ("1.1.1", "1.1"), ("2.0", "1.99.99"), ("1.0.10", "1.0.9")]
        for (newer, older) in pairs {
            XCTAssertGreaterThan(try XCTUnwrap(MarketingVersion(newer)),
                                 try XCTUnwrap(MarketingVersion(older)))
        }
        XCTAssertEqual(MarketingVersion("1"), MarketingVersion("1.0.0"))
        XCTAssertEqual(MarketingVersion("1.1"), MarketingVersion("1.1.0"))
    }

    func testMalformedAndPrereleaseVersionsAreNotComparablePublicReleases() {
        for value in ["", "1.", ".1", "1..2", "1.2.3.4", "1.1-beta", "-1.1", "1.+1", "1.a",
                      "1. 1", " 1.1 ", "١.٢", "999999999999999999999999.0"] {
            XCTAssertNil(MarketingVersion(value), value)
        }
    }

    func testOnlyTheExpectedHTTPSAppStoreProductCanBeOpened() throws {
        for value in ["https://apps.apple.com/kz/app/ai-reply/id6818570150",
                      "https://apps.apple.com/us/app/ai-reply/id6818570150?uo=4"] {
            XCTAssertTrue(AppStoreRelease.isValidStoreURL(try XCTUnwrap(URL(string: value))))
        }
        for value in ["http://apps.apple.com/app/id6818570150",
                      "https://example.test/app/id6818570150",
                      "https://apps.apple.com.example.test/app/id6818570150",
                      "https://apps.apple.com/app/id6818570151",
                      "https://user@apps.apple.com/app/id6818570150",
                      "https://apps.apple.com:444/app/id6818570150",
                      "https://apps.apple.com/app/id6818570150/other",
                      "itms-apps://apps.apple.com/app/id6818570150"] {
            XCTAssertFalse(AppStoreRelease.isValidStoreURL(try XCTUnwrap(URL(string: value))), value)
        }
    }

    private var lookupRecord: [String: Any] {
        ["trackId": Int64(6_818_570_150), "bundleId": "kz.ai-reply.reply.keyboard.keyboard",
         "version": "1.2", "trackViewUrl": "https://apps.apple.com/kz/app/ai-reply/id6818570150?uo=4",
         "minimumOsVersion": "17.0"]
    }

    private func lookupData(_ records: [[String: Any]]) throws -> Data {
        try JSONSerialization.data(withJSONObject: ["resultCount": records.count, "results": records])
    }

    func testLookupDecodesThePublicVersionAndActualProductPage() throws {
        let candidate = try XCTUnwrap(AppStoreLookupClient.decode(lookupData([lookupRecord])))
        XCTAssertEqual(candidate, release())
        XCTAssertTrue(candidate.isValid)
        XCTAssertEqual(AppStoreRelease.appID, 6_818_570_150)
        XCTAssertEqual(AppStoreRelease.bundleID, "kz.ai-reply.reply.keyboard.keyboard")
    }

    func testLookupWithNoPublishedResultOffersNothing() throws {
        XCTAssertNil(try AppStoreLookupClient.decode(lookupData([])))
    }

    func testLookupRejectsWrongIdentityUnsafeLinksAndMalformedVersions() throws {
        let changes: [(String, Any)] = [
            ("trackId", Int64(6_818_570_151)), ("bundleId", "com.example.other"),
            ("trackViewUrl", "https://example.test/app/id6818570150"),
            ("trackViewUrl", "https://apps.apple.com/app/id6818570151"),
            ("version", "1.2-beta"), ("version", 12), ("minimumOsVersion", "unknown")
        ]
        for (key, value) in changes {
            var record = lookupRecord
            record[key] = value
            XCTAssertThrowsError(try AppStoreLookupClient.decode(lookupData([record])), "\(key): \(value)")
        }
        for key in ["trackId", "bundleId", "version", "trackViewUrl", "minimumOsVersion"] {
            var record = lookupRecord
            record[key] = nil
            XCTAssertThrowsError(try AppStoreLookupClient.decode(lookupData([record])), key)
        }
        XCTAssertThrowsError(try AppStoreLookupClient.decode(Data("{".utf8)))
    }

    func testCountryUsesTheStorefrontBeforeTheDeviceRegion() {
        for (storefront, expected) in [("KAZ", "KZ"), ("USA", "US"), ("RUS", "RU"), ("UZB", "UZ"), ("kz", "KZ")] {
            XCTAssertEqual(AppStoreLookupClient.countryCode(storefront: storefront, region: "DE"), expected)
        }
        XCTAssertEqual(AppStoreLookupClient.countryCode(storefront: nil, region: "FR"), "FR")
        XCTAssertEqual(AppStoreLookupClient.countryCode(storefront: "invalid", region: "KZ"), "KZ")
        XCTAssertEqual(AppStoreLookupClient.countryCode(storefront: "ZZ", region: "RU"), "RU")
        XCTAssertEqual(AppStoreLookupClient.countryCode(storefront: nil, region: "ZZ"), "US")
        XCTAssertEqual(AppStoreLookupClient.countryCode(storefront: nil, region: nil), "US")
    }

    // MARK: Optional suggestion and compatibility

    func testANewerPublishedVersionOffersTheActualStorePage() async {
        let fetcher = FakeFetcher()
        fetcher.result = .success(release())
        let model = model(fetcher)
        await model.checkForUpdates()
        XCTAssertEqual(model.status, .available)
        XCTAssertEqual(model.availableRelease, release())
        XCTAssertTrue(model.showsUpdateCard)
        XCTAssertFalse(model.isChecking)
        XCTAssertEqual(fetcher.calls, 1)
    }

    func testEqualAndOlderPublicVersionsNeverOfferAnUpdateOrDowngrade() async {
        for published in ["1.0.9", "1.1", "1.1.0"] {
            let fetcher = FakeFetcher()
            fetcher.result = .success(release(published))
            let model = model(fetcher, installed: "1.1")
            await model.checkForUpdates()
            XCTAssertEqual(model.status, .current, published)
            XCTAssertNil(model.availableRelease)
            XCTAssertFalse(model.showsUpdateCard)
        }
    }

    func testAnInstalledVersionAheadOfThePublicReleaseNeverSuggestsDowngrading() async {
        let fetcher = FakeFetcher()
        fetcher.result = .success(release("1.1"))
        let model = model(fetcher, installed: "1.1.1")
        await model.checkForUpdates(manual: true)
        XCTAssertEqual(model.status, .current)
        XCTAssertFalse(model.showsUpdateCard)
        XCTAssertNil(model.availableRelease)
    }

    func testAReleaseRequiringANewerSystemIsNotOffered() async {
        let fetcher = FakeFetcher()
        fetcher.result = .success(release("1.2", minimumOS: "26.5"))
        let model = model(fetcher, os: "26.4.1")
        await model.checkForUpdates()
        XCTAssertEqual(model.status, .current)
        XCTAssertNil(model.availableRelease)
        XCTAssertFalse(model.showsUpdateCard)
    }

    func testAReleaseSupportingTheExactInstalledSystemCanBeOffered() async {
        let fetcher = FakeFetcher()
        fetcher.result = .success(release("1.2", minimumOS: "17.0.0"))
        let model = model(fetcher, os: "17.0")
        await model.checkForUpdates()
        XCTAssertTrue(model.showsUpdateCard)
    }

    func testMissingOrInvalidMetadataCannotCreateAnUpdateCard() async {
        for candidate in [nil, release("invalid"), release(minimumOS: "unknown"),
                          release(url: "https://example.test/app/id6818570150")] {
            let fetcher = FakeFetcher()
            fetcher.result = .success(candidate)
            let model = model(fetcher)
            await model.checkForUpdates(manual: true)
            XCTAssertEqual(model.status, .unavailable)
            XCTAssertNil(model.availableRelease)
            XCTAssertFalse(model.showsUpdateCard)
        }
    }

    // MARK: Quiet checks and an explicit retry

    func testAutomaticChecksWaitADayButManualChecksBypassTheInterval() async {
        let fetcher = FakeFetcher()
        fetcher.result = .success(release())
        let clock = Clock()
        let model = model(fetcher, clock: clock)
        await model.checkForUpdates()
        clock.date.addTimeInterval(24 * 60 * 60 - 1)
        await model.checkForUpdates()
        XCTAssertEqual(fetcher.calls, 1)
        clock.date.addTimeInterval(1)
        await model.checkForUpdates()
        XCTAssertEqual(fetcher.calls, 2)
        await model.checkForUpdates(manual: true)
        XCTAssertEqual(fetcher.calls, 3)
    }

    func testOfflineChecksAreQuietAndCanBeRetriedManually() async {
        let fetcher = FakeFetcher()
        fetcher.result = .failure(URLError(.notConnectedToInternet))
        let model = model(fetcher)
        await model.checkForUpdates()
        XCTAssertEqual(model.status, .offline)
        XCTAssertFalse(model.showsUpdateCard)
        XCTAssertFalse(model.isChecking)
        await model.checkForUpdates()
        XCTAssertEqual(fetcher.calls, 1, "a foreground cycle must not hammer an unavailable connection")

        fetcher.result = .success(release())
        await model.checkForUpdates(manual: true)
        XCTAssertEqual(fetcher.calls, 2)
        XCTAssertEqual(model.status, .available)
        XCTAssertTrue(model.showsUpdateCard)
    }

    func testOtherNetworkErrorsHaveAnUnavailableStateWithoutAnUpdateCard() async {
        let fetcher = FakeFetcher()
        fetcher.result = .failure(URLError(.badServerResponse))
        let model = model(fetcher)
        await model.checkForUpdates(manual: true)
        XCTAssertEqual(model.status, .unavailable)
        XCTAssertNil(model.availableRelease)
        XCTAssertFalse(model.showsUpdateCard)
    }

    func testTheDailyIntervalAndValidatedReleaseSurviveRelaunch() async {
        let shared = defaults()
        let firstFetcher = FakeFetcher()
        firstFetcher.result = .success(release())
        let clock = Clock()
        await model(firstFetcher, defaults: shared, clock: clock).checkForUpdates()

        let nextFetcher = FakeFetcher()
        let reopened = model(nextFetcher, defaults: shared, clock: clock)
        XCTAssertEqual(reopened.availableRelease, release())
        XCTAssertTrue(reopened.showsUpdateCard)
        await reopened.checkForUpdates()
        XCTAssertEqual(nextFetcher.calls, 0)
    }

    func testCachedReleaseStopsBeingAnUpdateAfterInstallingThatVersion() async {
        let shared = defaults()
        let fetcher = FakeFetcher()
        fetcher.result = .success(release())
        await model(fetcher, defaults: shared).checkForUpdates()
        let installed = model(FakeFetcher(), defaults: shared, installed: "1.2.0")
        XCTAssertNil(installed.availableRelease)
        XCTAssertFalse(installed.showsUpdateCard)
    }

    func testConcurrentChecksShareOneFetch() async {
        let fetcher = FakeFetcher()
        fetcher.holdsResponse = true
        fetcher.result = .success(release())
        let started = expectation(description: "lookup started")
        fetcher.onRequest = { started.fulfill() }
        let model = model(fetcher)
        let first = Task { await model.checkForUpdates() }
        await fulfillment(of: [started], timeout: 2)
        XCTAssertTrue(model.isChecking)
        let second = Task { await model.checkForUpdates(manual: true) }
        for _ in 0..<20 { await Task.yield() }
        XCTAssertEqual(fetcher.calls, 1)
        fetcher.finish()
        await first.value
        await second.value
        XCTAssertEqual(model.status, .available)
        XCTAssertEqual(fetcher.calls, 1)
        XCTAssertFalse(model.isChecking)
    }

    // MARK: Later, distribution and feedback

    func testLaterHidesTheSameReleaseForSevenDaysAcrossRelaunches() async {
        let fetcher = FakeFetcher()
        fetcher.result = .success(release())
        let shared = defaults()
        let clock = Clock()
        let original = model(fetcher, defaults: shared, clock: clock)
        await original.checkForUpdates()
        original.remindLater()
        XCTAssertFalse(original.showsUpdateCard)
        XCTAssertNotNil(original.availableRelease, "Settings may still offer the update")

        let reopened = model(FakeFetcher(), defaults: shared, clock: clock)
        clock.date.addTimeInterval(7 * 24 * 60 * 60 - 1)
        XCTAssertFalse(reopened.showsUpdateCard)
        clock.date.addTimeInterval(1)
        XCTAssertTrue(reopened.showsUpdateCard)
    }

    func testANewerReleaseDoesNotInheritThePreviousVersionsLaterChoice() async {
        let fetcher = FakeFetcher()
        fetcher.result = .success(release())
        let model = model(fetcher)
        await model.checkForUpdates()
        model.remindLater()
        fetcher.result = .success(release("1.3"))
        await model.checkForUpdates(manual: true)
        XCTAssertEqual(model.availableRelease?.version, "1.3")
        XCTAssertTrue(model.showsUpdateCard)
    }

    func testTestFlightExplainsWhereToUpdateWithoutLookingUpOrOfferingADowngrade() async {
        let fetcher = FakeFetcher()
        fetcher.result = .success(release())
        let environment = AppUpdateEnvironment(allowsAutomaticChecks: false, isTestFlight: true, isRunningTests: false)
        let model = model(fetcher, environment: environment)
        await model.checkForUpdates()
        await model.checkForUpdates(manual: true)
        XCTAssertEqual(fetcher.calls, 0)
        XCTAssertEqual(model.status, .testFlight)
        XCTAssertNil(model.availableRelease)
        XCTAssertFalse(model.showsUpdateCard)
    }

    func testDebugCanCheckManuallyWithoutShowingAnAutomaticHomeCard() async {
        let fetcher = FakeFetcher()
        fetcher.result = .success(release())
        let environment = AppUpdateEnvironment(allowsAutomaticChecks: false, isTestFlight: false, isRunningTests: false)
        let model = model(fetcher, environment: environment)
        await model.checkForUpdates()
        XCTAssertEqual(fetcher.calls, 0)
        await model.checkForUpdates(manual: true)
        XCTAssertEqual(fetcher.calls, 1)
        XCTAssertEqual(model.status, .available)
        XCTAssertNotNil(model.availableRelease)
        XCTAssertFalse(model.showsUpdateCard)
    }

    func testTheUnitTestHostCannotMakeAutomaticOrManualLookupRequests() async {
        let fetcher = FakeFetcher()
        let environment = AppUpdateEnvironment(allowsAutomaticChecks: false, isTestFlight: false, isRunningTests: true)
        let model = model(fetcher, environment: environment)
        await model.checkForUpdates()
        await model.checkForUpdates(manual: true)
        XCTAssertEqual(fetcher.calls, 0)
        XCTAssertFalse(model.showsUpdateCard)
    }

    func testDistributionDetectionSeparatesDebugTestFlightAndXCTest() {
        let production = AppUpdateEnvironment.make(isDebug: false, receiptName: "receipt", environment: [:])
        XCTAssertTrue(production.allowsAutomaticChecks)
        XCTAssertFalse(production.isTestFlight)
        XCTAssertFalse(production.isRunningTests)

        let debug = AppUpdateEnvironment.make(isDebug: true, receiptName: nil, environment: [:])
        XCTAssertFalse(debug.allowsAutomaticChecks)
        let testFlight = AppUpdateEnvironment.make(isDebug: false, receiptName: "sandboxReceipt", environment: [:])
        XCTAssertTrue(testFlight.isTestFlight)
        XCTAssertFalse(testFlight.allowsAutomaticChecks)
        let tests = AppUpdateEnvironment.make(isDebug: false, receiptName: "receipt",
                                              environment: ["XCTestConfigurationFilePath": "/test.xctestconfiguration"])
        XCTAssertTrue(tests.isRunningTests)
        XCTAssertFalse(tests.allowsAutomaticChecks)
    }

    func testFailureToOpenTheStoreKeepsTheOptionalUpdateAvailable() async {
        let fetcher = FakeFetcher()
        fetcher.result = .success(release())
        let model = model(fetcher)
        await model.checkForUpdates()
        model.appStoreDidFailToOpen()
        XCTAssertEqual(model.status, .openFailed)
        XCTAssertNotNil(model.availableRelease)
        XCTAssertTrue(model.showsUpdateCard)
    }

    func testUpdateCopyExistsInEverySupportedInterfaceLanguage() {
        let keys = ["updates.title", "updates.body %@", "updates.update", "updates.later", "updates.about",
                    "updates.version", "updates.check", "updates.checking", "updates.current", "updates.unavailable",
                    "updates.offline", "updates.testFlight", "updates.openFailed"]
        for language in AppLanguage.allCases {
            for key in keys {
                let value = AppSettings.localized(key, language: language)
                XCTAssertNotEqual(value, key, "\(key) in \(language.rawValue)")
                XCTAssertFalse(value.isEmpty)
            }
            let body = AppSettings.localized("updates.body %@", language: language)
            XCTAssertEqual(body.components(separatedBy: "%@").count, 2)
            XCTAssertTrue(String(format: body, "1.2.3").contains("1.2.3"))
        }
    }
}
