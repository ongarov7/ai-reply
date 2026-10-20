import UIKit
import XCTest
@testable import AIReply

/// The emoji key and the emoji page (contract §3.10): where the key goes and
/// where it does not, the page's height and bar, and that an emoji is one
/// character to delete and never a word to correct.
final class EmojiPageTests: XCTestCase {

    private let portraitWidths: [CGFloat] = [320, 375, 390, 393, 402, 414, 428, 430, 440]

    private func sizings() -> [KeyboardSizing] {
        portraitWidths.map { KeyboardSizing(width: $0, isLandscape: false) }
            + [KeyboardSizing(width: 667, isLandscape: true), KeyboardSizing(width: 844, isLandscape: true)]
    }

    private func bottomRow(_ options: KeyboardPageOptions, plane: KeyboardPlane = .letters,
                           language: KeyboardLanguage = .russian) -> [KeyAction] {
        let page = KeyboardLayout.page(language: language, plane: plane, options: options)
        return page.rows.last?.slots.compactMap(\.action) ?? []
    }

    // MARK: The key

    func testEmojiKeyComesRightAfter123() {
        var options = KeyboardPageOptions()
        options.showsEmojiKey = true
        for language in KeyboardLanguage.allCases {
            XCTAssertEqual(bottomRow(options, language: language),
                           [.plane(.numbers), .plane(.emoji), .nextLanguage, .space, .returnKey])
        }
        options.showsNextKeyboardKey = true
        XCTAssertEqual(bottomRow(options), [.plane(.numbers), .plane(.emoji), .nextKeyboard, .nextLanguage, .space, .returnKey])
        options.showsLanguageKey = false
        XCTAssertEqual(bottomRow(options), [.plane(.numbers), .plane(.emoji), .nextKeyboard, .space, .returnKey])
    }

    /// Letters only: 123 and #+= keep their rows exactly as they were, and
    /// addresses, links and handles never get the key.
    func testEmojiKeyOnlyOnTheLettersPageOfATextField() {
        var options = KeyboardPageOptions()
        options.showsEmojiKey = true
        XCTAssertEqual(bottomRow(options, plane: .numbers), bottomRow(KeyboardPageOptions(), plane: .numbers))
        XCTAssertEqual(bottomRow(options, plane: .symbols), bottomRow(KeyboardPageOptions(), plane: .symbols))
        XCTAssertFalse(bottomRow(options, plane: .numbers).contains(.plane(.emoji)))
        for field: KeyboardFieldKind in [.email, .url, .social] {
            options.field = field
            XCTAssertFalse(bottomRow(options).contains(.plane(.emoji)), "\(field)")
        }
        XCTAssertFalse(bottomRow(KeyboardPageOptions()).contains(.plane(.emoji)), "off unless the keyboard asks")
    }

    func testFieldsThatOfferEmoji() {
        for type: UIKeyboardType in [.default, .numbersAndPunctuation, .webSearch] {
            XCTAssertTrue(KeyboardFieldKind.offersEmoji(type, isSecure: false), "\(type.rawValue)")
        }
        for type: UIKeyboardType in [.emailAddress, .URL, .twitter, .asciiCapable, .numberPad, .decimalPad,
                                     .asciiCapableNumberPad, .phonePad, .namePhonePad] {
            XCTAssertFalse(KeyboardFieldKind.offersEmoji(type, isSecure: false), "\(type.rawValue)")
        }
        XCTAssertFalse(KeyboardFieldKind.offersEmoji(.default, isSecure: true), "never in a password field")
    }

