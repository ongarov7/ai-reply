import UIKit

protocol TemplateBarViewDelegate: AnyObject {
    func templateBar(_ bar: TemplateBarView, didSelectTemplateID id: String)
    /// "Create": write a new message with AI, no copied message involved.
    func templateBarDidTapCreate(_ bar: TemplateBarView)
}

/// The only thing the row needs to draw one persona: an identifier to report
/// back and a name to show.
///
/// Deliberately NOT a `ReplyTemplate`: the row has no business holding a
/// user's instructions or business rules, and two strings are what let it be
/// drawn from the App Group summary on the very first frame.
struct TemplateChip: Equatable {
    let id: String
    let name: String
}

/// The persona row above the keys:  ✨ | Дос | Клиент | Бизнес | Жұмыс
///
/// * 36pt tall, and ADAPTIVE to the width: personas that fit share the row
///   evenly; when space is short their padding tightens first; only when they
///   still do not fit does the row scroll (with a fade at the trailing edge,
///   never a cut label). The keys below never shrink. `PersonaRowLayout`
///   does the arithmetic.
/// * The persona used last is shown selected, so the row says who the user
///   was talking to a moment ago.
/// * "✨" (Create) is pinned at the LEADING edge, outside the personas, so it
///   is never cut off, faded or scrolled away. It opens the second AI mode -
///   writing a message from a description - and has nothing to do with the
///   copied message. Icon only: a label next to the personas read as one
///   more persona. It is also the row's first element for VoiceOver.
/// * Personas are made, hidden and ordered in the app; the row shows the
///   visible ones, in that order.
///
/// Not in the typing path: nothing here is touched on a keypress.
final class TemplateBarView: UIView {

    weak var delegate: TemplateBarViewDelegate?

    static let preferredHeight: CGFloat = PersonaRowLayout.height

    private let createButton = UIButton(type: .system)
    private let scrollView = UIScrollView()
    private let hintLabel = UILabel()
    /// Fades the personas out at the trailing edge while the row scrolls, so
    /// a partly hidden chip reads as "scroll for more" rather than a cut label.
    private let fadeMask = CAGradientLayer()

    private let pillFont = UIFont.systemFont(ofSize: 14, weight: .medium)

    private var theme = KeyboardTheme(isDark: true)
    private var strings = AIReplyStrings.forLanguage(.english)
    private var chips: [TemplateChip] = []
    private var selectedID: String?
    private var pills: [UIButton] = []

    // MARK: Init

    init() {
        super.init(frame: .zero)
        build()
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not used")
    }

