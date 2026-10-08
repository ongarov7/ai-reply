import Foundation
import UIKit

/// Product events the app can report: names and small values only.
///
/// Өнім оқиғалары: тек атау мен шағын мән - мәтін, жыныс, идентификатор жоқ.
///
/// `ProductEvents.sink` is the one seam events go through. At launch the app
/// installs `ProductEventReporter`, which sends them in batches to
/// `POST /api/v1/analytics/events` - only for a signed-in user, and only to a
/// server that publishes `features.product_events`. The server keeps its own
/// allowlist of names, keys and values and drops anything else. The keyboard
/// extension reports nothing at all.
///
/// A value can be a number, a yes/no or a fixed code from this file - never
/// text the user typed, copied or received, and never the gender chosen.
enum ProductEvent: String, CaseIterable, Sendable {
    /// `version`, `trigger`: `auto` | `settings`.
    case onboardingStarted = "onboarding_started"
    /// `step`: an `OnboardingFlow.Step` id.
    case onboardingStepViewed = "onboarding_step_viewed"
    case onboardingKeyboardStepViewed = "onboarding_keyboard_step_viewed"
    /// Once per install.
    case keyboardEnabledDetected = "keyboard_enabled_detected"
    /// Once per install.
    case fullAccessEnabledDetected = "full_access_enabled_detected"
    case pasteTutorialViewed = "paste_tutorial_viewed"
    case onboardingPracticeCompleted = "onboarding_practice_completed"
    /// `version`, `skipped`.
    case onboardingCompleted = "onboarding_completed"
    case onboardingReopened = "onboarding_reopened"
    /// `source`: `onboarding` | `settings`, `skipped`. Never the value.
    case genderSelected = "gender_selected"
    case autocorrectEnabled = "autocorrect_enabled"
    case autocorrectDisabled = "autocorrect_disabled"
}

enum ProductEventValue: Equatable, Sendable {
    case int(Int)
    case bool(Bool)
    /// A fixed identifier such as "settings" - never free text.
    case code(String)
}

/// On the wire a value is a bare JSON number, boolean or string.
extension ProductEventValue: Encodable {
    func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        switch self {
        case .int(let number): try container.encode(number)
        case .bool(let flag):  try container.encode(flag)
        case .code(let code):  try container.encode(code)
        }
    }
}

/// Where events go.
@MainActor
protocol ProductEventSink {
    func record(_ name: String, _ props: [String: ProductEventValue])
}

@MainActor
enum ProductEvents {

    /// `ProductEventReporter.shared` from launch on; the DEBUG log until then.
    static var sink: ProductEventSink = DebugLogSink()

    static func track(_ event: ProductEvent, _ props: [String: ProductEventValue] = [:]) {
        sink.record(event.rawValue, props)
    }

    /// Records an event the first time it happens on this install and never
    /// again, e.g. the first time the keyboard is seen enabled.
    static func trackOnce(
        _ event: ProductEvent,
        _ props: [String: ProductEventValue] = [:],
        defaults: UserDefaults = .standard
    ) {
        let key = "analytics.once.\(event.rawValue)"
        guard !defaults.bool(forKey: key) else { return }
        defaults.set(true, forKey: key)
        track(event, props)
    }

    // MARK: Server switch

    /// App-only: the keyboard never reports, so the App Group does not need it.
    private static let serverSwitchKey = "ai.features.productEvents"

    /// Remembers whether the server takes events, from each `/api/v1/config`.
    /// A server that does not publish the flag gets nothing.
    static func storeServerSupport(_ features: AccountAPI.Features?, defaults: UserDefaults = .standard) {
        defaults.set(features?.productEvents ?? false, forKey: serverSwitchKey)
    }

    /// The last published `features.product_events`; false until one arrives.
    static func serverAcceptsEvents(defaults: UserDefaults = .standard) -> Bool {
        defaults.bool(forKey: serverSwitchKey)
    }
}

/// A DEBUG console line, nothing in a release build.
private struct DebugLogSink: ProductEventSink {
    func record(_ name: String, _ props: [String: ProductEventValue]) {
        #if DEBUG
        let values = props.keys.sorted().map { key in "\(key)=\(props[key].map(Self.describe) ?? "")" }
        NSLog("[AIReply] event %@ %@", name, values.joined(separator: " "))
        #endif
    }

    private static func describe(_ value: ProductEventValue) -> String {
        switch value {
        case .int(let number): return String(number)
        case .bool(let flag):  return flag ? "true" : "false"
        case .code(let code):  return code
        }
    }
}

// MARK: - Sending

/// The body of `POST /api/v1/analytics/events`.
struct ProductEventBatch: Encodable, Equatable, Sendable {

    struct Event: Encodable, Equatable, Sendable {
        let name: String
        /// When it happened: RFC 3339, UTC, whole seconds.
        let ts: String
        let props: [String: ProductEventValue]
    }

    let platform: String
    let app_version: String
    let events: [Event]

    /// The most events the server takes in one request.
    static let maximumEvents = 20
}

/// Delivers one batch. Throws when it did not arrive.
protocol ProductEventTransport: Sendable {
    func send(_ batch: ProductEventBatch) async throws
}

/// The real transport: the signed-in account's token, one request per batch.
struct AccountProductEventTransport: ProductEventTransport {

    var session: AccountSession = .shared

    func send(_ batch: ProductEventBatch) async throws {
        guard let baseURL = AIConfiguration.shared.backendBaseURL else { throw APIError.invalidRequest }
        let client = APIClient(baseURL: baseURL)
        // The answer counts accepted and rejected events; nothing here acts on it.
        let _: APIClient.Empty = try await session.authenticated { token in
            try await client.post("api/v1/analytics/events", body: batch, token: token)
        }
    }
}

