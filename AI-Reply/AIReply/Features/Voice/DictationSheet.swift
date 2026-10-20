import SwiftUI

/// Dictation, presented as a sheet wherever text can be typed.
///
/// Wraps the existing `VoiceReplyViewModel` so profile text, template
/// instructions and the try-it message all dictate through one implementation
/// and one set of permission prompts.
///
/// Speech recognition runs on Apple's own frameworks with the locale that
/// matches the app language. Where a locale is not available on the device it
/// says so and the user types instead, rather than failing silently.
struct DictationSheet: View {

    let language: AppLanguage
    let onAccept: (String) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var model = VoiceReplyViewModel()
    @FocusState private var isEditing: Bool

    var body: some View {
        NavigationStack {
            VStack(spacing: DS.Spacing.l) {
                Spacer(minLength: 0)

                Text(model.statusMessage)
                    .font(.callout)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
                    .padding(.horizontal, DS.Spacing.l)

                // EDITABLE, not a read-only transcript. Speech recognition
                // mishears names, numbers and Kazakh endings, and the fix
                // belongs where the user is already looking rather than three
                // screens later. Recording is stopped as soon as they start
                // typing, so the recognizer cannot overwrite their correction.
                ZStack(alignment: .topLeading) {
                    if model.transcript.isEmpty && !model.isRecording {
                        Text("voice.placeholder")
                            .font(.body)
                            .foregroundStyle(.tertiary)
                            .padding(DS.Spacing.m)
                            .allowsHitTesting(false)
                    }
                    TextEditor(text: $model.transcript)
                        .font(.body)
                        .scrollContentBackground(.hidden)
                        .padding(DS.Spacing.xs)
                        .focused($isEditing)
                        .onChange(of: isEditing) { _, editing in
                            if editing { model.stopRecording() }
                        }
                }
                .frame(height: 220)
                .background(
                    RoundedRectangle(cornerRadius: DS.Radius.large, style: .continuous)
                        .fill(Color.dsSurface)
                )
                .padding(.horizontal, DS.Spacing.l)

                Button {
                    isEditing = false
                    if model.isRecording { model.stopRecording() } else { model.startRecording() }
                } label: {
                    Image(systemName: model.isRecording ? "stop.fill" : "mic.fill")
                        .font(.system(size: 28, weight: .semibold))
                        .foregroundStyle(.white)
                        .frame(width: 76, height: 76)
                        // Blue in both states: the glyph says whether it records.
                        .background(Circle().fill(Color.accentColor))
                }
                .accessibilityLabel(model.isRecording ? "voice.stop" : "voice.tapToSpeak")

                if model.isRecording {
                    Text(model.recordingLabel)
                        .font(.footnote.monospacedDigit())
                        .foregroundStyle(.secondary)
                }

                Spacer(minLength: 0)
            }
            .frame(maxWidth: .infinity)
            .background(Color.dsBackground)
            .navigationTitle("voice.title")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("common.cancel") {
                        model.stopRecording()
                        dismiss()
                    }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("common.save") {
                        model.stopRecording()
                        onAccept(model.transcript)
                        dismiss()
                    }
                    .disabled(model.transcript.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                }
            }
            .onAppear { model.updateLanguage(language) }
            .onDisappear { model.stopRecording() }
        }
    }
}
