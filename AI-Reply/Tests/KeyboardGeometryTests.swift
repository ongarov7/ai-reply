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
}
