import Foundation
import CryptoKit
import Observation

public enum ContextProfileRole: String, Codable, CaseIterable, Sendable {
    case personal, business
}

public struct ReferenceSegment: Codable, Equatable, Sendable {
    public var text: String
    public var section: String?
    public init(text: String, section: String? = nil) { self.text = text; self.section = section }
}

public struct ContextAttachment: Codable, Equatable, Identifiable, Sendable {
    public var id: UUID
    public var name: String
    public var format: String
    public var segments: [ReferenceSegment]
    public var importedAt: Date
    public init(id: UUID = UUID(), name: String, format: String, segments: [ReferenceSegment], importedAt: Date = Date()) {
        self.id = id; self.name = name; self.format = format; self.segments = segments; self.importedAt = importedAt
    }
}

public struct ContextProfile: Codable, Equatable, Identifiable, Sendable {
    public var id: UUID
    public var name: String
    public var role: ContextProfileRole
    public var occupation: String
    public var personName: String
    public var company: String
    public var industry: String
    public var communicationStyle: String
    public var products: String
    public var services: String
    public var policies: String
    public var description: String
    public var instructions: String
    public var attachments: [ContextAttachment]
    public init(id: UUID = UUID(), name: String, role: ContextProfileRole = .personal,
                occupation: String = "", personName: String = "", company: String = "", industry: String = "", communicationStyle: String = "",
                products: String = "", services: String = "", policies: String = "", description: String = "",
                instructions: String = "", attachments: [ContextAttachment] = []) {
        self.id = id; self.name = name; self.role = role; self.occupation = occupation; self.personName = personName; self.company = company
        self.industry = industry; self.communicationStyle = communicationStyle; self.products = products
        self.services = services; self.policies = policies; self.description = description
        self.instructions = instructions; self.attachments = attachments
    }
}

public enum ContextStorageError: Error, Equatable, Sendable {
    case invalidProfile, tooLarge, invalidStorage
}

/// Profile descriptions and parsed reference caches remain on this Mac. Only
/// the encryption key is in the existing ThisDeviceOnly keychain; the larger
/// cache is an authenticated encrypted file with an atomic replacement.
@MainActor
@Observable
public final class ContextProfileStore {
    public private(set) var profiles: [ContextProfile] = []
    private let files: ContextProfileFile
    private let worker: ContextProfilePersistence
    private var revision = 0

    public init(store: any SecureStore = KeychainStore(service: "kz.ai-reply.macos.context"), directory: URL? = nil) {
        let directory = directory ?? FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("AIReply/Context", isDirectory: true)
        let files = ContextProfileFile(store: store, directory: directory)
        self.files = files; worker = ContextProfilePersistence(files: files)
    }

    /// Synchronous access is retained for small caches and deterministic tests.
    /// UI actions use the async variants so XML indexes never block the main actor.
    public func load() throws { profiles = try files.load(); revision += 1 }
    public func save(_ profile: ContextProfile) throws { try persist(updating(profile)) }
    public func remove(_ id: UUID) throws { try persist(profiles.filter { $0.id != id }) }
    public func removeAttachment(profileID: UUID, attachmentID: UUID) throws {
        guard var profile = profiles.first(where: { $0.id == profileID }) else { return }
        profile.attachments.removeAll { $0.id == attachmentID }
        try save(profile)
    }
    public func removeAll() throws { try files.removeAll(); profiles = []; revision += 1 }

    public func loadAsync() async throws {
        let expected = revision
        let next = try await worker.load()
        guard revision == expected else { return }
        profiles = next
    }
    public func saveAsync(_ profile: ContextProfile) async throws { try await persistAsync(updating(profile)) }
    public func removeAsync(_ id: UUID) async throws { try await persistAsync(profiles.filter { $0.id != id }) }
    public func removeAttachmentAsync(profileID: UUID, attachmentID: UUID) async throws {
        guard var profile = profiles.first(where: { $0.id == profileID }) else { return }
        profile.attachments.removeAll { $0.id == attachmentID }
        try await saveAsync(profile)
    }
    public func removeAllAsync() async throws {
        let previous = profiles
        revision += 1
        let expected = revision
        profiles = []
        do { try await worker.removeAll(revision: expected) }
        catch { if revision == expected { profiles = previous }; throw error }
    }

    private func updating(_ profile: ContextProfile) throws -> [ContextProfile] {
        try ContextProfileFile.validate(profile)
        var next = profiles
        if let i = next.firstIndex(where: { $0.id == profile.id }) { next[i] = profile } else { next.append(profile) }
        guard next.count <= 24 else { throw ContextStorageError.tooLarge }
        return next
    }
    private func persist(_ next: [ContextProfile]) throws { try files.persist(next); profiles = next; revision += 1 }
    private func persistAsync(_ next: [ContextProfile]) async throws {
        let previous = profiles
        revision += 1
        let expected = revision
        // Optimistic metadata is cheap (arrays are copy-on-write); the worker
        // serializes and encrypts off-main, discarding any stale revision.
        profiles = next
        do { try await worker.persist(next, revision: expected) }
        catch { if revision == expected { profiles = previous }; throw error }
    }
}

