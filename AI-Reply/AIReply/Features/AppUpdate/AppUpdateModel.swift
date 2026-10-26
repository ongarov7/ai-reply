import Foundation
import Observation
import StoreKit

/// App Store versions are numeric components, not build numbers or text order.
struct MarketingVersion: Comparable, Sendable {
    private let components: [Int]

    init?(_ raw: String) {
        let parts = raw.split(separator: ".", omittingEmptySubsequences: false)
        guard (1...3).contains(parts.count) else { return nil }
        var values: [Int] = []
        for part in parts {
            guard !part.isEmpty, part.utf8.allSatisfy({ (48...57).contains($0) }),
                  let value = Int(part) else { return nil }
            values.append(value)
        }
        components = values + Array(repeating: 0, count: 3 - values.count)
    }

    static func < (lhs: Self, rhs: Self) -> Bool {
        lhs.components.lexicographicallyPrecedes(rhs.components)
    }

    var storageKey: String { components.map(String.init).joined(separator: ".") }
}

struct AppStoreRelease: Codable, Equatable, Sendable {
    static let appID: Int64 = 6818570150
    static let bundleID = "kz.ai-reply.reply.keyboard.keyboard"

    let version: String
    let storeURL: URL
    let minimumOSVersion: String

    var isValid: Bool {
        MarketingVersion(version) != nil && MarketingVersion(minimumOSVersion) != nil
            && Self.isValidStoreURL(storeURL)
    }

    static func isValidStoreURL(_ url: URL) -> Bool {
        guard let parts = URLComponents(url: url, resolvingAgainstBaseURL: false),
              parts.scheme?.lowercased() == "https", parts.host?.lowercased() == "apps.apple.com",
              parts.user == nil, parts.password == nil, parts.port == nil || parts.port == 443 else { return false }
        return url.path.split(separator: "/").last == "id\(appID)"
    }
}

@MainActor
protocol AppStoreVersionFetching {
    func latestRelease() async throws -> AppStoreRelease?
}

/// Public App Store metadata only: no account, device ID or app-server token.
@MainActor
struct AppStoreLookupClient: AppStoreVersionFetching {
    enum LookupError: Error { case invalidResponse }

    private let session: URLSession
    private let country: () async -> String

    init(session: URLSession? = nil, country: (() async -> String)? = nil) {
        if let session {
            self.session = session
        } else {
            let configuration = URLSessionConfiguration.ephemeral
            configuration.httpCookieStorage = nil
            configuration.urlCredentialStorage = nil
            configuration.httpShouldSetCookies = false
            configuration.timeoutIntervalForRequest = 10
            configuration.timeoutIntervalForResource = 15
            self.session = URLSession(configuration: configuration)
        }
        self.country = country ?? {
            let storefront = await Storefront.current
            return Self.countryCode(storefront: storefront?.countryCode, region: Locale.current.region?.identifier)
        }
    }

    func latestRelease() async throws -> AppStoreRelease? {
        let selectedCountry = await country()
        var components = URLComponents(string: "https://itunes.apple.com/lookup")!
        components.queryItems = [URLQueryItem(name: "id", value: String(AppStoreRelease.appID)),
                                 URLQueryItem(name: "country", value: Self.countryCode(storefront: selectedCountry, region: nil).lowercased())]
        guard let url = components.url else { throw LookupError.invalidResponse }
        var request = URLRequest(url: url, cachePolicy: .reloadIgnoringLocalCacheData, timeoutInterval: 10)
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse, http.statusCode == 200,
              http.url?.host?.lowercased() == "itunes.apple.com", http.url?.scheme?.lowercased() == "https" else {
            throw LookupError.invalidResponse
        }
        return try Self.decode(data)
    }

    static func countryCode(storefront: String?, region: String?) -> String {
        func alpha2(_ raw: String?) -> String? {
            guard let raw else { return nil }
            let code = raw.uppercased()
            let normalized: String
            if code.count == 3 {
                normalized = Locale(identifier: "und_" + code).region?.identifier ?? ""
            } else {
                normalized = code
            }
            guard normalized.utf8.count == 2, normalized.utf8.allSatisfy({ (65...90).contains($0) }),
                  Locale.Region.isoRegions.contains(where: { $0.identifier == normalized }) else { return nil }
            return normalized
        }
        return alpha2(storefront) ?? alpha2(region) ?? "US"
    }

