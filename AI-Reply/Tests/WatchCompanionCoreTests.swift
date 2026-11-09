import XCTest
@testable import AIReply

/// What the iPhone does with a message from the watch, and the path from
/// that message to a keyboard.
final class WatchCompanionCoreTests: XCTestCase {

    private var root: URL!
    private var store: WatchVoiceStore!
    private var notified = 0
    private var core: WatchCompanionCore!
    private let now = Date()

    override func setUpWithError() throws {
        root = FileManager.default.temporaryDirectory.appendingPathComponent("watch-core-\(UUID().uuidString)", isDirectory: true)
        store = WatchVoiceStore(rootURL: root)
        notified = 0
        let bump: @Sendable () -> Void = { [weak self] in self?.notified += 1 }
        core = WatchCompanionCore(store: store,
                                  snapshot: { _ in WatchPhoneSnapshot(isSignedIn: true) },
                                  notifyKeyboard: bump)
    }

    override func tearDownWithError() throws {
        try? FileManager.default.removeItem(at: root)
    }

    private func command(id: String = UUID().uuidString, session: String? = nil, createdAt: Date? = nil) -> WatchVoiceEnvelope {
        .command(requestID: id, instruction: "Скажи, что занят", language: .russian, options: WatchVoiceOptions(),
                 keyboardSessionID: session, createdAt: createdAt ?? now)
    }

    private func liveKeyboard() -> WatchKeyboardSession {
        let session = WatchKeyboardSession(id: UUID().uuidString, documentID: nil, startedAt: now, updatedAt: now,
                                           isActive: true, isWaiting: false, hasFullAccess: true)
        store.writeSession(session)
        return session
    }

    // MARK: Receiving

    func testACommandIsQueuedAndTheKeyboardToldOnce() {
        let envelope = command()
        let answer = core.handle(envelope, now: now)
        XCTAssertEqual(answer.type, .statusUpdate)
        XCTAssertEqual(answer.requestID, envelope.requestID)
        XCTAssertEqual(answer.deliveryStatus, .queued)
        XCTAssertEqual(answer.source, .phone)
        XCTAssertEqual(notified, 1)
        XCTAssertEqual(store.pendingCommands(now: now).map(\.id), [envelope.requestID])
    }

    func testTheAnswerSaysWhetherAKeyboardIsOnScreen() {
        XCTAssertEqual(core.handle(command(), now: now).phone, WatchPhoneSnapshot(isSignedIn: true))
    }

    func testARepeatedDeliveryIsReportedNotRedone() {
        let envelope = command()
        _ = core.handle(envelope, now: now)
        let answer = core.handle(envelope, now: now)
        XCTAssertEqual(answer.deliveryStatus, .queued)
        XCTAssertEqual(notified, 1, "the keyboard is not told twice")
        XCTAssertEqual(store.pendingCommands(now: now).count, 1)
    }

    func testARepeatAfterTheOutcomeTellsTheOutcome() {
        let envelope = command()
        _ = core.handle(envelope, now: now)
        store.write(WatchVoiceOutcome(id: envelope.requestID, status: .inserted, updatedAt: now, expiresAt: now.addingTimeInterval(600)))
        XCTAssertEqual(core.handle(envelope, now: now).deliveryStatus, .inserted)
    }

    func testABadCommandNeverReachesTheQueue() {
        var envelope = command()
        envelope.version = 99
        XCTAssertEqual(core.handle(envelope, now: now).failure, .unsupportedVersion)
        XCTAssertEqual(store.pendingCommands(now: now), [])
        XCTAssertEqual(notified, 0)
    }

    func testAnInstructionDeliveredTooLateIsExpired() {
        let envelope = command(createdAt: now.addingTimeInterval(-WatchVoice.commandLifetime - 1))
        let answer = core.handle(envelope, now: now)
        XCTAssertEqual(answer.deliveryStatus, .failed)
        XCTAssertEqual(answer.failure, .expired)
        XCTAssertEqual(store.pendingCommands(now: now), [])
    }

    func testAMalformedIdentifierIsNotEchoed() {
        let answer = core.handle(command(id: "../../evil"), now: now)
        XCTAssertEqual(answer.failure, .invalidRequest)
        XCTAssertTrue(WatchVoiceIdentifier.isValid(answer.requestID))
    }

    func testAFullQueueIsSaid() {
        for _ in 0..<WatchVoiceStore.maximumInbox { _ = core.handle(command(), now: now) }
        XCTAssertEqual(core.handle(command(), now: now).failure, .storageUnavailable)
    }

    // MARK: Which keyboard

    func testTheWatchsSessionIsTheTarget() throws {
        _ = liveKeyboard()
        let seen = UUID().uuidString
        _ = core.handle(command(session: seen), now: now)
        XCTAssertEqual(store.pendingCommands(now: now).first?.targetSessionID, seen)
    }

    func testWithoutOneTheKeyboardOnScreenIs() {
        let live = liveKeyboard()
        _ = core.handle(command(), now: now)
        XCTAssertEqual(store.pendingCommands(now: now).first?.targetSessionID, live.id)
    }

    func testAKeyboardThatLeftIsNotATarget() {
        var gone = liveKeyboard()
        gone.isActive = false
        store.writeSession(gone)
        _ = core.handle(command(), now: now)
        XCTAssertNil(store.pendingCommands(now: now).first?.targetSessionID)
    }

    func testNoKeyboardMeansNoTarget() {
        _ = core.handle(command(), now: now)
        XCTAssertNil(store.pendingCommands(now: now).first?.targetSessionID)
    }

    // MARK: Cancel

