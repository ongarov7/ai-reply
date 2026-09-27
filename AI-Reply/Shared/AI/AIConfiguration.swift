import Foundation

/// Fixed production configuration shared by the app and keyboard extension.
struct AIConfiguration: Sendable {

    static let productionBaseURL = URL(string: "https://api.meily.kz")!
    static let requestTimeout: TimeInterval = 25

    /// Longest incoming message, as the server last published it (see
    /// `AILimits`). Not a constant: the administrator owns this number.
    static var maximumMessageCharacters: Int { AILimits.current.sourceCharacters }

    static let shared = AIConfiguration()

    init() {
        LegacyCredentialMigration.runOnce()
    }

    var backendBaseURL: URL? { Self.resolvedBaseURL }
    var requiresAccount: Bool { true }
    var isReady: Bool { AccountCredentials.isSignedIn }

#if DEBUG

    /// DEBUG ONLY. Where development traffic goes.
    ///
    /// WHY THIS EXISTS. The base URL used to be the production constant with no
    /// way past it, so there was no way to exercise the reply flow without
    /// spending real requests against `api.meily.kz`. A release build cannot
    /// reach any of this - the whole block is compiled out - so production
    /// behaviour is unchanged and there is still no way to ship pointing
    /// somewhere else by accident.
    ///
    /// RESOLUTION ORDER:
    ///   1. `AIREPLY_BASE_URL` in the process environment, which is what an
    ///      Xcode scheme or `xcodebuild test` sets.
    ///   2. `debugBaseURLKey` in the APP GROUP defaults. A keyboard extension
    ///      is launched by iOS and never sees a scheme's environment, so the
    ///      value read from the environment is mirrored here once and the
    ///      keyboard picks up the same host the app was pointed at.
    ///   3. Production.
    ///
    /// Deliberately NOT one of the keys `LegacyCredentialMigration` clears: a
    /// developer's test host should survive the migration that removes the old
    /// direct-provider overrides.
    static let debugBaseURLKey = "ai.debugBaseURL"

    static var resolvedBaseURL: URL {
        if let raw = ProcessInfo.processInfo.environment["AIREPLY_BASE_URL"],
           let url = URL(string: raw), url.scheme != nil {
            if AppGroup.defaults.string(forKey: debugBaseURLKey) != raw {
                AppGroup.defaults.set(raw, forKey: debugBaseURLKey)
            }
            return url
        }
        if let raw = AppGroup.defaults.string(forKey: debugBaseURLKey),
           let url = URL(string: raw), url.scheme != nil {
            return url
        }
        return productionBaseURL
    }

    /// Points this install at a test backend, or back at production when
    /// `nil`. Callable from the app's own debug affordances and from tests.
    static func setDebugBaseURL(_ url: URL?) {
        if let url {
            AppGroup.defaults.set(url.absoluteString, forKey: debugBaseURLKey)
        } else {
            AppGroup.defaults.removeObject(forKey: debugBaseURLKey)
        }
    }

    static var isUsingProductionBackend: Bool { resolvedBaseURL == productionBaseURL }

    /// DEBUG ONLY. `-AIReplyMockReplies` on the app's launch arguments makes
    /// the keyboard answer from canned replies instead of the backend, so the
    /// whole Generate -> Edit -> Regenerate -> Insert flow can be exercised in
    /// the Simulator without an account. Mirrored through the App Group
    /// because the keyboard never sees the app's launch arguments; launching
    /// without the argument switches it off again.
    static let debugMockRepliesKey = "debug.mockReplies"

    static var debugMockReplies: Bool {
        AppGroup.defaults.bool(forKey: debugMockRepliesKey)
    }

    static func applyDebugLaunchArguments(_ arguments: [String] = CommandLine.arguments) {
        let mock = arguments.contains("-AIReplyMockReplies")
        if AppGroup.defaults.bool(forKey: debugMockRepliesKey) != mock {
            AppGroup.defaults.set(mock, forKey: debugMockRepliesKey)
        }
    }

#else

    static var resolvedBaseURL: URL { productionBaseURL }

#endif
}
