import XCTest
@testable import AIReply

/// Watch Voice in the keyboard, from a command in the App Group to text in
/// the field, with a stand-in for everything UIKit.
@MainActor
final class WatchVoiceCoordinatorTests: XCTestCase {

    private final class Host: WatchVoiceHost {
        var watchHasFullAccess = true
        var watchComposerState: WatchComposerState = .idle
        var document: UUID? = UUID()
        var before = ""
        var after = ""
        var copied: Result<ReplyContext, AIReplyError> = .failure(.noSourceMessage)
        var existing: String?
        var selection = ""
        var pasteboardChanges = 0
        var draft: String?

        private(set) var reads = 0
        private(set) var started: [(plan: WatchGenerationPlan, id: String)] = []
        private(set) var typedMessageOpens: [String] = []
        private(set) var inserted: [String] = []
        private(set) var shown: [WatchVoiceOutcome] = []
        private(set) var cancels = 0
        private(set) var discards = 0
        private(set) var ui = WatchVoiceUI()

        func watchInsertContext(sessionID: String) -> WatchInsertContext {
            WatchInsertContext(sessionID: sessionID, documentID: document, textBeforeCaret: before, textAfterCaret: after,
                               selectedText: selection)
        }
        var watchPasteboardChangeCount: Int { pasteboardChanges }
        func watchCurrentDraft() -> String? { draft }
        func watchReadCopiedMessage() -> Result<ReplyContext, AIReplyError> {
            reads += 1
            return copied
        }
        func watchExistingText() -> String? { existing }
        func watchStart(_ plan: WatchGenerationPlan, command: WatchVoiceCommand) { started.append((plan, command.id)) }
        func watchOpenReplyForTypedMessage(instruction: String, command: WatchVoiceCommand) { typedMessageOpens.append(instruction) }
        func watchCancelGeneration() { cancels += 1 }
        func watchInsert(_ text: String) { inserted.append(text) }
        func watchShow(_ saved: WatchVoiceOutcome) { shown.append(saved) }
        func watchDiscardComposer() { discards += 1 }
        func watchUIDidChange(_ ui: WatchVoiceUI) { self.ui = ui }
    }

    private var root: URL!
    private var store: WatchVoiceStore!
    private var defaults: UserDefaults!
    private var suite: String!
    private var host: Host!
    private var coordinator: WatchVoiceCoordinator!
    private let now = Date()
    private let message = ReplyContext(text: "Можем встретиться завтра?", source: .clipboard)

    override func setUp() async throws {
        root = FileManager.default.temporaryDirectory.appendingPathComponent("watch-coordinator-\(UUID().uuidString)", isDirectory: true)
        store = WatchVoiceStore(rootURL: root)
        suite = "watch.coordinator.\(UUID().uuidString)"
        defaults = UserDefaults(suiteName: suite)
        host = Host()
        let fixedNow = now
        coordinator = WatchVoiceCoordinator(
            store: store,
            settings: SharedSettings(defaults: defaults),
            limits: { WatchVoiceRouter.Limits(replyInstruction: 280, composeInstruction: 400) },
            now: { fixedNow },
            post: { _ in },
            observe: { _ in nil },
            sleep: { _ in try? await Task.sleep(for: .milliseconds(5)) }
        )
        coordinator.host = host
        pairWatch()
    }

    override func tearDown() async throws {
        coordinator.keyboardWillDisappear()
        try? FileManager.default.removeItem(at: root)
        defaults.removePersistentDomain(forName: suite)
    }

    private func pairWatch(contactAgo: TimeInterval? = 5) {
        store.writeLink(WatchLinkSnapshot(isSupported: true, isActivated: true, isPaired: true, isWatchAppInstalled: true,
                                          isReachable: true, updatedAt: now,
                                          lastWatchContactAt: contactAgo.map { now.addingTimeInterval(-$0) }))
    }

    @discardableResult
    private func send(target: String?, mode: WatchVoiceMode = .reply, id: String = UUID().uuidString,
                      instruction: String = "Скажи, что занят", previewID: String? = nil) -> String {
        let command = WatchVoiceCommand(
            envelope: .command(requestID: id, instruction: instruction, language: .russian,
                               options: WatchVoiceOptions(mode: mode), keyboardSessionID: nil,
                               previewID: previewID, createdAt: now),
            targetSessionID: target
        )!
        XCTAssertEqual(store.enqueue(command), .queued)
        return id
    }

