import Foundation

/// Writes a NEW message from the user's description - the keyboard's "Create"
/// mode. The second AI flow, independent of replying:
///
/// * there is no incoming message: the clipboard is never read, nothing copied
///   is ever sent, and the reply prompt is not involved;
/// * the request carries the instruction - plus, for a server that knows
///   them, the keyboard layout and the user's grammatical gender - and the
///   server writes the message with its own compose prompt
///   (`POST /api/v1/ai/compose`);
/// * it spends the same daily quota as a reply, and fails with the same
///   closed set of `AIReplyError`s.
struct ComposeService: Sendable {

    struct Request: Sendable, Equatable {
        /// What the user wants written, in their own words. Raw as typed.
        var instruction: String
        /// The APP's language. The message itself follows the language of the
        /// instruction; the server uses this only when that is unclear.
        var uiLanguage: AppLanguage
        /// Another version of a message the user has already seen.
        var isRegeneration: Bool = false
        /// The keyboard LAYOUT active when Write was tapped: the language hint
        /// for an instruction too short to tell.
        var inputLanguage: KeyboardLanguage? = nil
        /// «Рад» or «рада» when the message speaks for the user in Russian.
        var grammaticalGender: GrammaticalGender? = nil
    }

    private let configuration: AIConfiguration
    private let transportOverride: (@Sendable (Request) -> ComposeTransport)?
    private let hasConsent: @Sendable () -> Bool

    init(
        configuration: AIConfiguration = .shared,
        transportOverride: (@Sendable (Request) -> ComposeTransport)? = nil,
        hasConsent: @escaping @Sendable () -> Bool = { LegalConsentStore.hasAcceptedCurrentVersions() }
    ) {
        self.configuration = configuration
        self.transportOverride = transportOverride
        self.hasConsent = hasConsent
    }

    // MARK: Validation

    /// The instruction rule: not empty, and within the limit the server
    /// publishes as `max_instruction_length` (400 unless an administrator
    /// changed it). Counted in Unicode scalars, like the server.
    static func validate(
        instruction: String,
        limit: Int = AILimits.current.instructionCharacters
    ) -> Result<String, AIReplyError> {
        let trimmed = instruction.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return .failure(.noInstruction) }
        guard trimmed.unicodeScalars.count <= limit else { return .failure(.instructionTooLong(limit: limit)) }
        return .success(trimmed)
    }

    // MARK: Generation

    func compose(_ request: Request) async throws -> GeneratedReply {
        var prepared = request
        switch Self.validate(instruction: request.instruction) {
        case .success(let value): prepared.instruction = value
        case .failure(let error): throw error
        }

        let transport: ComposeTransport
        if let override = transportOverride?(prepared) {
            transport = override
        } else {
            if let refusal = AIReplyService.preflight(
                isSignedIn: configuration.isReady && AccountSession.shared.isSignedIn,
                hasConsent: hasConsent()
            ) {
                throw refusal
            }
            transport = AccountComposeTransport(onUsage: { usage in AccountUsageCache.store(usage) })
        }

        do {
            let message = try await transport.compose(prepared)
            try Task.checkCancellation()
            return message
        } catch let error as AIReplyError {
            throw error
        } catch is CancellationError {
            throw AIReplyError.cancelled
        } catch {
            throw ReplyNetworking.mapURLError(error)
        }
    }
}

/// Where a composed message comes from: the backend, or a test double.
protocol ComposeTransport: Sendable {
    func compose(_ request: ComposeService.Request) async throws -> GeneratedReply
}

#if DEBUG
/// DEBUG ONLY: canned messages for the Simulator (`-AIReplyMockReplies`), so
/// Create can be tried without an account. Instruction tags simulate
/// failures, as in the reply mock: `#offline`, `#quota`, `#slow`.
struct DebugComposeMock: ComposeTransport {

    private static let lock = NSLock()
    private static var served = 0

    private static let messages = [
        "Уважаемый Сакен Бакпакбекович!\n\nОт всей души поздравляем Вас с 55-летним юбилеем! 🎉 Желаем крепкого здоровья, благополучия и новых профессиональных достижений.\n\nС юбилеем! 🎂👏",
        "Құрметті Күлжан апай!\n\nТуған күніңізбен шын жүректен құттықтаймыз! 🌷 Зор денсаулық, отбасыңызға амандық пен береке тілейміз.",
        "Hi team! Starting tomorrow the office opens at 10:00. Thanks, and see you then 👋"
    ]

    func compose(_ request: ComposeService.Request) async throws -> GeneratedReply {
        let tags = request.instruction.lowercased()
        try await Task.sleep(for: tags.contains("#slow") ? .seconds(6) : .milliseconds(700))
        try Task.checkCancellation()
        if tags.contains("#offline") { throw AIReplyError.offline }
        if tags.contains("#quota") { throw AIReplyError.quotaExhausted }

        Self.lock.lock()
        let index = Self.served % Self.messages.count
        Self.served += 1
        Self.lock.unlock()
        return GeneratedReply(text: Self.messages[index], detectedLanguage: nil)
    }
}
#endif
