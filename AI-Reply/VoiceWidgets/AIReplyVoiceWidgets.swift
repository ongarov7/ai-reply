import ActivityKit
import AppIntents
import SwiftUI
import WidgetKit

@main
struct AIReplyVoiceWidgetBundle: WidgetBundle {
    var body: some Widget {
        AIReplyVoiceLiveActivity()
        if #available(iOS 18.0, *) {
            AIReplyVoiceControl()
        }
    }
}

/// AI Reply's conversation bubbles with a microphone. Control Center and the
/// Dynamic Island draw it in SpringBoard, outside this process, which only
/// resolves a custom symbol built as a variable template (Ultralight-S,
/// Regular-S, Black-S). Build 31's single Regular-M glyph drew an empty circle.
private let brandSymbol = "AIReplyVoiceControl"

@available(iOS 18.0, *)
struct AIReplyVoiceControl: ControlWidget {
    var body: some ControlWidgetConfiguration {
        StaticControlConfiguration(kind: "kz.ai-reply.voice.start") {
            ControlWidgetButton(action: StartVoiceAssistantIntent()) {
                Label("AI Reply Voice", image: brandSymbol)
            }
        }
        .displayName("AI Reply Voice")
        .description("Start a voice instruction without opening AI Reply after first-time setup.")
    }
}

/// Status, a running timer and the essential buttons. The Lock Screen and the
/// Dynamic Island never carry the instruction, message or response: the live
/// words are shown in the AI Reply keyboard and app (see the privacy policy).
struct AIReplyVoiceLiveActivity: Widget {
    var body: some WidgetConfiguration {
        ActivityConfiguration(for: VoiceAssistantAttributes.self) { context in
            VoiceLockScreenView(state: context.state, sessionID: context.attributes.sessionID,
                                copy: VoiceActivityCopy(context.attributes.language))
                .activityBackgroundTint(Color(.secondarySystemBackground))
                .activitySystemActionForegroundColor(.blue)
                .widgetURL(URL(string: "aireply://voice"))
        } dynamicIsland: { context in
            let state = context.state
            let copy = VoiceActivityCopy(context.attributes.language)
            let sessionID = context.attributes.sessionID
            return DynamicIsland {
                DynamicIslandExpandedRegion(.leading) {
                    Label {
                        Text(verbatim: "AI Reply")
                    } icon: {
                        Image(brandSymbol)
                    }
                    .font(.headline)
                    .foregroundStyle(.blue)
                    .lineLimit(1)
                    .minimumScaleFactor(0.8)
                    .padding(.leading, 4)
                }
                DynamicIslandExpandedRegion(.trailing) {
                    if state.isRecording, let start = state.recordingStartedAt {
                        RecordingTimer(start: start, end: state.deadline)
                            .font(.headline)
                            .foregroundStyle(.secondary)
                            .frame(width: 52, alignment: .trailing)
                            .padding(.trailing, 4)
                    } else {
                        Image(systemName: state.symbol)
                            .font(.headline)
                            .foregroundStyle(state.tint)
                            .padding(.trailing, 4)
                    }
                }
                DynamicIslandExpandedRegion(.center) {
                    Text(copy.status(state.phase))
                        .font(.subheadline.weight(.medium))
                        .lineLimit(1)
                }
                DynamicIslandExpandedRegion(.bottom) {
                    VoiceIslandControls(state: state, sessionID: sessionID, copy: copy)
                        .padding(.top, 6)
                }
            } compactLeading: {
                Image(brandSymbol)
                    .foregroundStyle(.blue)
                    .accessibilityLabel(Text(verbatim: "AI Reply Voice"))
            } compactTrailing: {
                if state.isRecording, let start = state.recordingStartedAt {
                    RecordingTimer(start: start, end: state.deadline)
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(.blue)
                        .frame(width: 40)
                } else if state.isPreparing || state.isProcessing {
                    ProgressView().controlSize(.mini).tint(.blue)
                } else {
                    Image(systemName: state.symbol).foregroundStyle(state.tint)
                }
            } minimal: {
                if state.isPreparing || state.isProcessing {
                    ProgressView().controlSize(.mini).tint(.blue)
                } else if state.phase == "responseReady" {
                    Image(systemName: "checkmark").foregroundStyle(.green)
                } else {
                    Image(brandSymbol).foregroundStyle(.blue)
                }
            }
            .widgetURL(URL(string: "aireply://voice"))
            .keylineTint(.blue)
        }
    }
}

