import Darwin
import Foundation

/// Lock-free IPC: one atomic snapshot writer and immutable per-command files.
/// No shared locks survive process suspension. Snapshot files use Class B:
/// atomic creation may finish while locked, but the closed file cannot be
/// reopened until unlock. Synchronous writes retain no file descriptors.
/// Text is excluded from backups, bounded in size and short lived.
struct VoiceAssistantStore: Sendable {
    let rootURL: URL?

    static let shared = VoiceAssistantStore(rootURL: FileManager.default
        .containerURL(forSecurityApplicationGroupIdentifier: AppGroup.identifier)?
        .appendingPathComponent("VoiceAssistant", isDirectory: true))

    private static let maximumRecordBytes = 128 * 1024

    func readSnapshot(now: Date = Date()) -> VoiceAssistantSnapshot? {
        guard let rootURL,
              let value: VoiceAssistantSnapshot = read(rootURL.appendingPathComponent("session.json")),
              value.isFresh(now: now) else { return nil }
        return value
    }

    @discardableResult
    func writeSnapshot(_ value: VoiceAssistantSnapshot) -> Bool {
        guard let root = directory(), validID(value.sessionID),
              let data = try? JSONEncoder().encode(value), data.count <= Self.maximumRecordBytes else { return false }
        do {
            try data.write(to: root.appendingPathComponent("session.json"),
                           options: [.atomic, .completeFileProtectionUnlessOpen])
            VoiceAssistantSignal.snapshot.post()
            return true
        } catch { return false }
    }

    /// A duplicate command ID cannot overwrite an earlier request.
    @discardableResult
    func send(_ command: VoiceAssistantCommand) -> Bool {
        guard validID(command.id), validID(command.sessionID), let root = directory(),
              let data = try? JSONEncoder().encode(command), data.count <= Self.maximumRecordBytes else { return false }
        guard files().filter({ $0.pathExtension == "command" }).count < 64 else { return false }
        let target = root.appendingPathComponent(command.id + ".command")
        let temporary = root.appendingPathComponent("." + UUID().uuidString + ".tmp")
        let protection: Data.WritingOptions = command.sourceMessage == nil
            ? .completeFileProtectionUntilFirstUserAuthentication : .completeFileProtection
        do { try data.write(to: temporary, options: protection) }
        catch { return false }
        defer { unlink(temporary.path) }
        guard Darwin.link(temporary.path, target.path) == 0 else { return false }
        VoiceAssistantSignal.command.post()
        return true
    }

    func pendingCommands(sessionID: String, now: Date = Date()) -> [VoiceAssistantCommand] {
        guard rootURL != nil else { return [] }
        return files().filter { $0.pathExtension == "command" }.compactMap { url in
            let command: VoiceAssistantCommand? = read(url)
            guard let command, validID(command.id), command.sessionID == sessionID,
                  command.isFresh(now: now) else { return nil }
            return command
        }.sorted { $0.createdAt == $1.createdAt ? $0.id < $1.id : $0.createdAt < $1.createdAt }
    }

    func removeCommand(_ command: VoiceAssistantCommand) {
        guard validID(command.id), let rootURL else { return }
        try? FileManager.default.removeItem(at: rootURL.appendingPathComponent(command.id + ".command"))
    }

    func purge(now: Date = Date()) {
        for url in files() {
            if url.lastPathComponent == "session.json" {
                if let value: VoiceAssistantSnapshot = read(url), !value.isFresh(now: now) {
                    try? FileManager.default.removeItem(at: url)
                }
            } else if url.pathExtension == "command" {
                let command: VoiceAssistantCommand? = read(url)
                if command?.isFresh(now: now) != true { try? FileManager.default.removeItem(at: url) }
            } else if url.pathExtension == "tmp",
                      let date = try? url.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate,
                      now.timeIntervalSince(date) > 180 {
                try? FileManager.default.removeItem(at: url)
            }
        }
    }

    private func directory() -> URL? {
        guard var root = rootURL else { return nil }
        do {
            try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true,
                                                    attributes: [.protectionKey: FileProtectionType.completeUntilFirstUserAuthentication])
            var values = URLResourceValues()
            values.isExcludedFromBackup = true
            try root.setResourceValues(values)
            return root
        } catch { return nil }
    }

    private func files() -> [URL] {
        guard let rootURL else { return [] }
        return (try? FileManager.default.contentsOfDirectory(at: rootURL,
                    includingPropertiesForKeys: [.contentModificationDateKey])) ?? []
    }

    private func read<T: Decodable>(_ url: URL) -> T? {
        guard let size = try? url.resourceValues(forKeys: [.fileSizeKey]).fileSize,
              size <= Self.maximumRecordBytes, let data = try? Data(contentsOf: url) else { return nil }
        return try? JSONDecoder().decode(T.self, from: data)
    }

    private func validID(_ value: String) -> Bool {
        !value.isEmpty && value.count <= 100 && value.unicodeScalars.allSatisfy {
            CharacterSet.alphanumerics.contains($0) || $0 == "-" || $0 == "_"
        }
    }
}

enum VoiceAssistantSignal: String, Sendable {
    case snapshot = "kz.ai-reply.voice-assistant.snapshot"
    case command = "kz.ai-reply.voice-assistant.command"

    func post() {
        CFNotificationCenterPostNotification(CFNotificationCenterGetDarwinNotifyCenter(),
            CFNotificationName(rawValue as CFString), nil, nil, true)
    }
}

final class VoiceAssistantSignalObserver: @unchecked Sendable {
    private let handler: @Sendable (VoiceAssistantSignal) -> Void

    init(signals: [VoiceAssistantSignal], handler: @escaping @Sendable (VoiceAssistantSignal) -> Void) {
        self.handler = handler
        let pointer = Unmanaged.passUnretained(self).toOpaque()
        for signal in signals {
            CFNotificationCenterAddObserver(CFNotificationCenterGetDarwinNotifyCenter(), pointer,
                { _, observer, name, _, _ in
                    guard let observer, let name,
                          let signal = VoiceAssistantSignal(rawValue: name.rawValue as String) else { return }
                    Unmanaged<VoiceAssistantSignalObserver>.fromOpaque(observer).takeUnretainedValue().handler(signal)
                }, signal.rawValue as CFString, nil, .deliverImmediately)
        }
    }

    deinit {
        CFNotificationCenterRemoveEveryObserver(CFNotificationCenterGetDarwinNotifyCenter(),
                                                Unmanaged.passUnretained(self).toOpaque())
    }
}
