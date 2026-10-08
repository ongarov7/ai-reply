import Foundation
import Observation
import UserNotifications

/// iOS notification permission, spelled the way the server stores it.
enum NotificationPermission: String, Equatable, Sendable {
    case authorized
    case denied
    case notDetermined = "not_determined"
    case provisional
    case ephemeral
    case unknown

    init(_ status: UNAuthorizationStatus) {
        switch status {
        case .authorized:    self = .authorized
        case .denied:        self = .denied
        case .notDetermined: self = .notDetermined
        case .provisional:   self = .provisional
        case .ephemeral:     self = .ephemeral
        @unknown default:    self = .unknown
        }
    }

    /// Whether iOS will show this app's notifications at all.
    var allowsDelivery: Bool {
        self == .authorized || self == .provisional || self == .ephemeral
    }
}

/// Asks iOS about notification permission. A protocol so the model can be
/// exercised without the system prompt.
protocol NotificationAuthorizing: Sendable {
    func currentPermission() async -> NotificationPermission
    /// Shows the system prompt when the permission is not determined yet.
    func requestPermission() async
}

struct SystemNotificationAuthorizer: NotificationAuthorizing {
    func currentPermission() async -> NotificationPermission {
        await withCheckedContinuation { continuation in
            UNUserNotificationCenter.current().getNotificationSettings { settings in
                continuation.resume(returning: NotificationPermission(settings.authorizationStatus))
            }
        }
    }

    func requestPermission() async {
        // Alerts and sounds. No badge: the app never sets one.
        do {
            _ = try await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound])
        } catch {
            ReplyLog.event("push: permission request failed: \(error)")
        }
    }
}

/// Notification categories of the signed-in account.
struct NotificationPreferences: Decodable, Equatable, Sendable {
    var values: [String: Bool]
    /// Categories the user may switch off. `security` is never among them.
    var optional: [String]

    /// Display order; the same order the server keeps them in.
    static let categories = ["account", "subscription", "security", "system", "marketing"]

    enum CodingKeys: String, CodingKey {
        case values = "preferences"
        case optional
    }

    init(values: [String: Bool], optional: [String]) {
        self.values = values
        self.optional = optional
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        values = try container.decode([String: Bool].self, forKey: .values)
        optional = try container.decodeIfPresent([String].self, forKey: .optional)
            ?? Self.categories.filter { $0 != "security" }
    }

    func isOptional(_ category: String) -> Bool { optional.contains(category) }

    /// News and offers need the user's own yes; everything else is on until
    /// switched off.
    static func isOnByDefault(_ category: String) -> Bool { category != "marketing" }

    /// A category the server did not mention has its default.
    func isOn(_ category: String) -> Bool { values[category] ?? Self.isOnByDefault(category) }

    /// What a new account starts with, shown until the server has answered.
    static let defaults = NotificationPreferences(
        values: Dictionary(uniqueKeysWithValues: categories.map { ($0, isOnByDefault($0)) }),
        optional: categories.filter { $0 != "security" }
    )
}

/// Reads and changes the account's categories. Throws `APIError`.
protocol NotificationPreferencesService: Sendable {
    func load() async throws -> NotificationPreferences
    func save(_ changes: [String: Bool]) async throws -> NotificationPreferences
}

/// What the notification UI shows, and what the user can change.
///
/// Хабарлама күйі: iOS рұқсаты, қосымшадағы ауыстырғыш, санаттар.
///
/// Holds no networking and no push plumbing of its own. It tells
/// `AppServices` through closures when something changed that the server
/// should hear about and when permission was just granted (time to ask for
/// a token).
@MainActor
@Observable
final class PushNotificationsModel {

    enum PreferencesState: Equatable {
        case idle, loading, loaded, failed
    }

    private(set) var permission: NotificationPermission = .unknown
    /// False until iOS has been asked once; registrations wait for it, so the
    /// first one already carries the real permission.
    private(set) var hasReadPermission = false
    private(set) var isEnabledInApp: Bool
    private(set) var preferences: NotificationPreferences?
    private(set) var preferencesState: PreferencesState = .idle
    /// A category whose change the server refused; the switch went back.
    private(set) var failedCategory: String?
    private(set) var savingCategories: Set<String> = []
    private(set) var isRequestingPermission = false
    private(set) var isSignedIn = false
    /// `features.push_notifications` from the server; nil until it answered.
    private(set) var serverDeliversPush: Bool?
    /// The build asks for push (AIREPLY_PUSH_NOTIFICATIONS) and Firebase is
    /// configured. Set by `AppServices` at launch.
    private(set) var buildSupportsPush: Bool
    /// DEBUG `-AIReplyForcePushCard YES`: shows the card and the Settings
    /// sections whatever the state, so both can be reviewed on a Simulator.
    let forcesPushUI: Bool

    private var isCardDismissed: Bool
    /// "Not now" or "Turn on" in a forced DEBUG launch: hidden for this launch only.
    private var isCardHiddenThisLaunch = false

    @ObservationIgnored private let authorizer: NotificationAuthorizing
    @ObservationIgnored private let preferencesService: NotificationPreferencesService
    @ObservationIgnored private let store: NotificationSettingsStore

    /// The installation's state changed: the server should hear about it.
    @ObservationIgnored var onStateChange: (() -> Void)?
    /// Permission now allows notifications: time to ask for a token.
    @ObservationIgnored var onPermissionAllowsDelivery: (() -> Void)?

