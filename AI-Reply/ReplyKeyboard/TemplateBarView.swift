import UIKit

protocol TemplateBarViewDelegate: AnyObject {
    func templateBar(_ bar: TemplateBarView, didSelectTemplateID id: String)
    func templateBarDidRequestNewTemplate(_ bar: TemplateBarView)
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

/// The persona row above the keys: Дос | Клиент | Бизнес | Жұмыс | +   ✨
///
/// * 36pt tall, and ADAPTIVE to the width: personas that fit share the row
///   evenly; when space is short their padding tightens first; only when they
///   still do not fit does the row scroll (with a fade, never a cut label).
///   The keys below never shrink.
/// * The persona used last is shown selected, so the row says who the user
///   was talking to a moment ago.
/// * "+" and "✨" are pinned together at the trailing edge, outside the
///   personas, so neither is ever cut off or scrolled away.
/// * "+" opens a menu: the personas hidden from the row, then "Add persona",
///   which says where personas are created - a keyboard extension cannot open
///   an editor or its own app, and pretending otherwise would be a dead tap.
///
/// * "✨" (Create) opens the second AI mode - writing a message from a
///   description - and has nothing to do with the copied message. Icon only,
///   styled like "+": a label next to the personas read as one more persona.
///
/// Not in the typing path: nothing here is touched on a keypress.
final class TemplateBarView: UIView {

    weak var delegate: TemplateBarViewDelegate?

    static let preferredHeight: CGFloat = 36

    private let scrollView = UIScrollView()
    private let hintLabel = UILabel()
    private let addButton = UIButton(type: .system)
    private let createButton = UIButton(type: .system)
    /// Fades the personas out at the right edge while the row scrolls, so a
    /// partly hidden chip reads as "scroll for more" rather than a cut label.
    private let fadeMask = CAGradientLayer()

    // Metrics
    private let pillHeight: CGFloat = 30
    private let actionWidth: CGFloat = 38
    private let spacing: CGFloat = 6
    private let edgeInset: CGFloat = 8
    private let pillFont = UIFont.systemFont(ofSize: 14, weight: .medium)
    /// Text padding per side: roomy when everything fits, tighter on a narrow
    /// row, and never below the last value.
    private let paddings: [CGFloat] = [14, 11, 8]
    /// Widest extra a pill gets when the row stretches: two personas on a Max
    /// phone should not become two slabs.
    private let maximumStretch: CGFloat = 28

    private var theme = KeyboardTheme(isDark: true)
    private var strings = AIReplyStrings.forLanguage(.english)
    private var chips: [TemplateChip] = []
    private var more: [TemplateChip] = []
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

        var add = UIButton.Configuration.plain()
        add.contentInsets = .zero
        add.background.cornerRadius = 15
        add.image = UIImage(systemName: "plus", withConfiguration: UIImage.SymbolConfiguration(pointSize: 13, weight: .semibold))
        addButton.configuration = add
        addButton.showsMenuAsPrimaryAction = true
        addSubview(addButton)

        var create = UIButton.Configuration.plain()
        create.contentInsets = .zero
        create.background.cornerRadius = 15
        create.image = UIImage(systemName: "sparkles", withConfiguration: UIImage.SymbolConfiguration(pointSize: 14, weight: .semibold))
        createButton.configuration = create
        // Same white disc as "+"; only the glyph carries the accent, so the
        // pair reads as the row's two actions.
        createButton.configurationUpdateHandler = { [weak self] button in
            guard let self else { return }
            button.configuration?.background.backgroundColor = button.isHighlighted
                ? self.theme.letterKey.withAlphaComponent(0.6)
                : self.theme.letterKey
            button.configuration?.baseForegroundColor = self.theme.accent
        }
        createButton.addTarget(self, action: #selector(createTapped), for: .touchUpInside)
        addSubview(createButton)
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        let y = ((bounds.height - pillHeight) / 2).rounded()
        createButton.frame = CGRect(x: bounds.width - edgeInset - actionWidth, y: y, width: actionWidth, height: pillHeight)
        addButton.frame = CGRect(x: createButton.frame.minX - spacing - actionWidth, y: y, width: actionWidth, height: pillHeight)
        scrollView.frame = CGRect(x: 0, y: 0, width: max(0, addButton.frame.minX - spacing / 2), height: bounds.height)
        hintLabel.frame = scrollView.frame.insetBy(dx: 12, dy: 0)
        layoutPills(y: y)
        updateFade()
    }

