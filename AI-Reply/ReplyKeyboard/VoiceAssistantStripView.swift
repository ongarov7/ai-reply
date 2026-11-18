import UIKit

/// The iPhone voice session inside the persona row, right of ✨. It uses the
/// personas' viewport and never takes space from the keys.
///
/// * Recording: a live waveform, the newest recognized words (two lines),
///   the copied-message toggle when there is room, Done and Cancel.
/// * Processing: progress and Cancel.
/// * Result: a two-line preview (tap to review and edit), Insert when the
///   result belongs to this field or Review when it may belong to another
///   chat, and Close. Nothing is ever inserted or sent on its own.
final class VoiceAssistantStripView: UIView {
    var onReview: (() -> Void)?
    var onInsert: (() -> Void)?
    var onDismiss: (() -> Void)?
    var onAction: ((KeyboardVoiceAssistantAction) -> Void)?

    private let statusImage = UIImageView()
    private let progress = UIActivityIndicatorView(style: .medium)
    private let caption = UILabel()
    /// The caption's tap target: a result opens for review from here.
    private let previewTarget = UIButton(type: .custom)
    private let attach = UIButton(type: .system)
    private let primary = UIButton(type: .system)
    private let close = UIButton(type: .system)
    private let controls = UIScrollView()
    private var controlButtons: [UIButton] = []

    private var theme = KeyboardTheme(isDark: true)
    /// Live speech is fitted to the caption at layout time, newest words
    /// last, so a growing sentence never shows as "first word … last word".
    private var liveText: String?
    private var recording = false
    private var contextAttached = false
    private var closeStopsSession = false
    private var primaryAction: KeyboardVoiceAssistantAction?
    private var showsControls = false
    private var controlsLanguage: AppLanguage?

    private static let captionFont = UIFont.systemFont(ofSize: 13, weight: .regular)
    private static let leadingWidth: CGFloat = 30
    private static let closeWidth: CGFloat = 30
    private static let attachWidth: CGFloat = 34