    private var sessionID: String { coordinator.session?.id ?? "" }

    private func status(_ id: String) -> WatchVoiceDeliveryStatus? { store.outcome(for: id)?.status }

    // MARK: TEST 1: active keyboard, instruction arrives, text inserted

    func testAnInstructionForTheOpenKeyboardEndsUpInTheField() async {
        host.copied = .success(message)
        await coordinator.keyboardDidAppear()
        let id = send(target: sessionID)
        await coordinator.reconcile()

        XCTAssertEqual(host.started.count, 1)
        guard case .reply(let plan)? = host.started.first?.plan else { return XCTFail("not a reply") }
        XCTAssertEqual(plan.source, message.text)
        XCTAssertEqual(plan.instruction, "Скажи, что занят")
        XCTAssertEqual(status(id), .generating)
        XCTAssertEqual(host.ui.indicator, .generating)

        coordinator.generationDidFinish(.success("Сегодня занят, давайте в среду."))
        XCTAssertEqual(host.inserted, ["Сегодня занят, давайте в среду."])
        XCTAssertEqual(status(id), .inserted)
        XCTAssertNil(store.outcome(for: id)?.text, "nothing of it stays on disk")
        XCTAssertNil(store.command(id: id), "nor the instruction")
    }

    func testTheCopiedMessageIsReadOnceForACommand() async {
        host.copied = .success(message)
        await coordinator.keyboardDidAppear()
        send(target: sessionID)
        await coordinator.reconcile()
        await coordinator.reconcile()
        XCTAssertEqual(host.reads, 1)
    }

    // MARK: TEST 2: arrives with the keyboard closed

    func testAnInstructionWithNoKeyboardDoesNothingByItself() async {
        await coordinator.keyboardDidAppear()
        let id = send(target: nil)
        await coordinator.reconcile()

        XCTAssertEqual(host.started.count, 0, "no reply spent and no clipboard read without a tap")
        XCTAssertEqual(host.reads, 0)
        XCTAssertEqual(host.ui.indicator, .instructionReceived)
        XCTAssertEqual(host.ui.strip, .instructionReceived(preview: "Скажи, что занят"))
        XCTAssertNil(status(id))

        host.copied = .success(message)
        coordinator.stripPrimaryTapped()
        XCTAssertEqual(host.started.count, 1)
        coordinator.generationDidFinish(.success("Ответ"))

        XCTAssertEqual(host.inserted, [], "never by itself: it was not asked for in this field")
        XCTAssertEqual(status(id), .ready)
    }

    func testAReplyForAnotherFieldIsSavedForInsertResponse() async {
        host.copied = .success(message)
        await coordinator.keyboardDidAppear()
        let id = send(target: sessionID)
        await coordinator.reconcile()

        host.document = UUID()
        coordinator.hostFieldDidChange()
        coordinator.generationDidFinish(.success("Ответ"))
        coordinator.keyboardWillDisappear()

        XCTAssertEqual(host.inserted, [])
        XCTAssertEqual(host.discards, 1)
        XCTAssertEqual(store.outcome(for: id)?.text, "Ответ")
        XCTAssertEqual(status(id), .ready)

        await coordinator.keyboardDidAppear()
        XCTAssertEqual(host.ui.strip, .responseReady(preview: "Ответ"))
        XCTAssertEqual(host.ui.indicator, .responseReady)

        coordinator.stripPrimaryTapped()
        XCTAssertEqual(host.inserted, ["Ответ"])
        XCTAssertEqual(status(id), .inserted)
        XCTAssertNil(store.outcome(for: id)?.text, "gone once it is used")
        XCTAssertNil(host.ui.strip)
    }

