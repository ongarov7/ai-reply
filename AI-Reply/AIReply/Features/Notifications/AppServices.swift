import SwiftUI
import UIKit
import UserNotifications

/// Wires push notifications, the installation record, app events and deep
/// links into the app's life.
///
/// Push, орнату, оқиғалар және сілтемелер — қосымшаның өмір циклына осы жерде қосылады.
///
/// One instance for the process, created by the app delegate at launch. The
/// screens never talk to it directly: they see `PushNotificationsModel` and
/// `AppRouter` through the environment. What happens when:
///
/// - Launch: the installation id is read (or made), `APIClient` starts adding
///   the installation and session ids to the app's requests, the notification
///   delegate is set so a tap that launched the app is not lost.
/// - After the account bootstrap, and on every sign-in, sign-out, token,
///   permission, switch or language change: the installation is registered if
///   the server offers it (`features.installations`) and the payload differs
///   from the last accepted one, else at most once a day.
/// - Foreground: `app_opened`, permission re-read, events sent every minute.
/// - Background: `app_backgrounded` and one last send in a short background task.
///
/// A server without these features (the production server until it is
/// upgraded) sees none of it: no registration, no events, and the app works
/// exactly as before.
@MainActor
final class AppServices {

    static let shared = AppServices()

    /// What `AccountModel` knows, mirrored in by the app's root view.
    struct AccountState: Equatable {
        var isBootstrapComplete = false
        var isSignedIn = false
        var userID: String?
        var features: AccountAPI.Features?
    }

    let router = AppRouter()
    let notifications: PushNotificationsModel
    let events: EventReporter

    /// The app's interface language, from `AppSettings`.
    var appLanguage: () -> String = { SharedSettings.shared.effectiveAppLanguage.rawValue }

    private let store: NotificationSettingsStore
    private let identity: InstallationIdentity
    private let identityHolder: ClientIdentityHolder
    private let sessionTracker: SessionTracker
    private let registrar: InstallationRegistrar
    private let notificationDelegate = NotificationCenterDelegate()

    private var isStarted = false
    private var account = AccountState()

    private var syncTask: Task<Void, Never>?
    private var syncAgain = false
    private var retryTask: Task<Void, Never>?
    private var syncFailures = 0

    private var flushTask: Task<Void, Never>?
    private var hasRequestedDeviceToken = false
    private var hasRetriedInvalidToken = false
    private var reportedTokenFailures: Set<String> = []
    private var lastAPIErrorReport: [String: Date] = [:]

    private init() {
        let store = NotificationSettingsStore()
        let identity = InstallationIdentity()
        let identityHolder = ClientIdentityHolder()
        let sessionTracker = SessionTracker()
        self.store = store
        self.identity = identity
        self.identityHolder = identityHolder
        self.sessionTracker = sessionTracker
        self.registrar = InstallationRegistrar(
            transport: BackendInstallationTransport(), tokens: AccountSession.shared, store: store)
        self.events = EventReporter(
            transport: BackendEventTransport(),
            userAllows: store.sharesDiagnostics,
            installationID: { identityHolder.current.installationID },
            sessionID: { sessionTracker.sessionID },
            // Attached when a fresh one is at hand; a refresh just for an
            // event would be a waste, and the endpoint accepts none.
            accessToken: { AccountCredentials.isAccessTokenFresh ? AccountCredentials.accessToken : nil }
        )
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
        notifications.onEvent = { [weak self] event in self?.events.record(event) }
        notifications.onShareDiagnosticsChange = { [weak self] allows in self?.events.setUserAllows(allows) }
    }

    /// Unit tests run inside the app; they build their own instances and must
    /// not find this one registering, sending or setting delegates.
    private static var isRunningUnitTests: Bool {
        ProcessInfo.processInfo.environment["XCTestConfigurationFilePath"] != nil
    }

    // MARK: Launch

