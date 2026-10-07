import SwiftUI

struct HomeView: View {

    @Environment(AppSettings.self) private var settings
    @Environment(ReplyConfigurationModel.self) private var model
    @Environment(AccountModel.self) private var account
    @Environment(PushNotificationsModel.self) private var notifications

    /// Re-read on appearance and on every return to the foreground -
    /// typically from iOS Settings, or from another app where the keyboard
    /// just ran and reported in. Never polled here.
    @Environment(KeyboardStatusMonitor.self) private var keyboard
    @Environment(\.scenePhase) private var scenePhase

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
            keyboard.refresh()
        }
        .onChange(of: scenePhase) { _, phase in
            if phase == .active { keyboard.refresh() }
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
                    Image(systemName: keyboard.status.isEnabled ? "checkmark.circle.fill" : "keyboard")
                        .foregroundStyle(keyboard.status.isEnabled ? Color.green : Color.secondary)
                }
                .font(.body.weight(.medium))

                if keyboard.status.isEnabled {
                    Label {
                        Text(fullAccessKey).foregroundStyle(.primary)
                    } icon: {
                        Image(systemName: hasFullAccess ? "lock.open.fill" : "lock.fill")
                            .foregroundStyle(hasFullAccess ? Color.green : Color.orange)
                    }
                    .font(.subheadline)
                }

                if !keyboard.status.isEnabled {
                    KeyboardSetupSteps().padding(.top, DS.Spacing.xxs)
                }

                // ONE action, and only while something is left to do. "Open
                // iOS Settings" and "Keyboard setup" used to sit side by side
                // and both ended on the same iOS page; the step-by-step guide
                // stays in Settings ▸ Keyboard setup.
                if needsKeyboardSetup {
                    OpenKeyboardSettingsButton()
                        .padding(.top, DS.Spacing.xxs)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .dsCard()
        }
    }

    /// The keyboard has not reported in yet, or it runs without Full Access.
    /// Both are only ever cleared by the keyboard itself reporting its state,
    /// so a "done" here is something it actually observed.
    private var needsKeyboardSetup: Bool {
        !keyboard.status.isEnabled || !hasFullAccess
    }

    private var hasFullAccess: Bool { keyboard.status.fullAccess == .on }

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
        // The short version; Settings ▸ Privacy has the full one.
        DSSection(title: "home.privacy.title") {
            Text("home.privacy.body")
                .font(.footnote)
                .foregroundStyle(.secondary)
                .dsCard()
        }
    }

    /// Explicitly typed. `Text(condition ? "a" : "b")` can resolve the ternary
    /// as `String` rather than `LocalizedStringKey`, which compiles happily and
    /// then ships the raw key to the user instead of the translation.
    private var keyboardStateKey: LocalizedStringKey {
        keyboard.status.isEnabled ? "home.keyboard.ready" : "home.keyboard.notReady"
    }

    private var fullAccessKey: LocalizedStringKey {
        hasFullAccess ? "home.keyboard.fullAccessOn" : "home.keyboard.fullAccessOff"
    }
}
