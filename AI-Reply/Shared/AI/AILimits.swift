import Foundation

/// The character limits the server publishes at `/api/v1/config`.
///
/// THE SERVER IS THE SOURCE OF TRUTH. An administrator changes these in the
/// admin panel without an app release, and the server enforces them on every
/// request whatever a client believes. The app and the keyboard only mirror
/// them so the user is told BEFORE a request is spent: the last published
/// values are cached in the App Group and `fallback` applies only until the
/// first config has been read.
///
/// Characters, tokens and quotas are three different things and stay apart:
/// this type is characters only. Output tokens never leave the server, and the
/// daily quota arrives with every reply's usage block.
struct AILimits: Equatable, Sendable {

    /// Longest incoming message, in Unicode scalars - what the user sees as
    /// characters in Kazakh, Russian and English, and what the server counts.
    var sourceCharacters: Int
    /// Longest reply instruction the server accepts, language rule included.
    var instructionCharacters: Int

    static let fallback = AILimits(sourceCharacters: 400, instructionCharacters: 400)

    /// What a published value has to fall inside to be believed - the same
    /// ranges the admin panel enforces. Anything outside them is a broken
    /// payload, and the previous value is kept.
    static let sourceRange: ClosedRange<Int> = 50...2000
    static let instructionRange: ClosedRange<Int> = 50...1000
}

extension AILimits {

    /// Limits built from published values. Each is checked on its own, so one
    /// bad field cannot throw away a good one.
    static func published(source: Int, instruction: Int, previous: AILimits = .fallback) -> AILimits {
        AILimits(
            sourceCharacters: sourceRange.contains(source) ? source : previous.sourceCharacters,
            instructionCharacters: instructionRange.contains(instruction) ? instruction : previous.instructionCharacters
        )
    }
}

// MARK: - Cache

extension AILimits {

    private enum Key {
        static let source = "ai.limits.sourceCharacters"
        static let instruction = "ai.limits.instructionCharacters"
        static let syncedAt = "ai.limits.syncedAt"
        static let replyPreferences = "ai.features.replyPreferences"
        static let senderProfile = "ai.features.senderProfile"
        static let instructionPolish = "ai.features.instructionPolish"
        static let aiReports = "ai.features.aiReports"
    }

    private static let lock = NSLock()
    private static var memory: AILimits?

    /// The limits in effect. Served from memory after the first read, so the
    /// composer can check a count on every keystroke without touching disk.
    static var current: AILimits {
        lock.lock()
        defer { lock.unlock() }
        if let memory { return memory }
        let loaded = load(from: AppGroup.defaults)
        memory = loaded
        return loaded
    }

    /// Re-reads the App Group - e.g. when the keyboard appears after the app
    /// refreshed the config.
    static func reload(defaults: UserDefaults = AppGroup.defaults) {
        let loaded = load(from: defaults)
        lock.lock()
        memory = loaded
        lock.unlock()
    }

    static func load(from defaults: UserDefaults) -> AILimits {
        // A missing key reads as 0, which is outside both ranges: fallback.
        published(
            source: defaults.integer(forKey: Key.source),
            instruction: defaults.integer(forKey: Key.instruction)
        )
    }

    /// Stores what the server published.
    static func store(sourceCharacters: Int, instructionCharacters: Int, defaults: UserDefaults = AppGroup.defaults) {
        let next = published(
            source: sourceCharacters,
            instruction: instructionCharacters,
            previous: load(from: defaults)
        )
        defaults.set(next.sourceCharacters, forKey: Key.source)
        defaults.set(next.instructionCharacters, forKey: Key.instruction)
        defaults.set(Date().timeIntervalSince1970, forKey: Key.syncedAt)
        lock.lock()
        memory = next
        lock.unlock()
    }