    init(authorizer: NotificationAuthorizing = SystemNotificationAuthorizer(),
         preferencesService: NotificationPreferencesService,
         store: NotificationSettingsStore = NotificationSettingsStore(),
         buildSupportsPush: Bool = false,
         forcesPushUI: Bool = false) {
        self.authorizer = authorizer
        self.preferencesService = preferencesService
        self.store = store
        self.buildSupportsPush = buildSupportsPush
        self.forcesPushUI = forcesPushUI
        self.isEnabledInApp = store.isEnabledInApp
        self.isCardDismissed = store.isPermissionCardDismissed
    }

    // MARK: What is shown

    /// Pushes can reach this build at all: the build is set up for FCM and
    /// the server can send through it. Until the server says so, nothing
    /// about notifications is shown - an older server keeps the app as it was.
    var isPushAvailable: Bool {
        buildSupportsPush && serverDeliversPush == true
    }

    /// Home's soft card: signed in, pushes possible, never asked, not dismissed.
    var showsPermissionCard: Bool {
        if forcesPushUI { return !isCardHiddenThisLaunch }
        return isSignedIn && isPushAvailable && permission == .notDetermined && !isCardDismissed
    }

    /// Settings ▸ Notifications.
    var showsNotificationSettings: Bool {
        forcesPushUI || isPushAvailable
    }

    /// The per-category switches: they belong to an account.
    var showsCategories: Bool {
        showsNotificationSettings && (isSignedIn || forcesPushUI)
    }

    /// Categories to render: the server's, or everything on while it answers.
    var displayedPreferences: NotificationPreferences {
        preferences ?? .defaults
    }

    // MARK: State from outside

    func setSignedIn(_ signedIn: Bool) {
        guard signedIn != isSignedIn else { return }
        isSignedIn = signedIn
        preferences = nil
        preferencesState = .idle
        failedCategory = nil
    }

    func setServerDeliversPush(_ value: Bool?) {
        serverDeliversPush = value
    }

    func setBuildSupportsPush(_ value: Bool) {
        buildSupportsPush = value
    }

    /// Categories that arrived with an installation registration.
    func adoptPreferences(_ values: [String: Bool]) {
        guard isSignedIn, savingCategories.isEmpty else { return }
        var next = preferences ?? .defaults
        next.values = values
        preferences = next
        if preferencesState != .loading { preferencesState = .loaded }
    }

    /// Re-reads the permission; the user may have changed it in iOS Settings.
    func refreshPermission() async {
        apply(await authorizer.currentPermission())
    }

    private func apply(_ next: NotificationPermission) {
        let isFirstReading = !hasReadPermission
        hasReadPermission = true
        guard next != permission || isFirstReading else { return }
        permission = next
        if next.allowsDelivery { onPermissionAllowsDelivery?() }
        onStateChange?()
    }

    // MARK: Actions

    /// "Turn on notifications": the system prompt, then a token. What iOS
    /// reports afterwards decides, not the prompt's answer: a user may have
    /// changed it in Settings meanwhile.
    func requestPermission() async {
        guard !isRequestingPermission else { return }
        isRequestingPermission = true
        defer { isRequestingPermission = false }
        if forcesPushUI { isCardHiddenThisLaunch = true }

        await authorizer.requestPermission()
        let status = await authorizer.currentPermission()
        let before = permission
        permission = status
        hasReadPermission = true
        if status.allowsDelivery { onPermissionAllowsDelivery?() }
        if status != before { onStateChange?() }
    }

    /// "Not now" on the card. Settings keeps the way to turn them on.
    func dismissCard() {
        if forcesPushUI {
            isCardHiddenThisLaunch = true
            return
        }
        isCardDismissed = true
        store.setPermissionCardDismissed(true)
    }

    func setEnabledInApp(_ enabled: Bool) {
        guard enabled != isEnabledInApp else { return }
        isEnabledInApp = enabled
        store.setEnabledInApp(enabled)
        onStateChange?()
    }

    // MARK: Categories

    func loadPreferences() async {
        guard isSignedIn, isPushAvailable || forcesPushUI, preferencesState != .loading,
              savingCategories.isEmpty else { return }
        preferencesState = .loading
        do {
            let loaded = try await preferencesService.load()
            guard isSignedIn else { return }
            preferences = loaded
            preferencesState = .loaded
        } catch {
            preferencesState = .failed
        }
    }

    func isSaving(_ category: String) -> Bool { savingCategories.contains(category) }

    /// Switches a category on or off right away and tells the server; if the
    /// server refuses or cannot be reached, the switch goes back.
    func setCategory(_ category: String, enabled: Bool) async {
        var current = displayedPreferences
        guard current.isOptional(category), current.isOn(category) != enabled,
              !savingCategories.contains(category) else { return }

        let previous = current.values[category]
        current.values[category] = enabled
        preferences = current
        failedCategory = nil
        savingCategories.insert(category)
        defer { savingCategories.remove(category) }

        do {
            var saved = try await preferencesService.save([category: enabled])
            // Another switch may have changed while this one was saving: keep
            // what the user sees for those until their own answer arrives.
            if let shown = preferences {
                for other in savingCategories where other != category {
                    saved.values[other] = shown.values[other]
                }
            }
            preferences = saved
            preferencesState = .loaded
        } catch {
            if var rolledBack = preferences {
                rolledBack.values[category] = previous
                preferences = rolledBack
            }
            failedCategory = category
        }
    }
}
