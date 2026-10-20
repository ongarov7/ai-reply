import UIKit

protocol EmojiKeyboardViewDelegate: AnyObject {
    /// An emoji was tapped; the keyboard puts it where a key would type.
    func emojiView(_ view: EmojiKeyboardView, didPick emoji: String)
}

/// The emoji page: the letters page's height, a grid of emoji that scrolls
/// sideways section after section, as on the iOS emoji keyboard, and a bottom
/// bar of ABC, the categories and delete.
///
/// Эмодзи беті: әріптер бетімен бірдей биіктік, көлденең айналатын тор, астында
/// ABC, санаттар және өшіру.
///
/// * The bar's keys are a `KeyboardKeysView`, the same view every page uses:
///   delete repeats and speeds up to words exactly as on the letters page,
///   the globe hands every touch to the system, and ABC is the letters key.
/// * The recently used section comes first. Its contents are whatever the
///   keyboard passed in when the page appeared: an emoji typed now shows up
///   there next time, so nothing moves under the finger.
/// * Cells are reused and the font is capped (`EmojiPageLayout`), so a full
///   scroll through the catalog stays light in the extension's memory.
///
/// Geometry comes from `EmojiPageLayout`; this view only places it.
final class EmojiKeyboardView: UIView {

    weak var delegate: EmojiKeyboardViewDelegate?

    /// ABC, the globe when there is one, delete. Its delegate is the
    /// keyboard's, as for the keys of every other page.
    let barKeys = KeyboardKeysView()

    /// Key-down haptics, as on the keys. Set by the controller: on only with
    /// Full Access.
    var hapticsEnabled = false {
        didSet { barKeys.hapticsEnabled = hapticsEnabled }
    }

    /// While a request runs nothing can be typed: the emoji dim and stay
    /// silent, and so does delete; ABC and the categories keep working.
    var isInputDimmed = false {
        didSet {
            barKeys.isInputDimmed = isInputDimmed
            collectionView.alpha = isInputDimmed ? 0.45 : 1
        }
    }

    private let titleLabel = UILabel()
    private let gridLayout = EmojiGridLayout()
    private lazy var collectionView = UICollectionView(frame: .zero, collectionViewLayout: gridLayout)
    private let categoryBar = EmojiCategoryBar()
    private lazy var haptics = UIImpactFeedbackGenerator(style: .light)

    private var layout: EmojiPageLayout?
    private var sections: [EmojiSection] = []
    private var counts: [Int] = []
    private var labels = KeyboardLabels(.english)
    private var theme = KeyboardTheme(isDark: false)
    private var emojiFont = UIFont.systemFont(ofSize: 28)
    /// The section the title names and the bar highlights.
    private var currentSection = -1
    /// A category tapped in the bar stays selected until the user scrolls
    /// by hand - also when the grid cannot bring it to the leading edge
    /// because it is the last one.
    private var pinnedSection: Int?

    // MARK: Init

    override init(frame: CGRect) {
        super.init(frame: frame)
        clipsToBounds = false
        addSubview(barKeys)

        titleLabel.font = .systemFont(ofSize: 12, weight: .semibold)
        titleLabel.isAccessibilityElement = false
        addSubview(titleLabel)

        collectionView.backgroundColor = .clear
        collectionView.showsHorizontalScrollIndicator = false
        collectionView.showsVerticalScrollIndicator = false
        collectionView.alwaysBounceHorizontal = true
        collectionView.alwaysBounceVertical = false
        // Highlight at once, like a key; a drag still scrolls.
        collectionView.delaysContentTouches = false
        collectionView.isPrefetchingEnabled = false
        collectionView.contentInsetAdjustmentBehavior = .never
        collectionView.register(EmojiCell.self, forCellWithReuseIdentifier: EmojiCell.reuseIdentifier)
        collectionView.dataSource = self
        collectionView.delegate = self
        addSubview(collectionView)

        categoryBar.onSelect = { [weak self] index in self?.scrollToSection(index) }
        addSubview(categoryBar)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not used")
    }

    // MARK: Configuration

    func configure(theme: KeyboardTheme) {
        self.theme = theme
        barKeys.configure(theme: theme)
        categoryBar.theme = theme
        titleLabel.textColor = theme.secondaryText
        for case let cell as EmojiCell in collectionView.visibleCells {
            cell.highlightColor = highlightColor
        }
    }

