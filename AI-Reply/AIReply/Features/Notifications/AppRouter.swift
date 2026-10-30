import Foundation
import Observation
import SwiftUI
import UIKit

/// A screen pushed onto Home's navigation stack.
enum AppRoute: Hashable {
    case subscription
    case settings
    /// Settings, scrolled to its notifications section.
    case notificationSettings
    case templates
    case profile
    case workingHours
    /// «Настройка клавиатуры и разрешений».
    case permissions
    case pasteHelp
    case compose
    /// Compatibility route for older links; opens account access only.
    case purchase
    /// «Чат с оператором».
    case support

    @MainActor @ViewBuilder
    var destination: some View {
        switch self {
        case .subscription:         SubscriptionView()
        case .settings:             SettingsView()
        case .notificationSettings: SettingsView(focus: .notifications)
        case .templates:            TemplateListView()
        case .profile:              ProfileEditorView()
        case .workingHours:         WorkingHoursView()
        case .permissions:          PermissionsView()
        case .pasteHelp:            PasteHelpView()
        case .compose:              ComposeView()
        case .purchase:             SubscriptionView()
        case .support:              SupportChatView()
        }
    }
}

/// Navigation that can be driven from outside the view tree.
///
/// Навигация: хабарламадан келген сілтеме осы жерден өтеді.
///
/// A notification can be tapped while the app is closed, in the background or
/// on screen - and while it is showing the legal consent, sign-in or
/// onboarding. The router never goes around those: a destination that arrives
/// while one of them is up waits here and is applied the moment Home appears.
/// One that waited longer than `pendingLifetime` is dropped, so signing in
/// much later does not jump to a screen the user has forgotten asking for.
@MainActor
@Observable
final class AppRouter {

    struct WebPage: Identifiable, Equatable {
        let url: URL
        var id: String { url.absoluteString }
    }

    /// The screens behind a server feature that this server offers now.
    /// A link to one it does not offer opens the nearest screen there is.
    struct Availability: Equatable, Sendable {
        /// `features.iap_ios`.
        var purchases = false
        /// `features.support_chat`.
        var supportChat = false
    }

    nonisolated static let pendingLifetime: TimeInterval = 15 * 60

    /// Home's navigation stack.
    var path: [AppRoute] = []
    /// A page on ai-reply.kz, shown in the in-app browser.
    var webPage: WebPage?
    /// The guide («Как пользоваться AI Reply») over Home: from Settings or
    /// from `aireply://onboarding`.
    var isShowingTutorial = false
    /// Set from the account's features (see `AppServices`).
    var availability = Availability()
    private(set) var isOpeningWebOffer = false
    var webOfferOpeningFailed = false

    private(set) var pending: DeepLinkDestination?
    @ObservationIgnored private var pendingSince: Date?
    private var isMainInterfaceVisible = false
    private var isSceneActive = false
    @ObservationIgnored private let now: () -> Date
    @ObservationIgnored private let openExternal: @MainActor (URL) async -> Bool
    @ObservationIgnored private let handoffProvider: WebPaymentHandoffProviding
    @ObservationIgnored private let backendBaseURL: () -> URL?
    @ObservationIgnored private let locale: () -> String
    private var accountID: String?
    private var accountIsReady = false
    @ObservationIgnored private var endingSessionUserID: String?
    @ObservationIgnored private var handoffTask: Task<Void, Never>?
    @ObservationIgnored private var handoffID: UUID?
    @ObservationIgnored private var retryOffer: (destination: DeepLinkDestination, accountID: String)?

    init(now: @escaping () -> Date = Date.init,
         openExternal: (@MainActor (URL) async -> Bool)? = nil,
         handoffProvider: WebPaymentHandoffProviding = BackendWebPaymentHandoffProvider(),
         backendBaseURL: @escaping () -> URL? = { AIConfiguration.shared.backendBaseURL },
         locale: @escaping () -> String = { SharedSettings.shared.effectiveAppLanguage.rawValue }) {
        self.now = now
        self.openExternal = openExternal ?? { url in
            await withCheckedContinuation { continuation in
                UIApplication.shared.open(url, options: [:]) { continuation.resume(returning: $0) }
            }
        }
        self.handoffProvider = handoffProvider
        self.backendBaseURL = backendBaseURL
        self.locale = locale
    }

