import Foundation

/// The contract between the watch app, the iPhone app and the keyboard.
///
/// Сағат, телефон және пернетақта арасындағы келісім: бір тұрақты пішім.
///
/// This file compiles into all three targets and imports nothing but
/// Foundation, so the watch app does not pull in the keyboard's layout tables
/// or the account code. Everything that crosses a process or device boundary
/// is one of the `Codable` values below; WatchConnectivity only ever sees a
/// one-key dictionary holding the encoded value.
enum WatchVoice {

    /// Raised only for a change an older peer cannot read. A peer on another
    /// version is refused by name instead of being half understood.
    static let protocolVersion = 1

    /// The only key in a WatchConnectivity dictionary or file metadata.
    static let payloadKey = "aireply.watchvoice"

    /// How long a dictated instruction may wait for a keyboard. Longer, and
    /// the user has moved on: it must not surface out of nowhere.
    static let commandLifetime: TimeInterval = 5 * 60

    /// How long a generated text waits for Insert or Dismiss. The same ten
    /// minutes a parked composer session has.
    static let resultLifetime: TimeInterval = 10 * 60

    /// A command dated this far ahead of the phone's clock is not trusted.
    /// The watch keeps the phone's time, so this only absorbs drift.
    static let clockTolerance: TimeInterval = 2 * 60

    /// The most an instruction can be: the top of the range the server's
    /// administrator may publish (`AILimits.instructionRange`). The keyboard
    /// applies the limit currently in force; this one only refuses a payload
    /// no limit could ever allow.
    static let maximumInstructionScalars = 1000

    /// A generated text this long is not a chat message.
    static let maximumResultScalars = 8000

    /// `POST /api/v1/ai/transcribe` takes at most 40 s and 4 MiB.
    static let maximumAudioSeconds = 40
    static let maximumAudioBytes = 4 << 20

    /// A recording goes in parts of this size, each in its own
    /// `sendMessageData`, which refuses anything over about 65 KB. That
    /// channel wakes the iPhone app; a file transfer would not, and would sit
    /// in the queue until the user opened the app.
    static let maximumAudioPartBytes = 56 * 1024
    /// 4 MiB in 56 KB parts.
    static let maximumAudioParts = 80

    /// Shortest recording the server accepts, in milliseconds.
    static let minimumAudioMilliseconds = 300

    /// How soon after the user confirmed a command it may still start by
    /// itself. One delivered later, from the system's queue, was not asked
    /// for in whatever field is open by then.
    static let liveCommandWindow: TimeInterval = 30

    /// The voice disclosure the user agreed to on the watch. A new wording
    /// that changes where a recording goes asks again.
    static let voiceConsentVersion = "2026-11-09"
}

// MARK: - Identifiers

/// Request and keyboard-session identifiers.
///
/// They become file names in the App Group and the server's
/// `Idempotency-Key`, so the format is that key's own: letters, digits and
/// dashes, 8 to 64 of them. A value that fails here can neither walk out of a
/// directory nor be refused by the server later.
enum WatchVoiceIdentifier {

    static func make() -> String { UUID().uuidString }

    static func isValid(_ value: String) -> Bool {
        let bytes = value.utf8
        guard (8...64).contains(bytes.count) else { return false }
        return bytes.allSatisfy { byte in
            (byte >= 0x30 && byte <= 0x39) // 0-9
                || (byte >= 0x41 && byte <= 0x5A) // A-Z
                || (byte >= 0x61 && byte <= 0x7A) // a-z
                || byte == 0x2D // -
        }
    }
}

// MARK: - Vocabulary

enum WatchVoiceDevice: String, Codable, Sendable {
    case watch
    case phone
}

enum WatchVoiceMessageType: String, Codable, Sendable {
    /// A confirmed instruction, watch to phone.
    case command
    /// The user took it back, watch to phone.
    case cancel
    /// "What is going on?", watch to phone. Wakes the phone app, which is
    /// the only way the watch can learn about a live keyboard.
    case statusRequest
    /// Progress of a request and the phone's state, phone to watch.
    case statusUpdate
    /// The text of a finished request, on the user's "View result".
    case resultRequest
    /// A recording to turn into text, with its audio.
    case transcribe
    /// What the server heard, phone to watch.
    case transcript
    /// "Test connection" in the app's settings.
    case ping
    case pong
}

