import Foundation

/// Polishes an instruction through the authenticated `/api/v1/ai/polish`
/// endpoint.
///
/// Нұсқау серверде түзетіледі; күнделікті лимит жұмсалмайды.
///
/// The body is the instruction alone - no copied message, no profile, no
/// reply. It costs no daily quota; the server has its own rate limit, and
/// every failure is ignored by the caller.
struct AccountPolishTransport: PolishTransport {

    private let session: AccountSession

    init(session: AccountSession = .shared) {
        self.session = session
    }

    /// The wire format, exposed so tests can pin it without a server.
    struct Body: Encodable, Equatable {
        let text: String
        let input_language: String?
        let platform: String
        let app_version: String
    }

    struct Response: Decodable, Sendable {
        let text: String
        let changed: Bool
    }

    /// The endpoint only exists on a server that publishes
    /// `instruction_polish`, and it takes `input_language` from the start.
    static func body(for request: PolishService.Request, descriptor: DeviceDescriptor = .current) -> Body {
        Body(
            text: request.text,
            input_language: request.inputLanguage?.rawValue,
            platform: descriptor.platform,
            app_version: descriptor.app_version
        )
    }

    func polish(_ request: PolishService.Request) async throws -> PolishService.Result {
        guard let baseURL = AIConfiguration.shared.backendBaseURL else { throw AIReplyError.notConfigured }
        guard session.isSignedIn else { throw AIReplyError.authenticationFailed }

        let client = APIClient(baseURL: baseURL)
        let body = Self.body(for: request)
        do {
            let response: Response = try await session.authenticated { token in
                try await client.post("api/v1/ai/polish", body: body, token: token)
            }
            return PolishService.Result(text: response.text, changed: response.changed)
        } catch let error as APIError {
            throw AccountComposeTransport.map(error)
        }
    }
}
