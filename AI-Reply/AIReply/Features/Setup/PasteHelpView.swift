import SwiftUI

/// «Разрешить вставку из других приложений»: where iOS keeps the setting
/// and what the user taps there (`aireply://paste_help`).
///
/// Басқа қолданбалардан қоюға рұқсат: баптау қайда және не басу керек.
///
/// The setting belongs to iOS and only the user can change it, so this page
/// explains and opens AI Reply's own page in Settings - it never claims to
/// allow anything. The path and the option are named as the keyboard's
/// refusal names them (`AIReplyStrings.pasteDenied`), in iOS's own words.
struct PasteHelpView: View {

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: DS.Spacing.l) {
                VStack(alignment: .leading, spacing: DS.Spacing.s) {
                    Text("pasteHelp.title")
                        .font(.title2.weight(.bold))
                        .accessibilityAddTraits(.isHeader)
                    Text("pasteHelp.body")
                        .foregroundStyle(.secondary)
                }

                VStack(alignment: .leading, spacing: DS.Spacing.m) {
                    DSStepRow(index: 1, text: "setup.connect.open", detail: "setup.connect.open.detail")
                    DSStepRow(index: 2, text: "pasteHelp.step.row")
                    DSStepRow(index: 3, text: "pasteHelp.step.allow")
                    Text("pasteHelp.path")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                .dsCard()

                VStack(alignment: .leading, spacing: DS.Spacing.s) {
                    note("person.crop.circle", "pasteHelp.onlyYou")
                    note("keyboard", "pasteHelp.manual")
                    note("questionmark.circle", "pasteHelp.noRow")
                }
                .dsCard()
            }
            .padding(DS.Spacing.l)
            .frame(maxWidth: DS.Layout.readableWidth, alignment: .leading)
            .frame(maxWidth: .infinity)
        }
        .background(Color.dsBackground)
        .safeAreaInset(edge: .bottom) {
            Button("permissions.openSettings", action: OpenKeyboardSettingsButton.openSystemSettings)
                .buttonStyle(.dsPrimary)
                .padding(.horizontal, DS.Spacing.l)
                .padding(.vertical, DS.Spacing.s)
                .frame(maxWidth: DS.Layout.readableWidth)
                .frame(maxWidth: .infinity)
                .background(Color.dsBackground)
        }
        .navigationTitle("pasteHelp.nav")
        .navigationBarTitleDisplayMode(.inline)
        .onAppear { ProductEvents.track(.pasteTutorialViewed) }
    }

    private func note(_ symbol: String, _ text: LocalizedStringKey) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: DS.Spacing.s) {
            Image(systemName: symbol)
                .foregroundStyle(.secondary)
                .frame(width: 22)
                .accessibilityHidden(true)
            Text(text).font(.subheadline)
        }
    }
}