    /// Called independently of view appearance: a response for an earlier
    /// account must never open a website session after a sign-out or switch.
    func accountDidChange(userID: String?, isReady: Bool) {
        // /me or config can publish the old visible phase while logout is
        // still awaiting the server. They cannot reactivate that session.
        if let endingSessionUserID, userID == endingSessionUserID, isReady { return }
        if userID != endingSessionUserID { endingSessionUserID = nil }
        guard userID != accountID || isReady != accountIsReady else { return }
        accountID = userID
        accountIsReady = isReady
        cancelWebOffer()
        applyPendingIfReady()
    }

    func accountWillSignOut() {
        endingSessionUserID = accountID
        accountIsReady = false
        cancelWebOffer()
        if let pending {
            switch pending {
            case .externalWeb, .websiteAccount:
                self.pending = nil
                pendingSince = nil
            default: break
            }
        }
    }

    /// Opens a destination now, or as soon as the gates in front of Home are passed.
    func open(_ destination: DeepLinkDestination) {
        guard DeepLink.isAllowed(destination) else { return }
        cancelWebOffer()
        pending = nil
        pendingSince = nil
        if isMainInterfaceVisible && canApply(destination) {
            apply(destination)
        } else {
            pending = destination
            pendingSince = now()
        }
    }

    /// The visible account card is independent of push permission and
    /// marketing consent. It still uses the same account-bound website login.
    var canOpenWebsiteAccount: Bool {
        IOSProductPolicy.showsWebsiteAccountLink && isMainInterfaceVisible && isSceneActive
            && accountIsReady && accountID != nil && !isOpeningWebOffer
    }

    func openWebsiteAccount() {
        guard canOpenWebsiteAccount else { return }
        open(.websiteAccount)
    }

    /// Inactive/background is not necessarily a view disappearance. A link
    /// tapped during activation waits; an in-flight response is discarded.
    func scenePhaseDidChange(_ phase: ScenePhase) {
        isSceneActive = phase == .active
        if isSceneActive { applyPendingIfReady() }
        else { cancelWebOffer() }
    }

    /// Home's navigation stack is on screen: every gate has been passed.
    func mainInterfaceDidAppear() {
        isMainInterfaceVisible = true
        applyPendingIfReady()
    }

    private func applyPendingIfReady() {
        guard isMainInterfaceVisible else { return }
        guard let destination = pending, let since = pendingSince else { return }
        guard now().timeIntervalSince(since) <= Self.pendingLifetime else {
            pending = nil
            pendingSince = nil
            return
        }
        guard canApply(destination) else { return }
        pending = nil
        pendingSince = nil
        apply(destination)
    }

    private func canApply(_ destination: DeepLinkDestination) -> Bool {
        switch destination {
        case .externalWeb, .websiteAccount: return isSceneActive && accountIsReady && accountID != nil
        default: return true
        }
    }

    /// A gate is back in front of Home: signed out, onboarding restarted.
    /// Home starts from its root next time, whoever signs in; a destination
    /// still waiting behind the gate stays.
    func mainInterfaceDidDisappear() {
        isMainInterfaceVisible = false
        cancelWebOffer()
        path = []
        webPage = nil
        isShowingTutorial = false
    }

    /// Settings ▸ «Как пользоваться AI Reply».
    func showTutorial() {
        isShowingTutorial = true
    }

    /// The first-run guide was just finished: a link to the guide that
    /// waited behind it has been answered by the guide itself.
    func firstRunOnboardingDidFinish() {
        guard pending == .screen(.onboarding) else { return }
        pending = nil
        pendingSince = nil
    }

