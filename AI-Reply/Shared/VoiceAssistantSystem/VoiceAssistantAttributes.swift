import ActivityKit
import Foundation

/// Only task status crosses into the Lock Screen. Message, instruction and
/// generated response stay in the protected App Group and the unlocked app.
struct VoiceAssistantAttributes: ActivityAttributes {
    struct ContentState: Codable, Hashable {
        var phase: String
        var level: Double = 0
        var deadline: Date
    }

    var sessionID: String
    var startedAt: Date
}
