import AppKit
import Foundation
import Testing
@testable import AirReplyCore
@testable import AirReplySystem
@testable import AirReplyUI
@testable import AirReplyVoice

// MARK: - Fakes

final class FakeGenerator: Generating, @unchecked Sendable {
    enum Call: Equatable {
        case compose(instruction: String, regenerate: Bool, key: String)
        case reply(source: String, recipient: Recipient, instruction: String?, key: String)
    }

    private let lock = NSLock()
    private var recorded: [Call] = []
    private var outcomes: [Result<GenerationResult, GenerationError>]

    init(_ outcomes: [Result<GenerationResult, GenerationError>]) {
        self.outcomes = outcomes
    }

    var calls: [Call] { lock.withLock { recorded } }

    private func next(_ call: Call) throws(GenerationError) -> GenerationResult {
        let outcome: Result<GenerationResult, GenerationError> = lock.withLock {
            recorded.append(call)
            return outcomes.isEmpty ? .failure(.server) : outcomes.removeFirst()
        }
        return try outcome.get()
    }

    func reply(to source: String, recipient: Recipient, instruction: String?, inputLanguage: String?,
               idempotencyKey: String) async throws(GenerationError) -> GenerationResult {
        try next(.reply(source: source, recipient: recipient, instruction: instruction, key: idempotencyKey))
    }

    func compose(_ instruction: String, regenerate: Bool, inputLanguage: String?,
                 idempotencyKey: String) async throws(GenerationError) -> GenerationResult {
        try next(.compose(instruction: instruction, regenerate: regenerate, key: idempotencyKey))
    }
}

@MainActor
final class FakePasteboard: PasteboardAccess {
    var changeCount = 0
    var types: [NSPasteboard.PasteboardType] = []
    var isAccessDenied = false
    var contents: String?

    func readString() -> String? { contents }

    func writeString(_ string: String) -> Int {
        contents = string
        types = [.string]
        changeCount += 1
        return changeCount
    }
}

final class SilentTranscriber: Transcribing {
    func transcribe(_ audio: RecordedAudio, language: DictationLanguage) async throws(VoiceError) -> String { "" }
}

@MainActor
final class DeniedMicrophone: MicrophonePermission {
    var status: MicrophoneStatus { .denied }
    func request() async -> Bool { false }
}

final class BackendStub: HTTPTransport, @unchecked Sendable {
    private let lock = NSLock()
    private var paths: [String] = []
    var hits: [String] { lock.withLock { paths } }

    func send(_ request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        let path = request.url!.path
        lock.withLock { paths.append(path) }
        let body: String = switch path {
        case let p where p.hasSuffix("/config"): #"{"max_source_characters":400,"max_instruction_length":400,"features":{"voice_transcription":true}}"#
        case let p where p.hasSuffix("/me"): #"{"user":{"id":"u-1"},"legal_consent":{"terms_version":"t","privacy_version":"p"}}"#
        default: "{}"
        }
        return (Data(body.utf8), HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: nil, headerFields: nil)!)
    }
}

// MARK: - Tests

@Suite("Quick bar")
@MainActor
struct QuickBarModelTests {
    struct Environment {
        let model: QuickBarModel
        let generator: FakeGenerator
        let pasteboard: FakePasteboard
        let backend: BackendStub
        let account: AccountStore
        let auth: AuthSession
        let settings: AppSettings
    }

    final class Clock { var now = Date(timeIntervalSince1970: 1_800_000_000) }