/// What the instruction asks for.
enum WatchVoiceMode: String, Codable, CaseIterable, Identifiable, Sendable {
    /// Answer the copied message; with none copied, write a new one.
    case reply
    case newMessage
    case rewrite
    case shorten
    case expand

    var id: String { rawValue }

    /// Works on text already in the field, or selected.
    var editsExistingText: Bool {
        switch self {
        case .rewrite, .shorten, .expand: return true
        case .reply, .newMessage: return false
        }
    }
}

enum WatchVoiceLength: String, Codable, CaseIterable, Identifiable, Sendable {
    case short
    case medium
    case long

    var id: String { rawValue }
}

/// The three languages of this feature.
///
/// Used twice and on purpose separately: the language the instruction was
/// SPOKEN in and the language the message should be WRITTEN in.
enum WatchVoiceLanguage: String, Codable, CaseIterable, Identifiable, Sendable {
    case english = "en"
    case russian = "ru"
    case kazakh = "kk"

    var id: String { rawValue }

    var nativeName: String {
        switch self {
        case .english: return "English"
        case .russian: return "Русский"
        case .kazakh:  return "Қазақша"
        }
    }

    /// Whether Apple's own dictation can be offered as the fallback. Apple
    /// lists English and Russian for watchOS and does not list Kazakh.
    var hasSystemDictation: Bool { self != .kazakh }

    /// The hints `POST /api/v1/ai/transcribe` takes, most likely first: the
    /// chosen language, then the others people here mix into it. The owner's
    /// rule: the microphone understands Russian, English and the language
    /// picked. At most three, the server's limit; a hint only biases the
    /// model, which writes down what it hears.
    var transcriptionHints: [String] {
        var hints = [rawValue]
        for other in [WatchVoiceLanguage.russian, .english, .kazakh] where !hints.contains(other.rawValue) {
            hints.append(other.rawValue)
        }
        return Array(hints.prefix(3))
    }
}

/// How a request is getting on. Only ever moves forward.
enum WatchVoiceDeliveryStatus: String, Codable, Sendable {
    /// On the iPhone, waiting for a keyboard.
    case queued
    /// A keyboard took it.
    case received
    case generating
    /// Written, and waiting for Insert in the keyboard.
    case ready
    case inserted
    case dismissed
    case failed
    case expired
    case cancelled

    /// Nothing more will happen to this request.
    var isFinal: Bool {
        switch self {
        case .inserted, .dismissed, .failed, .expired, .cancelled: return true
        case .queued, .received, .generating, .ready: return false
        }
    }
}

/// Why something did not work, as a value each screen words for itself.
enum WatchVoiceFailure: String, Codable, Sendable, Error {
    case unsupportedVersion
    case invalidRequest
    case instructionEmpty
    case instructionTooLong
    case expired
    case storageUnavailable
    case fullAccessRequired
    case notSignedIn
    case consentRequired
    case offline
    case timedOut
    case rateLimited
    case quotaExhausted
    case monthlyQuotaExhausted
    case serviceUnavailable
    case nothingToRewrite
    case copiedMessageUnreadable
    case copiedMessageTooLong
    case transcriptionUnavailable
    case transcriptionFailed
    case audioRejected
    case nothingHeard
    case voiceLimitReached
    case microphoneDenied
    case phoneUnreachable

    /// Whether sending the same thing again can work.
    var offersRetry: Bool {
        switch self {
        case .offline, .timedOut, .rateLimited, .serviceUnavailable, .storageUnavailable, .transcriptionFailed,
             .nothingHeard, .phoneUnreachable:
            return true
        case .unsupportedVersion, .invalidRequest, .instructionEmpty, .instructionTooLong, .expired,
             .fullAccessRequired, .notSignedIn, .consentRequired, .quotaExhausted, .monthlyQuotaExhausted,
             .nothingToRewrite, .copiedMessageUnreadable, .copiedMessageTooLong, .transcriptionUnavailable,
             .audioRejected, .voiceLimitReached, .microphoneDenied:
            return false
        }
    }
}

// MARK: - Options

/// How the watch wants the message written. Every field left nil means "as
/// the iPhone would": nothing has to be set up on the wrist for each command.
struct WatchVoiceOptions: Codable, Equatable, Sendable {
    var mode: WatchVoiceMode = .reply
    /// nil: the persona the keyboard last used.
    var templateID: String?
    /// nil: the message follows the copied message (a reply) or the
    /// instruction (a new one), as everywhere else in the product.
    var responseLanguage: WatchVoiceLanguage?
    /// nil: the persona's own length.
    var length: WatchVoiceLength?

