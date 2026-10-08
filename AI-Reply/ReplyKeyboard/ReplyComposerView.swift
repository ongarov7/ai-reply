import UIKit

protocol ReplyComposerViewDelegate: AnyObject {
    /// Discard everything and go back to the persona row.
    func composerDidTapClose(_ composer: ReplyComposerView)
    /// Keep the session and show the persona row, to pick another persona.
    func composerDidTapPersona(_ composer: ReplyComposerView)
    /// Explicit, user-initiated clipboard read.
    func composerDidTapPaste(_ composer: ReplyComposerView)
    /// Generate, or Retry after a failure.
    func composerDidTapGenerate(_ composer: ReplyComposerView)
    func composerDidTapStop(_ composer: ReplyComposerView)
    func composerDidTapRegenerate(_ composer: ReplyComposerView)
    func composerDidTapBack(_ composer: ReplyComposerView)
    /// Edit / Done on the reply.
    func composerDidTapEdit(_ composer: ReplyComposerView)
    func composerDidTapInsert(_ composer: ReplyComposerView)
    func composerDidTapPreviousVersion(_ composer: ReplyComposerView)
    func composerDidTapNextVersion(_ composer: ReplyComposerView)
    func composer(_ composer: ReplyComposerView, didResolveConflictWith choice: ReplyComposerFlow.ConflictChoice)
    /// The user tapped into the reply while it was read-only.
    func composerDidTapReply(_ composer: ReplyComposerView)
    /// One of the three texts changed through the keys or a quick intent.
    func composer(_ composer: ReplyComposerView, didEdit field: ReplyComposerView.Field, text: String)
    func composerDidChangeHeight(_ composer: ReplyComposerView)
    /// Create mode: discard the instruction and every version.
    func composerDidTapNew(_ composer: ReplyComposerView)
    /// Create mode: reply to the copied message with this instruction.
    func composerDidTapReplyToCopied(_ composer: ReplyComposerView)
    /// A word of the suggestion strip under the instruction.
    func composer(_ composer: ReplyComposerView, didPick suggestion: AutocorrectSuggestion)
    /// The suggested version of the instruction was tapped.
    func composerDidAcceptPolish(_ composer: ReplyComposerView)
    /// Undo after a suggested version was used.
    func composerDidUndoPolish(_ composer: ReplyComposerView)
    /// The caret moved without typing - a tap in a field, another field
    /// focused, a quick intent: the word suggestions belong to the old place.
    func composerDidMoveCaret(_ composer: ReplyComposerView)
    /// Send a report about the reply on screen; `text` is nil when the user
    /// switched "send the text" off. Answer with `reportDidFinish(sent:)`.
    func composer(_ composer: ReplyComposerView, didReport reason: AIReport.Reason, text: String?)
    /// The report panel opened or closed: the keys type nothing while it is open.
    func composerDidChangeReporting(_ composer: ReplyComposerView)
}

/// The AI reply composer.
///
/// THE ONE IDEA THIS VIEW EXISTS TO EXPRESS:
///
///     SOURCE MESSAGE  !=  REPLY INSTRUCTION  !=  GENERATED REPLY
///
/// * the SOURCE is what the other person wrote. It sits in the header as one
///   quiet line; a tap opens the whole message.
/// * the INSTRUCTION is what THIS user wants said. It is the field that looks
///   like a field, and it never becomes the reply.
/// * the REPLY is the answer - the only one of the three that can reach the
///   host app.
///
/// COMPACT BY CONSTRUCTION. Header, one field, one row of actions: about 120pt
/// while composing and 140-175pt with a reply, against ~185pt before. Heights
/// are solved here from the budget the controller hands down, never from what
/// has been typed, so the keyboard does not move while the user types.
///
/// This view RENDERS `ReplyComposerFlow` - the stage machine lives there and
/// is unit tested. What this view owns is the live text of the focused field
/// and the caret, because the keys edit those directly.
final class ReplyComposerView: UIView {

    weak var delegate: ReplyComposerViewDelegate?

    /// Which text the keys are editing.
    enum Field: Equatable {
        case none
        case source
        case instruction
        case draft
    }

    /// What the panel is for.
    ///
    /// * `reply` - answering a copied message: persona, message, instruction.
    /// * `compose` - "Create": writing a new message from a description. No
    ///   persona and no message; a title, New, a roomier instruction, and
    ///   "Reply to copied" under it, which turns the same instruction into a
    ///   reply to the copied message (the panel switches to `reply`).
    ///
    /// Same stages, same keys, same Insert; only the header, the field sizes,
    /// the quick intents and the words differ.
    enum Mode: Equatable {
        case reply
        case compose
    }

    struct Content {
        var personaName: String
        var source: String
        var instruction: String
        var flow: ReplyComposerFlow
        var errorMessage: String?
        var sourceLimit: Int
        var instructionLimit: Int
        var mode: Mode = .reply
        /// Whether the primary button turns into Retry while the error is
        /// shown. Not when the error is about the copied message: Retry
        /// would write a new message, which is not what was just tried.
        var errorOffersRetry = true
        /// The result offers Report: the server takes reports and the
        /// keyboard can reach it.
        var offersReport = false
    }

    // MARK: State

    private(set) var flow = ReplyComposerFlow()
    private(set) var mode: Mode = .reply
    private(set) var focus: Field = .instruction
    private var errorMessage: String?
    private var errorOffersRetry = true
    private var sourceLimit = AILimits.fallback.sourceCharacters
    private var instructionLimit = ReplyInstruction.maximumCharacters
    private var isSourceExpanded = false

    private var theme = KeyboardTheme(isDark: false)
    private var strings = AIReplyStrings.forLanguage(.english)
    private var layoutWidth: CGFloat = 0
    private var maximumHeight: CGFloat = 220
    private(set) var preferredHeight: CGFloat = 122

    var sourceText: String { sourceView.currentText }
    var instructionText: String { instructionView.currentText }
    var draftText: String { draftView.currentText }

    /// True while the whole message is open, which the controller allows a
    /// little more keyboard height for. It stays true through the "field is
    /// not empty" question, so the keyboard keeps its height there too.
    ///
    /// A composed message is usually longer than a reply (a congratulation
    /// runs to several lines), so its result asks for the same extra room.
    var wantsExpandedContext: Bool { isSourceExpanded || (isCompose && flow.showsReply) }

    private var isCompose: Bool { mode == .compose }

    /// The composer's height when the "field is not empty" question appeared.
    /// The question keeps it: the keys must not jump up for one question and
    /// back down after it.
    private var conflictHeight: CGFloat = 0

    /// Create only: whether the instruction was empty at the last refresh.
    /// Write and New depend on it, so the panel refreshes when it flips -
    /// not on every keystroke.
    private var instructionWasEmpty = true

    /// Views that were visible in the previous layout pass. Everything else
    /// is placed without animation - see `place(_:_:)`.
    private var visibleLastPass = Set<ObjectIdentifier>()
    private var visibleThisPass = Set<ObjectIdentifier>()

    /// What the slot left of the primary button shows.
    private enum Accessory {
        case intents
        case suggestions
        case polish
    }

    private var suggestions: [AutocorrectSuggestion] = []
    private var polish: InstructionPolisher.Chip = .none
    private var shownAccessory: Accessory = .intents
    /// A moment of ignored taps on the intents and the chip after the chip
    /// changed in their shared place.
    private var slotTapGuard = PolishSlotTapGuard()

    /// The report panel, while it is open over a result.
    private struct ReportPanel: Equatable {
        enum Phase: Equatable {
            case choosing
            case sending
            case sent
            case failed
        }

        var reason: AIReport.Reason?
        var includesText = true
        var phase: Phase = .choosing
        /// The composer's height when the panel opened. The panel keeps it,
        /// like the "field is not empty" question, and only grows when its
        /// own rows need more.
        var height: CGFloat
    }

    private var reportPanel: ReportPanel?
    private var offersReport = false
    private var reportCloseTimer: DispatchWorkItem?

    /// Whether the report panel is open: the keys type nothing meanwhile.
    var isReporting: Bool { reportPanel != nil }

    // MARK: Views

    private let panel = UIView()

    // Header
    private let personaChip = UIButton(type: .system)
    private let previewButton = UIButton(type: .system)
    private let counterLabel = UILabel()
    private let closeButton = CircleIconButton(symbol: "xmark", pointSize: 12)

    // Create header
    private let titleIcon = UIImageView()
    private let titleLabel = UILabel()
    private let newButton = UIButton(type: .system)
    /// Create: "Reply to copied", on its own line under the instruction (as
    /// on Android). The reply header's message-preview button, configured
    /// the same way, with a reply arrow in front.
    private let copiedButton = UIButton(type: .system)