    func testLeavingDuringARequestPutsTheInstructionBack() async {
        host.copied = .success(message)
        await coordinator.keyboardDidAppear()
        let id = send(target: sessionID)
        await coordinator.reconcile()
        XCTAssertEqual(host.started.count, 1)

        coordinator.keyboardWillDisappear()
        XCTAssertFalse(coordinator.hasGenerationInFlight, "no request outlives the keyboard")
        XCTAssertEqual(host.cancels, 1)
        XCTAssertEqual(host.discards, 1)
        XCTAssertEqual(status(id), .queued)
        XCTAssertNotNil(store.command(id: id), "the instruction is still there")

        // The cancelled request ending late changes nothing.
        coordinator.generationDidFinish(.success("Ответ"))
        XCTAssertEqual(host.inserted, [])
        XCTAssertEqual(status(id), .queued)

        // Back in a new keyboard: offered, not started by itself.
        await coordinator.keyboardDidAppear()
        await coordinator.reconcile()
        XCTAssertEqual(host.started.count, 1)
        XCTAssertEqual(host.ui.indicator, .instructionReceived)

        coordinator.stripPrimaryTapped()
        await coordinator.reconcile()
        XCTAssertEqual(host.started.map(\.id), [id, id], "the same command, and so the same key")
        XCTAssertEqual(status(id), .generating)
    }

    func testAnEditedAnswerIsWhatIsKept() async {
        host.copied = .success(message)
        host.before = "Уже написано"
        await coordinator.keyboardDidAppear()
        let id = send(target: sessionID)
        await coordinator.reconcile()
        coordinator.generationDidFinish(.success("Ответ"))
        XCTAssertEqual(status(id), .ready, "a field with text is not written over")

        host.draft = "Ответ, исправленный вручную"
        coordinator.keyboardWillDisappear()
        XCTAssertEqual(store.outcome(for: id)?.text, "Ответ, исправленный вручную")
    }

    func testAnOtherComposerClosesTheCommand() async {
        host.copied = .success(message)
        await coordinator.keyboardDidAppear()
        let id = send(target: sessionID)
        await coordinator.reconcile()

        coordinator.composerReplaced()
        XCTAssertFalse(coordinator.hasGenerationInFlight)
        XCTAssertEqual(status(id), .cancelled)
        XCTAssertNil(store.command(id: id))
        XCTAssertNil(host.ui.strip)
    }

    func testAnInstructionDeliveredLateWaitsForATap() async {
        host.copied = .success(message)
        await coordinator.keyboardDidAppear()
        let command = WatchVoiceCommand(
            envelope: .command(requestID: UUID().uuidString, instruction: "Скажи, что занят", language: .russian,
                               options: WatchVoiceOptions(mode: .reply), keyboardSessionID: nil,
                               createdAt: now.addingTimeInterval(-(WatchVoice.liveCommandWindow + 5))),
            targetSessionID: sessionID
        )!
        XCTAssertEqual(store.enqueue(command), .queued)
        await coordinator.reconcile()
        XCTAssertEqual(host.started.count, 0, "confirmed too long ago for this field")
        XCTAssertEqual(host.reads, 0)
        XCTAssertEqual(host.ui.indicator, .instructionReceived)
    }

    func testASavedReplyDoesNotGoOverWhatIsInTheField() async {
        let id = UUID().uuidString
        store.write(WatchVoiceOutcome(id: id, status: .ready, updatedAt: now, expiresAt: now.addingTimeInterval(600),
                                      text: "Ответ", instruction: "Скажи", kind: .reply))
        host.before = "Уже написано"
        await coordinator.keyboardDidAppear()
        coordinator.stripPrimaryTapped()
        XCTAssertEqual(host.inserted, [])
        XCTAssertEqual(host.shown.map(\.id), [id], "the composer asks Replace, Add or Cancel")
    }

    func testASavedReplyCanBeDismissed() async {
        let id = UUID().uuidString
        store.write(WatchVoiceOutcome(id: id, status: .ready, updatedAt: now, expiresAt: now.addingTimeInterval(600), text: "Ответ"))
        await coordinator.keyboardDidAppear()
        coordinator.stripDismissTapped()
        XCTAssertEqual(status(id), .dismissed)
        XCTAssertNil(store.outcome(for: id)?.text)
        XCTAssertNil(host.ui.strip)
    }

    // MARK: TEST 3: another chat or field

