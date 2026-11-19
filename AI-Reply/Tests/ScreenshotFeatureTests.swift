import XCTest
@testable import AIReply

/// The screenshot reply's storage, request body, price handling and words.
final class ScreenshotFeatureTests: XCTestCase {

    private func conversation() -> ScreenshotConversation {
        ScreenshotConversation(app: "whatsapp", title: "Aigerim", messages: [
            ScreenshotMessage(id: "m1", role: .other, sender: "Aigerim", text: "Are you free tomorrow?", time: "12:30",
                              frame: CGRect(x: 0.05, y: 0.2, width: 0.5, height: 0.04), textConfidence: 0.9, roleConfidence: 0.95),
            ScreenshotMessage(id: "m2", role: .me, text: "Not sure yet."),
            ScreenshotMessage(id: "m3", role: .other, text: "We really need your help."),
        ])
    }

    private func context(at date: Date = Date()) -> ScreenshotContext {
        ScreenshotContext(id: UUID().uuidString, createdAt: date, source: .shortcut, imageWidth: 1179, imageHeight: 2556,
                          conversation: conversation())
    }

    // MARK: Store

    func testStoreKeepsTheLatestScreenshotForAnHourOnly() throws {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent("shot-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: root) }
        let store = ScreenshotContextStore(rootURL: root)
        let saved = context()
        XCTAssertTrue(store.save(saved, image: Data([0xff, 0xd8, 0xff])))
        XCTAssertEqual(store.read(), saved)
        XCTAssertEqual(store.imageData(for: saved), Data([0xff, 0xd8, 0xff]))
        XCTAssertTrue(try root.resourceValues(forKeys: [.isExcludedFromBackupKey]).isExcludedFromBackup == true)

        let updated = store.update { $0.conversation.targetID = "m1" }
        XCTAssertEqual(updated?.conversation.target?.id, "m1")

        XCTAssertNil(store.read(now: saved.createdAt.addingTimeInterval(ScreenshotContext.lifetime + 1)),
                     "after the hour it is gone")
        XCTAssertFalse(FileManager.default.fileExists(atPath: root.appendingPathComponent("image.jpg").path))
    }

    func testDeleteRemovesPictureAndText() {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent("shot-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: root) }
        let store = ScreenshotContextStore(rootURL: root)
        store.save(context(), image: Data([1, 2, 3]))
        store.delete()
        XCTAssertNil(store.read())
        XCTAssertFalse(FileManager.default.fileExists(atPath: root.appendingPathComponent("context.json").path))
    }

    // MARK: Conversation

    func testTargetIsTheOtherSidesLatestUnlessChosen() {
        var conversation = conversation()
        XCTAssertEqual(conversation.target?.id, "m3")
        conversation.targetID = "m1"
        XCTAssertEqual(conversation.target?.id, "m1")
        let mine = ScreenshotConversation(messages: [ScreenshotMessage(id: "a", role: .me, text: "hi")])
        XCTAssertNil(mine.target, "never the person's own message")
    }

    func testWireCarriesNoFramesOrConfidences() throws {
        let data = try JSONEncoder().encode(conversation().wire)
        let json = String(decoding: data, as: UTF8.self)
        XCTAssertFalse(json.contains("frame"))
        XCTAssertFalse(json.contains("confidence"))
        XCTAssertTrue(json.contains("\"role\":\"self\""))
        XCTAssertTrue(json.contains("\"sender\":\"Aigerim\""))
    }

    func testLongChatsAreTrimmedFromTheOldestEnd() {
        var long = ScreenshotConversation()
        for index in 0..<80 {
            long.messages.append(ScreenshotMessage(id: "m\(index)", role: index.isMultiple(of: 2) ? .other : .me,
                                                   text: String(repeating: "x", count: 150)))
        }
        long.targetID = "m0"
        let trimmed = long.trimmedToServerLimits()
        XCTAssertLessThanOrEqual(trimmed.messages.count, ScreenshotConversation.maximumMessages)
        XCTAssertLessThanOrEqual(trimmed.characterCount, ScreenshotConversation.maximumTotalCharacters)
        XCTAssertEqual(trimmed.messages.last?.id, "m79", "the newest messages stay")
        XCTAssertNil(trimmed.targetID, "a chosen message that was trimmed away is not kept")
    }

    // MARK: Request

    private func request(mode: ScreenshotReplyMode = .text, approved: Int = 1) -> ScreenshotReplyRequest {
        ScreenshotReplyRequest(mode: mode, conversation: conversation(), image: Data([0xff, 0xd8]),
                               imageWidth: 1024, imageHeight: 2218, instruction: "  Say yes after 7 PM ",
                               template: ReplyTemplate.builtIn(.friend, sortIndex: 0), configuration: .initial,
                               uiLanguage: .russian, approvedUnits: approved)
    }

    func testTextRequestSendsTextOnly() throws {
        let body = ScreenshotReplyClient.body(for: request(approved: 3), includeImage: true)
        XCTAssertEqual(body.mode, "text")
        XCTAssertNil(body.image, "text mode never carries the picture")
        XCTAssertNil(body.image_consent)
        XCTAssertEqual(body.max_units, 3)
        XCTAssertEqual(body.instruction, "Say yes after 7 PM")
        XCTAssertEqual(body.conversation.messages.count, 3)
        XCTAssertEqual(body.language, "ru")
    }

    func testImageEstimateSendsTheSizeNotThePicture() {
        let estimate = ScreenshotReplyClient.body(for: request(mode: .image), includeImage: false)
        XCTAssertNil(estimate.image?.data)
        XCTAssertEqual(estimate.image?.width, 1024)
        XCTAssertNil(estimate.image_consent)
        let reply = ScreenshotReplyClient.body(for: request(mode: .image), includeImage: true)
        XCTAssertEqual(reply.image?.data, Data([0xff, 0xd8]).base64EncodedString())
        XCTAssertEqual(reply.image_consent, ScreenshotFeature.imageConsentVersion)
    }

    // MARK: Refusals

    func testConfirmationAndBalanceRefusalsCarryThePrice() {
        let quote = #""details":{"mode":"text","estimated_units":1,"max_units":2,"unlimited":false,"available_units":5,"input_tokens":2800,"max_output_tokens":180,"tokens_counted":false,"policy_version":"screenshot-credits-v1"}"#
        let confirm = Data(#"{"error":{"code":"CREDIT_CONFIRMATION_REQUIRED","message":"x",\#(quote)}}"#.utf8)
        guard case .confirmationRequired(let asked)? = ScreenshotReplyClient.screenshotFailure(confirm) else {
            return XCTFail("confirmation not recognised")
        }
        XCTAssertEqual(asked.maxUnits, 2)
        XCTAssertEqual(asked.availableUnits, 5)
        XCTAssertTrue(asked.needsConfirmation)
        XCTAssertFalse(asked.exceedsBalance)

        let poor = Data(#"{"error":{"code":"INSUFFICIENT_CREDITS","message":"x",\#(quote.replacingOccurrences(of: "\"available_units\":5", with: "\"available_units\":1,\"can_buy\":true"))}}"#.utf8)
        guard case .insufficientCredits(let price, let canBuy)? = ScreenshotReplyClient.screenshotFailure(poor) else {
            return XCTFail("insufficient credits not recognised")
        }
        XCTAssertTrue(canBuy)
        XCTAssertTrue(price.exceedsBalance)

        let limit = Data(#"{"error":{"code":"SCREENSHOT_IMAGE_LIMIT_REACHED","details":{"daily_limit":50}}}"#.utf8)
        XCTAssertEqual(ScreenshotReplyClient.screenshotFailure(limit), .imageLimitReached(limit: 50))
        let other = Data(#"{"error":{"code":"DAILY_LIMIT_REACHED"}}"#.utf8)
        XCTAssertNil(ScreenshotReplyClient.screenshotFailure(other), "the usual refusals keep their usual mapping")
    }

    func testCostMessagesExistInEveryLanguage() {
        for language in AppLanguage.allCases {
            let strings = AIReplyStrings.forLanguage(language)
            let confirm = strings.message(for: .screenshotCostConfirmation(maxUnits: 3))
            let poor = strings.message(for: .screenshotCostTooHigh(maxUnits: 3, available: 1))
            XCTAssertTrue(confirm.contains("3"), "\(language): \(confirm)")
            XCTAssertTrue(poor.contains("1"), "\(language): \(poor)")
            XCTAssertTrue(confirm.contains(strings.retry), "\(language): the line names the button that confirms")
            XCTAssertFalse(ScreenshotKeyboardStrings(language).ready(app: "WhatsApp", messages: 4).isEmpty)
        }
        XCTAssertTrue(AIReplyError.screenshotCostConfirmation(maxUnits: 2).offersRetry)
        XCTAssertFalse(AIReplyError.screenshotCostTooHigh(maxUnits: 2, available: 1).offersRetry)
    }

    // MARK: Voice

    func testInstructionSegmentsJoinWithOneSpace() {
        XCTAssertEqual(ScreenshotInstructionRecorder.join(" Say yes ", " after 7 "), "Say yes after 7")
        XCTAssertEqual(ScreenshotInstructionRecorder.join("", "ok"), "ok")
        XCTAssertEqual(ScreenshotInstructionRecorder.limit, 40, "a spoken instruction is at most 40 seconds")
    }

    @MainActor
    func testDictationLimitIsConfigurable() {
        let dictation = VoiceReplyViewModel(maximumDuration: 40)
        XCTAssertEqual(dictation.maximumDuration, 40)
        XCTAssertEqual(VoiceReplyViewModel().maximumDuration, 60, "other dictation keeps its minute")
    }
}

/// The in-chat shortcut: screenshot, then AI Reply Voice at once.
final class ScreenshotVoiceTests: XCTestCase {

    func testAnOlderVoiceSnapshotStillDecodes() throws {
        let old = #"{"sessionID":"s1","phase":"responseReady","transcript":"ok","response":"Hi","updatedAt":0,"expiresAt":0,"sourceMessage":"","keyboardSessionID":null}"#
        let snapshot = try JSONDecoder().decode(VoiceAssistantSnapshot.self, from: Data(old.utf8))
        XCTAssertNil(snapshot.screenshotID)
        var next = snapshot
        next.screenshotID = "shot-1"
        let round = try JSONDecoder().decode(VoiceAssistantSnapshot.self, from: JSONEncoder().encode(next))
        XCTAssertEqual(round.screenshotID, "shot-1")
    }

    func testTheScreenshotRemembersTheVoiceTurnAnsweringIt() throws {
        var context = ScreenshotContext(id: "shot-1", createdAt: Date(), source: .shortcut, imageWidth: 10, imageHeight: 10,
                                        conversation: ScreenshotConversation(messages: [ScreenshotMessage(id: "m1", role: .other, text: "Hi")]))
        context.voiceSessionID = "voice-1"
        let round = try JSONDecoder().decode(ScreenshotContext.self, from: JSONEncoder().encode(context))
        XCTAssertEqual(round.voiceSessionID, "voice-1")
    }

    @MainActor
    func testWithoutTheVoiceSetupTheShortcutSaysWhatToDo() async {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent("voice-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: root) }
        let controller = VoiceAssistantController(store: VoiceAssistantStore(rootURL: root), protectedDataAvailable: { true })
        let context = ScreenshotContext(id: "shot-1", createdAt: Date(), source: .shortcut, imageWidth: 10, imageHeight: 10,
                                        conversation: ScreenshotConversation(messages: [ScreenshotMessage(id: "m1", role: .other, text: "Hi")]))
        do {
            try await controller.startFromScreenshot(context)
            XCTFail("no microphone or voice consent was given in tests")
        } catch let error as VoiceAssistantControlStartError {
            XCTAssertFalse(error.message.isEmpty)
        } catch {
            XCTFail("unexpected \(error)")
        }
        XCTAssertFalse(controller.isCapturing)
    }

    func testVoiceConfirmationWordsExistInEveryLanguage() {
        for language in AppLanguage.allCases {
            let text = ScreenshotKeyboardStrings(language).voiceNeedsConfirmation(maxUnits: 3)
            XCTAssertTrue(text.contains("3"), "\(language): \(text)")
        }
    }
}
