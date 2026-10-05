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
    /// Create mode: start over with an empty instruction.
    case reset
}

protocol KeyboardActionBarDelegate: AnyObject {
    func actionBar(_ bar: KeyboardActionBar, didSelectTemplateID id: String)
    func actionBarDidRequestNewTemplate(_ bar: KeyboardActionBar)
    /// "Create" on the persona row: write a new message with AI.
    func actionBarDidRequestCompose(_ bar: KeyboardActionBar)
    func actionBar(_ bar: KeyboardActionBar, didSend event: ComposerEvent)
    func actionBarDidChangeHeight(_ bar: KeyboardActionBar)
    /// A word of the suggestion strip - above the keys or under the instruction.
    func actionBar(_ bar: KeyboardActionBar, didPick suggestion: AutocorrectSuggestion)
    /// The suggested version of the instruction was tapped.
    func actionBarDidAcceptPolish(_ bar: KeyboardActionBar)
    func actionBarDidUndoPolish(_ bar: KeyboardActionBar)
}

/// The area above the keys. Two shapes:
///
/// * PERSONAS - a 36pt row: Дос | Клиент | Бизнес | Жұмыс | + | ✨ Create,
///   plus a transient status line that changes no geometry. This is the
///   keyboard at rest; its height is what gets cached for the next launch.
///   While a word is typed into the host app the suggestion strip takes the
///   pills' place - only theirs: + and ✨ stay exactly where they are.
/// * COMPOSER - the AI composer (`ReplyComposerView`), answering a copied
///   message or, after Create, writing a new one.
///
/// Neither ever takes height from the keys: the keyboard grows instead, up to
/// the ceiling the controller hands down.
final class KeyboardActionBar: UIView {

    weak var delegate: KeyboardActionBarDelegate?

    private let templateBar = TemplateBarView()
    private let suggestionStrip = SuggestionStripView()
    private let toastLabel = UILabel()
    private let composer = ReplyComposerView()

    /// Whether the strip is over the persona pills right now.
    private var showsSuggestions = false

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
        suggestionStrip.alpha = 0
        suggestionStrip.isUserInteractionEnabled = false
        suggestionStrip.accessibilityElementsHidden = true
        suggestionStrip.onPick = { [weak self] suggestion in
            guard let self else { return }
            self.delegate?.actionBar(self, didPick: suggestion)
        }
        addSubview(suggestionStrip)
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
        templateBar.layoutIfNeeded()
        suggestionStrip.frame = templateBar.personasFrame.offsetBy(dx: templateBar.frame.minX, dy: templateBar.frame.minY)
        toastLabel.frame = CGRect(x: 14, y: 1, width: max(0, bounds.width - 28), height: TemplateBarView.preferredHeight + 2)
        composer.frame = bounds
    }

    // MARK: Configuration

    /// This whole area is product UI, so it follows the APP language.
    func configure(theme: KeyboardTheme, uiLanguage: AppLanguage) {
        toastLabel.textColor = theme.secondaryText
        templateBar.configure(theme: theme, uiLanguage: uiLanguage)
        suggestionStrip.configure(theme: theme, uiLanguage: uiLanguage)
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
        setHostSuggestionsVisible(false, animated: false)
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
    @discardableResult
    func replaceBeforeCursor(length: Int, with text: String) -> Bool { composer.replaceBeforeCaret(length: length, with: text) }
    func replaceInstruction(with text: String) { composer.replaceInstruction(with: text) }
    var instructionText: String { composer.instructionText }
    /// The composer field being typed into is one smart correction looks after.
    var correctsFocusedField: Bool { isComposing && composer.focusedFieldAcceptsCorrection }
    var isEditingInstruction: Bool { isComposing && composer.isEditingInstruction }

    // MARK: Suggestions

    /// The strip for the word being typed: over the persona pills at rest,
    /// under the instruction in the composer. Empty hides it.
    func showSuggestions(_ items: [AutocorrectSuggestion]) {
        if isComposing {
            composer.showSuggestions(items)
            return
        }
        suggestionStrip.show(items)
        setHostSuggestionsVisible(!items.isEmpty, animated: true)
    }

    /// The suggested version of the instruction, in the composer.
    func showPolish(_ chip: InstructionPolisher.Chip) {
        composer.showPolish(chip)
    }

    /// Crossfades the strip and the persona pills, as the status line does.
    private func setHostSuggestionsVisible(_ visible: Bool, animated: Bool) {
        guard visible != showsSuggestions else { return }
        showsSuggestions = visible
        suggestionStrip.isUserInteractionEnabled = visible
        suggestionStrip.accessibilityElementsHidden = !visible
        let apply = {
            self.suggestionStrip.alpha = visible && self.toastWorkItem == nil ? 1 : 0
            self.templateBar.setPersonasHidden(visible)
        }
        if animated, window != nil {
            UIView.animate(withDuration: 0.16, delay: 0, options: [.beginFromCurrentState, .allowUserInteraction], animations: apply)
        } else {
            apply()
        }
    }

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
            self.suggestionStrip.alpha = 0
        }
        let work = DispatchWorkItem { [weak self] in
            guard let self else { return }
            self.toastWorkItem = nil
            UIView.animate(withDuration: 0.2, animations: {
                self.toastLabel.alpha = 0
                self.templateBar.alpha = 1
                self.suggestionStrip.alpha = self.showsSuggestions ? 1 : 0
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
        suggestionStrip.alpha = showsSuggestions ? 1 : 0
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

    func templateBarDidTapCreate(_ bar: TemplateBarView) {
        delegate?.actionBarDidRequestCompose(self)
    }
}

// MARK: - Composer

extension KeyboardActionBar: ReplyComposerViewDelegate {

    private func send(_ event: ComposerEvent) {
        delegate?.actionBar(self, didSend: event)
    }

    func composerDidTapClose(_ composer: ReplyComposerView) { send(.close) }
    func composerDidTapNew(_ composer: ReplyComposerView) { send(.reset) }
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

    func composer(_ composer: ReplyComposerView, didPick suggestion: AutocorrectSuggestion) {
        delegate?.actionBar(self, didPick: suggestion)
    }

    func composerDidAcceptPolish(_ composer: ReplyComposerView) {
        delegate?.actionBarDidAcceptPolish(self)
    }

    func composerDidUndoPolish(_ composer: ReplyComposerView) {
        delegate?.actionBarDidUndoPolish(self)
    }
}
