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
    /// The plan's replies for today (or this period) are spent. Waiting a
    /// moment does NOT fix it, which is why it is not `rateLimited`.
    case quotaExhausted
    /// The service answered, but not with a usable reply.
    case emptyResponse
    /// Anything else: a 5xx, a malformed payload, an unreachable host.
    case serviceUnavailable
}
