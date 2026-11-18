import XCTest
@testable import AIReply

final class VoiceAssistantStoreTests: XCTestCase {
    private var root: URL!
    private var store: VoiceAssistantStore!
    private let now = Date(timeIntervalSince1970: 1_800_000_000)

    override func setUpWithError() throws {
        root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        store = VoiceAssistantStore(rootURL: root)
    }

    override func tearDownWithError() throws { try? FileManager.default.removeItem(at: root) }

    func testProtectedSnapshotRoundTripsAndExpires() {
        let snapshot = VoiceAssistantSnapshot(sessionID: "session-1", phase: .responseReady,
            transcript: "Ответь по-казахски", response: "Ертең келемін.", requestID: "request-1",
            updatedAt: now, expiresAt: now.addingTimeInterval(30), keyboardSessionID: "keyboard-1",
            sourceMessage: "Can you come?", templateID: "friend")
        XCTAssertTrue(store.writeSnapshot(snapshot))
        XCTAssertEqual(store.readSnapshot(now: now), snapshot)
        XCTAssertNil(store.readSnapshot(now: now.addingTimeInterval(30)))
    }

    func testCommandsAreImmutableBoundAndConsumedExplicitly() {
        let command = VoiceAssistantCommand(id: "command-1", kind: .request, sessionID: "session-1",
            keyboardSessionID: "keyboard-1", sourceMessage: "Message", templateID: "client", createdAt: now)
        XCTAssertTrue(store.send(command))
        var duplicate = command
        duplicate.sourceMessage = "Replaced message"
        XCTAssertFalse(store.send(duplicate))
        XCTAssertEqual(store.pendingCommands(sessionID: "session-1", now: now), [command])
        XCTAssertTrue(store.pendingCommands(sessionID: "session-2", now: now).isEmpty)
        store.removeCommand(command)
        XCTAssertTrue(store.pendingCommands(sessionID: "session-1", now: now).isEmpty)
    }

    func testOnlyOneConcurrentDeliveryWins() {
        let command = VoiceAssistantCommand(id: "same-command", kind: .stop, sessionID: "session-1", createdAt: now)
        let store = self.store!
        let lock = NSLock()
        var wins = 0
        DispatchQueue.concurrentPerform(iterations: 24) { _ in
            if store.send(command) {
                lock.lock(); wins += 1; lock.unlock()
            }
        }
        XCTAssertEqual(wins, 1)
        XCTAssertEqual(store.pendingCommands(sessionID: "session-1", now: now), [command])
    }

    func testStaleFutureAndInvalidIdentifiersAreRejected() {
        let stale = VoiceAssistantCommand(kind: .finish, sessionID: "session", createdAt: now.addingTimeInterval(-181))
        let future = VoiceAssistantCommand(kind: .finish, sessionID: "session", createdAt: now.addingTimeInterval(11))
        XCTAssertTrue(store.send(stale))
        XCTAssertTrue(store.send(future))
        XCTAssertTrue(store.pendingCommands(sessionID: "session", now: now).isEmpty)
        XCTAssertFalse(store.send(VoiceAssistantCommand(id: "../escape", kind: .stop, sessionID: "session")))
        XCTAssertFalse(store.writeSnapshot(VoiceAssistantSnapshot(sessionID: "../escape", phase: .ready)))
        store.purge(now: now)
        XCTAssertTrue((try? FileManager.default.contentsOfDirectory(atPath: root.path))?.isEmpty == true)
    }

    func testMissingAppGroupAndOversizedPayloadFailWithoutCrashing() {
        let missing = VoiceAssistantStore(rootURL: nil)
        XCTAssertNil(missing.readSnapshot())
        XCTAssertFalse(missing.send(VoiceAssistantCommand(kind: .stop, sessionID: "session")))
        let huge = VoiceAssistantCommand(kind: .context, sessionID: "session", sourceMessage: String(repeating: "a", count: 150_000))
        XCTAssertFalse(store.send(huge))
    }

    func testOnlyFreshCompletedResponseCanBeInserted() {
        var snapshot = VoiceAssistantSnapshot(sessionID: "session", phase: .responseReady,
            response: "Previous response", requestID: "request-1", updatedAt: now,
            expiresAt: now.addingTimeInterval(30))
        XCTAssertEqual(snapshot.readyResponse(now: now), "Previous response")
        snapshot.phase = .generating
        snapshot.requestID = "request-2"
        XCTAssertNil(snapshot.readyResponse(now: now), "retained text belongs to the previous request")
        snapshot.phase = .error
        XCTAssertNil(snapshot.readyResponse(now: now))
        snapshot.phase = .responseReady
        XCTAssertNil(snapshot.readyResponse(now: now.addingTimeInterval(31)))
        snapshot.response = " \n "
        XCTAssertNil(snapshot.readyResponse(now: now))
    }

