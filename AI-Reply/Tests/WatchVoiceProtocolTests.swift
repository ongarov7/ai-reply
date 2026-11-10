import XCTest
@testable import AIReply

/// The wire format between the watch and the phone: what is accepted, what
/// is refused, and that nothing is lost on the way.
final class WatchVoiceProtocolTests: XCTestCase {

    private let now = Date(timeIntervalSince1970: 1_800_000_000)

    private func command(
        instruction: String = "Скажи, что я занят сегодня",
        createdAt: Date? = nil,
        requestID: String = UUID().uuidString
    ) -> WatchVoiceEnvelope {
        .command(requestID: requestID, instruction: instruction, language: .russian,
                 options: WatchVoiceOptions(mode: .reply, templateID: "client", responseLanguage: .kazakh, length: .long),
                 keyboardSessionID: UUID().uuidString, createdAt: createdAt ?? now)
    }

    // MARK: Encoding

    func testACommandSurvivesTheWire() throws {
        let original = command()
        let dictionary = try WatchVoiceCodec.dictionary(original)
        XCTAssertEqual(Array(dictionary.keys), [WatchVoice.payloadKey], "one key: no loose dictionaries")
        XCTAssertTrue(dictionary[WatchVoice.payloadKey] is Data, "a property-list type WatchConnectivity accepts")
        XCTAssertEqual(WatchVoiceCodec.envelope(from: dictionary), original)
    }

    // MARK: The copied message on the watch

    func testACommandNamesTheMessageTheWatchShowed() throws {
        let shown = UUID().uuidString
        let sent = WatchVoiceEnvelope.command(instruction: "Ответь вежливо.", language: .russian,
                                              options: WatchVoiceOptions(templateID: "friend"),
                                              keyboardSessionID: nil, previewID: shown, createdAt: now)
        let received = try XCTUnwrap(WatchVoiceCodec.envelope(from: try WatchVoiceCodec.dictionary(sent)))
        XCTAssertEqual(received.previewID, shown)
        XCTAssertNil(WatchVoiceValidator.problem(with: received, now: now))
        XCTAssertEqual(WatchVoiceCommand(envelope: received, targetSessionID: nil)?.previewID, shown)
    }

    func testACommandFromBeforePreviewsStillReads() throws {
        // A command file written by the first builds has no previewID.
        let old = WatchVoiceCommand(envelope: command(), targetSessionID: nil)!
        var object = try XCTUnwrap(JSONSerialization.jsonObject(with: WatchVoiceCodec.data(old)) as? [String: Any])
        object.removeValue(forKey: "previewID")
        let data = try JSONSerialization.data(withJSONObject: object)
        let decoded = try XCTUnwrap(WatchVoiceCodec.value(WatchVoiceCommand.self, from: data))
        XCTAssertNil(decoded.previewID)
        XCTAssertEqual(decoded.id, old.id)
    }

    func testAPreviewIdentifierIsAnIdentifier() {
        var bad = command()
        bad.previewID = "../../outcomes/x"
        XCTAssertEqual(WatchVoiceValidator.problem(with: bad, now: now), .invalidRequest)
    }

    func testOnlyTheWatchAsksForTheCopiedMessage() {
        XCTAssertNil(WatchVoiceValidator.problem(with: .request(.previewRequest, createdAt: now), now: now))
        XCTAssertEqual(WatchVoiceValidator.problem(with: .request(.previewRequest, source: .phone, createdAt: now), now: now),
                       .invalidRequest)
    }

    func testTheWatchIsShownTheStartOfALongMessage() throws {
        let long = String(repeating: "Ә", count: WatchVoice.maximumPreviewScalars + 50)
        let preview = WatchSourcePreview.ready(long)
        XCTAssertTrue(preview.isTruncated)
        XCTAssertEqual(preview.text?.unicodeScalars.count, WatchVoice.maximumPreviewScalars)
        XCTAssertNil(WatchVoiceValidator.problem(with: .preview(requestID: UUID().uuidString, preview: preview, createdAt: now), now: now))

        let short = WatchSourcePreview.ready("Можем встретиться завтра?")
        XCTAssertFalse(short.isTruncated)
        XCTAssertEqual(short.text, "Можем встретиться завтра?")
    }

