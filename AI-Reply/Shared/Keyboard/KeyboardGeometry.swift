import CoreGraphics
import Foundation

/// Key height and the vertical gap between rows.
struct RowMetrics: Equatable, Sendable {
    let key: CGFloat
    let gap: CGFloat

    func height(rows: Int) -> CGFloat {
        guard rows > 0 else { return 0 }
        return CGFloat(rows) * key + CGFloat(rows - 1) * gap
    }
}

/// Everything about key size that depends on the device, derived from the live
/// width of the keyboard rather than from a list of iPhone models.
///
/// The numbers start from the iOS keyboard and lean slightly larger: keys are
/// a couple of points taller than the system's, because the complaint this
/// keyboard is answering is that its keys were too small to hit.
struct KeyboardSizing: Equatable, Sendable {
    let width: CGFloat
    let isLandscape: Bool
    /// Screen scale, so key edges land on whole pixels.
    let scale: CGFloat

    init(width: CGFloat, isLandscape: Bool, scale: CGFloat = 3) {
        self.width = max(width, 1)
        self.isLandscape = isLandscape
        self.scale = max(scale, 1)
    }

    /// A keyboard wider than the screen is tall is a landscape keyboard. When
    /// the screen is not known yet, only a width no iPhone has in portrait
    /// counts as landscape.
    static func isLandscape(width: CGFloat, screenHeight: CGFloat?) -> Bool {
        if let screenHeight, screenHeight > 0 { return width > screenHeight }
        return width >= 560
    }

    var sideInset: CGFloat { isLandscape ? 4 : 3 }
    var topInset: CGFloat { isLandscape ? 4 : 7 }
    var bottomInset: CGFloat { isLandscape ? 3 : 4 }

    /// Horizontal gap between keys. The eleven-column Cyrillic grid gives up a
    /// point per gap, which buys each key almost a point of width.
    func columnGap(columns: Int) -> CGFloat {
        let dense = columns >= 11
        if isLandscape { return dense ? 5 : 6 }
        if width < 360 { return dense ? 4 : 5 }
        return dense ? 5 : 6
    }

    /// Key height and row gap a page with `rows` rows has on its own.
    func natural(rows: Int) -> RowMetrics {
        let fiveRows = rows >= 5
        if isLandscape {
            return fiveRows ? RowMetrics(key: 30, gap: 5) : RowMetrics(key: 34, gap: 6)
        }
        switch width {
        case ..<360: return fiveRows ? RowMetrics(key: 38, gap: 7) : RowMetrics(key: 41, gap: 9)
        case ..<390: return fiveRows ? RowMetrics(key: 41, gap: 7.5) : RowMetrics(key: 44, gap: 10)
        case ..<420: return fiveRows ? RowMetrics(key: 43, gap: 8) : RowMetrics(key: 46, gap: 11)
        default:     return fiveRows ? RowMetrics(key: 45, gap: 8.5) : RowMetrics(key: 48, gap: 11)
        }
    }

    /// Height of the key area for layouts whose largest page has
    /// `maximumRows` rows. Every page is laid out into exactly this height.
    func keyAreaHeight(maximumRows: Int) -> CGFloat {
        let rows = max(maximumRows, 1)
        return pixel(topInset + natural(rows: rows).height(rows: rows) + bottomInset)
    }

    func pixel(_ value: CGFloat) -> CGFloat {
        (value * scale).rounded() / scale
    }
}

/// One key, positioned.
struct LaidOutKey: Equatable, Sendable {
    let action: KeyAction
    /// Where the key is drawn.
    let frame: CGRect
    /// Where a touch counts as this key. The hit frames of a page tile it
    /// completely: every gap, every spacer and every edge belongs to the
    /// nearest key, so there is no point on the keyboard a finger can land on
    /// and get nothing.
    let hitFrame: CGRect
    let row: Int
    let alternates: [String]
}

struct KeyboardPageLayout: Equatable, Sendable {
    let spec: KeyboardPageSpec
    let keys: [LaidOutKey]
    let size: CGSize
    let rowMetrics: RowMetrics

    /// The key under a point. Anything inside the page resolves to a key;
    /// a point outside it resolves to the nearest one.
    func keyIndex(at point: CGPoint) -> Int? {
        guard !keys.isEmpty else { return nil }
        if let hit = keys.firstIndex(where: { $0.hitFrame.contains(point) }) { return hit }
        // Outside the page (a touch that slid off an edge): nearest frame.
        var best: (index: Int, distance: CGFloat)?
        for (index, key) in keys.enumerated() {
            let dx = max(key.frame.minX - point.x, 0, point.x - key.frame.maxX)
            let dy = max(key.frame.minY - point.y, 0, point.y - key.frame.maxY)
            let distance = dx * dx + dy * dy
            if best == nil || distance < best!.distance { best = (index, distance) }
        }
        return best?.index
    }

