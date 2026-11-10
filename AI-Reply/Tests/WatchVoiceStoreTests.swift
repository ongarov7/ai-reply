import XCTest
@testable import AIReply

/// The App Group queue: what the app writes the keyboard reads, once.
final class WatchVoiceStoreTests: XCTestCase {

    private var root: URL!
    private var store: WatchVoiceStore!
    // Whole seconds survive the millisecond JSON codec exactly. Keep the
    // fixture near file creation times for the store's orphan/marker checks.
    private let now = Date(timeIntervalSince1970: Date().timeIntervalSince1970.rounded(.down))

    override func setUpWithError() throws {
        root = FileManager.default.temporaryDirectory.appendingPathComponent("watch-store-\(UUID().uuidString)", isDirectory: true)
        store = WatchVoiceStore(rootURL: root)
    }

    override func tearDownWithError() throws {
        try? FileManager.default.removeItem(at: root)
    }

    private func command(id: String = UUID().uuidString, createdAt: Date? = nil, target: String? = nil) -> WatchVoiceCommand {
        WatchVoiceCommand(
            envelope: .command(requestID: id, instruction: "  Скажи, что занят  ", language: .russian,
                               options: WatchVoiceOptions(), keyboardSessionID: nil, createdAt: createdAt ?? now),
            targetSessionID: target
        )!
    }

    private func outcome(_ id: String, _ status: WatchVoiceDeliveryStatus, updatedAt: Date? = nil,
                         text: String? = nil) -> WatchVoiceOutcome {
        let stamp = updatedAt ?? now
        return WatchVoiceOutcome(id: id, status: status, updatedAt: stamp,
                                 expiresAt: stamp.addingTimeInterval(WatchVoice.resultLifetime), text: text)
    }

    // MARK: Transfer

    func testWhatTheAppWritesTheKeyboardReads() {
        let original = command(target: "session-123")
        XCTAssertEqual(store.enqueue(original), .queued)
        let pending = store.pendingCommands(now: now)
        XCTAssertEqual(pending, [original])
        XCTAssertEqual(pending.first?.instruction, "Скажи, что занят", "trimmed on the way in")
        XCTAssertEqual(pending.first?.targetSessionID, "session-123")
    }

    func testOldestFirst() {
        let later = command(createdAt: now.addingTimeInterval(10))
        let earlier = command(createdAt: now)
        XCTAssertEqual(store.enqueue(later), .queued)
        XCTAssertEqual(store.enqueue(earlier), .queued)
        XCTAssertEqual(store.pendingCommands(now: now.addingTimeInterval(20)).map(\.id), [earlier.id, later.id])
    }

    func testWithoutTheAppGroupNothingCrashes() {
        let missing = WatchVoiceStore(rootURL: nil)
        XCTAssertEqual(missing.enqueue(command()), .unavailable)
        XCTAssertEqual(missing.pendingCommands(now: now), [])
        XCTAssertFalse(missing.claim(UUID().uuidString, now: now))
        XCTAssertNil(missing.readLink())
        missing.purge(now: now)
    }

    // MARK: Duplicates

    func testASecondDeliveryIsADuplicate() {
        let id = UUID().uuidString
        XCTAssertEqual(store.enqueue(command(id: id)), .queued)
        XCTAssertEqual(store.enqueue(command(id: id)), .duplicate)
        XCTAssertEqual(store.pendingCommands(now: now).count, 1)
    }

    func testADeliveryAfterTheKeyboardFinishedIsStillADuplicate() {
        let id = UUID().uuidString
        XCTAssertEqual(store.enqueue(command(id: id)), .queued)
        XCTAssertTrue(store.claim(id, now: now))
        store.removeCommand(id)
        XCTAssertEqual(store.enqueue(command(id: id)), .duplicate, "the claim outlives the command")
        XCTAssertEqual(store.pendingCommands(now: now), [])
    }

    func testOnlyOneProcessEverClaimsACommand() {
        let id = UUID().uuidString
        let store = self.store!
        let now = self.now
        let lock = NSLock()
        var wins = 0
        DispatchQueue.concurrentPerform(iterations: 64) { _ in
            if store.claim(id, now: now) {
                lock.lock()
                wins += 1
                lock.unlock()
            }
        }
        XCTAssertEqual(wins, 1)
    }