private struct VoiceLockScreenView: View {
    let state: VoiceAssistantAttributes.ContentState
    let sessionID: String
    let copy: VoiceActivityCopy

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 12) {
                ZStack {
                    Circle().fill(Color.blue.opacity(0.15))
                    if state.isPreparing || state.isProcessing {
                        ProgressView().tint(.blue)
                    } else if state.isRecording {
                        Image(brandSymbol)
                            .font(.title3)
                            .foregroundStyle(.blue)
                    } else {
                        Image(systemName: state.symbol)
                            .font(.title3.weight(.semibold))
                            .foregroundStyle(state.tint)
                    }
                }
                .frame(width: 40, height: 40)
                .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 1) {
                    Text(verbatim: "AI Reply Voice")
                        .font(.headline)
                        .lineLimit(1)
                    Text(copy.status(state.phase))
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                Spacer(minLength: 8)
                if state.isRecording, let start = state.recordingStartedAt {
                    // A timer Text asks for far more width than it draws:
                    // a fixed frame, never fixedSize, keeps the row intact.
                    RecordingTimer(start: start, end: state.deadline)
                        .font(.title3.weight(.semibold))
                        .foregroundStyle(.blue)
                        .frame(width: 56, alignment: .trailing)
                }
            }
            if state.isRecording {
                HStack(spacing: 10) {
                    Button(intent: FinishVoiceAssistantIntent(sessionID: sessionID)) {
                        Label(copy.finish, systemImage: "checkmark")
                            .font(.subheadline.weight(.semibold))
                            .lineLimit(1)
                            .frame(maxWidth: .infinity, minHeight: 36)
                    }
                    .buttonStyle(.borderedProminent)
                    .buttonBorderShape(.capsule)
                    .tint(.blue)
                    .accessibilityLabel(copy.finishLong)
                    cancelButton
                }
            } else {
                HStack(spacing: 10) {
                    Text(copy.detail(state.phase))
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                        .frame(maxWidth: .infinity, alignment: .leading)
                    if state.canCancel { cancelButton }
                }
            }
        }
        .padding(16)
    }

    private var cancelButton: some View {
        Button(intent: StopVoiceAssistantIntent(sessionID: sessionID)) {
            Image(systemName: "xmark")
                .font(.subheadline.weight(.semibold))
                .frame(width: 36, height: 36)
        }
        .buttonStyle(.bordered)
        .buttonBorderShape(.circle)
        .tint(.secondary)
        .accessibilityLabel(copy.cancel)
    }
}

/// Done, Pause and Cancel while recording; progress and Cancel while the
/// reply is made; a short confirmation once it waits in the keyboard.
private struct VoiceIslandControls: View {
    let state: VoiceAssistantAttributes.ContentState
    let sessionID: String
    let copy: VoiceActivityCopy

    var body: some View {
        HStack(spacing: 10) {
            if state.isRecording {
                Button(intent: FinishVoiceAssistantIntent(sessionID: sessionID)) {
                    Label(copy.finish, systemImage: "checkmark")
                        .font(.body.weight(.semibold))
                        .lineLimit(1)
                        .frame(maxWidth: .infinity, minHeight: 40)
                }
                .buttonStyle(.borderedProminent)
                .buttonBorderShape(.capsule)
                .tint(.blue)
                .accessibilityLabel(copy.finishLong)
                Button(intent: PauseVoiceAssistantIntent(sessionID: sessionID)) {
                    Image(systemName: "pause.fill")
                        .font(.body.weight(.semibold))
                        .frame(width: 40, height: 40)
                }
                .buttonStyle(.bordered)
                .buttonBorderShape(.circle)
                .tint(.white)
                .accessibilityLabel(copy.pause)
            } else if state.isPreparing || state.isProcessing {
                ProgressView().tint(.blue)
                Text(copy.detail(state.phase))
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
                    .frame(maxWidth: .infinity, alignment: .leading)
            } else if state.phase == "responseReady" {
                Image(systemName: "keyboard")
                    .font(.title3)
                    .foregroundStyle(.green)
                    .accessibilityHidden(true)
                Text(copy.detail(state.phase))
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
                    .frame(maxWidth: .infinity, alignment: .leading)
            } else {
                Text(copy.detail(state.phase))
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            if state.canCancel {
                Button(intent: StopVoiceAssistantIntent(sessionID: sessionID)) {
                    Image(systemName: "xmark")
                        .font(.body.weight(.semibold))
                        .frame(width: 40, height: 40)
                }
                .buttonStyle(.bordered)
                .buttonBorderShape(.circle)
                .tint(.red)
                .accessibilityLabel(copy.cancel)
            }
        }
    }
}

/// Elapsed recording time (0:12), drawn and advanced by the system without
/// waking the app; it stops at the 40-second limit.
private struct RecordingTimer: View {
    let start: Date
    let end: Date

    var body: some View {
        Text(timerInterval: start...max(start, end), countsDown: false, showsHours: false)
            .monospacedDigit()
            .multilineTextAlignment(.trailing)
    }
}

private extension VoiceAssistantAttributes.ContentState {
    var isPreparing: Bool { phase == "ready" }
    var isRecording: Bool { phase == "listening" || phase == "speechDetected" }
    var isProcessing: Bool { phase == "transcribing" || phase == "generating" }
    var canCancel: Bool { !["stopped", "error", "responseReady", "paused"].contains(phase) }

    var symbol: String {
        switch phase {
        case "listening", "speechDetected": return "mic.fill"
        case "transcribing": return "waveform"
        case "generating": return "sparkles"
        case "responseReady": return "checkmark.circle.fill"
        case "paused": return "pause.circle.fill"
        case "stopped": return "stop.circle.fill"
        case "error": return "exclamationmark.circle.fill"
        default: return "mic"
        }
    }

