import Foundation

/// The latest screenshot and what was read from it, in the App Group, so the
/// keyboard can answer it in the chat.
///
/// PRIVACY. Written only after the person chose a screenshot (the shortcut
/// they run themselves, or the photo picker). Complete file protection: the
/// files cannot be read while the iPhone is locked. Excluded from backups.
/// Removed after `ScreenshotContext.lifetime`, on Delete, on a new screenshot
/// and on sign-out; never sent anywhere by this type.
struct ScreenshotContextStore: Sendable {
    let rootURL: URL?

    static let shared = ScreenshotContextStore(rootURL: FileManager.default
        .containerURL(forSecurityApplicationGroupIdentifier: AppGroup.identifier)?
        .appendingPathComponent("Screenshot", isDirectory: true))

    private static let contextFile = "context.json"
    private static let imageFile = "image.jpg"
    private static let maximumContextBytes = 512 * 1024
    static let maximumImageBytes = 8 * 1024 * 1024

    // MARK: Read

    /// The current screenshot, or nil. A stale one is deleted on the way.
    func read(now: Date = Date()) -> ScreenshotContext? {
        guard let rootURL else { return nil }
        let url = rootURL.appendingPathComponent(Self.contextFile)
        guard let size = try? url.resourceValues(forKeys: [.fileSizeKey]).fileSize,
              size <= Self.maximumContextBytes,
              let data = try? Data(contentsOf: url),
              let context = try? JSONDecoder().decode(ScreenshotContext.self, from: data) else { return nil }
        guard context.isFresh(now: now) else {
            delete()
            return nil
        }
        return context
    }

    /// The stored picture (JPEG), for the composer's preview and image mode.
    func imageData(for context: ScreenshotContext) -> Data? {
        guard let rootURL else { return nil }
        let url = rootURL.appendingPathComponent(Self.imageFile)
        guard let size = try? url.resourceValues(forKeys: [.fileSizeKey]).fileSize,
              size <= Self.maximumImageBytes else { return nil }
        return try? Data(contentsOf: url)
    }

    // MARK: Write

    /// A new screenshot replaces the old one, picture and text together.
    @discardableResult
    func save(_ context: ScreenshotContext, image: Data?) -> Bool {
        guard let root = directory() else { return false }
        do {
            if let image {
                guard image.count <= Self.maximumImageBytes else { return false }
                try image.write(to: root.appendingPathComponent(Self.imageFile), options: [.atomic, .completeFileProtection])
            }
            try write(context, root: root)
            ScreenshotSignal.post()
            return true
        } catch {
            return false
        }
    }

    /// The same screenshot with new text, a chosen message or a draft.
    @discardableResult
    func update(_ change: (inout ScreenshotContext) -> Void, now: Date = Date()) -> ScreenshotContext? {
        guard var context = read(now: now), let root = directory() else { return nil }
        change(&context)
        do {
            try write(context, root: root)
            ScreenshotSignal.post()
            return context
        } catch {
            return nil
        }
    }

    /// Everything goes: picture, text and draft.
    func delete() {
        guard let rootURL else { return }
        let manager = FileManager.default
        for name in [Self.contextFile, Self.imageFile] {
            try? manager.removeItem(at: rootURL.appendingPathComponent(name))
        }
        ScreenshotSignal.post()
    }

    /// Removes a screenshot past its lifetime (also called on launch).
    func purge(now: Date = Date()) {
        _ = read(now: now)
    }

    private func write(_ context: ScreenshotContext, root: URL) throws {
        let data = try JSONEncoder().encode(context)
        guard data.count <= Self.maximumContextBytes else { throw CocoaError(.fileWriteOutOfSpace) }
        try data.write(to: root.appendingPathComponent(Self.contextFile), options: [.atomic, .completeFileProtection])
    }

    private func directory() -> URL? {
        guard var root = rootURL else { return nil }
        do {
            try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true,
                                                    attributes: [.protectionKey: FileProtectionType.complete])
            var values = URLResourceValues()
            values.isExcludedFromBackup = true
            try root.setResourceValues(values)
            return root
        } catch {
            return nil
        }
    }
}

/// "The latest screenshot changed" between the app and the keyboard. Carries
/// nothing: whoever cares reads the store.
enum ScreenshotSignal {
    static let name = "kz.ai-reply.screenshot.changed"

    static func post() {
        CFNotificationCenterPostNotification(CFNotificationCenterGetDarwinNotifyCenter(),
                                             CFNotificationName(name as CFString), nil, nil, true)
    }
}

/// Observes `ScreenshotSignal` while alive.
final class ScreenshotSignalObserver: @unchecked Sendable {
    private let handler: @Sendable () -> Void

    init(handler: @escaping @Sendable () -> Void) {
        self.handler = handler
        CFNotificationCenterAddObserver(CFNotificationCenterGetDarwinNotifyCenter(),
                                        Unmanaged.passUnretained(self).toOpaque(),
                                        { _, observer, _, _, _ in
                                            guard let observer else { return }
                                            Unmanaged<ScreenshotSignalObserver>.fromOpaque(observer)
                                                .takeUnretainedValue().handler()
                                        },
                                        ScreenshotSignal.name as CFString, nil, .deliverImmediately)
    }

    deinit {
        CFNotificationCenterRemoveEveryObserver(CFNotificationCenterGetDarwinNotifyCenter(),
                                                Unmanaged.passUnretained(self).toOpaque())
    }
}
