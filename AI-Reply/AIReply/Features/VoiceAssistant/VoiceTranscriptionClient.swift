import Foundation

/// Only our authenticated backend is contacted; no provider key reaches iOS.
actor VoiceTranscriptionClient {
    enum Failure: Error { case unavailable, empty, timeout, provider(String) }
    private var socket: URLSessionWebSocketTask?
    private var network: URLSession?

    func connect(requestID: String, languages: [String]) async throws {
        guard let base = AIConfiguration.shared.backendBaseURL,
              var components = URLComponents(url: base.appendingPathComponent("api/v1/ai/transcribe/realtime"), resolvingAgainstBaseURL: false) else {
            throw Failure.unavailable
        }
        components.scheme = components.scheme == "https" ? "wss" : "ws"
        guard let url = components.url else { throw Failure.unavailable }
        let token = try await AccountSession.shared.accessToken()
        var request = URLRequest(url: url, timeoutInterval: 15)
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        let session = ReplyNetworking.makeSession(timeout: 75)
        network = session
        let task = session.webSocketTask(with: request)
        socket = task
        task.resume()
        let timeout = Task { [weak self] in
            do { try await Task.sleep(for: .seconds(15)) } catch { return }
            await self?.close()
        }
        defer { timeout.cancel() }
        try await send(["type": "start", "request_id": requestID, "languages": languages])
        // Wait for upstream configuration before opening the microphone.
        let event = try await receive()
        guard event["type"] as? String == "ready" else {
            throw Failure.provider(event["code"] as? String ?? "UNAVAILABLE")
        }
    }

    func append(_ pcm: Data) async throws {
        try await send(["type": "audio", "audio": pcm.base64EncodedString()])
    }

    func events(onDelta: @escaping @Sendable (String) async -> Void) async throws -> String {
        while true {
            let event = try await receive()
            switch event["type"] as? String {
            case "delta": await onDelta(event["text"] as? String ?? "")
            case "completed":
                let text = (event["text"] as? String ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
                guard !text.isEmpty else { throw Failure.empty }
                return text
            case "error": throw Failure.provider(event["code"] as? String ?? "UNAVAILABLE")
            default: continue
            }
        }
    }

    func commit() async throws { try await send(["type": "commit"]) }

    func close() {
        socket?.cancel(with: .goingAway, reason: nil)
        socket = nil
        network?.invalidateAndCancel()
        network = nil
    }

    private func send(_ value: [String: Any]) async throws {
        guard let socket else { throw Failure.unavailable }
        let data = try JSONSerialization.data(withJSONObject: value)
        try await socket.send(.string(String(decoding: data, as: UTF8.self)))
    }

    private func receive() async throws -> [String: Any] {
        guard let socket else { throw Failure.unavailable }
        let message = try await socket.receive()
        let data: Data
        switch message {
        case .data(let value): data = value
        case .string(let value): data = Data(value.utf8)
        @unknown default: throw Failure.unavailable
        }
        guard data.count < 128 * 1024,
              let event = try JSONSerialization.jsonObject(with: data) as? [String: Any] else { throw Failure.unavailable }
        return event
    }

    static func transcribe(pcm: Data, requestID: String, languages: [String]) async throws -> String {
        guard let base = AIConfiguration.shared.backendBaseURL else { throw Failure.unavailable }
        let boundary = "aireply-" + UUID().uuidString
        let body = multipart(pcm: pcm, requestID: requestID, languages: languages, boundary: boundary)
        let session = ReplyNetworking.makeSession(timeout: 45)
        defer { session.invalidateAndCancel() }
        return try await AccountSession.shared.authenticated { token in
            var request = URLRequest(url: base.appendingPathComponent("api/v1/ai/transcribe"))
            request.httpMethod = "POST"
            request.httpBody = body
            request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
            request.setValue("multipart/form-data; boundary=\(boundary)", forHTTPHeaderField: "Content-Type")
            let (data, response) = try await session.data(for: request)
            guard let http = response as? HTTPURLResponse else { throw APIError.malformedResponse }
            guard (200..<300).contains(http.statusCode) else {
                throw APIClient.mapServerError(status: http.statusCode, data: data, headers: http)
            }
            struct Response: Decodable { let text: String }
            let text = try JSONDecoder().decode(Response.self, from: data).text.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !text.isEmpty else { throw Failure.empty }
            return text
        }
    }

    nonisolated static func multipart(pcm: Data, requestID: String, languages: [String], boundary: String) -> Data {
        var data = Data()
        for (key, value) in [("request_id", requestID), ("duration_ms", String(pcm.count * 1000 / 48_000)),
                             ("languages", languages.joined(separator: ",")), ("purpose", "instruction")] {
            data.append(Data("--\(boundary)\r\nContent-Disposition: form-data; name=\"\(key)\"\r\n\r\n\(value)\r\n".utf8))
        }
        data.append(Data("--\(boundary)\r\nContent-Disposition: form-data; name=\"audio\"; filename=\"voice.wav\"\r\nContent-Type: audio/wav\r\n\r\n".utf8))
        data.append(VoiceAudioCapture.wav(pcm: pcm))
        data.append(Data("\r\n--\(boundary)--\r\n".utf8))
        return data
    }
}
