import SafariServices
import SwiftUI
import UIKit

/// Home's navigation stack, driven by `AppRouter`.
///
/// It exists only once every gate - consent, sign-in, onboarding - has been
/// passed, which is exactly when a destination from a notification may be
/// applied: its appearance is what releases a pending one.
struct RootNavigationView: View {

    @Environment(AppRouter.self) private var router

    var body: some View {
        @Bindable var router = router
        NavigationStack(path: $router.path) {
            HomeView()
        }
        .onAppear { router.mainInterfaceDidAppear() }
        .onDisappear { router.mainInterfaceDidDisappear() }
        .sheet(item: $router.webPage) { page in
            SafariView(url: page.url).ignoresSafeArea()
        }
    }
}

/// A page on ai-reply.kz, in the app. Only ever given a URL `DeepLink`
/// accepted.
struct SafariView: UIViewControllerRepresentable {
    let url: URL

    func makeUIViewController(context: Context) -> SFSafariViewController {
        SFSafariViewController(url: url)
    }

    func updateUIViewController(_ controller: SFSafariViewController, context: Context) {}
}

/// The soft ask on Home: the app never shows the system prompt on its own.
///
/// Хабарламаларға рұқсат сұрау: жүйелік терезе тек «Қосу» басылғанда шығады.
struct PushPermissionCard: View {

    @Environment(PushNotificationsModel.self) private var notifications

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.s) {
            HStack(alignment: .top, spacing: DS.Spacing.s) {
                Image(systemName: "bell.badge")
                    .font(.title3)
                    .foregroundStyle(Color.accentColor)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: DS.Spacing.xxs) {
                    Text("notifications.card.title")
                        .font(.subheadline.weight(.semibold))
                    Text("notifications.card.body")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }

            HStack(spacing: DS.Spacing.s) {
                Button {
                    Task { await notifications.requestPermission() }
                } label: {
                    Text("notifications.card.enable")
                }
                .buttonStyle(.dsPrimary)
                .disabled(notifications.isRequestingPermission)
                .accessibilityIdentifier("notifications.card.enable")

                Button {
                    withAnimation { notifications.dismissCard() }
                } label: {
                    Text("notifications.card.notNow")
                }
                .buttonStyle(.dsSecondary)
                .accessibilityIdentifier("notifications.card.notNow")
            }
        }
        .dsCard()
    }
}

/// Settings ▸ Notifications: what iOS allows, the app's own switch and,
/// for an account, the categories.
///
/// Баптаулар ▸ Хабарламалар.
struct NotificationSettingsSection: View {

    @Environment(PushNotificationsModel.self) private var notifications

    var body: some View {
        Section {
            LabeledContent {
                Text(statusKey).foregroundStyle(.secondary)
            } label: {
                Label("settings.notifications.status", systemImage: "bell")
            }
            .id(SettingsView.Focus.notifications)

            switch notifications.permission {
            case .denied:
                Button("settings.notifications.openSettings") { openNotificationSettings() }
            case .notDetermined:
                Button("settings.notifications.allow") {
                    Task { await notifications.requestPermission() }
                }
                .disabled(notifications.isRequestingPermission)
            default:
                EmptyView()
            }

            Toggle("settings.notifications.master", isOn: masterBinding)
        } header: {
            Text("settings.notifications")
        } footer: {
            Text("settings.notifications.footer")
        }
        .task { await notifications.refreshPermission() }

        if notifications.showsCategories {
            Section {
                ForEach(NotificationPreferences.categories, id: \.self) { category in
                    categoryRow(category)
                }
                if notifications.preferencesState == .failed {
                    Button("settings.notifications.retry") {
                        Task { await notifications.loadPreferences() }
                    }
                }
                if notifications.failedCategory != nil {
                    Label {
                        Text("settings.notifications.saveFailed")
                    } icon: {
                        Image(systemName: "exclamationmark.triangle")
                    }
                    .font(.footnote)
                    .foregroundStyle(Color.orange)
                }
            } header: {
                Text("settings.notifications.categories")
            } footer: {
                Text("settings.notifications.categories.footer")
            }
            .task { await notifications.loadPreferences() }
        }
    }

    @ViewBuilder
    private func categoryRow(_ category: String) -> some View {
        let preferences = notifications.displayedPreferences
        if preferences.isOptional(category) {
            Toggle(isOn: categoryBinding(category)) {
                Text(Self.titleKey(category))
            }
            .disabled(!notifications.isEnabledInApp || notifications.isSaving(category)
                      || notifications.preferencesState == .loading)
        } else {
            LabeledContent {
                Text("settings.notifications.alwaysOn").foregroundStyle(.secondary)
            } label: {
                Text(Self.titleKey(category))
            }
        }
    }

    static func titleKey(_ category: String) -> LocalizedStringKey {
        switch category {
        case "account":      return "settings.notifications.category.account"
        case "subscription": return "settings.notifications.category.subscription"
        case "security":     return "settings.notifications.category.security"
        case "system":       return "settings.notifications.category.system"
        case "marketing":    return "settings.notifications.category.marketing"
        default:             return LocalizedStringKey(category)
        }
    }

    /// Explicitly typed, so the ternary cannot turn into a plain `String`
    /// and put the raw key on screen.
    private var statusKey: LocalizedStringKey {
        switch notifications.permission {
        case .authorized:    return "settings.notifications.status.allowed"
        case .denied:        return "settings.notifications.status.denied"
        case .notDetermined: return "settings.notifications.status.notDetermined"
        case .provisional:   return "settings.notifications.status.provisional"
        case .ephemeral:     return "settings.notifications.status.ephemeral"
        case .unknown:       return "settings.notifications.status.unknown"
        }
    }

    private var masterBinding: Binding<Bool> {
        Binding(get: { notifications.isEnabledInApp }, set: { notifications.setEnabledInApp($0) })
    }

    private func categoryBinding(_ category: String) -> Binding<Bool> {
        Binding(
            get: { notifications.displayedPreferences.isOn(category) },
            set: { enabled in Task { await notifications.setCategory(category, enabled: enabled) } }
        )
    }

    /// This app's page in iOS Settings, notifications included.
    private func openNotificationSettings() {
        guard let url = URL(string: UIApplication.openNotificationSettingsURLString) else { return }
        UIApplication.shared.open(url)
    }
}

/// Settings ▸ Diagnostics: the one switch for app events.
struct DiagnosticsSettingsSection: View {

    @Environment(PushNotificationsModel.self) private var notifications

    var body: some View {
        Section {
            Toggle("settings.diagnostics.share", isOn: Binding(
                get: { notifications.sharesDiagnostics },
                set: { notifications.setSharesDiagnostics($0) }
            ))
        } header: {
            Text("settings.diagnostics")
        } footer: {
            Text("settings.diagnostics.footer")
        }
    }
}
