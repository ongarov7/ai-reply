import XCTest
@testable import AIReply

final class VoiceAssistantControlStartTests: XCTestCase {
    /// A system control is not permission to bypass the app's explicit audio
    /// consent. This refusal must happen before session/UI/capture mutation.
    @MainActor
    func testControlWithoutVoiceConsentDoesNotStartOrOpenTheAppInterface() async throws {
        let root = FileManager.default.temporaryDirectory
            .appendingPathComponent("voice-control-preflight-" + UUID().uuidString)
        let store = VoiceAssistantStore(rootURL: root)
        let key = "voiceAssistant.audioConsent.v1"
        let previousConsent = UserDefaults.standard.object(forKey: key)
        UserDefaults.standard.set(false, forKey: key)
        let voice = VoiceAssistantController(store: store)
        defer {
            voice.stop(clearPrivateContext: true)
            if let previousConsent { UserDefaults.standard.set(previousConsent, forKey: key) }
            else { UserDefaults.standard.removeObject(forKey: key) }
            try? FileManager.default.removeItem(at: root)
        }
        let before = voice.snapshot
        voice.sourceMessage = "An earlier deliberately supplied message"

        do {
            try await voice.startFromControl()
            XCTFail("A control started voice without its explicit consent")
        } catch let error as VoiceAssistantControlStartError {
            XCTAssertFalse(error.localizedDescription.isEmpty)
        }

        XCTAssertFalse(voice.isPresented)
        XCTAssertFalse(voice.isStarting)
        XCTAssertFalse(voice.isCapturing)
        XCTAssertEqual(voice.snapshot, before)
        XCTAssertEqual(store.readSnapshot(), before)
        XCTAssertEqual(voice.sourceMessage, "An earlier deliberately supplied message")
    }
}
