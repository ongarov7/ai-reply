import UIKit
import XCTest
@testable import AIReply

/// The four recipients - Друг, Клиент, Коллега, Партнёр - their icons, their
/// order, and every place their names are spelled (contract §3.1).
final class RecipientKindTests: XCTestCase {

    // MARK: Stored values

    /// Templates, the profile and `shared.lastTemplateID` store these, and
    /// the server takes them: renaming one would fail the whole decode.
    func testRawValuesNeverChange() {
        XCTAssertEqual(RelationshipKind.allCases.map(\.rawValue), ["friend", "client", "business", "work", "custom"])
    }

    func testNewInstallsGetFriendClientColleaguePartner() {
        XCTAssertEqual(RelationshipKind.builtIns, [.friend, .client, .work, .business])
        XCTAssertEqual(ReplyTemplate.defaults.map(\.id), ["friend", "client", "work", "business"])
        XCTAssertEqual(ReplyConfiguration.initial.visibleTemplates.map { $0.displayName(appLanguage: .russian) },
                       ["Друг", "Клиент", "Коллега", "Партнёр"])
    }

    /// A configuration saved by an older build lists Business before Work. It
    /// loads with the same values and the same order: only new installs get
    /// the new one.
    func testAnExistingConfigurationKeepsItsValuesAndOrder() throws {
        let stored = ReplyConfiguration(profile: .empty, templates: [
            .builtIn(.friend, sortIndex: 0),
            .builtIn(.client, sortIndex: 1),
            .builtIn(.business, sortIndex: 2),
            .builtIn(.work, sortIndex: 3)
        ])
        let data = try JSONEncoder().encode(stored)
        let json = try XCTUnwrap(String(data: data, encoding: .utf8))
        XCTAssertTrue(json.contains("\"business\""))
        XCTAssertTrue(json.contains("\"work\""))

        let loaded = try JSONDecoder().decode(ReplyConfiguration.self, from: data).normalized()
        XCTAssertEqual(loaded.visibleTemplates.map(\.id), ["friend", "client", "business", "work"])
        XCTAssertEqual(loaded.visibleTemplates.map(\.relationship), [.friend, .client, .business, .work])
        XCTAssertEqual(loaded.profile.activeRelationships, stored.profile.activeRelationships)
    }

    // MARK: Icons

    /// Run on a newer simulator, so this proves only that the names are
    /// spelled right; that each exists on iOS 17.0 was checked against the
    /// SF Symbols availability table (`name_availability.plist`).
    func testEveryKindHasItsOwnIcon() {
        for kind in RelationshipKind.allCases {
            XCTAssertNotNil(UIImage(systemName: kind.symbolName), "\(kind.rawValue): \(kind.symbolName)")
        }
        XCTAssertEqual(Set(RelationshipKind.allCases.map(\.symbolName)).count, RelationshipKind.allCases.count)
    }

    func testSummaryChipsGetTheTemplatesIcon() {
        for kind in RelationshipKind.builtIns {
            XCTAssertEqual(TemplateSummary(template: .builtIn(kind, sortIndex: 0)).relationship, kind)
        }
        XCTAssertEqual(TemplateSummary(template: .custom(name: "Supplier", sortIndex: 4)).relationship, .custom)
    }

    // MARK: Names

    /// The app's catalog and the keyboard's tables say the same thing in
    /// every language, so a persona is called the same on both sides.
    func testCatalogAndCodeSpellTheNamesAlike() throws {
        for language in AppLanguage.allCases {
            let path = try XCTUnwrap(
                Bundle.main.path(forResource: "Localizable", ofType: "strings", inDirectory: "\(language.rawValue).lproj")
            )
            let table = try XCTUnwrap(NSDictionary(contentsOfFile: path) as? [String: String])
            for kind in RelationshipKind.allCases {
                XCTAssertEqual(table["relationship.\(kind.rawValue)"],
                               ReplyTemplate.builtIn(kind, sortIndex: 0).displayName(appLanguage: language),
                               "\(kind.rawValue) in \(language.rawValue)")
            }
        }
    }

    /// «Кому отвечаем?»: the grid asks in the dative where the language needs
    /// it; a name the user typed is never declined.
    func testRecipientGridNamesTakeTheDative() {
        let names = { (language: AppLanguage) in
            RelationshipKind.builtIns.map { ReplyTemplate.builtIn($0, sortIndex: 0).recipientName(appLanguage: language) }
        }
        XCTAssertEqual(names(.russian), ["Другу", "Клиенту", "Коллеге", "Партнёру"])
        XCTAssertEqual(names(.kazakh), ["Досқа", "Клиентке", "Әріптеске", "Серіктеске"])
        XCTAssertEqual(names(.english), ["Friend", "Client", "Colleague", "Partner"])
        XCTAssertEqual(names(.uzbek), ["Do\u{02BB}st", "Mijoz", "Hamkasb", "Hamkor"])

        var renamed = ReplyTemplate.builtIn(.client, sortIndex: 1)
        renamed.setName("Покупатели")
        XCTAssertEqual(renamed.recipientName(appLanguage: .russian), "Покупатели")
        XCTAssertEqual(ReplyTemplate.custom(name: "Supplier", sortIndex: 4).recipientName(appLanguage: .kazakh), "Supplier")
    }

    // MARK: Keyboard summary

    /// A summary an older build wrote holds the old names, resolved. The app
    /// writes it again once, at launch, so the keyboard's first frame shows
    /// the new ones; a summary the current build wrote is left alone.
    func testOldSummariesAreRewrittenOnce() throws {
        let defaults = try XCTUnwrap(UserDefaults(suiteName: "RecipientKindTests.\(UUID().uuidString)"))
        let settings = SharedSettings(defaults: defaults)
        let old = [TemplateSummary(id: "business", names: ["ru": "Бизнес"]), TemplateSummary(id: "work", names: ["ru": "Работа"])]
        defaults.set(try JSONEncoder().encode(old), forKey: "shared.templateSummaries")

        XCTAssertTrue(settings.refreshTemplateSummariesIfOutdated(from: .initial))
        XCTAssertEqual(settings.templateSummaries?.map { $0.name(for: .russian) }, ["Друг", "Клиент", "Коллега", "Партнёр"])
        XCTAssertFalse(settings.refreshTemplateSummariesIfOutdated(from: .initial), "only once")

        let current = SharedSettings(defaults: try XCTUnwrap(UserDefaults(suiteName: "RecipientKindTests.\(UUID().uuidString)")))
        current.setTemplateSummaries([TemplateSummary(template: .custom(name: "Supplier", sortIndex: 0))])
        XCTAssertFalse(current.refreshTemplateSummariesIfOutdated(from: .initial))
        XCTAssertEqual(current.templateSummaries?.map(\.id).count, 1, "a save by this build is the truth")
    }
}