    /// With the keyboard's own globe the space bar must keep 2.5 units, or
    /// the key goes; in portrait it keeps about 2.7 even at 320pt, in
    /// landscape it would not, so there the key is dropped. Without the
    /// globe there is always room.
    func testGlobeKeepsTheEmojiKeyOnlyWhileTheSpaceBarHasRoom() {
        var globe = KeyboardPageOptions(showsNextKeyboardKey: true, showsLanguageKey: true)
        for width in portraitWidths {
            let sizing = KeyboardSizing(width: width, isLandscape: false)
            XCTAssertTrue(KeyboardGeometry.fitsEmojiKey(options: globe, sizing: sizing), "w=\(width)")
            var withEmoji = globe
            withEmoji.showsEmojiKey = true
            XCTAssertGreaterThanOrEqual(KeyboardGeometry.spaceBarUnits(options: withEmoji, sizing: sizing), 2.5, "w=\(width)")
        }
        for width: CGFloat in [568, 667, 736] {
            let sizing = KeyboardSizing(width: width, isLandscape: true)
            XCTAssertFalse(KeyboardGeometry.fitsEmojiKey(options: globe, sizing: sizing), "landscape w=\(width)")
            XCTAssertTrue(KeyboardGeometry.fitsEmojiKey(options: KeyboardPageOptions(), sizing: sizing), "no globe w=\(width)")
        }
        // One layout only: no language key, room again.
        globe.showsLanguageKey = false
        XCTAssertTrue(KeyboardGeometry.fitsEmojiKey(options: globe, sizing: KeyboardSizing(width: 667, isLandscape: true)))

        var email = KeyboardPageOptions()
        email.field = .email
        XCTAssertFalse(KeyboardGeometry.fitsEmojiKey(options: email, sizing: KeyboardSizing(width: 393, isLandscape: false)))
    }

    /// The new key changes no other key's size: the bottom row's controls
    /// keep their width, and the hit frames still tile the whole page.
    func testLettersPageWithTheEmojiKeyStillTiles() {
        var options = KeyboardPageOptions(showsNextKeyboardKey: true, showsLanguageKey: true)
        options.showsEmojiKey = true
        for sizing in sizings() {
            let area = sizing.keyAreaHeight(maximumRows: 5)
            for language in KeyboardLanguage.allCases {
                let page = KeyboardLayout.page(language: language, plane: .letters, options: options)
                let layout = KeyboardGeometry.layout(page: page, sizing: sizing, areaHeight: area)
                let plain = KeyboardGeometry.layout(
                    page: KeyboardLayout.page(language: language, plane: .letters,
                                              options: KeyboardPageOptions(showsNextKeyboardKey: true, showsLanguageKey: true)),
                    sizing: sizing, areaHeight: area)
                let emoji = try? XCTUnwrap(layout.keys.first { $0.action == .plane(.emoji) })
                let numbers = plain.keys.first { $0.action == .plane(.numbers) }
                XCTAssertEqual(emoji?.frame.width ?? 0, numbers?.frame.width ?? -1, accuracy: 0.5)
                XCTAssertEqual(emoji?.frame.minY, numbers?.frame.minY)
                var y: CGFloat = 0.5
                while y < area {
                    var x: CGFloat = 0.5
                    while x < sizing.width {
                        let owners = layout.keys.filter { $0.hitFrame.contains(CGPoint(x: x, y: y)) }.count
                        XCTAssertEqual(owners, 1, "\(language) w=\(sizing.width) at \(x),\(y)")
                        if owners != 1 { return }
                        x += 4
                    }
                    y += 4
                }
            }
        }
    }

    // MARK: The page

    func testEmojiPageBarIsABCGlobeAndDelete() {
        for language in KeyboardLanguage.allCases {
            let page = KeyboardLayout.page(language: language, plane: .emoji, options: KeyboardPageOptions())
            XCTAssertEqual(page.plane, .emoji)
            XCTAssertEqual(page.rows.count, KeyboardLayout.rowCount(language: language, plane: .letters))
            XCTAssertEqual(KeyboardLayout.rowCount(language: language, plane: .emoji),
                           KeyboardLayout.rowCount(language: language, plane: .letters))
            XCTAssertEqual(page.rows.flatMap(\.slots).compactMap(\.action), [.plane(.letters), .delete])
        }
        let globe = KeyboardLayout.page(language: .kazakh, plane: .emoji, options: KeyboardPageOptions(showsNextKeyboardKey: true))
        XCTAssertEqual(globe.rows.flatMap(\.slots).compactMap(\.action), [.plane(.letters), .nextKeyboard, .delete])
    }