    func testInactiveSessionsNeverOccupyTheKeyboardRow() {
        for phase in [VoiceAssistantPhase.ready, .paused, .stopped] {
            let snapshot = VoiceAssistantSnapshot(sessionID: "session", phase: phase,
                response: "Retained response", updatedAt: now, expiresAt: now.addingTimeInterval(180),
                error: "Recording failed")
            XCTAssertFalse(snapshot.isVisibleInKeyboard(now: now), "inactive phase: \(phase)")
            XCTAssertFalse(snapshot.showsMicrophoneInKeyboard(now: now))
        }
    }

    func testMeaningfulFailureNoticeExpiresAfterTenSecondsAndNeverIndicatesRecording() {
        var snapshot = VoiceAssistantSnapshot(sessionID: "session", phase: .error,
            updatedAt: now, expiresAt: now.addingTimeInterval(180), error: "No speech detected. Please try again.")
        XCTAssertFalse(snapshot.phase.isRecording)
        XCTAssertFalse(snapshot.showsMicrophoneInKeyboard(now: now))
        XCTAssertTrue(snapshot.isVisibleInKeyboard(now: now))
        XCTAssertTrue(snapshot.isVisibleInKeyboard(now: now.addingTimeInterval(9)))
        XCTAssertFalse(snapshot.isVisibleInKeyboard(now: now.addingTimeInterval(10)))
        XCTAssertFalse(snapshot.isVisibleInKeyboard(now: now.addingTimeInterval(11)))
        snapshot.error = nil
        XCTAssertFalse(snapshot.isVisibleInKeyboard(now: now))
        snapshot.error = " \n "
        XCTAssertFalse(snapshot.isVisibleInKeyboard(now: now))
    }

    func testKeyboardShowsOnlyLiveWorkOrACompletedNonemptyResponse() {
        for phase in [VoiceAssistantPhase.listening, .speechDetected, .transcribing, .generating] {
            let snapshot = VoiceAssistantSnapshot(sessionID: "session", phase: phase,
                updatedAt: now, expiresAt: now.addingTimeInterval(180))
            XCTAssertTrue(snapshot.isVisibleInKeyboard(now: now), "active phase: \(phase)")
            XCTAssertEqual(snapshot.showsMicrophoneInKeyboard(now: now), phase.isRecording)
        }
        var ready = VoiceAssistantSnapshot(sessionID: "session", phase: .responseReady,
            updatedAt: now, expiresAt: now.addingTimeInterval(180))
        XCTAssertFalse(ready.isVisibleInKeyboard(now: now))
        ready.response = "Ready to insert"
        XCTAssertTrue(ready.isVisibleInKeyboard(now: now))
        XCTAssertFalse(ready.showsMicrophoneInKeyboard(now: now), "the keyboard cannot start another recording")
    }

    func testOrphanedRecordingAndProcessingIndicatorsDisappearWithoutAnOwnerHeartbeat() {
        var snapshot = VoiceAssistantSnapshot(sessionID: "session", phase: .listening,
            updatedAt: now, expiresAt: now.addingTimeInterval(180))
        XCTAssertTrue(snapshot.isVisibleInKeyboard(now: now.addingTimeInterval(10)))
        XCTAssertFalse(snapshot.isVisibleInKeyboard(now: now.addingTimeInterval(11)))
        snapshot.phase = .speechDetected
        XCTAssertFalse(snapshot.isVisibleInKeyboard(now: now.addingTimeInterval(11)))
        snapshot.phase = .transcribing
        XCTAssertTrue(snapshot.isVisibleInKeyboard(now: now.addingTimeInterval(90)))
        XCTAssertFalse(snapshot.isVisibleInKeyboard(now: now.addingTimeInterval(91)))
        snapshot.phase = .generating
        XCTAssertFalse(snapshot.isVisibleInKeyboard(now: now.addingTimeInterval(91)))
    }

    func testCompletedResponseUsesResultLifetimeWithoutPretendingTheMicrophoneIsActive() {
        let snapshot = VoiceAssistantSnapshot(sessionID: "session", phase: .responseReady,
            response: "Completed response", updatedAt: now, expiresAt: now.addingTimeInterval(180))
        XCTAssertFalse(snapshot.phase.isRecording)
        XCTAssertFalse(snapshot.showsMicrophoneInKeyboard(now: now))
        XCTAssertTrue(snapshot.isVisibleInKeyboard(now: now.addingTimeInterval(179)))
        XCTAssertFalse(snapshot.isVisibleInKeyboard(now: now.addingTimeInterval(180)))
    }