    func testAnotherFieldGetsNothingByItself() async {
        host.copied = .success(message)
        await coordinator.keyboardDidAppear()
        let id = send(target: sessionID)
        await coordinator.reconcile()

        host.document = UUID()
        coordinator.hostFieldDidChange()
        coordinator.generationDidFinish(.success("Ответ"))

        XCTAssertEqual(host.inserted, [])
        XCTAssertEqual(status(id), .ready)
    }

    func testTheSameFieldWithOtherTextGetsNothingByItself() async {
        host.copied = .success(message)
        await coordinator.keyboardDidAppear()
        send(target: sessionID)
        await coordinator.reconcile()
        host.before = "Текст другого чата"
        coordinator.generationDidFinish(.success("Ответ"))
        XCTAssertEqual(host.inserted, [])
    }

    func testAnInstructionForAnOlderSessionWaitsForATap() async {
        host.copied = .success(message)
        await coordinator.keyboardDidAppear()
        send(target: "AAAAAAAA-0000-0000-0000-000000000000")
        await coordinator.reconcile()
        XCTAssertEqual(host.started.count, 0)
        XCTAssertEqual(host.ui.indicator, .instructionReceived)
    }

    func testTheAutoInsertSwitchIsHonoured() async {
        SharedSettings(defaults: defaults).setWatchAutoInsertEnabled(false)
        host.copied = .success(message)
        await coordinator.keyboardDidAppear()
        let id = send(target: sessionID)
        await coordinator.reconcile()
        coordinator.generationDidFinish(.success("Ответ"))
        XCTAssertEqual(host.inserted, [])
        XCTAssertEqual(status(id), .ready)
        XCTAssertTrue(coordinator.ownsComposer, "the composer shows it, with Insert")

        coordinator.composerDidInsert()
        XCTAssertEqual(status(id), .inserted)
        XCTAssertFalse(coordinator.ownsComposer)
    }

    func testAFieldWithTextIsLeftAlone() async {
        host.before = "Мой черновик"
        host.copied = .success(message)
        await coordinator.keyboardDidAppear()
        send(target: sessionID)
        await coordinator.reconcile()
        coordinator.generationDidFinish(.success("Ответ"))
        XCTAssertEqual(host.inserted, [])
    }

    func testClosingTheComposerDismissesTheResult() async {
        SharedSettings(defaults: defaults).setWatchAutoInsertEnabled(false)
        await coordinator.keyboardDidAppear()
        let id = send(target: sessionID)
        await coordinator.reconcile()
        coordinator.generationDidFinish(.success("Ответ"))
        coordinator.composerDidClose()
        XCTAssertEqual(status(id), .dismissed)
        XCTAssertNil(store.outcome(for: id)?.text)
    }

    // MARK: TEST 5: repeated delivery

    func testARepeatedDeliveryStartsNothingAndInsertsOnce() async {
        host.copied = .success(message)
        await coordinator.keyboardDidAppear()
        let id = send(target: sessionID)
        await coordinator.reconcile()
        await coordinator.reconcile()
        XCTAssertEqual(host.started.count, 1)

        coordinator.generationDidFinish(.success("Ответ"))
        await coordinator.reconcile()

        let again = WatchVoiceCommand(envelope: .command(requestID: id, instruction: "Скажи, что занят", language: .russian,
                                                         options: WatchVoiceOptions(), keyboardSessionID: nil, createdAt: now),
                                      targetSessionID: sessionID)!
        XCTAssertEqual(store.enqueue(again), .duplicate)
        await coordinator.reconcile()
        XCTAssertEqual(host.started.count, 1)
        XCTAssertEqual(host.inserted, ["Ответ"])
    }

    func testASecondKeyboardCannotStartTheSameCommand() async {
        host.copied = .success(message)
        await coordinator.keyboardDidAppear()
        let id = send(target: sessionID)
        XCTAssertTrue(store.claim(id, now: now), "another keyboard process was first")
        await coordinator.reconcile()
        XCTAssertEqual(host.started.count, 0)
    }

    // MARK: TEST 6: cancellation

    func testACancelledInstructionGeneratesNothing() async {
        await coordinator.keyboardDidAppear()
        let id = send(target: sessionID)
        store.cancel(id, now: now)
        await coordinator.reconcile()
        XCTAssertEqual(host.started.count, 0)
    }