/// Collects events in memory and sends them in small batches.
///
/// Оқиғалар жадта жиналып, шағын топпен жіберіледі; дискіге ештеңе жазылмайды.
///
/// A batch goes out once ten events are waiting, when the app goes to the
/// background, and otherwise at most thirty seconds after an event arrived.
/// Nothing is written to disk: what has not been sent when the app is killed
/// is gone, which is the right trade for numbers that only count. At most a
/// hundred events wait; the oldest make room. A signed-out user, or a server
/// that has not asked for events, gets none - what is waiting is dropped
/// rather than kept for later. A batch that fails for a network reason is
/// tried once more with the next send; anything else is dropped at once.
@MainActor
final class ProductEventReporter: ProductEventSink {

    struct Policy: Sendable {
        /// Waiting events that send a batch without waiting for the timer.
        var flushThreshold = 10
        var batchSize = ProductEventBatch.maximumEvents
        /// The most events that wait; the oldest are dropped beyond it.
        var capacity = 100
        /// The longest an event waits while the app stays open.
        var interval: Duration = .seconds(30)
    }

    static let shared = ProductEventReporter()

    private struct Pending {
        let event: ProductEventBatch.Event
        /// Whether a send of this event already failed once.
        var isRetry = false
    }

    private let transport: ProductEventTransport
    private let policy: Policy
    private let appVersion: String
    private let isAllowed: @MainActor () -> Bool
    private let now: () -> Date

    private var queue: [Pending] = []
    private var sending: Task<Void, Never>?
    private var timer: Task<Void, Never>?

    /// - Parameter isAllowed: asked before every send; false drops what waits.
    init(
        transport: ProductEventTransport = AccountProductEventTransport(),
        policy: Policy = Policy(),
        appVersion: String = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "",
        isAllowed: @escaping @MainActor () -> Bool = { AccountCredentials.isSignedIn && ProductEvents.serverAcceptsEvents() },
        now: @escaping () -> Date = Date.init
    ) {
        self.transport = transport
        self.policy = policy
        self.appVersion = appVersion
        self.isAllowed = isAllowed
        self.now = now
    }

    /// How many events are waiting to be sent.
    var waitingCount: Int { queue.count }

    func record(_ name: String, _ props: [String: ProductEventValue]) {
        DebugLogSink().record(name, props)
        queue.append(Pending(event: .init(name: name, ts: now().formatted(.iso8601), props: props)))
        dropOldestBeyondCapacity()
        if queue.count >= policy.flushThreshold {
            flush()
        } else {
            startTimer()
        }
    }

    /// Sends everything that waits, in batches, one request at a time. While
    /// a send is running this returns that send, which also takes whatever
    /// arrives in the meantime.
    @discardableResult
    func flush() -> Task<Void, Never> {
        if let sending { return sending }
        timer?.cancel()
        timer = nil
        let task = Task { await self.sendWaiting() }
        sending = task
        return task
    }

    /// Drops what waits: the account it belonged to was deleted.
    func discardWaiting() {
        timer?.cancel()
        timer = nil
        queue.removeAll()
    }

    /// The app is going to the background: send what waits now, and ask iOS
    /// for the few seconds that takes.
    func flushBeforeSuspension() {
        guard !queue.isEmpty || sending != nil else { return }
        SuspensionGrace().cover(flush(), in: UIApplication.shared)
    }

    // MARK: Private

    private func sendWaiting() async {
        defer {
            sending = nil
            startTimer()
        }
        guard isAllowed() else {
            queue.removeAll()
            return
        }
        while !queue.isEmpty {
            let batch = Array(queue.prefix(policy.batchSize))
            queue.removeFirst(batch.count)
            do {
                try await transport.send(ProductEventBatch(
                    platform: "ios",
                    app_version: appVersion,
                    events: batch.map(\.event)
                ))
            } catch {
                keepForOneRetry(batch, after: error)
                // The rest waits for the timer: no burst of failing requests.
                return
            }
        }
    }

    /// A network failure earns the batch one more try with the next send; a
    /// refusal (bad request, switched off, signed out) would only repeat.
    private func keepForOneRetry(_ batch: [Pending], after error: Error) {
        ReplyLog.event("product events not sent: \(error)")
        guard (error as? APIError)?.isTransient == true else { return }
        let again = batch.filter { !$0.isRetry }.map { Pending(event: $0.event, isRetry: true) }
        queue.insert(contentsOf: again, at: 0)
        dropOldestBeyondCapacity()
    }

    private func dropOldestBeyondCapacity() {
        let excess = queue.count - policy.capacity
        if excess > 0 { queue.removeFirst(excess) }
    }

    private func startTimer() {
        guard timer == nil, sending == nil, !queue.isEmpty else { return }
        let interval = policy.interval
        timer = Task { [weak self] in
            try? await Task.sleep(for: interval)
            guard !Task.isCancelled, let self else { return }
            self.timer = nil
            self.flush()
        }
    }
}

/// Keeps the app running for the moment a last send needs after it went to
/// the background, and lets go as soon as the send ends or iOS wants the time
/// back.
@MainActor
private final class SuspensionGrace {

    private var identifier: UIBackgroundTaskIdentifier = .invalid

    func cover(_ send: Task<Void, Never>, in application: UIApplication) {
        identifier = application.beginBackgroundTask(withName: "ProductEvents") { [weak self] in
            send.cancel()
            self?.end(in: application)
        }
        Task {
            await send.value
            end(in: application)
        }
    }

    private func end(in application: UIApplication) {
        guard identifier != .invalid else { return }
        application.endBackgroundTask(identifier)
        identifier = .invalid
    }
}
