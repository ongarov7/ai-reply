import SwiftUI
import UIKit
import UserNotifications

/// Wires push notifications, the installation record and deep links into the
/// app's life.
///
/// Push, орнату және сілтемелер — қосымшаның өмір циклына осы жерде қосылады.
///
/// One instance for the process, started by the app delegate at launch. The
/// screens never talk to it directly: they see `PushNotificationsModel` and
/// `AppRouter` through the environment. What happens when:
///
/// - Launch: Firebase is configured when this build has its options, and the
///   notification delegate is set so a tap that launched the app is not lost.
/// - Nothing is registered and no token is asked for before the legal
///   consent; right after it, the installation registers.
/// - After the account bootstrap, and on every sign-in, sign-out, token,
///   permission, switch or language change: the installation is registered if
///   the server offers it (`features.installations`) and the payload differs
///   from the last accepted one, else at most once a day.
/// - The APNs token is asked for once per launch, only after the consent,
///   with permission, and when the server can send pushes
///   (`features.push_notifications`). FCM turns it into the token the server
///   sends to.
/// - Foreground: the permission is re-read and the registration checked.
///
/// A server without these features sees none of it: no registration, no
/// token, and the app works exactly as before.
@MainActor
final class AppServices {

    static let shared = AppServices()

    /// What `AccountModel` knows, mirrored in by the app's root view.
    struct AccountState: Equatable {
        var isBootstrapComplete = false
        /// The legal consent screen was accepted (for the current versions).
        var hasAcceptedLegal = false
        var isSignedIn = false
        var userID: String?
        var features: AccountAPI.Features?
    }

    let router = AppRouter()
    let notifications: PushNotificationsModel

    /// The app's interface language, from `AppSettings`.
    var appLanguage: () -> String = { SharedSettings.shared.effectiveAppLanguage.rawValue }
    /// Runs after a tapped notification was handled; the app refreshes the
    /// account, so a quota or plan notification opens on current numbers.
    var onNotificationOpened: (() -> Void)?

    private let store: NotificationSettingsStore
    private let identity: InstallationIdentity
    private let registrar: InstallationRegistrar
    private let openedReporter: NotificationOpenedReporting
    private let notificationDelegate = NotificationCenterDelegate()

    private var isStarted = false
    private var account = AccountState()

    private var syncTask: Task<Void, Never>?
    private var syncAgain = false
    private var retryTask: Task<Void, Never>?
    private var syncFailures = 0

    private var hasRequestedDeviceToken = false
    /// APNs handed over its token this launch: FCM can make one now.
    private var hasAPNsToken = false
    private var tokenTask: Task<Void, Never>?
    /// The server said FCM no longer takes the current token.
    private var replacesTokenOnNextFetch = false
    private var hasReplacedInvalidToken = false
    /// Delivery ids of tapped notifications, until the server can be told.
    private var openedDeliveries: [String] = []

    private init() {
        let store = NotificationSettingsStore()
        self.store = store
        self.identity = InstallationIdentity()
        self.registrar = InstallationRegistrar(
            transport: BackendInstallationTransport(), tokens: AccountSession.shared, store: store)
        self.openedReporter = BackendNotificationOpenedReporter()
        #if DEBUG
        let forcesPushUI = DebugLaunchOptions.forcesPushUI
        #else
        let forcesPushUI = false
        #endif
        self.notifications = PushNotificationsModel(
            preferencesService: BackendNotificationPreferencesService(), store: store,
            forcesPushUI: forcesPushUI)

        notifications.onStateChange = { [weak self] in self?.requestInstallationSync() }
        notifications.onPermissionAllowsDelivery = { [weak self] in self?.requestDeviceTokenIfPossible() }
    }

    /// Unit tests run inside the app; they build their own instances and must
    /// not find this one configuring Firebase, registering or setting delegates.
    private static var isRunningUnitTests: Bool {
        ProcessInfo.processInfo.environment["XCTestConfigurationFilePath"] != nil
    }

    // MARK: Launch

    /// `application(_:didFinishLaunchingWithOptions:)`.
    func start() {
        guard !isStarted, !Self.isRunningUnitTests else { return }
        isStarted = true

        // Set here, not later: a tap that launched the app is delivered as
        // soon as launching finishes.
        UNUserNotificationCenter.current().delegate = notificationDelegate

        let supportsPush = PushBuildConfiguration.isEnabledInThisBuild && FirebasePush.configureIfPossible()
        notifications.setBuildSupportsPush(supportsPush)
        if supportsPush {
            FirebasePush.observeTokens { [weak self] token in self?.didReceivePushToken(token) }
        }

        #if DEBUG
        applyDebugLaunchOptions()
        #endif
        Task { await notifications.refreshPermission() }
    }

    // MARK: Inputs from the app