    init(mode: WatchVoiceMode = .reply, templateID: String? = nil,
         responseLanguage: WatchVoiceLanguage? = nil, length: WatchVoiceLength? = nil) {
        self.mode = mode
        self.templateID = templateID
        self.responseLanguage = responseLanguage
        self.length = length
    }
}

/// One persona the watch can offer. Identifier and name only: instructions
/// and business rules never leave the phone.
struct WatchTemplateChoice: Codable, Equatable, Identifiable, Sendable {
    let id: String
    let name: String
}

/// What the phone knows that the watch shows or needs.
struct WatchPhoneSnapshot: Codable, Equatable, Sendable {
    /// The keyboard on screen right now, if one is.
    var keyboardSessionID: String?
    /// The user tapped Watch Voice in the keyboard and it is waiting.
    var keyboardIsWaiting = false
    /// Whether that keyboard has Full Access; nil with no keyboard on screen.
    var keyboardHasFullAccess: Bool?
    var isSignedIn = false
    var templates: [WatchTemplateChoice] = []
    /// The persona the keyboard last used.
    var currentTemplateID: String?
    /// The app's interface language, which the watch follows.
    var appLanguage: String?
    /// The longest instruction a reply takes right now.
    var replyInstructionLimit: Int?
    /// The server turns speech into text (`features.voice_transcription`),
    /// the same OpenAI path as the Android keyboard's microphone.
    var transcriptionAvailable = false
}

// MARK: - Envelope

/// One message between the devices.
///
/// Flat on purpose: a `type` and the optionals that type uses. The fields
/// `validate` requires for a type are what makes it complete.
struct WatchVoiceEnvelope: Codable, Equatable, Sendable {
    var version: Int = WatchVoice.protocolVersion
    /// Correlates a command with everything said about it afterwards. For a
    /// message about no request in particular, simply a fresh identifier.
    var requestID: String
    var type: WatchVoiceMessageType
    var createdAt: Date
    var source: WatchVoiceDevice
    /// The keyboard session the watch last saw when the user confirmed.
    var keyboardSessionID: String?
    /// The language the instruction was spoken in.
    var language: WatchVoiceLanguage?
    var instruction: String?
    var deliveryStatus: WatchVoiceDeliveryStatus?
    var options: WatchVoiceOptions?
    var failure: WatchVoiceFailure?
    /// The limit behind `.instructionTooLong`, when known.
    var failureLimit: Int?
    var phone: WatchPhoneSnapshot?
    /// The generated text, only as the answer to `.resultRequest`.
    var result: String?
    /// Length of the recording, with `.transcribe`.
    var audioMilliseconds: Int?
    /// Which part of the recording this is, from 0, and how many there are.
    var audioPart: Int?
    var audioParts: Int?
}

extension WatchVoiceEnvelope {

    static func command(
        requestID: String = WatchVoiceIdentifier.make(),
        instruction: String,
        language: WatchVoiceLanguage,
        options: WatchVoiceOptions,
        keyboardSessionID: String?,
        createdAt: Date = Date()
    ) -> WatchVoiceEnvelope {
        WatchVoiceEnvelope(requestID: requestID, type: .command, createdAt: createdAt, source: .watch,
                           keyboardSessionID: keyboardSessionID, language: language, instruction: instruction,
                           options: options)
    }

    static func request(_ type: WatchVoiceMessageType, requestID: String = WatchVoiceIdentifier.make(),
                        source: WatchVoiceDevice = .watch, createdAt: Date = Date()) -> WatchVoiceEnvelope {
        WatchVoiceEnvelope(requestID: requestID, type: type, createdAt: createdAt, source: source)
    }

    static func status(
        requestID: String,
        status: WatchVoiceDeliveryStatus?,
        failure: WatchVoiceFailure? = nil,
        failureLimit: Int? = nil,
        phone: WatchPhoneSnapshot? = nil,
        createdAt: Date = Date()
    ) -> WatchVoiceEnvelope {
        WatchVoiceEnvelope(requestID: requestID, type: .statusUpdate, createdAt: createdAt, source: .phone,
                           deliveryStatus: status, failure: failure, failureLimit: failureLimit, phone: phone)
    }

