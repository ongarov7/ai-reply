import Foundation

/// Who the reply is for. Raw values are the server's built-in templates
/// (`template_id`); colleague and partner are `work` and `business` there.
public enum Recipient: String, CaseIterable, Codable, Sendable {
    case friend
    case client
    case work
    case business
}

public struct GenerationResult: Equatable, Sendable {
    public let text: String
    /// Language of the generated text when the server is sure (kk, ru, en, uz).
    public let language: String?
    /// Balance after this generation.
    public let usage: UsageDTO?
}

public enum GenerationError: Error, Equatable, Sendable {
    case signedOut
    case consentRequired
    case accountDisabled
    case sourceTooLong(limit: Int)
    case instructionTooLong(limit: Int)
    case instructionMissing
    /// Every bucket is empty until `resetsAt` (or until a purchase).
    case limitReached(resetsAt: Date?)
    case rateLimited(retryAfter: TimeInterval?)
    /// The AI provider failed or timed out; nothing was charged.
    case aiUnavailable
    case offline
    case server

    init(_ error: APIError) {
        switch error {
        case .signedOut: self = .signedOut
        case .offline, .cancelled: self = .offline
        case .invalidResponse: self = .server
        case let .server(_, code, _, details, retryAfter):
            switch code {
            case APIErrorCode.consentRequired: self = .consentRequired
            case APIErrorCode.accountDisabled: self = .accountDisabled
            case APIErrorCode.unauthorized, APIErrorCode.tokenExpired: self = .signedOut
            case APIErrorCode.dailyLimit, APIErrorCode.monthlyLimit, APIErrorCode.paymentRequired, APIErrorCode.subscriptionExpired:
                self = .limitReached(resetsAt: details["resets_at"]?.stringValue.flatMap(RFC3339.parse))
            case APIErrorCode.rateLimited: self = .rateLimited(retryAfter: retryAfter)
            case APIErrorCode.providerTimeout, APIErrorCode.providerUnavailable, APIErrorCode.emptyCompletion:
                self = .aiUnavailable
            case APIErrorCode.invalidRequest:
                let limit = details["max_characters"]?.intValue
                switch (details["field"]?.stringValue, limit) {
                case let ("source_text", limit?): self = .sourceTooLong(limit: limit)
                case let ("instruction", limit?): self = .instructionTooLong(limit: limit)
                case ("instruction", nil): self = .instructionMissing
                default: self = .server
                }
            default: self = .server
            }
        }
    }
}

/// Writes replies and messages. One `idempotencyKey` per user action: a retry
/// with the same key after a lost answer is never charged twice.
public protocol Generating: Sendable {
    func reply(to source: String, recipient: Recipient, instruction: String?, inputLanguage: String?,
               idempotencyKey: String) async throws(GenerationError) -> GenerationResult
    func compose(_ instruction: String, regenerate: Bool, inputLanguage: String?,
                 idempotencyKey: String) async throws(GenerationError) -> GenerationResult
}

public struct ServerGenerator: Generating {
    private let client: APIClient
    private let configuration: AppConfiguration

    public init(client: APIClient, configuration: AppConfiguration) {
        self.client = client
        self.configuration = configuration
    }

    public func reply(to source: String, recipient: Recipient, instruction: String?, inputLanguage: String?,
                      idempotencyKey: String) async throws(GenerationError) -> GenerationResult {
        struct Body: Encodable {
            let source_text: String
            let instruction: String?
            let language: String
            let input_language: String?
            let template_id: String
            let platform: String
            let app_version: String
        }
        struct Response: Decodable, Sendable {
            let reply: String
            let detected_language: String?
            let usage: UsageDTO?
        }
        let trimmed = instruction?.trimmingCharacters(in: .whitespacesAndNewlines)
        let body = Body(source_text: source, instruction: trimmed?.isEmpty == false ? trimmed : nil,
                        language: APIClient.preferredLanguage(), input_language: Self.inputLanguage(inputLanguage),
                        template_id: recipient.rawValue, platform: configuration.platform, app_version: configuration.appVersion)
        do {
            let response = try await client.send(.json("POST", "/api/v1/ai/reply", body, idempotencyKey: idempotencyKey, timeout: 45),
                                                 as: Response.self)
            return GenerationResult(text: response.reply, language: response.detected_language, usage: response.usage)
        } catch {
            throw GenerationError(error)
        }
    }

    public func compose(_ instruction: String, regenerate: Bool, inputLanguage: String?,
                        idempotencyKey: String) async throws(GenerationError) -> GenerationResult {
        struct Body: Encodable {
            let instruction: String
            let language: String
            let regenerate: Bool
            let input_language: String?
            let platform: String
            let app_version: String
        }
        struct Response: Decodable, Sendable {
            let text: String
            let detected_language: String?
            let usage: UsageDTO?
        }
        let body = Body(instruction: instruction, language: APIClient.preferredLanguage(), regenerate: regenerate,
                        input_language: Self.inputLanguage(inputLanguage), platform: configuration.platform,
                        app_version: configuration.appVersion)
        do {
            let response = try await client.send(.json("POST", "/api/v1/ai/compose", body, idempotencyKey: idempotencyKey, timeout: 45),
                                                 as: Response.self)
            return GenerationResult(text: response.text, language: response.detected_language, usage: response.usage)
        } catch {
            throw GenerationError(error)
        }
    }

    /// The server recognises only kk, ru and en here; anything else is left out.
    static func inputLanguage(_ raw: String?) -> String? {
        guard let code = raw?.prefix(2).lowercased(), ["kk", "ru", "en"].contains(code) else { return nil }
        return code
    }
}