    func testTheCutNeverSplitsACharacter() {
        // A family emoji is several scalars: it is kept whole or left out.
        let family = "👨‍👩‍👧"
        let text = String(repeating: "a", count: WatchVoice.maximumPreviewScalars - 2) + family
        let preview = WatchSourcePreview.ready(text)
        XCTAssertTrue(preview.isTruncated)
        XCTAssertFalse(preview.text?.contains("👨") ?? true)
        XCTAssertLessThanOrEqual(preview.text?.unicodeScalars.count ?? .max, WatchVoice.maximumPreviewScalars)
    }

    func testAnOversizedPreviewIsRefused() {
        var envelope = WatchVoiceEnvelope.preview(requestID: UUID().uuidString, preview: .ready("x"), createdAt: now)
        envelope.preview?.text = String(repeating: "x", count: WatchVoice.maximumPreviewScalars + 1)
        XCTAssertEqual(WatchVoiceValidator.problem(with: envelope, now: now), .invalidRequest)
    }

    func testQuickRepliesAreInstructionsInEveryLanguage() {
        for language in WatchVoiceLanguage.allCases {
            let texts = WatchQuickReply.allCases.map { $0.instruction(in: language) }
            XCTAssertEqual(Set(texts).count, texts.count, language.rawValue)
            for text in texts {
                XCTAssertFalse(text.trimmingCharacters(in: .whitespaces).isEmpty)
                XCTAssertLessThan(text.count, 80, "short enough for any limit")
            }
        }
        XCTAssertEqual(WatchQuickReply.polite.instruction(in: .russian), "Ответь вежливо.")
        XCTAssertEqual(WatchQuickReply.polite.instruction(in: .kazakh), "Сыпайы жауап бер.")
    }

    func testTheEnvelopeCarriesWhatTheProtocolPromises() {
        let envelope = command()
        XCTAssertEqual(envelope.version, WatchVoice.protocolVersion)
        XCTAssertEqual(envelope.type, .command)
        XCTAssertEqual(envelope.source, .watch)
        XCTAssertEqual(envelope.createdAt, now)
        XCTAssertNotNil(envelope.keyboardSessionID)
        XCTAssertEqual(envelope.language, .russian)
        XCTAssertNotNil(envelope.instruction)
    }

    func testAnotherDictionaryIsNotAnEnvelope() {
        XCTAssertNil(WatchVoiceCodec.envelope(from: [:]))
        XCTAssertNil(WatchVoiceCodec.envelope(from: ["text": "hello"]))
        XCTAssertNil(WatchVoiceCodec.envelope(from: [WatchVoice.payloadKey: Data("not json".utf8)]))
    }

    func testAnAudioPacketKeepsEnvelopeAndAudioApart() throws {
        let envelope = WatchVoiceEnvelope(requestID: UUID().uuidString, type: .transcribe, createdAt: now, source: .watch,
                                          language: .kazakh, audioMilliseconds: 3200)
        let audio = Data((0..<4096).map { UInt8($0 % 256) })
        let decoded = try XCTUnwrap(WatchVoiceAudioPacket.decode(try WatchVoiceAudioPacket.encode(envelope, audio: audio)))
        XCTAssertEqual(decoded.envelope, envelope)
        XCTAssertEqual(decoded.audio, audio)
    }

    func testABrokenAudioPacketIsRefused() {
        XCTAssertNil(WatchVoiceAudioPacket.decode(Data()))
        XCTAssertNil(WatchVoiceAudioPacket.decode(Data([0, 0, 0, 10, 1, 2])), "shorter than it claims")
        XCTAssertNil(WatchVoiceAudioPacket.decode(Data([0xFF, 0xFF, 0xFF, 0xFF, 1, 2, 3, 4])), "an absurd length")
    }

    // MARK: Audio in parts

    func testARecordingIsSplitIntoPartsThatFitTheFastChannel() {
        let audio = Data((0..<(150 * 1024)).map { UInt8($0 % 251) })
        let parts = WatchVoiceAudioAssembly.split(audio)
        XCTAssertEqual(parts.count, 3)
        XCTAssertTrue(parts.allSatisfy { $0.count <= WatchVoice.maximumAudioPartBytes })
        XCTAssertEqual(parts.reduce(Data(), +), audio)
        XCTAssertEqual(WatchVoiceAudioAssembly.split(Data([1, 2, 3])).count, 1)
        XCTAssertEqual(WatchVoiceAudioAssembly.split(Data()), [])
    }

