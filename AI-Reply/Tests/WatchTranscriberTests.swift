import XCTest
@testable import AIReply

/// The microphone's request: the same form the Android keyboard posts to
/// `/api/v1/ai/transcribe`.
final class WatchTranscriberTests: XCTestCase {

    private func body(_ language: WatchVoiceLanguage = .kazakh) -> String {
        let data = WatchTranscriber.multipartBody(audio: Data([0x41, 0x55, 0x44]), milliseconds: 3200, requestID: "voice-request-1",
                                                  language: language, boundary: "BOUNDARY")
        return String(decoding: data, as: UTF8.self)
    }

    func testTheFormIsTheOneTheServerReads() {
        let form = body()
        XCTAssertTrue(form.contains("name=\"request_id\"\r\n\r\nvoice-request-1\r\n"))
        XCTAssertTrue(form.contains("name=\"duration_ms\"\r\n\r\n3200\r\n"))
        XCTAssertTrue(form.contains("name=\"purpose\"\r\n\r\ninstruction\r\n"))
        XCTAssertTrue(form.contains("name=\"audio\"; filename=\"voice.m4a\"\r\nContent-Type: audio/mp4\r\n\r\nAUD\r\n"))
        XCTAssertTrue(form.hasPrefix("--BOUNDARY\r\n"))
        XCTAssertTrue(form.hasSuffix("--BOUNDARY--\r\n"))
    }

    func testTheChosenLanguageIsTheFirstHint() {
        XCTAssertTrue(body(.kazakh).contains("name=\"languages\"\r\n\r\nkk,ru,en\r\n"))
        XCTAssertTrue(body(.russian).contains("name=\"languages\"\r\n\r\nru,en,kk\r\n"))
        XCTAssertTrue(body(.english).contains("name=\"languages\"\r\n\r\nen,ru,kk\r\n"))
    }

    func testFailuresKeepTheirMeaning() {
        XCTAssertEqual(WatchTranscriber.failure(for: .voiceLimitReached), .voiceLimitReached)
        XCTAssertEqual(WatchTranscriber.failure(for: .rateLimited(retryAfter: 3)), .rateLimited)
        XCTAssertEqual(WatchTranscriber.failure(for: .offline), .offline)
        XCTAssertEqual(WatchTranscriber.failure(for: .timedOut), .timedOut)
        XCTAssertEqual(WatchTranscriber.failure(for: .unauthorized), .notSignedIn)
        XCTAssertEqual(WatchTranscriber.failure(for: .consentRequired), .consentRequired)
        XCTAssertEqual(WatchTranscriber.failure(for: .notFound), .transcriptionUnavailable, "the server has no such endpoint")
        XCTAssertEqual(WatchTranscriber.failure(for: .invalidRequest), .audioRejected)
        XCTAssertEqual(WatchTranscriber.failure(for: .server), .transcriptionFailed)
    }

    func testTheServersDailyVoiceLimitIsRecognised() throws {
        let http = try XCTUnwrap(HTTPURLResponse(url: URL(string: "https://example.test")!, statusCode: 429, httpVersion: nil, headerFields: nil))
        let body = Data(#"{"error":{"code":"VOICE_LIMIT_REACHED","message":"limit"}}"#.utf8)
        XCTAssertEqual(APIClient.mapServerError(status: 429, data: body, headers: http), .voiceLimitReached)
    }

    func testTheFeatureFlagIsRead() throws {
        let json = Data(#"{"voice_transcription":true}"#.utf8)
        XCTAssertEqual(try JSONDecoder().decode(AccountAPI.Features.self, from: json).voiceTranscription, true)
    }
}