    func testOnlyLiveCaptureShowsMicrophoneWhileProcessingAndResultsKeepTheirOwnUI() {
        for phase in VoiceAssistantPhase.allCases {
            let snapshot = VoiceAssistantSnapshot(sessionID: "session", phase: phase,
                response: phase == .responseReady ? "Response ready to review" : "",
                updatedAt: now, expiresAt: now.addingTimeInterval(180))
            XCTAssertEqual(snapshot.showsMicrophoneInKeyboard(now: now), phase.isRecording, "phase: \(phase)")
            if phase == .transcribing || phase == .generating || phase == .responseReady {
                XCTAssertTrue(snapshot.isVisibleInKeyboard(now: now), "status or result remains available without a microphone")
            }
        }
    }

    func testCaptureCaptionPromptsBeforeSpeechAndKeepsFullMultilingualText() {
        var snapshot = VoiceAssistantSnapshot(sessionID: "session", phase: .listening,
            updatedAt: now, expiresAt: now.addingTimeInterval(180))
        XCTAssertEqual(snapshot.keyboardCaptureCaption(), .speak)
        snapshot.transcript = " \n "
        XCTAssertEqual(snapshot.keyboardCaptureCaption(), .speak)
        let instruction = String(repeating: "Жауапты ертең жіберемін. ", count: 12) + "Айдана, встреча в 15:30 — tomorrow."
        snapshot.transcript = instruction
        XCTAssertEqual(snapshot.keyboardCaptureCaption(), .recognized(preview: String(instruction.suffix(200)), fullText: instruction))
        XCTAssertEqual(snapshot.transcript, instruction, "preview truncation must not modify names, numbers, or the full instruction")
        snapshot.phase = .generating
        XCTAssertNil(snapshot.keyboardCaptureCaption())
    }

    func testKeyboardRevisionChangesWithLiveTranscriptWithoutRedrawingForHeartbeats() {
        var snapshot = VoiceAssistantSnapshot(sessionID: "session", phase: .speechDetected,
            transcript: "Ответь", requestID: "request", updatedAt: now, expiresAt: now.addingTimeInterval(180))
        let first = snapshot.keyboardRenderRevision(now: now)
        XCTAssertNotNil(first)
        snapshot.updatedAt = now.addingTimeInterval(1)
        XCTAssertEqual(snapshot.keyboardRenderRevision(now: now.addingTimeInterval(1)), first)
        snapshot.transcript = "Ответь Айдане, что приеду завтра в 15:30"
        XCTAssertNotEqual(snapshot.keyboardRenderRevision(now: now.addingTimeInterval(1)), first)
        snapshot.phase = .stopped
        XCTAssertNil(snapshot.keyboardRenderRevision(now: now.addingTimeInterval(1)))
    }

    func testCompletedSnapshotSurvivesWriterRecreationAndKeepsItsExpiry() throws {
        let listening = VoiceAssistantSnapshot(sessionID: "session", phase: .listening,
            requestID: "request", updatedAt: now, expiresAt: now.addingTimeInterval(180))
        XCTAssertTrue(store.writeSnapshot(listening))
        var completed = listening
        completed.phase = .responseReady
        completed.transcript = "Reply that I will come tomorrow"
        completed.response = "I will come tomorrow."
        completed.updatedAt = now.addingTimeInterval(5)
        completed.expiresAt = now.addingTimeInterval(185)
        XCTAssertTrue(store.writeSnapshot(completed), "durable handoff must succeed before the owner releases background time")

        let nextProcessStore = VoiceAssistantStore(rootURL: root)
        XCTAssertEqual(nextProcessStore.readSnapshot(now: now.addingTimeInterval(6)), completed)
        XCTAssertNil(nextProcessStore.readSnapshot(now: now.addingTimeInterval(185)))
        XCTAssertTrue(try root.resourceValues(forKeys: [.isExcludedFromBackupKey]).isExcludedFromBackup == true)
    }

    func testAtomicSnapshotReplacesEarlierCompleteProtectionWithUnlessOpenWhenMetadataIsExposed() throws {
        let snapshot = VoiceAssistantSnapshot(sessionID: "session", phase: .responseReady,
            response: "Completed result", updatedAt: now, expiresAt: now.addingTimeInterval(180))
        XCTAssertTrue(store.writeSnapshot(snapshot))
        let file = root.appendingPathComponent("session.json")
        // A snapshot from the preceding build used Class A. The next atomic
        // snapshot must apply the new class, including to an existing path.
        try JSONEncoder().encode(snapshot).write(to: file, options: [.atomic, .completeFileProtection])
        XCTAssertTrue(store.writeSnapshot(snapshot))
        let attributes = try FileManager.default.attributesOfItem(atPath: file.path)
        let protection = (attributes[.protectionKey] as? FileProtectionType)?.rawValue
            ?? attributes[.protectionKey] as? String
        guard let protection else {
            throw XCTSkip("This runtime does not expose Data Protection metadata; physical lock behavior is not tested here.")
        }
        XCTAssertEqual(protection, FileProtectionType.completeUnlessOpen.rawValue)
    }
}
