import Foundation

/// Generates a reply through the authenticated `/api/v1/ai/reply` endpoint.
///
/// Жауап серверде жасалады: құрылғыда провайдер кілті жоқ.
///
/// Requests are attributed to an account and the response carries quota state,
/// so the app can update the remaining count without another round trip.
struct AccountReplyTransport: ReplyTransport {

    /// What this one reply needs. No device identifier, no contacts, no chat
    /// history, no location: only the message being answered and how to answer it.
    struct RequestContext: Sendable {
        var message: String
        /// What the user asked for in this particular reply, if anything.
        var instruction: String = ""
        var templateID: String
        var templateName: String
        var templateRelationship: String
        var templateTone: ReplyTone
        var templateInstructions: String
        var templateReplyLength: ReplyLength
        var templateEmojiPolicy: EmojiPolicy
        var templateWorkingHoursBehaviour: WorkingHoursBehaviour
        var templateBusiness: BusinessContext?
        /// The app's language, used for logging and template naming only. The
        /// reply's language follows the incoming message unless the user set a
        /// preference (`replyLanguage`).
        var appLanguage: String
        /// The keyboard layout ("kk" / "ru" / "en") active when Reply was
        /// tapped, if the request came from the keyboard.
        var inputLanguage: String? = nil
        var business: WorkingHours.Context?
        /// The user's own profile as edited in the app. Sent with every
        /// request because the server copy is only written at registration:
        /// without this, edits made later never reached a single reply.
        var profile: Profile?
    }

    /// The personalisation a reply may use. Short, structured, and only what
    /// the user entered in the app.
    struct Profile: Sendable, Equatable {
        var description: String
        var role: String
        var preferredTone: ReplyTone
        var business: BusinessContext
        /// "kk" / "ru" / "en" / "uz", or nil to answer in the language of the
        /// incoming message. Only sent to a server that understands it.
        var replyLanguage: String?
        /// «Рад» or «рада»; nil when the user was never asked. Only sent to a
        /// server that understands it.
        var grammaticalGender: GrammaticalGender? = nil
    }

    private let context: RequestContext
    private let session: AccountSession
    private let onUsage: (@Sendable (AccountAPI.Usage) -> Void)?

    init(
        context: RequestContext,
        session: AccountSession = .shared,
        onUsage: (@Sendable (AccountAPI.Usage) -> Void)? = nil
    ) {
        self.context = context
        self.session = session
        self.onUsage = onUsage
    }

    // MARK: Wire format

    struct Business: Encodable {
        let offering: String?
        let summary: String?
        let rules: [String]?

        init?(_ source: BusinessContext?) {
            guard let source, !source.isEmpty else { return nil }
            let offering = source.offering.trimmingCharacters(in: .whitespacesAndNewlines)
            let summary = source.summary.trimmingCharacters(in: .whitespacesAndNewlines)
            let rules = source.cleanRules
            self.offering = offering.isEmpty ? nil : offering
            self.summary = summary.isEmpty ? nil : summary
            self.rules = rules.isEmpty ? nil : rules
        }
    }

    struct Template: Encodable {
        let name: String
        let relationship: String
        let tone: String
        let instructions: String
        let reply_length: String
        let emoji_policy: String
        let working_hours_behaviour: String
        let business: Business?
    }

    struct WorkingHoursBlock: Encodable {
        let enabled: Bool
        let is_within_working_hours: Bool
        let current_local_time: String
        let next_working_period: String?
        let weekly_schedule: String?
    }

    struct ProfileBlock: Encodable {
        let description: String?
        let role: String?
        let preferred_tone: String
        let business: Business?
        let reply_language: String?
        let grammatical_gender: String?
    }

    /// The request body. Built by `body(for:...)` so tests can pin exactly
    /// which fields an older server is spared.
    struct Body: Encodable {
        let source_text: String
        let instruction: String?
        let language: String
        let input_language: String?
        let template_id: String
        let template: Template
        let profile: ProfileBlock?
        let business_context: WorkingHoursBlock?
        let platform: String
        let app_version: String
    }

    /// The profile block, or nil when there is nothing personal to send.
    /// `reply_language` and `grammatical_gender` are included only when the
    /// server has said it knows them: an older server rejects unknown fields
    /// outright.
    static func profileBlock(
        _ profile: Profile?,
        serverSupportsPreferences: Bool,
        serverSupportsSenderProfile: Bool
    ) -> ProfileBlockSnapshot? {
        guard let profile else { return nil }
        let description = profile.description.trimmingCharacters(in: .whitespacesAndNewlines)
        let role = profile.role.trimmingCharacters(in: .whitespacesAndNewlines)
        return ProfileBlockSnapshot(
            description: description.isEmpty ? nil : description,
            role: role.isEmpty ? nil : role,
            preferredTone: profile.preferredTone.rawValue,
            hasBusiness: !profile.business.isEmpty,
            replyLanguage: serverSupportsPreferences ? profile.replyLanguage : nil,
            grammaticalGender: serverSupportsSenderProfile ? profile.grammaticalGender?.rawValue : nil
        )
    }

