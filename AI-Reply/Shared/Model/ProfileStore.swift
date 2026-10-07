import Foundation

/// Everything the user configured, as one value.
struct ReplyConfiguration: Codable, Hashable, Sendable {
    var profile: UserProfile
    var templates: [ReplyTemplate]

    static let initial = ReplyConfiguration(profile: .empty, templates: ReplyTemplate.defaults)

    /// Templates the keyboard bar should show, in order.
    var visibleTemplates: [ReplyTemplate] {
        templates.filter(\.isVisible).sorted { $0.sortIndex < $1.sortIndex }
    }

    func template(id: String) -> ReplyTemplate? {
        templates.first { $0.id == id }
    }

    /// Repairs anything that would leave the user stuck: missing built-ins
    /// after a decode, every template hidden, duplicate sort indices.
    func normalized() -> ReplyConfiguration {
        var copy = self
        for kind in RelationshipKind.builtIns where !copy.templates.contains(where: { $0.id == kind.rawValue }) {
            copy.templates.append(.builtIn(kind, sortIndex: copy.templates.count))
        }
        if !copy.templates.contains(where: \.isVisible) {
            for index in copy.templates.indices where copy.templates[index].isBuiltIn {
                copy.templates[index].isVisible = true
            }
        }
        copy.templates.sort { $0.sortIndex < $1.sortIndex }
        for index in copy.templates.indices {
            copy.templates[index].sortIndex = index
        }
        return copy
    }
}

/// Persistence for the profile and templates.
///
/// WHY A JSON FILE AND NOT SwiftData / Core Data: the keyboard extension has to
/// read this, and it has to do so without paying a store-stack setup cost every
/// time it appears inside WhatsApp. The whole configuration is a few kilobytes
/// that is written when a settings screen closes and read when the keyboard
/// appears. A database framework would add a dependency, a migration surface
/// and a launch cost to solve a problem this app does not have.
///
/// WHY NOT UserDefaults: the brief rules out copying an app database into
/// UserDefaults, and it is right to. The App Group container holds the file;
/// App Group *defaults* keep only the small flags in `SharedSettings`.
final class ProfileStore: @unchecked Sendable {

    static let shared = ProfileStore()

    /// Posted after a successful write. The keyboard does not observe this -
    /// it reloads when it appears - but the app's own screens do.
    static let didChangeNotification = Notification.Name("ReplyProfileStoreDidChange")

    private let fileURL: URL?
    private let queue = DispatchQueue(label: "kz.ai-reply.profilestore")
    private let settings: SharedSettings

    /// Guarded by `queue`.
    private var cached: ReplyConfiguration?
    private var cachedModificationDate: Date?

    init(
        containerURL: URL? = FileManager.default.containerURL(
            forSecurityApplicationGroupIdentifier: AppGroup.identifier
        ),
        settings: SharedSettings = .shared
    ) {
        self.fileURL = containerURL?.appendingPathComponent("reply-configuration.json")
        self.settings = settings
    }

    /// Whether a shared container was actually available. When false the store
    /// still works in memory for the lifetime of the process, so the UI degrades
    /// instead of crashing.
    var isPersistent: Bool { fileURL != nil }

    // MARK: Reading

    /// Reads the configuration, decoding only when the file has actually
    /// changed since the last read.
    ///
    /// PERFORMANCE. The keyboard calls this when it appears, never per
    /// keystroke. The modification-date check means a keyboard that appears
    /// repeatedly inside the same messenger session pays one `stat` rather than
    /// a full JSON decode.
    func load() -> ReplyConfiguration {
        queue.sync {
            guard let fileURL else {
                let value = cached ?? .initial
                cached = value
                return value
            }

            let modified = (try? FileManager.default.attributesOfItem(atPath: fileURL.path)[.modificationDate]) as? Date

            if let cached, modified == cachedModificationDate {
                return cached
            }

            guard let data = try? Data(contentsOf: fileURL),
                  let decoded = try? JSONDecoder().decode(ReplyConfiguration.self, from: data) else {
                let value = ReplyConfiguration.initial
                cached = value
                cachedModificationDate = modified
                return value
            }

            let value = decoded.normalized()
            cached = value
            cachedModificationDate = modified
            return value
        }
    }

    // MARK: Writing

    /// Persists and notifies. Synchronous because every caller is a settings
    /// screen leaving the foreground, never anything on a typing path.
    @discardableResult
    func save(_ configuration: ReplyConfiguration) -> Bool {
        let normalized = configuration.normalized()

        let written: Bool = queue.sync {
            cached = normalized
            guard let fileURL else { return false }
            guard let data = try? JSONEncoder().encode(normalized) else { return false }
            do {
                // Atomic: a keyboard reading mid-write sees either the old file
                // or the new one, never a truncated one.
                try data.write(to: fileURL, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
                cachedModificationDate = (try? FileManager.default
                    .attributesOfItem(atPath: fileURL.path)[.modificationDate]) as? Date
                return true
            } catch {
                return false
            }
        }

        // The keyboard draws its row from this summary before the full file is
        // read, so it is refreshed with every save rather than derived later.
        settings.setTemplateSummaries(normalized.visibleTemplates.map(TemplateSummary.init(template:)))

        NotificationCenter.default.post(name: Self.didChangeNotification, object: nil)
        return written
    }

    /// Convenience for a single-field edit.
    func update(_ mutate: (inout ReplyConfiguration) -> Void) {
        var configuration = load()
        mutate(&configuration)
        save(configuration)
    }
}
