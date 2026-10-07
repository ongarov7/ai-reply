import XCTest
@testable import AIReply

/// Key geometry across real iPhone widths: no dead zones, no height jumps,
/// no key too small to hit.
final class KeyboardGeometryTests: XCTestCase {

    /// iPhone SE (1st), SE / mini, 12-16, 16 Pro, Pro Max, and a landscape.
    private let portraitWidths: [CGFloat] = [320, 375, 390, 393, 402, 414, 428, 430, 440]

    private func sizings() -> [KeyboardSizing] {
        portraitWidths.map { KeyboardSizing(width: $0, isLandscape: false) }
            + [KeyboardSizing(width: 844, isLandscape: true)]
    }

    private func allPages(_ options: KeyboardPageOptions = KeyboardPageOptions()) -> [KeyboardPageSpec] {
        KeyboardLanguage.allCases.flatMap { language in
            [KeyboardPlane.letters, .numbers, .symbols].map {
                KeyboardLayout.page(language: language, plane: $0, options: options)
            }
        }
    }

    /// Every point of the key area belongs to exactly one key. This is the
    /// test that fails if a gap, a margin or a spacer swallows a touch.
    func testHitFramesTileTheWholeKeyArea() {
        let options = KeyboardPageOptions(showsNextKeyboardKey: true, showsLanguageKey: true)
        for sizing in sizings() {
            let area = sizing.keyAreaHeight(maximumRows: 5)
            for page in allPages(options) {
                let layout = KeyboardGeometry.layout(page: page, sizing: sizing, areaHeight: area)
                var y: CGFloat = 0.5
                while y < area {
                    var x: CGFloat = 0.5
                    while x < sizing.width {
                        let point = CGPoint(x: x, y: y)
                        let owners = layout.keys.filter { $0.hitFrame.contains(point) }.count
                        XCTAssertEqual(owners, 1, "\(page.language) \(page.plane) w=\(sizing.width) at \(point)")
                        if owners != 1 { return }
                        x += 3
                    }
                    y += 3
                }
            }
        }
    }

    /// Letters, numbers and symbols of every enabled layout share one height,
    /// so the keyboard never jumps under the conversation.
    func testEveryPageFitsTheSameHeight() {
        for sizing in sizings() {
            let area = sizing.keyAreaHeight(maximumRows: KeyboardLayout.maximumRowCount(languages: KeyboardLanguage.cycleOrder))
            for page in allPages() {
                let layout = KeyboardGeometry.layout(page: page, sizing: sizing, areaHeight: area)
                XCTAssertEqual(layout.size.height, area)
                let bottom = layout.keys.map(\.frame.maxY).max() ?? 0
                XCTAssertLessThanOrEqual(bottom, area - sizing.bottomInset + 0.5, "\(page.language) \(page.plane)")
                XCTAssertGreaterThanOrEqual(bottom, area - sizing.bottomInset - 1.5, "\(page.language) \(page.plane) leaves a gap")
            }
        }
    }

    /// The five-row Kazakh page must not buy its extra row with tiny keys.
    func testKeysStayComfortable() {
        for width in portraitWidths where width >= 375 {
            let sizing = KeyboardSizing(width: width, isLandscape: false)
            let area = sizing.keyAreaHeight(maximumRows: 5)
            for page in allPages() {
                let layout = KeyboardGeometry.layout(page: page, sizing: sizing, areaHeight: area)
                for key in layout.keys {
                    XCTAssertGreaterThanOrEqual(key.frame.height, 40, "\(page.language) \(page.plane) w=\(width)")
                    XCTAssertGreaterThanOrEqual(key.frame.width, 27, "\(key.action) w=\(width)")
                }
            }
        }
    }

    /// The Kazakh row is centred, and its keys are the size of the keys below.
    func testKazakhTopRowIsCentredWithFullSizeKeys() {
        let sizing = KeyboardSizing(width: 390, isLandscape: false)
        let page = KeyboardLayout.page(language: .kazakh, plane: .letters, options: KeyboardPageOptions())
        let layout = KeyboardGeometry.layout(page: page, sizing: sizing, areaHeight: sizing.keyAreaHeight(maximumRows: 5))
        let top = layout.keys.filter { $0.row == 0 }
        let second = layout.keys.filter { $0.row == 1 }
        XCTAssertEqual(top.count, 9)
        XCTAssertEqual(second.count, 11)
        XCTAssertEqual(top[0].frame.width, second[0].frame.width, accuracy: 0.5)
        let leftMargin = top.first!.frame.minX
        let rightMargin = sizing.width - top.last!.frame.maxX
        XCTAssertEqual(leftMargin, rightMargin, accuracy: 1)
        XCTAssertGreaterThan(leftMargin, second[0].frame.minX + second[0].frame.width * 0.9)
        // ...and the margin still belongs to the edge keys.
        XCTAssertEqual(layout.key(at: CGPoint(x: 2, y: top[0].frame.midY))?.action, .character("ә"))
        XCTAssertEqual(layout.key(at: CGPoint(x: 388, y: top[0].frame.midY))?.action, .character("һ"))
    }

    /// 123, the layout key and return are the same size on every layout, so
    /// switching language does not move them under the thumb.
    func testBottomRowIsIdenticalAcrossLayouts() {
        let sizing = KeyboardSizing(width: 393, isLandscape: false)
        let area = sizing.keyAreaHeight(maximumRows: 5)
        let frames = KeyboardLanguage.allCases.map { language -> [CGRect] in
            let page = KeyboardLayout.page(language: language, plane: .letters, options: KeyboardPageOptions())
            let layout = KeyboardGeometry.layout(page: page, sizing: sizing, areaHeight: area)
            let lastRow = layout.keys.map(\.row).max()!
            return layout.keys.filter { $0.row == lastRow }.map(\.frame)
        }
        for other in frames.dropFirst() {
            XCTAssertEqual(other.count, frames[0].count)
            for (a, b) in zip(frames[0], other) {
                XCTAssertEqual(a.minX, b.minX, accuracy: 0.5)
                XCTAssertEqual(a.width, b.width, accuracy: 0.5)
            }
        }
    }

