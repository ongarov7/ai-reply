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

    struct Content {
        var personaName: String
        var source: String
        var instruction: String
        var flow: ReplyComposerFlow
        var errorMessage: String?
        var sourceLimit: Int
        var instructionLimit: Int
    }

    // MARK: State

    private(set) var flow = ReplyComposerFlow()
    private(set) var focus: Field = .instruction
    private var errorMessage: String?
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
    /// little more keyboard height for.
    var wantsExpandedContext: Bool { isSourceExpanded && !isConflict }

    // MARK: Views

    private let panel = UIView()

    // Header
    private let personaChip = UIButton(type: .system)
    private let previewButton = UIButton(type: .system)
    private let counterLabel = UILabel()
    private let closeButton = UIButton(type: .system)

    // Full message
    private let sourceCard = UIView()
    private let quoteBar = UIView()
    private let sourceView = ComposerTextView(font: .systemFont(ofSize: 14), inset: UIEdgeInsets(top: 5, left: 0, bottom: 5, right: 0))
    private let collapseButton = UIButton(type: .system)
    private let pasteButton = UIButton(type: .system)
    private let clearButton = UIButton(type: .system)

    // Instruction / reply
    private let instructionView = ComposerTextView(font: .systemFont(ofSize: 15.5), inset: UIEdgeInsets(top: 8, left: 10, bottom: 8, right: 10))
    private let draftView = ComposerTextView(font: .systemFont(ofSize: 16), inset: UIEdgeInsets(top: 7, left: 10, bottom: 7, right: 10))

    // Status
    private let errorLabel = UILabel()
    private let conflictLabel = UILabel()

    // Composing row
    private let quickActions = QuickActionRow()
    private let primaryButton = UIButton(type: .system)

    // Result row
    private let backButton = UIButton(type: .system)
    private let regenerateButton = UIButton(type: .system)
    private let editButton = UIButton(type: .system)
    private let previousButton = UIButton(type: .system)
    private let versionLabel = UILabel()
    private let nextButton = UIButton(type: .system)
    private let insertButton = UIButton(type: .system)

    // Conflict row
    private let replaceButton = UIButton(type: .system)
    private let appendButton = UIButton(type: .system)
    private let conflictCancelButton = UIButton(type: .system)

    // MARK: Metrics

    private let outerInset: CGFloat = 5
    private let innerInset: CGFloat = 8
    private let gap: CGFloat = 6
    private let headerHeight: CGFloat = 30
    private let rowHeight: CGFloat = 32
    private let iconSize: CGFloat = 32

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

        var preview = UIButton.Configuration.plain()
        preview.contentInsets = NSDirectionalEdgeInsets(top: 0, leading: 8, bottom: 0, trailing: 6)
        preview.background.cornerRadius = 8
        preview.imagePlacement = .trailing
        preview.imagePadding = 5
        preview.titleLineBreakMode = .byTruncatingTail
        previewButton.configuration = preview
        previewButton.contentHorizontalAlignment = .leading
        previewButton.addTarget(self, action: #selector(previewTapped), for: .touchUpInside)

        counterLabel.font = .monospacedDigitSystemFont(ofSize: 11, weight: .medium)
        counterLabel.textAlignment = .right

        configureIcon(closeButton, symbol: "xmark", pointSize: 12)
        closeButton.addTarget(self, action: #selector(closeTapped), for: .touchUpInside)

        sourceCard.layer.cornerRadius = 9
        sourceCard.layer.cornerCurve = .continuous
        quoteBar.layer.cornerRadius = 1.5
        sourceCard.addSubview(quoteBar)
        sourceCard.addSubview(sourceView)
        configureIcon(collapseButton, symbol: "chevron.up", pointSize: 11)
        configureIcon(pasteButton, symbol: "doc.on.clipboard", pointSize: 11)
        configureIcon(clearButton, symbol: "xmark.circle.fill", pointSize: 12)
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

        conflictLabel.font = .systemFont(ofSize: 13, weight: .medium)
        conflictLabel.numberOfLines = 2
        conflictLabel.textAlignment = .center

        quickActions.delegate = self

        configurePill(primaryButton, symbol: nil, trailingImage: false)
        primaryButton.addTarget(self, action: #selector(primaryTapped), for: .touchUpInside)

        configureIcon(backButton, symbol: "chevron.left", pointSize: 13)
        configureIcon(regenerateButton, symbol: "arrow.clockwise", pointSize: 13)
        configureIcon(editButton, symbol: "pencil", pointSize: 13)
        configureIcon(previousButton, symbol: "chevron.left", pointSize: 11, filled: false)
        configureIcon(nextButton, symbol: "chevron.right", pointSize: 11, filled: false)
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

        [personaChip, previewButton, counterLabel, closeButton, sourceCard, instructionView, draftView,
         errorLabel, conflictLabel, quickActions, primaryButton, backButton, regenerateButton, editButton,
         previousButton, versionLabel, nextButton, insertButton, replaceButton, appendButton,
         conflictCancelButton].forEach(panel.addSubview)
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

    private func configureIcon(_ button: UIButton, symbol: String, pointSize: CGFloat, filled: Bool = true) {
        var configuration = UIButton.Configuration.plain()
        configuration.contentInsets = .zero
        configuration.background.cornerRadius = iconSize / 2
        configuration.image = UIImage(systemName: symbol,
                                      withConfiguration: UIImage.SymbolConfiguration(pointSize: pointSize, weight: .semibold))
        button.configuration = configuration
        button.tag = filled ? 1 : 0
    }

    private func attachTap(to view: UIView, action: Selector) {
        let tap = UITapGestureRecognizer(target: self, action: action)
        view.addGestureRecognizer(tap)
    }

    // MARK: Configuration

    func configure(theme: KeyboardTheme, uiLanguage: AppLanguage) {
        self.theme = theme
        self.strings = AIReplyStrings.forLanguage(uiLanguage)
        quickActions.configure(theme: theme, strings: strings)
        applyTheme()
        refresh()
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
        flow = content.flow
        errorMessage = content.errorMessage
        sourceLimit = content.sourceLimit
        instructionLimit = content.instructionLimit

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
        if isConflict { isSourceExpanded = false }
        refresh()
        remeasure()
    }

    /// Back to a clean slate when the composer closes.
    func reset() {
        flow = ReplyComposerFlow()
        focus = .instruction
        errorMessage = nil
        isSourceExpanded = false
        sourceView.setText("")
        instructionView.setText("")
        draftView.setText("")
        refresh()
        remeasure()
    }

    private var isConflict: Bool {
        if case .conflict = flow.stage { return true }
        return false
    }

    private func applyTheme() {
        panel.backgroundColor = theme.panelBackground
        personaChip.configuration?.background.backgroundColor = theme.fieldBackground
        personaChip.configuration?.baseForegroundColor = theme.primaryText
        previewButton.configuration?.background.backgroundColor = theme.quoteBackground
        previewButton.configuration?.baseForegroundColor = theme.quoteText
        previewButton.tintColor = theme.secondaryText
        counterLabel.textColor = theme.secondaryText

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

        for button in [closeButton, collapseButton, pasteButton, clearButton, backButton, regenerateButton,
                       editButton, previousButton, nextButton] {
            styleIcon(button, highlighted: false)
        }
        for button in [replaceButton, appendButton] {
            button.configuration?.background.backgroundColor = theme.accent
            button.configuration?.baseForegroundColor = .white
        }
        conflictCancelButton.configuration?.background.backgroundColor = theme.fieldBackground
        conflictCancelButton.configuration?.baseForegroundColor = theme.primaryText
        refreshBorders()
    }

    private func styleIcon(_ button: UIButton, highlighted: Bool) {
        let filled = button.tag == 1
        button.configuration?.background.backgroundColor = highlighted
            ? theme.accent
            : (filled ? theme.fieldBackground : .clear)
        button.configuration?.baseForegroundColor = highlighted ? .white : theme.primaryText
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

        // Fields
        instructionView.isHidden = !composingLike || isConflict
        instructionView.placeholder = strings.instructionPlaceholder
        instructionView.accessibilityLabel = strings.instructionPlaceholder
        instructionView.alpha = flow.isGenerating ? 0.6 : 1
        draftView.isHidden = !replyLike
        draftView.alpha = flow.generationOrigin == .result ? 0.55 : 1
        draftView.accessibilityLabel = strings.draftTitle

        sourceView.isFocused = focus == .source && stage == .composing
        instructionView.isFocused = focus == .instruction && stage == .composing
        draftView.isFocused = focus == .draft && stage == .editing
        refreshBorders()

        // Error
        errorLabel.text = errorMessage
        errorLabel.isHidden = errorMessage == nil || isConflict

        // Composing row
        quickActions.isHidden = !composingLike || isConflict
        quickActions.setEnabled(stage == .composing)
        primaryButton.isHidden = !composingLike || isConflict
        let overLimit = count > sourceLimit
        let canGenerate = stage == .composing && hasSource && !overLimit
        let primaryTitle: String
        if flow.isGenerating {
            primaryTitle = strings.stop
        } else if errorMessage != nil {
            primaryTitle = strings.retry
        } else {
            primaryTitle = strings.generate
        }
        applyText(primaryTitle, to: primaryButton, size: 15, weight: .semibold)
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

        backButton.accessibilityLabel = strings.back
        regenerateButton.accessibilityLabel = flow.isGenerating ? strings.stop : strings.regenerate
        regenerateButton.configuration?.showsActivityIndicator = flow.generationOrigin == .result
        regenerateButton.configuration?.image = flow.generationOrigin == .result ? nil : UIImage(
            systemName: "arrow.clockwise",
            withConfiguration: UIImage.SymbolConfiguration(pointSize: 13, weight: .semibold)
        )
        regenerateButton.isEnabled = stage == .result || stage == .editing || flow.generationOrigin == .result

        let editing = stage == .editing
        editButton.configuration?.image = UIImage(
            systemName: editing ? "checkmark" : "pencil",
            withConfiguration: UIImage.SymbolConfiguration(pointSize: 13, weight: .semibold)
        )
        editButton.accessibilityLabel = editing ? strings.doneEditing : strings.editReply
        editButton.isEnabled = stage == .result || stage == .editing
        styleIcon(editButton, highlighted: editing)

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

        setNeedsLayout()
    }

    private func refreshBorders() {
        instructionView.layer.borderColor = (instructionView.isFocused ? theme.fieldBorderFocused : theme.fieldBorder).cgColor
        draftView.layer.borderColor = (draftView.isFocused ? theme.fieldBorderFocused : theme.fieldBorder).cgColor
        sourceCard.layer.borderWidth = sourceView.isFocused ? 1 : 0
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

    /// Solves the heights for the current stage within `maximumHeight`.
    ///
    /// Measured on content EVENTS - a paste, a new version, a stage change, a
    /// width change - never on a keystroke: the field scrolls rather than
    /// growing under the user's fingers.
    private func plan() -> Plan {
        var plan = Plan()
        let width = contentWidth
        let chrome = 2 + innerInset / 2 + headerHeight + gap + gap + rowHeight + innerInset / 2 + 2 + 4

        if isConflict {
            plan.total = 2 + innerInset / 2 + headerHeight + gap + 36 + gap + rowHeight + innerInset / 2 + 2 + 4
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

        if flow.showsReply {
            let natural = naturalLines(of: draftView, width: width)
            let minimum = maximumHeight < 190 ? 2 : 3
            let wanted = min(max(natural, minimum), 6)
            var field = lines(draftView.font, wanted, inset: draftView.textContainerInset)
            let floor = lines(draftView.font, 2, inset: draftView.textContainerInset)
            let budget = maximumHeight - chrome - errorBlock - sourceBlock
            if field > budget { field = max(floor, budget) }
            plan.field = field
        } else {
            let two = lines(instructionView.font, 2, inset: instructionView.textContainerInset)
            let one = lines(instructionView.font, 1, inset: instructionView.textContainerInset)
            let budget = maximumHeight - chrome - errorBlock - sourceBlock
            plan.field = budget >= two ? two : one
        }

        plan.total = (chrome + errorBlock + sourceBlock + plan.field).rounded(.up)
        return plan
    }

    private func remeasure() {
        guard layoutWidth > 0 else { return }
        let height = plan().total
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
        let plan = plan()
        panel.frame = CGRect(x: outerInset, y: 2, width: bounds.width - outerInset * 2, height: max(0, bounds.height - 6))
        let width = panel.bounds.width - innerInset * 2
        let left = innerInset
        var y = innerInset / 2

        // Header: persona, message preview, counter, close.
        let chipWidth = min(max(personaChip.intrinsicContentSize.width, 64), 140)
        personaChip.frame = CGRect(x: left, y: y + 1, width: chipWidth, height: headerHeight - 2)
        closeButton.frame = CGRect(x: left + width - 28, y: y + 1, width: 28, height: 28)
        var previewRight = closeButton.frame.minX - 6
        if !counterLabel.isHidden {
            let counterWidth = ceil(counterLabel.sizeThatFits(CGSize(width: 90, height: 20)).width)
            counterLabel.frame = CGRect(x: previewRight - counterWidth, y: y, width: counterWidth, height: headerHeight)
            previewRight = counterLabel.frame.minX - 6
        }
        let previewLeft = personaChip.frame.maxX + 6
        previewButton.frame = CGRect(x: previewLeft, y: y + 2, width: max(0, previewRight - previewLeft), height: headerHeight - 4)
        y += headerHeight + gap

        if isConflict {
            conflictLabel.frame = CGRect(x: left, y: y, width: width, height: 36)
            y += 36 + gap
            let buttonWidth = (width - 12) / 3
            for (index, button) in [replaceButton, appendButton, conflictCancelButton].enumerated() {
                button.frame = CGRect(x: left + CGFloat(index) * (buttonWidth + 6), y: y, width: buttonWidth, height: rowHeight)
            }
            return
        }

        // Full message.
        if plan.sourceCard > 0 {
            sourceCard.frame = CGRect(x: left, y: y, width: width, height: plan.sourceCard)
            let buttonsX = sourceCard.bounds.width - 28
            quoteBar.frame = CGRect(x: 8, y: 8, width: 3, height: max(0, plan.sourceCard - 16))
            sourceView.frame = CGRect(x: 8 + 3 + 8, y: 2, width: max(0, buttonsX - 19 - 4), height: plan.sourceCard - 4)
            var buttonY: CGFloat = 4
            for button in [collapseButton, pasteButton, clearButton] where !button.isHidden {
                button.frame = CGRect(x: buttonsX, y: buttonY, width: 24, height: 24)
                buttonY += 26
            }
            y += plan.sourceCard + gap
        }

        // Instruction or reply.
        let field = CGRect(x: left, y: y, width: width, height: plan.field)
        instructionView.frame = field
        draftView.frame = field
        y += plan.field + gap

        if plan.error > 0 {
            errorLabel.frame = CGRect(x: left + 2, y: y - 2, width: width - 4, height: plan.error)
            y += plan.error + 4
        }

        // Bottom row.
        if flow.showsReply {
            var x = left
            for button in [backButton, regenerateButton, editButton] {
                button.frame = CGRect(x: x, y: y, width: iconSize, height: iconSize)
                x += iconSize + 6
            }
            let insertWidth = max(96, ceil(insertButton.intrinsicContentSize.width))
            insertButton.frame = CGRect(x: left + width - insertWidth, y: y, width: insertWidth, height: rowHeight)
            if !versionLabel.isHidden {
                let pagerWidth: CGFloat = 26 + 34 + 26
                let available = insertButton.frame.minX - x
                let pagerX = x + max(0, (available - pagerWidth) / 2)
                previousButton.frame = CGRect(x: pagerX, y: y + 3, width: 26, height: 26)
                versionLabel.frame = CGRect(x: pagerX + 26, y: y, width: 34, height: rowHeight)
                nextButton.frame = CGRect(x: pagerX + 60, y: y + 3, width: 26, height: 26)
            }
        } else {
            let primaryWidth = min(max(104, ceil(primaryButton.intrinsicContentSize.width)), width * 0.5)
            primaryButton.frame = CGRect(x: left + width - primaryWidth, y: y, width: primaryWidth, height: rowHeight)
            quickActions.frame = CGRect(x: left, y: y + 1, width: max(0, primaryButton.frame.minX - left - 8), height: rowHeight - 2)
        }

        for view in [sourceView, instructionView, draftView] where view.isFocused {
            view.scrollCaretIntoView()
        }
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

    var textBeforeCursor: String? { focusedView?.textBeforeCaret }

    private func textChanged(in view: ComposerTextView) {
        let field = focusedField
        if field == .source {
            // The counter follows the message as it is edited; nothing else
            // in the header does.
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
    @objc private func pasteTapped() { delegate?.composerDidTapPaste(self) }
    @objc private func backTapped() { delegate?.composerDidTapBack(self) }
    @objc private func editTapped() { delegate?.composerDidTapEdit(self) }
    @objc private func insertTapped() { delegate?.composerDidTapInsert(self) }
    @objc private func previousTapped() { delegate?.composerDidTapPreviousVersion(self) }
    @objc private func nextTapped() { delegate?.composerDidTapNextVersion(self) }
    @objc private func replaceTapped() { delegate?.composer(self, didResolveConflictWith: .replace) }
    @objc private func appendTapped() { delegate?.composer(self, didResolveConflictWith: .append) }
    @objc private func conflictCancelTapped() { delegate?.composer(self, didResolveConflictWith: .cancel) }

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
    }

    @objc private func clearSourceTapped() {
        guard flow.stage == .composing else { return }
        sourceView.setText("")
        isSourceExpanded = false
        focus = .instruction
        delegate?.composer(self, didEdit: .source, text: "")
        refresh()
        remeasure()
    }

    @objc private func sourceTapped(_ recognizer: UITapGestureRecognizer) {
        guard flow.stage == .composing else { return }
        focus = .source
        sourceView.placeCaret(at: recognizer.location(in: sourceView))
        refresh()
    }

    @objc private func instructionTapped(_ recognizer: UITapGestureRecognizer) {
        guard flow.stage == .composing else { return }
        focus = .instruction
        instructionView.placeCaret(at: recognizer.location(in: instructionView))
        refresh()
    }

    @objc private func draftTapped(_ recognizer: UITapGestureRecognizer) {
        let point = recognizer.location(in: draftView)
        switch flow.stage {
        case .editing:
            draftView.placeCaret(at: point)
        case .result:
            // Tapping the reply means "let me change this": it becomes
            // editable with the caret where the finger was.
            delegate?.composerDidTapReply(self)
            if flow.stage == .editing { draftView.placeCaret(at: point) }
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
        guard flow.stage == .composing else { return }
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
    }
}