    // Full message
    private let sourceCard = UIView()
    private let quoteBar = UIView()
    private let sourceView = ComposerTextView(font: .systemFont(ofSize: 14), inset: UIEdgeInsets(top: 5, left: 0, bottom: 5, right: 0))
    private let collapseButton = CircleIconButton(symbol: "chevron.up", pointSize: 11)
    private let pasteButton = CircleIconButton(symbol: "doc.on.clipboard", pointSize: 11)
    private let clearButton = CircleIconButton(symbol: "xmark.circle.fill", pointSize: 15, filled: false)

    // Instruction / reply
    private let instructionView = ComposerTextView(font: .systemFont(ofSize: 15.5), inset: UIEdgeInsets(top: 8, left: 10, bottom: 8, right: 10))
    private let draftView = ComposerTextView(font: .systemFont(ofSize: 16), inset: UIEdgeInsets(top: 7, left: 10, bottom: 7, right: 10))

    // Status
    private let errorLabel = UILabel()
    private let conflictLabel = UILabel()

    // Composing row
    private let quickActions = QuickActionRow()
    private let primaryButton = UIButton(type: .system)
    /// In the quick intents' place while a word of the instruction is being
    /// typed, or when a cleaner version of it is on offer.
    private let suggestionStrip = SuggestionStripView()
    private let polishChip = PolishChipView()

    // Result row
    private let backButton = CircleIconButton(symbol: "chevron.left", pointSize: 14)
    private let regenerateButton = CircleIconButton(symbol: "arrow.clockwise", pointSize: 13)
    private let editButton = CircleIconButton(symbol: "pencil", pointSize: 13)
    private let previousButton = CircleIconButton(symbol: "chevron.left", pointSize: 12, filled: false)
    private let versionLabel = UILabel()
    private let nextButton = CircleIconButton(symbol: "chevron.right", pointSize: 12, filled: false)
    private let insertButton = UIButton(type: .system)

    private var iconButtons: [CircleIconButton] {
        [closeButton, collapseButton, pasteButton, clearButton, backButton, regenerateButton,
         editButton, previousButton, nextButton, reportButton]
    }

    // Conflict row
    private let replaceButton = UIButton(type: .system)
    private let appendButton = UIButton(type: .system)
    private let conflictCancelButton = UIButton(type: .system)

    // Report: a small flag at the end of the header's flexible part, and the
    // panel it opens in the reply's place - reasons, "send the text", Send.
    // An inline panel, because a keyboard cannot present a sheet.
    private let reportButton = CircleIconButton(symbol: "flag", pointSize: 11, filled: false)
    private let reasonButtons: [(reason: AIReport.Reason, button: UIButton)] =
        AIReport.Reason.allCases.map { ($0, UIButton(type: .system)) }
    private let includeTextSwitch = UISwitch()
    private let includeTextLabel = UILabel()
    private let reportStatusLabel = UILabel()
    private let reportCancelButton = UIButton(type: .system)
    private let reportSendButton = UIButton(type: .system)
    private let reasonFont = UIFont.systemFont(ofSize: 12.5, weight: .medium)

    private var reportViews: [UIView] {
        reasonButtons.map(\.button) + [includeTextSwitch, includeTextLabel, reportStatusLabel,
                                        reportCancelButton, reportSendButton]
    }

    // MARK: Metrics

    private let outerInset: CGFloat = 5
    private let innerInset: CGFloat = 8
    private let gap: CGFloat = 6
    private let headerHeight: CGFloat = 30
    /// The message preview's height in the header, and "Reply to copied"'s.
    private var previewHeight: CGFloat { headerHeight - 4 }
    private let rowHeight: CGFloat = 32
    private let iconSize: CGFloat = 32
    private let reasonHeight: CGFloat = 28
    /// A switch at its natural size, and its label beside it.
    private let includeTextHeight: CGFloat = 31

    // MARK: Init

    override init(frame: CGRect) {
        super.init(frame: frame)
        build()
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not used")
    }

