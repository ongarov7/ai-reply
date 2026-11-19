import Foundation

/// The server's price of a screenshot reply (`/api/v1/ai/screenshot/estimate`,
/// and the `quote` of a reply or a refusal). Replies, not money: one standard
/// reply is the prompt of an ordinary reply plus a 400-character answer.
struct ScreenshotQuote: Codable, Equatable, Sendable {
    var mode: String
    var estimatedUnits: Int
    var maxUnits: Int
    var unlimited: Bool
    /// Replies that can be spent right now (absent on unlimited plans).
    var availableUnits: Int?
    /// Image analyses an unlimited plan has left today.
    var imagesLeftToday: Int?
    var inputTokens: Int
    var imageTokens: Int?
    var maxOutputTokens: Int
    var tokensCounted: Bool?
    var policyVersion: String

    enum CodingKeys: String, CodingKey {
        case mode, unlimited
        case estimatedUnits = "estimated_units"
        case maxUnits = "max_units"
        case availableUnits = "available_units"
        case imagesLeftToday = "images_left_today"
        case inputTokens = "input_tokens"
        case imageTokens = "image_tokens"
        case maxOutputTokens = "max_output_tokens"
        case tokensCounted = "tokens_counted"
        case policyVersion = "policy_version"
    }

    /// The balance cannot pay the most this reply may cost.
    var exceedsBalance: Bool {
        guard !unlimited, let availableUnits else { return false }
        return maxUnits > availableUnits
    }

    /// Worth asking before charging: it may cost more than an ordinary reply.
    var needsConfirmation: Bool { !unlimited && maxUnits > 1 }
}

/// Text recognised on the iPhone, or the screenshot itself (image mode).
enum ScreenshotReplyMode: String, Codable, Sendable {
    case text
    case image
}

/// One estimate or reply request.
struct ScreenshotReplyRequest: Sendable {
    var mode: ScreenshotReplyMode
    var conversation: ScreenshotConversation
    /// The screenshot, JPEG (image mode, reply only).
    var image: Data?
    var imageWidth: Int = 0
    var imageHeight: Int = 0
    var instruction: String = ""
    var instructionIsVoice: Bool = false
    var template: ReplyTemplate
    var configuration: ReplyConfiguration
    var uiLanguage: AppLanguage
    var inputLanguage: KeyboardLanguage? = nil
    /// A reply language chosen for this reply only; nil keeps the profile's.
    var replyLanguage: ReplyLanguagePreference? = nil
    /// The most the person approved (`max_units`).
    var approvedUnits: Int = 1
}

/// A finished screenshot reply.
struct ScreenshotReplyResult: Decodable, Sendable {
    let reply: String
    let detectedLanguage: String?
    let usage: AccountAPI.Usage
    /// Replies the server charged (0: a free replay of the same action).
    let units: Int
    let refundedUnits: Int
    let quote: ScreenshotQuote

    enum CodingKeys: String, CodingKey {
        case reply, usage, units, quote
        case detectedLanguage = "detected_language"
        case refundedUnits = "refunded_units"
    }
}

/// Why a screenshot reply did not happen. Nothing was charged in any case.
enum ScreenshotReplyFailure: Error, Equatable, Sendable {
    /// It may cost more than approved: ask with this price.
    case confirmationRequired(ScreenshotQuote)
    /// The balance cannot pay the maximum.
    case insufficientCredits(ScreenshotQuote, canBuy: Bool)
    /// Image mode needs the in-app consent first.
    case imageConsentRequired
    /// Image analysis is switched off on the server.
    case imageUnavailable
    /// An unlimited plan used today's image analyses.
    case imageLimitReached(limit: Int)
    /// The server could not use the recognised chat (too long or empty).
    case unreadable
    /// Everything else, in the closed set the UI already shows.
    case failed(AIReplyError)
}

/// `/api/v1/ai/screenshot/*` with the session of the signed-in account. Only
/// the server is contacted; no provider key exists on the device.
enum ScreenshotReplyClient {

    /// The request body (testable without the network).
    struct Body: Encodable {
        struct Image: Encodable {
            let data: String?
            let width: Int
            let height: Int
        }
        let mode: String
        let conversation: ScreenshotConversation.Wire
        let image: Image?
        let image_consent: String?
        let instruction: String?
        let instruction_source: String?
        let language: String
        let input_language: String?
        let template_id: String
        let template: AccountReplyTransport.Template
        let profile: AccountReplyTransport.ProfileBlock?
        let max_units: Int?
        let platform: String
        let app_version: String
    }

    /// The disclosure the person accepted before a screenshot itself may be
    /// sent; the server publishes the current one.
    static var imageConsentVersion: String { ScreenshotFeature.imageConsentVersion }