    /// A refusal to whatever was asked, in the shape every reply takes.
    static func refusal(requestID: String, failure: WatchVoiceFailure, createdAt: Date = Date()) -> WatchVoiceEnvelope {
        status(requestID: requestID, status: .failed, failure: failure, createdAt: createdAt)
    }
}

// MARK: - Validation

enum WatchVoiceValidator {

    /// Why this envelope must not be acted on; nil when it is fine.
    /// Nothing that fails here reaches the App Group or the server.
    static func problem(with envelope: WatchVoiceEnvelope, now: Date = Date()) -> WatchVoiceFailure? {
        guard envelope.version == WatchVoice.protocolVersion else { return .unsupportedVersion }
        guard WatchVoiceIdentifier.isValid(envelope.requestID) else { return .invalidRequest }
        if let session = envelope.keyboardSessionID, !WatchVoiceIdentifier.isValid(session) { return .invalidRequest }
        if let template = envelope.options?.templateID, template.isEmpty || template.utf8.count > 128 {
            return .invalidRequest
        }

        switch envelope.type {
        case .command:
            guard envelope.source == .watch, envelope.language != nil, envelope.options != nil else {
                return .invalidRequest
            }
            guard let instruction = envelope.instruction else { return .instructionEmpty }
            let trimmed = instruction.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !trimmed.isEmpty else { return .instructionEmpty }
            guard trimmed.unicodeScalars.count <= WatchVoice.maximumInstructionScalars else { return .instructionTooLong }
            if isExpired(envelope, now: now) { return .expired }
        case .transcribe:
            guard envelope.source == .watch, envelope.language != nil, let length = envelope.audioMilliseconds,
                  length >= WatchVoice.minimumAudioMilliseconds,
                  length <= (WatchVoice.maximumAudioSeconds * 1000) + 500 else {
                return .audioRejected
            }
            let parts = envelope.audioParts ?? 1
            let part = envelope.audioPart ?? 0
            guard (1...WatchVoice.maximumAudioParts).contains(parts), (0..<parts).contains(part) else { return .audioRejected }
            if isExpired(envelope, now: now) { return .expired }
        case .cancel, .statusRequest, .statusUpdate, .resultRequest, .transcript, .ping, .pong:
            break
        }
        if let result = envelope.result, result.unicodeScalars.count > WatchVoice.maximumResultScalars {
            return .invalidRequest
        }
        return nil
    }

    static func isExpired(_ envelope: WatchVoiceEnvelope, now: Date = Date()) -> Bool {
        let age = now.timeIntervalSince(envelope.createdAt)
        return age > WatchVoice.commandLifetime || age < -WatchVoice.clockTolerance
    }
}

// MARK: - Codec

/// The only code that touches WatchConnectivity's dictionaries.
enum WatchVoiceCodec {

    private static func makeEncoder() -> JSONEncoder {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .millisecondsSince1970
        return encoder
    }

    private static func makeDecoder() -> JSONDecoder {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .millisecondsSince1970
        return decoder
    }

    static func data<Value: Encodable>(_ value: Value) throws -> Data {
        try makeEncoder().encode(value)
    }

    static func value<Value: Decodable>(_ type: Value.Type, from data: Data) -> Value? {
        try? makeDecoder().decode(type, from: data)
    }

    static func dictionary(_ envelope: WatchVoiceEnvelope) throws -> [String: Any] {
        [WatchVoice.payloadKey: try data(envelope)]
    }

    static func envelope(from dictionary: [String: Any]) -> WatchVoiceEnvelope? {
        guard let data = dictionary[WatchVoice.payloadKey] as? Data else { return nil }
        return value(WatchVoiceEnvelope.self, from: data)
    }
}

// MARK: - Audio packet

/// A recording and its envelope in one `sendMessageData`: the length of the
/// envelope, the envelope, then the audio.
enum WatchVoiceAudioPacket {

    static func encode(_ envelope: WatchVoiceEnvelope, audio: Data) throws -> Data {
        let header = try WatchVoiceCodec.data(envelope)
        var packet = Data()
        var length = UInt32(header.count).bigEndian
        withUnsafeBytes(of: &length) { packet.append(contentsOf: $0) }
        packet.append(header)
        packet.append(audio)
        return packet
    }

