import Darwin
import Foundation

/// A confirmed instruction waiting on the iPhone for a keyboard.
struct WatchVoiceCommand: Codable, Equatable, Identifiable, Sendable {
    let id: String
    let createdAt: Date
    let expiresAt: Date
    let instruction: String
    let instructionLanguage: WatchVoiceLanguage
    let options: WatchVoiceOptions
    /// The keyboard session this was meant for: the one the watch saw when
    /// the user confirmed or, when it saw none, the one live as the phone
    /// received it. nil when no keyboard was on screen.
    let targetSessionID: String?

    /// Built from an envelope `WatchVoiceValidator` has passed.
    init?(envelope: WatchVoiceEnvelope, targetSessionID: String?) {
        guard envelope.type == .command, let instruction = envelope.instruction,
              let language = envelope.language, let options = envelope.options else { return nil }
        id = envelope.requestID
        createdAt = envelope.createdAt
        expiresAt = envelope.createdAt.addingTimeInterval(WatchVoice.commandLifetime)
        self.instruction = instruction.trimmingCharacters(in: .whitespacesAndNewlines)
        instructionLanguage = language
        self.options = options
        self.targetSessionID = targetSessionID
    }

    func isExpired(now: Date = Date()) -> Bool { now >= expiresAt }
}

/// How a command turned out, written by the keyboard.
struct WatchVoiceOutcome: Codable, Equatable, Identifiable, Sendable {

    /// Which composer a saved text reopens in.
    enum Kind: String, Codable, Sendable {
        case reply
        case compose
    }

    let id: String
    var status: WatchVoiceDeliveryStatus
    var updatedAt: Date
    var expiresAt: Date
    var failure: WatchVoiceFailure?
    var failureLimit: Int?
    /// The generated text, only while it waits for Insert (`.ready`).
    var text: String?
    /// The dictated instruction, only with `text`, so a reopened composer
    /// can show what was asked.
    var instruction: String?
    var kind: Kind?
}

/// The App Group side of Watch Voice: a queue the app writes and the
/// keyboard reads, and the small state each publishes for the other.
///
/// Әр команда жеке файл: құлыпсыз, атомарлы, қайталанбайды.
///
/// LOCK-FREE, ON PURPOSE. iOS terminates a process that is suspended while
/// holding a lock on a file in a shared container (0xdead10cc), and either
/// side here can be suspended at any moment. So there is no shared queue file
/// to read, change and write back. Each command is its own small file made
/// visible by a single atomic `link`, and a decision only one process may win
/// is the exclusive creation of an empty marker file. Nothing ever waits.
///
/// SENSITIVE TEXT. Instructions and generated texts are the only sensitive
/// data here. They are written with
/// `.completeFileProtectionUnlessOpen`, so the phone can take a command while
/// locked and nothing can read it back until the user unlocks; they expire in
/// minutes (`WatchVoice.commandLifetime`, `resultLifetime`), and the folder
/// is excluded from backups. The copied message is never written at all: it
/// stays in the keyboard's memory.
struct WatchVoiceStore: Sendable {

    enum EnqueueResult: Equatable, Sendable {
        case queued
        /// This request ID has been seen before: another delivery of it.
        case duplicate
        case queueFull
        case unavailable
    }

    /// Empty marker files, one per decision and request.
    enum Marker: String, Sendable {
        /// A keyboard took the command.
        case claimed
        /// Its text reached a host field. Only ever once.
        case inserted
        /// The user took it back on the watch.
        case cancelled
    }

    private enum Area: String, CaseIterable {
        case inbox
        case outcomes
        case markers
    }

    /// nil when the App Group is not reachable from this process.
    let rootURL: URL?

    /// More unclaimed commands than this means something is wrong.
    static let maximumInbox = 20
    /// A claim with no progress for this long lost its keyboard process.
    static let orphanAfter: TimeInterval = 120
    /// Markers stay long enough to refuse any late redelivery.
    static let markerLifetime: TimeInterval = 24 * 60 * 60

    static let shared = WatchVoiceStore(
        rootURL: FileManager.default
            .containerURL(forSecurityApplicationGroupIdentifier: AppGroup.identifier)?
            .appendingPathComponent("WatchVoice", isDirectory: true)
    )

    // MARK: Inbox

