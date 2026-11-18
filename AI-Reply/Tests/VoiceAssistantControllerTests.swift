import XCTest
@testable import AIReply

/// Exercises real context/privacy state transitions without opening a
/// microphone, contacting a server, or relying on a host conversation.
final class VoiceAssistantControllerTests: XCTestCase {
    @MainActor
    private func withController(_ body: (VoiceAssistantController, VoiceAssistantStore) throws -> Void) throws {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent("voice-controller-" + UUID().uuidString)
        let store = VoiceAssistantStore(rootURL: root)
        let key = "voiceAssistant.audioConsent.v1"
        let previousConsent = UserDefaults.standard.object(forKey: key)
        UserDefaults.standard.set(false, forKey: key)
        let controller = VoiceAssistantController(store: store)
        defer {
            controller.stop(clearPrivateContext: true)
            if let previousConsent { UserDefaults.standard.set(previousConsent, forKey: key) }
            else { UserDefaults.standard.removeObject(forKey: key) }
            try? FileManager.default.removeItem(at: root)
        }
        try body(controller, store)
    }

    @MainActor
    func testSystemLaunchClearsPriorConversationAndDoesNotRecordWithoutConsent() throws {
        try withController { voice, _ in
            voice.sourceMessage = "The preceding chat's message"
            voice.open(startWhenPossible: true)
            XCTAssertTrue(voice.isPresented)
            XCTAssertTrue(voice.sourceMessage.isEmpty)
            XCTAssertFalse(voice.isStarting)
            XCTAssertFalse(voice.isCapturing)
            XCTAssertEqual(voice.snapshot.phase, .stopped)
        }
    }

    @MainActor
    func testOpeningAppInterfacePreservesExplicitlyEnteredContext() throws {
        try withController { voice, _ in
            voice.sourceMessage = "Message deliberately entered in the app"
            voice.templateID = "chosen-template"
            voice.open()
            XCTAssertEqual(voice.sourceMessage, "Message deliberately entered in the app")
            XCTAssertEqual(voice.templateID, "chosen-template")
            XCTAssertFalse(voice.isCapturing)
        }
    }

    @MainActor
    func testActiveKeyboardContextUpdatesRequestAndDisplayedContextTogether() throws {
        try withController { voice, store in
            voice.snapshot = VoiceAssistantSnapshot(sessionID: "authorized-session", phase: .listening,
                requestID: "original-request", sourceMessage: "Previous context", templateID: "friend")
            let command = VoiceAssistantCommand(kind: .request, sessionID: "authorized-session",
                keyboardSessionID: "current-field", sourceMessage: "Explicitly pasted current message", templateID: "business")
            XCTAssertTrue(store.send(command))
            voice.refreshPublishedState()
            XCTAssertEqual(voice.snapshot.sourceMessage, "Explicitly pasted current message")
            XCTAssertEqual(voice.sourceMessage, voice.snapshot.sourceMessage)
            XCTAssertEqual(voice.templateID, "business")
            XCTAssertEqual(voice.snapshot.templateID, voice.templateID)
            XCTAssertEqual(voice.snapshot.keyboardSessionID, "current-field")
            XCTAssertEqual(voice.snapshot.requestID, "original-request")
            XCTAssertTrue(store.pendingCommands(sessionID: "authorized-session").isEmpty)
        }
    }

    @MainActor
    func testProcessingRequestCannotBeRetargetedByALateClipboardCommand() throws {
        try withController { voice, store in
            voice.snapshot = VoiceAssistantSnapshot(sessionID: "authorized-session", phase: .generating,
                requestID: "request", keyboardSessionID: "original-field", sourceMessage: "Original message", templateID: "friend")
            XCTAssertTrue(store.send(VoiceAssistantCommand(kind: .context, sessionID: "authorized-session",
                keyboardSessionID: "different-field", sourceMessage: "Late message", templateID: "business")))
            voice.refreshPublishedState()
            XCTAssertEqual(voice.snapshot.sourceMessage, "Original message")
            XCTAssertEqual(voice.snapshot.keyboardSessionID, "original-field")
            XCTAssertEqual(voice.snapshot.templateID, "friend")
            XCTAssertTrue(store.pendingCommands(sessionID: "authorized-session").isEmpty)
        }
    }