    func testCancellingStopsWhatIsRunning() async {
        await coordinator.keyboardDidAppear()
        let id = send(target: sessionID)
        await coordinator.reconcile()
        XCTAssertEqual(host.started.count, 1)
        store.cancel(id, now: now)
        await coordinator.reconcile()
        XCTAssertEqual(host.cancels, 1)
        XCTAssertEqual(status(id), .cancelled)
        coordinator.generationDidFinish(.success("Поздно"))
        XCTAssertEqual(host.inserted, [], "an answer nobody wants any more")
    }

    func testStopInTheComposerIsReported() async {
        await coordinator.keyboardDidAppear()
        let id = send(target: sessionID)
        await coordinator.reconcile()
        coordinator.generationDidFinish(.failure(.cancelled))
        XCTAssertEqual(status(id), .cancelled)
    }

    // MARK: TEST 7: nothing copied

    func testNothingCopiedStillWritesAMessage() async {
        host.copied = .failure(.noSourceMessage)
        await coordinator.keyboardDidAppear()
        send(target: sessionID)
        await coordinator.reconcile()
        guard case .compose(let plan)? = host.started.first?.plan else { return XCTFail("not a new message") }
        XCTAssertEqual(plan.instruction, "Скажи, что занят")
    }

    func testARefusedPasteOpensTheCardToTypeIn() async {
        host.copied = .failure(.pasteDenied)
        await coordinator.keyboardDidAppear()
        let id = send(target: sessionID)
        await coordinator.reconcile()
        XCTAssertEqual(host.started.count, 0)
        XCTAssertEqual(host.typedMessageOpens, ["Скажи, что занят"])
        XCTAssertEqual(store.outcome(for: id)?.failure, .copiedMessageUnreadable)
    }

    // MARK: TEST 8 and 9: failures

    func testAFailedRequestIsReportedAndRecoverable() async {
        await coordinator.keyboardDidAppear()
        let id = send(target: sessionID)
        await coordinator.reconcile()
        coordinator.generationDidFinish(.failure(.offline))
        XCTAssertEqual(status(id), .failed)
        XCTAssertEqual(store.outcome(for: id)?.failure, .offline)
        XCTAssertEqual(store.outcome(for: id)?.failure?.offersRetry, true)
        XCTAssertEqual(host.inserted, [])
    }

    func testASpentQuotaIsReportedAsSuch() async {
        await coordinator.keyboardDidAppear()
        let id = send(target: sessionID)
        await coordinator.reconcile()
        coordinator.generationDidFinish(.failure(.quotaExhausted))
        XCTAssertEqual(store.outcome(for: id)?.failure, .quotaExhausted)
        XCTAssertEqual(store.outcome(for: id)?.failure?.offersRetry, false)
    }

    func testAnInstructionTooLongSaysSoOnTheWatchAndInTheStrip() async {
        await coordinator.keyboardDidAppear()
        host.copied = .success(message)
        let id = send(target: sessionID, instruction: String(repeating: "а", count: 281))
        await coordinator.reconcile()
        XCTAssertEqual(host.started.count, 0)
        XCTAssertEqual(store.outcome(for: id)?.failure, .instructionTooLong)
        XCTAssertEqual(store.outcome(for: id)?.failureLimit, 280)
        XCTAssertEqual(host.ui.strip, .failure(.instructionTooLong))
    }

    // MARK: Full Access and the composer

    func testNoFullAccessStartsNothing() async {
        host.watchHasFullAccess = false
        await coordinator.keyboardDidAppear()
        send(target: "none-of-these")
        await coordinator.reconcile()
        XCTAssertEqual(host.started.count, 0)
        XCTAssertEqual(host.ui.strip, .fullAccessRequired)
        XCTAssertEqual(host.reads, 0)
    }

    func testUnfinishedWorkOnScreenIsNotTakenAway() async {
        host.watchComposerState = .busy
        await coordinator.keyboardDidAppear()
        let id = send(target: sessionID)
        await coordinator.reconcile()
        XCTAssertEqual(host.started.count, 0)
        XCTAssertEqual(host.ui.indicator, .instructionReceived)
        XCTAssertNil(status(id))
    }