    private func build() {
        // ✨ first: the leading element on screen and for VoiceOver.
        var create = UIButton.Configuration.plain()
        create.contentInsets = .zero
        create.background.cornerRadius = 15
        create.image = UIImage(systemName: "sparkles", withConfiguration: UIImage.SymbolConfiguration(pointSize: 14, weight: .semibold))
        createButton.configuration = create
        // A key-coloured disc; only the glyph carries the accent, so it reads
        // as an action rather than one more persona.
        createButton.configurationUpdateHandler = { [weak self] button in
            guard let self else { return }
            button.configuration?.background.backgroundColor = button.isHighlighted
                ? self.theme.letterKey.withAlphaComponent(0.6)
                : self.theme.letterKey
            button.configuration?.baseForegroundColor = self.theme.accent
        }
        createButton.addTarget(self, action: #selector(createTapped), for: .touchUpInside)
        addSubview(createButton)

        scrollView.showsHorizontalScrollIndicator = false
        scrollView.alwaysBounceHorizontal = false
        // Otherwise a horizontal drag that starts on a pill is swallowed by
        // the button and the row feels stuck.
        scrollView.delaysContentTouches = false
        scrollView.canCancelContentTouches = true
        scrollView.delegate = self
        addSubview(scrollView)
        fadeMask.startPoint = CGPoint(x: 0, y: 0.5)
        fadeMask.endPoint = CGPoint(x: 1, y: 0.5)
        fadeMask.colors = [UIColor.black.cgColor, UIColor.black.cgColor, UIColor.clear.cgColor]

        hintLabel.font = .systemFont(ofSize: 11.5, weight: .regular)
        hintLabel.textAlignment = .center
        hintLabel.adjustsFontSizeToFitWidth = true
        hintLabel.minimumScaleFactor = 0.8
        hintLabel.isHidden = true
        addSubview(hintLabel)
        updateAccessibilityOrder()
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        // Measures every name; `PersonaRowLayout` picks the roomiest padding
        // at which the whole set fits, stretches a set that fits, and lets
        // one that does not fit even at the tightest padding scroll.
        let texts = pills.indices.map { index -> CGFloat in
            let name = chips.indices.contains(index) ? chips[index].name : ""
            return ceil((name as NSString).size(withAttributes: [.font: pillFont]).width)
        }
        let layout = PersonaRowLayout(width: bounds.width, height: bounds.height, textWidths: texts)
        createButton.frame = layout.createFrame
        scrollView.frame = layout.personasFrame
        hintLabel.frame = layout.personasFrame.insetBy(dx: 12, dy: 0)
        for (pill, frame) in zip(pills, layout.pillFrames) {
            pill.frame = frame
        }
        scrollView.contentSize = CGSize(width: layout.contentWidth, height: bounds.height)
        updateFade()
    }

    /// The fade is drawn only while there is more row past the trailing edge,
    /// and only there: the leading edge borders ✨, which is never faded.
    fileprivate func updateFade() {
        let width = scrollView.bounds.width
        let hidden = scrollView.contentSize.width - (scrollView.contentOffset.x + width)
        guard width > 0, hidden > 1 else {
            scrollView.layer.mask = nil
            return
        }
        let fade: CGFloat = 24
        CATransaction.begin()
        CATransaction.setDisableActions(true)
        // The mask lives in the layer's bounds, which move with the offset.
        fadeMask.frame = scrollView.bounds
        fadeMask.locations = [0, NSNumber(value: Double(max(0, width - fade) / width)), 1]
        scrollView.layer.mask = fadeMask
        CATransaction.commit()
    }

    // MARK: Suggestion strip

    /// Where the persona pills are: the row to the right of ✨. The
    /// suggestion strip covers exactly this while a word is typed, so ✨
    /// never moves or disappears.
    var personasFrame: CGRect { scrollView.frame }

    /// Fades the pills out for the suggestion strip, and back. Inside an
    /// animation block the fade is animated.
    func setPersonasHidden(_ hidden: Bool) {
        scrollView.alpha = hidden ? 0 : 1
        hintLabel.alpha = hidden ? 0 : 1
        scrollView.isUserInteractionEnabled = !hidden
        scrollView.accessibilityElementsHidden = hidden
        hintLabel.accessibilityElementsHidden = hidden
        updateAccessibilityOrder()
    }

    /// ✨ first, then the personas (or the hint when there are none), so
    /// VoiceOver reads the row in the order it is drawn.
    private func updateAccessibilityOrder() {
        var elements: [Any] = [createButton]
        if !scrollView.accessibilityElementsHidden { elements.append(scrollView) }
        if !hintLabel.isHidden, !hintLabel.accessibilityElementsHidden { elements.append(hintLabel) }
        accessibilityElements = elements
    }

    // MARK: Configuration

    /// - Parameter uiLanguage: the APP's language. Switching the keyboard
    ///   layout does not reach here: the chips stay in the user's language
    ///   while they switch layouts to type.
    func configure(theme: KeyboardTheme, uiLanguage: AppLanguage) {
        self.theme = theme
        self.strings = AIReplyStrings.forLanguage(uiLanguage)
        hintLabel.textColor = theme.secondaryText
        hintLabel.text = strings.chooseTemplate
        createButton.accessibilityLabel = strings.compose.createButtonAccessibility
        restyle()
    }

    /// Replaces the row. Rebuilds only when the set of personas changed; a
    /// rename (app language switch) or a new selection is applied in place.
    func setChips(_ newChips: [TemplateChip], selectedID newSelected: String?) {
        let sameSet = newChips.count == chips.count && zip(newChips, chips).allSatisfy { $0.id == $1.id }
        let changed = newChips != chips || newSelected != selectedID
        chips = newChips
        selectedID = newSelected
        guard changed else { return }

        if sameSet, !pills.isEmpty {
            for (index, pill) in pills.enumerated() where index < chips.count {
                applyTitle(chips[index].name, to: pill)
            }
        } else {
            rebuildPills()
        }
        restyle()
    }

    private func rebuildPills() {
        pills.forEach { $0.removeFromSuperview() }
        pills.removeAll(keepingCapacity: true)

        for (index, chip) in chips.enumerated() {
            let pill = makePill()
            pill.tag = index
            applyTitle(chip.name, to: pill)
            pill.addTarget(self, action: #selector(templateTapped(_:)), for: .touchUpInside)
            scrollView.addSubview(pill)
            pills.append(pill)
        }
        hintLabel.isHidden = !chips.isEmpty
        updateAccessibilityOrder()
        setNeedsLayout()
    }

    private func makePill() -> UIButton {
        let button = UIButton(type: .system)
        var configuration = UIButton.Configuration.plain()
        // The frame is set by `layoutSubviews`; the title is centred in it.
        configuration.contentInsets = NSDirectionalEdgeInsets(top: 0, leading: 4, bottom: 0, trailing: 4)
        configuration.background.cornerRadius = 15
        configuration.titleLineBreakMode = .byTruncatingTail
        button.configuration = configuration
        // A configured button rewrites its background on every state change,
        // so selection and highlight live in the update handler.
        button.configurationUpdateHandler = { [weak self] button in
            guard let self else { return }
            let selected = self.isSelected(button)
            let background = selected ? self.theme.accent : self.theme.letterKey
            button.configuration?.background.backgroundColor = button.isHighlighted
                ? background.withAlphaComponent(0.6)
                : background
            button.configuration?.baseForegroundColor = selected ? .white : self.theme.primaryText
        }
        return button
    }

    private func isSelected(_ pill: UIButton) -> Bool {
        guard chips.indices.contains(pill.tag), pills.contains(where: { $0 === pill }) else { return false }
        return chips[pill.tag].id == selectedID
    }

    private func applyTitle(_ title: String, to button: UIButton) {
        var attributes = AttributeContainer()
        attributes.font = pillFont
        button.configuration?.attributedTitle = AttributedString(title, attributes: attributes)
        button.accessibilityLabel = title
        setNeedsLayout()
    }

    private func restyle() {
        for pill in pills {
            pill.accessibilityTraits = isSelected(pill) ? [.button, .selected] : .button
            pill.setNeedsUpdateConfiguration()
        }
        createButton.setNeedsUpdateConfiguration()
    }

    // MARK: Actions

    @objc private func createTapped() {
        delegate?.templateBarDidTapCreate(self)
    }

    @objc private func templateTapped(_ sender: UIButton) {
        guard chips.indices.contains(sender.tag) else { return }
        delegate?.templateBar(self, didSelectTemplateID: chips[sender.tag].id)
    }
}

extension TemplateBarView: UIScrollViewDelegate {
    func scrollViewDidScroll(_ scrollView: UIScrollView) {
        updateFade()
    }
}
