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
    /// Create mode: the same instruction, as a reply to the copied message.
    case replyToCopied
    /// Send a report about the text on screen; the text itself only when the
    /// user left "send the text" on.
    case report(AIReport.Reason, text: String?)
    /// The report panel opened or closed.
    case reportingChanged
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
    /// The Apple Watch control in the row.
    func actionBarDidTapWatch(_ bar: KeyboardActionBar)
    /// Generate or Insert Response on the Watch Voice strip.
    func actionBarDidTapWatchPrimary(_ bar: KeyboardActionBar)
    func actionBarDidDismissWatchStrip(_ bar: KeyboardActionBar)
    /// The text on the strip: review a saved reply before inserting it.
    func actionBarDidTapWatchPreview(_ bar: KeyboardActionBar)
    func actionBarDidTapVoiceAssistant(_ bar: KeyboardActionBar)
    func actionBarDidReviewVoiceAssistant(_ bar: KeyboardActionBar)
    func actionBarDidInsertVoiceAssistant(_ bar: KeyboardActionBar)
    func actionBarDidDismissVoiceAssistant(_ bar: KeyboardActionBar)
    func actionBar(_ bar: KeyboardActionBar, didChooseVoiceAction action: KeyboardVoiceAssistantAction)
}

/// The area above the keys. Two shapes:
///
/// * PERSONAS - a 44pt row: ✨ AI | Дос | Клиент | Әріптес | Серіктес. This
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
    private let watchStrip = WatchVoiceStripView()
    private let voiceStrip = VoiceAssistantStripView()
    private var voiceSnapshot: VoiceAssistantSnapshot?
    private var voiceInsertsDirectly = false
    private var voiceTheme = KeyboardTheme(isDark: true)
    private var voiceLanguage = AppLanguage.english
    private var voiceControlsVisible = false
    private var voiceNotice: String?
    private var voiceNoticeReset: DispatchWorkItem?
    private let composer = ReplyComposerView()
    private var watchUI = WatchVoiceUI()
    private var watchStrings = WatchVoiceStrings.forLanguage(.english)

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
        watchStrip.isHidden = true
        watchStrip.onPrimary = { [weak self] in
            guard let self else { return }
            self.delegate?.actionBarDidTapWatchPrimary(self)
        }
        watchStrip.onDismiss = { [weak self] in
            guard let self else { return }
            self.delegate?.actionBarDidDismissWatchStrip(self)
        }
        watchStrip.onPreview = { [weak self] in
            guard let self else { return }
            self.delegate?.actionBarDidTapWatchPreview(self)
        }
        addSubview(watchStrip)
        voiceStrip.isHidden = true
        voiceStrip.onReview = { [weak self] in
            guard let self else { return }
            self.delegate?.actionBarDidReviewVoiceAssistant(self)
        }
        voiceStrip.onInsert = { [weak self] in
            guard let self else { return }
            self.delegate?.actionBarDidInsertVoiceAssistant(self)
        }
        voiceStrip.onDismiss = { [weak self] in
            guard let self else { return }
            if self.dismissVoiceControls() { return }
            self.delegate?.actionBarDidDismissVoiceAssistant(self)
        }
        voiceStrip.onAction = { [weak self] action in
            guard let self else { return }
            _ = self.dismissVoiceControls()
            self.delegate?.actionBar(self, didChooseVoiceAction: action)
        }
        addSubview(voiceStrip)
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
        // The strips use the space between the visible row controls.
        suggestionStrip.frame = templateBar.personasFrame.offsetBy(dx: templateBar.frame.minX, dy: templateBar.frame.minY)
        watchStrip.frame = suggestionStrip.frame.insetBy(dx: 3, dy: 4)
        voiceStrip.frame = watchStrip.frame
        composer.frame = bounds
    }

    // MARK: Configuration

    /// This whole area is product UI, so it follows the APP language.
    func configure(theme: KeyboardTheme, uiLanguage: AppLanguage) {
        templateBar.configure(theme: theme, uiLanguage: uiLanguage)
        suggestionStrip.configure(theme: theme, uiLanguage: uiLanguage)
        watchStrip.configure(theme: theme, uiLanguage: uiLanguage)
        watchStrings = WatchVoiceStrings.forLanguage(uiLanguage)
        voiceTheme = theme
        voiceLanguage = uiLanguage
        setWatchVoice(watchUI)
        setVoiceAssistant(voiceSnapshot, insertsDirectly: voiceInsertsDirectly)
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
        refreshWatchStrip()
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
        refreshWatchStrip()
    }

    // MARK: Watch Voice

    /// The control in the row and, when there is something to say, the strip
    /// over the personas. Nothing here changes the keyboard's height.
    func setWatchVoice(_ ui: WatchVoiceUI) {
        watchUI = ui
        if templateBar.setWatchIndicator(ui.indicator, name: watchStrings.controlName, status: watchStatus(ui.indicator)) {
            // The personas got narrower or wider: the strips follow at once.
            setNeedsLayout()
            layoutIfNeeded()
        }
        if let strip = ui.strip { watchStrip.show(strip) }
        refreshWatchStrip()
    }

    private func watchStatus(_ indicator: WatchKeyboardIndicator) -> String? {
        switch indicator {
        case .hidden, .ready:        return nil
        case .installApp:            return watchStrings.installWatchApp
        case .openWatchApp:          return watchStrings.openWatchApp
        case .waiting:               return watchStrings.waiting
        case .instructionReceived:   return watchStrings.instructionReceived
        case .generating:            return watchStrings.generating
        case .responseReady:         return watchStrings.responseReady
        case .connectionLost:        return watchStrings.connectionLost
        }
    }

    /// A word being typed has the row to itself; the strip comes back after.
    private var showsWatchStrip: Bool {
        watchUI.strip != nil && !isComposing && !showsSuggestions
    }

    private func refreshWatchStrip() {
        let voiceInProgress = voiceSnapshot?.phase.isInProgress == true && !isComposing
        let showsVoice = (voiceSnapshot != nil || voiceNotice != nil || voiceControlsVisible)
            && !isComposing && (voiceInProgress || !showsSuggestions)
        let visible = showsWatchStrip && !showsVoice
        suggestionStrip.isHidden = voiceInProgress
        let createChanged = templateBar.setCreateHidden(voiceInProgress)
        let watchChanged = templateBar.setWatchSuppressed(showsVoice)
        if createChanged || watchChanged {
            setNeedsLayout()
            layoutIfNeeded()
        }
        watchStrip.isHidden = !visible
        voiceStrip.isHidden = !showsVoice
        templateBar.setPersonasHidden(visible || showsVoice || showsSuggestions)
    }

    /// - Parameter insertsDirectly: the result was requested from the field
    ///   on screen; any other result opens for review before insertion.
    func setVoiceAssistant(_ snapshot: VoiceAssistantSnapshot?, insertsDirectly: Bool = false) {
        // Defensive gating also covers a snapshot delivered just as it
        // expires. A meaningful failure notice lasts at most ten seconds;
        // inactive sessions never replace persona controls.
        let snapshot = snapshot?.isVisibleInKeyboard() == true ? snapshot : nil
        // The microphone menu stays open while the user speaks (listening →
        // speech detected); it closes when recording ends or the session changes.
        if snapshot?.sessionID != voiceSnapshot?.sessionID
            || snapshot?.phase.isRecording != voiceSnapshot?.phase.isRecording {
            voiceControlsVisible = false
        }
        voiceSnapshot = snapshot
        voiceInsertsDirectly = insertsDirectly
        let strings = KeyboardVoiceStrings(voiceLanguage)
        templateBar.setMicrophoneLabel(strings.menu, hint: strings.menuHint)
        if templateBar.setMicrophone(visible: snapshot?.showsMicrophoneInKeyboard() == true,
                                    active: snapshot?.phase.isRecording == true,
                                    status: snapshot.map { strings.status($0.phase) }) {
            setNeedsLayout()
            layoutIfNeeded()
        }
        renderVoiceStrip()
        refreshWatchStrip()
    }

    func showVoiceControls(_ snapshot: VoiceAssistantSnapshot) {
        guard snapshot.showsMicrophoneInKeyboard(), !isComposing else { return }
        voiceNoticeReset?.cancel()
        voiceNotice = nil
        // The microphone button toggles its menu.
        voiceControlsVisible.toggle()
        renderVoiceStrip()
        refreshWatchStrip()
    }

    /// A short inline message in place of the strip; ✕ closes it sooner.
    func showVoiceNotice(_ message: String, duration: TimeInterval = 6) {
        voiceNoticeReset?.cancel()
        voiceControlsVisible = false
        voiceNotice = message
        renderVoiceStrip()
        refreshWatchStrip()
        let reset = DispatchWorkItem { [weak self] in
            guard let self else { return }
            self.voiceNotice = nil
            self.renderVoiceStrip()
            self.refreshWatchStrip()
        }
        voiceNoticeReset = reset
        DispatchQueue.main.asyncAfter(deadline: .now() + duration, execute: reset)
    }

    @discardableResult
    func dismissVoiceControls() -> Bool {
        guard voiceControlsVisible || voiceNotice != nil else { return false }
        voiceNoticeReset?.cancel()
        voiceControlsVisible = false
        voiceNotice = nil
        renderVoiceStrip()
        refreshWatchStrip()
        return true
    }

    private func renderVoiceStrip() {
        if let voiceNotice {
            voiceStrip.showNotice(voiceNotice, theme: voiceTheme, language: voiceLanguage)
        } else if let voiceSnapshot {
            if voiceControlsVisible {
                voiceStrip.showControls(snapshot: voiceSnapshot, theme: voiceTheme, language: voiceLanguage)
            } else {
                voiceStrip.configure(snapshot: voiceSnapshot, theme: voiceTheme, language: voiceLanguage,
                                     insertsDirectly: voiceInsertsDirectly)
            }
        }
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
    /// The report panel is open over a result: the keys type nothing.
    var isReporting: Bool { isComposing && composer.isReporting }
    /// How the report went: thanks, or the panel stays for another try.
    func reportDidFinish(sent: Bool) { composer.reportDidFinish(sent: sent) }

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
            self.templateBar.setPersonasHidden(visible || self.showsWatchStrip)
        }
        if animated, window != nil {
            UIView.animate(withDuration: 0.16, delay: 0, options: [.beginFromCurrentState, .allowUserInteraction], animations: apply)
        } else {
            apply()
        }
        refreshWatchStrip()
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

    func templateBarDidTapWatch(_ bar: TemplateBarView) {
        delegate?.actionBarDidTapWatch(self)
    }

    func templateBarDidTapVoiceAssistant(_ bar: TemplateBarView) {
        delegate?.actionBarDidTapVoiceAssistant(self)
    }
}

// MARK: - Composer

extension KeyboardActionBar: ReplyComposerViewDelegate {

    private func send(_ event: ComposerEvent) {
        delegate?.actionBar(self, didSend: event)
    }

    func composerDidTapClose(_ composer: ReplyComposerView) { send(.close) }
    func composerDidTapNew(_ composer: ReplyComposerView) { send(.reset) }
    func composerDidTapReplyToCopied(_ composer: ReplyComposerView) { send(.replyToCopied) }
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

    func composer(_ composer: ReplyComposerView, didReport reason: AIReport.Reason, text: String?) {
        send(.report(reason, text: text))
    }

    func composerDidChangeReporting(_ composer: ReplyComposerView) {
        guard isComposing else { return }
        send(.reportingChanged)
    }
}