    func testAnOpenReplyLendsItsCopiedMessage() async {
        host.watchComposerState = .replyDraft(message)
        await coordinator.keyboardDidAppear()
        send(target: sessionID)
        await coordinator.reconcile()
        XCTAssertEqual(host.reads, 0, "already acquired by the persona tap")
        guard case .reply(let plan)? = host.started.first?.plan else { return XCTFail("not a reply") }
        XCTAssertEqual(plan.source, message.text)
    }

    // MARK: The copied message on the watch

    @discardableResult
    private func ask(_ id: String = UUID().uuidString, at time: Date? = nil) -> String {
        store.writePreviewRequest(WatchPreviewRequest(id: id, sessionID: sessionID, createdAt: time ?? now))
        return id
    }

    func testTheWatchIsShownTheCopiedMessage() async {
        host.copied = .success(message)
        await coordinator.keyboardDidAppear()
        let id = ask()
        await coordinator.reconcile()
        XCTAssertEqual(host.reads, 1, "the tap on the watch is the gesture")
        XCTAssertEqual(store.takePreview(id: id, now: now)?.preview, .ready(message.text))
    }

    func testEachAskIsAnsweredOnceAndACopyIsReadOnce() async {
        host.copied = .success(message)
        await coordinator.keyboardDidAppear()
        let id = ask()
        await coordinator.reconcile()
        await coordinator.reconcile()
        XCTAssertEqual(host.reads, 1)
        XCTAssertNotNil(store.takePreview(id: id, now: now))
        await coordinator.reconcile()
        XCTAssertNil(store.takePreview(id: id, now: now), "an ask already answered is not answered again")

        // The answer was lost and the app asks again: answered from memory.
        ask(id, at: now.addingTimeInterval(1))
        await coordinator.reconcile()
        XCTAssertEqual(host.reads, 1)
        XCTAssertEqual(store.takePreview(id: id, now: now)?.preview.text, message.text)

        // Another persona's screen, the same copy: still one read.
        ask(at: now.addingTimeInterval(2))
        await coordinator.reconcile()
        XCTAssertEqual(host.reads, 1)

        // Something new copied is read.
        host.pasteboardChanges += 1
        ask(at: now.addingTimeInterval(3))
        await coordinator.reconcile()
        XCTAssertEqual(host.reads, 2)
    }

    func testWhatTheWatchShowedIsWhatGetsAnswered() async {
        host.copied = .success(message)
        await coordinator.keyboardDidAppear()
        let shown = ask()
        await coordinator.reconcile()

        send(target: sessionID, previewID: shown)
        await coordinator.reconcile()
        XCTAssertEqual(host.reads, 1, "not read a second time")
        guard case .reply(let plan)? = host.started.first?.plan else { return XCTFail("not a reply") }
        XCTAssertEqual(plan.source, message.text)
    }

    func testACopyMadeAfterThePreviewWaitsForATap() async {
        host.copied = .success(message)
        await coordinator.keyboardDidAppear()
        let shown = ask()
        await coordinator.reconcile()

        let other = ReplyContext(text: "А в пятницу?", source: .clipboard)
        host.copied = .success(other)
        host.pasteboardChanges += 1
        send(target: sessionID, previewID: shown)
        await coordinator.reconcile()
        XCTAssertEqual(host.started.count, 0, "the watch showed another message")
        XCTAssertEqual(host.ui.indicator, .instructionReceived)

        coordinator.stripPrimaryTapped()
        guard case .reply(let plan)? = host.started.first?.plan else { return XCTFail("not a reply") }
        XCTAssertEqual(plan.source, other.text, "the tap reads what is copied now; the composer shows it")
    }

    func testAPreviewFromAnotherFieldIsNotUsed() async {
        host.copied = .success(message)
        await coordinator.keyboardDidAppear()
        let shown = ask()
        await coordinator.reconcile()

        host.document = UUID()
        coordinator.hostFieldDidChange()
        send(target: sessionID, previewID: shown)
        await coordinator.reconcile()
        XCTAssertEqual(host.started.count, 0)
        XCTAssertEqual(host.ui.indicator, .instructionReceived)
    }