    func testCancelTakesItBackAndTellsTheKeyboard() {
        let envelope = command()
        _ = core.handle(envelope, now: now)
        let answer = core.handle(.request(.cancel, requestID: envelope.requestID, createdAt: now), now: now)
        XCTAssertEqual(answer.deliveryStatus, .cancelled)
        XCTAssertEqual(notified, 2)
        XCTAssertEqual(store.pendingCommands(now: now), [])
    }

    // MARK: Status

    func testStatusOfAQueuedCommand() {
        let envelope = command()
        _ = core.handle(envelope, now: now)
        XCTAssertEqual(core.handle(.request(.statusRequest, requestID: envelope.requestID, createdAt: now), now: now).deliveryStatus, .queued)
    }

    func testStatusCarriesTheFailure() {
        let id = UUID().uuidString
        store.write(WatchVoiceOutcome(id: id, status: .failed, updatedAt: now, expiresAt: now.addingTimeInterval(600),
                                      failure: .quotaExhausted))
        let answer = core.handle(.request(.statusRequest, requestID: id, createdAt: now), now: now)
        XCTAssertEqual(answer.deliveryStatus, .failed)
        XCTAssertEqual(answer.failure, .quotaExhausted)
    }

    func testStatusOfSomethingUnknownIsNothing() {
        let answer = core.handle(.request(.statusRequest, createdAt: now), now: now)
        XCTAssertNil(answer.deliveryStatus)
        XCTAssertNotNil(answer.phone, "still useful: it says what the keyboard is doing")
    }

    func testTheResultIsOnlyGivenWhileItIsWaiting() {
        let id = UUID().uuidString
        store.write(WatchVoiceOutcome(id: id, status: .ready, updatedAt: now, expiresAt: now.addingTimeInterval(600), text: "Ответ"))
        XCTAssertEqual(core.handle(.request(.resultRequest, requestID: id, createdAt: now), now: now).result, "Ответ")

        store.write(WatchVoiceOutcome(id: id, status: .inserted, updatedAt: now, expiresAt: now.addingTimeInterval(600)))
        XCTAssertNil(core.handle(.request(.resultRequest, requestID: id, createdAt: now), now: now).result)
    }

    func testAStatusRequestDoesNotTouchTheQueue() {
        _ = core.handle(.request(.statusRequest, createdAt: now), now: now)
        XCTAssertEqual(notified, 0)
    }

    func testPingIsAnswered() {
        let ping = WatchVoiceEnvelope.request(.ping, source: .phone, createdAt: now)
        let answer = core.handle(ping, now: now)
        XCTAssertEqual(answer.type, .pong)
        XCTAssertEqual(answer.requestID, ping.requestID)
    }

    // MARK: What the watch is told

    func testThePhonesStateNamesNoSecrets() throws {
        let session = liveKeyboard()
        let snapshot = WatchPhoneSnapshot.current(store: store, now: now)
        XCTAssertEqual(snapshot.keyboardSessionID, session.id)
        XCTAssertEqual(snapshot.keyboardHasFullAccess, true)
        XCTAssertFalse(snapshot.templates.isEmpty)

        let json = String(decoding: try WatchVoiceCodec.data(snapshot), as: UTF8.self).lowercased()
        for secret in ["token", "instructions", "password"] {
            XCTAssertFalse(json.contains(secret), secret)
        }
    }

    func testAKeyboardOffScreenIsNotReported() {
        var gone = liveKeyboard()
        gone.isActive = false
        store.writeSession(gone)
        let snapshot = WatchPhoneSnapshot.current(store: store, now: now)
        XCTAssertNil(snapshot.keyboardSessionID)
        XCTAssertNil(snapshot.keyboardHasFullAccess)
    }

    // MARK: Watch, phone, keyboard

    /// The whole relay with no WatchConnectivity: over the wire format, into
    /// the store, out to a keyboard's reading of it.
    func testACommandTravelsFromTheWatchToTheKeyboard() throws {
        let live = liveKeyboard()
        let sent = command(session: live.id)
        let wire = try WatchVoiceCodec.dictionary(sent)

        let received = try XCTUnwrap(WatchVoiceCodec.envelope(from: wire))
        let answer = core.handle(received, now: now)
        XCTAssertEqual(try XCTUnwrap(WatchVoiceCodec.envelope(from: try WatchVoiceCodec.dictionary(answer))).deliveryStatus, .queued)

        let keyboard = WatchVoiceStore(rootURL: root)
        let pending = keyboard.pendingCommands(now: now)
        XCTAssertEqual(pending.map(\.id), [sent.requestID])
        XCTAssertEqual(pending.first?.targetSessionID, live.id)
        XCTAssertTrue(keyboard.claim(sent.requestID, now: now))

        keyboard.write(WatchVoiceOutcome(id: sent.requestID, status: .inserted, updatedAt: now, expiresAt: now.addingTimeInterval(600)))
        XCTAssertEqual(core.handle(.request(.statusRequest, requestID: sent.requestID, createdAt: now), now: now).deliveryStatus, .inserted)
    }

    /// The link dropped after the phone took the command: the watch asks
    /// again and is told where it stands, with nothing done twice.
    func testAfterALostConnectionTheWatchFindsOutWhatHappened() {
        let envelope = command()
        _ = core.handle(envelope, now: now)
        XCTAssertTrue(store.claim(envelope.requestID, now: now))
        store.write(WatchVoiceOutcome(id: envelope.requestID, status: .generating, updatedAt: now, expiresAt: now.addingTimeInterval(600)))

        // The acknowledgement was lost, so the watch queued it as well.
        XCTAssertEqual(core.handle(envelope, now: now).deliveryStatus, .generating)
        XCTAssertEqual(notified, 1)
    }
}
