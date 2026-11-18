import ActivityKit
import AppIntents
import SwiftUI
import UIKit
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

@available(iOS 18.0, *)
struct AIReplyVoiceControl: ControlWidget {
    // Control Center renders this outside our process. Use an OS-owned
    // symbol, with the original microphone on systems lacking the reply badge.
    private var symbolName: String {
        let replyMicrophone = "microphone.badge.ellipsis.fill"
        return UIImage(systemName: replyMicrophone) != nil ? replyMicrophone : "mic.fill"
    }

    var body: some ControlWidgetConfiguration {
        StaticControlConfiguration(kind: "kz.ai-reply.voice.start") {
            ControlWidgetButton(action: StartVoiceAssistantIntent()) {
                Label("AI Reply Voice", systemImage: symbolName)
                    .contentTransition(.identity)
            }
        }
        .displayName("AI Reply Voice")
        .description("Start a voice instruction without opening AI Reply after first-time setup.")
    }
}

struct AIReplyVoiceLiveActivity: Widget {
    var body: some WidgetConfiguration {
        ActivityConfiguration(for: VoiceAssistantAttributes.self) { context in
            HStack(spacing: 14) {
                if context.state.isPreparing || context.state.isProcessing {
                    ProgressView().tint(.blue)
                } else {
                    Image(systemName: context.state.symbol)
                        .font(.title2.weight(.semibold))
                        .foregroundStyle(.blue)
                }
                VStack(alignment: .leading, spacing: 4) {
                    Text("AI Reply Voice").font(.headline)
                    Text(context.state.status).font(.subheadline).foregroundStyle(.secondary)
                }
                Spacer(minLength: 0)
                if context.state.isRecording {
                    Button(intent: FinishVoiceAssistantIntent(sessionID: context.attributes.sessionID)) {
                        Label("Finish", systemImage: "checkmark")
                            .font(.subheadline.weight(.semibold))
                            .frame(minHeight: 44)
                    }
                    .buttonStyle(.borderedProminent)
                    .tint(.blue)
                    .accessibilityLabel("Finish and generate response")
                }
                if context.state.canCancel {
                    Button(intent: StopVoiceAssistantIntent(sessionID: context.attributes.sessionID)) {
                        Image(systemName: "xmark")
                            .font(.subheadline.weight(.semibold))
                            .frame(width: 28, height: 44)
                    }
                    .buttonStyle(.borderless)
                    .tint(.red)
                    .accessibilityLabel("Cancel voice session")
                }
            }
            .padding(16)
            .activityBackgroundTint(Color(.secondarySystemBackground))
            .activitySystemActionForegroundColor(.blue)
            .widgetURL(URL(string: "aireply://voice"))
        } dynamicIsland: { context in
            DynamicIsland {
                DynamicIslandExpandedRegion(.leading) {
                    Label("AI Reply", systemImage: context.state.symbol)
                        .font(.headline)
                        .foregroundStyle(.blue)
                }
                DynamicIslandExpandedRegion(.trailing) {
                    if context.state.isRecording {
                        Text(timerInterval: context.attributes.startedAt...max(context.attributes.startedAt, context.state.deadline), countsDown: true)
                            .font(.caption.monospacedDigit())
                            .frame(width: 48)
                    } else {
                        Image(systemName: context.state.symbol).foregroundStyle(.blue)
                    }
                }
                DynamicIslandExpandedRegion(.center) {
                    Text(context.state.status).font(.caption).lineLimit(1)
                }
                DynamicIslandExpandedRegion(.bottom) {
                    HStack(spacing: 12) {
                        if context.state.isRecording {
                            VoiceActivityMeter(level: context.state.level, active: true)
                                .frame(width: 28)
                                .accessibilityLabel(context.state.status)
                            Spacer(minLength: 0)
                            Button(intent: FinishVoiceAssistantIntent(sessionID: context.attributes.sessionID)) {
                                Label("Finish", systemImage: "checkmark")
                                    .font(.subheadline.weight(.semibold))
                                    .frame(minWidth: 96, minHeight: 44)
                            }
                            .buttonStyle(.borderedProminent)
                            .tint(.blue)
                            .accessibilityLabel("Finish and generate response")
                            Button(intent: PauseVoiceAssistantIntent(sessionID: context.attributes.sessionID)) {
                                Image(systemName: "pause.fill")
                                    .frame(width: 28, height: 44)
                            }
                            .buttonStyle(.bordered)
                            .accessibilityLabel("Pause recording")
                        } else if context.state.isPreparing || context.state.isProcessing {
                            ProgressView().tint(.blue)
                            Spacer(minLength: 0)
                        } else if context.state.phase == "responseReady" {
                            Image(systemName: "checkmark.circle.fill")
                                .font(.title2)
                                .foregroundStyle(.green)
                            Spacer(minLength: 0)
                            if #available(iOS 18.0, *) {
                                Button(intent: StartVoiceAssistantIntent()) {
                                    Label("Next instruction", systemImage: "mic.fill")
                                        .font(.subheadline.weight(.semibold))
                                        .frame(minHeight: 44)
                                }
                                .buttonStyle(.borderedProminent)
                                .tint(.blue)
                            }
                        } else {
                            Spacer(minLength: 0)
                        }
                        if context.state.canCancel {
                            Button(intent: StopVoiceAssistantIntent(sessionID: context.attributes.sessionID)) {
                                Image(systemName: "xmark")
                                    .font(.subheadline.weight(.semibold))
                                    .frame(width: 28, height: 44)
                            }
                            .buttonStyle(.borderless)
                            .tint(.red)
                            .accessibilityLabel("Cancel voice session")
                        }
                    }
                    .padding(.top, 8)
                }
            } compactLeading: {
                Image(systemName: context.state.symbol).foregroundStyle(.blue)
            } compactTrailing: {
                if context.state.isRecording {
                    VoiceActivityMeter(level: context.state.level, active: true)
                        .frame(width: 25, height: 18)
                } else if context.state.isPreparing || context.state.isProcessing {
                    ProgressView().controlSize(.mini)
                } else {
                    Image(systemName: context.state.symbol).foregroundStyle(.blue)
                }
            } minimal: {
                Image(systemName: context.state.symbol).foregroundStyle(.blue)
            }
            .widgetURL(URL(string: "aireply://voice"))
            .keylineTint(.blue)
        }
    }
}

