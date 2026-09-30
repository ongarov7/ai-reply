import SwiftUI
import UIKit

struct HomeView: View {

    @Environment(AppSettings.self) private var settings
    @Environment(ReplyConfigurationModel.self) private var model
    @Environment(AccountModel.self) private var account
    @Environment(PushNotificationsModel.self) private var notifications

    /// Read once per appearance rather than polled: the keyboard writes this
    /// flag when it runs, and it cannot change while this screen is in front.
    @State private var keyboardStatus = KeyboardStatus.current()

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: DS.Spacing.xl) {
                header
                if account.isSignedIn { usageCard }
                if notifications.showsPermissionCard {
                    PushPermissionCard()
                        .transition(.opacity)
                }
                tryItCard
                setupCard
                keyboardCard
                howItWorks
                privacy
            }
            .padding(.horizontal, DS.Spacing.l)
            .padding(.vertical, DS.Spacing.l)
            .frame(maxWidth: DS.Layout.readableWidth, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .center)
        }
        .background(Color.dsBackground)
        .navigationTitle("home.title")
        .navigationBarTitleDisplayMode(.inline)
        // Every screen Home opens is a route, so a notification can open the
        // same screens the same way (see AppRouter).
        .navigationDestination(for: AppRoute.self) { route in route.destination }
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                NavigationLink(value: AppRoute.settings) {
                    Image(systemName: "gearshape")
                }
                .accessibilityLabel("common.settings")
            }
        }
        .onAppear {
            keyboardStatus = .current()
        }
        .task {
            await account.refresh()
        }
    }

    // MARK: Sections

    private var header: some View {
        HStack(alignment: .top, spacing: DS.Spacing.m) {
            AppMarkView(size: 56)
            VStack(alignment: .leading, spacing: DS.Spacing.xxs) {
                Text("home.title").font(.title.weight(.semibold))
                Text("home.subtitle").font(.subheadline).foregroundStyle(.secondary)
            }
        }
    }

    /// What is left today, and a way to the plans. Shown only for accounts,
    /// because only the server knows the number.
    private var usageCard: some View {
        NavigationLink(value: AppRoute.subscription) {
            HStack(alignment: .center, spacing: DS.Spacing.m) {
                VStack(alignment: .leading, spacing: DS.Spacing.xxs) {
                    Text("home.usage.title").font(.subheadline.weight(.semibold))
                    Text(verbatim: planName)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                Spacer()
                VStack(alignment: .trailing, spacing: 0) {
                    Text(verbatim: "\(account.usage.remainingToday)")
                        .font(.system(size: 28, weight: .semibold, design: .rounded))
                        .foregroundStyle(account.usage.remainingToday > 0 ? Color.accentColor : Color.red)
                    Text("home.usage.left").font(.caption2).foregroundStyle(.secondary)
                }
                Image(systemName: "chevron.right").font(.footnote).foregroundStyle(.secondary)
            }
            .dsCard()
        }
        .buttonStyle(.plain)
    }

    private var planName: String {
        account.subscription?.plan.localizedName(settings.effectiveLanguage.rawValue) ?? ""
    }

    private var tryItCard: some View {
        NavigationLink(value: AppRoute.compose) {
            HStack {
                Label("home.tryIt", systemImage: "sparkles")
                    .font(.body.weight(.medium))
                    .foregroundStyle(.primary)
                Spacer()
                Image(systemName: "chevron.right").font(.footnote).foregroundStyle(.secondary)
            }
            .dsCard()
        }
        .buttonStyle(.plain)
    }

    private var setupCard: some View {
        DSSection(title: "home.profile.title") {
            VStack(spacing: 0) {
                link("home.profile.edit", "person.text.rectangle", to: .profile)
                Divider().padding(.leading, 44)
                link("home.profile.templates", "text.bubble", to: .templates)
                Divider().padding(.leading, 44)
                link("home.profile.hours", "clock", to: .workingHours)
            }
            .padding(.vertical, DS.Spacing.xxs)
            .background(
                RoundedRectangle(cornerRadius: DS.Radius.large, style: .continuous).fill(Color.dsSurface)
            )
        }
    }

    private func link(_ titleKey: LocalizedStringKey, _ symbol: String, to route: AppRoute) -> some View {
        NavigationLink(value: route) {
            HStack(spacing: DS.Spacing.s) {
                Image(systemName: symbol)
                    .frame(width: 24)
                    .foregroundStyle(Color.accentColor)
                Text(titleKey).foregroundStyle(.primary)
                Spacer()
                Image(systemName: "chevron.right").font(.footnote).foregroundStyle(.secondary)
            }
            .padding(.horizontal, DS.Spacing.m)
            .padding(.vertical, DS.Spacing.s)
        }
        .buttonStyle(.plain)
    }

    private var keyboardCard: some View {
        DSSection(title: "home.keyboard.title") {
            VStack(alignment: .leading, spacing: DS.Spacing.s) {
                Label {
                    Text(keyboardStateKey).foregroundStyle(.primary)
                } icon: {
                    Image(systemName: keyboardStatus.isConfigured ? "checkmark.circle.fill" : "keyboard")
                        .foregroundStyle(keyboardStatus.isConfigured ? Color.green : Color.secondary)
                }
                .font(.body.weight(.medium))

                if keyboardStatus.isConfigured {
                    Label {
                        Text(fullAccessKey)
                    } icon: {
                        Image(systemName: keyboardStatus.hasFullAccess ? "lock.open" : "lock")
                    }
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                }

                if !keyboardStatus.isConfigured {
                    KeyboardSetupSteps().padding(.top, DS.Spacing.xxs)
                }

                HStack(spacing: DS.Spacing.s) {
                    Button("home.keyboard.openSettings") { openSystemSettings() }
                        .buttonStyle(.dsSecondary)
                    NavigationLink(value: AppRoute.keyboardSetup) {
                        Text("settings.setup.guide")
                    }
                    .buttonStyle(.dsSecondary)
                }
                .padding(.top, DS.Spacing.xxs)
            }
            .dsCard()
        }
    }

    private var howItWorks: some View {
        DSSection(title: "home.howItWorks.title") {
            VStack(alignment: .leading, spacing: DS.Spacing.s) {
                Text("home.howItWorks.body").font(.body)
                Text("home.howItWorks.fullAccess").font(.footnote).foregroundStyle(.secondary)
            }
            .dsCard()
        }
    }

    private var privacy: some View {
        DSSection(title: "home.privacy.title") {
            Text("settings.privacy.body")
                .font(.footnote)
                .foregroundStyle(.secondary)
                .dsCard()
        }
    }

    /// Explicitly typed. `Text(condition ? "a" : "b")` can resolve the ternary
    /// as `String` rather than `LocalizedStringKey`, which compiles happily and
    /// then ships the raw key to the user instead of the translation.
    private var keyboardStateKey: LocalizedStringKey {
        keyboardStatus.isConfigured ? "home.keyboard.ready" : "home.keyboard.notReady"
    }

    private var fullAccessKey: LocalizedStringKey {
        keyboardStatus.hasFullAccess ? "home.keyboard.fullAccessOn" : "home.keyboard.fullAccessOff"
    }

    private func openSystemSettings() {
        guard let url = URL(string: UIApplication.openSettingsURLString) else { return }
        UIApplication.shared.open(url)
    }
}

/// Snapshot of what the keyboard extension last reported about itself.
struct KeyboardStatus {
    let isConfigured: Bool
    let hasFullAccess: Bool

    static func current(store: SharedSettings = .shared) -> KeyboardStatus {
        KeyboardStatus(
            isConfigured: store.isKeyboardConfigured,
            hasFullAccess: store.keyboardHasFullAccess
        )
    }
}