    func testOnlyOneDeliveryIsEverQueuedWhenTheyRace() {
        let id = UUID().uuidString
        let store = self.store!
        let original = command(id: id)
        let lock = NSLock()
        var queued = 0
        DispatchQueue.concurrentPerform(iterations: 32) { _ in
            if store.enqueue(original) == .queued {
                lock.lock()
                queued += 1
                lock.unlock()
            }
        }
        XCTAssertEqual(queued, 1)
    }

    func testTextIsInsertedOnlyOnce() {
        let id = UUID().uuidString
        XCTAssertTrue(store.create(.inserted, for: id))
        XCTAssertFalse(store.create(.inserted, for: id))
        XCTAssertTrue(store.has(.inserted, for: id))
    }

    func testAClaimedCommandIsNoLongerPending() {
        let original = command()
        XCTAssertEqual(store.enqueue(original), .queued)
        XCTAssertTrue(store.claim(original.id, now: now))
        XCTAssertEqual(store.pendingCommands(now: now), [])
    }

    func testAReleasedCommandCanBeStartedAgain() {
        let original = command()
        XCTAssertEqual(store.enqueue(original), .queued)
        XCTAssertTrue(store.claim(original.id, now: now))
        store.release(original.id)
        XCTAssertEqual(store.pendingCommands(now: now), [original], "back in the queue")
        XCTAssertTrue(store.claim(original.id, now: now))
    }

    // MARK: Files the lock keeps closed

    /// While the phone is locked the files cannot be opened. That is not
    /// damage: they stay until they could no longer be of any use.
    func testAFileThatCannotBeOpenedYetIsKept() {
        let original = command()
        XCTAssertEqual(store.enqueue(original), .queued)
        let id = UUID().uuidString
        store.write(outcome(id, .ready, text: "Ответ"))
        let inboxFile = root.appendingPathComponent("inbox/\(original.id).json")
        let outcomeFile = root.appendingPathComponent("outcomes/\(id).json")
        XCTAssertEqual(chmod(inboxFile.path, 0), 0)
        XCTAssertEqual(chmod(outcomeFile.path, 0), 0)

        XCTAssertNil(store.command(id: original.id))
        XCTAssertTrue(store.hasCommand(original.id), "known without opening it")
        store.purge(now: now)
        XCTAssertTrue(FileManager.default.fileExists(atPath: inboxFile.path))
        XCTAssertTrue(FileManager.default.fileExists(atPath: outcomeFile.path))

        store.purge(now: now.addingTimeInterval(WatchVoice.resultLifetime + WatchVoice.clockTolerance + 5))
        XCTAssertFalse(FileManager.default.fileExists(atPath: inboxFile.path))
        XCTAssertFalse(FileManager.default.fileExists(atPath: outcomeFile.path))
    }

    func testABrokenFileIsRemoved() throws {
        XCTAssertEqual(store.enqueue(command()), .queued)
        store.write(outcome(UUID().uuidString, .ready, text: "Ответ"))
        let brokenCommand = root.appendingPathComponent("inbox/\(UUID().uuidString).json")
        let brokenOutcome = root.appendingPathComponent("outcomes/\(UUID().uuidString).json")
        try Data("not json".utf8).write(to: brokenCommand)
        try Data("not json".utf8).write(to: brokenOutcome)

        store.purge(now: now)
        XCTAssertFalse(FileManager.default.fileExists(atPath: brokenCommand.path))
        XCTAssertFalse(FileManager.default.fileExists(atPath: brokenOutcome.path))
        XCTAssertEqual(store.pendingCommands(now: now).count, 1, "the good one stays")
    }

    // MARK: A keyboard that died

    func testACommandLeftBehindCanBeTakenOver() {
        let original = command()
        XCTAssertEqual(store.enqueue(original), .queued)
        XCTAssertTrue(store.claim(original.id, now: now))
        store.write(outcome(original.id, .generating))

        let soon = now.addingTimeInterval(10)
        XCTAssertEqual(store.pendingCommands(now: soon), [], "its keyboard may still be working")
        XCTAssertFalse(store.claim(original.id, now: soon))

        let later = now.addingTimeInterval(WatchVoiceStore.orphanAfter + 1)
        XCTAssertEqual(store.pendingCommands(now: later).map(\.id), [original.id])
        XCTAssertTrue(store.claim(original.id, now: later))
    }