    func testPartsComeBackTogetherInAnyOrderAndTwice() {
        let audio = Data((0..<(120 * 1024)).map { UInt8($0 % 239) })
        let parts = WatchVoiceAudioAssembly.split(audio)
        var assembly = WatchVoiceAudioAssembly()
        XCTAssertEqual(assembly.add(parts[2], part: 2, of: 3, requestID: "voice-request-1", now: now), .waiting)
        XCTAssertEqual(assembly.add(parts[0], part: 0, of: 3, requestID: "voice-request-1", now: now), .waiting)
        XCTAssertEqual(assembly.add(parts[0], part: 0, of: 3, requestID: "voice-request-1", now: now), .waiting, "a part sent again")
        XCTAssertEqual(assembly.add(parts[1], part: 1, of: 3, requestID: "voice-request-1", now: now), .complete(audio))
        XCTAssertEqual(assembly.pendingCount, 0)
    }

    func testAOnePartRecordingIsCompleteAtOnce() {
        var assembly = WatchVoiceAudioAssembly()
        XCTAssertEqual(assembly.add(Data([7, 8]), part: 0, of: 1, requestID: "voice-request-2", now: now), .complete(Data([7, 8])))
    }

    func testPartsThatDoNotAddUpAreRefused() {
        var assembly = WatchVoiceAudioAssembly()
        XCTAssertEqual(assembly.add(Data([1]), part: 3, of: 3, requestID: "voice-request-3", now: now), .rejected)
        XCTAssertEqual(assembly.add(Data([1]), part: 0, of: 0, requestID: "voice-request-3", now: now), .rejected)
        XCTAssertEqual(assembly.add(Data([1]), part: 0, of: WatchVoice.maximumAudioParts + 1, requestID: "voice-request-3", now: now), .rejected)
        XCTAssertEqual(assembly.add(Data([1]), part: 0, of: 2, requestID: "voice-request-3", now: now), .waiting)
        XCTAssertEqual(assembly.add(Data([2]), part: 1, of: 3, requestID: "voice-request-3", now: now), .rejected, "another count for the same recording")
    }

    func testTooMuchAudioIsRefused() {
        var assembly = WatchVoiceAudioAssembly()
        let big = Data(count: WatchVoice.maximumAudioBytes / 2 + 1)
        XCTAssertEqual(assembly.add(big, part: 0, of: 3, requestID: "voice-request-4", now: now), .waiting)
        XCTAssertEqual(assembly.add(big, part: 1, of: 3, requestID: "voice-request-4", now: now), .rejected)
    }

    func testPartsThatStopComingAreDropped() {
        var assembly = WatchVoiceAudioAssembly()
        XCTAssertEqual(assembly.add(Data([1]), part: 0, of: 2, requestID: "voice-request-5", now: now), .waiting)
        let later = now.addingTimeInterval(WatchVoiceAudioAssembly.lifetime + 1)
        XCTAssertEqual(assembly.add(Data([2]), part: 1, of: 2, requestID: "voice-request-5", now: later), .waiting,
                       "the first part is gone, so this one starts a new recording")
    }

    func testPartNumbersAreValidated() {
        var envelope = WatchVoiceEnvelope(requestID: UUID().uuidString, type: .transcribe, createdAt: now, source: .watch,
                                          language: .kazakh, audioMilliseconds: 12_000, audioPart: 1, audioParts: 3)
        XCTAssertNil(WatchVoiceValidator.problem(with: envelope, now: now))
        envelope.audioPart = 3
        XCTAssertEqual(WatchVoiceValidator.problem(with: envelope, now: now), .audioRejected)
        envelope.audioPart = nil
        envelope.audioParts = nil
        XCTAssertNil(WatchVoiceValidator.problem(with: envelope, now: now), "the first builds sent no part numbers")
    }

    // MARK: Validation

    func testAGoodCommandPasses() {
        XCTAssertNil(WatchVoiceValidator.problem(with: command(), now: now))
    }

    func testAnotherProtocolVersionIsRefusedByName() {
        var envelope = command()
        envelope.version = WatchVoice.protocolVersion + 1
        XCTAssertEqual(WatchVoiceValidator.problem(with: envelope, now: now), .unsupportedVersion)
    }