    static func decode(_ packet: Data) -> (envelope: WatchVoiceEnvelope, audio: Data)? {
        guard packet.count > 4 else { return nil }
        let length = packet.prefix(4).reduce(UInt32(0)) { ($0 << 8) | UInt32($1) }
        let start = packet.startIndex + 4
        guard length > 0, length < 64 * 1024, packet.count >= 4 + Int(length) else { return nil }
        let end = start + Int(length)
        guard let envelope = WatchVoiceCodec.value(WatchVoiceEnvelope.self, from: packet.subdata(in: start..<end)) else {
            return nil
        }
        return (envelope, packet.subdata(in: end..<packet.endIndex))
    }
}

/// Puts a recording sent in parts back together, on the phone. Parts may
/// arrive twice (an acknowledgement was lost) and in any order.
struct WatchVoiceAudioAssembly {

    enum Outcome: Equatable {
        case waiting
        case complete(Data)
        case rejected
    }

    private struct Partial {
        let count: Int
        let started: Date
        var parts: [Int: Data] = [:]
        var bytes = 0
    }

    /// A recording whose parts stop coming is dropped after this long.
    static let lifetime: TimeInterval = 120

    private var partials: [String: Partial] = [:]

    var pendingCount: Int { partials.count }

    mutating func add(_ data: Data, part: Int, of count: Int, requestID: String, now: Date = Date()) -> Outcome {
        partials = partials.filter { now.timeIntervalSince($0.value.started) < Self.lifetime }
        guard (1...WatchVoice.maximumAudioParts).contains(count), (0..<count).contains(part), !data.isEmpty else {
            return .rejected
        }
        var partial = partials[requestID] ?? Partial(count: count, started: now)
        guard partial.count == count else {
            partials[requestID] = nil
            return .rejected
        }
        if partial.parts[part] == nil { partial.bytes += data.count }
        partial.parts[part] = data
        guard partial.bytes <= WatchVoice.maximumAudioBytes else {
            partials[requestID] = nil
            return .rejected
        }
        guard partial.parts.count == count else {
            partials[requestID] = partial
            return .waiting
        }
        partials[requestID] = nil
        var whole = Data(capacity: partial.bytes)
        for index in 0..<count {
            guard let piece = partial.parts[index] else { return .rejected }
            whole.append(piece)
        }
        return .complete(whole)
    }

    /// The parts a recording of `bytes` is sent in.
    static func split(_ audio: Data, partSize: Int = WatchVoice.maximumAudioPartBytes) -> [Data] {
        guard !audio.isEmpty, partSize > 0 else { return [] }
        return stride(from: 0, to: audio.count, by: partSize).map {
            audio.subdata(in: audio.startIndex.advanced(by: $0)..<audio.startIndex.advanced(by: min($0 + partSize, audio.count)))
        }
    }
}

// MARK: - Delivery

/// What the watch does when sending a confirmed instruction did not work.
enum WatchVoiceDelivery {

    /// How long to wait for the phone's acknowledgement. The phone answers
    /// as soon as the command is on disk, so this only has to cover waking it.
    static let acknowledgementTimeout: TimeInterval = 8

    /// `sendMessage` tries before the instruction goes into the system's queue.
    static let maximumDirectAttempts = 2

    /// A part of a recording may have to wake the phone app first, which
    /// can take longer than an answer to a running app.
    static let audioPartTimeout: TimeInterval = 15
    /// Tries per part. A part sent again is harmless: the phone keeps one copy.
    static let audioPartAttempts = 3

    enum Step: Equatable {
        case retry(after: TimeInterval)
        /// `transferUserInfo`: delivered when the system can, which may be
        /// much later. Never presented as sent.
        case queueForLater
    }

    /// - Parameters:
    ///   - attempt: 1-based number of the attempt that failed.
    ///   - isReachable: what WatchConnectivity says right now.
    static func step(afterAttempt attempt: Int, isReachable: Bool) -> Step {
        guard isReachable, attempt < maximumDirectAttempts else { return .queueForLater }
        return .retry(after: 1)
    }

    /// Asked for while a request is in flight, because a suspended phone app
    /// cannot push: every three seconds and for 45 at most, in the foreground.
    static let statusPollInterval: TimeInterval = 3
    static let statusPollLimit: TimeInterval = 45
}