    /// Installs the page. `resetsPosition` when the page is being opened:
    /// it then starts at its first section, the recently used ones.
    func show(_ layout: EmojiPageLayout, labels: KeyboardLabels, sections: [EmojiSection], resetsPosition: Bool) {
        let contentChanged = sections != self.sections || layout.cellSize != self.layout?.cellSize
            || layout.rows != self.layout?.rows
        self.layout = layout
        self.labels = labels
        self.sections = sections
        counts = sections.map(\.emoji.count)
        emojiFont = .systemFont(ofSize: layout.glyphSize)

        barKeys.show(layout.bar, labels: labels, languages: [], currentLanguage: labels.language)
        gridLayout.configure(layout, counts: counts)
        categoryBar.show(kinds: sections.map(\.kind), labels: labels,
                         frames: layout.categoryItemFrames(count: sections.count))
        if contentChanged { collectionView.reloadData() }
        setNeedsLayout()
        layoutIfNeeded()

        if resetsPosition || contentChanged {
            pinnedSection = nil
            collectionView.setContentOffset(CGPoint(x: -layout.contentInset, y: 0), animated: false)
        }
        updateCurrentSection(force: true)
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        guard let layout else { return }
        barKeys.frame = layout.barFrame
        categoryBar.frame = layout.categoryFrame.offsetBy(dx: 0, dy: layout.barFrame.minY)
        titleLabel.frame = layout.titleFrame ?? .zero
        titleLabel.isHidden = layout.titleFrame == nil
        if collectionView.frame != layout.gridFrame { collectionView.frame = layout.gridFrame }
        let inset = UIEdgeInsets(top: 0, left: layout.contentInset, bottom: 0, right: layout.contentInset)
        if collectionView.contentInset != inset { collectionView.contentInset = inset }
    }

    private var highlightColor: UIColor {
        theme.isDark ? theme.letterKey : theme.letterKey.withAlphaComponent(0.9)
    }

    // MARK: Sections

    private func scrollToSection(_ index: Int) {
        guard let layout, sections.indices.contains(index) else { return }
        let starts = layout.sectionStarts(counts: counts)
        let inset = collectionView.contentInset
        let maximum = max(-inset.left, collectionView.contentSize.width + inset.right - collectionView.bounds.width)
        let x = min(max(starts[index] - inset.left, -inset.left), maximum)
        pinnedSection = index
        collectionView.setContentOffset(CGPoint(x: x, y: 0), animated: false)
        updateCurrentSection(force: true)
    }

    fileprivate func updateCurrentSection(force: Bool = false) {
        guard let layout, !sections.isEmpty else {
            titleLabel.text = nil
            return
        }
        let index: Int
        if let pinnedSection, sections.indices.contains(pinnedSection) {
            index = pinnedSection
        } else {
            let inset = collectionView.contentInset
            let offset = collectionView.contentOffset.x + inset.left
            let end = collectionView.contentSize.width + inset.right - collectionView.bounds.width
            // At the very end the last section is the one being looked at,
            // even when it is too short to reach the leading edge.
            index = collectionView.contentOffset.x >= end - 1 && end > -inset.left
                ? sections.count - 1
                : layout.section(atOffset: offset, counts: counts)
        }
        guard force || index != currentSection else { return }
        currentSection = index
        titleLabel.text = labels.emojiSectionTitle(sections[index].kind).uppercased()
        categoryBar.select(index)
    }

    // MARK: Feedback

    private func playFeedback() {
        UIDevice.current.playInputClick()
        guard hapticsEnabled else { return }
        haptics.impactOccurred(intensity: 0.55)
        haptics.prepare()
    }
}

// MARK: - Grid

extension EmojiKeyboardView: UICollectionViewDataSource, UICollectionViewDelegate {

    func numberOfSections(in collectionView: UICollectionView) -> Int {
        sections.count
    }

    func collectionView(_ collectionView: UICollectionView, numberOfItemsInSection section: Int) -> Int {
        sections.indices.contains(section) ? sections[section].emoji.count : 0
    }

    func collectionView(_ collectionView: UICollectionView, cellForItemAt indexPath: IndexPath) -> UICollectionViewCell {
        let cell = collectionView.dequeueReusableCell(withReuseIdentifier: EmojiCell.reuseIdentifier, for: indexPath)
        if let cell = cell as? EmojiCell, let emoji = emoji(at: indexPath) {
            cell.configure(emoji, font: emojiFont, highlight: highlightColor)
        }
        return cell
    }

    func collectionView(_ collectionView: UICollectionView, shouldSelectItemAt indexPath: IndexPath) -> Bool {
        !isInputDimmed
    }

    func collectionView(_ collectionView: UICollectionView, shouldHighlightItemAt indexPath: IndexPath) -> Bool {
        !isInputDimmed
    }