    func testAFinishedCommandIsNeverTakenOver() {
        let original = command()
        XCTAssertEqual(store.enqueue(original), .queued)
        XCTAssertTrue(store.claim(original.id, now: now))
        store.write(outcome(original.id, .ready, text: "Ответ"))
        let later = now.addingTimeInterval(WatchVoiceStore.orphanAfter + 1)
        XCTAssertFalse(store.claim(original.id, now: later), "a saved reply is not to be paid for twice")
    }

    // MARK: Cancel and expiry

    func testACancelledCommandNeverStarts() {
        let original = command()
        XCTAssertEqual(store.enqueue(original), .queued)
        store.cancel(original.id, now: now)
        XCTAssertEqual(store.pendingCommands(now: now), [])
        XCTAssertEqual(store.enqueue(original), .duplicate, "a late delivery does not bring it back")
    }

    func testAnExpiredCommandIsDeletedAndReported() {
        let original = command()
        XCTAssertEqual(store.enqueue(original), .queued)
        let later = now.addingTimeInterval(WatchVoice.commandLifetime + 1)
        XCTAssertEqual(store.pendingCommands(now: later), [])
        store.purge(now: later)
        XCTAssertNil(store.command(id: original.id), "the instruction is gone from disk")
        XCTAssertEqual(store.outcome(for: original.id)?.status, .expired)
    }

    func testASavedReplyExpires() {
        let id = UUID().uuidString
        store.write(outcome(id, .ready, text: "Ответ"))
        XCTAssertEqual(store.savedResults(now: now).map(\.id), [id])
        let later = now.addingTimeInterval(WatchVoice.resultLifetime + 1)
        XCTAssertEqual(store.savedResults(now: later), [])
        store.purge(now: later)
        XCTAssertNil(store.outcome(for: id), "the text is gone from disk")
    }

    func testOnlyReadyRepliesAreOffered() {
        store.write(outcome(UUID().uuidString, .inserted))
        store.write(outcome(UUID().uuidString, .failed))
        let ready = UUID().uuidString
        store.write(outcome(ready, .ready, text: "Ответ"))
        XCTAssertEqual(store.savedResults(now: now).map(\.id), [ready])
    }

    func testTheQueueIsBounded() {
        for _ in 0..<WatchVoiceStore.maximumInbox {
            XCTAssertEqual(store.enqueue(command()), .queued)
        }
        XCTAssertEqual(store.enqueue(command()), .queueFull)
    }

    // MARK: Protection

    func testTheFolderIsLeftOutOfBackups() throws {
        XCTAssertEqual(store.enqueue(command()), .queued)
        let values = try root.resourceValues(forKeys: [.isExcludedFromBackupKey])
        XCTAssertEqual(values.isExcludedFromBackup, true)
    }

    // MARK: Published state

    func testSessionAndLinkRoundTrip() {
        let session = WatchKeyboardSession(id: UUID().uuidString, documentID: nil, startedAt: now, updatedAt: now,
                                           isActive: true, isWaiting: false, hasFullAccess: true)
        store.writeSession(session)
        XCTAssertEqual(store.readSession(), session)
        let link = WatchLinkSnapshot(isSupported: true, isActivated: true, isPaired: true, updatedAt: now)
        store.writeLink(link)
        XCTAssertEqual(store.readLink(), link)
    }

    func testAKilledKeyboardStopsCountingAsLive() {
        let session = WatchKeyboardSession(id: UUID().uuidString, documentID: nil, startedAt: now, updatedAt: now,
                                           isActive: true, isWaiting: false, hasFullAccess: true)
        XCTAssertTrue(session.isLive(now: now.addingTimeInterval(60)))
        XCTAssertFalse(session.isLive(now: now.addingTimeInterval(WatchKeyboardSession.maximumSilence + 1)))
    }
}
