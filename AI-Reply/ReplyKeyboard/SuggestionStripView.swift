import UIKit

/// The suggestion strip, drawn like the system's QuickType bar: three equal
/// slots with hairlines between them, the typed word in the app language's
/// quotation marks (“…” or «…»), the word a space would put in its place in
/// bold on a raised background.
///
/// It only ever covers a slot that already exists - the persona pills, or the
/// quick intents in the composer - so showing it never changes the
/// keyboard's height. Texts are swapped in place on every keystroke; nothing
/// is rebuilt.
final class SuggestionStripView: UIView {

    var onPick: ((AutocorrectSuggestion) -> Void)?

    private static let slotCount = AutocorrectEngine.stripLimit

    private var slots: [SuggestionSlot] = []
    private var separators: [UIView] = []
    private var items: [AutocorrectSuggestion?] = Array(repeating: nil, count: SuggestionStripView.slotCount)
    private var theme = KeyboardTheme(isDark: false)
    private var strings = TypingStrings.forLanguage(.english)

    override init(frame: CGRect) {
        super.init(frame: frame)
        for index in 0..<Self.slotCount {
            let slot = SuggestionSlot()
            slot.tag = index
            slot.addTarget(self, action: #selector(slotTapped(_:)), for: .touchUpInside)
            addSubview(slot)
            slots.append(slot)
        }
        for _ in 1..<Self.slotCount {
            let separator = UIView()
            separator.isUserInteractionEnabled = false
            addSubview(separator)
            separators.append(separator)
        }
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not used")
    }

    func configure(theme: KeyboardTheme, uiLanguage: AppLanguage) {
        self.theme = theme
        strings = TypingStrings.forLanguage(uiLanguage)
        separators.forEach { $0.backgroundColor = theme.secondaryText.withAlphaComponent(0.35) }
        slots.forEach { $0.apply(theme: theme) }
        render()
    }

    /// Up to three suggestions in the engine's order. One word sits in the
    /// middle slot and a pending correction always lands there too, where the
    /// system keyboard puts it.
    func show(_ suggestions: [AutocorrectSuggestion]) {
        var placed: [AutocorrectSuggestion?] = Array(repeating: nil, count: Self.slotCount)
        if suggestions.count == 1 {
            placed[1] = suggestions[0]
        } else {
            for (index, suggestion) in suggestions.prefix(Self.slotCount).enumerated() {
                placed[index] = suggestion
            }
        }
        guard placed != items else { return }
        items = placed
        render()
    }

    private func render() {
        for (slot, item) in zip(slots, items) {
            slot.show(item, strings: strings)
        }
        // A hairline only between two words, never next to the raised slot.
        for (index, separator) in separators.enumerated() {
            let left = items[index]
            let right = items[index + 1]
            separator.isHidden = left == nil || right == nil || left?.kind == .correction || right?.kind == .correction
        }
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        let count = CGFloat(Self.slotCount)
        let width = bounds.width / count
        let hairline = 1 / max(traitCollection.displayScale, 1)
        for (index, slot) in slots.enumerated() {
            slot.frame = CGRect(x: (CGFloat(index) * width).rounded(), y: 0, width: width.rounded(), height: bounds.height)
        }
        let separatorHeight = min(bounds.height, 20)
        for (index, separator) in separators.enumerated() {
            let x = (CGFloat(index + 1) * width).rounded()
            separator.frame = CGRect(x: x - hairline / 2, y: (bounds.height - separatorHeight) / 2, width: hairline, height: separatorHeight)
        }
    }

    @objc private func slotTapped(_ sender: SuggestionSlot) {
        guard items.indices.contains(sender.tag), let item = items[sender.tag] else { return }
        onPick?(item)
    }
}

/// One slot of the strip.
private final class SuggestionSlot: UIControl {

    private let label = UILabel()
    private let highlight = UIView()
    private var theme = KeyboardTheme(isDark: false)
    private var isDefault = false

    override init(frame: CGRect) {
        super.init(frame: frame)
        highlight.isUserInteractionEnabled = false
        highlight.layer.cornerRadius = 7
        highlight.layer.cornerCurve = .continuous
        addSubview(highlight)
        label.textAlignment = .center
        label.lineBreakMode = .byTruncatingMiddle
        label.adjustsFontSizeToFitWidth = true
        label.minimumScaleFactor = 0.8
        addSubview(label)
        isAccessibilityElement = true
        accessibilityTraits = .button
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not used")
    }

    func apply(theme: KeyboardTheme) {
        self.theme = theme
        label.textColor = theme.primaryText
        refreshBackground()
    }

    func show(_ item: AutocorrectSuggestion?, strings: TypingStrings) {
        guard let item else {
            label.text = nil
            isDefault = false
            isEnabled = false
            isAccessibilityElement = false
            refreshBackground()
            return
        }
        isEnabled = true
        isAccessibilityElement = true
        isDefault = item.kind == .correction
        label.font = .systemFont(ofSize: 16, weight: isDefault ? .semibold : .regular)
        switch item.kind {
        case .typed:
            label.text = strings.typed(item.text)
            accessibilityLabel = strings.keepTyped(item.text)
        case .correction:
            label.text = item.text
            accessibilityLabel = strings.correction(item.text)
        case .word:
            label.text = item.text
            accessibilityLabel = item.text
        }
        refreshBackground()
    }

    override var isHighlighted: Bool {
        didSet { refreshBackground() }
    }

    private func refreshBackground() {
        if isHighlighted {
            highlight.backgroundColor = theme.isDark ? theme.letterKey : theme.specialKey
        } else {
            highlight.backgroundColor = isDefault ? theme.letterKey : .clear
        }
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        highlight.frame = bounds.insetBy(dx: 3, dy: 2)
        label.frame = bounds.insetBy(dx: 8, dy: 0)
    }
}
