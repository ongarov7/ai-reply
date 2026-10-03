import UIKit

protocol QuickActionRowDelegate: AnyObject {
    func quickActionRow(_ row: QuickActionRow, didSelect intent: QuickIntent)
}

/// The one-tap reply intents, laid out so that a button is either FULLY VISIBLE
/// or not on the row at all.
///
/// WHAT THIS REPLACES, AND WHY. The intents used to live in a horizontal
/// `UIScrollView` that shared a 38pt row with the primary button and had its
/// scroll indicator switched off. On a Kazakh keyboard the three pills the user
/// actually wants - `Келісу`, `Бас тарту`, `Нақтылау` - measure ~250pt while the
/// scroll view is handed ~224pt, so the third one was drawn with its right half
/// past the clip edge and nothing on screen said it could be scrolled. That is
/// the cropped button in the bug report. It was never a frame miscalculation;
/// it was a scroll view pretending to be a row.
///
/// THE RULE HERE: measure every localized label first, lay out only what fits
/// whole, and put the remainder behind an overflow menu that shows the labels
/// in full. Nothing is clipped, nothing is shrunk to fit, and the row's height
/// never depends on how long Kazakh happens to be.
///
/// When two or three pills fit with width to spare they are stretched to equal
/// widths, which is what makes a short set read as a deliberate row of primary
/// actions rather than a ragged left-aligned strip.
final class QuickActionRow: UIView {

    weak var delegate: QuickActionRowDelegate?

    static let preferredHeight: CGFloat = 30

    private let pillHeight: CGFloat = 28
    private let spacing: CGFloat = 6
    private let overflowWidth: CGFloat = 38
    private let horizontalInset: CGFloat = 11
    private let font = UIFont.systemFont(ofSize: 12.5, weight: .medium)

    /// Widest a single pill may become when the row stretches to fill. Without
    /// it two short labels on a 430pt screen become two 190pt slabs.
    private let maximumPillWidth: CGFloat = 132

    private var intents: [QuickIntent] = []
    private var pills: [UIButton] = []
    private var theme = KeyboardTheme(isDark: true)
    private var moreLabel = "More"

    private let overflowButton = UIButton(type: .system)

    /// Index after the last intent laid out as a pill; everything from here on
    /// is in the overflow menu.
    private var visibleCount = 0
    private var laidOutWidth: CGFloat = -1
    private var isRowEnabled = true

    // MARK: Init

    init() {
        super.init(frame: .zero)
        translatesAutoresizingMaskIntoConstraints = false
        clipsToBounds = false
        buildOverflowButton()
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not used")
    }

    private func buildOverflowButton() {
        var configuration = UIButton.Configuration.plain()
        configuration.contentInsets = .zero
        configuration.background.cornerRadius = pillHeight / 2
        configuration.image = UIImage(
            systemName: "ellipsis",
            withConfiguration: UIImage.SymbolConfiguration(pointSize: 13, weight: .semibold)
        )
        overflowButton.configuration = configuration
        overflowButton.showsMenuAsPrimaryAction = true
        overflowButton.isHidden = true
        applyPressedStateHandling(to: overflowButton)
        addSubview(overflowButton)
    }

    // MARK: Configuration

    /// - Parameter intents: the set to show; the reply intents when nil.
    func configure(theme: KeyboardTheme, strings: AIReplyStrings, intents: [QuickIntent]? = nil) {
        self.theme = theme
        self.moreLabel = strings.moreActions
        overflowButton.accessibilityLabel = strings.moreActions
        setIntents(intents ?? strings.quickIntents)
        restyle()
    }

    /// Swaps the set - the Create panel shows occasions and tones instead of
    /// reply intents.
    func showIntents(_ newIntents: [QuickIntent]) {
        setIntents(newIntents)
        restyle()
    }