    /// Exactly the letters page's height; ABC exactly where 123 is; delete
    /// on the same row; the grid above, never under the bar.
    func testEmojiPageKeepsTheLettersPageHeightAndRow() {
        for sizing in sizings() {
            for maximumRows in [4, 5] {
                let area = sizing.keyAreaHeight(maximumRows: maximumRows)
                for language in KeyboardLanguage.allCases where KeyboardLayout.rowCount(language: language, plane: .letters) <= maximumRows {
                    for globe in [false, true] {
                        let options = KeyboardPageOptions(showsNextKeyboardKey: globe)
                        let letters = KeyboardGeometry.layout(
                            page: KeyboardLayout.page(language: language, plane: .letters, options: options),
                            sizing: sizing, areaHeight: area)
                        let keys = KeyboardGeometry.layout(
                            page: KeyboardLayout.page(language: language, plane: .emoji, options: options),
                            sizing: sizing, areaHeight: area)
                        let page = EmojiPageLayout(keys: keys, sizing: sizing)
                        let label = "\(language) w=\(sizing.width) rows=\(maximumRows) globe=\(globe)"

                        XCTAssertEqual(page.size.height, area, label)
                        XCTAssertEqual(page.barFrame.maxY, area, accuracy: 0.01, label)
                        let abc = page.bar.keys.first { $0.action == .plane(.letters) }?.frame.offsetBy(dx: 0, dy: page.barFrame.minY)
                        let numbers = letters.keys.first { $0.action == .plane(.numbers) }?.frame
                        XCTAssertEqual(abc, numbers, label)
                        if globe {
                            let barGlobe = page.bar.keys.first { $0.action == .nextKeyboard }?.frame.offsetBy(dx: 0, dy: page.barFrame.minY)
                            XCTAssertEqual(barGlobe, letters.keys.first { $0.action == .nextKeyboard }?.frame, label)
                        }
                        let delete = page.bar.keys.first { $0.action == .delete }
                        XCTAssertEqual(delete?.frame.minY, page.bar.keys.first?.frame.minY, label)
                        XCTAssertEqual(delete?.frame.maxX ?? 0, sizing.width - sizing.sideInset, accuracy: 0.5, label)

                        XCTAssertLessThanOrEqual(page.gridFrame.maxY, page.barFrame.minY, label)
                        if let title = page.titleFrame {
                            XCTAssertLessThanOrEqual(title.maxY, page.gridFrame.minY, label)
                        }
                        XCTAssertTrue((EmojiPageLayout.minimumRows...EmojiPageLayout.maximumRows).contains(page.rows), label)
                        XCTAssertGreaterThanOrEqual(page.cellSize.height, EmojiPageLayout.minimumCellHeight - 0.5, label)
                        XCTAssertGreaterThanOrEqual(page.cellSize.width, 36, label)
                        XCTAssertLessThanOrEqual(page.glyphSize, EmojiPageLayout.maximumGlyphSize, label)
                        XCTAssertGreaterThanOrEqual(page.glyphSize, 24, label)
                    }
                }
            }
        }
    }

    /// The category buttons sit between ABC (and the globe) and delete,
    /// never under either, each wide enough to hit - ten of them, with the
    /// recents, even at 320pt with the globe.
    func testCategoryBarSitsBetweenTheControls() {
        for sizing in sizings() {
            for globe in [false, true] {
                let keys = KeyboardGeometry.layout(
                    page: KeyboardLayout.page(language: .english, plane: .emoji, options: KeyboardPageOptions(showsNextKeyboardKey: globe)),
                    sizing: sizing, areaHeight: sizing.keyAreaHeight(maximumRows: 4))
                let page = EmojiPageLayout(keys: keys, sizing: sizing)
                for key in page.bar.keys {
                    XCTAssertFalse(key.frame.intersects(page.categoryFrame), "\(key.action) w=\(sizing.width)")
                }
                let items = page.categoryItemFrames(count: 10)
                XCTAssertEqual(items.count, 10)
                XCTAssertGreaterThanOrEqual(items[0].width, 19, "w=\(sizing.width) globe=\(globe)")
                XCTAssertEqual(items.last?.maxX ?? 0, page.categoryFrame.width, accuracy: 0.01)
                let delete = page.bar.keys.first { $0.action == .delete }!
                XCTAssertEqual(items[0].minY, delete.frame.minY)
                XCTAssertEqual(items[0].height, delete.frame.height)
            }
        }
    }

