import UIKit

/// A compact native voice row. It uses the persona viewport and never takes
/// space from the keys. Reviewing a result is always an explicit tap.
final class VoiceAssistantStripView: UIView {
    var onReview: (() -> Void)?
    var onInsert: (() -> Void)?
    var onDismiss: (() -> Void)?
    var onAction: ((KeyboardVoiceAssistantAction) -> Void)?

    private let preview = UIButton(type: .system)
    private let primary = UIButton(type: .system)
    private let paste = UIButton(type: .system)
    private let dismiss = UIButton(type: .system)
    private let statusImage = UIImageView()
    private let progress = UIActivityIndicatorView(style: .medium)
    private let controls = UIScrollView()
    private var controlButtons: [UIButton] = []
    private var recording = false
    private var hasRecognizedCaption = false
    private var cancelsSession = false
    private var primaryAction: KeyboardVoiceAssistantAction?

    init() {
        super.init(frame: .zero)
        layer.cornerRadius = 14
        preview.titleLabel?.font = .systemFont(ofSize: 13, weight: .regular)
        preview.titleLabel?.lineBreakMode = .byTruncatingTail
        preview.titleLabel?.textAlignment = .left
        preview.contentHorizontalAlignment = .leading
        preview.contentVerticalAlignment = .center
        preview.addTarget(self, action: #selector(reviewTapped), for: .touchUpInside)
        primary.addTarget(self, action: #selector(primaryTapped), for: .touchUpInside)
        paste.setImage(UIImage(systemName: "doc.on.clipboard",
            withConfiguration: UIImage.SymbolConfiguration(pointSize: 15, weight: .medium)), for: .normal)
        paste.addTarget(self, action: #selector(pasteTapped), for: .touchUpInside)
        dismiss.setImage(UIImage(systemName: "xmark", withConfiguration: UIImage.SymbolConfiguration(pointSize: 11, weight: .semibold)), for: .normal)
        dismiss.addTarget(self, action: #selector(dismissTapped), for: .touchUpInside)
        statusImage.contentMode = .scaleAspectFit
        progress.hidesWhenStopped = true
        [statusImage, progress, preview, paste, primary, dismiss].forEach(addSubview)
        controls.showsHorizontalScrollIndicator = false
        controls.delaysContentTouches = false
        controls.isHidden = true
        addSubview(controls)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) { fatalError("init(coder:) is not used") }

    func configure(snapshot: VoiceAssistantSnapshot, theme: KeyboardTheme, language: AppLanguage) {
        controls.isHidden = true
        preview.isHidden = false
        let strings = KeyboardVoiceStrings(language)
        let ready = snapshot.readyResponse() != nil
        recording = snapshot.phase.isRecording
        let processing = snapshot.phase == .transcribing || snapshot.phase == .generating
        cancelsSession = recording || processing
        primaryAction = recording ? .finish : nil
        backgroundColor = theme.letterKey

        let message: String
        var fullCaption: String?
        if ready { message = snapshot.response }
        else if let caption = snapshot.keyboardCaptureCaption() {
            switch caption {
            case .speak: message = strings.speak
            case .recognized(let preview, let fullText):
                message = preview
                fullCaption = fullText
            }
        } else if snapshot.phase == .error {
            message = String((snapshot.error ?? "").trimmingCharacters(in: .whitespacesAndNewlines).prefix(220))
        } else { message = strings.status(snapshot.phase) }
        preview.setTitle(message, for: .normal)
        hasRecognizedCaption = fullCaption != nil
        preview.setTitleColor(theme.primaryText, for: .normal)
        preview.titleLabel?.numberOfLines = ready || fullCaption != nil ? 2 : 1
        preview.titleLabel?.lineBreakMode = fullCaption != nil ? .byTruncatingHead : .byTruncatingTail
        preview.accessibilityLabel = ready ? strings.status(.responseReady) + ". " + strings.review + ": " + message
            : fullCaption.map { strings.status(.listening) + ": " + $0 } ?? message
        preview.isUserInteractionEnabled = ready
        preview.accessibilityTraits = ready ? .button : .staticText

        statusImage.image = UIImage(systemName: ready ? "checkmark.circle.fill" : "waveform",
            withConfiguration: UIImage.SymbolConfiguration(pointSize: 15, weight: .medium))
        statusImage.tintColor = ready ? .systemGreen : theme.accent
        statusImage.isHidden = !ready && !recording
        progress.color = theme.accent
        if processing { progress.startAnimating() } else { progress.stopAnimating() }

        primary.isHidden = !ready && !recording
        configurePrimary(title: recording ? strings.finishShort : strings.insert,
                         symbol: recording ? "checkmark" : "arrow.up", theme: theme)
        primary.accessibilityLabel = recording ? strings.finish : strings.insert
        paste.isHidden = !recording
        paste.tintColor = theme.accent
        paste.accessibilityLabel = strings.useCopiedMessage
        dismiss.tintColor = theme.secondaryText
        dismiss.accessibilityLabel = cancelsSession ? strings.stop : strings.cancel
        setNeedsLayout()
    }

    private func configurePrimary(title: String, symbol: String, theme: KeyboardTheme) {
        var configuration = UIButton.Configuration.filled()
        configuration.cornerStyle = .capsule
        configuration.contentInsets = NSDirectionalEdgeInsets(top: 0, leading: 9, bottom: 0, trailing: 9)
        configuration.image = UIImage(systemName: symbol, withConfiguration: UIImage.SymbolConfiguration(pointSize: 12, weight: .semibold))
        configuration.imagePadding = 4
        configuration.baseBackgroundColor = theme.accent
        configuration.baseForegroundColor = .white
        var attributes = AttributeContainer()
        attributes.font = .systemFont(ofSize: 13, weight: .semibold)
        configuration.attributedTitle = AttributedString(title, attributes: attributes)
        primary.configuration = configuration
    }

    func showControls(snapshot: VoiceAssistantSnapshot, theme: KeyboardTheme, language: AppLanguage) {
        configure(snapshot: snapshot, theme: theme, language: language)
        let strings = KeyboardVoiceStrings(language)
        let actions: [(KeyboardVoiceAssistantAction, String, String)] = [
            (.pasteContext, strings.pasteShort, strings.useCopiedMessage),
            (.finish, strings.finishShort, strings.finish),
            (.pause, strings.pauseShort, strings.pause),
            (.stop, strings.stopShort, strings.stop)
        ]
        controlButtons.forEach { $0.removeFromSuperview() }
        controlButtons = actions.map { action, title, label in
            let button = UIButton(type: .system)
            button.titleLabel?.font = .systemFont(ofSize: 12, weight: .semibold)
            button.setTitle(title, for: .normal)
            button.setTitleColor(action == .stop ? .systemRed : theme.accent, for: .normal)
            button.accessibilityLabel = label
            button.addAction(UIAction { [weak self] _ in self?.onAction?(action) }, for: .touchUpInside)
            controls.addSubview(button)
            return button
        }
        controls.contentOffset = .zero
        controls.isHidden = false
        preview.isHidden = true
        primary.isHidden = true
        paste.isHidden = true
        statusImage.isHidden = true
        progress.stopAnimating()
        cancelsSession = false
        dismiss.accessibilityLabel = strings.cancel
        setNeedsLayout()
    }

    func showNotice(_ message: String, theme: KeyboardTheme, language: AppLanguage) {
        controls.isHidden = true
        preview.isHidden = false
        preview.isUserInteractionEnabled = false
        preview.titleLabel?.numberOfLines = 2
        preview.titleLabel?.lineBreakMode = .byTruncatingTail
        preview.accessibilityTraits = .staticText
        preview.setTitle(message, for: .normal)
        preview.setTitleColor(theme.primaryText, for: .normal)
        preview.accessibilityLabel = message
        primary.isHidden = true
        paste.isHidden = true
        statusImage.isHidden = true
        progress.stopAnimating()
        cancelsSession = false
        recording = false
        backgroundColor = theme.letterKey
        dismiss.tintColor = theme.secondaryText
        dismiss.accessibilityLabel = KeyboardVoiceStrings(language).cancel
        setNeedsLayout()
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        dismiss.frame = CGRect(x: max(0, bounds.width - 28), y: 0, width: 28, height: bounds.height)
        let primaryWidth: CGFloat = primary.isHidden ? 0 : max(58, min(98, primary.intrinsicContentSize.width))
        primary.frame = CGRect(x: dismiss.frame.minX - primaryWidth, y: 1, width: primaryWidth, height: max(0, bounds.height - 2))
        let leading: CGFloat = (!statusImage.isHidden || progress.isAnimating) ? 28 : 8
        // On narrow phones Done and Cancel remain visible; Paste is also
        // available in the inline controls opened by the waveform button.
        let minimumCaptionAndPaste: CGFloat = hasRecognizedCaption ? 154 : 110
        let canFitPaste = recording && !preview.isHidden && primary.frame.minX - leading >= minimumCaptionAndPaste
        paste.isHidden = !canFitPaste
        paste.frame = CGRect(x: primary.frame.minX - (canFitPaste ? 32 : 0), y: 0,
                             width: canFitPaste ? 32 : 0, height: bounds.height)
        let textEnd = canFitPaste ? paste.frame.minX : primary.frame.minX
        preview.frame = CGRect(x: leading, y: 0, width: max(0, textEnd - leading - 5), height: bounds.height)
        statusImage.frame = CGRect(x: 8, y: (bounds.height - 16) / 2, width: 16, height: 16)
        progress.frame = CGRect(x: 6, y: (bounds.height - 20) / 2, width: 20, height: 20)
        controls.frame = CGRect(x: 4, y: 0, width: max(0, dismiss.frame.minX - 8), height: bounds.height)
        var x: CGFloat = 0
        for button in controlButtons {
            let width = max(44, button.intrinsicContentSize.width + 16)
            button.frame = CGRect(x: x, y: 0, width: width, height: bounds.height)
            x += width
        }
        controls.contentSize = CGSize(width: x, height: bounds.height)
    }

    @objc private func reviewTapped() { onReview?() }
    @objc private func primaryTapped() {
        if let primaryAction { onAction?(primaryAction) } else { onInsert?() }
    }
    @objc private func pasteTapped() { onAction?(.pasteContext) }
    @objc private func dismissTapped() {
        if cancelsSession { onAction?(.stop) } else { onDismiss?() }
    }
}