    func enqueue(_ command: WatchVoiceCommand) -> EnqueueResult {
        guard WatchVoiceIdentifier.isValid(command.id), let inbox = directory(.inbox),
              let data = try? WatchVoiceCodec.data(command) else { return .unavailable }
        if isKnown(command.id) { return .duplicate }
        if fileNames(in: inbox).filter({ $0.hasSuffix(".json") }).count >= Self.maximumInbox { return .queueFull }

        let destination = inbox.appendingPathComponent(command.id + ".json")
        let temporary = inbox.appendingPathComponent("." + command.id + ".tmp")
        do {
            try data.write(to: temporary, options: .completeFileProtectionUnlessOpen)
        } catch {
            return .unavailable
        }
        defer { unlink(temporary.path) }
        // `link` fails if the name exists, so two deliveries racing here
        // cannot both queue; `rename` would have replaced the first.
        guard Darwin.link(temporary.path, destination.path) == 0 else {
            return errno == EEXIST ? .duplicate : .unavailable
        }
        return .queued
    }

    /// Commands a keyboard may start, oldest first: unexpired and either
    /// unclaimed or left behind by a keyboard that no longer runs.
    func pendingCommands(now: Date = Date()) -> [WatchVoiceCommand] {
        guard let inbox = directory(.inbox) else { return [] }
        return fileNames(in: inbox)
            .filter { $0.hasSuffix(".json") && !$0.hasPrefix(".") }
            .compactMap { read(WatchVoiceCommand.self, at: inbox.appendingPathComponent($0)) }
            .filter { !$0.isExpired(now: now) && !has(.cancelled, for: $0.id) }
            .filter { !has(.claimed, for: $0.id) || isOrphaned($0.id, now: now) }
            .sorted { $0.createdAt < $1.createdAt }
    }

    func command(id: String) -> WatchVoiceCommand? {
        guard WatchVoiceIdentifier.isValid(id), let inbox = directory(.inbox) else { return nil }
        return read(WatchVoiceCommand.self, at: inbox.appendingPathComponent(id + ".json"))
    }

    /// Exactly one caller gets true for a command. A claim left behind by a
    /// keyboard that died may be taken over.
    func claim(_ id: String, now: Date = Date()) -> Bool {
        if create(.claimed, for: id) { return true }
        guard isOrphaned(id, now: now), let url = markerURL(.claimed, for: id) else { return false }
        unlink(url.path)
        return create(.claimed, for: id)
    }

    /// The user took the command back. Whatever has not started never will.
    func cancel(_ id: String, now: Date = Date()) {
        guard WatchVoiceIdentifier.isValid(id) else { return }
        create(.cancelled, for: id)
        removeCommand(id)
    }

    func removeCommand(_ id: String) {
        guard WatchVoiceIdentifier.isValid(id), let inbox = directory(.inbox) else { return }
        unlink(inbox.appendingPathComponent(id + ".json").path)
    }

    // MARK: Markers

    /// Creates the marker; false when it already exists or cannot be made.
    @discardableResult
    func create(_ marker: Marker, for id: String) -> Bool {
        guard let url = markerURL(marker, for: id) else { return false }
        let descriptor = open(url.path, O_CREAT | O_EXCL | O_WRONLY | O_CLOEXEC, 0o600)
        guard descriptor >= 0 else { return false }
        close(descriptor)
        return true
    }

    func has(_ marker: Marker, for id: String) -> Bool {
        guard let url = markerURL(marker, for: id) else { return false }
        return FileManager.default.fileExists(atPath: url.path)
    }

    /// Whether this request ID was ever delivered: in any state, for as long
    /// as its traces last.
    func isKnown(_ id: String) -> Bool {
        if has(.claimed, for: id) || has(.cancelled, for: id) { return true }
        // Existence only: a locked phone cannot read these files, but it can
        // see them.
        if let outcomes = directory(.outcomes),
           FileManager.default.fileExists(atPath: outcomes.appendingPathComponent(id + ".json").path) { return true }
        guard let inbox = directory(.inbox) else { return false }
        return FileManager.default.fileExists(atPath: inbox.appendingPathComponent(id + ".json").path)
    }

    /// Gives a claimed command back: the keyboard that took it went away
    /// before the answer came, and the next one may run it again (with the
    /// same key, so it is not paid for twice).
    /// Whether the instruction is still in the inbox. Asks the file system
    /// only, so the answer holds while the lock keeps the file closed.
    func hasCommand(_ id: String) -> Bool {
        guard let inbox = directory(.inbox) else { return false }
        return FileManager.default.fileExists(atPath: inbox.appendingPathComponent(id + ".json").path)
    }

    func release(_ id: String) {
        guard let url = markerURL(.claimed, for: id) else { return }
        unlink(url.path)
    }

    private func isOrphaned(_ id: String, now: Date) -> Bool {
        if let outcome = outcome(for: id) {
            if outcome.status.isFinal || outcome.status == .ready { return false }
            return now.timeIntervalSince(outcome.updatedAt) > Self.orphanAfter
        }
        guard let url = markerURL(.claimed, for: id),
              let attributes = try? FileManager.default.attributesOfItem(atPath: url.path),
              let created = attributes[.creationDate] as? Date else { return false }
        return now.timeIntervalSince(created) > Self.orphanAfter
    }