    func accountDidChange(_ state: AccountState) {
        let previous = account
        account = state
        notifications.setSignedIn(state.isSignedIn)
        notifications.setServerDeliversPush(state.features.map { $0.pushNotifications ?? false })

        requestDeviceTokenIfPossible()
        reportOpenedNotifications()
        // Covers the consent too: the registration right after it is accepted.
        if state != previous { requestInstallationSync() }
    }

    func scenePhaseDidChange(_ phase: ScenePhase) {
        guard isStarted, phase == .active else { return }
        // The user may have changed the permission in iOS Settings; a change
        // registers again on its own.
        Task { await notifications.refreshPermission() }
        // A token FCM could not make earlier (offline at launch) is tried again.
        if hasAPNsToken, currentPushToken == nil { fetchPushToken() }
        requestInstallationSync(routine: true)
    }

    /// The installation to name on the logout request, so the server detaches
    /// it from the account at once. Nil when the server has no installations
    /// or before the consent, when no id is ever made.
    var installationIDForSignOut: String? {
        guard account.hasAcceptedLegal, account.features?.installations == true else { return nil }
        return identity.id
    }

    // MARK: Push

    func didRegisterForRemoteNotifications(deviceToken: Data) {
        FirebasePush.setAPNSToken(deviceToken)
        hasAPNsToken = true
        fetchPushToken()
    }

    func didFailToRegisterForRemoteNotifications(error: Error) {
        ReplyLog.event("push: APNs registration failed: \(error)")
    }

    /// A notification was tapped (or, in DEBUG with auto-open, arrived).
    func notificationOpened(_ payload: NotificationPayload) {
        router.open(payload.destination)
        if let deliveryID = payload.deliveryID, !openedDeliveries.contains(deliveryID) {
            // A handful at most; a tap arriving before the account is loaded waits for it.
            openedDeliveries = Array((openedDeliveries + [deliveryID]).suffix(10))
            reportOpenedNotifications()
        }
        onNotificationOpened?()
    }

    /// Asks APNs for a token: once per launch, and only after the legal
    /// consent, when this build can receive pushes, the server can send them
    /// and iOS will show them.
    private func requestDeviceTokenIfPossible() {
        guard isStarted, account.hasAcceptedLegal, notifications.buildSupportsPush,
              account.features?.pushNotifications == true,
              notifications.permission.allowsDelivery,
              !hasRequestedDeviceToken else { return }
        hasRequestedDeviceToken = true
        UIApplication.shared.registerForRemoteNotifications()
    }

    /// Asks FCM for its token, which needs the APNs token first. `replacing`
    /// drops the current one and makes a new one.
    private func fetchPushToken(replacing: Bool = false) {
        if replacing { replacesTokenOnNextFetch = true }
        guard hasAPNsToken, tokenTask == nil else { return }
        let replace = replacesTokenOnNextFetch
        replacesTokenOnNextFetch = false
        tokenTask = Task { [weak self] in
            do {
                let token = replace ? try await FirebasePush.replaceToken() : try await FirebasePush.token()
                self?.didReceivePushToken(token)
            } catch {
                ReplyLog.event("push: no FCM token: \(error)")
            }
            self?.tokenTask = nil
        }
    }

    private func didReceivePushToken(_ token: String) {
        // A token only exists after the consent; the id is not made before it.
        guard account.hasAcceptedLegal, let installationID = identity.id else { return }
        guard store.pushToken(for: installationID) != token else { return }
        // Kept with the installation it was handed to (see NotificationSettingsStore).
        store.setPushToken(token, installationID: installationID)
        requestInstallationSync()
    }

    private var currentPushToken: String? {
        guard account.hasAcceptedLegal, let installationID = identity.id else { return nil }
        return store.pushToken(for: installationID)
    }

    // MARK: Opened notifications

    /// Best effort: the server marks the delivery opened. Sent once the
    /// consent is given and the server offers installations; a failure is
    /// not retried.
    private func reportOpenedNotifications() {
        guard isStarted, !openedDeliveries.isEmpty, account.hasAcceptedLegal,
              account.features?.installations == true, let installationID = identity.id else { return }
        let deliveries = openedDeliveries
        openedDeliveries = []
        let reporter = openedReporter
        Task {
            for deliveryID in deliveries {
                do {
                    try await reporter.reportOpened(installationID: installationID, deliveryID: deliveryID)
                } catch {
                    ReplyLog.event("push: opened report failed: \(error)")
                }
            }
        }
    }

    // MARK: Installation

    /// Registers the installation now, or right after the pass in progress.
    /// `routine` checks (every foreground) wait out a retry pause.
    func requestInstallationSync(routine: Bool = false) {
        guard isStarted else { return }
        if routine, retryTask != nil { return }
        if syncTask != nil {
            syncAgain = true
            return
        }
        retryTask?.cancel()
        retryTask = nil
        syncTask = Task { [weak self] in await self?.runSyncPasses() }
    }

