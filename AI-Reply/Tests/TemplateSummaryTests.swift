import XCTest
@testable import AIReply

/// The compact cache the keyboard draws its first frame from.
final class TemplateSummaryTests: XCTestCase {

    private func makeSettings() -> (SharedSettings, UserDefaults) {
        let suite = "TemplateSummaryTests.\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: suite)!
        return (SharedSettings(defaults: defaults), defaults)
    }

    func testSummaryCarriesEveryLanguage() {
        let summary = TemplateSummary(template: .builtIn(.client, sortIndex: 0))
        XCTAssertEqual(summary.id, "client")
        XCTAssertEqual(summary.name(for: .english), "Client")
        XCTAssertEqual(summary.name(for: .russian), "Клиент")
        XCTAssertEqual(summary.name(for: .kazakh), "Клиент")

        let work = TemplateSummary(template: .builtIn(.work, sortIndex: 1))
        XCTAssertEqual(work.name(for: .kazakh), "Әріптес")
    }

    func testCustomNamesAreCarriedVerbatim() {
        let summary = TemplateSummary(template: .custom(name: "Supplier", sortIndex: 4))
        XCTAssertEqual(summary.name(for: .kazakh), "Supplier")
        XCTAssertEqual(summary.name(for: .russian), "Supplier")
    }

    func testRoundTripThroughSharedDefaults() {
        let (settings, _) = makeSettings()
        XCTAssertNil(settings.templateSummaries, "nothing written yet means nothing to read")

        let summaries = ReplyConfiguration.initial.visibleTemplates.map(TemplateSummary.init(template:))
        settings.setTemplateSummaries(summaries)

        let loaded = settings.templateSummaries
        XCTAssertEqual(loaded?.count, summaries.count)
        XCTAssertEqual(loaded?.map(\.id), summaries.map(\.id))
        XCTAssertEqual(loaded?.first?.name(for: .kazakh), "Дос")
    }

    /// The cache must stay small enough that reading it on the keyboard's
    /// appearance path is free. A whole configuration must never end up here.
    func testCacheStaysSmall() throws {
        var configuration = ReplyConfiguration.initial
        for index in 0..<8 {
            configuration.templates.append(.custom(name: "Custom \(index)", sortIndex: 10 + index))
        }
        let data = try JSONEncoder().encode(
            configuration.visibleTemplates.map(TemplateSummary.init(template:))
        )
        XCTAssertLessThan(data.count, 4_096, "the keyboard cache is meant to be a few hundred bytes")
    }

    func testHiddenTemplatesAreNotCached() {
        var configuration = ReplyConfiguration.initial
        configuration.templates[0].isVisible = false
        let summaries = configuration.visibleTemplates.map(TemplateSummary.init(template:))
        XCTAssertFalse(summaries.contains { $0.id == RelationshipKind.friend.rawValue })
    }
}