    /// What `profileBlock` decided, in a form tests can inspect without
    /// decoding JSON.
    struct ProfileBlockSnapshot: Equatable {
        let description: String?
        let role: String?
        let preferredTone: String
        let hasBusiness: Bool
        let replyLanguage: String?
        let grammaticalGender: String?
    }

    static func body(
        for context: RequestContext,
        descriptor: DeviceDescriptor = .current,
        serverSupportsPreferences: Bool = AILimits.serverSupportsReplyPreferences,
        serverSupportsSenderProfile: Bool = AILimits.serverSupportsSenderProfile
    ) -> Body {
        let instruction = context.instruction.trimmingCharacters(in: .whitespacesAndNewlines)
        let profile = profileBlock(
            context.profile,
            serverSupportsPreferences: serverSupportsPreferences,
            serverSupportsSenderProfile: serverSupportsSenderProfile
        )
        return Body(
            source_text: context.message,
            instruction: instruction.isEmpty ? nil : instruction,
            language: context.appLanguage,
            input_language: serverSupportsSenderProfile ? context.inputLanguage : nil,
            template_id: context.templateID,
            template: Template(
                name: context.templateName,
                relationship: context.templateRelationship,
                tone: context.templateTone.rawValue,
                instructions: context.templateInstructions,
                reply_length: context.templateReplyLength.rawValue,
                emoji_policy: context.templateEmojiPolicy.rawValue,
                working_hours_behaviour: context.templateWorkingHoursBehaviour.rawValue,
                business: Business(context.templateBusiness)
            ),
            profile: profile.map { snapshot in
                ProfileBlock(
                    description: snapshot.description,
                    role: snapshot.role,
                    preferred_tone: snapshot.preferredTone,
                    business: snapshot.hasBusiness ? Business(context.profile?.business) : nil,
                    reply_language: snapshot.replyLanguage,
                    grammatical_gender: snapshot.grammaticalGender
                )
            },
            business_context: context.business.map {
                WorkingHoursBlock(
                    enabled: $0.isEnabled,
                    is_within_working_hours: $0.isWithinWorkingHours,
                    current_local_time: $0.currentLocalTime,
                    next_working_period: $0.nextWorkingPeriod,
                    weekly_schedule: $0.weeklySchedule
                )
            },
            platform: descriptor.platform,
            app_version: descriptor.app_version
        )
    }

    // MARK: Call

    func generate(prompt: ReplyPromptBuilder.Prompt) async throws -> GeneratedReply {
        guard let baseURL = AIConfiguration.shared.backendBaseURL else {
            throw AIReplyError.notConfigured
        }
        guard session.isSignedIn else { throw AIReplyError.authenticationFailed }

        let client = APIClient(baseURL: baseURL)
        let body = Self.body(for: context)

        do {
            let response: AccountAPI.ReplyResponse = try await session.authenticated { token in
                try await client.post("api/v1/ai/reply", body: body, token: token)
            }
            onUsage?(response.usage)

            let text = ReplyNetworking.unwrapQuotes(
                response.reply.trimmingCharacters(in: .whitespacesAndNewlines)
            )
            guard !text.isEmpty else { throw AIReplyError.emptyResponse }
            return GeneratedReply(text: text, detectedLanguage: response.detectedLanguage)
        } catch let error as APIError {
            // The server states its real limit when it rejects a message as
            // too long. Remember it, so the counter is right from now on.
            if case .sourceTooLong(let limit) = error {
                AILimits.storeSourceLimit(limit)
            }
            throw Self.map(error)
        }
    }

    /// Backend failures become the closed set the UI already knows how to show.
    ///
    /// A spent quota and a burst of requests are different problems with
    /// different fixes - change plan or wait until tomorrow, versus wait a few
    /// seconds - so they stay different errors. They used to share one, and a
    /// user who tapped Regenerate twice was told their day's replies were gone.
    static func map(_ error: APIError) -> AIReplyError {
        switch error {
        case .offline:                 return .offline
        case .timedOut, .providerTimeout: return .timedOut
        case .cancelled:               return .cancelled
        case .unauthorized, .accountDisabled: return .authenticationFailed
        case .dailyLimitReached, .subscriptionExpired, .paymentRequired:
            return .quotaExhausted
        case .rateLimited:             return .rateLimited
        case .emptyResponse:           return .emptyResponse
        case .sourceTooLong(let limit): return .messageTooLong(limit: limit)
        // An older server says only INVALID_REQUEST. The one thing a user can
        // get wrong in this request is the message length.
        case .invalidRequest:          return .messageTooLong(limit: AIConfiguration.maximumMessageCharacters)
        default:                       return .serviceUnavailable
        }
    }
}
