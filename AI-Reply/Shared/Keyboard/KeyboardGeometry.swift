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

/// The persona row above the keys:  ✨ | Дос | Клиент | Әріптес | Серіктес
///
/// * ✨ (Create) is pinned at the LEADING edge, outside the personas, so it is
///   never scrolled away, faded or covered - not even by the suggestion strip.
/// * The personas take the rest of the width. A set that fits is stretched
///   evenly (up to `maximumStretch` each); when space is short their padding
///   tightens first; only when they still do not fit does the row scroll.
/// * Each persona can carry its icon in front of the name. The icons are the
///   first thing to go: every padding is tried with them, then every padding
///   without them, and only then does the row scroll. They never change a
///   pill's height or move ✨.
///
/// Pure geometry: the keyboard measures the names and the icons, this places
/// everything.
struct PersonaRowLayout: Equatable, Sendable {

    static let height: CGFloat = 44
    static let pillHeight: CGFloat = 44
    /// The pinned ✨ AI action, including its visible label.
    static let actionWidth: CGFloat = 64
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
    /// Whether the pills have room for their icons. Never true for a row
    /// that scrolls.
    let showsIcons: Bool

    var scrolls: Bool { contentWidth > personasFrame.width + 0.5 }

    /// - Parameters:
    ///   - textWidths: each persona's measured title width, in order.
    ///   - iconWidths: each persona's icon plus the gap before its title, in
    ///     the same order; empty for a row without icons.
    init(width: CGFloat, height: CGFloat = PersonaRowLayout.height, textWidths: [CGFloat], iconWidths: [CGFloat] = []) {
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
            showsIcons = false
            return
        }
        let texts = textWidths.map { max(0, ceil($0)) }
        let icons = iconWidths.count == texts.count ? iconWidths.map { max(0, ceil($0)) } : texts.map { _ in 0 }
        let gaps = Self.spacing * CGFloat(texts.count - 1)
        let iconTotal = icons.reduce(0, +)
        func fits(padding: CGFloat, withIcons: Bool) -> Bool {
            zip(texts, icons).reduce(CGFloat(0)) { total, item in
                total + max(44, item.0 + (withIcons ? item.1 : 0) + padding * 2)
            } + gaps <= available
        }
        let withIcons = iconTotal > 0 ? Self.paddings.first { fits(padding: $0, withIcons: true) } : nil
        let withoutIcons = Self.paddings.first { fits(padding: $0, withIcons: false) }
        let padding = withIcons ?? withoutIcons ?? Self.paddings.last ?? 8
        showsIcons = withIcons != nil
        var widths = zip(texts, icons).map { text, icon in max(44, text + (withIcons != nil ? icon : 0) + padding * 2) }
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

/// The reply controls keep their position beneath the reply. On narrow
/// keyboards the pager and Insert use a second row instead of overlapping.
/// Pager space is reserved even for the first variant, so regeneration does
/// not change the panel's height when another variant arrives.
struct ReplyResultActionsLayout: Equatable, Sendable {
    static let target: CGFloat = 44
    static let gap: CGFloat = 6
    let back: CGRect
    let regenerate: CGRect
    let edit: CGRect
    let previous: CGRect
    let version: CGRect
    let next: CGRect
    let insert: CGRect
    let height: CGFloat

    init(width: CGFloat, regenerateWidth: CGFloat, insertWidth: CGFloat) {
        let width = max(0, width)
        let target = Self.target
        let gap = Self.gap
        let pagerWidth = target * 2 + 34
        let regenerateWidth = min(max(112, regenerateWidth), max(target, width - target * 2 - gap * 2))
        let insertWidth = min(width, max(96, insertWidth))
        let firstRowWidth = target * 2 + regenerateWidth + gap * 2
        let fitsOneRow = firstRowWidth + gap + pagerWidth + gap + insertWidth <= width
        back = CGRect(x: 0, y: 0, width: target, height: target)
        regenerate = CGRect(x: back.maxX + gap, y: 0, width: regenerateWidth, height: target)
        edit = CGRect(x: regenerate.maxX + gap, y: 0, width: target, height: target)
        let secondY = fitsOneRow ? 0 : target + gap
        insert = CGRect(x: width - insertWidth, y: secondY, width: insertWidth, height: target)
        let pagerX = fitsOneRow
            ? edit.maxX + gap + max(0, (insert.minX - gap - edit.maxX - gap - pagerWidth) / 2)
            : CGFloat(0)
        previous = CGRect(x: pagerX, y: secondY, width: target, height: target)
        version = CGRect(x: previous.maxX, y: secondY, width: 34, height: target)
        next = CGRect(x: version.maxX, y: secondY, width: target, height: target)
        height = secondY + target
    }
}

enum KeyboardGeometry {

