import Foundation
import Observation
import SwiftUI

/// A screen pushed onto Home's navigation stack.
enum AppRoute: Hashable {
    case subscription
    case settings
    /// Settings, scrolled to its notifications section.
    case notificationSettings
    case templates
    case profile
    case workingHours
    case keyboardSetup
    case compose

    @MainActor @ViewBuilder
    var destination: some View {
        switch self {
        case .subscription:         SubscriptionView()
        case .settings:             SettingsView()
        case .notificationSettings: SettingsView(focus: .notifications)
        case .templates:            TemplateListView()
        case .profile:              ProfileEditorView()
        case .workingHours:         WorkingHoursView()
        case .keyboardSetup:        KeyboardSetupView()
        case .compose:              ComposeView()
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

    nonisolated static let pendingLifetime: TimeInterval = 15 * 60

    /// Home's navigation stack.
    var path: [AppRoute] = []
    /// A page on ai-reply.kz, shown in the in-app browser.
    var webPage: WebPage?

    private(set) var pending: DeepLinkDestination?
    @ObservationIgnored private var pendingSince: Date?
    @ObservationIgnored private var isMainInterfaceVisible = false
    @ObservationIgnored private let now: () -> Date

    init(now: @escaping () -> Date = Date.init) {
        self.now = now
    }

    /// Opens a destination now, or as soon as the gates in front of Home are passed.
    func open(_ destination: DeepLinkDestination) {
        guard destination != .none else { return }
        if isMainInterfaceVisible {
            apply(destination)
        } else {
            pending = destination
            pendingSince = now()
        }
    }

    /// Home's navigation stack is on screen: every gate has been passed.
    func mainInterfaceDidAppear() {
        isMainInterfaceVisible = true
        guard let destination = pending, let since = pendingSince else { return }
        pending = nil
        pendingSince = nil
        guard now().timeIntervalSince(since) <= Self.pendingLifetime else { return }
        apply(destination)
    }

    /// A gate is back in front of Home (signed out, say).
    func mainInterfaceDidDisappear() {
        isMainInterfaceVisible = false
    }

    private func apply(_ destination: DeepLinkDestination) {
        switch destination {
        case .none:
            return
        case .web(let url):
            webPage = WebPage(url: url)
        case .screen(let screen):
            webPage = nil
            path = Self.path(for: screen)
        }
    }

    nonisolated static func path(for screen: AppScreen) -> [AppRoute] {
        switch screen {
        case .home:          return []
        case .subscription:  return [.subscription]
        case .settings:      return [.settings]
        case .notifications: return [.notificationSettings]
        case .templates:     return [.templates]
        case .profile:       return [.profile]
        case .keyboard:      return [.keyboardSetup]
        case .compose:       return [.compose]
        }
    }
}
