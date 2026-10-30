import Foundation

/// Where the app talks to and how it introduces itself. Read once at launch.
public struct AppConfiguration: Sendable, Equatable {
    /// Backend root, e.g. https://ai-reply.kz (Release) or https://ai-reply.kz/sandbox (Debug).
    public let backendBaseURL: URL
    public let appVersion: String
    public let build: String
    /// Always "macos" for this client.
    public let platform: String
    public let osVersion: String
    public let bundleIdentifier: String

    public init(backendBaseURL: URL, appVersion: String, build: String, platform: String = "macos",
                osVersion: String = AppConfiguration.currentOSVersion(), bundleIdentifier: String = "kz.ai-reply.desktop") {
        self.backendBaseURL = backendBaseURL
        self.appVersion = appVersion
        self.build = build
        self.platform = platform
        self.osVersion = osVersion
        self.bundleIdentifier = bundleIdentifier
    }

    public static let productionURL = URL(string: "https://ai-reply.kz")!

    /// Values from the app's Info.plist. A missing or non-HTTPS backend URL falls back to production.
    /// Debug builds also accept `-AIReplyBackendURL http://127.0.0.1:<port>` for a local server.
    public static func fromMainBundle(_ bundle: Bundle = .main, defaults: UserDefaults = .standard) -> AppConfiguration {
        let info = bundle.infoDictionary ?? [:]
        var raw = (info["AIReplyBackendBaseURL"] as? String)?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        #if DEBUG
        if let local = defaults.string(forKey: "AIReplyBackendURL"), let url = URL(string: local), isLocal(url) {
            raw = local
        }
        #endif
        let url = URL(string: raw).flatMap { ($0.scheme == "https" && $0.host != nil) || isLocal($0) ? $0 : nil } ?? productionURL
        return AppConfiguration(
            backendBaseURL: url,
            appVersion: info["CFBundleShortVersionString"] as? String ?? "0",
            build: info["CFBundleVersion"] as? String ?? "0",
            bundleIdentifier: bundle.bundleIdentifier ?? "kz.ai-reply.desktop"
        )
    }

    /// Plain HTTP only to this Mac, for a developer's local backend.
    static func isLocal(_ url: URL) -> Bool {
        url.scheme == "http" && ["127.0.0.1", "localhost"].contains(url.host ?? "")
    }

    public static func currentOSVersion() -> String {
        let v = ProcessInfo.processInfo.operatingSystemVersion
        return "\(v.majorVersion).\(v.minorVersion).\(v.patchVersion)"
    }

    /// A page of the AI Reply website under the same base, e.g. "payment" → https://ai-reply.kz/payment.
    public func websiteURL(_ path: String, query: [URLQueryItem] = []) -> URL {
        var components = URLComponents(url: backendBaseURL, resolvingAgainstBaseURL: false)!
        let base = components.path.hasSuffix("/") ? String(components.path.dropLast()) : components.path
        components.path = base + "/" + path.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
        components.queryItems = query.isEmpty ? nil : query
        return components.url!
    }
}