    func testIdentifiersCannotLeaveTheirDirectory() {
        for bad in ["", "short", "../../etc/passwd", "a/b/c/d/e/f/g/h", String(repeating: "a", count: 65), "имя-запроса-99"] {
            XCTAssertFalse(WatchVoiceIdentifier.isValid(bad), bad)
        }
        XCTAssertTrue(WatchVoiceIdentifier.isValid(WatchVoiceIdentifier.make()))
        XCTAssertEqual(WatchVoiceValidator.problem(with: command(requestID: "../evil-request"), now: now), .invalidRequest)
    }

    func testAnEmptyInstructionIsRefused() {
        XCTAssertEqual(WatchVoiceValidator.problem(with: command(instruction: "  \n "), now: now), .instructionEmpty)
    }

    func testAnImpossiblyLongInstructionIsRefused() {
        let long = String(repeating: "я", count: WatchVoice.maximumInstructionScalars + 1)
        XCTAssertEqual(WatchVoiceValidator.problem(with: command(instruction: long), now: now), .instructionTooLong)
        let longest = String(repeating: "я", count: WatchVoice.maximumInstructionScalars)
        XCTAssertNil(WatchVoiceValidator.problem(with: command(instruction: longest), now: now))
    }

    func testAnOldCommandExpires() {
        let late = now.addingTimeInterval(WatchVoice.commandLifetime + 1)
        XCTAssertEqual(WatchVoiceValidator.problem(with: command(), now: late), .expired)
        let inTime = now.addingTimeInterval(WatchVoice.commandLifetime - 1)
        XCTAssertNil(WatchVoiceValidator.problem(with: command(), now: inTime))
    }

    func testACommandFromTheFutureIsNotTrusted() {
        let future = now.addingTimeInterval(WatchVoice.clockTolerance + 1)
        XCTAssertEqual(WatchVoiceValidator.problem(with: command(createdAt: future), now: now), .expired)
    }

    func testRecordingLimitsAreTheServers() {
        func transcribe(_ milliseconds: Int?) -> WatchVoiceFailure? {
            WatchVoiceValidator.problem(with: WatchVoiceEnvelope(requestID: UUID().uuidString, type: .transcribe, createdAt: now,
                                                                 source: .watch, language: .english, audioMilliseconds: milliseconds), now: now)
        }
        XCTAssertNil(transcribe(WatchVoice.minimumAudioMilliseconds))
        XCTAssertNil(transcribe(40_000))
        XCTAssertEqual(transcribe(100), .audioRejected)
        XCTAssertEqual(transcribe(41_000), .audioRejected)
        XCTAssertEqual(transcribe(nil), .audioRejected)
    }

    // MARK: Language

    func testTheChosenLanguageIsHeardFirst() {
        XCTAssertEqual(WatchVoiceLanguage.kazakh.transcriptionHints, ["kk", "ru", "en"])
        XCTAssertEqual(WatchVoiceLanguage.russian.transcriptionHints, ["ru", "en", "kk"])
        XCTAssertEqual(WatchVoiceLanguage.english.transcriptionHints, ["en", "ru", "kk"])
    }

    func testAppleDictationIsOnlyOfferedWhereAppleHasIt() {
        XCTAssertTrue(WatchVoiceLanguage.russian.hasSystemDictation)
        XCTAssertTrue(WatchVoiceLanguage.english.hasSystemDictation)
        XCTAssertFalse(WatchVoiceLanguage.kazakh.hasSystemDictation)
    }

    // MARK: Delivery

    func testASendThatFailedIsTriedOnceMoreThenQueued() {
        XCTAssertEqual(WatchVoiceDelivery.step(afterAttempt: 1, isReachable: true), .retry(after: 1))
        XCTAssertEqual(WatchVoiceDelivery.step(afterAttempt: 2, isReachable: true), .queueForLater)
    }

    func testAnUnreachablePhoneGoesStraightToTheQueue() {
        XCTAssertEqual(WatchVoiceDelivery.step(afterAttempt: 1, isReachable: false), .queueForLater)
    }

    func testOnlyWhatRetryCanFixOffersRetry() {
        XCTAssertTrue(WatchVoiceFailure.offline.offersRetry)
        XCTAssertTrue(WatchVoiceFailure.phoneUnreachable.offersRetry)
        XCTAssertFalse(WatchVoiceFailure.quotaExhausted.offersRetry)
        XCTAssertFalse(WatchVoiceFailure.fullAccessRequired.offersRetry)
        XCTAssertFalse(WatchVoiceFailure.microphoneDenied.offersRetry)
    }
}
