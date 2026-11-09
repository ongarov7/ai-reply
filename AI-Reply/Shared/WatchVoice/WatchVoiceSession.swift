import Foundation

// MARK: - Keyboard session

/// One appearance of the keyboard on one host field, as published in the App
/// Group. Identifiers and flags only: never anything from the field.
struct WatchKeyboardSession: Codable, Equatable, Sendable {
    let id: String
    let documentID: String?
    let startedAt: Date
    var updatedAt: Date
    var isActive: Bool
    /// The user tapped Watch Voice and the keyboard is listening for a command.
    var isWaiting: Bool
    let hasFullAccess: Bool

    /// A keyboard killed while on screen never says goodbye. It writes no
    /// heartbeats (no timers for this), so its session simply stops counting.
    static let maximumSilence: TimeInterval = 30 * 60

    func isLive(now: Date = Date()) -> Bool {
        isActive && now.timeIntervalSince(updatedAt) < Self.maximumSilence
    }
}

// MARK: - Watch link

/// What `WCSession` says on the phone, written by the app for the keyboard,
/// which cannot ask (WatchConnectivity is the containing app's).
struct WatchLinkSnapshot: Codable, Equatable, Sendable {
    var isSupported = false
    var isActivated = false
    var isPaired = false
    var isWatchAppInstalled = false
    var isReachable = false
    var updatedAt: Date
    /// The last time anything arrived from the watch app.
    var lastWatchContactAt: Date?

    /// How long a contact from the watch counts as "it is open". A suspended
    /// app cannot report the watch going away, so `isReachable` alone can be
    /// left true for hours; a recent contact cannot.
    static let contactFreshness: TimeInterval = 60

    func connectionState(now: Date = Date()) -> WatchConnectionState {
        guard isSupported else { return .unsupported }
        guard isActivated else { return .activating }
        guard isPaired else { return .notPaired }
        guard isWatchAppInstalled else { return .appNotInstalled }
        if let lastWatchContactAt, now.timeIntervalSince(lastWatchContactAt) <= Self.contactFreshness {
            return .connected
        }
        // Fresh from `sessionReachabilityDidChange` while the app is awake.
        if isReachable, now.timeIntervalSince(updatedAt) <= Self.contactFreshness {
            return .connected
        }
        return .appNotOpen
    }
}

/// The states `WCSession` distinguishes, in the words of what is true. A paired
/// watch is not an installed app, and an installed app is not a reachable one.
enum WatchConnectionState: Equatable, Sendable {
    /// No Apple Watch support on this device (an iPad).
    case unsupported
    case activating
    case notPaired
    case appNotInstalled
    /// Installed; not known to be open right now.
    case appNotOpen
    case connected
}

/// The small control in the keyboard's row.
enum WatchKeyboardIndicator: Equatable, Sendable {
    /// No paired watch: the keyboard is exactly what it was.
    case hidden
    case installApp
    case openWatchApp
    case ready
    case waiting
    case instructionReceived
    case generating
    case responseReady
    case connectionLost
}

// MARK: - Insertion

/// Where an insert would land, as far as a keyboard can know.
///
/// `documentIdentifier` is not "this WhatsApp chat": an app may reuse a field
/// across conversations. So the text around the caret is part of the check,
/// held in memory for a few seconds and never written anywhere.
struct WatchInsertContext: Equatable, Sendable {
    var sessionID: String
    /// nil when the host reports none, or the all-zero UUID.
    var documentID: UUID?
    var textBeforeCaret: String
    var textAfterCaret: String

    /// Whether the field looks empty. A host can report nothing for a field
    /// with text in it, which is what the other checks are for.
    var isFieldEmpty: Bool {
        (textBeforeCaret + textAfterCaret).trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    static func validDocumentID(_ raw: UUID?) -> UUID? {
        guard let raw, raw != UUID(uuid: UUID_NULL) else { return nil }
        return raw
    }
}

private let UUID_NULL: uuid_t = (0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)

enum WatchInsertDecision: Equatable, Sendable {
    case automatic
    case manual(Reason)

    enum Reason: Equatable, Sendable {
        /// The Auto Insert switch is off.
        case preferenceOff
        /// The command was not meant for this keyboard session.
        case notBound
        case keyboardNotVisible
        case sessionChanged
        case documentChanged
        case documentUnknown
        case textChanged
        /// Automatic insert never touches what the user has written.
        case fieldHasText
        case alreadyInserted
    }
}

/// The one rule for whether a generated text may reach the host field by
/// itself. Everything not positively verified is a manual "Insert Response".
///
/// Pure: the keyboard reads the live state and asks; the tests pin the answer.
enum WatchInsertPolicy {

    /// - Parameters:
    ///   - isAutoInsertEnabled: the user's switch in the app's settings.
    ///   - bound: the field as it was when generation started, for a command
    ///     meant for this session; nil for any other command.
    ///   - current: the field now; nil with the keyboard off screen.
    static func decide(
        isAutoInsertEnabled: Bool,
        bound: WatchInsertContext?,
        current: WatchInsertContext?,
        alreadyInserted: Bool
    ) -> WatchInsertDecision {
        guard !alreadyInserted else { return .manual(.alreadyInserted) }
        guard isAutoInsertEnabled else { return .manual(.preferenceOff) }
        guard let bound else { return .manual(.notBound) }
        guard let current else { return .manual(.keyboardNotVisible) }
        guard current.sessionID == bound.sessionID else { return .manual(.sessionChanged) }
        guard let boundDocument = bound.documentID, let currentDocument = current.documentID else {
            return .manual(.documentUnknown)
        }
        guard boundDocument == currentDocument else { return .manual(.documentChanged) }
        guard current.textBeforeCaret == bound.textBeforeCaret,
              current.textAfterCaret == bound.textAfterCaret else { return .manual(.textChanged) }
        guard current.isFieldEmpty else { return .manual(.fieldHasText) }
        return .automatic
    }
}

// MARK: - Settings

extension SharedSettings {

    private static let watchAutoInsertKey = "shared.watchAutoInsert"

    /// On by default: generated text goes into the field the user explicitly
    /// started from, only when every check in `WatchInsertPolicy` passes,
    /// and never sends anything.
    var watchAutoInsertEnabled: Bool {
        defaults.object(forKey: Self.watchAutoInsertKey) as? Bool ?? true
    }

    func setWatchAutoInsertEnabled(_ enabled: Bool) {
        defaults.set(enabled, forKey: Self.watchAutoInsertKey)
    }
}