    func collectionView(_ collectionView: UICollectionView, didSelectItemAt indexPath: IndexPath) {
        collectionView.deselectItem(at: indexPath, animated: false)
        guard let emoji = emoji(at: indexPath) else { return }
        playFeedback()
        delegate?.emojiView(self, didPick: emoji)
    }

    func scrollViewWillBeginDragging(_ scrollView: UIScrollView) {
        pinnedSection = nil
    }

    func scrollViewDidScroll(_ scrollView: UIScrollView) {
        updateCurrentSection()
    }

    private func emoji(at indexPath: IndexPath) -> String? {
        guard sections.indices.contains(indexPath.section),
              sections[indexPath.section].emoji.indices.contains(indexPath.item) else { return nil }
        return sections[indexPath.section].emoji[indexPath.item]
    }
}

// MARK: - Grid layout

/// Columns of `rows` emoji, filled top to bottom, section after section, at
/// the positions `EmojiPageLayout` works out - so the category bar, the title
/// and the grid always agree on where a section starts.
final class EmojiGridLayout: UICollectionViewLayout {

    private var page: EmojiPageLayout?
    private var counts: [Int] = []
    private var starts: [CGFloat] = []
    private var width: CGFloat = 0

    func configure(_ page: EmojiPageLayout, counts: [Int]) {
        self.page = page
        self.counts = counts
        starts = page.sectionStarts(counts: counts)
        width = page.contentWidth(counts: counts)
        invalidateLayout()
    }

    override var collectionViewContentSize: CGSize {
        CGSize(width: width, height: page?.gridFrame.height ?? 0)
    }

    override func layoutAttributesForElements(in rect: CGRect) -> [UICollectionViewLayoutAttributes]? {
        guard let page, page.cellSize.width > 0 else { return [] }
        var result: [UICollectionViewLayoutAttributes] = []
        for (section, count) in counts.enumerated() where count > 0 {
            let columns = page.columns(forCount: count)
            let start = starts[section]
            guard start + CGFloat(columns) * page.cellSize.width > rect.minX, start < rect.maxX else { continue }
            let first = max(0, Int(((rect.minX - start) / page.cellSize.width).rounded(.down)))
            let last = min(columns - 1, Int(((rect.maxX - start) / page.cellSize.width).rounded(.down)))
            guard first <= last else { continue }
            for item in (first * page.rows)..<min(count, (last + 1) * page.rows) {
                result.append(attributes(for: IndexPath(item: item, section: section), page: page))
            }
        }
        return result
    }

    override func layoutAttributesForItem(at indexPath: IndexPath) -> UICollectionViewLayoutAttributes? {
        guard let page, counts.indices.contains(indexPath.section),
              indexPath.item < counts[indexPath.section] else { return nil }
        return attributes(for: indexPath, page: page)
    }

    /// Frames depend on the page, not on where the grid is scrolled.
    override func shouldInvalidateLayout(forBoundsChange newBounds: CGRect) -> Bool {
        false
    }

    private func attributes(for indexPath: IndexPath, page: EmojiPageLayout) -> UICollectionViewLayoutAttributes {
        let attributes = UICollectionViewLayoutAttributes(forCellWith: indexPath)
        attributes.frame = page.itemFrame(item: indexPath.item, sectionStart: starts[indexPath.section])
        return attributes
    }
}

// MARK: - Cell

/// One emoji. A label is all it is: the glyph does the drawing.
final class EmojiCell: UICollectionViewCell {

    static let reuseIdentifier = "EmojiCell"

    var highlightColor: UIColor = .clear {
        didSet { if isHighlighted { contentView.backgroundColor = highlightColor } }
    }

    private let label = UILabel()

    override init(frame: CGRect) {
        super.init(frame: frame)
        label.textAlignment = .center
        label.baselineAdjustment = .alignCenters
        contentView.addSubview(label)
        contentView.layer.cornerRadius = 8
        contentView.layer.cornerCurve = .continuous
        isAccessibilityElement = true
        accessibilityTraits = .keyboardKey
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not used")
    }

    /// VoiceOver reads the emoji's own name from the character itself.
    func configure(_ emoji: String, font: UIFont, highlight: UIColor) {
        if label.text != emoji { label.text = emoji }
        if label.font != font { label.font = font }
        accessibilityLabel = emoji
        highlightColor = highlight
    }

    override var isHighlighted: Bool {
        didSet { contentView.backgroundColor = isHighlighted ? highlightColor : .clear }
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        label.frame = contentView.bounds
    }

    override func prepareForReuse() {
        super.prepareForReuse()
        contentView.backgroundColor = .clear
    }
}

// MARK: - Category bar