    /// The server rejected a message and said what its limit really is. The
    /// client corrects itself on the spot instead of waiting for the next
    /// config refresh.
    static func storeSourceLimit(_ limit: Int, defaults: UserDefaults = AppGroup.defaults) {
        let current = load(from: defaults)
        store(sourceCharacters: limit, instructionCharacters: current.instructionCharacters, defaults: defaults)
    }

    static func lastSynced(defaults: UserDefaults = AppGroup.defaults) -> Date? {
        let stamp = defaults.double(forKey: Key.syncedAt)
        return stamp > 0 ? Date(timeIntervalSince1970: stamp) : nil
    }

    /// Whether the server understands the reply-preference fields of the
    /// `profile` block. An older server rejects unknown fields outright, so
    /// they are only sent once the server has said it knows them.
    static var serverSupportsReplyPreferences: Bool {
        AppGroup.defaults.bool(forKey: Key.replyPreferences)
    }

    /// Whether the server accepts the sender fields: `grammatical_gender` in
    /// the reply and compose `profile`, top-level `input_language`, and
    /// `grammatical_gender` / `onboarding_version` on `PATCH /api/v1/me`. Such
    /// a server also honours a language the instruction names by itself, so
    /// the client stops appending `ReplyInstruction.languageRule`.
    static var serverSupportsSenderProfile: Bool {
        AppGroup.defaults.bool(forKey: Key.senderProfile)
    }

    /// Whether `POST /api/v1/ai/polish` exists and is switched on.
    static var serverSupportsInstructionPolish: Bool {
        AppGroup.defaults.bool(forKey: Key.instructionPolish)
    }

    /// Whether `POST /api/v1/ai/reports` takes reports about a generated
    /// text: the Report control is shown only then.
    static var serverSupportsAIReports: Bool {
        AppGroup.defaults.bool(forKey: Key.aiReports)
    }

    /// Stores the request-shaping flags. Anything the server did not publish
    /// is false: a field is only ever sent to a server that asked for it.
    static func storeFeatures(_ features: AccountAPI.Features?, defaults: UserDefaults = AppGroup.defaults) {
        defaults.set(features?.replyPreferences ?? false, forKey: Key.replyPreferences)
        defaults.set(features?.senderProfile ?? false, forKey: Key.senderProfile)
        defaults.set(features?.instructionPolish ?? false, forKey: Key.instructionPolish)
        defaults.set(features?.aiReports ?? false, forKey: Key.aiReports)
    }

    /// Everything `/api/v1/config` publishes that the reply flow uses,
    /// including the legal versions the keyboard checks consent against.
    static func apply(_ config: AccountAPI.ServerConfig, defaults: UserDefaults = AppGroup.defaults) {
        store(
            sourceCharacters: config.maxSourceCharacters,
            instructionCharacters: config.maxInstructionLength,
            defaults: defaults
        )
        storeFeatures(config.features, defaults: defaults)
        LegalConsentStore.storeCurrentVersions(config.legal ?? .production, defaults: defaults)
    }
}

// MARK: - Refresh

/// Keeps the cached limits current from the keyboard itself, so a limit an
/// administrator changes reaches users who have not opened the app since.
///
/// At most one request every six hours, in the background, and only with Full
/// Access - without it the keyboard has no network. It never runs on a
/// keystroke and nothing waits for it.
enum AILimitsRefresher {

    static let interval: TimeInterval = 6 * 60 * 60
    private static var inFlight = false

    /// Main thread only.
    static func refreshIfStale(now: Date = Date()) {
        if let last = AILimits.lastSynced(), now.timeIntervalSince(last) < interval { return }
        guard !inFlight else { return }
        let baseURL = AIConfiguration.resolvedBaseURL
        inFlight = true
        Task.detached(priority: .utility) {
            let client = APIClient(baseURL: baseURL)
            let config: AccountAPI.ServerConfig? = try? await client.get("api/v1/config")
            if let config { AILimits.apply(config) }
            await MainActor.run { inFlight = false }
        }
    }
}
