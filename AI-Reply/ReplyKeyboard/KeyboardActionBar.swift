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
    /// "Create" on the persona row: write a new message with AI.
    func actionBarDidRequestCompose(_ bar: KeyboardActionBar)
    func actionBar(_ bar: KeyboardActionBar, didSend event: ComposerEvent)
    func actionBarDidChangeHeight(_ bar: KeyboardActionBar)
    /// A word of the suggestion strip - above the keys or under the instruction.
    func actionBar(_ bar: KeyboardActionBar, didPick suggestion: AutocorrectSuggestion)
    /// The suggested version of the instruction was tapped.
    func actionBarDidAcceptPolish(_ bar: KeyboardActionBar)
    func actionBarDidUndoPolish(_ bar: KeyboardActionBar)
    /// The composer's caret moved without typing: a tap, another field.
    func actionBarDidMoveComposerCaret(_ bar: KeyboardActionBar)
}

/// The area above the keys. Two shapes:
///
/// * PERSONAS - a 36pt row: ✨ Create | Дос | Клиент | Бизнес | Жұмыс. This
///   is the keyboard at rest; its height is what gets cached for the next
///   launch. While a word is typed into the host app the suggestion strip
///   takes the pills' place - only theirs: ✨ stays exactly where it is.
/// * COMPOSER - the AI composer (`ReplyComposerView`), answering a copied
///   message or, after Create, writing a new one.
///
/// Neither ever takes height from the keys: the keyboard grows instead, up to
/// the ceiling the controller hands down.
final class KeyboardActionBar: UIView {

    weak var delegate: KeyboardActionBarDelegate?

    private let templateBar = TemplateBarView()
    private let suggestionStrip = SuggestionStripView()
    private let composer = ReplyComposerView()

    /// Whether the strip is over the persona pills right now.
    private var showsSuggestions = false

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
        // Exactly the pills' viewport, right of ✨: the strip never covers it.
        suggestionStrip.frame = templateBar.personasFrame.offsetBy(dx: templateBar.frame.minX, dy: templateBar.frame.minY)
        composer.frame = bounds
    }

    // MARK: Configuration

    /// This whole area is product UI, so it follows the APP language.
    func configure(theme: KeyboardTheme, uiLanguage: AppLanguage) {
        templateBar.configure(theme: theme, uiLanguage: uiLanguage)
        suggestionStrip.configure(theme: theme, uiLanguage: uiLanguage)
        composer.configure(theme: theme, uiLanguage: uiLanguage)
    }

    func setChips(_ chips: [TemplateChip], selectedID: String?) {
        templateBar.setChips(chips, selectedID: selectedID)
    }

    func layout(forWidth width: CGFloat) {
        composer.layout(forWidth: width)
    }

    func setMaximumComposerHeight(_ height: CGFloat) {
        composer.setMaximumHeight(height)
    }

    // MARK: Composer

    func beginComposing() {
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
    /// The character right after the composer's caret; nil at the end of the
    /// text or when the composer is closed.
    var textAfterCursor: String? { isComposing ? composer.textAfterCursor : nil }
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

    /// Crossfades the strip and the persona pills; ✨ is not touched.
    private func setHostSuggestionsVisible(_ visible: Bool, animated: Bool) {
        guard visible != showsSuggestions else { return }
        showsSuggestions = visible
        suggestionStrip.isUserInteractionEnabled = visible
        suggestionStrip.accessibilityElementsHidden = !visible
        let apply = {
            self.suggestionStrip.alpha = visible ? 1 : 0
            self.templateBar.setPersonasHidden(visible)
        }
        if animated, window != nil {
            UIView.animate(withDuration: 0.16, delay: 0, options: [.beginFromCurrentState, .allowUserInteraction], animations: apply)
        } else {
            apply()
        }
    }
}

// MARK: - Personas

extension KeyboardActionBar: TemplateBarViewDelegate {

    func templateBar(_ bar: TemplateBarView, didSelectTemplateID id: String) {
        delegate?.actionBar(self, didSelectTemplateID: id)
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

    func composerDidMoveCaret(_ composer: ReplyComposerView) {
        guard isComposing else { return }
        delegate?.actionBarDidMoveComposerCaret(self)
    }
}