    /// `application(_:didFinishLaunchingWithOptions:)`.
    func start() {
        guard !isStarted, !Self.isRunningUnitTests else { return }
        isStarted = true

        refreshIdentity()
        let holder = identityHolder
        APIClientHooks.shared.install(
            identity: { holder.current },
            transportFailures: { failure in
                Task { @MainActor in AppServices.shared.requestFailedWithoutResponse(failure) }
            }
        )
        // Set here, not later: a tap that launched the app is delivered as
        // soon as launching finishes.
        UNUserNotificationCenter.current().delegate = notificationDelegate

        #if DEBUG
        applyDebugLaunchOptions()
        #endif
        Task { await notifications.refreshPermission() }
    }

    /// What `APIClient` adds to the app's requests. The session id changes
    /// after half an hour away; the installation id is read again here in case
    /// the Keychain was still locked when the app launched.
    private func refreshIdentity() {
        identityHolder.update(installationID: identity.id, sessionID: sessionTracker.sessionID)
    }

    // MARK: Inputs from the app

    func accountDidChange(_ state: AccountState) {
        let previous = account
        account = state
        notifications.setSignedIn(state.isSignedIn)
        notifications.setServerDeliversPush(state.features.map { $0.pushNotifications ?? false })

        // Unknown until the server answered; a missing key is a "no".
        let telemetry = state.features.map { $0.telemetry ?? false }
        events.setServerSupport(telemetry)
        if isStarted, telemetry == true, previous.features?.telemetry != true {
            Task { await events.flush() }
        }

        requestDeviceTokenIfPossible()
        if state != previous { requestInstallationSync() }
    }

    func accountActivity(_ activity: AccountActivity) {
        switch activity {
        case .signedOut:
            events.record(.logout)
        case let .providerSignInFailed(method, errorCode):
            events.record(.loginFailed(method: method, errorCode: errorCode))
        }
    }

    func scenePhaseDidChange(_ phase: ScenePhase) {
        guard isStarted else { return }
        switch phase {
        case .active:
            if case let .opened(coldStart, _)? = sessionTracker.didBecomeActive() {
                events.record(.appOpened(coldStart: coldStart))
            }
            refreshIdentity()
            // The user may have changed the permission in iOS Settings; a
            // change registers again on its own.
            Task { await notifications.refreshPermission() }
            requestInstallationSync(routine: true)
            startSendingEvents()
        case .background:
            if case let .backgrounded(seconds)? = sessionTracker.didEnterBackground() {
                events.record(.appBackgrounded(foregroundSeconds: seconds))
            }
            stopSendingEvents()
            sendEventsInBackground()
        default:
            break
        }
    }

    // MARK: Push

    func didRegisterForRemoteNotifications(deviceToken: Data) {
        store.setDeviceToken(PushToken.hexString(deviceToken))
        requestInstallationSync()
    }

    func didFailToRegisterForRemoteNotifications(error: Error) {
        events.record(.pushTokenRegistrationFailed(errorCode: "apns_\((error as NSError).code)", requestID: nil))
    }

    /// A notification was tapped (or, in DEBUG with auto-open, arrived).
    func notificationOpened(_ payload: NotificationPayload) {
        events.record(payload.openedEvent)
        router.open(payload.destination)
    }