    static func body(for request: ScreenshotReplyRequest, includeImage: Bool,
                     descriptor: DeviceDescriptor = .current) -> Body {
        var configuration = request.configuration
        if let language = request.replyLanguage { configuration.profile.replyLanguage = language }
        let template = request.template
        let conversation = request.conversation.trimmedToServerLimits()
        let base = AccountReplyTransport.body(for: AccountReplyTransport.RequestContext(
            message: conversation.target?.text ?? "",
            instruction: ReplyInstruction.prepare(request.instruction, voice: request.instructionIsVoice),
            instructionIsVoice: request.instructionIsVoice,
            templateID: template.id,
            templateName: template.displayName(appLanguage: request.uiLanguage),
            templateRelationship: template.relationship.rawValue,
            templateTone: template.tone,
            templateInstructions: template.instructions,
            templateReplyLength: template.replyLength,
            templateEmojiPolicy: template.emojiPolicy,
            templateWorkingHoursBehaviour: template.workingHoursBehaviour,
            templateBusiness: template.effectiveBusiness,
            appLanguage: request.uiLanguage.rawValue,
            inputLanguage: request.inputLanguage?.rawValue,
            business: nil,
            profile: AIReplyService.profile(from: configuration.profile)
        ), descriptor: descriptor)
        var image: Body.Image?
        if request.mode == .image {
            let data = includeImage ? request.image?.base64EncodedString() : nil
            image = Body.Image(data: data, width: request.imageWidth, height: request.imageHeight)
        }
        return Body(
            mode: request.mode.rawValue,
            conversation: conversation.wire,
            image: image,
            image_consent: request.mode == .image && includeImage ? imageConsentVersion : nil,
            instruction: base.instruction,
            instruction_source: base.instruction_source,
            language: base.language,
            input_language: base.input_language,
            template_id: base.template_id,
            template: base.template,
            profile: base.profile,
            max_units: max(1, request.approvedUnits),
            platform: base.platform,
            app_version: base.app_version
        )
    }

    // MARK: Calls

    static func estimate(_ request: ScreenshotReplyRequest) async throws -> ScreenshotQuote {
        try await post("api/v1/ai/screenshot/estimate", body: body(for: request, includeImage: false), key: nil, timeout: 20)
    }

    /// The reply. One key per tap on Generate: an automatic retry of the same
    /// tap is never charged twice.
    static func reply(_ request: ScreenshotReplyRequest,
                      key: IdempotencyKey? = IdempotencyKey.forAction()) async throws -> ScreenshotReplyResult {
        let timeout: TimeInterval = request.mode == .image ? 75 : 45
        let result: ScreenshotReplyResult = try await post("api/v1/ai/screenshot/reply",
                                                           body: body(for: request, includeImage: true),
                                                           key: key, timeout: timeout)
        AccountUsageCache.store(result.usage)
        return result
    }

    private static func post<Response: Decodable & Sendable>(_ path: String, body: Body, key: IdempotencyKey?,
                                                              timeout: TimeInterval) async throws -> Response {
        guard let baseURL = AIConfiguration.shared.backendBaseURL else {
            throw ScreenshotReplyFailure.failed(.notConfigured)
        }
        guard AccountSession.shared.isSignedIn else { throw ScreenshotReplyFailure.failed(.authenticationFailed) }
        let payload: Data
        do { payload = try JSONEncoder().encode(body) } catch { throw ScreenshotReplyFailure.failed(.serviceUnavailable) }
        let session = ReplyNetworking.makeSession(timeout: timeout)
        defer { session.finishTasksAndInvalidate() }
        do {
            return try await IdempotentGeneration.run(key: key) { headers in
                try await AccountSession.shared.authenticated { token in
                    var request = URLRequest(url: baseURL.appendingPathComponent(path))
                    request.httpMethod = "POST"
                    request.httpBody = payload
                    request.setValue("application/json", forHTTPHeaderField: "Content-Type")
                    request.setValue("application/json", forHTTPHeaderField: "Accept")
                    request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
                    for (name, value) in headers { request.setValue(value, forHTTPHeaderField: name) }
                    let data: Data
                    let response: URLResponse
                    do {
                        (data, response) = try await session.data(for: request)
                    } catch {
                        throw APIClient.mapTransportError(error)
                    }
                    guard let http = response as? HTTPURLResponse else { throw APIError.malformedResponse }
                    guard (200..<300).contains(http.statusCode) else {
                        if let failure = screenshotFailure(data) { throw failure }
                        throw APIClient.mapServerError(status: http.statusCode, data: data, headers: http)
                    }
                    do { return try JSONDecoder().decode(Response.self, from: data) }
                    catch { throw APIError.malformedResponse }
                }
            }
        } catch let failure as ScreenshotReplyFailure {
            throw failure
        } catch let error as APIError {
            AccountReplyTransport.noteRefusal(error)
            throw failure(for: error)
        } catch is CancellationError {
            throw ScreenshotReplyFailure.failed(.cancelled)
        } catch {
            throw ScreenshotReplyFailure.failed(ReplyNetworking.mapURLError(error))
        }
    }