private actor ContextProfilePersistence {
    let files: ContextProfileFile
    private var revision = 0
    init(files: ContextProfileFile) { self.files = files }
    func load() throws -> [ContextProfile] { try files.load() }
    func persist(_ profiles: [ContextProfile], revision: Int) throws {
        guard revision >= self.revision else { return }
        self.revision = revision
        try files.persist(profiles)
    }
    func removeAll(revision: Int) throws {
        guard revision >= self.revision else { return }
        self.revision = revision
        try files.removeAll()
    }
}

private struct ContextProfileFile: Sendable {
    let store: any SecureStore
    let directory: URL
    private let keyName = "context.profiles.encryption-key.v1"
    private let maximumBytes = 32 * 1024 * 1024
    private var file: URL { directory.appendingPathComponent("profiles-v1.aicontext") }

    func load() throws -> [ContextProfile] {
        guard FileManager.default.fileExists(atPath: file.path) else { return [] }
        let attributes = try file.resourceValues(forKeys: [.isRegularFileKey, .isSymbolicLinkKey, .fileSizeKey])
        guard attributes.isRegularFile == true, attributes.isSymbolicLink != true,
              (attributes.fileSize ?? maximumBytes + 1) <= maximumBytes else { throw ContextStorageError.invalidStorage }
        let encrypted = try Data(contentsOf: file)
        guard let key = try store.read(keyName), key.count == 32 else { throw ContextStorageError.invalidStorage }
        let plain = try AES.GCM.open(try AES.GCM.SealedBox(combined: encrypted), using: SymmetricKey(data: key))
        let decoded = try JSONDecoder().decode([ContextProfile].self, from: plain)
        guard decoded.count <= 24 else { throw ContextStorageError.tooLarge }
        try decoded.forEach(Self.validate)
        return decoded
    }
    static func validate(_ profile: ContextProfile) throws {
        let fields = [profile.occupation, profile.personName, profile.company, profile.industry, profile.communicationStyle,
                      profile.products, profile.services, profile.policies, profile.description, profile.instructions]
        guard !profile.name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty, profile.name.count <= 80,
              fields.allSatisfy({ $0.unicodeScalars.count <= 4000 }), profile.attachments.count <= 8 else {
            throw ContextStorageError.invalidProfile
        }
    }
    func persist(_ next: [ContextProfile]) throws {
        let plain = try JSONEncoder().encode(next)
        guard plain.count < maximumBytes - 128 else { throw ContextStorageError.tooLarge }
        let key: Data
        if let existing = try store.read(keyName) {
            guard existing.count == 32 else { throw ContextStorageError.invalidStorage }
            key = existing
        } else {
            key = SymmetricKey(size: .bits256).withUnsafeBytes { Data($0) }
            try store.write(key, for: keyName)
        }
        guard let sealed = try AES.GCM.seal(plain, using: SymmetricKey(data: key)).combined else { throw ContextStorageError.invalidStorage }
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        let attributes = try directory.resourceValues(forKeys: [.isDirectoryKey, .isSymbolicLinkKey])
        guard attributes.isDirectory == true, attributes.isSymbolicLink != true else { throw ContextStorageError.invalidStorage }
        try sealed.write(to: file, options: .atomic)
        try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: file.path)
    }
    func removeAll() throws {
        if FileManager.default.fileExists(atPath: file.path) { try FileManager.default.removeItem(at: file) }
        try store.delete(keyName)
    }
}

public enum OutputLength: Int, CaseIterable, Codable, Sendable {
    case standard = 400
    case extended = 600
}

public struct DesktopGenerationContext: Codable, Equatable, Sendable {
    public var profile: String?
    public var references: [String]
    public init(profile: String? = nil, references: [String] = []) { self.profile = profile; self.references = references }
}

public struct GenerationOptions: Equatable, Sendable {
    public var context: DesktopGenerationContext?
    public var outputCharacterLimit: Int?
    public var recipient: Recipient?
    public init(context: DesktopGenerationContext? = nil, outputCharacterLimit: Int? = nil, recipient: Recipient? = nil) {
        self.context = context; self.outputCharacterLimit = outputCharacterLimit; self.recipient = recipient
    }
}