    func key(at point: CGPoint) -> LaidOutKey? {
        keyIndex(at: point).map { keys[$0] }
    }
}

/// The persona row above the keys:  ✨ | Дос | Клиент | Бизнес | Жұмыс
///
/// * ✨ (Create) is pinned at the LEADING edge, outside the personas, so it is
///   never scrolled away, faded or covered - not even by the suggestion strip.
/// * The personas take the rest of the width. A set that fits is stretched
///   evenly (up to `maximumStretch` each); when space is short their padding
///   tightens first; only when they still do not fit does the row scroll.
///
/// Pure geometry: the keyboard measures the names, this places everything.
struct PersonaRowLayout: Equatable, Sendable {

    static let height: CGFloat = 36
    static let pillHeight: CGFloat = 30
    /// The ✨ disc.
    static let actionWidth: CGFloat = 38
    static let spacing: CGFloat = 6
    static let edgeInset: CGFloat = 8
    /// Text padding per side: roomy when everything fits, tighter on a narrow
    /// row, and never below the last value.
    static let paddings: [CGFloat] = [14, 11, 8]
    /// Widest extra a pill gets when the row stretches: two personas on a Max
    /// phone should not become two slabs.
    static let maximumStretch: CGFloat = 28

    /// The ✨ (Create) button, in the row's coordinates.
    let createFrame: CGRect
    /// The personas' viewport (the scroll view) in the row's coordinates.
    /// The suggestion strip covers exactly this while a word is typed.
    let personasFrame: CGRect
    /// Each pill, in the viewport's content coordinates, leading to trailing.
    let pillFrames: [CGRect]
    /// Width of the scrollable content: wider than the viewport only when the
    /// pills do not fit.
    let contentWidth: CGFloat

    var scrolls: Bool { contentWidth > personasFrame.width + 0.5 }

    /// - Parameter textWidths: each persona's measured title width, in order.
    init(width: CGFloat, height: CGFloat = PersonaRowLayout.height, textWidths: [CGFloat]) {
        let width = max(width, 0)
        let y = ((height - Self.pillHeight) / 2).rounded()
        createFrame = CGRect(x: Self.edgeInset, y: y, width: Self.actionWidth, height: Self.pillHeight)
        let viewportX = createFrame.maxX + Self.spacing / 2
        personasFrame = CGRect(x: viewportX, y: 0, width: max(0, width - viewportX), height: height)

        // Half a gap inside the viewport puts the first pill one full gap
        // after ✨; the last one keeps the row's edge inset.
        let leading = Self.spacing / 2
        let available = personasFrame.width - leading - Self.edgeInset
        guard !textWidths.isEmpty, available > 0 else {
            pillFrames = []
            contentWidth = personasFrame.width
            return
        }
        let texts = textWidths.map { max(0, ceil($0)) }
        let gaps = Self.spacing * CGFloat(texts.count - 1)
        let total = texts.reduce(0, +)
        let padding = Self.paddings.first { total + $0 * 2 * CGFloat(texts.count) + gaps <= available }
            ?? Self.paddings.last ?? 8
        var widths = texts.map { $0 + padding * 2 }
        let used = widths.reduce(0, +) + gaps
        if used <= available {
            let extra = min(Self.maximumStretch, ((available - used) / CGFloat(widths.count)).rounded(.down))
            widths = widths.map { $0 + extra }
        }
        var frames: [CGRect] = []
        var x = leading
        for pillWidth in widths {
            frames.append(CGRect(x: x, y: y, width: pillWidth, height: Self.pillHeight))
            x += pillWidth + Self.spacing
        }
        pillFrames = frames
        contentWidth = max(personasFrame.width, x - Self.spacing + Self.edgeInset)
    }
}

enum KeyboardGeometry {