    /// The screenshot-only refusals; nil for everything the usual mapping knows.
    static func screenshotFailure(_ data: Data) -> ScreenshotReplyFailure? {
        struct Envelope: Decodable {
            struct Payload: Decodable {
                let code: String
                let details: Details?
            }
            struct Details: Decodable {
                let canBuy: Bool?
                let dailyLimit: Int?
                enum CodingKeys: String, CodingKey {
                    case canBuy = "can_buy"
                    case dailyLimit = "daily_limit"
                }
            }
            let error: Payload
        }
        struct QuoteEnvelope: Decodable {
            struct Payload: Decodable { let details: ScreenshotQuote? }
            let error: Payload
        }
        guard let envelope = try? JSONDecoder().decode(Envelope.self, from: data) else { return nil }
        let quote = (try? JSONDecoder().decode(QuoteEnvelope.self, from: data))?.error.details
        switch envelope.error.code {
        case "CREDIT_CONFIRMATION_REQUIRED":
            return quote.map { .confirmationRequired($0) }
        case "INSUFFICIENT_CREDITS":
            return quote.map { .insufficientCredits($0, canBuy: envelope.error.details?.canBuy ?? false) }
        case "SCREENSHOT_CONSENT_REQUIRED":
            return .imageConsentRequired
        case "SCREENSHOT_IMAGE_UNAVAILABLE":
            return .imageUnavailable
        case "SCREENSHOT_IMAGE_LIMIT_REACHED":
            return .imageLimitReached(limit: envelope.error.details?.dailyLimit ?? 0)
        default:
            return nil
        }
    }

    /// The reply mapping, with the screenshot's own wording for a chat the
    /// server could not take.
    static func failure(for error: APIError) -> ScreenshotReplyFailure {
        switch error {
        case .invalidRequest, .sourceTooLong, .instructionTooLong, .instructionMissing:
            return .unreadable
        default:
            return .failed(AccountReplyTransport.map(error))
        }
    }
}

/// What the server published about screenshot replies (`/api/v1/config`),
/// kept in the App Group for the keyboard.
enum ScreenshotFeature {
    private enum Key {
        static let enabled = "screenshot.feature.enabled"
        static let image = "screenshot.feature.image"
        static let consentVersion = "screenshot.feature.consentVersion"
        static let shortcutURL = "screenshot.feature.shortcutURL"
        static let imagesPerDay = "screenshot.feature.unlimitedImagesPerDay"
    }

    /// The version this build was written against, used until the server says.
    static let bundledConsentVersion = "2026-11-19-screenshot-v1"

    static var isAvailable: Bool { AppGroup.defaults.bool(forKey: Key.enabled) }
    static var imageAvailable: Bool { AppGroup.defaults.bool(forKey: Key.image) }

    static var imageConsentVersion: String {
        AppGroup.defaults.string(forKey: Key.consentVersion).flatMap { $0.isEmpty ? nil : $0 } ?? bundledConsentVersion
    }

    /// The ready-made capture shortcut (an iCloud link), when the owner set one.
    static var shortcutURL: URL? {
        guard let raw = AppGroup.defaults.string(forKey: Key.shortcutURL), let url = URL(string: raw),
              url.scheme == "https", url.host == "www.icloud.com", url.path.hasPrefix("/shortcuts/") else { return nil }
        return url
    }

    static var unlimitedImagesPerDay: Int { AppGroup.defaults.integer(forKey: Key.imagesPerDay) }

    static func store(features: AccountAPI.Features?, config: AccountAPI.ScreenshotConfig?,
                      defaults: UserDefaults = AppGroup.defaults) {
        defaults.set(features?.screenshotReply ?? false, forKey: Key.enabled)
        defaults.set((features?.screenshotImage ?? false) && (config?.imageEnabled ?? true), forKey: Key.image)
        defaults.set(config?.imageConsentVersion ?? "", forKey: Key.consentVersion)
        defaults.set(config?.shortcutURL ?? "", forKey: Key.shortcutURL)
        defaults.set(config?.unlimitedImagesPerDay ?? 0, forKey: Key.imagesPerDay)
    }
}