    @MainActor
    func testAnotherSessionCannotChangeRecordingContext() throws {
        try withController { voice, store in
            voice.snapshot = VoiceAssistantSnapshot(sessionID: "current-session", phase: .listening,
                keyboardSessionID: "current-field", sourceMessage: "Current message")
            XCTAssertTrue(store.send(VoiceAssistantCommand(kind: .context, sessionID: "old-session",
                keyboardSessionID: "old-field", sourceMessage: "Message from another conversation")))
            voice.refreshPublishedState()
            XCTAssertEqual(voice.snapshot.sessionID, "current-session")
            XCTAssertEqual(voice.snapshot.keyboardSessionID, "current-field")
            XCTAssertEqual(voice.snapshot.sourceMessage, "Current message")
        }
    }

    @MainActor
    func testPrivacyResetClearsPrivateTextAndInvalidatesOldSession() throws {
        try withController { voice, store in
            voice.sourceMessage = "Private copied message"
            voice.templateID = "private-template"
            voice.snapshot = VoiceAssistantSnapshot(sessionID: "old-session", phase: .responseReady,
                transcript: "Private instruction", response: "Private response", requestID: "old-request",
                keyboardSessionID: "old-field", error: "Private failure", sourceMessage: voice.sourceMessage,
                templateID: voice.templateID)
            voice.stop(clearPrivateContext: true)
            XCTAssertTrue(voice.sourceMessage.isEmpty)
            XCTAssertNil(voice.templateID)
            XCTAssertNotEqual(voice.snapshot.sessionID, "old-session")
            XCTAssertEqual(voice.snapshot.phase, .stopped)
            XCTAssertTrue(voice.snapshot.transcript.isEmpty)
            XCTAssertTrue(voice.snapshot.response.isEmpty)
            XCTAssertTrue(voice.snapshot.sourceMessage.isEmpty)
            XCTAssertNil(voice.snapshot.keyboardSessionID)
            XCTAssertNil(voice.snapshot.requestID)
            XCTAssertNil(voice.snapshot.error)
            XCTAssertEqual(store.readSnapshot(), voice.snapshot)
        }
    }

    @MainActor
    func testExpiredForegroundResultClearsBothMemoryAndStoredPrivateText() throws {
        try withController { voice, store in
            voice.sourceMessage = "Expired private source"
            voice.snapshot = VoiceAssistantSnapshot(sessionID: "expired-session", phase: .responseReady,
                transcript: "Expired private instruction", response: "Expired private response",
                updatedAt: Date().addingTimeInterval(-300), expiresAt: Date().addingTimeInterval(-1),
                sourceMessage: voice.sourceMessage)
            XCTAssertTrue(store.writeSnapshot(voice.snapshot))
            voice.refreshPublishedState()
            XCTAssertTrue(voice.sourceMessage.isEmpty)
            XCTAssertTrue(voice.snapshot.response.isEmpty)
            XCTAssertTrue(voice.snapshot.transcript.isEmpty)
            XCTAssertNotEqual(voice.snapshot.sessionID, "expired-session")
            let data = try Data(contentsOf: XCTUnwrap(store.rootURL).appendingPathComponent("session.json"))
            XCTAssertFalse(String(decoding: data, as: UTF8.self).contains("Expired private"))
        }
    }

    @MainActor
    func testSavedResultRestoresMatchingContextWithoutResumingMicrophone() throws {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent("voice-restore-" + UUID().uuidString)
        let store = VoiceAssistantStore(rootURL: root)
        defer { try? FileManager.default.removeItem(at: root) }
        let saved = VoiceAssistantSnapshot(sessionID: "saved-session", phase: .responseReady,
            transcript: "Instruction", response: "Response", requestID: "request",
            sourceMessage: "Saved copied message", templateID: "saved-template")
        XCTAssertTrue(store.writeSnapshot(saved))
        let voice = VoiceAssistantController(store: store)
        defer { voice.stop(clearPrivateContext: true) }
        XCTAssertEqual(voice.snapshot, saved)
        XCTAssertEqual(voice.sourceMessage, saved.sourceMessage)
        XCTAssertEqual(voice.templateID, saved.templateID)
        XCTAssertFalse(voice.isCapturing)
        XCTAssertFalse(voice.isStarting)
    }