    private func runSyncPasses() async {
        defer { syncTask = nil }
        repeat {
            syncAgain = false
            guard let snapshot = installationSnapshot() else { return }
            let outcome = await registrar.sync(snapshot)
            switch outcome {
            case .registered(let response):
                syncFailures = 0
                handle(response, for: snapshot)
            case .unchanged:
                syncFailures = 0
            case .failed(let failure):
                ReplyLog.event("push: installation registration failed: \(failure)")
                if InstallationRegistrar.isRetryable(failure) {
                    scheduleSyncRetry(serverDelay: InstallationRegistrar.retryAfter(failure))
                }
                return
            }
        } while syncAgain
    }

    /// Everything the server should know right now, or nil while the app may
    /// not or cannot register yet (see `InstallationSnapshot.make`).
    private func installationSnapshot() -> InstallationSnapshot? {
        // The consent first: the installation id is not read (or made) before it.
        guard account.hasAcceptedLegal else { return nil }
        let installationID = identity.id
        return InstallationSnapshot.make(InstallationSnapshot.Inputs(
            isBootstrapComplete: account.isBootstrapComplete,
            hasAcceptedLegal: account.hasAcceptedLegal,
            serverOffersInstallations: account.features.map { $0.installations ?? false },
            hasReadPermission: notifications.hasReadPermission,
            installationID: installationID,
            permission: notifications.permission,
            notificationsEnabled: notifications.isEnabledInApp,
            pushToken: installationID.flatMap { store.pushToken(for: $0) },
            locale: appLanguage(),
            // The session in the Keychain decides, not the screen: it is what
            // the server will see.
            hasSession: AccountCredentials.isSignedIn,
            accountID: account.userID
        ))
    }

    private func handle(_ response: InstallationResponse, for snapshot: InstallationSnapshot) {
        if let preferences = response.preferences {
            notifications.adoptPreferences(preferences)
        }
        // FCM told the server this token is dead: make a new one, once a launch.
        if response.pushStatus == "invalid", snapshot.payload.push != nil, !hasReplacedInvalidToken {
            hasReplacedInvalidToken = true
            fetchPushToken(replacing: true)
        }
    }

    private func scheduleSyncRetry(serverDelay: Int?) {
        syncFailures += 1
        let backoff = min(30 * pow(2, Double(syncFailures - 1)), 30 * 60)
        let delay = max(backoff, TimeInterval(serverDelay ?? 0))
        retryTask?.cancel()
        retryTask = Task { [weak self] in
            try? await Task.sleep(for: .seconds(delay))
            guard !Task.isCancelled, let self else { return }
            self.retryTask = nil
            self.requestInstallationSync()
        }
    }

    // MARK: DEBUG

    #if DEBUG
    private func applyDebugLaunchOptions() {
        if let link = DebugLaunchOptions.openLink {
            router.open(DeepLink.destination(for: link))
        }
        if DebugLaunchOptions.requestsProvisionalPush {
            // Provisional authorization needs no alert, which a Simulator
            // session without a person could not answer.
            UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .provisional]) { _, _ in
                Task { @MainActor in await AppServices.shared.notifications.refreshPermission() }
            }
        }
    }
    #endif
}

/// Receives notifications while the app is open and taps on them.
///
/// Called by iOS on a queue of its choosing, hence no actor here; everything
/// the app does with a notification happens on the main actor.
final class NotificationCenterDelegate: NSObject, UNUserNotificationCenterDelegate {

    /// In the foreground: shown as a banner and in the list, with its sound.
    /// No badge, and never a second, local copy of the same notification.
    func userNotificationCenter(_ center: UNUserNotificationCenter,
                                willPresent notification: UNNotification,
                                withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void) {
        #if DEBUG
        if DebugLaunchOptions.autoOpensPushes {
            let payload = NotificationPayload(userInfo: notification.request.content.userInfo)
            Task { @MainActor in AppServices.shared.notificationOpened(payload) }
        }
        #endif
        completionHandler([.banner, .list, .sound])
    }

    /// A tap: from a cold start, from the background or on screen.
    func userNotificationCenter(_ center: UNUserNotificationCenter,
                                didReceive response: UNNotificationResponse,
                                withCompletionHandler completionHandler: @escaping () -> Void) {
        guard response.actionIdentifier == UNNotificationDefaultActionIdentifier else {
            completionHandler()
            return
        }
        let payload = NotificationPayload(userInfo: response.notification.request.content.userInfo)
        Task { @MainActor in
            AppServices.shared.notificationOpened(payload)
            completionHandler()
        }
    }
}

/// The app delegate SwiftUI keeps for UIKit's push callbacks.
final class AppDelegate: NSObject, UIApplicationDelegate {

    func application(_ application: UIApplication,
                     didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil) -> Bool {
        AppServices.shared.start()
        return true
    }

    func application(_ application: UIApplication,
                     didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        AppServices.shared.didRegisterForRemoteNotifications(deviceToken: deviceToken)
    }

    func application(_ application: UIApplication,
                     didFailToRegisterForRemoteNotificationsWithError error: Error) {
        AppServices.shared.didFailToRegisterForRemoteNotifications(error: error)
    }
}