    // MARK: Outcomes

    func write(_ outcome: WatchVoiceOutcome) {
        guard WatchVoiceIdentifier.isValid(outcome.id), let directory = directory(.outcomes),
              let data = try? WatchVoiceCodec.data(outcome) else { return }
        try? data.write(to: directory.appendingPathComponent(outcome.id + ".json"),
                        options: [.atomic, .completeFileProtectionUnlessOpen])
    }

    func outcome(for id: String) -> WatchVoiceOutcome? {
        guard WatchVoiceIdentifier.isValid(id), let directory = directory(.outcomes) else { return nil }
        return read(WatchVoiceOutcome.self, at: directory.appendingPathComponent(id + ".json"))
    }

    func outcomes(now: Date = Date()) -> [WatchVoiceOutcome] {
        guard let directory = directory(.outcomes) else { return [] }
        return fileNames(in: directory)
            .filter { $0.hasSuffix(".json") }
            .compactMap { read(WatchVoiceOutcome.self, at: directory.appendingPathComponent($0)) }
            .filter { $0.expiresAt > now }
            .sorted { $0.updatedAt < $1.updatedAt }
    }

    /// Texts waiting for Insert, newest first.
    func savedResults(now: Date = Date()) -> [WatchVoiceOutcome] {
        outcomes(now: now).filter { $0.status == .ready && $0.text != nil }.reversed()
    }

    // MARK: Published state

    func writeSession(_ session: WatchKeyboardSession) {
        writeState(session, name: "keyboard-session.json")
    }

    func readSession() -> WatchKeyboardSession? {
        readState(WatchKeyboardSession.self, name: "keyboard-session.json")
    }

    func writeLink(_ link: WatchLinkSnapshot) {
        writeState(link, name: "watch-link.json")
    }

    func readLink() -> WatchLinkSnapshot? {
        readState(WatchLinkSnapshot.self, name: "watch-link.json")
    }

    // MARK: Expiry

    /// Deletes what has outlived its use, and tells the watch about
    /// instructions that did: they count as expired, not as lost.
    ///
    /// A file that cannot be OPENED is not junk: while the iPhone is locked
    /// these files are unreadable by design. Only its age, which can always
    /// be read, may condemn it then. A file read and not understood is junk.
    func purge(now: Date = Date()) {
        if let inbox = directory(.inbox) {
            for name in fileNames(in: inbox) {
                let url = inbox.appendingPathComponent(name)
                if name.hasPrefix(".") {
                    // A temporary file only outlives its writer by accident.
                    if age(of: url, now: now) > 60 { unlink(url.path) }
                    continue
                }
                let command: WatchVoiceCommand
                switch load(WatchVoiceCommand.self, at: url) {
                case .value(let value):
                    command = value
                case .unreadable:
                    if age(of: url, now: now) > WatchVoice.commandLifetime + WatchVoice.clockTolerance { unlink(url.path) }
                    continue
                case .corrupt:
                    unlink(url.path)
                    continue
                }
                guard command.isExpired(now: now) else { continue }
                unlink(url.path)
                if outcome(for: command.id) == nil {
                    write(WatchVoiceOutcome(id: command.id, status: .expired, updatedAt: now,
                                            expiresAt: now.addingTimeInterval(WatchVoice.resultLifetime),
                                            failure: .expired))
                }
            }
        }
        if let outcomes = directory(.outcomes) {
            for name in fileNames(in: outcomes) {
                let url = outcomes.appendingPathComponent(name)
                switch load(WatchVoiceOutcome.self, at: url) {
                case .value(let outcome):
                    if outcome.expiresAt <= now { unlink(url.path) }
                case .unreadable:
                    if age(of: url, now: now) > WatchVoice.resultLifetime { unlink(url.path) }
                case .corrupt:
                    unlink(url.path)
                }
            }
        }
        if let markers = directory(.markers) {
            for name in fileNames(in: markers) {
                let url = markers.appendingPathComponent(name)
                if age(of: url, now: now) > Self.markerLifetime { unlink(url.path) }
            }
        }
    }

    // MARK: Files

    private func directory(_ area: Area) -> URL? {
        guard let rootURL else { return nil }
        let url = rootURL.appendingPathComponent(area.rawValue, isDirectory: true)
        var isDirectory: ObjCBool = false
        if FileManager.default.fileExists(atPath: url.path, isDirectory: &isDirectory), isDirectory.boolValue {
            return url
        }
        do {
            try FileManager.default.createDirectory(
                at: url, withIntermediateDirectories: true,
                attributes: [.protectionKey: FileProtectionType.completeUntilFirstUserAuthentication]
            )
        } catch {
            return nil
        }
        // Nothing here is worth restoring, and an instruction must not end
        // up in a backup.
        var root = rootURL
        var values = URLResourceValues()
        values.isExcludedFromBackup = true
        try? root.setResourceValues(values)
        return url
    }