    /// Row metrics for `rows` rows in `available` points.
    ///
    /// A page with fewer rows than the tallest page gets the difference: the
    /// gaps grow a little (never past 1.4 times their natural size) and the
    /// keys take the rest. That is what the iOS Kazakh keyboard does on its
    /// numbers plane, and it is what keeps the keyboard's height constant.
    static func rowMetrics(rows: Int, available: CGFloat, sizing: KeyboardSizing) -> RowMetrics {
        let rows = max(rows, 1)
        let natural = sizing.natural(rows: rows)
        let naturalHeight = natural.height(rows: rows)
        guard rows > 1 else { return RowMetrics(key: max(available, 1), gap: 0) }

        if available <= naturalHeight + 0.5 {
            let scale = max(available, 1) / naturalHeight
            return RowMetrics(key: natural.key * scale, gap: natural.gap * scale)
        }
        let scale = available / naturalHeight
        let gap = natural.gap * min(scale, 1.4)
        let key = (available - CGFloat(rows - 1) * gap) / CGFloat(rows)
        return RowMetrics(key: key, gap: gap)
    }

    static func layout(page: KeyboardPageSpec, sizing: KeyboardSizing, areaHeight: CGFloat) -> KeyboardPageLayout {
        let width = sizing.width
        let innerWidth = width - sizing.sideInset * 2
        let available = areaHeight - sizing.topInset - sizing.bottomInset
        let metrics = rowMetrics(rows: page.rows.count, available: available, sizing: sizing)

        var keys: [LaidOutKey] = []
        var rowTop = sizing.topInset

        for (rowIndex, row) in page.rows.enumerated() {
            // A row on its own grid (the bottom row) also takes that grid's
            // gap, so it is identical on every layout.
            let columns = max(row.unitColumns ?? page.columns, 1)
            let gap = sizing.columnGap(columns: columns)
            let unit = (innerWidth - CGFloat(columns - 1) * gap) / CGFloat(columns)

            // Gaps sit only between two KEYS. A spacer is itself the space
            // between its neighbours, so it never gets a gap of its own.
            var fixedWidth: CGFloat = 0
            var flexibleWeight: CGFloat = 0
            var gapCount = 0
            for (index, slot) in row.slots.enumerated() {
                switch slot.width {
                case .units(let units): fixedWidth += units * unit
                case .flexible(let weight): flexibleWeight += max(weight, 0)
                }
                if index > 0, slot.action != nil, row.slots[index - 1].action != nil { gapCount += 1 }
            }
            let flexibleWidth = max(0, innerWidth - fixedWidth - CGFloat(gapCount) * gap)

            let top = sizing.pixel(rowTop)
            let bottom = sizing.pixel(rowTop + metrics.key)
            var x = sizing.sideInset
            var rowKeys: [(action: KeyAction, minX: CGFloat, maxX: CGFloat, alternates: [String])] = []

            for (index, slot) in row.slots.enumerated() {
                if index > 0, slot.action != nil, row.slots[index - 1].action != nil { x += gap }
                let slotWidth: CGFloat
                switch slot.width {
                case .units(let units): slotWidth = units * unit
                case .flexible(let weight):
                    slotWidth = flexibleWeight > 0 ? flexibleWidth * max(weight, 0) / flexibleWeight : 0
                }
                if let action = slot.action {
                    rowKeys.append((action, sizing.pixel(x), sizing.pixel(x + slotWidth), slot.alternates))
                }
                x += slotWidth
            }

            // Vertical band: half the row gap either side, and the outer rows
            // reach the very top and bottom of the page.
            let bandTop: CGFloat = rowIndex == 0 ? 0 : sizing.pixel(rowTop - metrics.gap / 2)
            let bandBottom: CGFloat = rowIndex == page.rows.count - 1
                ? areaHeight
                : sizing.pixel(rowTop + metrics.key + metrics.gap / 2)

            for (index, key) in rowKeys.enumerated() {
                // Horizontal: the midpoint to each neighbour, and the screen
                // edge for the first and last key of the row.
                let left: CGFloat = index == 0 ? 0 : sizing.pixel((rowKeys[index - 1].maxX + key.minX) / 2)
                let right: CGFloat = index == rowKeys.count - 1
                    ? width
                    : sizing.pixel((key.maxX + rowKeys[index + 1].minX) / 2)
                keys.append(LaidOutKey(
                    action: key.action,
                    frame: CGRect(x: key.minX, y: top, width: key.maxX - key.minX, height: bottom - top),
                    hitFrame: CGRect(x: left, y: bandTop, width: right - left, height: bandBottom - bandTop),
                    row: rowIndex,
                    alternates: key.alternates
                ))
            }

            rowTop += metrics.key + metrics.gap
        }

        return KeyboardPageLayout(
            spec: page,
            keys: keys,
            size: CGSize(width: width, height: areaHeight),
            rowMetrics: metrics
        )
    }
}