    /// Measures every name, then picks the roomiest padding at which the whole
    /// set fits. A set that fits is stretched evenly to the row's width (up to
    /// `maximumStretch` each); one that does not fit even at the tightest
    /// padding scrolls.
    private func layoutPills(y: CGFloat) {
        let available = scrollView.bounds.width - edgeInset - spacing / 2
        guard !pills.isEmpty, available > 0 else {
            scrollView.contentSize = scrollView.bounds.size
            return
        }
        let texts = pills.indices.map { index -> CGFloat in
            let name = chips.indices.contains(index) ? chips[index].name : ""
            return ceil((name as NSString).size(withAttributes: [.font: pillFont]).width)
        }
        let gaps = spacing * CGFloat(pills.count - 1)
        var padding = paddings.last ?? 8
        for candidate in paddings where texts.reduce(0, +) + candidate * 2 * CGFloat(texts.count) + gaps <= available {
            padding = candidate
            break
        }
        var widths = texts.map { $0 + padding * 2 }
        let used = widths.reduce(0, +) + gaps
        if used <= available {
            let extra = min(maximumStretch, ((available - used) / CGFloat(widths.count)).rounded(.down))
            widths = widths.map { $0 + extra }
        }
        var x = edgeInset
        for (pill, width) in zip(pills, widths) {
            pill.frame = CGRect(x: x, y: y, width: width, height: pillHeight)
            x += width + spacing
        }
        scrollView.contentSize = CGSize(width: max(scrollView.bounds.width, x - spacing + spacing / 2), height: bounds.height)
    }

    /// The fade is drawn only while there is more row past the right edge.
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

    /// Where the persona pills are. The suggestion strip covers exactly this
    /// while a word is typed, so "+" and "✨" never move or disappear.
    var personasFrame: CGRect { scrollView.frame }

    /// Fades the pills out for the suggestion strip, and back. Inside an
    /// animation block the fade is animated.
    func setPersonasHidden(_ hidden: Bool) {
        scrollView.alpha = hidden ? 0 : 1
        hintLabel.alpha = hidden ? 0 : 1
        scrollView.isUserInteractionEnabled = !hidden
        scrollView.accessibilityElementsHidden = hidden
        hintLabel.accessibilityElementsHidden = hidden
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
        addButton.accessibilityLabel = strings.moreActions
        createButton.accessibilityLabel = strings.compose.createButtonAccessibility
        rebuildMenu()
        restyle()
    }

    /// Replaces the row. Rebuilds only when the set of personas changed; a
    /// rename (app language switch) or a new selection is applied in place.
    func setChips(_ newChips: [TemplateChip], more newMore: [TemplateChip], selectedID newSelected: String?) {
        let sameSet = newChips.count == chips.count && zip(newChips, chips).allSatisfy { $0.id == $1.id }
        let changed = newChips != chips || newMore != more || newSelected != selectedID
        chips = newChips
        more = newMore
        selectedID = newSelected
        guard changed else { return }

        if sameSet, !pills.isEmpty {
            for (index, pill) in pills.enumerated() where index < chips.count {
                applyTitle(chips[index].name, to: pill)
            }
        } else {
            rebuildPills()
        }
        rebuildMenu()
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
        setNeedsLayout()
    }

    private func makePill() -> UIButton {
        let button = UIButton(type: .system)
        var configuration = UIButton.Configuration.plain()
        // The frame is set by `layoutPills`; the title is centred in it.
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
        addButton.configuration?.background.backgroundColor = theme.letterKey
        addButton.configuration?.baseForegroundColor = theme.primaryText
        createButton.setNeedsUpdateConfiguration()
    }

    /// "+" : the personas that are not on the row, then the way to add one.
    private func rebuildMenu() {
        let hidden = more.map { chip in
            UIAction(title: chip.name) { [weak self] _ in
                guard let self else { return }
                self.delegate?.templateBar(self, didSelectTemplateID: chip.id)
            }
        }
        let add = UIAction(title: strings.addTemplate, image: UIImage(systemName: "plus.circle")) { [weak self] _ in
            guard let self else { return }
            self.delegate?.templateBarDidRequestNewTemplate(self)
        }
        var children: [UIMenuElement] = []
        if !hidden.isEmpty {
            children.append(UIMenu(title: "", options: .displayInline, children: hidden))
        }
        children.append(add)
        addButton.menu = UIMenu(title: strings.moreActions, children: children)
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