    private func markerURL(_ marker: Marker, for id: String) -> URL? {
        guard WatchVoiceIdentifier.isValid(id), let directory = directory(.markers) else { return nil }
        return directory.appendingPathComponent(id + "." + marker.rawValue)
    }

    private func fileNames(in directory: URL) -> [String] {
        (try? FileManager.default.contentsOfDirectory(atPath: directory.path)) ?? []
    }

    private func age(of url: URL, now: Date) -> TimeInterval {
        let attributes = try? FileManager.default.attributesOfItem(atPath: url.path)
        guard let date = attributes?[.creationDate] as? Date else { return .infinity }
        return now.timeIntervalSince(date)
    }

    private enum Loaded<Value> {
        case value(Value)
        /// Could not be opened: locked, or gone in the meantime.
        case unreadable
        /// Opened and not understood, or absurdly large.
        case corrupt
    }

    private func load<Value: Decodable>(_ type: Value.Type, at url: URL) -> Loaded<Value> {
        guard let attributes = try? FileManager.default.attributesOfItem(atPath: url.path) else { return .unreadable }
        guard let size = attributes[.size] as? NSNumber, size.intValue <= 128 * 1024 else { return .corrupt }
        guard let data = try? Data(contentsOf: url) else { return .unreadable }
        guard let value = WatchVoiceCodec.value(type, from: data) else { return .corrupt }
        return .value(value)
    }

    private func read<Value: Decodable>(_ type: Value.Type, at url: URL) -> Value? {
        guard let attributes = try? FileManager.default.attributesOfItem(atPath: url.path),
              let size = attributes[.size] as? NSNumber, size.intValue <= 128 * 1024,
              let data = try? Data(contentsOf: url) else { return nil }
        return WatchVoiceCodec.value(type, from: data)
    }

    private func writeState<Value: Encodable>(_ value: Value, name: String) {
        guard let root = directory(.markers)?.deletingLastPathComponent(),
              let data = try? WatchVoiceCodec.data(value) else { return }
        try? data.write(to: root.appendingPathComponent(name), options: .atomic)
    }

    private func readState<Value: Decodable>(_ type: Value.Type, name: String) -> Value? {
        guard let root = rootURL else { return nil }
        return read(type, at: root.appendingPathComponent(name))
    }
}

// MARK: - Signals

/// What one process tells the other: "look in the App Group".
///
/// Darwin notifications carry nothing, which is the point - no text crosses
/// here - and they cost nothing while nobody is listening. They reach a
/// process only while it is running, so each side also re-reads the store at
/// the moments it comes to life.
enum WatchVoiceSignal: String, CaseIterable, Sendable {
    /// App to keyboard: a new command, or one was cancelled.
    case command = "kz.ai-reply.watch.command"
    /// App to keyboard: the watch's reachability changed.
    case link = "kz.ai-reply.watch.link"
    /// Keyboard to app: a command moved on.
    case outcome = "kz.ai-reply.watch.outcome"
    /// Keyboard to app: a session began, ended, or started waiting.
    case session = "kz.ai-reply.watch.session"

    func post() {
        CFNotificationCenterPostNotification(CFNotificationCenterGetDarwinNotifyCenter(),
                                             CFNotificationName(rawValue as CFString), nil, nil, true)
    }
}

/// Listens for signals while it is alive.
final class WatchVoiceSignalObserver: @unchecked Sendable {

    private let handler: @Sendable (WatchVoiceSignal) -> Void

    init(signals: [WatchVoiceSignal], handler: @escaping @Sendable (WatchVoiceSignal) -> Void) {
        self.handler = handler
        let observer = Unmanaged.passUnretained(self).toOpaque()
        for signal in signals {
            CFNotificationCenterAddObserver(
                CFNotificationCenterGetDarwinNotifyCenter(), observer,
                { _, observer, name, _, _ in
                    guard let observer, let name, let signal = WatchVoiceSignal(rawValue: name.rawValue as String) else { return }
                    Unmanaged<WatchVoiceSignalObserver>.fromOpaque(observer).takeUnretainedValue().handler(signal)
                },
                signal.rawValue as CFString, nil, .deliverImmediately
            )
        }
    }

    deinit {
        CFNotificationCenterRemoveEveryObserver(CFNotificationCenterGetDarwinNotifyCenter(),
                                                Unmanaged.passUnretained(self).toOpaque())
    }
}
