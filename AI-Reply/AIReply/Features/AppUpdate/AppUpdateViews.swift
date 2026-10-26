import SwiftUI

struct AppUpdateCard: View {
    @Environment(AppUpdateModel.self) private var updates
    @Environment(AppSettings.self) private var settings
    @Environment(\.openURL) private var openURL

    var body: some View {
        if let release = updates.availableRelease {
            VStack(alignment: .leading, spacing: DS.Spacing.s) {
                HStack(alignment: .top, spacing: DS.Spacing.s) {
                    Image(systemName: "arrow.down.circle")
                        .font(.title3)
                        .foregroundStyle(Color.accentColor)
                        .accessibilityHidden(true)
                    VStack(alignment: .leading, spacing: DS.Spacing.xxs) {
                        Text("updates.title").font(.subheadline.weight(.semibold))
                        Text(verbatim: String(format: settings.localized("updates.body %@"), release.version))
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
                HStack(spacing: DS.Spacing.s) {
                    Button("updates.update") {
                        openURL(release.storeURL) { accepted in
                            if !accepted { updates.appStoreDidFailToOpen() }
                        }
                    }
                    .buttonStyle(.dsPrimary)
                    .accessibilityIdentifier("updates.card.update")
                    Button("updates.later") {
                        withAnimation { updates.remindLater() }
                    }
                    .buttonStyle(.dsSecondary)
                    .accessibilityIdentifier("updates.card.later")
                }
                if updates.status == .openFailed {
                    Text("updates.openFailed").font(.footnote).foregroundStyle(.secondary)
                }
            }
            .dsCard()
            .accessibilityIdentifier("updates.card")
        }
    }
}

struct AppUpdateSettingsSection: View {
    @Environment(AppUpdateModel.self) private var updates
    @Environment(AppSettings.self) private var settings
    @Environment(\.openURL) private var openURL

    var body: some View {
        Section("updates.about") {
            LabeledContent("updates.version", value: updates.installedVersion)
            Button {
                Task { await updates.checkForUpdates(manual: true) }
            } label: {
                HStack {
                    Text(LocalizedStringKey(updates.isChecking ? "updates.checking" : "updates.check"))
                    if updates.isChecking {
                        Spacer()
                        ProgressView()
                    }
                }
            }
            .disabled(updates.isChecking)
            .accessibilityIdentifier("updates.settings.check")

            if let release = updates.availableRelease {
                Button("updates.update") {
                    openURL(release.storeURL) { accepted in
                        if !accepted { updates.appStoreDidFailToOpen() }
                    }
                }
                .accessibilityIdentifier("updates.settings.update")
                if updates.status == .available {
                    Text(verbatim: String(format: settings.localized("updates.body %@"), release.version))
                        .font(.footnote).foregroundStyle(.secondary)
                }
            }
            if let key = updates.statusKey {
                Text(LocalizedStringKey(key))
                    .font(.footnote).foregroundStyle(.secondary)
                    .accessibilityIdentifier("updates.settings.status")
            }
        }
    }
}
