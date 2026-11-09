import Foundation

/// Turns a recording from the watch into text: the iPhone uploads it to the
/// same `POST /api/v1/ai/transcribe` the Android keyboard's microphone uses,
/// and the server, which keeps the OpenAI key, runs the speech model.
///
/// It goes through the iPhone because the account is there: the watch has no
/// session and no key, and is not a device the server knows about. The audio
/// is held only for this call. It spends no replies; the endpoint has its own
/// daily cap.
struct WatchTranscriber: Sendable {

    /// Upload plus recognition of 40 seconds of audio, as on Android.
    static let timeout: TimeInterval = 45

    /// Whether the microphone can be offered.
    static var isAvailable: Bool {
        #if DEBUG
        if AIConfiguration.debugMockReplies { return true }
        #endif
        return AILimits.serverSupportsVoiceTranscription
    }

    func transcribe(audio: Data, milliseconds: Int, requestID: String,
                    language: WatchVoiceLanguage) async -> Result<String, WatchVoiceFailure> {
        #if DEBUG
        if AIConfiguration.debugMockReplies {
            try? await Task.sleep(for: .milliseconds(600))
            return .success(Self.debugText(language))
        }
        #endif
        guard Self.isAvailable else { return .failure(.transcriptionUnavailable) }
        if let refusal = AIReplyService.preflight(isSignedIn: AccountCredentials.isSignedIn,
                                                  hasConsent: LegalConsentStore.hasAcceptedCurrentVersions()) {
            return .failure(WatchVoiceFailure(refusal))
        }
        guard let baseURL = AIConfiguration.shared.backendBaseURL else { return .failure(.transcriptionUnavailable) }

        let boundary = "aireply-" + UUID().uuidString
        let body = Self.multipartBody(audio: audio, milliseconds: milliseconds, requestID: requestID,
                                      language: language, boundary: boundary)
        let url = baseURL.appendingPathComponent("api/v1/ai/transcribe")
        let session = ReplyNetworking.makeSession(timeout: Self.timeout)
        do {
            let response: Response = try await AccountSession.shared.authenticated { token in
                var request = URLRequest(url: url)
                request.httpMethod = "POST"
                request.httpBody = body
                request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
                request.setValue("application/json", forHTTPHeaderField: "Accept")
                request.setValue("multipart/form-data; boundary=\(boundary)", forHTTPHeaderField: "Content-Type")
                return try await Self.send(request, using: session)
            }
            return .success(response.text)
        } catch let error as APIError {
            AccountReplyTransport.noteRefusal(error)
            return .failure(Self.failure(for: error))
        } catch {
            return .failure(.transcriptionFailed)
        }
    }

    private struct Response: Decodable, Sendable {
        let text: String
    }

    private static func send(_ request: URLRequest, using session: URLSession) async throws -> Response {
        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await session.data(for: request)
        } catch {
            throw APIClient.mapTransportError(error)
        }
        guard let http = response as? HTTPURLResponse else { throw APIError.malformedResponse }
        guard (200..<300).contains(http.statusCode) else {
            throw APIClient.mapServerError(status: http.statusCode, data: data, headers: http)
        }
        guard let decoded = try? JSONDecoder().decode(Response.self, from: data) else {
            throw APIError.malformedResponse
        }
        return decoded
    }

    // MARK: Wire format

    /// The form the endpoint reads; separate so tests can pin it.
    static func multipartBody(audio: Data, milliseconds: Int, requestID: String,
                              language: WatchVoiceLanguage, boundary: String) -> Data {
        var body = Data()
        func field(_ name: String, _ value: String) {
            body.append(Data("--\(boundary)\r\n".utf8))
            body.append(Data("Content-Disposition: form-data; name=\"\(name)\"\r\n\r\n".utf8))
            body.append(Data("\(value)\r\n".utf8))
        }
        field("request_id", requestID)
        field("duration_ms", String(milliseconds))
        field("languages", language.transcriptionHints.joined(separator: ","))
        field("purpose", "instruction")
        body.append(Data("--\(boundary)\r\n".utf8))
        body.append(Data("Content-Disposition: form-data; name=\"audio\"; filename=\"voice.m4a\"\r\n".utf8))
        body.append(Data("Content-Type: audio/mp4\r\n\r\n".utf8))
        body.append(audio)
        body.append(Data("\r\n--\(boundary)--\r\n".utf8))
        return body
    }

    /// The daily cap and a burst of requests are different problems: tomorrow
    /// versus a few seconds. Android keeps them apart for the same reason.
    static func failure(for error: APIError) -> WatchVoiceFailure {
        switch error {
        case .offline:                     return .offline
        case .timedOut, .providerTimeout:  return .timedOut
        case .unauthorized, .accountDisabled: return .notSignedIn
        case .consentRequired:             return .consentRequired
        case .voiceLimitReached:           return .voiceLimitReached
        case .rateLimited:                 return .rateLimited
        case .notFound:                    return .transcriptionUnavailable
        case .invalidRequest:              return .audioRejected
        default:                           return .transcriptionFailed
        }
    }

    #if DEBUG
    /// With `-AIReplyMockReplies`, so the flow runs with no server.
    private static func debugText(_ language: WatchVoiceLanguage) -> String {
        switch language {
        case .english: return "Tell him I am busy today and tomorrow. Suggest Wednesday afternoon."
        case .russian: return "Скажи, что я занят сегодня и завтра. Предложи среду после обеда."
        case .kazakh:  return "Бүгін де, ертең де бос емеспін де. Сәрсенбі түстен кейін кездесуді ұсын."
        }
    }
    #endif
}