    init() {
        super.init(frame: .zero)
        layer.cornerRadius = 14
        layer.cornerCurve = .continuous

        statusImage.contentMode = .center
        statusImage.isAccessibilityElement = false
        progress.hidesWhenStopped = true

        caption.font = Self.captionFont
        caption.numberOfLines = 2
        caption.isAccessibilityElement = false
        previewTarget.addTarget(self, action: #selector(reviewTapped), for: .touchUpInside)

        attach.addTarget(self, action: #selector(attachTapped), for: .touchUpInside)
        primary.addTarget(self, action: #selector(primaryTapped), for: .touchUpInside)
        var closeConfiguration = UIButton.Configuration.plain()
        closeConfiguration.contentInsets = .zero
        closeConfiguration.image = UIImage(systemName: "xmark",
            withConfiguration: UIImage.SymbolConfiguration(pointSize: 12, weight: .semibold))
        close.configuration = closeConfiguration
        close.addTarget(self, action: #selector(closeTapped), for: .touchUpInside)

        [statusImage, progress, caption, previewTarget, attach, primary, close].forEach(addSubview)
        controls.showsHorizontalScrollIndicator = false
        controls.delaysContentTouches = false
        controls.isHidden = true
        addSubview(controls)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) { fatalError("init(coder:) is not used") }

    // MARK: States

    /// - Parameter insertsDirectly: the result was requested from this very
    ///   field. Otherwise the primary action opens it for review first.
    func configure(snapshot: VoiceAssistantSnapshot, theme: KeyboardTheme, language: AppLanguage,
                   insertsDirectly: Bool = false) {
        self.theme = theme
        let strings = KeyboardVoiceStrings(language)
        let ready = snapshot.readyResponse() != nil
        let processing = snapshot.phase == .transcribing || snapshot.phase == .generating
        let failed = snapshot.phase == .error
        recording = snapshot.phase.isRecording
        contextAttached = recording && !snapshot.sourceMessage.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        closeStopsSession = recording || processing
        primaryAction = recording ? .finish : nil
        showLiveViews()
        backgroundColor = theme.letterKey

        // Leading status.
        if processing { progress.color = theme.accent; progress.startAnimating() } else { progress.stopAnimating() }
        statusImage.removeAllSymbolEffects()
        if recording {
            setStatusSymbol("waveform", color: theme.accent)
            statusImage.addSymbolEffect(.variableColor.iterative.dimInactiveLayers, options: .repeating)
        } else if ready {
            setStatusSymbol("checkmark.circle.fill", color: .systemGreen)
        } else if failed {
            setStatusSymbol("exclamationmark.triangle.fill", color: .systemOrange)
        } else {
            statusImage.image = nil
        }
        statusImage.isHidden = processing || statusImage.image == nil

        // Caption.
        let message: String
        var accessibility: String
        liveText = nil
        caption.textColor = theme.primaryText
        if ready {
            message = snapshot.response.trimmingCharacters(in: .whitespacesAndNewlines)
            accessibility = strings.status(.responseReady) + ". " + message
        } else if let live = snapshot.keyboardCaptureCaption() {
            switch live {
            case .speak:
                message = contextAttached ? strings.speakReply : strings.speak
                caption.textColor = theme.secondaryText
                accessibility = strings.status(.listening) + ". " + message
            case .recognized(_, let fullText):
                liveText = fullText
                message = fullText
                accessibility = strings.status(.listening) + ": " + fullText
            }
        } else if failed {
            message = String((snapshot.error ?? strings.status(.error)).trimmingCharacters(in: .whitespacesAndNewlines).prefix(220))
            accessibility = message
        } else {
            message = strings.status(snapshot.phase)
            accessibility = message
        }
        caption.text = message
        caption.lineBreakMode = .byTruncatingTail
        previewTarget.isUserInteractionEnabled = ready
        previewTarget.accessibilityLabel = accessibility
        previewTarget.accessibilityHint = ready ? strings.reviewHint : nil
        previewTarget.accessibilityTraits = ready ? .button : [.staticText, .updatesFrequently]

        // Copied message as context, while recording.
        configureAttach(strings: strings)

        // Primary.
        primary.isHidden = !(recording || ready)
        if recording {
            configurePrimary(title: strings.finishShort, symbol: "checkmark")
            primary.accessibilityLabel = strings.finish
        } else if ready {
            configurePrimary(title: insertsDirectly ? strings.insert : strings.openShort,
                             symbol: insertsDirectly ? "text.insert" : "text.magnifyingglass")
            primary.accessibilityLabel = insertsDirectly ? strings.insert : strings.review
            primary.accessibilityHint = insertsDirectly ? nil : strings.confirmConversation
        }

        close.tintColor = theme.secondaryText
        close.accessibilityLabel = closeStopsSession ? strings.stop : strings.cancel
        updateAccessibilityOrder()
        setNeedsLayout()
    }

    func showControls(snapshot: VoiceAssistantSnapshot, theme: KeyboardTheme, language: AppLanguage) {
        let attached = snapshot.phase.isRecording
            && !snapshot.sourceMessage.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        let strings = KeyboardVoiceStrings(language)
        // Live speech refreshes the session twice a second. An open menu keeps
        // its buttons (a finger may be on one) and only updates the toggle.
        if showsControls, controlButtons.count == 4, attached == contextAttached,
           theme.isDark == self.theme.isDark, language == controlsLanguage {
            return
        }
        configure(snapshot: snapshot, theme: theme, language: language)
        controlsLanguage = language
        let actions: [(KeyboardVoiceAssistantAction, String, String, String)] = [
            (.pasteContext, strings.pasteShort, contextAttached ? "doc.on.clipboard.fill" : "doc.on.clipboard", strings.useCopiedMessage),
            (.finish, strings.finishShort, "checkmark", strings.finish),
            (.pause, strings.pauseShort, "pause.fill", strings.pause),
            (.stop, strings.stopShort, "xmark", strings.stop)
        ]
        controlButtons.forEach { $0.removeFromSuperview() }
        controlButtons = actions.map { action, title, symbol, label in
            let tint = action == .stop ? theme.destructive : theme.accent
            var configuration = UIButton.Configuration.tinted()
            configuration.cornerStyle = .capsule
            configuration.baseBackgroundColor = tint
            configuration.baseForegroundColor = tint
            configuration.contentInsets = NSDirectionalEdgeInsets(top: 0, leading: 10, bottom: 0, trailing: 10)
            configuration.image = UIImage(systemName: symbol,
                withConfiguration: UIImage.SymbolConfiguration(pointSize: 11, weight: .semibold))
            configuration.imagePadding = 4
            var attributes = AttributeContainer()
            attributes.font = UIFont.systemFont(ofSize: 12, weight: .semibold)
            configuration.attributedTitle = AttributedString(title, attributes: attributes)
            let button = UIButton(configuration: configuration)
            button.accessibilityLabel = label
            if action == .pasteContext, contextAttached { button.accessibilityValue = strings.attached }
            button.addAction(UIAction { [weak self] _ in self?.onAction?(action) }, for: .touchUpInside)
            controls.addSubview(button)
            return button
        }
        showsControls = true
        controls.contentOffset = .zero
        controls.isHidden = false
        [statusImage, caption, previewTarget, attach, primary].forEach { $0.isHidden = true }
        statusImage.removeAllSymbolEffects()
        progress.stopAnimating()
        closeStopsSession = false
        close.accessibilityLabel = strings.cancel
        updateAccessibilityOrder()
        setNeedsLayout()
    }

    func showNotice(_ message: String, theme: KeyboardTheme, language: AppLanguage) {
        self.theme = theme
        showLiveViews()
        backgroundColor = theme.letterKey
        liveText = nil
        recording = false
        contextAttached = false
        closeStopsSession = false
        primaryAction = nil
        statusImage.removeAllSymbolEffects()
        setStatusSymbol("info.circle", color: theme.accent)
        statusImage.isHidden = false
        progress.stopAnimating()
        caption.text = message
        caption.textColor = theme.primaryText
        caption.lineBreakMode = .byTruncatingTail
        previewTarget.isUserInteractionEnabled = false
        previewTarget.accessibilityLabel = message
        previewTarget.accessibilityHint = nil
        previewTarget.accessibilityTraits = .staticText
        attach.isHidden = true
        primary.isHidden = true
        close.tintColor = theme.secondaryText
        close.accessibilityLabel = KeyboardVoiceStrings(language).cancel
        updateAccessibilityOrder()
        setNeedsLayout()
        UIAccessibility.post(notification: .announcement, argument: message)
    }

    // MARK: Pieces

    private func showLiveViews() {
        showsControls = false
        controls.isHidden = true
        controlButtons.forEach { $0.removeFromSuperview() }
        controlButtons = []
        caption.isHidden = false
        previewTarget.isHidden = false
    }

    private func setStatusSymbol(_ name: String, color: UIColor) {
        statusImage.image = UIImage(systemName: name,
            withConfiguration: UIImage.SymbolConfiguration(pointSize: 15, weight: .semibold))
        statusImage.tintColor = color
    }

    private func configureAttach(strings: KeyboardVoiceStrings) {
        var configuration = UIButton.Configuration.plain()
        configuration.contentInsets = .zero
        configuration.cornerStyle = .capsule
        configuration.image = UIImage(systemName: contextAttached ? "doc.on.clipboard.fill" : "doc.on.clipboard",
            withConfiguration: UIImage.SymbolConfiguration(pointSize: 14, weight: .medium))
        configuration.baseForegroundColor = theme.accent
        configuration.background.backgroundColor = contextAttached ? theme.accent.withAlphaComponent(0.16) : .clear
        attach.configuration = configuration
        attach.accessibilityLabel = strings.useCopiedMessage
        attach.accessibilityValue = contextAttached ? strings.attached : nil
        // Shown in layoutSubviews when the caption keeps enough room.
        attach.isHidden = !recording
    }

    private func configurePrimary(title: String, symbol: String) {
        var configuration = UIButton.Configuration.filled()
        configuration.cornerStyle = .capsule
        configuration.contentInsets = NSDirectionalEdgeInsets(top: 0, leading: 10, bottom: 0, trailing: 11)
        configuration.image = UIImage(systemName: symbol,
            withConfiguration: UIImage.SymbolConfiguration(pointSize: 12, weight: .semibold))
        configuration.imagePadding = 4
        configuration.baseBackgroundColor = theme.accent
        configuration.baseForegroundColor = .white
        var attributes = AttributeContainer()
        attributes.font = UIFont.systemFont(ofSize: 13, weight: .semibold)
        configuration.attributedTitle = AttributedString(title, attributes: attributes)
        configuration.titleLineBreakMode = .byTruncatingTail
        primary.configuration = configuration
        primary.accessibilityHint = nil
    }

    private func updateAccessibilityOrder() {
        accessibilityElements = showsControls
            ? controlButtons + [close]
            : [previewTarget, attach, primary, close].filter { !$0.isHidden }
    }

    // MARK: Layout

    override func layoutSubviews() {
        super.layoutSubviews()
        let height = bounds.height
        close.frame = CGRect(x: max(0, bounds.width - Self.closeWidth), y: 0, width: Self.closeWidth, height: height)
        controls.frame = CGRect(x: 4, y: 0, width: max(0, close.frame.minX - 6), height: height)
        var x: CGFloat = 0
        let buttonHeight = min(30, height - 6)
        for button in controlButtons {
            let width = max(44, ceil(button.intrinsicContentSize.width))
            button.frame = CGRect(x: x, y: (height - buttonHeight) / 2, width: width, height: buttonHeight)
            x += width + 6
        }
        controls.contentSize = CGSize(width: max(0, x - 6), height: height)
        guard !showsControls else { return }

        let leading: CGFloat = (!statusImage.isHidden || progress.isAnimating) ? Self.leadingWidth : 10
        statusImage.frame = CGRect(x: 6, y: 0, width: 22, height: height)
        progress.frame = CGRect(x: 6, y: (height - 20) / 2, width: 20, height: 20)

        let primaryWidth: CGFloat = primary.isHidden ? 0
            : min(max(64, ceil(primary.intrinsicContentSize.width)), max(64, bounds.width * 0.42))
        let primaryHeight = min(32, height - 4)
        primary.frame = CGRect(x: close.frame.minX - primaryWidth, y: (height - primaryHeight) / 2,
                               width: primaryWidth, height: primaryHeight)

        // Done and Cancel always stay; the copied-message toggle needs a
        // caption that can still show a few words, otherwise it lives in the
        // microphone menu only.
        let roomBeforePrimary = primary.frame.minX - leading - 6
        let showsAttach = recording && roomBeforePrimary - Self.attachWidth >= 96
        attach.isHidden = !showsAttach
        let attachSize = min(Self.attachWidth, height - 4)
        attach.frame = showsAttach
            ? CGRect(x: primary.frame.minX - 4 - attachSize, y: (height - attachSize) / 2, width: attachSize, height: attachSize)
            : .zero
        let textEnd = (showsAttach ? attach.frame.minX : primary.isHidden ? close.frame.minX : primary.frame.minX) - 6
        caption.frame = CGRect(x: leading, y: 0, width: max(0, textEnd - leading), height: height)
        previewTarget.frame = caption.frame
        if let liveText {
            caption.lineBreakMode = .byWordWrapping
            caption.text = VoiceCaption.newestWords(of: liveText, fitting: caption.bounds.width, lines: 2, font: Self.captionFont)
        }
        updateAccessibilityOrder()
    }

    // MARK: Actions

    @objc private func reviewTapped() { onReview?() }
    @objc private func attachTapped() { onAction?(.pasteContext) }
    @objc private func primaryTapped() {
        if let primaryAction { onAction?(primaryAction) } else { onInsert?() }
    }
    @objc private func closeTapped() {
        if closeStopsSession { onAction?(.stop) } else { onDismiss?() }
    }
}