    @MainActor
    func testLockedColdLaunchPreservesSavedResultAndRestoresItAfterUnlock() throws {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent("voice-locked-" + UUID().uuidString)
        let store = VoiceAssistantStore(rootURL: root)
        defer { try? FileManager.default.removeItem(at: root) }
        let saved = VoiceAssistantSnapshot(sessionID: "saved-before-lock", phase: .responseReady,
            transcript: "Private instruction", response: "Private response", requestID: "saved-request",
            sourceMessage: "Private source", templateID: "saved-template")
        XCTAssertTrue(store.writeSnapshot(saved))
        let file = root.appendingPathComponent("session.json")
        let original = try Data(contentsOf: file)
        var available = false
        let voice = VoiceAssistantController(store: store, protectedDataAvailable: { available })
        defer { voice.stop(clearPrivateContext: true) }

        XCTAssertTrue(voice.snapshot.response.isEmpty)
        voice.refreshPublishedState()
        XCTAssertEqual(try Data(contentsOf: file), original)

        available = true
        voice.refreshPublishedState()
        XCTAssertEqual(voice.snapshot, saved)
        XCTAssertEqual(voice.sourceMessage, saved.sourceMessage)
        XCTAssertEqual(voice.templateID, saved.templateID)
        XCTAssertFalse(voice.isCapturing)
        XCTAssertFalse(voice.isStarting)
        XCTAssertEqual(store.readSnapshot(), saved)
    }

    @MainActor
    func testLockedColdLaunchDefersOrphanAndExpiredCommandCleanupUntilUnlock() throws {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent("voice-locked-orphan-" + UUID().uuidString)
        let store = VoiceAssistantStore(rootURL: root)
        defer { try? FileManager.default.removeItem(at: root) }
        let orphan = VoiceAssistantSnapshot(sessionID: "orphan-recording", phase: .listening,
            transcript: "Orphan private text", sourceMessage: "Orphan private source")
        XCTAssertTrue(store.writeSnapshot(orphan))
        let expired = VoiceAssistantCommand(kind: .context, sessionID: orphan.sessionID,
            sourceMessage: "Expired private command", createdAt: Date().addingTimeInterval(-300))
        XCTAssertTrue(store.send(expired))
        let commandFile = root.appendingPathComponent(expired.id + ".command")
        var available = false
        let voice = VoiceAssistantController(store: store, protectedDataAvailable: { available })
        defer { voice.stop(clearPrivateContext: true) }
        voice.refreshPublishedState()
        XCTAssertEqual(store.readSnapshot(), orphan)
        XCTAssertTrue(FileManager.default.fileExists(atPath: commandFile.path))

        available = true
        voice.refreshPublishedState()
        XCTAssertEqual(voice.snapshot.phase, .stopped)
        XCTAssertNotEqual(voice.snapshot.sessionID, orphan.sessionID)
        XCTAssertTrue(voice.snapshot.transcript.isEmpty)
        XCTAssertTrue(voice.sourceMessage.isEmpty)
        XCTAssertFalse(voice.isCapturing)
        XCTAssertFalse(FileManager.default.fileExists(atPath: commandFile.path))
    }

    @MainActor
    func testPrivacyResetWhileLockedCancelsDeferredResultRestore() throws {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent("voice-locked-reset-" + UUID().uuidString)
        let store = VoiceAssistantStore(rootURL: root)
        defer { try? FileManager.default.removeItem(at: root) }
        let saved = VoiceAssistantSnapshot(sessionID: "old-private-result", phase: .responseReady,
            transcript: "Withdrawn instruction", response: "Withdrawn response", sourceMessage: "Withdrawn source")
        XCTAssertTrue(store.writeSnapshot(saved))
        var available = false
        let voice = VoiceAssistantController(store: store, protectedDataAvailable: { available })
        defer { voice.stop(clearPrivateContext: true) }

        voice.stop(clearPrivateContext: true)
        let resetID = voice.snapshot.sessionID
        available = true
        voice.refreshPublishedState()
        XCTAssertEqual(voice.snapshot.sessionID, resetID)
        XCTAssertNotEqual(resetID, saved.sessionID)
        XCTAssertEqual(voice.snapshot.phase, .stopped)
        XCTAssertTrue(voice.snapshot.response.isEmpty)
        XCTAssertTrue(voice.snapshot.transcript.isEmpty)
        XCTAssertTrue(voice.sourceMessage.isEmpty)
        XCTAssertEqual(store.readSnapshot(), voice.snapshot)
    }
}
