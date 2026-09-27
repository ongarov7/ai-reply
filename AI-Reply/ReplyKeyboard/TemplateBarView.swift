import UIKit

protocol TemplateBarViewDelegate: AnyObject {
    func templateBar(_ bar: TemplateBarView, didSelectTemplateID id: String)
    func templateBarDidRequestNewTemplate(_ bar: TemplateBarView)
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

/// The persona row above the keys: Дос | Клиент | Бизнес | Жұмыс | +
///
/// * 36pt tall and horizontally scrolling, so however many personas a user
///   has, the keys below never shrink.
/// * The persona used last is shown selected, so the row says who the user
///   was talking to a moment ago.
/// * "+" opens a menu: the personas hidden from the row, then "Add persona",
///   which says where personas are created - a keyboard extension cannot open
///   an editor or its own app, and pretending otherwise would be a dead tap.
///
/// Not in the typing path: nothing here is touched on a keypress.
final class TemplateBarView: UIView {

    weak var delegate: TemplateBarViewDelegate?

    static let preferredHeight: CGFloat = 36

    private let scrollView = UIScrollView()
    private let stack = UIStackView()
    private let hintLabel = UILabel()
    private let addButton = UIButton(type: .system)

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
        scrollView.alwaysBounceHorizontal = true
        scrollView.contentInset = UIEdgeInsets(top: 0, left: 8, bottom: 0, right: 8)
        // Otherwise a horizontal drag that starts on a pill is swallowed by
        // the button and the row feels stuck.
        scrollView.delaysContentTouches = false
        scrollView.canCancelContentTouches = true
        addSubview(scrollView)

        stack.translatesAutoresizingMaskIntoConstraints = false
        stack.axis = .horizontal
        stack.alignment = .center
        stack.spacing = 6
        scrollView.addSubview(stack)

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
        addButton.widthAnchor.constraint(equalToConstant: 40).isActive = true
        addButton.heightAnchor.constraint(equalToConstant: 30).isActive = true

        NSLayoutConstraint.activate([
            stack.leadingAnchor.constraint(equalTo: scrollView.contentLayoutGuide.leadingAnchor),
            stack.trailingAnchor.constraint(equalTo: scrollView.contentLayoutGuide.trailingAnchor),
            stack.topAnchor.constraint(equalTo: scrollView.contentLayoutGuide.topAnchor),
            stack.bottomAnchor.constraint(equalTo: scrollView.contentLayoutGuide.bottomAnchor),
            stack.heightAnchor.constraint(equalTo: scrollView.frameLayoutGuide.heightAnchor)
        ])
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        scrollView.frame = bounds
        hintLabel.frame = bounds.insetBy(dx: 12, dy: 0)
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
        for view in stack.arrangedSubviews {
            stack.removeArrangedSubview(view)
            view.removeFromSuperview()
        }
        pills.removeAll(keepingCapacity: true)

        for (index, chip) in chips.enumerated() {
            let pill = makePill()
            pill.tag = index
            applyTitle(chip.name, to: pill)
            pill.addTarget(self, action: #selector(templateTapped(_:)), for: .touchUpInside)
            stack.addArrangedSubview(pill)
            pills.append(pill)
        }
        stack.addArrangedSubview(addButton)
        hintLabel.isHidden = !chips.isEmpty
    }

    private func makePill() -> UIButton {
        let button = UIButton(type: .system)
        var configuration = UIButton.Configuration.plain()
        configuration.contentInsets = NSDirectionalEdgeInsets(top: 0, leading: 14, bottom: 0, trailing: 14)
        configuration.background.cornerRadius = 15
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
        button.heightAnchor.constraint(equalToConstant: 30).isActive = true
        return button
    }

    private func isSelected(_ pill: UIButton) -> Bool {
        guard chips.indices.contains(pill.tag), pills.contains(where: { $0 === pill }) else { return false }
        return chips[pill.tag].id == selectedID
    }

    private func applyTitle(_ title: String, to button: UIButton) {
        var attributes = AttributeContainer()
        attributes.font = .systemFont(ofSize: 14, weight: .medium)
        button.configuration?.attributedTitle = AttributedString(title, attributes: attributes)
        button.accessibilityLabel = title
    }

    private func restyle() {
        for pill in pills {
            pill.accessibilityTraits = isSelected(pill) ? [.button, .selected] : .button
            pill.setNeedsUpdateConfiguration()
        }
        addButton.configuration?.background.backgroundColor = theme.letterKey
        addButton.configuration?.baseForegroundColor = theme.primaryText
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

    @objc private func templateTapped(_ sender: UIButton) {
        guard chips.indices.contains(sender.tag) else { return }
        delegate?.templateBar(self, didSelectTemplateID: chips[sender.tag].id)
    }
}
