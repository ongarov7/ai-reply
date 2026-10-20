import Foundation

/// Every way generating a reply can fail, as a value the UI localizes.
///
/// Deliberately closed and deliberately coarse. The user needs to know what to
/// do next; they do not need an HTTP status, a JSON body or an upstream error
/// string, and showing them one would leak detail that has no business on a
/// keyboard above WhatsApp.
enum AIReplyError: Error, Equatable, Sendable {
    /// Nothing was copied, or the copied value held no text.
    case noSourceMessage
    /// Something was copied, but iOS refused the keyboard the paste: the
    /// user tapped "Don't Allow", or "Paste from Other Apps" is set to deny.
    case pasteDenied
    /// The copied message is longer than the limit the server publishes
    /// (400 characters unless an administrator changed it).
    case messageTooLong(limit: Int)
    /// The keyboard cannot read the clipboard without Full Access.
    case fullAccessRequired
    /// The account is not ready to make a request.
    case notConfigured
    /// Device is offline.
    case offline
    /// The request exceeded its time budget.
    case timedOut
    /// The user or the keyboard cancelled before a reply arrived.
    case cancelled
    /// The account session is missing, expired or rejected.
    case authenticationFailed
    /// Too many requests in a short time. Waiting a moment fixes it.
    case rateLimited
    /// The plan's replies for today are spent. Waiting a moment does NOT fix
    /// it, which is why it is not `rateLimited`.
    case quotaExhausted
    /// The plan's replies for this month are spent: they come back next
    /// month, not tomorrow.
    case monthlyQuotaExhausted
    /// The current terms were not accepted (or the consent was withdrawn):
    /// the app has to be opened to accept them. Said before any request.
    case consentRequired
    /// The service answered, but not with a usable reply.
    case emptyResponse
    /// Anything else: a 5xx, a malformed payload, an unreachable host.
    case serviceUnavailable
    /// Compose mode: Generate was tapped with nothing described yet.
    case noInstruction
    /// Compose mode: the description is longer than the server's limit.
    case instructionTooLong(limit: Int)

    /// Whether doing the same thing again can work, so the composer offers
    /// Retry. Not when the keyboard itself is in the way (no Full Access),
    /// when iOS refused the paste or nothing was copied, or when a text is
    /// too long: Retry would only bring the same sentence back.
    var offersRetry: Bool {
        switch self {
        case .noSourceMessage, .pasteDenied, .fullAccessRequired, .messageTooLong, .noInstruction, .instructionTooLong:
            return false
        case .notConfigured, .offline, .timedOut, .cancelled, .authenticationFailed, .rateLimited,
             .quotaExhausted, .monthlyQuotaExhausted, .consentRequired, .emptyResponse, .serviceUnavailable:
            return true
        }
    }
}
