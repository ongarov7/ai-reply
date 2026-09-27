import UIKit

/// Everything the composer can ask for, as one value, so the controller
/// handles the whole AI flow in one `switch`.
enum ComposerEvent {
    case close
    case changePersona
    case paste
    case generate
    case stop
    case regenerate
    case back
    case toggleEditing
    case tapReply
    case insert
    case previousVersion
    case nextVersion
    case resolveConflict(ReplyComposerFlow.ConflictChoice)
    case edited(ReplyComposerView.Field, String)
}

protocol KeyboardActionBarDelegate: AnyObject {
    func actionBar(_ bar: KeyboardActionBar, didSelectTemplateID id: String)
    func actionBarDidRequestNewTemplate(_ bar: KeyboardActionBar)
    func actionBar(_ bar: KeyboardActionBar, didSend event: ComposerEvent)
    func actionBarDidChangeHeight(_ bar: KeyboardActionBar)
}

/// The area above the keys. Two shapes:
///
/// * PERSONAS - a 36pt row: Дос | Клиент | Бизнес | Жұмыс | + , plus a
///   transient status line that changes no geometry. This is the keyboard at
///   rest; its height is what gets cached for the next launch.
/// * COMPOSER - the AI reply composer (`ReplyComposerView`).
///
/// Neither ever takes height from the keys: the keyboard grows instead, up to
/// the ceiling the controller hands down.
final class KeyboardActionBar: UIView {

    weak var delegate: KeyboardActionBarDelegate?

    private let templateBar = TemplateBarView()
    private let toastLabel = UILabel()
    private let composer = ReplyComposerView()

    private var toastWorkItem: DispatchWorkItem?
    private let idleHeight: CGFloat = TemplateBarView.preferredHeight + 4

    private(set) var isComposing = false

    var preferredHeight: CGFloat {
        isComposing ? composer.preferredHeight : idleHeight
    }

    var compactHeight: CGFloat { idleHeight }

    var wantsExpandedContext: Bool { isComposing && composer.wantsExpandedContext }

    // MARK: Init

    init() {
        super.init(frame: .zero)
        translatesAutoresizingMaskIntoConstraints = false
        composer.delegate = self
        templateBar.delegate = self

        addSubview(templateBar)
        toastLabel.textAlignment = .center
        toastLabel.numberOfLines = 2
        toastLabel.font = .systemFont(ofSize: 12, weight: .medium)
        toastLabel.adjustsFontSizeToFitWidth = true
        toastLabel.minimumScaleFactor = 0.75
        toastLabel.isHidden = true
        addSubview(toastLabel)
        composer.isHidden = true
        addSubview(composer)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not used")
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        templateBar.frame = CGRect(x: 0, y: 2, width: bounds.width, height: TemplateBarView.preferredHeight)
        toastLabel.frame = CGRect(x: 14, y: 1, width: max(0, bounds.width - 28), height: TemplateBarView.preferredHeight + 2)
        composer.frame = bounds
    }

    // MARK: Configuration

    /// This whole area is product UI, so it follows the APP language.
    func configure(theme: KeyboardTheme, uiLanguage: AppLanguage) {
        toastLabel.textColor = theme.secondaryText
        templateBar.configure(theme: theme, uiLanguage: uiLanguage)
        composer.configure(theme: theme, uiLanguage: uiLanguage)
    }

    func setChips(_ chips: [TemplateChip], more: [TemplateChip], selectedID: String?) {
        templateBar.setChips(chips, more: more, selectedID: selectedID)
    }

    func layout(forWidth width: CGFloat) {
        composer.layout(forWidth: width)
    }

    func setMaximumComposerHeight(_ height: CGFloat) {
        composer.setMaximumHeight(height)
    }

    // MARK: Composer

    func beginComposing() {
        cancelToast()
        guard !isComposing else { return }
        isComposing = true
        composer.isHidden = false
        templateBar.isHidden = true
    }

    func render(_ content: ReplyComposerView.Content) {
        composer.render(content)
    }

