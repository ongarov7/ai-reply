import CoreGraphics
import Foundation

/// Where everything on the emoji page goes.
///
/// Эмодзи бетінің геометриясы: биіктігі әріптер бетімен бірдей, ABC мен өшіру
/// пернесі 123 тұрған жерде.
///
/// Worked out from the same key layout as every other page
/// (`KeyboardGeometry.layout` of `KeyboardLayout.page(... .emoji ...)`), in the
/// same key-area height, so the page is exactly as tall as the letters page
/// and ABC and delete sit on the letters page's bottom row. Above them, the
/// grid: a few rows of emoji that scroll sideways, section after section, as
/// on the iOS emoji keyboard; between ABC and delete, the category buttons.
///
/// Pure geometry: the keyboard's views only place what this says.
struct EmojiPageLayout: Equatable, Sendable {

    /// A row of emoji is never shorter than this; the grid has fewer rows
    /// instead, never fewer than `minimumRows`.
    static let minimumCellHeight: CGFloat = 34
    static let minimumRows = 3
    static let maximumRows = 5
    /// The emoji font never grows past this: bigger glyphs cost memory in
    /// the extension and look bloated.
    static let maximumGlyphSize: CGFloat = 32
    /// Between one section's last column and the next one's first.
    static let sectionGap: CGFloat = 12
    static let titleHeight: CGFloat = 16

    let size: CGSize

    /// The bar's keys - ABC, the globe when there is one, delete - in the
    /// bar's own coordinates. The bar view is only as tall as the bottom
    /// row's band, so no touch above it can reach a key.
    let bar: KeyboardPageLayout
    /// The bar view's frame in the page.
    let barFrame: CGRect
    /// The category buttons, in the bar's coordinates: between the last
    /// control on the left and delete, the band's full height.
    let categoryFrame: CGRect
    /// The height band of a key in the bar, in the bar's coordinates: the
    /// category icons sit on it.
    let categoryKeyMinY: CGFloat
    let categoryKeyHeight: CGFloat

    /// The section title, top left. nil when the page is too short for it.
    let titleFrame: CGRect?
    /// The scrolling grid's frame in the page: the full width (its content
    /// is inset by `contentInset`), exactly `rows` cells tall.
    let gridFrame: CGRect
    let contentInset: CGFloat
    let rows: Int
    let cellSize: CGSize
    let glyphSize: CGFloat