    func testAnAskForAnotherKeyboardIsNotAnswered() async {
        host.copied = .success(message)
        await coordinator.keyboardDidAppear()
        let id = UUID().uuidString
        store.writePreviewRequest(WatchPreviewRequest(id: id, sessionID: UUID().uuidString, createdAt: now))
        await coordinator.reconcile()
        XCTAssertEqual(host.reads, 0)
        XCTAssertNil(store.takePreview(id: id, now: now))
    }

    func testTheWatchIsToldWhyThereIsNoMessage() async {
        await coordinator.keyboardDidAppear()
        let nothing = ask()
        await coordinator.reconcile()
        XCTAssertEqual(store.takePreview(id: nothing, now: now)?.preview.state, .nothingCopied)

        host.copied = .failure(.pasteDenied)
        host.pasteboardChanges += 1
        let denied = ask(at: now.addingTimeInterval(1))
        await coordinator.reconcile()
        XCTAssertEqual(store.takePreview(id: denied, now: now)?.preview.state, .pasteDenied)
    }

    func testTheOpenReplysMessageIsTheOneShown() async {
        host.watchComposerState = .replyDraft(message)
        await coordinator.keyboardDidAppear()
        let id = ask()
        await coordinator.reconcile()
        XCTAssertEqual(host.reads, 0, "the persona tap in the keyboard already read it")
        XCTAssertEqual(store.takePreview(id: id, now: now)?.preview.text, message.text)
    }

    func testNothingOfThePreviewOutlivesTheKeyboard() async {
        host.copied = .success(message)
        await coordinator.keyboardDidAppear()
        let shown = ask()
        await coordinator.reconcile()
        coordinator.keyboardWillDisappear()
        await coordinator.keyboardDidAppear()

        send(target: sessionID, previewID: shown)
        await coordinator.reconcile()
        XCTAssertEqual(host.started.count, 0, "a new keyboard does not hold the old preview")
    }

    // MARK: Watch Voice control

    func testANewCopyIsReadAgain() async {
        host.copied = .success(message)
        await coordinator.keyboardDidAppear()
        coordinator.controlTapped()
        XCTAssertEqual(host.reads, 1)

        let other = ReplyContext(text: "А в пятницу?", source: .clipboard)
        host.copied = .success(other)
        host.pasteboardChanges += 1
        send(target: sessionID)
        await coordinator.reconcile()
        XCTAssertEqual(host.reads, 2, "what was read on the tap is not what is copied now")
        guard case .reply(let plan)? = host.started.first?.plan else { return XCTFail("not a reply") }
        XCTAssertEqual(plan.source, other.text)
    }

    func testTheControlReadsTheCopiedMessageOnceOnItsTap() async {
        host.copied = .success(message)
        await coordinator.keyboardDidAppear()
        XCTAssertEqual(host.reads, 0, "appearing reads nothing")
        coordinator.controlTapped()
        XCTAssertEqual(host.reads, 1)
        XCTAssertTrue(coordinator.isWaiting)
        XCTAssertEqual(host.ui.indicator, .waiting)
        XCTAssertEqual(host.ui.strip, .waiting)

        send(target: sessionID)
        await coordinator.reconcile()
        XCTAssertEqual(host.reads, 1, "the tap's read is the one that counts")
        XCTAssertFalse(coordinator.isWaiting)
        XCTAssertEqual(host.started.count, 1)
    }

    func testATapStopsWaiting() async {
        await coordinator.keyboardDidAppear()
        coordinator.controlTapped()
        coordinator.controlTapped()
        XCTAssertFalse(coordinator.isWaiting)
        XCTAssertNil(host.ui.strip)
    }

    func testAMissingWatchAppIsSaidOnTheTap() async {
        store.writeLink(WatchLinkSnapshot(isSupported: true, isActivated: true, isPaired: true, isWatchAppInstalled: false, updatedAt: now))
        await coordinator.keyboardDidAppear()
        XCTAssertEqual(host.ui.indicator, .installApp)
        coordinator.controlTapped()
        XCTAssertFalse(coordinator.isWaiting)
        XCTAssertEqual(host.ui.strip, .installWatchApp)
    }

