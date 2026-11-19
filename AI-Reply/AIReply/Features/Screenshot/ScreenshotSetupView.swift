import SwiftUI

/// How to reach screenshot replies from any chat. iOS lets no app capture
/// another app's screen, so the capture is the person's own shortcut (Take
/// Screenshot → Reply to Screenshot), which they put in Control Center, on
/// Back Tap or on the Action button. Every step is theirs to take.
struct ScreenshotSetupView: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(\.openURL) private var openURL

    var body: some View {
        NavigationStack {
            List {
                Section {
                    Text("screenshot.setup.intro")
                        .font(.callout)
                        .fixedSize(horizontal: false, vertical: true)
                }

                Section {
                    if let link = ScreenshotFeature.shortcutURL {
                        Button {
                            openURL(link)
                        } label: {
                            Label("screenshot.setup.getShortcut", systemImage: "square.and.arrow.down")
                        }
                        .accessibilityIdentifier("screenshot.setup.getShortcut")
                    }
                    step(1, "screenshot.setup.step.open")
                    step(2, "screenshot.setup.step.take")
                    step(3, "screenshot.setup.step.reply")
                    step(4, "screenshot.setup.step.name")
                    Button {
                        if let url = URL(string: "shortcuts://create-shortcut") { openURL(url) }
                    } label: {
                        Label("screenshot.setup.openShortcuts", systemImage: "square.on.square")
                    }
                } header: {
                    Text("screenshot.setup.shortcut.header")
                } footer: {
                    Text("screenshot.setup.shortcut.footer")
                }

                Section {
                    step(1, "screenshot.setup.cc.open")
                    step(2, "screenshot.setup.cc.add")
                    step(3, "screenshot.setup.cc.choose")
                } header: {
                    Text("screenshot.setup.cc.header")
                } footer: {
                    Text("screenshot.setup.cc.footer")
                }

                Section {
                    Label("screenshot.setup.backTap", systemImage: "hand.tap")
                    Label("screenshot.setup.actionButton", systemImage: "button.horizontal.top.press")
                    Label("screenshot.setup.keyboard", systemImage: "keyboard")
                } header: {
                    Text("screenshot.setup.more.header")
                }

                Section {
                    Text("screenshot.setup.privacy")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                } header: {
                    Text("screenshot.setup.privacy.header")
                }
            }
            .navigationTitle("screenshot.setup.title")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("common.done") { dismiss() } }
            }
        }
    }

    private func step(_ number: Int, _ key: LocalizedStringKey) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: DS.Spacing.s) {
            Text(verbatim: "\(number)")
                .font(.footnote.weight(.bold).monospacedDigit())
                .foregroundStyle(.white)
                .frame(width: 22, height: 22)
                .background(Circle().fill(Color.accentColor))
                .accessibilityHidden(true)
            Text(key)
                .font(.callout)
                .fixedSize(horizontal: false, vertical: true)
        }
    }
}