    /// The bar's keys own the bar's band and nothing above it.
    func testBarKeysTileOnlyTheBar() {
        let sizing = KeyboardSizing(width: 375, isLandscape: false)
        let keys = KeyboardGeometry.layout(
            page: KeyboardLayout.page(language: .russian, plane: .emoji, options: KeyboardPageOptions(showsNextKeyboardKey: true)),
            sizing: sizing, areaHeight: sizing.keyAreaHeight(maximumRows: 5))
        let page = EmojiPageLayout(keys: keys, sizing: sizing)
        XCTAssertEqual(page.bar.size, page.barFrame.size)
        var x: CGFloat = 0.5
        while x < sizing.width {
            for y in stride(from: CGFloat(0.5), to: page.barFrame.height, by: 3) {
                XCTAssertEqual(page.bar.keys.filter { $0.hitFrame.contains(CGPoint(x: x, y: y)) }.count, 1, "\(x),\(y)")
            }
            x += 3
        }
    }

    // MARK: Sections

    func testSectionsFillColumnsAndFollowEachOther() {
        let sizing = KeyboardSizing(width: 393, isLandscape: false)
        let keys = KeyboardGeometry.layout(page: KeyboardLayout.page(language: .kazakh, plane: .emoji, options: KeyboardPageOptions()),
                                           sizing: sizing, areaHeight: sizing.keyAreaHeight(maximumRows: 5))
        let page = EmojiPageLayout(keys: keys, sizing: sizing)
        let rows = page.rows
        let cell = page.cellSize
        let counts = [rows * 2 + 1, rows, 1]
        XCTAssertEqual(page.columns(forCount: counts[0]), 3)
        let starts = page.sectionStarts(counts: counts)
        XCTAssertEqual(starts[0], 0)
        XCTAssertEqual(starts[1], cell.width * 3 + EmojiPageLayout.sectionGap, accuracy: 0.01)
        XCTAssertEqual(starts[2], starts[1] + cell.width + EmojiPageLayout.sectionGap, accuracy: 0.01)
        XCTAssertEqual(page.contentWidth(counts: counts), starts[2] + cell.width, accuracy: 0.01)

        // Top to bottom, then the next column.
        XCTAssertEqual(page.itemFrame(item: 0, sectionStart: 0).origin, .zero)
        XCTAssertEqual(page.itemFrame(item: 1, sectionStart: 0).origin, CGPoint(x: 0, y: cell.height))
        XCTAssertEqual(page.itemFrame(item: rows, sectionStart: 0).origin, CGPoint(x: cell.width, y: 0))

        XCTAssertEqual(page.section(atOffset: 0, counts: counts), 0)
        XCTAssertEqual(page.section(atOffset: starts[1] - cell.width, counts: counts), 0)
        XCTAssertEqual(page.section(atOffset: starts[1], counts: counts), 1)
        XCTAssertEqual(page.section(atOffset: starts[2] + 5, counts: counts), 2)
    }

    // MARK: Typing an emoji

    /// One backspace takes one whole emoji, however many scalars it has.
    func testOneBackspaceRemovesAWholeEmoji() {
        for emoji in ["👨‍👩‍👧‍👦", "🇰🇿", "1️⃣", "🏴󠁧󠁢󠁳󠁣󠁴󠁿", "👍🏽", "❤️", "🙂‍↔️", "é"] {
            XCTAssertEqual(TextDeletion.lastCharacterLength(before: "ok " + emoji), emoji.utf16.count, emoji)
        }
        XCTAssertEqual(TextDeletion.lastCharacterLength(before: ""), 0)
        XCTAssertEqual(TextDeletion.wordLength(before: "привет 😀😀"), 2, "held delete takes the emoji run")
    }

    /// An emoji is not part of a word: no strip after it, no correction.
    func testAnEmojiEndsTheWord() {
        XCTAssertNil(TypedWord(before: "привт😀"))
        XCTAssertNil(TypedWord(before: "helo 🇰🇿"))
        var caret = AutocorrectCaret()
        caret.typed("😀")
        XCTAssertFalse(caret.allowsCorrection(caretInsideWord: false))
        // A keycap starts with a digit: it reads as one, which correction
        // leaves alone.
        if let keycap = TypedWord(before: "1️⃣") {
            XCTAssertEqual(TokenGuard.reason(for: keycap, language: .russian, learned: LearnedWords(language: .russian, words: [])), .digits)
        }
    }
}