private struct VoiceActivityMeter: View {
    let level: Double
    let active: Bool
    private let heights: [Double] = [0.45, 0.75, 1, 0.65, 0.4]

    var body: some View {
        HStack(spacing: 3) {
            ForEach(heights.indices, id: \.self) { index in
                Capsule()
                    .fill(active ? Color.blue : Color.secondary)
                    .frame(width: 3, height: active ? 5 + 20 * heights[index] * min(1, max(0.1, level)) : 4)
            }
        }
        .frame(height: 28)
    }
}

private extension VoiceAssistantAttributes.ContentState {
    var isPreparing: Bool { phase == "ready" }
    var isRecording: Bool { phase == "listening" || phase == "speechDetected" }
    var isProcessing: Bool { phase == "transcribing" || phase == "generating" }
    var canCancel: Bool { !["stopped", "error", "responseReady"].contains(phase) }

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

    var status: LocalizedStringKey {
        switch phase {
        case "listening": return "Listening"
        case "speechDetected": return "Speech detected"
        case "transcribing": return "Transcribing"
        case "generating": return "Generating response"
        case "responseReady": return "Response in keyboard"
        case "paused": return "Recording paused"
        case "stopped": return "Recording stopped"
        case "error": return "Open AI Reply to try again"
        default: return "Preparing microphone…"
        }
    }
}

#if DEBUG
#Preview("Preparing", as: .dynamicIsland(.expanded),
         using: VoiceAssistantAttributes(sessionID: "preview-preparing", startedAt: .now)) {
    AIReplyVoiceLiveActivity()
} contentStates: {
    VoiceAssistantAttributes.ContentState(phase: "ready", deadline: .now.addingTimeInterval(40))
}

#Preview("Listening", as: .dynamicIsland(.expanded),
         using: VoiceAssistantAttributes(sessionID: "preview-listening", startedAt: .now)) {
    AIReplyVoiceLiveActivity()
} contentStates: {
    VoiceAssistantAttributes.ContentState(phase: "listening", level: 0.7, deadline: .now.addingTimeInterval(40))
}

#Preview("Response ready", as: .dynamicIsland(.expanded),
         using: VoiceAssistantAttributes(sessionID: "preview-response", startedAt: .now)) {
    AIReplyVoiceLiveActivity()
} contentStates: {
    VoiceAssistantAttributes.ContentState(phase: "responseReady", deadline: .now.addingTimeInterval(3))
}
#endif
