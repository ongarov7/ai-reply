import UIKit

/// The cleaner version of the instruction, offered in the quick-intent slot
/// after a pause: "✨ Ответь ему вежливо: …". A tap uses it; for a few seconds
/// afterwards the same place offers Undo. Drawn like a quick-intent pill so it
/// reads as one more thing to tap, not as a message.
final class PolishChipView: UIControl {

    private let icon = UIImageView()
    private let label = UILabel()
    private var theme = KeyboardTheme(isDark: false)
    private var strings = TypingStrings.forLanguage(.english)
    private(set) var chip: InstructionPolisher.Chip = .none

    private let pillHeight: CGFloat = 28

    override init(frame: CGRect) {
        super.init(frame: frame)
        layer.cornerCurve = .continuous
        icon.contentMode = .center
        icon.isUserInteractionEnabled = false
        label.font = .systemFont(ofSize: 13, weight: .medium)
        label.lineBreakMode = .byTruncatingTail
        label.isUserInteractionEnabled = false
        addSubview(icon)
        addSubview(label)
        isAccessibilityElement = true
        accessibilityTraits = .button
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not used")
    }

    func configure(theme: KeyboardTheme, uiLanguage: AppLanguage) {
        self.theme = theme
        strings = TypingStrings.forLanguage(uiLanguage)
        label.textColor = theme.primaryText
        icon.tintColor = theme.accent
        refreshBackground()
        show(chip)
    }

    func show(_ chip: InstructionPolisher.Chip) {
        self.chip = chip
        switch chip {
        case .none:
            label.text = nil
            accessibilityLabel = nil
            accessibilityHint = nil
        case .suggestion(let text):
            setSymbol("sparkles")
            label.text = text
            accessibilityLabel = strings.polishSuggestion(text)
            accessibilityHint = strings.polishSuggestionHint
        case .undo:
            setSymbol("arrow.uturn.backward")
            label.text = strings.undo
            accessibilityLabel = strings.undoAccessibility
            accessibilityHint = nil
        }
        setNeedsLayout()
    }

    private func setSymbol(_ name: String) {
        icon.image = UIImage(systemName: name, withConfiguration: UIImage.SymbolConfiguration(pointSize: 12, weight: .semibold))
    }

    override var isHighlighted: Bool {
        didSet { refreshBackground() }
    }

    private func refreshBackground() {
        backgroundColor = isHighlighted ? theme.specialKey : theme.fieldBackground
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        layer.cornerRadius = min(bounds.height, pillHeight) / 2
        let inset: CGFloat = 11
        let iconWidth: CGFloat = 16
        icon.frame = CGRect(x: inset, y: 0, width: iconWidth, height: bounds.height)
        let textX = icon.frame.maxX + 5
        if chip == .undo {
            // Undo is short: centred in the pill, the arrow before it.
            let width = min(ceil(label.intrinsicContentSize.width), bounds.width - textX - inset)
            let start = max(inset, (bounds.width - iconWidth - 5 - width) / 2)
            icon.frame.origin.x = start
            label.frame = CGRect(x: start + iconWidth + 5, y: 0, width: width, height: bounds.height)
        } else {
            label.frame = CGRect(x: textX, y: 0, width: max(0, bounds.width - textX - inset), height: bounds.height)
        }
    }
}