    private func build() {
        panel.layer.cornerRadius = 12
        panel.layer.cornerCurve = .continuous
        addSubview(panel)

        configurePill(personaChip, symbol: "chevron.down", trailingImage: true)
        personaChip.addTarget(self, action: #selector(personaTapped), for: .touchUpInside)

        configurePreview(previewButton)
        previewButton.addTarget(self, action: #selector(previewTapped), for: .touchUpInside)
        configurePreview(copiedButton)
        copiedButton.configuration?.imagePlacement = .leading
        copiedButton.addTarget(self, action: #selector(copiedTapped), for: .touchUpInside)

        counterLabel.font = .monospacedDigitSystemFont(ofSize: 11, weight: .medium)
        counterLabel.textAlignment = .right

        closeButton.addTarget(self, action: #selector(closeTapped), for: .touchUpInside)

        titleIcon.image = UIImage(systemName: "sparkles",
                                  withConfiguration: UIImage.SymbolConfiguration(pointSize: 13, weight: .semibold))
        titleIcon.contentMode = .center
        titleLabel.font = .systemFont(ofSize: 14, weight: .semibold)
        titleLabel.adjustsFontSizeToFitWidth = true
        titleLabel.minimumScaleFactor = 0.85
        titleLabel.accessibilityTraits = .header
        configurePill(newButton, symbol: "square.and.pencil", trailingImage: false)
        newButton.addTarget(self, action: #selector(newTapped), for: .touchUpInside)

        sourceCard.layer.cornerRadius = 9
        sourceCard.layer.cornerCurve = .continuous
        quoteBar.layer.cornerRadius = 1.5
        sourceCard.addSubview(quoteBar)
        sourceCard.addSubview(sourceView)
        collapseButton.addTarget(self, action: #selector(collapseTapped), for: .touchUpInside)
        pasteButton.addTarget(self, action: #selector(pasteTapped), for: .touchUpInside)
        clearButton.addTarget(self, action: #selector(clearSourceTapped), for: .touchUpInside)
        [collapseButton, pasteButton, clearButton].forEach(sourceCard.addSubview)

        instructionView.layer.cornerRadius = 9
        instructionView.layer.cornerCurve = .continuous
        instructionView.layer.borderWidth = 1
        draftView.layer.cornerRadius = 9
        draftView.layer.cornerCurve = .continuous
        draftView.layer.borderWidth = 1

        attachTap(to: sourceView, action: #selector(sourceTapped(_:)))
        attachTap(to: instructionView, action: #selector(instructionTapped(_:)))
        attachTap(to: draftView, action: #selector(draftTapped(_:)))

        errorLabel.font = .systemFont(ofSize: 12, weight: .medium)
        errorLabel.numberOfLines = 2
        errorLabel.adjustsFontSizeToFitWidth = true
        errorLabel.minimumScaleFactor = 0.8

        conflictLabel.font = .systemFont(ofSize: 14, weight: .medium)
        conflictLabel.numberOfLines = 3
        conflictLabel.textAlignment = .center
        conflictLabel.adjustsFontSizeToFitWidth = true
        conflictLabel.minimumScaleFactor = 0.8

        quickActions.delegate = self
        suggestionStrip.onPick = { [weak self] suggestion in
            guard let self else { return }
            self.delegate?.composer(self, didPick: suggestion)
        }
        polishChip.addTarget(self, action: #selector(polishTapped), for: .touchUpInside)

        configurePill(primaryButton, symbol: nil, trailingImage: false)
        primaryButton.addTarget(self, action: #selector(primaryTapped), for: .touchUpInside)

        backButton.addTarget(self, action: #selector(backTapped), for: .touchUpInside)
        regenerateButton.addTarget(self, action: #selector(regenerateTapped), for: .touchUpInside)
        editButton.addTarget(self, action: #selector(editTapped), for: .touchUpInside)
        previousButton.addTarget(self, action: #selector(previousTapped), for: .touchUpInside)
        nextButton.addTarget(self, action: #selector(nextTapped), for: .touchUpInside)
        versionLabel.font = .monospacedDigitSystemFont(ofSize: 12, weight: .semibold)
        versionLabel.textAlignment = .center

        configurePill(insertButton, symbol: nil, trailingImage: false)
        insertButton.addTarget(self, action: #selector(insertTapped), for: .touchUpInside)

        for (button, action) in [
            (replaceButton, #selector(replaceTapped)),
            (appendButton, #selector(appendTapped)),
            (conflictCancelButton, #selector(conflictCancelTapped))
        ] {
            configurePill(button, symbol: nil, trailingImage: false)
            button.addTarget(self, action: action, for: .touchUpInside)
        }

        reportButton.addTarget(self, action: #selector(reportTapped), for: .touchUpInside)
        for (_, button) in reasonButtons {
            configurePill(button, symbol: nil, trailingImage: false)
            button.addTarget(self, action: #selector(reasonTapped(_:)), for: .touchUpInside)
        }
        includeTextSwitch.addTarget(self, action: #selector(includeTextChanged), for: .valueChanged)
        includeTextLabel.font = .systemFont(ofSize: 13, weight: .regular)
        includeTextLabel.numberOfLines = 2
        includeTextLabel.adjustsFontSizeToFitWidth = true
        includeTextLabel.minimumScaleFactor = 0.8
        includeTextLabel.isUserInteractionEnabled = true
        includeTextLabel.addGestureRecognizer(UITapGestureRecognizer(target: self, action: #selector(includeTextLabelTapped)))
        reportStatusLabel.font = .systemFont(ofSize: 12, weight: .medium)
        reportStatusLabel.numberOfLines = 2
        reportStatusLabel.adjustsFontSizeToFitWidth = true
        reportStatusLabel.minimumScaleFactor = 0.8
        configurePill(reportCancelButton, symbol: nil, trailingImage: false)
        reportCancelButton.addTarget(self, action: #selector(reportCancelTapped), for: .touchUpInside)
        configurePill(reportSendButton, symbol: nil, trailingImage: false)
        reportSendButton.addTarget(self, action: #selector(reportSendTapped), for: .touchUpInside)

        [personaChip, previewButton, counterLabel, closeButton, titleIcon, titleLabel, newButton,
         copiedButton, sourceCard, instructionView, draftView,
         errorLabel, conflictLabel, quickActions, suggestionStrip, polishChip, primaryButton, backButton,
         regenerateButton, editButton,
         previousButton, versionLabel, nextButton, insertButton, replaceButton, appendButton,
         conflictCancelButton, reportButton].forEach(panel.addSubview)
        reportViews.forEach(panel.addSubview)
    }

    /// The quoted-message look: the reply header's message preview, and
    /// Create's "Reply to copied".
    private func configurePreview(_ button: UIButton) {
        var preview = UIButton.Configuration.plain()
        preview.contentInsets = NSDirectionalEdgeInsets(top: 0, leading: 8, bottom: 0, trailing: 6)
        preview.background.cornerRadius = 8
        preview.imagePlacement = .trailing
        preview.imagePadding = 5
        preview.titleLineBreakMode = .byTruncatingTail
        button.configuration = preview
        button.contentHorizontalAlignment = .leading
    }

    private func configurePill(_ button: UIButton, symbol: String?, trailingImage: Bool) {
        var configuration = UIButton.Configuration.plain()
        configuration.contentInsets = NSDirectionalEdgeInsets(top: 0, leading: 12, bottom: 0, trailing: 12)
        configuration.background.cornerRadius = 14
        configuration.titleLineBreakMode = .byTruncatingTail
        if let symbol {
            configuration.image = UIImage(systemName: symbol,
                                          withConfiguration: UIImage.SymbolConfiguration(pointSize: 9, weight: .bold))
            configuration.imagePlacement = trailingImage ? .trailing : .leading
            configuration.imagePadding = 5
        }
        button.configuration = configuration
    }

    private func attachTap(to view: UIView, action: Selector) {
        let tap = UITapGestureRecognizer(target: self, action: action)
        view.addGestureRecognizer(tap)
    }

    // MARK: Configuration

    func configure(theme: KeyboardTheme, uiLanguage: AppLanguage) {
        self.theme = theme
        self.strings = AIReplyStrings.forLanguage(uiLanguage)
        quickActions.configure(theme: theme, strings: strings, intents: currentIntents)
        suggestionStrip.configure(theme: theme, uiLanguage: uiLanguage)
        polishChip.configure(theme: theme, uiLanguage: uiLanguage)
        applyTheme()
        refresh()
    }

    private var currentIntents: [QuickIntent] {
        isCompose ? strings.compose.intents : strings.quickIntents
    }

    func layout(forWidth width: CGFloat) {
        guard width > 0, abs(width - layoutWidth) > 0.5 else { return }
        layoutWidth = width
        remeasure()
    }

    /// The tallest the composer may become, from the controller.
    func setMaximumHeight(_ height: CGFloat) {
        guard abs(height - maximumHeight) > 0.5 else { return }
        maximumHeight = height
        remeasure()
    }

    // MARK: Rendering

    /// Shows a session. Texts are only replaced when they actually differ,
    /// so rendering never moves the caret the user just placed.
    func render(_ content: Content) {
        let previous = flow
        let wasConflict = Self.isConflictStage(previous.stage)
        if content.mode != mode {
            mode = content.mode
            isSourceExpanded = false
            quickActions.showIntents(currentIntents)
        }
        flow = content.flow
        if isConflict && !wasConflict {
            conflictHeight = preferredHeight
        }
        errorMessage = content.errorMessage
        errorOffersRetry = content.errorOffersRetry
        sourceLimit = content.sourceLimit
        instructionLimit = content.instructionLimit
        offersReport = content.offersReport
        // The report is about the reply on screen; once that is gone - a
        // new version, editing, Back - so is the panel.
        if reportPanel != nil, flow.stage != .result || !offersReport || previous.drafts != flow.drafts {
            closeReport()
        }

        applyText(content.personaName, to: personaChip, size: 13, weight: .semibold)
        personaChip.accessibilityLabel = content.personaName

        if sourceView.currentText != content.source {
            sourceView.setText(content.source)
        }
        if instructionView.currentText != content.instruction {
            instructionView.setText(content.instruction)
        }
        // A new version, or moving between versions, replaces the reply text
        // with the caret at its end.
        if draftView.currentText != flow.draftText {
            draftView.setText(flow.draftText)
        }
        if previous.drafts.count != flow.drafts.count {
            draftView.setContentOffset(.zero, animated: false)
        }

        switch flow.stage {
        case .composing:
            if focus != .source || !isSourceExpanded { focus = .instruction }
        case .editing:
            focus = .draft
        case .generating, .result, .conflict:
            focus = .none
        }
        refresh()
        remeasure()
    }

    /// Back to a clean slate when the composer closes.
    func reset() {
        flow = ReplyComposerFlow()
        if mode != .reply {
            mode = .reply
            quickActions.showIntents(currentIntents)
        }
        focus = .instruction
        errorMessage = nil
        errorOffersRetry = true
        isSourceExpanded = false
        conflictHeight = 0
        reportCloseTimer?.cancel()
        reportCloseTimer = nil
        reportPanel = nil
        suggestions = []
        polish = .none
        polishChip.show(.none)
        sourceView.setText("")
        instructionView.setText("")
        draftView.setText("")
        refresh()
        remeasure()
    }

    private var isConflict: Bool { Self.isConflictStage(flow.stage) }

    private static func isConflictStage(_ stage: ReplyComposerFlow.Stage) -> Bool {
        if case .conflict = stage { return true }
        return false
    }

    private func applyTheme() {
        panel.backgroundColor = theme.panelBackground
        personaChip.configuration?.background.backgroundColor = theme.fieldBackground
        personaChip.configuration?.baseForegroundColor = theme.primaryText
        previewButton.configuration?.background.backgroundColor = theme.quoteBackground
        previewButton.configuration?.baseForegroundColor = theme.quoteText
        previewButton.tintColor = theme.secondaryText
        copiedButton.configuration?.background.backgroundColor = theme.quoteBackground
        copiedButton.configuration?.baseForegroundColor = theme.quoteText
        copiedButton.tintColor = theme.secondaryText
        counterLabel.textColor = theme.secondaryText
        titleIcon.tintColor = theme.accent
        titleLabel.textColor = theme.primaryText
        newButton.configuration?.background.backgroundColor = theme.fieldBackground
        newButton.configuration?.baseForegroundColor = theme.primaryText

        sourceCard.backgroundColor = theme.quoteBackground
        quoteBar.backgroundColor = theme.accent.withAlphaComponent(0.75)
        for view in [sourceView, instructionView, draftView] {
            view.caretColor = theme.accent
            view.placeholderColor = theme.secondaryText
            view.indicatorStyle = theme.isDark ? .white : .black
        }
        sourceView.textColor = theme.quoteText
        instructionView.textColor = theme.primaryText
        instructionView.backgroundColor = theme.fieldBackground
        draftView.textColor = theme.primaryText
        draftView.backgroundColor = theme.fieldBackground

        errorLabel.textColor = theme.destructive
        conflictLabel.textColor = theme.primaryText
        versionLabel.textColor = theme.secondaryText

        for button in iconButtons {
            button.fillColor = theme.fieldBackground
            button.glyphColor = theme.primaryText
            button.accentColor = theme.accent
        }
        clearButton.glyphColor = theme.secondaryText
        for button in [replaceButton, appendButton] {
            button.configuration?.background.backgroundColor = theme.accent
            button.configuration?.baseForegroundColor = .white
        }
        conflictCancelButton.configuration?.background.backgroundColor = theme.fieldBackground
        conflictCancelButton.configuration?.baseForegroundColor = theme.primaryText
        reportButton.glyphColor = theme.secondaryText
        includeTextSwitch.onTintColor = theme.accent
        includeTextLabel.textColor = theme.primaryText
        reportCancelButton.configuration?.background.backgroundColor = theme.fieldBackground
        reportCancelButton.configuration?.baseForegroundColor = theme.primaryText
        reportSendButton.configuration?.baseForegroundColor = .white
        refreshBorders()
    }

    private func applyText(_ title: String, to button: UIButton, size: CGFloat, weight: UIFont.Weight) {
        var attributes = AttributeContainer()
        attributes.font = .systemFont(ofSize: size, weight: weight)
        button.configuration?.attributedTitle = AttributedString(title, attributes: attributes)
        button.accessibilityLabel = title
    }

    /// Everything that depends on the stage and the texts, but not on height.
    private func refresh() {
        let stage = flow.stage
        let composingLike = stage == .composing || flow.generationOrigin == .composing
        let replyLike = stage == .result || stage == .editing || flow.generationOrigin == .result

        // Header
        let trimmedSource = sourceText.trimmingCharacters(in: .whitespacesAndNewlines)
        let hasSource = !trimmedSource.isEmpty
        let preview = hasSource
            ? trimmedSource.split(whereSeparator: \.isNewline).joined(separator: " ")
            : strings.pasteMessage
        var attributes = AttributeContainer()
        attributes.font = .systemFont(ofSize: 13, weight: hasSource ? .regular : .medium)
        previewButton.configuration?.attributedTitle = AttributedString(preview, attributes: attributes)
        previewButton.configuration?.image = UIImage(
            systemName: hasSource ? "chevron.down" : "doc.on.clipboard",
            withConfiguration: UIImage.SymbolConfiguration(pointSize: 10, weight: .semibold)
        )
        previewButton.accessibilityLabel = hasSource ? "\(strings.copiedMessage): \(preview)" : strings.pasteMessage
        previewButton.accessibilityHint = hasSource ? strings.showFullMessage : nil
        previewButton.isEnabled = !flow.isGenerating && !isConflict

        let count = AIReplyService.characterCount(sourceText)
        counterLabel.text = "\(count) / \(sourceLimit)"
        counterLabel.textColor = count > sourceLimit ? theme.destructive : theme.secondaryText
        counterLabel.accessibilityLabel = strings.characterCount(count, limit: sourceLimit)
        counterLabel.isHidden = !hasSource

        closeButton.accessibilityLabel = strings.cancel
        personaChip.isEnabled = !flow.isGenerating

        // Full message
        let showsCard = isSourceExpanded && !isConflict
        sourceCard.isHidden = !showsCard
        previewButton.isHidden = showsCard
        collapseButton.accessibilityLabel = strings.hideFullMessage
        pasteButton.accessibilityLabel = strings.pasteMessage
        clearButton.accessibilityLabel = strings.clearSource
        pasteButton.isHidden = stage != .composing
        clearButton.isHidden = stage != .composing || !hasSource
        sourceView.placeholder = strings.noSourceMessage
        sourceView.accessibilityLabel = strings.copiedMessage

        // Create: no persona and no message - a title, and New once there is
        // something to clear.
        let compose = strings.compose
        let hasInstruction = !instructionText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        titleIcon.isHidden = !isCompose
        titleLabel.isHidden = !isCompose
        titleLabel.text = compose.title
        newButton.isHidden = !isCompose || isConflict || (!hasInstruction && flow.drafts.isEmpty)
        applyText(compose.newDraft, to: newButton, size: 13, weight: .semibold)
        newButton.accessibilityLabel = compose.newDraftAccessibility
        personaChip.isHidden = isCompose
        var copiedAttributes = AttributeContainer()
        copiedAttributes.font = .systemFont(ofSize: 13, weight: .medium)
        copiedButton.configuration?.attributedTitle = AttributedString(compose.replyToCopied, attributes: copiedAttributes)
        copiedButton.configuration?.image = UIImage(
            systemName: "arrowshape.turn.up.left",
            withConfiguration: UIImage.SymbolConfiguration(pointSize: 10, weight: .semibold)
        )
        copiedButton.accessibilityLabel = compose.replyToCopied
        copiedButton.isHidden = !showsCopiedAction
        copiedButton.isEnabled = stage == .composing
        if isCompose {
            previewButton.isHidden = true
            counterLabel.isHidden = true
            sourceCard.isHidden = true
        }
        instructionWasEmpty = !hasInstruction

        // Fields
        instructionView.isHidden = !composingLike || isConflict
        instructionView.placeholder = isCompose ? compose.placeholder : strings.instructionPlaceholder
        instructionView.placeholderLines = isCompose ? 3 : 1
        instructionView.accessibilityLabel = instructionView.placeholder
        instructionView.alpha = flow.isGenerating ? 0.6 : 1
        draftView.isHidden = !replyLike
        draftView.alpha = flow.generationOrigin == .result ? 0.55 : 1
        draftView.accessibilityLabel = isCompose ? compose.draftTitle : strings.draftTitle

        sourceView.showsCaret = focus == .source && stage == .composing
        instructionView.showsCaret = focus == .instruction && stage == .composing
        draftView.showsCaret = focus == .draft && stage == .editing
        refreshBorders()

        // Error
        errorLabel.text = errorMessage
        errorLabel.isHidden = errorMessage == nil || isConflict

        // Composing row
        let showsComposingRow = composingLike && !isConflict
        for view in [quickActions, suggestionStrip, polishChip] as [UIView] {
            view.isHidden = !showsComposingRow
        }
        quickActions.setEnabled(stage == .composing)
        updateAccessory(animated: false)
        primaryButton.isHidden = !composingLike || isConflict
        let overLimit = count > sourceLimit
        let canGenerate = isCompose
            ? stage == .composing && hasInstruction
            : stage == .composing && hasSource && !overLimit
        let primaryTitle: String
        if flow.isGenerating {
            primaryTitle = strings.stop
        } else if errorMessage != nil, errorOffersRetry {
            primaryTitle = strings.retry
        } else {
            primaryTitle = isCompose ? compose.write : strings.generate
        }
        applyText(primaryTitle, to: primaryButton, size: 15, weight: .semibold)
        primaryButton.configuration?.image = isCompose && !flow.isGenerating
            ? UIImage(systemName: "sparkles", withConfiguration: UIImage.SymbolConfiguration(pointSize: 12, weight: .semibold))
            : nil
        primaryButton.configuration?.imagePadding = 5
        primaryButton.configuration?.showsActivityIndicator = flow.isGenerating
        primaryButton.isEnabled = canGenerate || flow.isGenerating
        primaryButton.configuration?.background.backgroundColor = (canGenerate || flow.isGenerating)
            ? theme.accent
            : theme.accent.withAlphaComponent(0.35)
        primaryButton.configuration?.baseForegroundColor = .white

        // Result row
        for view in [backButton, regenerateButton, editButton, insertButton] as [UIView] {
            view.isHidden = !replyLike || isConflict
        }
        let versions = flow.drafts
        let pagerVisible = replyLike && !isConflict && versions.count > 1
        previousButton.isHidden = !pagerVisible
        nextButton.isHidden = !pagerVisible
        versionLabel.isHidden = !pagerVisible
        versionLabel.text = "\(versions.position)/\(versions.count)"
        versionLabel.accessibilityLabel = strings.versionPosition(versions.position, of: versions.count)
        previousButton.isEnabled = versions.canSelectPrevious && !flow.isGenerating
        nextButton.isEnabled = versions.canSelectNext && !flow.isGenerating
        previousButton.accessibilityLabel = strings.previousVersion
        nextButton.accessibilityLabel = strings.nextVersion

        // Back stays live while a new version is made: it stops the request.
        backButton.accessibilityLabel = isCompose ? compose.editRequest : strings.back
        // While a new version is being made the same button stops it: a
        // spinner in place of the arrow, and a tap cancels.
        let regenerating = flow.generationOrigin == .result
        regenerateButton.isBusy = regenerating
        regenerateButton.accessibilityLabel = regenerating ? strings.stop : strings.regenerate
        regenerateButton.isEnabled = stage == .result || stage == .editing || regenerating

        let editing = stage == .editing
        editButton.setSymbol(editing ? "checkmark" : "pencil")
        editButton.isSelectedStyle = editing
        editButton.accessibilityLabel = editing ? strings.doneEditing : strings.editReply
        editButton.isEnabled = stage == .result || stage == .editing

        applyText(strings.insert, to: insertButton, size: 15, weight: .semibold)
        let canInsert = (stage == .result || stage == .editing)
            && !draftText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        insertButton.isEnabled = canInsert
        insertButton.configuration?.background.backgroundColor = canInsert
            ? theme.accent
            : theme.accent.withAlphaComponent(0.35)
        insertButton.configuration?.baseForegroundColor = .white

        // Conflict
        conflictLabel.text = strings.hostFieldNotEmpty
        conflictLabel.isHidden = !isConflict
        for button in [replaceButton, appendButton, conflictCancelButton] {
            button.isHidden = !isConflict
        }
        applyText(strings.replaceExisting, to: replaceButton, size: 14, weight: .semibold)
        applyText(strings.appendToExisting, to: appendButton, size: 14, weight: .semibold)
        applyText(strings.keepTyping, to: conflictCancelButton, size: 14, weight: .semibold)

        refreshReport()
        setNeedsLayout()
    }

    /// The flag on a result, and the panel in the reply's place while it is
    /// open: the header names it, everything about the reply steps aside.
    private func refreshReport() {
        let words = strings.report
        let reporting = reportPanel != nil
        reportButton.isHidden = reporting || !offersReport || flow.stage != .result || isConflict
        reportButton.accessibilityLabel = words.report
        titleIcon.image = UIImage(systemName: reporting ? "flag" : "sparkles",
                                  withConfiguration: UIImage.SymbolConfiguration(pointSize: 13, weight: .semibold))
        for view in reportViews { view.isHidden = !reporting }
        guard let panel = reportPanel else { return }

        for view in [personaChip, previewButton, counterLabel, newButton, closeButton, copiedButton, sourceCard,
                     instructionView, draftView, errorLabel, backButton, regenerateButton, editButton,
                     previousButton, versionLabel, nextButton, insertButton] as [UIView] {
            view.isHidden = true
        }
        titleIcon.isHidden = false
        titleLabel.isHidden = false
        titleLabel.text = words.title

        let editable = panel.phase == .choosing || panel.phase == .failed
        for (reason, button) in reasonButtons {
            let selected = panel.reason == reason
            applyText(words.reason(reason), to: button, size: reasonFont.pointSize, weight: .medium)
            button.configuration?.background.backgroundColor = selected ? theme.accent : theme.fieldBackground
            button.configuration?.baseForegroundColor = selected ? .white : theme.primaryText
            button.isSelected = selected
            button.accessibilityTraits = selected ? [.button, .selected] : .button
            button.isEnabled = editable
        }
        includeTextSwitch.isOn = panel.includesText
        includeTextSwitch.isEnabled = editable
        includeTextSwitch.accessibilityLabel = words.includeText
        includeTextLabel.text = words.includeText
        includeTextLabel.alpha = editable ? 1 : 0.5

        switch panel.phase {
        case .sent:
            reportStatusLabel.text = words.thanks
            reportStatusLabel.textColor = theme.primaryText
        case .failed:
            reportStatusLabel.text = words.failed
            reportStatusLabel.textColor = theme.destructive
        case .choosing, .sending:
            reportStatusLabel.text = nil
        }

        applyText(panel.phase == .sent ? strings.doneEditing : strings.cancel,
                  to: reportCancelButton, size: 14, weight: .semibold)
        applyText(words.send, to: reportSendButton, size: 14, weight: .semibold)
        reportSendButton.isHidden = panel.phase == .sent
        let canSend = panel.reason != nil && editable
        reportSendButton.isEnabled = canSend
        reportSendButton.configuration?.showsActivityIndicator = panel.phase == .sending
        reportSendButton.configuration?.background.backgroundColor = canSend || panel.phase == .sending
            ? theme.accent
            : theme.accent.withAlphaComponent(0.35)
    }

    private func refreshBorders() {
        instructionView.layer.borderColor = (instructionView.showsCaret ? theme.fieldBorderFocused : theme.fieldBorder).cgColor
        draftView.layer.borderColor = (draftView.showsCaret ? theme.fieldBorderFocused : theme.fieldBorder).cgColor
        sourceCard.layer.borderWidth = sourceView.showsCaret ? 1 : 0
        sourceCard.layer.borderColor = theme.fieldBorderFocused.cgColor
    }

    // MARK: Heights

    private func lines(_ font: UIFont?, _ count: Int, inset: UIEdgeInsets) -> CGFloat {
        let lineHeight = (font ?? .systemFont(ofSize: 16)).lineHeight
        return (lineHeight * CGFloat(count) + inset.top + inset.bottom).rounded(.up)
    }

    private func naturalLines(of view: ComposerTextView, width: CGFloat) -> Int {
        let fitting = view.sizeThatFits(CGSize(width: max(width, 1), height: .greatestFiniteMagnitude)).height
        let inset = view.textContainerInset.top + view.textContainerInset.bottom
        let lineHeight = (view.font ?? .systemFont(ofSize: 16)).lineHeight
        return max(1, Int(((fitting - inset) / lineHeight).rounded(.up)))
    }

    private struct Plan {
        var total: CGFloat = 0
        var sourceCard: CGFloat = 0
        var field: CGFloat = 0
        var error: CGFloat = 0
    }

    private var contentWidth: CGFloat { max(0, layoutWidth - outerInset * 2 - innerInset * 2) }

    /// Create shows "Reply to copied" while the instruction is on screen -
    /// also while it is being written from, so the row does not jump away
    /// under a running request.
    private var showsCopiedAction: Bool { isCompose && !flow.showsReply }

    /// Solves the heights for the current stage within `maximumHeight`.
    ///
    /// Measured on content EVENTS - a paste, a new version, a stage change, a
    /// width change - never on a keystroke: the field scrolls rather than
    /// growing under the user's fingers.
    private func solveHeights() -> Plan {
        var plan = Plan()
        let width = contentWidth
        let chrome = 2 + innerInset / 2 + headerHeight + gap + gap + rowHeight + innerInset / 2 + 2 + 4

        if isConflict {
            // The question takes the place of the reply, at the reply's
            // height: the keys stay exactly where they were.
            let minimum = chrome + 36
            plan.total = max(minimum, conflictHeight).rounded(.up)
            plan.field = plan.total - chrome
            return plan
        }

        if let report = reportPanel {
            // The reasons and the switch where the reply was; Cancel and
            // Send where its buttons were.
            let rows = reasonFrames(width: width).map(\.maxY).max() ?? reasonHeight
            let minimum = chrome + rows + gap + includeTextHeight
            plan.total = max(minimum, report.height).rounded(.up)
            plan.field = plan.total - chrome
            return plan
        }

        if let message = errorMessage, !message.isEmpty {
            let fit = errorLabel.sizeThatFits(CGSize(width: width, height: .greatestFiniteMagnitude)).height
            plan.error = min(32, max(15, ceil(fit)))
        }
        let errorBlock = plan.error > 0 ? plan.error + 4 : 0

        if isSourceExpanded {
            let natural = naturalLines(of: sourceView, width: width - 3 - 8 - 30)
            plan.sourceCard = lines(sourceView.font, min(max(natural, 1), flow.showsReply ? 3 : 4),
                                    inset: sourceView.textContainerInset) + 4
        }
        let sourceBlock = plan.sourceCard > 0 ? plan.sourceCard + gap : 0
        // Create's "Reply to copied" line: always there while the instruction
        // is, so it never comes or goes with a keystroke.
        let copiedBlock = showsCopiedAction ? previewHeight + gap : 0

        if flow.showsReply {
            let natural = naturalLines(of: draftView, width: width)
            // A composed message is read before it is sent, and is usually
            // longer than a reply: it gets more lines when there is room.
            let minimum = isCompose ? (maximumHeight < 190 ? 3 : 4) : (maximumHeight < 190 ? 2 : 3)
            let wanted = min(max(natural, minimum), isCompose ? 8 : 6)
            var field = lines(draftView.font, wanted, inset: draftView.textContainerInset)
            let floor = lines(draftView.font, 2, inset: draftView.textContainerInset)
            let budget = maximumHeight - chrome - errorBlock - sourceBlock - copiedBlock
            if field > budget { field = max(floor, budget) }
            plan.field = field
        } else {
            let two = lines(instructionView.font, 2, inset: instructionView.textContainerInset)
            let one = lines(instructionView.font, 1, inset: instructionView.textContainerInset)
            let budget = maximumHeight - chrome - errorBlock - sourceBlock - copiedBlock
            if isCompose {
                // The instruction IS the request here - often two or three
                // sentences - so it gets a real text area: four lines where
                // the screen allows, three on a small one. It scrolls beyond.
                let wanted = maximumHeight >= 200 ? 4 : 3
                let candidates = (1...wanted).reversed().map { lines(instructionView.font, $0, inset: instructionView.textContainerInset) }
                plan.field = candidates.first { $0 <= budget } ?? one
            } else {
                plan.field = budget >= two ? two : one
            }
        }

        plan.total = (chrome + errorBlock + sourceBlock + copiedBlock + plan.field).rounded(.up)
        return plan
    }

    private func remeasure() {
        guard layoutWidth > 0 else { return }
        let height = solveHeights().total
        guard abs(height - preferredHeight) > 0.5 else {
            setNeedsLayout()
            return
        }
        preferredHeight = height
        setNeedsLayout()
        delegate?.composerDidChangeHeight(self)
    }

    // MARK: Layout

    override func layoutSubviews() {
        super.layoutSubviews()
        let plan = solveHeights()
        panel.frame = CGRect(x: outerInset, y: 2, width: bounds.width - outerInset * 2, height: max(0, bounds.height - 6))
        visibleThisPass.removeAll(keepingCapacity: true)
        defer { visibleLastPass = visibleThisPass }

        let width = panel.bounds.width - innerInset * 2
        let left = innerInset
        var y = innerInset / 2

        // Header: persona, message preview, counter, close - or, in Create,
        // a title, New and close.
        let chipWidth = min(max(personaChip.intrinsicContentSize.width, 64), 140)
        place(personaChip, CGRect(x: left, y: y + 1, width: chipWidth, height: headerHeight - 2))
        place(closeButton, CGRect(x: left + width - 28, y: y + 1, width: 28, height: 28))
        let newWidth = min(max(64, ceil(newButton.intrinsicContentSize.width)), width * 0.4)
        place(newButton, CGRect(x: closeButton.frame.minX - 6 - newWidth, y: y + 1, width: newWidth, height: headerHeight - 2))
        place(titleIcon, CGRect(x: left + 2, y: y, width: 20, height: headerHeight))
        // The flag takes its place from the title or the message preview,
        // at their end: no other control moves for it.
        let showsReportButton = !reportButton.isHidden
        var titleRight = newButton.isHidden ? closeButton.frame.minX - 6 : newButton.frame.minX - 6
        if showsReportButton, isCompose {
            place(reportButton, CGRect(x: titleRight - 24, y: y + 3, width: 24, height: 24))
            titleRight = reportButton.frame.minX - 4
        }
        if reportPanel != nil { titleRight = left + width }
        place(titleLabel, CGRect(x: titleIcon.frame.maxX + 5, y: y, width: max(0, titleRight - titleIcon.frame.maxX - 5), height: headerHeight))
        var previewRight = closeButton.frame.minX - 6
        if !counterLabel.isHidden {
            let counterWidth = ceil(counterLabel.sizeThatFits(CGSize(width: 90, height: 20)).width)
            place(counterLabel, CGRect(x: previewRight - counterWidth, y: y, width: counterWidth, height: headerHeight))
            previewRight = counterLabel.frame.minX - 6
        }
        if showsReportButton, !isCompose {
            place(reportButton, CGRect(x: previewRight - 24, y: y + 3, width: 24, height: 24))
            previewRight = reportButton.frame.minX - 4
        }
        let previewLeft = personaChip.frame.maxX + 6
        place(previewButton, CGRect(x: previewLeft, y: y + 2, width: max(0, previewRight - previewLeft), height: previewHeight))
        y += headerHeight + gap

        if reportPanel != nil {
            layoutReport(left: left, width: width, top: y, area: plan.field)
            return
        }

        if isConflict {
            // The question sits where the reply was, the answers where the
            // reply's buttons were.
            let area = max(36, plan.field)
            place(conflictLabel, CGRect(x: left + 4, y: y, width: width - 8, height: area))
            y += area + gap
            let buttonWidth = (width - 12) / 3
            for (index, button) in [replaceButton, appendButton, conflictCancelButton].enumerated() {
                place(button, CGRect(x: left + CGFloat(index) * (buttonWidth + 6), y: y, width: buttonWidth, height: rowHeight))
            }
            return
        }

        // Full message.
        if plan.sourceCard > 0 {
            place(sourceCard, CGRect(x: left, y: y, width: width, height: plan.sourceCard))
            let buttonsX = sourceCard.bounds.width - 28
            place(quoteBar, CGRect(x: 8, y: 8, width: 3, height: max(0, plan.sourceCard - 16)))
            place(sourceView, CGRect(x: 8 + 3 + 8, y: 2, width: max(0, buttonsX - 19 - 4), height: plan.sourceCard - 4))
            var buttonY: CGFloat = 4
            for button in [collapseButton, pasteButton, clearButton] where !button.isHidden {
                place(button, CGRect(x: buttonsX, y: buttonY, width: 24, height: 24))
                buttonY += 26
            }
            y += plan.sourceCard + gap
        }

        // Instruction or reply.
        let field = CGRect(x: left, y: y, width: width, height: plan.field)
        place(instructionView, field)
        place(draftView, field)
        y += plan.field + gap

        if plan.error > 0 {
            place(errorLabel, CGRect(x: left + 2, y: y - 2, width: width - 4, height: plan.error))
            y += plan.error + 4
        }

        // Create: "Reply to copied", as wide as its words - under the
        // instruction and right after a sentence about it, when there is one.
        if showsCopiedAction {
            let copiedWidth = min(width, max(120, ceil(copiedButton.intrinsicContentSize.width)))
            place(copiedButton, CGRect(x: left, y: y, width: copiedWidth, height: previewHeight))
            y += previewHeight + gap
        }

        // Bottom row.
        if flow.showsReply {
            var x = left
            for button in [backButton, regenerateButton, editButton] {
                place(button, CGRect(x: x, y: y, width: iconSize, height: iconSize))
                x += iconSize + 6
            }
            let insertWidth = max(96, ceil(insertButton.intrinsicContentSize.width))
            place(insertButton, CGRect(x: left + width - insertWidth, y: y, width: insertWidth, height: rowHeight))
            if !versionLabel.isHidden {
                let pagerWidth: CGFloat = 28 + 34 + 28
                let available = insertButton.frame.minX - x
                let pagerX = x + max(0, (available - pagerWidth) / 2)
                place(previousButton, CGRect(x: pagerX, y: y + 2, width: 28, height: 28))
                place(versionLabel, CGRect(x: pagerX + 28, y: y, width: 34, height: rowHeight))
                place(nextButton, CGRect(x: pagerX + 62, y: y + 2, width: 28, height: 28))
            }
        } else {
            let primaryWidth = min(max(104, ceil(primaryButton.intrinsicContentSize.width)), width * 0.5)
            place(primaryButton, CGRect(x: left + width - primaryWidth, y: y, width: primaryWidth, height: rowHeight))
            let slot = CGRect(x: left, y: y + 1, width: max(0, primaryButton.frame.minX - left - 8), height: rowHeight - 2)
            place(quickActions, slot)
            place(suggestionStrip, slot)
            place(polishChip, slot.insetBy(dx: 0, dy: 1))
        }

        for view in [sourceView, instructionView, draftView] where view.showsCaret {
            view.scrollCaretIntoView()
        }
    }

    /// The reasons, wrapped onto as many rows as their words need; frames
    /// from the top-left of the reasons' area.
    private func reasonFrames(width: CGFloat) -> [CGRect] {
        var frames: [CGRect] = []
        var x: CGFloat = 0
        var y: CGFloat = 0
        for (reason, _) in reasonButtons {
            let title = strings.report.reason(reason) as NSString
            let pill = min(width, ceil(title.size(withAttributes: [.font: reasonFont]).width) + 24)
            if x > 0, x + pill > width {
                x = 0
                y += reasonHeight + gap
            }
            frames.append(CGRect(x: x, y: y, width: pill, height: reasonHeight))
            x += pill + gap
        }
        return frames
    }

    private func layoutReport(left: CGFloat, width: CGFloat, top: CGFloat, area: CGFloat) {
        let frames = reasonFrames(width: width)
        for ((_, button), frame) in zip(reasonButtons, frames) {
            place(button, frame.offsetBy(dx: left, dy: top))
        }
        let rowsBottom = top + (frames.map(\.maxY).max() ?? reasonHeight)

        let toggleY = rowsBottom + gap
        let switchSize = includeTextSwitch.intrinsicContentSize
        place(includeTextSwitch, CGRect(x: left, y: toggleY + (includeTextHeight - switchSize.height) / 2,
                                        width: switchSize.width, height: switchSize.height))
        let labelX = left + switchSize.width + 8
        place(includeTextLabel, CGRect(x: labelX, y: toggleY, width: max(0, left + width - labelX), height: includeTextHeight))

        // Cancel and Send on the reply's button row; what happened beside them.
        let rowY = top + area + gap
        var right = left + width
        if !reportSendButton.isHidden {
            let sendWidth = min(max(96, ceil(reportSendButton.intrinsicContentSize.width)), width * 0.4)
            place(reportSendButton, CGRect(x: right - sendWidth, y: rowY, width: sendWidth, height: rowHeight))
            right = reportSendButton.frame.minX - 6
        }
        let cancelWidth = min(max(80, ceil(reportCancelButton.intrinsicContentSize.width)), width * 0.35)
        place(reportCancelButton, CGRect(x: right - cancelWidth, y: rowY, width: cancelWidth, height: rowHeight))
        place(reportStatusLabel, CGRect(x: left + 2, y: rowY - 2, width: max(0, reportCancelButton.frame.minX - left - 8),
                                        height: rowHeight + 4))
    }

    /// Sets a frame. The keyboard animates height changes, and inside that
    /// animation a view that was hidden until now would fly in from wherever
    /// it last was - or grow out of a zero-size frame in the corner, which is
    /// how the Back button once ended up a small dot at the left edge. Views
    /// that are hidden, or were not visible in the previous pass, are placed
    /// instantly; only views already on screen move smoothly.
    private func place(_ view: UIView, _ frame: CGRect) {
        let id = ObjectIdentifier(view)
        let visible = !view.isHidden
        if visible && visibleLastPass.contains(id) {
            view.frame = frame
        } else {
            UIView.performWithoutAnimation {
                view.frame = frame
                view.layoutIfNeeded()
            }
        }
        if visible { visibleThisPass.insert(id) }
    }

    // MARK: Keys

    /// Whether a keystroke should edit one of the composer's own fields.
    var acceptsTextInput: Bool { focusedView != nil }

    private var focusedView: ComposerTextView? {
        switch (focus, flow.stage) {
        case (.source, .composing) where isSourceExpanded: return sourceView
        case (.instruction, .composing): return instructionView
        case (.draft, .editing): return draftView
        default: return nil
        }
    }

    private var focusedField: Field {
        guard let view = focusedView else { return .none }
        if view === sourceView { return .source }
        if view === instructionView { return .instruction }
        return .draft
    }

    func insertText(_ text: String) {
        guard let view = focusedView else { return }
        let inserted: Bool
        if view === instructionView {
            // The instruction's limit is enforced at the keystroke that would
            // pass it: cutting text silently at request time would send
            // something the user never saw.
            let limit = instructionLimit
            inserted = view.insert(text) { $0.unicodeScalars.count <= limit }
        } else {
            inserted = view.insert(text)
        }
        if inserted { textChanged(in: view) }
    }

    func deleteBackward() {
        guard let view = focusedView, view.deleteBackward() else { return }
        textChanged(in: view)
    }

    func deleteWordBackward() {
        guard let view = focusedView, view.deleteWordBackward() else { return }
        textChanged(in: view)
    }

    func moveCaret(by offset: Int) {
        focusedView?.moveCaret(by: offset)
    }

    /// Replaces the `length` UTF-16 units before the caret of the focused
    /// field - a typed word swapped for a correction. False when there is no
    /// field, or the instruction would pass its limit.
    @discardableResult
    func replaceBeforeCaret(length: Int, with text: String) -> Bool {
        guard let view = focusedView else { return false }
        let replaced: Bool
        if view === instructionView {
            let limit = instructionLimit
            replaced = view.replaceBeforeCaret(length: length, with: text) { $0.unicodeScalars.count <= limit }
        } else {
            replaced = view.replaceBeforeCaret(length: length, with: text)
        }
        if replaced { textChanged(in: view) }
        return replaced
    }

    /// The whole instruction replaced - a suggested version used or undone -
    /// with the caret at the end.
    func replaceInstruction(with text: String) {
        guard flow.stage == .composing, text.unicodeScalars.count <= instructionLimit else { return }
        focus = .instruction
        instructionView.setText(text)
        refresh()
        delegate?.composer(self, didEdit: .instruction, text: text)
    }

    /// Whether the keys are typing into a field smart correction looks after:
    /// the instruction only - the one field whose row has room for the strip.
    /// A correction nobody could see coming is not made: never in the copied
    /// message, and never in a reply being edited, where the result row has
    /// no free slot to show what a space would do.
    var focusedFieldAcceptsCorrection: Bool { focusedField == .instruction }

    /// Whether the focused field is the instruction while it is being written.
    var isEditingInstruction: Bool { focusedField == .instruction }

    /// The character right after the focused field's caret, nil at its end.
    var textAfterCursor: String? { focusedView?.textAfterCaret }

    // MARK: Suggestions

    /// The strip for the word being typed; empty hides it. Shown only under
    /// the instruction, in place of the quick intents.
    func showSuggestions(_ items: [AutocorrectSuggestion]) {
        guard items != suggestions else { return }
        suggestions = items
        suggestionStrip.show(items)
        updateAccessory(animated: true)
    }

    /// The suggested version of the instruction, or Undo after it was used.
    /// It wins over the strip: it arrives after a pause, when the user has
    /// stopped typing anyway.
    func showPolish(_ chip: InstructionPolisher.Chip) {
        guard chip != polish else { return }
        slotTapGuard.chipChanged(from: polish, to: chip, at: ProcessInfo.processInfo.systemUptime)
        polish = chip
        if chip != .none { polishChip.show(chip) }
        updateAccessory(animated: true)
    }

    private var wantedAccessory: Accessory {
        guard flow.stage == .composing else { return .intents }
        if polish != .none { return .polish }
        if !suggestions.isEmpty, focus == .instruction { return .suggestions }
        return .intents
    }

    /// Crossfades the slot's three occupants. Their frames never change, so
    /// neither does the panel's height.
    private func updateAccessory(animated: Bool) {
        let wanted = wantedAccessory
        let apply = {
            self.quickActions.alpha = wanted == .intents ? 1 : 0
            self.suggestionStrip.alpha = wanted == .suggestions ? 1 : 0
            self.polishChip.alpha = wanted == .polish ? 1 : 0
        }
        quickActions.isUserInteractionEnabled = wanted == .intents
        suggestionStrip.isUserInteractionEnabled = wanted == .suggestions
        polishChip.isUserInteractionEnabled = wanted == .polish
        quickActions.accessibilityElementsHidden = wanted != .intents
        suggestionStrip.accessibilityElementsHidden = wanted != .suggestions
        polishChip.accessibilityElementsHidden = wanted != .polish
        let changed = wanted != shownAccessory
        shownAccessory = wanted
        if animated, changed, window != nil {
            UIView.animate(withDuration: 0.16, delay: 0, options: [.beginFromCurrentState, .allowUserInteraction], animations: apply)
        } else {
            apply()
        }
    }

    /// The text the next keystroke would follow - for auto-capitalization and
    /// the double-space period. With a reply on screen that is the reply: a
    /// keystroke there starts editing it at its caret. Nil while nothing can
    /// be typed (a request is running, or the "field is not empty" question).
    var textBeforeCursor: String? {
        if let view = focusedView { return view.textBeforeCaret }
        if flow.stage == .result { return draftView.textBeforeCaret }
        return nil
    }

    private func textChanged(in view: ComposerTextView) {
        let field = focusedField
        if field == .source {
            // The counter follows the message as it is edited; nothing else
            // in the header does.
            refresh()
        } else if field == .instruction, isCompose,
                  view.currentText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty != instructionWasEmpty {
            // Create: Write and New wake up with the first character and go
            // quiet when the field is emptied again.
            refresh()
        } else if field == .draft {
            let canInsert = !view.currentText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            insertButton.isEnabled = canInsert
            insertButton.configuration?.background.backgroundColor = canInsert
                ? theme.accent
                : theme.accent.withAlphaComponent(0.35)
        }
        if errorMessage != nil {
            errorMessage = nil
            errorLabel.isHidden = true
            remeasure()
        }
        delegate?.composer(self, didEdit: field, text: view.currentText)
    }

    // MARK: Actions

    @objc private func personaTapped() { delegate?.composerDidTapPersona(self) }
    @objc private func closeTapped() { delegate?.composerDidTapClose(self) }
    @objc private func newTapped() { delegate?.composerDidTapNew(self) }
    @objc private func copiedTapped() {
        guard flow.stage == .composing else { return }
        delegate?.composerDidTapReplyToCopied(self)
    }
    @objc private func pasteTapped() { delegate?.composerDidTapPaste(self) }
    @objc private func backTapped() { delegate?.composerDidTapBack(self) }
    @objc private func editTapped() { delegate?.composerDidTapEdit(self) }
    @objc private func insertTapped() { delegate?.composerDidTapInsert(self) }
    @objc private func previousTapped() { delegate?.composerDidTapPreviousVersion(self) }
    @objc private func nextTapped() { delegate?.composerDidTapNextVersion(self) }
    @objc private func replaceTapped() { delegate?.composer(self, didResolveConflictWith: .replace) }
    @objc private func appendTapped() { delegate?.composer(self, didResolveConflictWith: .append) }
    @objc private func conflictCancelTapped() { delegate?.composer(self, didResolveConflictWith: .cancel) }

    // MARK: Report

    @objc private func reportTapped() {
        guard offersReport, flow.stage == .result, reportPanel == nil else { return }
        reportPanel = ReportPanel(height: preferredHeight)
        refresh()
        remeasure()
        delegate?.composerDidChangeReporting(self)
    }

    @objc private func reasonTapped(_ sender: UIButton) {
        guard var panel = reportPanel, panel.phase == .choosing || panel.phase == .failed,
              let reason = reasonButtons.first(where: { $0.button === sender })?.reason else { return }
        panel.reason = reason
        panel.phase = .choosing
        reportPanel = panel
        refresh()
    }

    @objc private func includeTextChanged() {
        reportPanel?.includesText = includeTextSwitch.isOn
    }

    @objc private func includeTextLabelTapped() {
        guard includeTextSwitch.isEnabled, !includeTextSwitch.isHidden else { return }
        includeTextSwitch.setOn(!includeTextSwitch.isOn, animated: true)
        includeTextChanged()
    }

    @objc private func reportSendTapped() {
        guard var panel = reportPanel, let reason = panel.reason,
              panel.phase == .choosing || panel.phase == .failed else { return }
        panel.phase = .sending
        reportPanel = panel
        refresh()
        // What the model wrote, not the user's edits of it.
        let generated = flow.drafts.current?.generated.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        let text = panel.includesText && !generated.isEmpty ? generated : nil
        delegate?.composer(self, didReport: reason, text: text)
    }

    @objc private func reportCancelTapped() {
        closeReport()
    }

    /// How sending went. Sent: thanks, and the reply comes back by itself a
    /// moment later. Not sent: the panel stays, for another try.
    func reportDidFinish(sent: Bool) {
        guard var panel = reportPanel, panel.phase == .sending else { return }
        panel.phase = sent ? .sent : .failed
        reportPanel = panel
        refresh()
        guard sent else { return }
        let close = DispatchWorkItem { [weak self] in self?.closeReport() }
        reportCloseTimer = close
        DispatchQueue.main.asyncAfter(deadline: .now() + 1.6, execute: close)
    }

    private func closeReport() {
        reportCloseTimer?.cancel()
        reportCloseTimer = nil
        guard reportPanel != nil else { return }
        reportPanel = nil
        refresh()
        remeasure()
        delegate?.composerDidChangeReporting(self)
    }

    @objc private func polishTapped() {
        guard slotTapGuard.acceptsTap(at: ProcessInfo.processInfo.systemUptime) else { return }
        switch polishChip.chip {
        case .suggestion: delegate?.composerDidAcceptPolish(self)
        case .undo: delegate?.composerDidUndoPolish(self)
        case .none: break
        }
    }

    @objc private func primaryTapped() {
        if flow.isGenerating {
            delegate?.composerDidTapStop(self)
        } else {
            delegate?.composerDidTapGenerate(self)
        }
    }

    @objc private func regenerateTapped() {
        if flow.generationOrigin == .result {
            delegate?.composerDidTapStop(self)
        } else {
            delegate?.composerDidTapRegenerate(self)
        }
    }

    @objc private func previewTapped() {
        guard !flow.isGenerating, !isConflict else { return }
        if sourceText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            delegate?.composerDidTapPaste(self)
            return
        }
        isSourceExpanded = true
        refresh()
        remeasure()
    }

    @objc private func collapseTapped() {
        isSourceExpanded = false
        if focus == .source { focus = .instruction }
        refresh()
        remeasure()
        delegate?.composerDidMoveCaret(self)
    }

    @objc private func clearSourceTapped() {
        guard flow.stage == .composing else { return }
        sourceView.setText("")
        isSourceExpanded = false
        focus = .instruction
        delegate?.composer(self, didEdit: .source, text: "")
        refresh()
        remeasure()
        delegate?.composerDidMoveCaret(self)
    }

    // A tap in a field only moves the caret, but the keyboard must hear of
    // it: the word suggestions on screen were for the word at the old caret,
    // and a pick would otherwise replace whatever is before the new one.

    @objc private func sourceTapped(_ recognizer: UITapGestureRecognizer) {
        guard flow.stage == .composing else { return }
        focus = .source
        sourceView.placeCaret(at: recognizer.location(in: sourceView))
        refresh()
        delegate?.composerDidMoveCaret(self)
    }

    @objc private func instructionTapped(_ recognizer: UITapGestureRecognizer) {
        guard flow.stage == .composing else { return }
        focus = .instruction
        instructionView.placeCaret(at: recognizer.location(in: instructionView))
        refresh()
        delegate?.composerDidMoveCaret(self)
    }

    @objc private func draftTapped(_ recognizer: UITapGestureRecognizer) {
        let point = recognizer.location(in: draftView)
        switch flow.stage {
        case .editing:
            draftView.placeCaret(at: point)
            delegate?.composerDidMoveCaret(self)
        case .result:
            // Tapping the reply means "let me change this": it becomes
            // editable with the caret where the finger was.
            delegate?.composerDidTapReply(self)
            if flow.stage == .editing {
                draftView.placeCaret(at: point)
                delegate?.composerDidMoveCaret(self)
            }
        default:
            break
        }
    }
}

// MARK: - Quick actions

extension ReplyComposerView: QuickActionRowDelegate {

    /// Presets WRITE INTO THE INSTRUCTION. They never replace the source and
    /// never become the reply: the user can read what was added, edit it, and
    /// add another intent on top.
    func quickActionRow(_ row: QuickActionRow, didSelect intent: QuickIntent) {
        guard flow.stage == .composing,
              slotTapGuard.acceptsTap(at: ProcessInfo.processInfo.systemUptime) else { return }
        focus = .instruction
        let current = instructionText.trimmingCharacters(in: .whitespacesAndNewlines)
        let combined = current.isEmpty ? intent.phrase : current + " " + intent.phrase
        let limit = instructionLimit
        let clamped = combined.unicodeScalars.count <= limit
            ? combined
            : String(String.UnicodeScalarView(combined.unicodeScalars.prefix(limit)))
        instructionView.setText(clamped)
        refresh()
        delegate?.composer(self, didEdit: .instruction, text: clamped)
        // The phrase was added, not typed: no word is in progress at the caret.
        delegate?.composerDidMoveCaret(self)
    }
}

// MARK: - Round icon button

/// The composer's round icon buttons: Close, Back, Regenerate, Edit, the
/// version arrows and the full-message buttons.
///
/// Drawn by hand rather than with `UIButton.Configuration`, whose background
/// is a separate view the button lays out itself - inside the keyboard's
/// animated height changes that produced a misshapen Back button. Here the
/// disc is the control's own layer: it is always exactly the frame.
final class CircleIconButton: UIControl {

    /// Filled buttons sit on a disc; the others are just the glyph.
    let isFilled: Bool

    var fillColor: UIColor = .white { didSet { refreshAppearance() } }
    var glyphColor: UIColor = .label { didSet { refreshAppearance() } }
    var accentColor: UIColor = .systemBlue { didSet { refreshAppearance() } }

    /// "On": an accent disc with a white glyph - Edit while editing.
    var isSelectedStyle = false { didSet { refreshAppearance() } }

    /// A spinner in place of the glyph while a request runs.
    var isBusy = false {
        didSet {
            guard isBusy != oldValue else { return }
            imageView.isHidden = isBusy
            if isBusy { spinner.startAnimating() } else { spinner.stopAnimating() }
        }
    }

    override var isEnabled: Bool { didSet { refreshAppearance() } }
    override var isHighlighted: Bool { didSet { refreshAppearance() } }

    private let imageView = UIImageView()
    private let spinner = UIActivityIndicatorView(style: .medium)
    private let pointSize: CGFloat
    private var symbolName = ""

    init(symbol: String, pointSize: CGFloat, filled: Bool = true) {
        self.pointSize = pointSize
        self.isFilled = filled
        super.init(frame: .zero)
        imageView.contentMode = .center
        imageView.isUserInteractionEnabled = false
        spinner.hidesWhenStopped = true
        spinner.isUserInteractionEnabled = false
        addSubview(imageView)
        addSubview(spinner)
        isAccessibilityElement = true
        accessibilityTraits = .button
        setSymbol(symbol)
        refreshAppearance()
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not used")
    }

    func setSymbol(_ name: String) {
        guard name != symbolName else { return }
        symbolName = name
        imageView.image = UIImage(
            systemName: name,
            withConfiguration: UIImage.SymbolConfiguration(pointSize: pointSize, weight: .semibold)
        )?.withRenderingMode(.alwaysTemplate)
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        layer.cornerRadius = min(bounds.width, bounds.height) / 2
        imageView.frame = bounds
        spinner.center = CGPoint(x: bounds.midX, y: bounds.midY)
    }

    /// A little more than the drawn disc answers a touch. Neighbours are 6pt
    /// apart, so 3pt to each side never reaches the next button.
    override func point(inside point: CGPoint, with event: UIEvent?) -> Bool {
        bounds.insetBy(dx: -3, dy: -5).contains(point)
    }

    private func refreshAppearance() {
        let selected = isSelectedStyle && isEnabled
        let glyph = selected ? UIColor.white : glyphColor
        imageView.tintColor = glyph
        spinner.color = glyph
        if selected {
            backgroundColor = accentColor
        } else {
            backgroundColor = isFilled ? fillColor : .clear
        }
        alpha = !isEnabled ? 0.35 : (isHighlighted ? 0.55 : 1)
        accessibilityTraits = isEnabled ? .button : [.button, .notEnabled]
    }
}
