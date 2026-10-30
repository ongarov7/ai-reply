import Foundation
import Testing

/// Every interface string is a key in the app's catalog, translated into
/// Russian, Kazakh and English with the same placeholders.
@Suite("String catalog")
struct LocalizationTests {
    private static let root = URL(fileURLWithPath: #filePath)
        .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
    private static let sources = root.appendingPathComponent("Sources/AirReplyUI")
    private static let catalogURL = root.appendingPathComponent("../../AirReply/Localizable.xcstrings").standardizedFileURL
    private static let prefixes = ["bar.", "copied.", "recipient.", "voice.", "result.", "balance.", "issue.", "consent.",
                                   "signin.", "menu.", "settings.", "language."]

    private static func catalog() throws -> [String: [String: String]] {
        let data = try Data(contentsOf: catalogURL)
        let json = try #require(try JSONSerialization.jsonObject(with: data) as? [String: Any])
        let strings = try #require(json["strings"] as? [String: [String: Any]])
        return strings.mapValues { entry in
            let localizations = entry["localizations"] as? [String: [String: [String: String]]] ?? [:]
            return localizations.compactMapValues { $0["stringUnit"]?["value"] }
        }
    }

    /// Keys as written in Swift, with interpolations reduced to their count.
    private static func keysInSources() throws -> [String: Int] {
        var keys: [String: Int] = [:]
        let files = try FileManager.default.contentsOfDirectory(at: sources, includingPropertiesForKeys: nil)
        let literal = try NSRegularExpression(pattern: #""([a-z]+\.[a-z0-9_.]+)((?: \\\((?:[^()"]|\([^()]*\)|"[^"]*")*\))*)""#)
        for file in files where file.pathExtension == "swift" {
            let text = try String(contentsOf: file, encoding: .utf8)
            for match in literal.matches(in: text, range: NSRange(text.startIndex..., in: text)) {
                let key = String(text[Range(match.range(at: 1), in: text)!])
                guard prefixes.contains(where: key.hasPrefix) else { continue }
                let arguments = String(text[Range(match.range(at: 2), in: text)!]).components(separatedBy: "\\(").count - 1
                keys[key] = arguments
            }
        }
        return keys
    }

    private static func baseKey(_ catalogKey: String) -> (String, Int) {
        let parts = catalogKey.split(separator: " ")
        return (String(parts[0]), parts.count - 1)
    }

    private static func placeholders(_ value: String) -> Int {
        value.components(separatedBy: "%").dropFirst().filter { !$0.hasPrefix("%") }.count
    }

    @Test func everyKeyInTheCodeIsTranslated() throws {
        let catalog = try Self.catalog()
        let declared = Dictionary(uniqueKeysWithValues: catalog.keys.map(Self.baseKey))
        for (key, arguments) in try Self.keysInSources() {
            #expect(declared[key] == arguments, "missing or different placeholders for \(key)")
        }
    }

    @Test func everyEntryHasThreeLanguagesWithMatchingPlaceholders() throws {
        for (key, values) in try Self.catalog() {
            let (_, arguments) = Self.baseKey(key)
            #expect(Set(values.keys) == ["ru", "kk", "en"], "\(key) languages: \(values.keys.sorted())")
            for (language, value) in values {
                #expect(!value.trimmingCharacters(in: .whitespaces).isEmpty, "\(key) [\(language)] is empty")
                #expect(Self.placeholders(value) == arguments, "\(key) [\(language)] placeholders")
            }
        }
    }

    @Test func noUnusedEntries() throws {
        let used = Set(try Self.keysInSources().keys)
        let declared = Set(try Self.catalog().keys.map { Self.baseKey($0).0 })
        #expect(declared.subtracting(used).isEmpty, "unused: \(declared.subtracting(used).sorted())")
    }
}