    static func decode(_ data: Data) throws -> AppStoreRelease? {
        struct Response: Decodable { let results: [Result] }
        struct Result: Decodable {
            let trackId: Int64?
            let bundleId: String?
            let version: String?
            let trackViewUrl: String?
            let minimumOsVersion: String?
        }
        let response = try JSONDecoder().decode(Response.self, from: data)
        guard !response.results.isEmpty else { return nil }
        guard response.results.count == 1, let result = response.results.first,
              result.trackId == AppStoreRelease.appID, result.bundleId == AppStoreRelease.bundleID,
              let version = result.version, let minimumOS = result.minimumOsVersion,
              let rawURL = result.trackViewUrl, let url = URL(string: rawURL) else {
            throw LookupError.invalidResponse
        }
        let release = AppStoreRelease(version: version, storeURL: url, minimumOSVersion: minimumOS)
        guard release.isValid else { throw LookupError.invalidResponse }
        return release
    }
}

struct AppUpdateEnvironment: Equatable, Sendable {
    let allowsAutomaticChecks: Bool
    let isTestFlight: Bool
    let isRunningTests: Bool

    static var current: Self {
        #if DEBUG
        let isDebug = true
        #else
        let isDebug = false
        #endif
        return make(isDebug: isDebug, receiptName: Bundle.main.appStoreReceiptURL?.lastPathComponent,
                    environment: ProcessInfo.processInfo.environment)
    }

    static func make(isDebug: Bool, receiptName: String?, environment: [String: String]) -> Self {
        let isTestFlight = receiptName == "sandboxReceipt"
        let isRunningTests = environment["XCTestConfigurationFilePath"] != nil
        return Self(allowsAutomaticChecks: !isDebug && !isTestFlight && !isRunningTests,
                    isTestFlight: isTestFlight, isRunningTests: isRunningTests)
    }
}

/// An optional Home card; checking never gates sign-in, writing or the keyboard.
@MainActor
@Observable
final class AppUpdateModel {
    enum Status: Equatable {
        case idle, checking, available, current, offline, unavailable, testFlight, openFailed
    }

    static let checkInterval: TimeInterval = 24 * 60 * 60
    static let reminderInterval: TimeInterval = 7 * 24 * 60 * 60

    let installedVersion: String
    private(set) var availableRelease: AppStoreRelease?
    private(set) var status: Status = .idle

    @ObservationIgnored private let fetcher: any AppStoreVersionFetching
    @ObservationIgnored private let defaults: UserDefaults
    @ObservationIgnored private let osVersion: String
    @ObservationIgnored private let environment: AppUpdateEnvironment
    @ObservationIgnored private let now: () -> Date
    @ObservationIgnored private var checkTask: Task<Void, Never>?
    /// Changing Later is observed even though UserDefaults itself is not.
    private var dismissedVersion: String?
    private var dismissedUntil: Date?

    private enum Key {
        static let lastCheck = "appUpdate.lastCheck.v1"
        static let release = "appUpdate.release.v1"
        static let dismissedVersion = "appUpdate.dismissedVersion.v1"
        static let dismissedUntil = "appUpdate.dismissedUntil.v1"
    }

    init(fetcher: (any AppStoreVersionFetching)? = nil, defaults: UserDefaults = .standard,
         installedVersion: String = BuildInfo.current.appVersion, osVersion: String = BuildInfo.current.osVersion,
         environment: AppUpdateEnvironment = .current, now: @escaping () -> Date = Date.init) {
        self.fetcher = fetcher ?? AppStoreLookupClient()
        self.defaults = defaults
        self.installedVersion = installedVersion
        self.osVersion = osVersion
        self.environment = environment
        self.now = now
        self.dismissedVersion = defaults.string(forKey: Key.dismissedVersion)
        self.dismissedUntil = defaults.object(forKey: Key.dismissedUntil) as? Date
        if environment.isTestFlight {
            status = .testFlight
        } else if !environment.isRunningTests,
                  let data = defaults.data(forKey: Key.release),
                  let release = try? JSONDecoder().decode(AppStoreRelease.self, from: data),
                  Self.isEligible(release, installedVersion: installedVersion, osVersion: osVersion) {
            availableRelease = release
            status = .available
        }
    }

    var isChecking: Bool { status == .checking }

    var showsUpdateCard: Bool {
        guard environment.allowsAutomaticChecks, !environment.isTestFlight, !environment.isRunningTests,
              let release = availableRelease, let version = MarketingVersion(release.version) else { return false }
        return dismissedVersion != version.storageKey || dismissedUntil.map { now() >= $0 } != false
    }