    /// Rebuilds the buttons only when the intent SET changed. A theme change or
    /// a re-layout reuses them, so nothing here is ever rebuilt on a keystroke.
    private func setIntents(_ newIntents: [QuickIntent]) {
        guard newIntents != intents else { return }
        intents = newIntents

        pills.forEach { $0.removeFromSuperview() }
        pills.removeAll(keepingCapacity: true)

        for (index, intent) in intents.enumerated() {
            let pill = makePill(title: intent.label)
            pill.tag = index
            pill.addTarget(self, action: #selector(pillTapped(_:)), for: .touchUpInside)
            pill.isEnabled = isRowEnabled
            pill.alpha = isRowEnabled ? 1 : 0.45
            addSubview(pill)
            pills.append(pill)
        }
        laidOutWidth = -1
        setNeedsLayout()
    }

    private func makePill(title: String) -> UIButton {
        let button = UIButton(type: .system)
        var configuration = UIButton.Configuration.plain()
        configuration.contentInsets = NSDirectionalEdgeInsets(
            top: 0, leading: horizontalInset, bottom: 0, trailing: horizontalInset
        )
        configuration.background.cornerRadius = pillHeight / 2
        var attributes = AttributeContainer()
        attributes.font = font
        configuration.attributedTitle = AttributedString(title, attributes: attributes)
        button.configuration = configuration
        // Truncation would be a quieter version of the same bug, so a pill that
        // cannot be drawn whole is moved into the menu instead of being cut.
        button.titleLabel?.lineBreakMode = .byTruncatingTail
        button.accessibilityLabel = title
        button.accessibilityTraits = .button
        applyPressedStateHandling(to: button)
        return button
    }

    /// A configured button rewrites its own background on every state change,
    /// so a pressed state assigned directly to `backgroundColor` is undone the
    /// moment UIKit updates the configuration. This is the supported hook.
    private func applyPressedStateHandling(to button: UIButton) {
        button.configurationUpdateHandler = { [weak self] button in
            guard let self else { return }
            button.configuration?.background.backgroundColor = button.isHighlighted
                ? self.theme.specialKey
                : self.theme.fieldBackground
            button.configuration?.baseForegroundColor = self.theme.primaryText
        }
    }

    private func restyle() {
        for button in pills + [overflowButton] {
            button.tintColor = theme.primaryText
            button.setNeedsUpdateConfiguration()
        }
    }

    /// Dimmed and inert while a request is in flight or a result is on screen,
    /// because an intent can only be written into an instruction that is still
    /// being composed.
    func setEnabled(_ enabled: Bool) {
        guard enabled != isRowEnabled else { return }
        isRowEnabled = enabled
        for button in pills + [overflowButton] {
            button.isEnabled = enabled
            button.alpha = enabled ? 1 : 0.45
        }
    }

    // MARK: Measurement

    private func pillWidth(for title: String) -> CGFloat {
        let text = title as NSString
        let measured = text.size(withAttributes: [.font: font]).width
        return (measured + horizontalInset * 2).rounded(.up)
    }

    /// Height is fixed by design. A row whose height depended on the length of
    /// the current language's labels would move the keys every time the app
    /// language changed, which is exactly the kind of jump this pass removes.
    func preferredHeight(forWidth width: CGFloat) -> CGFloat {
        width > 0 && !intents.isEmpty ? Self.preferredHeight : 0
    }

    // MARK: Layout

    override func layoutSubviews() {
        super.layoutSubviews()
        let width = bounds.width
        guard width > 0 else { return }
        guard abs(width - laidOutWidth) > 0.5 else { return }
        laidOutWidth = width
        packRow(width: width)
    }

    private func packRow(width: CGFloat) {
        guard !intents.isEmpty else {
            overflowButton.isHidden = true
            return
        }

        var widths = intents.map { pillWidth(for: $0.label) }
        // A single label longer than the whole row can only be shown in the
        // menu; clamping it here would be truncation by another name.
        let maximumSingle = width - overflowWidth - spacing
        var fitted = 0
        var used: CGFloat = 0

        for index in widths.indices {
            let candidate = used + (fitted == 0 ? 0 : spacing) + widths[index]
            let remaining = intents.count - (index + 1)
            // Room for the overflow control has to be reserved whenever
            // something is going to be left over, otherwise the last pill is
            // laid out and then the menu button lands on top of it.
            let reserve = remaining > 0 ? overflowWidth + spacing : 0
            guard candidate + reserve <= width, widths[index] <= maximumSingle else { break }
            used = candidate
            fitted += 1
        }

        // Never show a bare menu button: one pill plus "…" reads better than
        // "…" alone, and there is always room for the shortest label.
        if fitted == 0, let first = widths.first, first <= max(maximumSingle, 0) {
            fitted = 1
        }

        visibleCount = fitted
        let hasOverflow = fitted < intents.count
        overflowButton.isHidden = !hasOverflow

        for (index, pill) in pills.enumerated() {
            pill.isHidden = index >= fitted
        }

        guard fitted > 0 else {
            if hasOverflow { layoutOverflowOnly(width: width) }
            rebuildOverflowMenu()
            return
        }

        // Two or three actions with width to spare are stretched to equal
        // widths. This is the "equal-width buttons when they fit comfortably"
        // case; more than three would make each one too narrow to read.
        let reserve = hasOverflow ? overflowWidth + spacing : 0
        let available = width - reserve
        let gaps = CGFloat(fitted - 1) * spacing
        if fitted <= 3 {
            let equal = min(maximumPillWidth, ((available - gaps) / CGFloat(fitted)).rounded(.down))
            let natural = widths.prefix(fitted).max() ?? equal
            let slot = max(natural, equal)
            // Only stretch when the stretched row still fits whole.
            if slot * CGFloat(fitted) + gaps <= available {
                for index in 0..<fitted { widths[index] = slot }
            }
        }

        // Left-aligned, always. A row that centres itself when it happens to
        // be short moves its buttons sideways when the app language changes,
        // and predictable beats pretty on a control the thumb aims at.
        var x: CGFloat = 0
        let y = ((bounds.height - pillHeight) / 2).rounded()
        for index in 0..<fitted {
            pills[index].frame = CGRect(x: x, y: y, width: widths[index], height: pillHeight)
            x += widths[index] + spacing
        }

        if hasOverflow {
            overflowButton.frame = CGRect(
                x: width - overflowWidth,
                y: y,
                width: overflowWidth,
                height: pillHeight
            )
        }
        rebuildOverflowMenu()
    }

    private func layoutOverflowOnly(width: CGFloat) {
        let y = ((bounds.height - pillHeight) / 2).rounded()
        overflowButton.frame = CGRect(
            x: width - overflowWidth, y: y, width: overflowWidth, height: pillHeight
        )
    }

    /// The overflow menu carries the FULL label of every action that did not
    /// fit, so a long Kazakh phrase is never the reason an action becomes
    /// unreachable.
    private func rebuildOverflowMenu() {
        guard visibleCount < intents.count else {
            overflowButton.menu = nil
            return
        }
        let hidden = intents[visibleCount...]
        let actions = hidden.map { intent in
            UIAction(title: intent.label) { [weak self] _ in
                guard let self else { return }
                self.delegate?.quickActionRow(self, didSelect: intent)
            }
        }
        overflowButton.menu = UIMenu(title: moreLabel, children: actions)
    }

    // MARK: Actions

    @objc private func pillTapped(_ sender: UIButton) {
        guard intents.indices.contains(sender.tag) else { return }
        delegate?.quickActionRow(self, didSelect: intents[sender.tag])
    }
}