    private func apply(_ destination: DeepLinkDestination) {
        guard DeepLink.isAllowed(destination) else { return }
        switch destination {
        case .none:
            return
        case .web(let url):
            webPage = WebPage(url: url)
        case .externalWeb, .websiteAccount:
            webPage = nil
            beginWebOffer(destination)
        case .screen(let screen):
            webPage = nil
            path = Self.path(for: screen, availability: availability)
            if Self.presentsTutorial(screen) { isShowingTutorial = true }
        }
    }

    private func cancelWebOffer() {
        handoffTask?.cancel()
        handoffTask = nil
        handoffID = nil
        isOpeningWebOffer = false
        webOfferOpeningFailed = false
        retryOffer = nil
    }

    func dismissWebOfferFailure() {
        webOfferOpeningFailed = false
        retryOffer = nil
    }

    func retryWebOffer() {
        guard let retryOffer, retryOffer.accountID == accountID,
              isSceneActive, isMainInterfaceVisible, accountIsReady else {
            dismissWebOfferFailure()
            return
        }
        let destination = retryOffer.destination
        cancelWebOffer()
        beginWebOffer(destination)
    }

    private func beginWebOffer(_ destination: DeepLinkDestination) {
        guard isSceneActive, isMainInterfaceVisible, accountIsReady, let userID = accountID else { return }
        let requestID = UUID()
        handoffID = requestID
        isOpeningWebOffer = true
        webOfferOpeningFailed = false
        let requestLocale = locale()
        let baseURL = backendBaseURL()
        let started = now()
        handoffTask = Task { [weak self] in
            guard let self else { return }
            defer {
                if handoffID == requestID {
                    handoffTask = nil
                    handoffID = nil
                    isOpeningWebOffer = false
                }
            }
            do {
                guard let baseURL, ["ru", "kk", "en", "uz"].contains(requestLocale) else { throw APIError.invalidRequest }
                let response = try await handoffProvider.create(expectedUserID: userID, locale: requestLocale)
                guard mayOpenWebOffer(requestID: requestID, userID: userID), !Task.isCancelled else { return }
                let elapsed = now().timeIntervalSince(started)
                guard backendBaseURL() == baseURL,
                      (1...60).contains(response.expiresIn), elapsed >= 0, elapsed < Double(response.expiresIn),
                      let url = DeepLink.handoffURL(response.url, backendBaseURL: baseURL) else { throw APIError.invalidRequest }
                // No plain-link fallback: a browser may still hold another
                // person's session. Only this account-bound handoff is opened.
                let opened = await openExternal(url)
                if !opened { throw APIError.invalidRequest }
            } catch {
                guard mayOpenWebOffer(requestID: requestID, userID: userID), !Task.isCancelled else { return }
                retryOffer = (destination, userID)
                webOfferOpeningFailed = true
            }
        }
    }

    private func mayOpenWebOffer(requestID: UUID, userID: String) -> Bool {
        handoffID == requestID && accountID == userID && accountIsReady && isMainInterfaceVisible && isSceneActive
    }

    /// `aireply://onboarding` opens the guide over Home.
    nonisolated static func presentsTutorial(_ screen: AppScreen) -> Bool {
        screen == .onboarding
    }

    nonisolated static func path(for screen: AppScreen, availability: Availability = Availability()) -> [AppRoute] {
        switch screen {
        case .home:          return []
        case .subscription:  return [.subscription]
        case .settings:      return [.settings]
        case .notifications: return [.notificationSettings]
        case .templates:     return [.templates]
        case .profile:       return [.profile]
        case .keyboard:      return [.permissions]
        case .permissions:   return [.permissions]
        case .pasteHelp:     return [.pasteHelp]
        case .compose:       return [.compose]
        case .onboarding:    return []
        // A remote feature flag never turns an old link into a storefront.
        case .purchase:      return [.subscription]
        // No chat on this server: Home, as an older build does.
        case .support:       return availability.supportChat ? [.support] : []
        }
    }
}