    var statusKey: String? {
        switch status {
        case .idle, .available: return nil
        case .checking: return "updates.checking"
        case .current: return "updates.current"
        case .offline: return "updates.offline"
        case .unavailable: return "updates.unavailable"
        case .testFlight: return "updates.testFlight"
        case .openFailed: return "updates.openFailed"
        }
    }

    func checkForUpdates(manual: Bool = false) async {
        guard !environment.isRunningTests else { return }
        guard !environment.isTestFlight else {
            availableRelease = nil
            status = .testFlight
            return
        }
        guard manual || environment.allowsAutomaticChecks else { return }
        if let checkTask {
            await checkTask.value
            return
        }
        if !manual, let last = defaults.object(forKey: Key.lastCheck) as? Date,
           now() >= last, now().timeIntervalSince(last) < Self.checkInterval { return }
        guard MarketingVersion(installedVersion) != nil, MarketingVersion(osVersion) != nil else {
            status = .unavailable
            return
        }
        defaults.set(now(), forKey: Key.lastCheck)
        status = .checking
        let task = Task { await performCheck() }
        checkTask = task
        await task.value
        checkTask = nil
    }

    func remindLater() {
        guard let version = availableRelease.flatMap({ MarketingVersion($0.version) }) else { return }
        dismissedVersion = version.storageKey
        dismissedUntil = now().addingTimeInterval(Self.reminderInterval)
        defaults.set(dismissedVersion, forKey: Key.dismissedVersion)
        defaults.set(dismissedUntil, forKey: Key.dismissedUntil)
    }

    func appStoreDidFailToOpen() { status = .openFailed }

    private func performCheck() async {
        do {
            guard let release = try await fetcher.latestRelease(), release.isValid else {
                clearRelease()
                status = .unavailable
                return
            }
            if Self.isEligible(release, installedVersion: installedVersion, osVersion: osVersion) {
                availableRelease = release
                if let data = try? JSONEncoder().encode(release) { defaults.set(data, forKey: Key.release) }
                status = .available
            } else {
                clearRelease()
                status = .current
            }
        } catch {
            // Keep a previously verified offer usable while offline.
            let code = (error as? URLError)?.code
            status = code == .notConnectedToInternet || code == .networkConnectionLost ? .offline : .unavailable
        }
    }

    private func clearRelease() {
        availableRelease = nil
        defaults.removeObject(forKey: Key.release)
    }

    private static func isEligible(_ release: AppStoreRelease, installedVersion: String, osVersion: String) -> Bool {
        guard release.isValid, let latest = MarketingVersion(release.version),
              let installed = MarketingVersion(installedVersion), let system = MarketingVersion(osVersion),
              let minimum = MarketingVersion(release.minimumOSVersion) else { return false }
        return latest > installed && minimum <= system
    }
}

#if DEBUG
extension AppUpdateModel {
    /// A local sample for the existing screenshot routes; no store request.
    /// `-AIReplyDebugUpdate available | current | offline | testFlight`.
    static func debugPreviewIfRequested() -> AppUpdateModel? {
        guard let state = DebugScreen.value(after: "-AIReplyDebugUpdate"),
              ["available", "current", "offline", "testFlight"].contains(state) else { return nil }
        let defaults = UserDefaults(suiteName: "AppUpdatePreview.\(UUID().uuidString)")!
        let model = AppUpdateModel(fetcher: PreviewFetcher(), defaults: defaults,
                                   environment: AppUpdateEnvironment(allowsAutomaticChecks: true,
                                                                     isTestFlight: state == "testFlight",
                                                                     isRunningTests: false))
        // Keep lifecycle tasks on this sample from replacing the shown state.
        defaults.set(Date(), forKey: Key.lastCheck)
        switch state {
        case "available":
            let current = MarketingVersion(model.installedVersion)?.storageKey.split(separator: ".").compactMap { Int($0) }
            let version = current.map { "\($0[0]).\($0[1]).\($0[2] + 1)" } ?? "2.0.0"
            model.availableRelease = AppStoreRelease(version: version,
                                                      storeURL: URL(string: "https://apps.apple.com/app/id6818570150")!,
                                                      minimumOSVersion: "17.0")
            model.status = .available
        case "offline": model.status = .offline
        case "testFlight": model.status = .testFlight
        default: model.status = .current
        }
        return model
    }

    private struct PreviewFetcher: AppStoreVersionFetching {
        func latestRelease() async throws -> AppStoreRelease? { throw URLError(.notConnectedToInternet) }
    }
}
#endif
