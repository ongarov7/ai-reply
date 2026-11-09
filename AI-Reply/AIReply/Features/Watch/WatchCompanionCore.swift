import Foundation

/// What the iPhone does with a message from the watch, with no
/// WatchConnectivity in it: `WatchCompanion` is the wire, this is the logic,
/// and the tests drive it directly.
struct WatchCompanionCore: Sendable {

    let store: WatchVoiceStore
    let snapshot: @Sendable (Date) -> WatchPhoneSnapshot
    /// Tells a keyboard on screen to look in the App Group.
    let notifyKeyboard: @Sendable () -> Void

    /// Handles one envelope and returns the answer to it. The answer is
    /// ready as soon as the store is, so the watch is never kept waiting
    /// for anything slower than a file.
    func handle(_ envelope: WatchVoiceEnvelope, now: Date = Date()) -> WatchVoiceEnvelope {
        if let problem = WatchVoiceValidator.problem(with: envelope, now: now) {
            return .refusal(requestID: Self.correlation(envelope.requestID), failure: problem, createdAt: now)
        }
        store.purge(now: now)
        switch envelope.type {
        case .command:
            return receive(envelope, now: now)
        case .cancel:
            store.cancel(envelope.requestID, now: now)
            notifyKeyboard()
            return .status(requestID: envelope.requestID, status: .cancelled, phone: snapshot(now), createdAt: now)
        case .statusRequest:
            return status(of: envelope.requestID, now: now)
        case .resultRequest:
            var answer = status(of: envelope.requestID, now: now)
            let outcome = store.outcome(for: envelope.requestID)
            if outcome?.status == .ready { answer.result = outcome?.text }
            return answer
        case .ping:
            return .request(.pong, requestID: envelope.requestID, source: .phone, createdAt: now)
        case .transcribe, .statusUpdate, .transcript, .pong:
            return .status(requestID: envelope.requestID, status: nil, phone: snapshot(now), createdAt: now)
        }
    }

    private func receive(_ envelope: WatchVoiceEnvelope, now: Date) -> WatchVoiceEnvelope {
        let live = store.readSession().flatMap { $0.isLive(now: now) ? $0 : nil }
        // Which keyboard this is for: the one the watch saw when the user
        // confirmed; when it saw none, the one on screen right now.
        guard let command = WatchVoiceCommand(envelope: envelope,
                                              targetSessionID: envelope.keyboardSessionID ?? live?.id) else {
            return .refusal(requestID: envelope.requestID, failure: .invalidRequest, createdAt: now)
        }
        switch store.enqueue(command) {
        case .queued:
            notifyKeyboard()
            return .status(requestID: command.id, status: .queued, phone: snapshot(now), createdAt: now)
        case .duplicate:
            // Another delivery of what is already here: reported, not redone.
            return status(of: command.id, now: now)
        case .queueFull, .unavailable:
            return .refusal(requestID: command.id, failure: .storageUnavailable, createdAt: now)
        }
    }

    private func status(of id: String, now: Date) -> WatchVoiceEnvelope {
        let outcome = store.outcome(for: id)
        let status = outcome?.status ?? (store.command(id: id) != nil ? .queued : nil)
        return .status(requestID: id, status: status, failure: outcome?.failure, failureLimit: outcome?.failureLimit,
                       phone: snapshot(now), createdAt: now)
    }

    /// A malformed identifier cannot be echoed back: it is not ours to trust.
    private static func correlation(_ requestID: String) -> String {
        WatchVoiceIdentifier.isValid(requestID) ? requestID : WatchVoiceIdentifier.make()
    }
}

extension WatchPhoneSnapshot {

    /// The phone's state right now, for the watch.
    static func current(store: WatchVoiceStore = .shared, settings: SharedSettings = .shared,
                        now: Date = Date()) -> WatchPhoneSnapshot {
        let session = store.readSession().flatMap { $0.isLive(now: now) ? $0 : nil }
        let language = settings.effectiveAppLanguage
        let templates: [WatchTemplateChoice]
        if let summaries = settings.templateSummaries {
            templates = summaries.map { WatchTemplateChoice(id: $0.id, name: $0.name(for: language)) }
        } else {
            templates = ReplyConfiguration.initial.visibleTemplates.map {
                WatchTemplateChoice(id: $0.id, name: $0.displayName(appLanguage: language))
            }
        }
        return WatchPhoneSnapshot(
            keyboardSessionID: session?.id,
            keyboardIsWaiting: session?.isWaiting ?? false,
            keyboardHasFullAccess: session?.hasFullAccess,
            isSignedIn: AccountCredentials.isSignedIn,
            templates: templates,
            currentTemplateID: settings.lastTemplateID,
            appLanguage: language.rawValue,
            replyInstructionLimit: ReplyInstruction.maximumCharacters,
            transcriptionAvailable: WatchTranscriber.isAvailable
        )
    }
}