/// Local lexical retrieval: no embeddings, network calls, or generation credits.
/// The budget is in Unicode scalar characters. Tokens vary by language, so the
/// server also enforces a separate input/output token ceiling.
public enum ContextRetriever {
    public static func context(profile: ContextProfile?, query: String, budget: Int = 4000) -> DesktopGenerationContext? {
        guard let profile else { return nil }
        let fields: [(String, String)] = [("Name", profile.personName), ("Occupation", profile.occupation), ("Company", profile.company), ("Industry", profile.industry),
            ("Style", profile.communicationStyle), ("Products", profile.products), ("Services", profile.services),
            ("Policies", profile.policies), ("Description", profile.description), ("Preferences", profile.instructions)]
        let availableBytes = max(0, min(3072, budget) - query.utf8.count)
        let profileText = boundedBytes(bounded(fields.filter { !$0.1.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }
            .map { "\($0.0): \($0.1)" }.joined(separator: "\n"), maximum: 1500), maximum: min(1000, availableBytes))
        let terms = keywords(query)
        let queryWords = query.folding(options: [.caseInsensitive, .diacriticInsensitive], locale: .current)
            .components(separatedBy: CharacterSet.alphanumerics.inverted).filter { !$0.isEmpty && !stopWords.contains($0) }
        let phrases = zip(queryWords, queryWords.dropFirst()).map { "\($0) \($1)" }
        let productNames = zip(queryWords, queryWords.dropFirst()).filter {
            ["product", "товар", "продукт", "өнім"].contains($0.0)
        }.map { $0.1 }
        let queryLower = query.folding(options: [.caseInsensitive, .diacriticInsensitive], locale: .current)
        var ranked: [(Int, Int, String)] = []
        var order = 0
        for attachment in profile.attachments {
            for segment in attachment.segments {
                let normalized = segment.text.folding(options: [.caseInsensitive, .diacriticInsensitive], locale: .current)
                let segmentTerms = keywords(segment.text)
                guard productNames.allSatisfy({ segmentTerms.contains($0) }) else { order += 1; continue }
                // A full product phrase gets preference over a header containing
                // one common word. Row segments retain their actual header/value pairs.
                let matches = terms.intersection(segmentTerms)
                var score = matches.reduce(0) { $0 + min(12, $1.count) }
                if !queryLower.isEmpty, normalized.contains(queryLower) { score += 30 }
                if phrases.contains(where: { normalized.contains($0) }) { score += 30 }
                if score > 0 {
                    let section = segment.section.map { " · \($0)" } ?? ""
                    ranked.append((score, order, "[\(attachment.name)\(section)]\n\(segment.text)"))
                }
                order += 1
            }
        }
        ranked.sort { $0.0 == $1.0 ? $0.1 < $1.1 : $0.0 > $1.0 }
        var remaining = max(0, availableBytes - profileText.utf8.count)
        let threshold = max(1, (ranked.first?.0 ?? 0) / 2)
        var references: [String] = []
        for item in ranked.prefix(8) {
            // Keep structured rows intact. A partial price or stock record is
            // worse evidence than omitting it and asking for clarification.
            let length = item.2.utf8.count
            guard item.0 >= threshold, length <= remaining else { continue }
            references.append(item.2)
            remaining -= length
        }
        if profileText.isEmpty && references.isEmpty { return nil }
        return DesktopGenerationContext(profile: profileText.isEmpty ? nil : profileText, references: references)
    }

    private static let stopWords: Set<String> = ["the", "and", "for", "what", "with", "hello", "please", "this", "that", "price", "cost", "about", "is", "of", "how", "much", "an",
        "как", "что", "это", "для", "здравствуйте", "пожалуйста", "цена", "стоимость", "есть", "можно", "нужно",
        "сәлем", "үшін", "қандай", "бағасы", "қанша", "бар"]

    private static func keywords(_ text: String) -> Set<String> {
        Set(text.folding(options: [.caseInsensitive, .diacriticInsensitive], locale: .current)
            .components(separatedBy: CharacterSet.alphanumerics.inverted)
            .filter { !$0.isEmpty && !stopWords.contains($0) }.prefix(300))
    }

    private static func boundedBytes(_ text: String, maximum: Int) -> String {
        guard text.utf8.count > maximum else { return text }
        var result = ""
        for word in text.split(whereSeparator: \.isWhitespace) {
            let addition = (result.isEmpty ? "" : " ") + word
            guard result.utf8.count + addition.utf8.count <= maximum else { break }
            result += addition
        }
        return result
    }

    static func bounded(_ text: String, maximum: Int) -> String {
        guard text.unicodeScalars.count > maximum else { return text }
        let prefix = String(String.UnicodeScalarView(text.unicodeScalars.prefix(maximum)))
        guard let end = prefix.rangeOfCharacter(from: .whitespacesAndNewlines, options: .backwards) else { return "" }
        return String(prefix[..<end.lowerBound])
    }
}