    func testAClosedWatchAppIsSaid() async {
        pairWatch(contactAgo: nil)
        store.writeLink(WatchLinkSnapshot(isSupported: true, isActivated: true, isPaired: true, isWatchAppInstalled: true, updatedAt: now.addingTimeInterval(-600)))
        await coordinator.keyboardDidAppear()
        XCTAssertEqual(host.ui.indicator, .openWatchApp)
        coordinator.controlTapped()
        XCTAssertEqual(host.ui.strip, .openWatchApp)
    }

    func testWaitingEndsByItself() async {
        await coordinator.keyboardDidAppear()
        coordinator.controlTapped()
        XCTAssertTrue(coordinator.isWaiting)
        coordinator.keyboardWillDisappear()
        XCTAssertFalse(coordinator.isWaiting, "nothing keeps running for a keyboard off screen")
    }

    func testFullAccessIsAskedForOnTheTap() async {
        host.watchHasFullAccess = false
        await coordinator.keyboardDidAppear()
        coordinator.controlTapped()
        XCTAssertFalse(coordinator.isWaiting)
        XCTAssertEqual(host.ui.strip, .fullAccessRequired)
    }

    // MARK: TEST 14: no watch

    func testWithoutAWatchTheKeyboardIsWhatItWas() async throws {
        try? FileManager.default.removeItem(at: root)
        await coordinator.keyboardDidAppear()
        XCTAssertEqual(host.ui, WatchVoiceUI())
        coordinator.controlTapped()
        XCTAssertFalse(coordinator.isWaiting)
        try await Task.sleep(for: .milliseconds(120))
        XCTAssertNil(store.readSession(), "nothing is written for nobody")
        XCTAssertEqual(host.reads, 0)
    }

    // MARK: Sessions

    func testAnotherFieldIsAnotherSession() async {
        await coordinator.keyboardDidAppear()
        let first = sessionID
        coordinator.hostFieldDidChange()
        XCTAssertNotEqual(sessionID, first)
    }

    func testTheSessionIsPublishedAndEnded() async throws {
        await coordinator.keyboardDidAppear()
        let id = sessionID
        try await Task.sleep(for: .milliseconds(120))
        XCTAssertEqual(store.readSession()?.id, id)
        XCTAssertEqual(store.readSession()?.isActive, true)
        XCTAssertEqual(store.readSession()?.documentID, host.document?.uuidString)
        coordinator.keyboardWillDisappear()
        try await Task.sleep(for: .milliseconds(120))
        XCTAssertEqual(store.readSession()?.isActive, false)
    }

    func testNothingFromTheFieldIsPublished() async throws {
        host.before = "секрет"
        host.after = "пароль"
        await coordinator.keyboardDidAppear()
        try await Task.sleep(for: .milliseconds(120))
        let data = try Data(contentsOf: root.appendingPathComponent("keyboard-session.json"))
        let text = String(decoding: data, as: UTF8.self)
        XCTAssertFalse(text.contains("секрет"))
        XCTAssertFalse(text.contains("пароль"))
    }

    func testAnExpiredInstructionIsNotOffered() async {
        let old = WatchVoiceCommand(
            envelope: .command(instruction: "Старое", language: .russian, options: WatchVoiceOptions(), keyboardSessionID: nil,
                               createdAt: now.addingTimeInterval(-WatchVoice.commandLifetime - 1)),
            targetSessionID: nil
        )!
        XCTAssertEqual(store.enqueue(old), .queued)
        await coordinator.keyboardDidAppear()
        XCTAssertNil(host.ui.strip)
        XCTAssertEqual(status(old.id), .expired)
    }

    func testTheCopiedMessageIsNeverWrittenDown() async throws {
        host.copied = .success(ReplyContext(text: "личное сообщение", source: .clipboard))
        await coordinator.keyboardDidAppear()
        send(target: sessionID)
        await coordinator.reconcile()
        coordinator.generationDidFinish(.success("Ответ"))
        let enumerator = try XCTUnwrap(FileManager.default.enumerator(at: root, includingPropertiesForKeys: nil))
        for case let url as URL in enumerator {
            if let data = try? Data(contentsOf: url), let text = String(data: data, encoding: .utf8) {
                XCTAssertFalse(text.contains("личное сообщение"), url.lastPathComponent)
            }
        }
    }
}
