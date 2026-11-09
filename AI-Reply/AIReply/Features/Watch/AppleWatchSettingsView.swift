import SwiftUI

/// Settings ▸ Apple Watch: what is connected, the Auto Insert switch, how to
/// set it up, and what to do when it does not work.
struct AppleWatchSettingsView: View {

    @Environment(KeyboardStatusMonitor.self) private var keyboard
    @State private var model = WatchLinkModel()
    @State private var autoInsert = SharedSettings.shared.watchAutoInsertEnabled

    var body: some View {
        Form {
            Section("watchSettings.status") {
                row("watchSettings.pairing", pairing)
                row("watchSettings.companion", companion)
                row("watchSettings.connection", connection)
                row("watchSettings.keyboard", keyboard.keyboardSighting.isConfirmed ? "watchSettings.value.on" : "watchSettings.value.unknown")
                row("watchSettings.fullAccess", fullAccess)
                Button("watchSettings.test") {
                    Task { await model.testConnection() }
                }
                .disabled(model.test == .running)
                testResult
            }

            Section {
                Toggle("watchSettings.autoInsert", isOn: $autoInsert)
                    .onChange(of: autoInsert) { _, enabled in
                        SharedSettings.shared.setWatchAutoInsertEnabled(enabled)
                    }
            } footer: {
                Text("watchSettings.autoInsert.footer")
            }

            Section("watchSettings.features") {
                Label("watchSettings.features.voice", systemImage: "mic")
                Label("watchSettings.features.languages", systemImage: "globe")
                Label("watchSettings.features.modes", systemImage: "text.bubble")
                Label("watchSettings.features.never", systemImage: "hand.raised")
            }

            Section {
                NavigationLink("watchSettings.guide") { AppleWatchGuideView() }
                NavigationLink("watchSettings.paste") { PasteHelpView() }
            } footer: {
                Text("watchSettings.paste.footer")
            }

            Section {
                Text("watchSettings.privacy.body")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            } header: {
                Text("watchSettings.privacy")
            }

            Section("watchSettings.help") {
                ForEach(Self.troubleshooting, id: \.self) { key in
                    DisclosureGroup(LocalizedStringKey(key)) {
                        Text(LocalizedStringKey(key + ".body"))
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                }
            }
        }
        .navigationTitle("watchSettings.title")
        .navigationBarTitleDisplayMode(.inline)
        .onAppear { keyboard.refresh() }
        .task { model.start() }
    }

    private func row(_ title: LocalizedStringKey, _ value: LocalizedStringKey) -> some View {
        LabeledContent {
            Text(value)
        } label: {
            Text(title)
        }
    }

    @ViewBuilder
    private var testResult: some View {
        switch model.test {
        case .idle:
            EmptyView()
        case .running:
            Text("watchSettings.value.checking").font(.footnote).foregroundStyle(.secondary)
        case .answered(let milliseconds):
            Label("watchSettings.test.ok \(milliseconds)", systemImage: "checkmark.circle.fill")
                .font(.footnote)
                .foregroundStyle(.green)
        case .noAnswer:
            Label("watchSettings.test.failed", systemImage: "exclamationmark.triangle")
                .font(.footnote)
                .foregroundStyle(.orange)
        }
    }

    private var pairing: LocalizedStringKey {
        switch model.connection {
        case .unsupported: return "watchSettings.value.unsupported"
        case .activating:  return "watchSettings.value.checking"
        case .notPaired:   return "watchSettings.value.notPaired"
        case .appNotInstalled, .appNotOpen, .connected: return "watchSettings.value.paired"
        }
    }

    private var companion: LocalizedStringKey {
        switch model.connection {
        case .appNotInstalled:        return "watchSettings.value.notInstalled"
        case .appNotOpen, .connected: return "watchSettings.value.installed"
        case .activating:             return "watchSettings.value.checking"
        case .unsupported, .notPaired: return "watchSettings.value.unknown"
        }
    }

    private var connection: LocalizedStringKey {
        switch model.connection {
        case .connected:  return "watchSettings.value.connected"
        case .appNotOpen: return "watchSettings.value.notOpen"
        case .activating: return "watchSettings.value.checking"
        case .unsupported, .notPaired, .appNotInstalled: return "watchSettings.value.unavailable"
        }
    }

    private var fullAccess: LocalizedStringKey {
        switch keyboard.fullAccessSighting {
        case .confirmed: return "watchSettings.value.on"
        case .denied:    return "watchSettings.value.off"
        case .unknown, .outdated: return "watchSettings.value.unknown"
        }
    }

    /// Each has its title under this key and its text under `<key>.body`.
    private static let troubleshooting = [
        "watchHelp.install", "watchHelp.unavailable", "watchHelp.bluetooth", "watchHelp.keyboard",
        "watchHelp.fullAccess", "watchHelp.field", "watchHelp.internet", "watchHelp.language", "watchHelp.paste"
    ]
}

/// The setup, step by step, in the order it is done.
struct AppleWatchGuideView: View {

    private static let steps: [LocalizedStringKey] = [
        "watchGuide.1", "watchGuide.2", "watchGuide.3", "watchGuide.4", "watchGuide.5", "watchGuide.6", "watchGuide.7",
        "watchGuide.8", "watchGuide.9", "watchGuide.10", "watchGuide.11", "watchGuide.12", "watchGuide.13", "watchGuide.14"
    ]

    var body: some View {
        List {
            ForEach(Array(Self.steps.enumerated()), id: \.offset) { index, step in
                HStack(alignment: .firstTextBaseline, spacing: 10) {
                    Text(verbatim: "\(index + 1).")
                        .monospacedDigit()
                        .foregroundStyle(.secondary)
                    Text(step)
                }
                .accessibilityElement(children: .combine)
            }
        }
        .navigationTitle("watchSettings.guide")
        .navigationBarTitleDisplayMode(.inline)
    }
}