    /// One key unit of a grid with `columns` columns at this width.
    static func unitWidth(columns: Int, sizing: KeyboardSizing) -> CGFloat {
        let columns = max(columns, 1)
        let innerWidth = sizing.width - sizing.sideInset * 2
        return (innerWidth - CGFloat(columns - 1) * sizing.columnGap(columns: columns)) / CGFloat(columns)
    }

    // MARK: Emoji key

    /// The narrowest the space bar may get for the emoji key to stay, in
    /// units of the bottom row's grid.
    static let minimumSpaceUnitsWithEmojiKey: CGFloat = 2.5

    /// Whether the letters page has room for the emoji key with these
    /// options (contract §3.10). Only in plain text fields; and where the
    /// keyboard draws its own globe - phones with a Home button - only while
    /// the space bar keeps at least 2.5 units. There the system globe still
    /// reaches Apple's emoji keyboard, so nothing is lost when it goes.
    static func fitsEmojiKey(options: KeyboardPageOptions, sizing: KeyboardSizing) -> Bool {
        guard options.field == .text else { return false }
        guard options.showsNextKeyboardKey else { return true }
        var withEmoji = options
        withEmoji.showsEmojiKey = true
        return spaceBarUnits(options: withEmoji, sizing: sizing) >= minimumSpaceUnitsWithEmojiKey
    }

    /// The letters page's space bar, in units of its row's grid. The bottom
    /// row is the same on every layout, so English stands for all of them.
    static func spaceBarUnits(options: KeyboardPageOptions, sizing: KeyboardSizing) -> CGFloat {
        let page = KeyboardLayout.page(language: .english, plane: .letters, options: options)
        let laidOut = layout(page: page, sizing: sizing, areaHeight: sizing.keyAreaHeight(maximumRows: 4))
        guard let space = laidOut.keys.first(where: { $0.action == .space }) else { return 0 }
        let columns = page.rows.last?.unitColumns ?? page.columns
        return space.frame.width / unitWidth(columns: columns, sizing: sizing)
    }

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
        var previousHitBottom: CGFloat = 0

        for (rowIndex, row) in page.rows.enumerated() {
            // A row on its own grid (the bottom row) also takes that grid's
            // gap, so it is identical on every layout.
            let columns = max(row.unitColumns ?? page.columns, 1)
            let gap = sizing.columnGap(columns: columns)
            let unit = unitWidth(columns: columns, sizing: sizing)

            // Gaps sit only between two KEYS. A spacer is itself the space
            // between its neighbours, so it never gets a gap of its own.
            var fixedWidth: CGFloat = 0
            var flexibleWeight: CGFloat = 0
            var gapCount = 0
            for (index, slot) in row.slots.enumerated() {
                switch slot.width {
                case .units(let units): fixedWidth += units * unit
                case .atLeast(let minimum, let units): fixedWidth += max(minimum, units * unit)
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
                case .atLeast(let minimum, let units): slotWidth = max(minimum, units * unit)
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
            let bandTop = previousHitBottom
            let bandBottom: CGFloat = rowIndex == page.rows.count - 1
                ? areaHeight
                : sizing.pixel(rowTop + metrics.key + metrics.gap / 2)
            let band = CGRect(x: 0, y: bandTop, width: width, height: bandBottom - bandTop)
            previousHitBottom = band.maxY

            var previousHitRight: CGFloat = 0
            for (index, key) in rowKeys.enumerated() {
                // Horizontal: the midpoint to each neighbour, and the screen
                // edge for the first and last key of the row. Carry the
                // CGRect's actual edge to its neighbour: on a 3x screen,
                // origin + (right - origin) can round one ULP past right.
                // Recomputing the next origin from right would overlap it.
                let left = previousHitRight
                let right: CGFloat = index == rowKeys.count - 1
                    ? width
                    : sizing.pixel((key.maxX + rowKeys[index + 1].minX) / 2)
                let hitFrame = CGRect(x: left, y: band.minY, width: right - left, height: band.height)
                previousHitRight = hitFrame.maxX
                keys.append(LaidOutKey(
                    action: key.action,
                    frame: CGRect(x: key.minX, y: top, width: key.maxX - key.minX, height: bottom - top),
                    hitFrame: hitFrame,
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
