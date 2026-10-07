import SwiftUI

/// Shown after dictation when the app recognised settings in what was said.
///
/// It exists so the extraction is never silent. The user hears themselves say
/// "we work from ten to six" and is then shown, in plain words, exactly what
/// the app took from it and what it will change - with the option to take none
/// of it. Whatever they decide, the sentence they actually said is kept as the
/// template's own instructions, so nothing they dictated is ever lost.
struct VoiceSuggestionSheet: View {

    let suggestion: VoiceConfigurationParser.Suggestion
    let existingHours: WorkingHours
    /// Called with the choices the user confirmed. Not called at all on Skip.
    let onApply: (VoiceConfigurationParser.Suggestion) -> Void

    @Environment(\.dismiss) private var dismiss

    @State private var applyHours = true
    @State private var selectedRules: Set<Int> = []

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Text("voice.suggestions.body")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }

                if suggestion.hasWorkingHours {
                    Section {
                        Toggle(isOn: $applyHours) {
                            VStack(alignment: .leading, spacing: 2) {
                                Text("hours.title")
                                Text(verbatim: hoursSummary)
                                    .font(.footnote)
                                    .foregroundStyle(.secondary)
                            }
                        }
                    } header: {
                        Text("voice.suggestions.hours")
                    }
                }

                if !suggestion.rules.isEmpty {
                    Section {
                        ForEach(Array(suggestion.rules.enumerated()), id: \.offset) { index, rule in
                            Button {
                                if selectedRules.contains(index) {
                                    selectedRules.remove(index)
                                } else {
                                    selectedRules.insert(index)
                                }
                            } label: {
                                HStack(alignment: .firstTextBaseline, spacing: DS.Spacing.s) {
                                    Image(systemName: selectedRules.contains(index)
                                        ? "checkmark.circle.fill" : "circle")
                                        .foregroundStyle(selectedRules.contains(index)
                                            ? Color.accentColor : .secondary)
                                    Text(verbatim: rule)
                                        .foregroundStyle(.primary)
                                        .multilineTextAlignment(.leading)
                                }
                            }
                        }
                    } header: {
                        Text("voice.suggestions.rules")
                    } footer: {
                        Text("voice.suggestions.rules.footer")
                    }
                }
            }
            .navigationTitle("voice.suggestions.title")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("voice.suggestions.skip") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("common.apply") {
                        onApply(confirmed)
                        dismiss()
                    }
                    .disabled(!hasSelection)
                }
            }
            // Rules start unticked: adding a sentence to the rules the model
            // must follow is the user's decision, not a default.
            .onAppear { applyHours = suggestion.hasWorkingHours }
        }
    }

    private var hasSelection: Bool {
        (applyHours && suggestion.hasWorkingHours) || !selectedRules.isEmpty
    }

    /// Only what the user ticked.
    private var confirmed: VoiceConfigurationParser.Suggestion {
        var result = suggestion
        if !applyHours {
            result.start = nil
            result.end = nil
            result.weekdays = nil
        }
        result.rules = suggestion.rules.enumerated()
            .filter { selectedRules.contains($0.offset) }
            .map(\.element)
        return result
    }

    private var hoursSummary: String {
        guard let start = suggestion.start, let end = suggestion.end else { return "" }
        let days = suggestion.weekdays ?? existingHours.days.filter(\.isEnabled).map(\.weekday)
        let symbols = Calendar.current.shortStandaloneWeekdaySymbols
        let names = [2, 3, 4, 5, 6, 7, 1]
            .filter { days.contains($0) }
            .map { symbols[$0 - 1] }
            .joined(separator: ", ")
        return names.isEmpty
            ? "\(start.formatted)-\(end.formatted)"
            : "\(names) · \(start.formatted)-\(end.formatted)"
    }
}

/// `sheet(item:)` needs identity, and a suggestion is a value with no natural
/// id. Conforming here rather than on the parser keeps that presentation detail
/// out of the shared logic.
extension VoiceConfigurationParser.Suggestion: Identifiable {
    public var id: String {
        "\(start?.minutes ?? -1)-\(end?.minutes ?? -1)-\(rules.count)-\(weekdays?.count ?? 0)"
    }
}