    func makeEnvironment(_ outcomes: [Result<GenerationResult, GenerationError>], consent: Bool = true,
                         clock: Clock = Clock()) throws -> Environment {
        let configuration = AppConfiguration(backendBaseURL: URL(string: "https://ai-reply.kz")!, appVersion: "1.0.0", build: "1")
        let sessions = SessionStore(store: InMemorySecureStore())
        try sessions.save(SessionTokens(accessToken: "a", refreshToken: "r", accessExpiresAt: .distantFuture,
                                        refreshExpiresAt: nil, deviceID: "d", userID: "u-1", email: "a@b.kz"))
        let backend = BackendStub()
        let client = APIClient(configuration: configuration, transport: backend, sessions: sessions)
        let auth = AuthSession(client: client, restoredEmail: "a@b.kz", isSignedIn: true)
        let account = AccountStore(client: client, configuration: configuration)
        account.apply(MeResponse(user: try JSONDecoder.api.decode(UserDTO.self, from: Data(#"{"id":"u-1"}"#.utf8)),
                                 subscription: nil, usage: nil,
                                 legalConsent: consent ? try JSONDecoder.api.decode(LegalConsentDTO.self,
                                     from: Data(#"{"terms_version":"t","privacy_version":"p"}"#.utf8)) : nil))
        let pasteboard = FakePasteboard()
        let clipboard = ClipboardMonitor(pasteboard: pasteboard)
        let voice = VoiceController(transcriber: SilentTranscriber(), permission: DeniedMicrophone())
        let settings = AppSettings(defaults: UserDefaults(suiteName: "test-\(UUID().uuidString)")!)
        let generator = FakeGenerator(outcomes)
        let model = QuickBarModel(auth: auth, account: account, voice: voice, generator: generator,
                                  clipboard: clipboard, settings: settings, now: { clock.now })
        return Environment(model: model, generator: generator, pasteboard: pasteboard, backend: backend,
                           account: account, auth: auth, settings: settings)
    }

    static func output(_ text: String) -> Result<GenerationResult, GenerationError> {
        .success(GenerationResult(text: text, language: "ru", usage: nil))
    }

    func waitUntil(_ condition: () -> Bool) async {
        for _ in 0..<200 where !condition() {
            try? await Task.sleep(for: .milliseconds(5))
        }
    }

    @Test func composeShowsTheResult() async throws {
        let env = try makeEnvironment([Self.output("Привет! Давай завтра погуляем?")])
        env.model.draft = "  напиши другу, что завтра погуляем  "
        #expect(env.model.canSubmit)
        env.model.submit()
        #expect(env.model.phase == .generating)
        await waitUntil { env.model.phase == .result }
        #expect(env.model.result == "Привет! Давай завтра погуляем?")
        guard case let .compose(instruction, regenerate, key) = env.generator.calls.first else {
            Issue.record("no compose call"); return
        }
        #expect(instruction == "напиши другу, что завтра погуляем")
        #expect(!regenerate)
        #expect(key.range(of: "^[A-Za-z0-9-]{8,64}$", options: .regularExpression) != nil)
    }

    @Test func nothingToSendWhenEmptyOrTooLong() throws {
        let env = try makeEnvironment([])
        #expect(!env.model.canSubmit)
        env.model.draft = String(repeating: "а", count: 401)
        #expect(!env.model.canSubmit)
        env.model.draft = String(repeating: "а", count: 400)
        #expect(env.model.canSubmit)
    }

    @Test func copiedMessageRepliesWithOneClick() async throws {
        let env = try makeEnvironment([Self.output("Да, документы получили.")])
        let copied = CopiedText(text: "Документы получили?", changeCount: 3, observedAt: .now)
        env.model.panelDidOpen(copied: .text(copied))
        #expect(env.model.copiedText == copied)
        env.model.reply(to: .work)
        await waitUntil { env.model.phase == .result }
        #expect(env.generator.calls == [.reply(source: "Документы получили?", recipient: .work, instruction: nil,
                                              key: keyOf(env.generator.calls.first))])
        #expect(env.settings.lastRecipient == "work")
        #expect(env.model.result == "Да, документы получили.")
    }

    @Test func tooLongCopiedMessageIsNotSent() throws {
        let env = try makeEnvironment([])
        env.model.panelDidOpen(copied: .text(CopiedText(text: String(repeating: "я", count: 401), changeCount: 1, observedAt: .now)))
        #expect(env.model.sourceTooLong)
        #expect(!env.model.canSubmit)
        env.model.reply(to: .friend)
        #expect(env.generator.calls.isEmpty)
    }

    @Test func retryReusesTheKeyAndRegenerateDoesNot() async throws {
        let env = try makeEnvironment([.failure(.offline), Self.output("Первый"), Self.output("Второй")])
        env.model.draft = "поздравь коллегу"
        env.model.submit()
        await waitUntil { env.model.issue != nil }
        #expect(env.model.issue == .generation(.offline))
        env.model.retry()
        await waitUntil { env.model.phase == .result }
        env.model.regenerate()
        await waitUntil { env.model.result == "Второй" }

        let keys = env.generator.calls.map(keyOf)
        #expect(keys.count == 3)
        #expect(keys[0] == keys[1], "a retry after a lost answer must not be charged twice")
        #expect(keys[2] != keys[1])
        if case let .compose(_, regenerate, _) = env.generator.calls[2] { #expect(regenerate) }
    }

    @Test func limitReachedAsksForAFreshBalance() async throws {
        let env = try makeEnvironment([.failure(.limitReached(resetsAt: nil))])
        env.model.draft = "x"
        env.model.submit()
        await waitUntil { env.backend.hits.contains { $0.hasSuffix("/api/v1/me") } }
        #expect(env.model.issue == .generation(.limitReached(resetsAt: nil)))
        #expect(env.model.phase == .editing)
    }

    @Test func endedSessionSignsOut() async throws {
        let env = try makeEnvironment([.failure(.signedOut)])
        env.model.draft = "x"
        env.model.submit()
        await waitUntil { !env.auth.isSignedIn }
        #expect(env.auth.state == .signedOut)
        #expect(!env.model.canSubmit)
    }

    @Test func copyPutsTheReplyOnThePasteboardThenCloses() async throws {
        let env = try makeEnvironment([Self.output("  Готово!  ")])
        var copiedEvents = 0
        env.model.didCopy = { copiedEvents += 1 }
        env.model.draft = "x"
        env.model.submit()
        await waitUntil { env.model.phase == .result }
        env.model.copyResult()
        #expect(env.pasteboard.contents == "Готово!")
        #expect(env.model.justCopied)
        await waitUntil { copiedEvents == 1 }
        #expect(!env.model.justCopied)
    }

    @Test func escapeCancelsGenerationThenCloses() throws {
        let env = try makeEnvironment([])
        var closed = 0
        env.model.requestClose = { closed += 1 }
        env.model.draft = "x"
        env.model.submit()
        env.model.escape()
        #expect(env.model.phase == .editing)
        #expect(closed == 0)
        env.model.escape()
        #expect(closed == 1)
    }

    @Test func consentGatesGeneration() throws {
        let env = try makeEnvironment([], consent: false)
        #expect(env.model.needsConsent)
        env.model.draft = "x"
        #expect(!env.model.canSubmit)
    }

    @Test func draftExpiresAfterTenMinutesClosed() throws {
        let clock = Clock()
        let env = try makeEnvironment([], clock: clock)
        env.model.draft = "черновик"
        env.model.panelDidClose()
        clock.now += 5 * 60
        env.model.panelDidOpen(copied: .none)
        #expect(env.model.draft == "черновик")
        env.model.panelDidClose()
        clock.now += 11 * 60
        env.model.panelDidOpen(copied: .none)
        #expect(env.model.draft.isEmpty)
    }

    @Test func dismissingTheCopiedMessageReturnsToCompose() throws {
        let env = try makeEnvironment([])
        env.model.panelDidOpen(copied: .text(CopiedText(text: "Привет", changeCount: 1, observedAt: .now)))
        env.model.dismissCopied()
        #expect(env.model.mode == .compose)
    }

    @Test func voiceUnavailableWhenTheServerSaysSo() throws {
        let env = try makeEnvironment([])
        #expect(!env.model.voiceAvailable)
    }

    private func keyOf(_ call: FakeGenerator.Call?) -> String {
        switch call {
        case let .compose(_, _, key)?, let .reply(_, _, _, key)?: key
        case nil: ""
        }
    }
}

@Suite("Transcription upload")
struct TranscriberTests {
    @Test func languageHints() {
        #expect(ServerTranscriber.hints(setting: .auto, keyboard: "kk") == ["kk", "ru", "en"])
        #expect(ServerTranscriber.hints(setting: .en, keyboard: "ru") == ["en", "ru", "kk"])
        #expect(ServerTranscriber.hints(setting: .auto, keyboard: "de") == ["ru", "kk", "en"])
    }

    @Test(arguments: [
        (APIError.offline, VoiceError.offline),
        (.server(status: 404, code: "NOT_FOUND", message: "", details: [:], retryAfter: nil), .unavailable),
        (.server(status: 429, code: "VOICE_LIMIT_REACHED", message: "", details: [:], retryAfter: nil), .limitReached),
        (.server(status: 429, code: "RATE_LIMITED", message: "", details: [:], retryAfter: 5), .rateLimited),
        (.server(status: 400, code: "INVALID_REQUEST", message: "", details: ["reason": .string("too_short")], retryAfter: nil), .tooShort),
        (.server(status: 403, code: "CONSENT_REQUIRED", message: "", details: [:], retryAfter: nil), .server(code: "CONSENT_REQUIRED")),
    ])
    func errorMapping(error: APIError, expected: VoiceError) {
        #expect(ServerTranscriber.voiceError(error) == expected)
    }
}
