import Foundation
import Testing
@testable import AirReplyCore
@testable import AirReplyUI
@testable import AirReplyVoice

/// End to end against a real backend with a real AI provider. Opt-in only:
///
///     AIREPLY_INTEGRATION_URL=http://127.0.0.1:18084 AIREPLY_INTEGRATION_OTP=2580 \
///     AIREPLY_INTEGRATION_AUDIO=/path/to/samples swift test --filter IntegrationTests
///
/// The server must run with APP_ENV=development and AUTH_DEMO_MODE (fixed code); never point
/// this at production.
@Suite("Integration with a local backend", .serialized,
       .enabled(if: ProcessInfo.processInfo.environment["AIREPLY_INTEGRATION_URL"] != nil))
@MainActor
struct IntegrationTests {
    private let environment = ProcessInfo.processInfo.environment

    @Test func signInConsentGenerateTranscribeSignOut() async throws {
        let base = try #require(environment["AIREPLY_INTEGRATION_URL"].flatMap(URL.init(string:)))
        #expect(base.host == "127.0.0.1" || base.host == "localhost", "integration tests only run against a local server")
        let configuration = AppConfiguration(backendBaseURL: base, appVersion: "1.0.0", build: "1")
        let sessions = SessionStore(store: InMemorySecureStore())
        let client = APIClient(configuration: configuration, transport: URLSessionTransport(timeout: 60), sessions: sessions)
        let auth = AuthSession(client: client, restoredEmail: nil, isSignedIn: false)
        let account = AccountStore(client: client, configuration: configuration)
        auth.onSignedIn = { account.apply($0.me) }

        // Sign in with the server's fixed development code.
        let email = "desktop-e2e-\(UUID().uuidString.prefix(8).lowercased())@example.com"
        await auth.requestCode(email: email)
        #expect(auth.lastError == nil)
        #expect(await auth.verify(code: environment["AIREPLY_INTEGRATION_OTP"] ?? "1111"))
        #expect(auth.isSignedIn)

        // A new account has to accept the documents, as "macos".
        await account.refreshConfig(maxAge: 0)
        try await account.refreshMe()
        #expect(account.hasConsent == false)
        try await account.acceptConsent()
        #expect(account.hasConsent == true)
        let before = try #require(account.balance)
        print("balance after sign-in: \(before.remainingTotal) (\(before.planCode))")

        // Compose and reply through the real provider.
        let generator = ServerGenerator(client: client, configuration: configuration)
        let key = UUID().uuidString.lowercased()
        let composed = try await generator.compose("Напиши клиенту вежливо, что документы получили и завтра ответим",
                                                   regenerate: false, inputLanguage: "ru", idempotencyKey: key)
        print("compose: \(composed.text)")
        #expect(!composed.text.isEmpty)
        let charged = try #require(composed.usage?.remainingTotal)
        #expect(charged == before.remainingTotal - 1)

        // The same key again (a lost answer) is not charged a second time.
        let replayed = try await generator.compose("Напиши клиенту вежливо, что документы получили и завтра ответим",
                                                   regenerate: false, inputLanguage: "ru", idempotencyKey: key)
        #expect(replayed.usage?.remainingTotal == charged)

        let reply = try await generator.reply(to: "Сәлем! Ертең кездесеміз бе?", recipient: .friend, instruction: nil,
                                              inputLanguage: "kk", idempotencyKey: UUID().uuidString.lowercased())
        print("reply (\(reply.language ?? "?")): \(reply.text)")
        #expect(!reply.text.isEmpty)

        // Voice: a real recording through the server's transcription.
        if let folder = environment["AIREPLY_INTEGRATION_AUDIO"] {
            let transcriber = ServerTranscriber(client: client) { ["ru", "kk", "en"] }
            for (name, language) in [("ru.m4a", DictationLanguage.ru), ("kk.m4a", .kk)] {
                let url = URL(fileURLWithPath: folder).appendingPathComponent(name)
                guard FileManager.default.fileExists(atPath: url.path) else { continue }
                let audio = RecordedAudio(fileURL: url, duration: 3.5, contentType: "audio/mp4", fileName: "voice.m4a")
                let text = try await transcriber.transcribe(audio, language: language)
                print("transcribe \(name): \(text)")
                #expect(!text.isEmpty)
            }
        }

        // Sign out ends this session on the server: the old refresh token no longer works.
        let session = try #require(await client.session)
        await auth.signOut()
        #expect(sessions.load() == nil)
        struct Body: Encodable { let refresh_token: String }
        await #expect(throws: APIError.self) {
            try await client.send(.json("POST", "/api/v1/auth/refresh", Body(refresh_token: session.refreshToken),
                                        authenticated: false))
        }
    }
}