    func endComposing() {
        guard isComposing else { return }
        isComposing = false
        composer.reset()
        composer.isHidden = true
        templateBar.isHidden = false
    }

    // MARK: Text target passthrough

    /// Whether a keystroke should edit one of the composer's own fields rather
    /// than the host field.
    var acceptsTextInput: Bool { isComposing && composer.acceptsTextInput }

    func insertText(_ text: String) { composer.insertText(text) }
    func deleteBackward() { composer.deleteBackward() }
    func deleteWordBackward() { composer.deleteWordBackward() }
    func moveCaret(by offset: Int) { composer.moveCaret(by: offset) }
    var textBeforeCursor: String? { composer.textBeforeCursor }

    // MARK: Transient status

    /// A short message on the persona row. It never changes the keyboard's
    /// geometry. Failures with the composer open are shown inside it instead.
    func showToast(_ message: String) {
        guard !isComposing, !message.isEmpty else { return }
        cancelToast()
        toastLabel.text = message
        toastLabel.alpha = 0
        toastLabel.isHidden = false
        UIView.animate(withDuration: 0.16) {
            self.toastLabel.alpha = 1
            self.templateBar.alpha = 0
        }
        let work = DispatchWorkItem { [weak self] in
            guard let self else { return }
            UIView.animate(withDuration: 0.2, animations: {
                self.toastLabel.alpha = 0
                self.templateBar.alpha = 1
            }, completion: { _ in
                self.toastLabel.isHidden = true
            })
        }
        toastWorkItem = work
        // Long enough to read a two-line sentence in Kazakh or Russian.
        DispatchQueue.main.asyncAfter(deadline: .now() + 3.2, execute: work)
    }

    private func cancelToast() {
        toastWorkItem?.cancel()
        toastWorkItem = nil
        toastLabel.isHidden = true
        toastLabel.alpha = 0
        templateBar.alpha = 1
    }
}

// MARK: - Personas

extension KeyboardActionBar: TemplateBarViewDelegate {

    func templateBar(_ bar: TemplateBarView, didSelectTemplateID id: String) {
        delegate?.actionBar(self, didSelectTemplateID: id)
    }

    func templateBarDidRequestNewTemplate(_ bar: TemplateBarView) {
        delegate?.actionBarDidRequestNewTemplate(self)
    }
}

// MARK: - Composer

extension KeyboardActionBar: ReplyComposerViewDelegate {

    private func send(_ event: ComposerEvent) {
        delegate?.actionBar(self, didSend: event)
    }

    func composerDidTapClose(_ composer: ReplyComposerView) { send(.close) }
    func composerDidTapPersona(_ composer: ReplyComposerView) { send(.changePersona) }
    func composerDidTapPaste(_ composer: ReplyComposerView) { send(.paste) }
    func composerDidTapGenerate(_ composer: ReplyComposerView) { send(.generate) }
    func composerDidTapStop(_ composer: ReplyComposerView) { send(.stop) }
    func composerDidTapRegenerate(_ composer: ReplyComposerView) { send(.regenerate) }
    func composerDidTapBack(_ composer: ReplyComposerView) { send(.back) }
    func composerDidTapEdit(_ composer: ReplyComposerView) { send(.toggleEditing) }
    func composerDidTapReply(_ composer: ReplyComposerView) { send(.tapReply) }
    func composerDidTapInsert(_ composer: ReplyComposerView) { send(.insert) }
    func composerDidTapPreviousVersion(_ composer: ReplyComposerView) { send(.previousVersion) }
    func composerDidTapNextVersion(_ composer: ReplyComposerView) { send(.nextVersion) }

    func composer(_ composer: ReplyComposerView, didResolveConflictWith choice: ReplyComposerFlow.ConflictChoice) {
        send(.resolveConflict(choice))
    }

    func composer(_ composer: ReplyComposerView, didEdit field: ReplyComposerView.Field, text: String) {
        send(.edited(field, text))
    }

    func composerDidChangeHeight(_ composer: ReplyComposerView) {
        guard isComposing else { return }
        delegate?.actionBarDidChangeHeight(self)
    }
}