    /// Asks APNs for a token: once per launch, and only when this build can
    /// receive pushes, the server can send them and iOS will show them.
    private func requestDeviceTokenIfPossible(again: Bool = false) {
        guard isStarted, notifications.buildSupportsPush,
              account.features?.pushNotifications == true,
              notifications.permission.allowsDelivery,
              again || !hasRequestedDeviceToken else { return }
        hasRequestedDeviceToken = true
        UIApplication.shared.registerForRemoteNotifications()
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
            handle(outcome, for: snapshot)
            if case .failed(let failure) = outcome {
                if InstallationRegistrar.isRetryable(failure) { scheduleSyncRetry(serverDelay: failure.retryAfter) }
                return
            }
            syncFailures = 0
        } while syncAgain
    }

    /// Everything the server should know right now, or nil while something
    /// it depends on is not known yet.
    private func installationSnapshot() -> InstallationSnapshot? {
        guard account.isBootstrapComplete,
              account.features?.installations == true,
              notifications.hasReadPermission,
              let installationID = identity.id else { return nil }
        let context = ClientContext(installationID: installationID, locale: appLanguage())
        let push = store.deviceToken.map {
            InstallationPayload.Push(provider: "apns", token: $0, environment: APNsEnvironment.current.rawValue)
        }
        // The session in the Keychain decides, not the screen: it is what the
        // server will see.
        let isSignedIn = AccountCredentials.isSignedIn
        return InstallationSnapshot(
            payload: context.installationPayload(permission: notifications.permission,
                                                 notificationsEnabled: notifications.isEnabledInApp,
                                                 push: push),
            isSignedIn: isSignedIn,
            accountID: isSignedIn ? account.userID : nil
        )
    }

    private func handle(_ outcome: InstallationRegistrar.Outcome, for snapshot: InstallationSnapshot) {
        switch outcome {
        case .registered(let response):
            if let token = snapshot.payload.push?.token, token != store.registeredToken {
                events.record(store.registeredToken == nil ? .pushTokenRegistered : .pushTokenRefreshed)
                store.setRegisteredToken(token)
            }
            if let preferences = response.preferences {
                notifications.adoptPreferences(preferences)
            }
            // APNs told the server this token is dead: ask for the current one.
            if response.pushStatus == "invalid", !hasRetriedInvalidToken {
                hasRetriedInvalidToken = true
                requestDeviceTokenIfPossible(again: true)
            }
        case .failed(let failure):
            // Reported once per token and error; no response at all is an
            // `api_error`, and a 401 is the session's business.
            guard let token = snapshot.payload.push?.token, failure.status != 0, failure.status != 401 else { return }
            let code = failure.code ?? "http_\(failure.status)"
            if reportedTokenFailures.insert("\(token.prefix(12))|\(code)").inserted {
                events.record(.pushTokenRegistrationFailed(errorCode: code, requestID: failure.requestID))
            }
        case .unchanged:
            break
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

    // MARK: Events

    /// `APIClient` got no response at all: `api_error`, at most once per route
    /// and kind every five minutes, and never about the events call itself.
    private func requestFailedWithoutResponse(_ failure: APITransportFailure) {
        let route = AppEvent.route(forPath: failure.path)
        guard route != "/api/v1/events" else { return }
        let key = route + "|" + failure.kind.rawValue
        let now = Date()
        if let last = lastAPIErrorReport[key], now.timeIntervalSince(last) < 5 * 60 { return }
        lastAPIErrorReport[key] = now
        events.record(.apiError(route: route, errorCode: failure.kind.rawValue, requestID: failure.requestID))
    }

    private func startSendingEvents() {
        guard flushTask == nil else { return }
        flushTask = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(60))
                guard !Task.isCancelled, let self else { return }
                await self.events.flush()
            }
        }
    }

    private func stopSendingEvents() {
        flushTask?.cancel()
        flushTask = nil
    }

    /// One last send when the app leaves the screen, inside the few seconds
    /// iOS grants a background task.
    private func sendEventsInBackground() {
        guard events.isSending, !events.queue.isEmpty else { return }
        let task = BackgroundTask(name: "AIReply.events")
        Task {
            await events.flush()
            task.end()
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

/// Account events other parts of the app hear about.
enum AccountActivity: Equatable, Sendable {
    /// The user signed out (not a session that expired on its own).
    case signedOut
    /// Apple's or Google's own sheet failed before our server was involved.
    case providerSignInFailed(method: AppEvent.LoginMethod, errorCode: String)
}

/// A short background task that is ended exactly once.
@MainActor
private final class BackgroundTask {
    private var identifier: UIBackgroundTaskIdentifier = .invalid

    init(name: String) {
        identifier = UIApplication.shared.beginBackgroundTask(withName: name) { [weak self] in
            MainActor.assumeIsolated { self?.end() }
        }
    }

    func end() {
        guard identifier != .invalid else { return }
        UIApplication.shared.endBackgroundTask(identifier)
        identifier = .invalid
    }
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