    init(keys: KeyboardPageLayout, sizing: KeyboardSizing) {
        size = keys.size
        let lastRow = keys.keys.map(\.row).max() ?? 0
        let barKeys = keys.keys.filter { $0.row == lastRow }
        let bandTop = barKeys.map(\.hitFrame.minY).min() ?? size.height
        barFrame = CGRect(x: 0, y: bandTop, width: size.width, height: max(0, size.height - bandTop))
        bar = KeyboardPageLayout(
            spec: keys.spec,
            keys: barKeys.map { key in
                LaidOutKey(action: key.action,
                           frame: key.frame.offsetBy(dx: 0, dy: -bandTop),
                           hitFrame: key.hitFrame.offsetBy(dx: 0, dy: -bandTop),
                           row: key.row,
                           alternates: key.alternates)
            },
            size: barFrame.size,
            rowMetrics: keys.rowMetrics
        )

        let gap = sizing.columnGap(columns: 10)
        let leftEdge = bar.keys.filter { $0.action != .delete }.map(\.frame.maxX).max() ?? sizing.sideInset
        let delete = bar.keys.first { $0.action == .delete }
        let rightEdge = delete?.frame.minX ?? size.width - sizing.sideInset
        categoryFrame = CGRect(x: leftEdge + gap, y: 0, width: max(0, rightEdge - gap - (leftEdge + gap)),
                               height: barFrame.height)
        categoryKeyMinY = delete?.frame.minY ?? 0
        categoryKeyHeight = delete?.frame.height ?? barFrame.height

        // The title, when the grid keeps at least its minimum rows with it.
        let titleTop = max(2, sizing.topInset - 3)
        let withTitle = titleTop + Self.titleHeight + 2
        let gridBottom = bandTop - 2
        let roomWithTitle = gridBottom - withTitle
        let showsTitle = roomWithTitle >= Self.minimumCellHeight * CGFloat(Self.minimumRows)
        titleFrame = showsTitle
            ? CGRect(x: sizing.sideInset + 6, y: titleTop, width: max(0, size.width - (sizing.sideInset + 6) * 2),
                     height: Self.titleHeight)
            : nil
        let gridTop = showsTitle ? withTitle : 2
        let available = max(1, gridBottom - gridTop)

        let rows = min(Self.maximumRows, max(Self.minimumRows, Int(available / Self.minimumCellHeight)))
        self.rows = rows
        let cellHeight = Self.floor(available / CGFloat(rows), scale: sizing.scale)
        contentInset = sizing.sideInset
        let gridWidth = max(1, size.width - contentInset * 2)
        let target = min(cellHeight * 1.15, 46)
        let columns = max(1, Int((gridWidth / target).rounded()))
        let cellWidth = Self.floor(gridWidth / CGFloat(columns), scale: sizing.scale)
        cellSize = CGSize(width: cellWidth, height: cellHeight)
        let gridHeight = cellHeight * CGFloat(rows)
        gridFrame = CGRect(x: 0, y: sizing.pixel(gridTop + (available - gridHeight) / 2), width: size.width, height: gridHeight)
        glyphSize = min(Self.maximumGlyphSize, (min(cellWidth, cellHeight) * 0.78).rounded(.down))
    }

    // MARK: Categories

    /// The category buttons, side by side, each the full width it gets, in
    /// the strip's own coordinates.
    func categoryItemFrames(count: Int) -> [CGRect] {
        guard count > 0, categoryFrame.width > 0 else { return [] }
        let width = categoryFrame.width / CGFloat(count)
        return (0..<count).map { index in
            CGRect(x: CGFloat(index) * width, y: categoryKeyMinY, width: width, height: categoryKeyHeight)
        }
    }

    // MARK: Sections

    /// Columns a section of `count` emoji takes: filled top to bottom, then
    /// left to right, as on the iOS emoji keyboard.
    func columns(forCount count: Int) -> Int {
        guard count > 0 else { return 0 }
        return (count + rows - 1) / rows
    }

    /// Where each section starts in the grid's content, from its leading edge.
    func sectionStarts(counts: [Int]) -> [CGFloat] {
        var starts: [CGFloat] = []
        var x: CGFloat = 0
        for count in counts {
            starts.append(x)
            x += CGFloat(columns(forCount: count)) * cellSize.width + Self.sectionGap
        }
        return starts
    }

    /// The scrolling content's width, edge insets not included.
    func contentWidth(counts: [Int]) -> CGFloat {
        guard let last = counts.last, let start = sectionStarts(counts: counts).last else { return 0 }
        return start + CGFloat(columns(forCount: last)) * cellSize.width
    }

    /// An emoji's cell in the grid's content.
    func itemFrame(item: Int, sectionStart: CGFloat) -> CGRect {
        let column = item / rows
        let row = item % rows
        return CGRect(x: sectionStart + CGFloat(column) * cellSize.width, y: CGFloat(row) * cellSize.height,
                      width: cellSize.width, height: cellSize.height)
    }

    /// The section that owns the grid's leading edge at this content offset:
    /// the one the title names and the category bar highlights.
    func section(atOffset offset: CGFloat, counts: [Int]) -> Int {
        let starts = sectionStarts(counts: counts)
        guard !starts.isEmpty else { return 0 }
        // A section counts once its first column is mostly in view.
        let edge = offset + cellSize.width / 2
        return starts.lastIndex { $0 <= edge } ?? 0
    }

    private static func floor(_ value: CGFloat, scale: CGFloat) -> CGFloat {
        (value * scale).rounded(.down) / scale
    }
}