/// The category buttons between ABC and delete: icons, the current one on a
/// key-coloured disc. A tap - or a finger sliding along the bar - jumps the
/// grid to that category. Each button is a VoiceOver element named in the
/// layout's language.
final class EmojiCategoryBar: UIView {

    var onSelect: ((Int) -> Void)?

    var theme = KeyboardTheme(isDark: false) {
        didSet { restyle() }
    }

    private var kinds: [EmojiSection.Kind] = []
    private var titles: [String] = []
    private var frames: [CGRect] = []
    private var icons: [UIImageView] = []
    private let disc = UIView()
    private(set) var selectedIndex = 0
    private var touchedIndex: Int?

    override init(frame: CGRect) {
        super.init(frame: frame)
        isMultipleTouchEnabled = false
        disc.isUserInteractionEnabled = false
        disc.layer.cornerCurve = .continuous
        addSubview(disc)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not used")
    }

    /// - Parameter frames: each button's frame in this view, from
    ///   `EmojiPageLayout.categoryItemFrames(count:)`.
    func show(kinds: [EmojiSection.Kind], labels: KeyboardLabels, frames: [CGRect]) {
        let rebuild = kinds != self.kinds
        self.kinds = kinds
        self.frames = frames
        titles = kinds.map { labels.emojiSectionTitle($0) }
        if rebuild {
            icons.forEach { $0.removeFromSuperview() }
            icons = kinds.map { kind in
                let icon = UIImageView(image: KeySymbolCache.image(name: kind.symbolName, pointSize: 16, weight: .medium))
                icon.contentMode = .center
                icon.isUserInteractionEnabled = false
                addSubview(icon)
                return icon
            }
            selectedIndex = min(selectedIndex, max(0, kinds.count - 1))
        }
        setNeedsLayout()
        restyle()
    }

    func select(_ index: Int) {
        guard kinds.indices.contains(index), index != selectedIndex || disc.isHidden else { return }
        selectedIndex = index
        setNeedsLayout()
        restyle()
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        for (icon, frame) in zip(icons, frames) {
            icon.frame = frame
        }
        if frames.indices.contains(selectedIndex) {
            let frame = frames[selectedIndex]
            let side = max(0, min(frame.width - 2, frame.height - 4, 34))
            disc.frame = CGRect(x: frame.midX - side / 2, y: frame.midY - side / 2, width: side, height: side)
            disc.layer.cornerRadius = side / 2
            disc.isHidden = false
        } else {
            disc.isHidden = true
        }
        rebuildAccessibility()
    }

    private func restyle() {
        disc.backgroundColor = theme.isDark ? theme.letterKey : theme.specialKey
        for (index, icon) in icons.enumerated() {
            icon.tintColor = index == selectedIndex ? theme.primaryText : theme.secondaryText
        }
        rebuildAccessibility()
    }

    // MARK: Touches

    override func touchesBegan(_ touches: Set<UITouch>, with event: UIEvent?) {
        guard let touch = touches.first else { return }
        touchedIndex = nil
        track(touch)
    }

    override func touchesMoved(_ touches: Set<UITouch>, with event: UIEvent?) {
        guard let touch = touches.first else { return }
        track(touch)
    }

    override func touchesEnded(_ touches: Set<UITouch>, with event: UIEvent?) {
        touchedIndex = nil
    }

    override func touchesCancelled(_ touches: Set<UITouch>, with event: UIEvent?) {
        touchedIndex = nil
    }

    private func track(_ touch: UITouch) {
        guard let index = index(at: touch.location(in: self)), index != touchedIndex else { return }
        touchedIndex = index
        UIDevice.current.playInputClick()
        choose(index)
    }

    private func choose(_ index: Int) {
        select(index)
        onSelect?(index)
    }

    private func index(at point: CGPoint) -> Int? {
        guard !frames.isEmpty else { return nil }
        if let hit = frames.firstIndex(where: { $0.minX <= point.x && point.x < $0.maxX }) { return hit }
        return point.x < frames[0].minX ? 0 : frames.count - 1
    }

    // MARK: Accessibility

    private func rebuildAccessibility() {
        accessibilityElements = zip(titles, frames).enumerated().map { index, item in
            let element = CategoryAccessibilityElement(accessibilityContainer: self)
            element.accessibilityLabel = item.0
            element.accessibilityFrameInContainerSpace = item.1
            element.accessibilityTraits = index == selectedIndex ? [.button, .selected] : .button
            element.onActivate = { [weak self] in
                self?.choose(index)
                return true
            }
            return element
        }
    }
}

private final class CategoryAccessibilityElement: UIAccessibilityElement {
    var onActivate: (() -> Bool)?

    override func accessibilityActivate() -> Bool {
        onActivate?() ?? false
    }
}
