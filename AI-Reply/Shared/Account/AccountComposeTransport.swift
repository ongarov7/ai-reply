import Foundation

/// Writes a message through the authenticated `/api/v1/ai/compose` endpoint.
///
/// Хабарлама серверде жазылады: құрылғыда провайдер кілті жоқ.
///
/// The body is the instruction - no copied text, no contacts, and of the
/// profile only the grammatical gender, for a server that asked for it. The
/// response carries quota state like a reply does.
struct AccountComposeTransport: ComposeTransport {

    private let session: AccountSession
    private let onUsage: (@Sendable (AccountAPI.Usage) -> Void)?

    init(session: AccountSession = .shared, onUsage: (@Sendable (AccountAPI.Usage) -> Void)? = nil) {
        self.session = session
        self.onUsage = onUsage
    }

    /// The wire format, exposed so tests can pin it without a server.
    struct Body: Encodable, Equatable {
        let instruction: String
        let language: String
        let regenerate: Bool
        let input_language: String?
        let profile: Profile?
        let platform: String
        let app_version: String

        struct Profile: Encodable, Equatable {
            let grammatical_gender: String
        }
    }

    /// `input_language` and `profile` only go to a server that publishes
    /// `sender_profile`: an older one rejects unknown fields outright.
    static func body(
        for request: ComposeService.Request,
        descriptor: DeviceDescriptor = .current,
        serverSupportsSenderProfile: Bool = AILimits.serverSupportsSenderProfile
    ) -> Body {
        let gender = serverSupportsSenderProfile ? request.grammaticalGender : nil
        return Body(
            instruction: request.instruction,
            language: request.uiLanguage.rawValue,
            regenerate: request.isRegeneration,
            input_language: serverSupportsSenderProfile ? request.inputLanguage?.rawValue : nil,
            profile: gender.map { Body.Profile(grammatical_gender: $0.rawValue) },
            platform: descriptor.platform,
            app_version: descriptor.app_version
        )
    }

    func compose(_ request: ComposeService.Request) async throws -> GeneratedReply {
        guard let baseURL = AIConfiguration.shared.backendBaseURL else { throw AIReplyError.notConfigured }
        guard session.isSignedIn else { throw AIReplyError.authenticationFailed }

        let client = APIClient(baseURL: baseURL)
        let body = Self.body(for: request)
        do {
            let response: AccountAPI.ComposeResponse = try await session.authenticated { token in
                try await client.post("api/v1/ai/compose", body: body, token: token)
            }
            onUsage?(response.usage)
            let text = ReplyNetworking.unwrapQuotes(response.text.trimmingCharacters(in: .whitespacesAndNewlines))
            guard !text.isEmpty else { throw AIReplyError.emptyResponse }
            return GeneratedReply(text: text, detectedLanguage: response.detectedLanguage)
        } catch let error as APIError {
            throw Self.map(error)
        }
    }

    /// Same closed set as replies; only the two request-shape errors differ,
    /// because here the user's text is the instruction, not a copied message.
    static func map(_ error: APIError) -> AIReplyError {
        switch error {
        case .instructionTooLong(let limit): return .instructionTooLong(limit: limit)
        case .instructionMissing:            return .noInstruction
        // A server without the endpoint (not deployed yet) or a request it
        // could not read: nothing the user can fix by rewording.
        case .notFound, .invalidRequest, .sourceTooLong: return .serviceUnavailable
        default: return AccountReplyTransport.map(error)
        }
    }
}
