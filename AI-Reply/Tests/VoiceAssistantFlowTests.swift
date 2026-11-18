import XCTest
@testable import AIReply

final class VoiceAssistantFlowTests: XCTestCase {
    func testPausesInARecognizedInstructionDoNotEndRecording() {
        // The old two-second silence cutoff interrupted natural speech and
        // removed the Island before the user could press Done.
        for elapsed in [2.0, 5, 12, 20, 30, 39.9] {
            XCTAssertEqual(VoiceAssistantRecordingPolicy.decision(elapsed: elapsed, hasSpeech: true), .keepRecording)
        }
    }

    func testRecordingStillHasAnExplicitFiniteSafetyLimit() {
        XCTAssertEqual(VoiceAssistantRecordingPolicy.decision(elapsed: 40, hasSpeech: true), .finish)
        XCTAssertEqual(VoiceAssistantRecordingPolicy.decision(elapsed: 50, hasSpeech: true), .finish)
    }

    func testInitialSilenceLeavesTimeToReturnToAChatThenReleasesTheMicrophone() {
        for elapsed in [2.0, 12, 19.9] {
            XCTAssertEqual(VoiceAssistantRecordingPolicy.decision(elapsed: elapsed, hasSpeech: false), .keepRecording)
        }
        XCTAssertEqual(VoiceAssistantRecordingPolicy.decision(elapsed: 20, hasSpeech: false), .noSpeech)
    }
}