    var tint: Color {
        switch phase {
        case "responseReady": return .green
        case "error": return .orange
        case "paused", "stopped": return .secondary
        default: return .blue
        }
    }
}

/// The activity speaks the app's language (as the keyboard does), not the
/// device's: `VoiceAssistantAttributes.language` carries its code.
struct VoiceActivityCopy {
    private let code: String

    init(_ code: String?) { self.code = code ?? Locale.preferredLanguages.first.map { String($0.prefix(2)) } ?? "en" }

    private func text(_ en: String, _ ru: String, _ kk: String, _ uz: String) -> String {
        switch code {
        case "ru": return ru
        case "kk": return kk
        case "uz": return uz
        default: return en
        }
    }

    var finish: String { text("Done", "Готово", "Дайын", "Tayyor") }
    var finishLong: String { text("Finish and generate response", "Закончить и создать ответ", "Аяқтау және жауап жасау", "Tugatish va javob yaratish") }
    var pause: String { text("Pause recording", "Приостановить запись", "Жазуды кідірту", "Yozishni pauza qilish") }
    var cancel: String { text("Cancel voice session", "Отменить голосовой запрос", "Дауыс сеансын болдырмау", "Ovoz seansini bekor qilish") }

    func status(_ phase: String) -> String {
        switch phase {
        case "listening": return text("Listening", "Слушаю", "Тыңдап тұрмын", "Tinglayapman")
        case "speechDetected": return text("Hearing you", "Слышу вас", "Естіп тұрмын", "Eshityapman")
        case "transcribing": return text("Transcribing", "Распознаю речь", "Сөз танылуда", "Nutq tanilmoqda")
        case "generating": return text("Writing reply", "Создаю ответ", "Жауап жасалуда", "Javob yozilmoqda")
        case "responseReady": return text("Reply ready", "Ответ готов", "Жауап дайын", "Javob tayyor")
        case "paused": return text("Recording paused", "Запись на паузе", "Жазу кідіртілді", "Yozish pauzada")
        case "stopped": return text("Recording stopped", "Запись остановлена", "Жазу тоқтатылды", "Yozish to‘xtatildi")
        case "error": return text("Please try again", "Попробуйте снова", "Қайталап көріңіз", "Qayta urinib ko‘ring")
        default: return text("Preparing microphone", "Готовлю микрофон", "Микрофон дайындалуда", "Mikrofon tayyorlanmoqda")
        }
    }

    func detail(_ phase: String) -> String {
        switch phase {
        case "transcribing", "generating":
            return text("The reply will appear in the AI Reply keyboard.", "Ответ появится в клавиатуре AI Reply.", "Жауап AI Reply пернетақтасында шығады.", "Javob AI Reply klaviaturasida chiqadi.")
        case "responseReady":
            return text("Open the AI Reply keyboard and tap Insert.", "Откройте клавиатуру AI Reply и нажмите «Вставить».", "AI Reply пернетақтасын ашып, «Кірістіру» түймесін басыңыз.", "AI Reply klaviaturasini ochib, «Kiritish»ni bosing.")
        case "error":
            return text("Open AI Reply to try again.", "Откройте AI Reply, чтобы повторить.", "Қайталау үшін AI Reply ашыңыз.", "Qayta urinish uchun AI Reply’ni oching.")
        case "paused":
            return text("Continue in AI Reply.", "Продолжите в AI Reply.", "AI Reply ішінде жалғастырыңыз.", "AI Reply’da davom eting.")
        default:
            return text("The microphone starts in a moment.", "Микрофон включится через мгновение.", "Микрофон бірден қосылады.", "Mikrofon hozir yoqiladi.")
        }
    }
}

#if DEBUG
#Preview("Listening", as: .dynamicIsland(.expanded),
         using: VoiceAssistantAttributes(sessionID: "preview-listening", startedAt: .now, language: "ru")) {
    AIReplyVoiceLiveActivity()
} contentStates: {
    VoiceAssistantAttributes.ContentState(phase: "listening", level: 0.7, deadline: .now.addingTimeInterval(40),
                                          recordingStartedAt: .now)
}

#Preview("Generating", as: .dynamicIsland(.expanded),
         using: VoiceAssistantAttributes(sessionID: "preview-generating", startedAt: .now, language: "ru")) {
    AIReplyVoiceLiveActivity()
} contentStates: {
    VoiceAssistantAttributes.ContentState(phase: "generating", deadline: .now.addingTimeInterval(45))
}

#Preview("Response ready", as: .dynamicIsland(.expanded),
         using: VoiceAssistantAttributes(sessionID: "preview-response", startedAt: .now, language: "ru")) {
    AIReplyVoiceLiveActivity()
} contentStates: {
    VoiceAssistantAttributes.ContentState(phase: "responseReady", deadline: .now.addingTimeInterval(3))
}
#endif
