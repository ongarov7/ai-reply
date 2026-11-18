import AVFoundation
import XCTest
@testable import AIReply

final class VoiceAssistantTransportTests: XCTestCase {
    /// Decode with Apple's WAV reader rather than trusting our writer's own
    /// offsets. Signed samples catch endian/format mistakes that silence hides.
    func testFallbackWAVIsLossless24kMonoPCM16() throws {
        let samples: [Int16] = [Int16.min, -1200, -1, 0, 1, 1200, Int16.max]
        var pcm = Data()
        for index in 0..<12_000 {
            var sample = samples[index % samples.count].littleEndian
            withUnsafeBytes(of: &sample) { pcm.append(contentsOf: $0) }
        }
        let wav = VoiceAudioCapture.wav(pcm: pcm)
        let fileURL = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".wav")
        try wav.write(to: fileURL)
        defer { try? FileManager.default.removeItem(at: fileURL) }

        let file = try AVAudioFile(forReading: fileURL, commonFormat: .pcmFormatInt16, interleaved: true)
        XCTAssertEqual(file.fileFormat.sampleRate, 24_000)
        XCTAssertEqual(file.fileFormat.channelCount, 1)
        XCTAssertEqual(file.fileFormat.streamDescription.pointee.mBitsPerChannel, 16)
        XCTAssertEqual(file.length, 12_000)
        let buffer = try XCTUnwrap(AVAudioPCMBuffer(pcmFormat: file.processingFormat, frameCapacity: 12_000))
        try file.read(into: buffer)
        XCTAssertEqual(buffer.frameLength, 12_000)
        let decoded = try XCTUnwrap(buffer.int16ChannelData?[0])
        for index in 0..<12_000 {
            XCTAssertEqual(decoded[index], samples[index % samples.count], "sample \(index)")
        }
    }

    /// Both ends of the supported recording window must pass the Go endpoint's
    /// independent declared-duration (300...40500ms), WAV-duration (200...41000ms)
    /// and upload-size (4 MiB) checks. Duration includes PCM only, never headers.
    func testShortestAndLongestCaptureProduceBackendAcceptedUploadMetadata() throws {
        for milliseconds in [500, 40_000] {
            let pcm = Data(repeating: 0x25, count: milliseconds * 48)
            let requestID = UUID().uuidString
            let parts = try parseMultipart(VoiceTranscriptionClient.multipart(
                pcm: pcm, requestID: requestID, languages: ["kk", "ru", "en"], boundary: "VOICE-TEST-BOUNDARY"
            ), boundary: "VOICE-TEST-BOUNDARY")
            XCTAssertEqual(Set(parts.keys), ["request_id", "duration_ms", "languages", "purpose", "audio"])
            XCTAssertEqual(parts["request_id"]?.text, requestID)
            XCTAssertEqual(parts["languages"]?.text, "kk,ru,en")
            XCTAssertEqual(parts["purpose"]?.text, "instruction")
            let duration = try XCTUnwrap(Int(try XCTUnwrap(parts["duration_ms"]?.text)))
            XCTAssertEqual(duration, milliseconds)
            XCTAssertTrue((300...40_500).contains(duration))

            let audio = try XCTUnwrap(parts["audio"])
            XCTAssertTrue(audio.headers.contains("filename=\"voice.wav\""))
            XCTAssertTrue(audio.headers.contains("Content-Type: audio/wav"))
            XCTAssertLessThanOrEqual(audio.body.count, 4 << 20)
            guard audio.body.count >= 44 else { XCTFail("WAV header is truncated"); continue }
            // Independently inspect RIFF sizes, byte rate and data length, the
            // same properties the server uses rather than MIME/file extension.
            XCTAssertEqual(String(decoding: audio.body.prefix(4), as: UTF8.self), "RIFF")
            XCTAssertEqual(String(decoding: audio.body[8..<12], as: UTF8.self), "WAVE")
            XCTAssertEqual(littleEndian32(audio.body, offset: 4), UInt32(audio.body.count - 8))
            let byteRate = littleEndian32(audio.body, offset: 28)
            let payloadBytes = littleEndian32(audio.body, offset: 40)
            XCTAssertEqual(byteRate, 48_000)
            XCTAssertEqual(payloadBytes, UInt32(pcm.count))
            if byteRate > 0 {
                XCTAssertEqual(UInt64(payloadBytes) * 1000 / UInt64(byteRate), UInt64(milliseconds))
            }
            XCTAssertEqual(Data(audio.body.dropFirst(44)), pcm)
        }
    }

    func testOldServerKeepsBatchAvailableAndClearsCachedRealtimeCapability() throws {
        let suite = "VoiceAssistantTransportTests.\(UUID().uuidString)"
        let defaults = try XCTUnwrap(UserDefaults(suiteName: suite))
        defer { defaults.removePersistentDomain(forName: suite) }
        let decoder = JSONDecoder()
        let current = try decoder.decode(AccountAPI.Features.self, from: Data(
            #"{"voice_transcription":true,"voice_realtime_transcription":true}"#.utf8
        ))
        AILimits.storeFeatures(current, defaults: defaults)
        XCTAssertTrue(defaults.bool(forKey: "ai.features.voiceRealtimeTranscription"))

        // This is the production server's current capability shape: omission
        // means use the bounded-file fallback, even after a newer server was seen.
        let older = try decoder.decode(AccountAPI.Features.self, from: Data(#"{"voice_transcription":true}"#.utf8))
        XCTAssertTrue(older.voiceTranscription == true)
        XCTAssertNil(older.voiceRealtimeTranscription)
        AILimits.storeFeatures(older, defaults: defaults)
        XCTAssertTrue(defaults.bool(forKey: "ai.features.voiceTranscription"))
        XCTAssertFalse(defaults.bool(forKey: "ai.features.voiceRealtimeTranscription"))

        AILimits.storeFeatures(nil, defaults: defaults)
        XCTAssertFalse(defaults.bool(forKey: "ai.features.voiceTranscription"))
        XCTAssertFalse(defaults.bool(forKey: "ai.features.voiceRealtimeTranscription"))
    }

    private struct Part {
        let headers: String
        let body: Data
        var text: String { String(decoding: body, as: UTF8.self) }
    }

    /// A small independent MIME reader: rejects duplicate parts, missing
    /// separators, a missing final boundary and data after the final boundary.
    private func parseMultipart(_ body: Data, boundary: String) throws -> [String: Part] {
        enum InvalidMultipart: Error { case malformed }
        let delimiter = Data("--\(boundary)".utf8)
        let newline = Data("\r\n".utf8)
        let headerEnd = Data("\r\n\r\n".utf8)
        let nextBoundary = newline + delimiter
        var cursor = 0
        var result: [String: Part] = [:]
        while cursor < body.count {
            guard body[cursor...].starts(with: delimiter) else { throw InvalidMultipart.malformed }
            cursor += delimiter.count
            if body[cursor...].starts(with: Data("--\r\n".utf8)) {
                guard cursor + 4 == body.count else { throw InvalidMultipart.malformed }
                return result
            }
            guard body[cursor...].starts(with: newline) else { throw InvalidMultipart.malformed }
            cursor += newline.count
            guard let headerRange = body.range(of: headerEnd, in: cursor..<body.count) else { throw InvalidMultipart.malformed }
            let headers = String(decoding: body[cursor..<headerRange.lowerBound], as: UTF8.self)
            guard let nameStart = headers.range(of: "name=\""),
                  let nameEnd = headers[nameStart.upperBound...].firstIndex(of: "\"") else { throw InvalidMultipart.malformed }
            let name = String(headers[nameStart.upperBound..<nameEnd])
            cursor = headerRange.upperBound
            guard let end = body.range(of: nextBoundary, in: cursor..<body.count), result[name] == nil else {
                throw InvalidMultipart.malformed
            }
            result[name] = Part(headers: headers, body: Data(body[cursor..<end.lowerBound]))
            cursor = end.lowerBound + newline.count
        }
        throw InvalidMultipart.malformed
    }

    private func littleEndian32(_ data: Data, offset: Int) -> UInt32 {
        (0..<4).reduce(UInt32(0)) { $0 | UInt32(data[offset + $1]) << ($1 * 8) }
    }

    // MARK: Dictated instruction length

    /// A 40-second voice turn is longer than a typed note. The field that says
    /// so only goes to a server that publishes it: bodies reject unknown fields.
    func testDictatedComposeIsMarkedOnlyForAServerThatKnowsVoiceInstructions() throws {
        let request = ComposeService.Request(instruction: String(repeating: "сөз ", count: 200), uiLanguage: .kazakh,
                                             isVoice: true)
        let newer = try json(AccountComposeTransport.body(for: request, serverSupportsSenderProfile: true,
                                                         serverSupportsVoiceInstruction: true))
        XCTAssertEqual(newer["instruction_source"] as? String, "voice")
        let older = try json(AccountComposeTransport.body(for: request, serverSupportsSenderProfile: true,
                                                         serverSupportsVoiceInstruction: false))
        XCTAssertNil(older["instruction_source"])
        var typed = request
        typed.isVoice = false
        let typedBody = try json(AccountComposeTransport.body(for: typed, serverSupportsVoiceInstruction: true))
        XCTAssertNil(typedBody["instruction_source"], "typed text never claims the voice allowance")
    }

    func testDictatedReplyInstructionIsMarkedAndKeepsItsRoom() throws {
        var context = AccountReplyTransport.RequestContext(
            message: "Ертең кездесеміз бе?", instruction: "Иә деп жауап бер", instructionIsVoice: true,
            templateID: "friend", templateName: "Дос", templateRelationship: "friend", templateTone: .friendly,
            templateInstructions: "", templateReplyLength: .short, templateEmojiPolicy: .allowed,
            templateWorkingHoursBehaviour: .ignore, templateBusiness: nil, appLanguage: "kk", business: nil, profile: nil)
        let newer = try json(AccountReplyTransport.body(for: context, serverSupportsVoiceInstruction: true))
        XCTAssertEqual(newer["instruction_source"] as? String, "voice")
        XCTAssertNil(try json(AccountReplyTransport.body(for: context, serverSupportsVoiceInstruction: false))["instruction_source"])
        context.instruction = ""
        XCTAssertNil(try json(AccountReplyTransport.body(for: context, serverSupportsVoiceInstruction: true))["instruction_source"],
                     "no instruction, nothing to mark")

        // A typed note stays capped at 280; a dictated turn keeps the server's room.
        XCTAssertEqual(ReplyInstruction.maximumCharacters(serverLimit: 1000, reservesLanguageRule: false), 280)
        XCTAssertEqual(ReplyInstruction.maximumCharacters(serverLimit: 1000, reservesLanguageRule: false, voice: true), 1000)
        XCTAssertEqual(ReplyInstruction.maximumCharacters(serverLimit: 400, reservesLanguageRule: false, voice: true), 400)
    }

    func testDictatedInstructionIsCutAfterTheLastWholeWordOnlyWhenItMustBe() {
        let words = String(repeating: "Ответь Айдане ", count: 60)
        XCTAssertEqual(VoiceAssistantController.fitted("  Скажи, что опоздаю.  ", limit: 400), "Скажи, что опоздаю.")
        let cut = VoiceAssistantController.fitted(words, limit: 400)
        XCTAssertLessThanOrEqual(cut.unicodeScalars.count, 400)
        XCTAssertTrue(cut.hasSuffix("Ответь") || cut.hasSuffix("Айдане"), "no word is cut in half: \(cut.suffix(20))")
        XCTAssertEqual(VoiceAssistantController.fitted(words, limit: 2000), words.trimmingCharacters(in: .whitespaces))
    }

    func testVoiceLimitComesFromAServerThatPublishesIt() throws {
        let suite = "voice-limit-" + UUID().uuidString
        let defaults = try XCTUnwrap(UserDefaults(suiteName: suite))
        defer { defaults.removePersistentDomain(forName: suite) }
        let typed = AILimits.current.instructionCharacters
        XCTAssertEqual(AILimits.voiceInstructionCharacters(defaults: defaults), typed, "an older server: the typed limit")
        defaults.set(true, forKey: "ai.features.voiceInstruction")
        defaults.set(1000, forKey: "ai.limits.voiceInstructionCharacters")
        XCTAssertEqual(AILimits.voiceInstructionCharacters(defaults: defaults), max(typed, 1000))
        defaults.set(5, forKey: "ai.limits.voiceInstructionCharacters")
        XCTAssertEqual(AILimits.voiceInstructionCharacters(defaults: defaults), typed, "a broken value is not believed")
    }

    private func json<T: Encodable>(_ body: T) throws -> [String: Any] {
        try XCTUnwrap(JSONSerialization.jsonObject(with: JSONEncoder().encode(body)) as? [String: Any])
    }
}
