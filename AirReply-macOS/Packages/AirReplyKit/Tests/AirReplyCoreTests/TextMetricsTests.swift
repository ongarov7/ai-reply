import Foundation
import Testing
@testable import AirReplyCore

@Suite("Text length as the server counts it")
struct TextMetricsTests {
    @Test func trimsWhitespaceAndNewlinesLikeGoTrimSpace() {
        #expect(TextMetrics.serverLength("  привет \n\t") == 6)
        #expect(TextMetrics.serverLength("\u{00A0}сәлем\u{3000}") == 5)
        #expect(TextMetrics.serverLength("   ") == 0)
    }

    @Test func countsCodePointsNotGraphemes() {
        // Family emoji: 1 grapheme, 5 code points (3 people + 2 joiners).
        #expect(TextMetrics.serverLength("👨‍👩‍👧") == 5)
        // Kazakh and Cyrillic letters are single code points.
        #expect(TextMetrics.serverLength("Қазақша") == 7)
        // A decomposed "й" (и + combining breve) is two code points.
        #expect(TextMetrics.serverLength("и\u{0306}") == 2)
    }

    @Test func limitIsInclusive() {
        let exact = String(repeating: "а", count: 400)
        #expect(TextMetrics.fits(exact, limit: 400))
        #expect(!TextMetrics.fits(exact + "б", limit: 400))
    }
}

@Suite("Configuration")
struct AppConfigurationTests {
    @Test func websiteURLKeepsTheBasePath() {
        let sandbox = AppConfiguration(backendBaseURL: URL(string: "https://ai-reply.kz/sandbox")!, appVersion: "1.0.0", build: "1")
        #expect(sandbox.websiteURL("payment").absoluteString == "https://ai-reply.kz/sandbox/payment")
        let production = AppConfiguration(backendBaseURL: URL(string: "https://ai-reply.kz")!, appVersion: "1.0.0", build: "1")
        #expect(production.websiteURL("/account/").absoluteString == "https://ai-reply.kz/account")
        #expect(production.websiteURL("payment", query: [URLQueryItem(name: "lang", value: "kk")]).absoluteString
            == "https://ai-reply.kz/payment?lang=kk")
    }

    @Test func rejectsNonHTTPSBackendFromInfoPlist() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        let bundleURL = directory.appendingPathComponent("Fake.bundle")
        try FileManager.default.createDirectory(at: bundleURL, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let info: [String: Any] = ["AIReplyBackendBaseURL": "http://example.com", "CFBundleShortVersionString": "2.0", "CFBundleVersion": "7"]
        let data = try PropertyListSerialization.data(fromPropertyList: info, format: .xml, options: 0)
        try data.write(to: bundleURL.appendingPathComponent("Info.plist"))
        let bundle = try #require(Bundle(url: bundleURL))
        let configuration = AppConfiguration.fromMainBundle(bundle)
        #expect(configuration.backendBaseURL == AppConfiguration.productionURL)
        #expect(configuration.appVersion == "2.0")
        #expect(configuration.build == "7")
        #expect(configuration.platform == "macos")
    }
}

@Suite("In-memory secure store")
struct SecureStoreTests {
    @Test func writeReadDelete() throws {
        let store = InMemorySecureStore()
        #expect(try store.read("token") == nil)
        try store.write(Data("a".utf8), for: "token")
        try store.write(Data("b".utf8), for: "token")
        #expect(try store.read("token") == Data("b".utf8))
        try store.delete("token")
        try store.delete("token")
        #expect(try store.read("token") == nil)
    }
}
