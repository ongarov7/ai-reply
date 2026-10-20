import UIKit
import XCTest
@testable import AIReply

/// The shared emoji list, what each iOS version gets of it, and the recents.
final class EmojiCatalogTests: XCTestCase {

    /// The file shipped in the keyboard, read where it lives in the repo (the
    /// tests run in the app, which does not carry it).
    private static let shippedCatalog = URL(fileURLWithPath: #filePath)
        .deletingLastPathComponent()
        .deletingLastPathComponent()
        .appendingPathComponent("Shared/Keyboard/Emoji/emoji-catalog.txt")

    // MARK: Parsing

    func testParsesCategoriesAndVersionsInOrder() {
        let catalog = EmojiCatalog.parse("@smileys_emotion\n😀\t1.0\n🫠\t14.0\n@flags\n🏁\t0.6\n")
        XCTAssertEqual(catalog.categories.map(\.category), [.smileysEmotion, .flags])
        XCTAssertEqual(catalog.categories[0].entries, [
            EmojiCatalog.Entry(emoji: "😀", version: EmojiVersion(1, 0)),
            EmojiCatalog.Entry(emoji: "🫠", version: EmojiVersion(14, 0))
        ])
        XCTAssertEqual(catalog.categories[1].entries.map(\.emoji), ["🏁"])
    }

    /// A broken line never breaks the page: unknown categories, lines before
    /// any category, malformed versions, two emoji on one line and repeats
    /// are all left out.
    func testSkipsWhatItCannotUse() {
        let text = """
        😀\t1.0
        @smileys_emotion
        😀\t1.0
        😃
        😄\tx.y
        😁😆\t0.6
        😀\t1.0
        \t1.0
        @something_new
        🦄\t1.0
        @flags
        🏁\t0.6\r
        """
        let catalog = EmojiCatalog.parse(text)
        XCTAssertEqual(catalog.categories.map(\.category), [.smileysEmotion, .flags])
        XCTAssertEqual(catalog.categories[0].entries.map(\.emoji), ["😀"])
        XCTAssertEqual(catalog.categories[1].entries.map(\.emoji), ["🏁"])
    }

    // MARK: Versions

    func testIOSVersionDecidesTheNewestEmoji() {
        let os = { (major: Int, minor: Int) in OperatingSystemVersion(majorVersion: major, minorVersion: minor, patchVersion: 0) }
        XCTAssertEqual(EmojiVersion.supported(by: os(17, 0)), EmojiVersion(15, 0))
        XCTAssertEqual(EmojiVersion.supported(by: os(17, 3)), EmojiVersion(15, 0))
        XCTAssertEqual(EmojiVersion.supported(by: os(17, 4)), EmojiVersion(15, 1))
        XCTAssertEqual(EmojiVersion.supported(by: os(18, 0)), EmojiVersion(15, 1))
        XCTAssertEqual(EmojiVersion.supported(by: os(26, 5)), EmojiVersion(15, 1))
        XCTAssertLessThan(EmojiVersion(0, 6), EmojiVersion(0, 7))
        XCTAssertLessThan(EmojiVersion(13, 1), EmojiVersion(14, 0))
        XCTAssertNil(EmojiVersion("15"))
        XCTAssertEqual(EmojiVersion("15.1"), EmojiVersion(15, 1))
    }

    func testOlderSystemsDoNotGetNewerEmoji() {
        let catalog = EmojiCatalog.parse("@smileys_emotion\n😀\t1.0\n🙂‍↔️\t15.1\n@flags\n🐦‍🔥\t15.1\n")
        XCTAssertEqual(catalog.sections(supporting: EmojiVersion(15, 0)),
                       [EmojiSection(kind: .category(.smileysEmotion), emoji: ["😀"])],
                       "a category left with nothing drawable is left out")
        XCTAssertEqual(catalog.sections(supporting: EmojiVersion(15, 1)).map(\.emoji), [["😀", "🙂‍↔️"], ["🐦‍🔥"]])
    }

    // MARK: The shipped list

    func testShippedCatalogIsWhole() throws {
        let catalog = EmojiCatalog.parse(try String(contentsOf: Self.shippedCatalog, encoding: .utf8))
        XCTAssertEqual(catalog.categories.map(\.category), EmojiCategory.allCases)
        let entries = catalog.categories.flatMap(\.entries)
        XCTAssertGreaterThan(entries.count, 1_800)
        XCTAssertEqual(Set(entries.map(\.emoji)).count, entries.count, "no emoji twice")
        XCTAssertTrue(entries.allSatisfy { $0.emoji.count == 1 }, "one character per cell")
        XCTAssertTrue(entries.allSatisfy { $0.version <= EmojiVersion(15, 1) })
        for group in catalog.categories {
            XCTAssertFalse(group.entries.isEmpty, group.category.rawValue)
        }
        // iOS 17.0-17.3 loses only the 15.1 sequences, and every category.
        let base = catalog.sections(supporting: EmojiVersion(15, 0))
        XCTAssertEqual(base.count, EmojiCategory.allCases.count)
        let newer = entries.filter { $0.version == EmojiVersion(15, 1) }.count
        XCTAssertGreaterThan(newer, 0)
        XCTAssertEqual(base.flatMap(\.emoji).count, entries.count - newer)
    }

    // MARK: Page

    func testRecentsComeFirstWhenThereAreAny() {
        let catalog = EmojiCatalog.parse("@smileys_emotion\n😀\t1.0\n")
        XCTAssertEqual(catalog.page(recents: [], supporting: EmojiVersion(15, 1)).map(\.kind), [.category(.smileysEmotion)])
        XCTAssertEqual(catalog.page(recents: ["👍"], supporting: EmojiVersion(15, 1)).map(\.kind),
                       [.recents, .category(.smileysEmotion)])
    }

    /// Run on a newer simulator, so this proves only that the names are
    /// right; their availability on iOS 17.0 was checked against the SF
    /// Symbols availability table.
    func testEverySectionHasAnIconAndATitle() {
        let kinds: [EmojiSection.Kind] = [.recents] + EmojiCategory.allCases.map { .category($0) }
        XCTAssertEqual(Set(kinds.map(\.symbolName)).count, kinds.count)
        XCTAssertNotNil(UIImage(systemName: "face.smiling"), "the emoji key")
        for kind in kinds {
            XCTAssertNotNil(UIImage(systemName: kind.symbolName), kind.symbolName)
            let titles = KeyboardLanguage.allCases.map { KeyboardLabels($0).emojiSectionTitle(kind) }
            XCTAssertFalse(titles.contains(where: \.isEmpty))
            XCTAssertNotEqual(titles[0], titles[1], "\(kind) untranslated")
        }
        for language in KeyboardLanguage.allCases {
            let labels = KeyboardLabels(language)
            XCTAssertFalse(labels.emojiAccessibility.isEmpty)
            XCTAssertEqual(labels.planeKey(.emoji), labels.emojiAccessibility)
        }
    }

    // MARK: Recents

    func testRecentsAreMostRecentFirstOnceEachAndCapped() {
        var recents = RecentEmoji()
        recents.record("😀")
        recents.record("👍")
        recents.record("😀")
        XCTAssertEqual(recents.items, ["😀", "👍"])

        for index in 0..<40 { recents.record(String(UnicodeScalar(0x1F600 + index)!)) }
        XCTAssertEqual(recents.items.count, RecentEmoji.capacity)
        XCTAssertEqual(RecentEmoji.capacity, 30)
        XCTAssertEqual(recents.items.first, String(UnicodeScalar(0x1F600 + 39)!))
        XCTAssertFalse(recents.items.contains("👍"), "the oldest goes first")

        XCTAssertEqual(RecentEmoji(items: ["👍", "👍", "", "😀"]).items, ["👍", "😀"])
    }

    func testRecentsSurviveARelaunch() throws {
        let defaults = try XCTUnwrap(UserDefaults(suiteName: "EmojiCatalogTests.\(UUID().uuidString)"))
        let store = DefaultsRecentEmojiStore(defaults: defaults)
        XCTAssertEqual(store.load(), [])
        var recents = RecentEmoji(items: store.load())
        recents.record("🇰🇿")
        recents.record("❤️")
        store.save(recents.items)
        XCTAssertEqual(DefaultsRecentEmojiStore(defaults: defaults).load(), ["❤️", "🇰🇿"])
        XCTAssertNotNil(defaults.stringArray(forKey: DefaultsRecentEmojiStore.key))
    }

    /// The keyboard's own container: the App Group never sees them.
    func testRecentsNeverGoToTheAppGroup() {
        XCTAssertFalse(DefaultsRecentEmojiStore.key.hasPrefix("shared."))
        XCTAssertNil(SharedSettings.shared.defaults.object(forKey: DefaultsRecentEmojiStore.key))
    }
}