    func testTouchesOffThePageResolveToTheNearestKey() {
        let sizing = KeyboardSizing(width: 390, isLandscape: false)
        let page = KeyboardLayout.page(language: .english, plane: .letters, options: KeyboardPageOptions())
        let layout = KeyboardGeometry.layout(page: page, sizing: sizing, areaHeight: sizing.keyAreaHeight(maximumRows: 4))
        XCTAssertEqual(layout.key(at: CGPoint(x: -10, y: -10))?.action, .character("q"))
        XCTAssertEqual(layout.key(at: CGPoint(x: 500, y: -10))?.action, .character("p"))
    }

    // MARK: Persona row

    /// Roughly "Друг", "Клиент", "Бизнес", "Работа" at the row's 14pt font.
    private let fourPersonas: [CGFloat] = [33, 47, 50, 50]

    /// ✨ comes first, at the leading edge; the personas follow it, in order,
    /// and fill the rest of the row. There is no other action on the row.
    /// (Below 375pt four Russian names scroll; see the crowded-row test.)
    func testPersonaRowStartsWithCreateAndPersonasFollowIt() {
        for width in portraitWidths + [844] where width >= 375 {
            let row = PersonaRowLayout(width: width, textWidths: fourPersonas)
            let create = row.createFrame
            XCTAssertEqual(create, CGRect(x: 8, y: 3, width: 38, height: 30), "w=\(width)")

            XCTAssertEqual(row.pillFrames.count, 4)
            let pills = row.pillFrames.map { $0.offsetBy(dx: row.personasFrame.minX, dy: 0) }
            XCTAssertEqual(pills[0].minX - create.maxX, PersonaRowLayout.spacing, accuracy: 0.01, "one gap after ✨")
            for (left, right) in zip(pills, pills.dropFirst()) {
                XCTAssertEqual(right.minX - left.maxX, PersonaRowLayout.spacing, accuracy: 0.01)
            }
            for pill in pills {
                XCTAssertEqual(pill.minY, create.minY)
                XCTAssertEqual(pill.height, create.height)
                // Nothing past the trailing inset: no slot is left for a "+".
                XCTAssertLessThanOrEqual(pill.maxX, width - PersonaRowLayout.edgeInset + 0.01, "w=\(width)")
            }
            XCTAssertFalse(row.scrolls, "four personas fit at w=\(width)")
            // Stretched to the trailing edge, unless each already got the
            // largest stretch allowed.
            let slack = width - PersonaRowLayout.edgeInset - pills[3].maxX
            let stretched = pills[0].width - (fourPersonas[0] + PersonaRowLayout.paddings[0] * 2)
            if stretched < PersonaRowLayout.maximumStretch {
                XCTAssertLessThan(slack, CGFloat(pills.count), "w=\(width)")
            }
        }
    }

    /// The suggestion strip covers exactly the personas' viewport: everything
    /// right of ✨ and nothing of ✨ itself, so ✨ stays visible and tappable
    /// while a word is typed.
    func testSuggestionAreaIsTheRowRightOfCreate() {
        for width in portraitWidths {
            let row = PersonaRowLayout(width: width, textWidths: fourPersonas)
            XCTAssertFalse(row.personasFrame.intersects(row.createFrame), "w=\(width)")
            XCTAssertEqual(row.personasFrame.minX, row.createFrame.maxX + PersonaRowLayout.spacing / 2)
            XCTAssertEqual(row.personasFrame.maxX, width)
            XCTAssertEqual(row.personasFrame.minY, 0)
            XCTAssertEqual(row.personasFrame.height, PersonaRowLayout.height)
        }
    }

    /// Neither ✨ nor the strip's area depends on the personas, so a new
    /// template, a rename or a language switch never moves them.
    func testCreateAndSuggestionAreaNeverMove() {
        let reference = PersonaRowLayout(width: 393, textWidths: fourPersonas)
        for texts in [[], [40], fourPersonas, Array(repeating: CGFloat(90), count: 8)] {
            let row = PersonaRowLayout(width: 393, textWidths: texts)
            XCTAssertEqual(row.createFrame, reference.createFrame)
            XCTAssertEqual(row.personasFrame, reference.personasFrame)
        }
    }

    /// Too many personas for the width: they keep the tightest padding and
    /// scroll, never cut a name, and never slide under ✨.
    func testCrowdedPersonaRowScrollsRightOfCreate() {
        let texts = Array(repeating: CGFloat(80), count: 7)
        let row = PersonaRowLayout(width: 375, textWidths: texts)
        XCTAssertTrue(row.scrolls)
        let tightest = PersonaRowLayout.paddings.last!
        for pill in row.pillFrames {
            XCTAssertEqual(pill.width, 80 + tightest * 2)
            XCTAssertGreaterThanOrEqual(pill.minX, 0, "content starts inside the viewport, after ✨")
        }
        XCTAssertEqual(row.contentWidth, row.pillFrames.last!.maxX + PersonaRowLayout.edgeInset)
    }

    func testEmptyPersonaRowStillOffersCreate() {
        let row = PersonaRowLayout(width: 390, textWidths: [])
        XCTAssertTrue(row.pillFrames.isEmpty)
        XCTAssertFalse(row.scrolls)
        XCTAssertEqual(row.createFrame.minX, PersonaRowLayout.edgeInset)
    }
}
