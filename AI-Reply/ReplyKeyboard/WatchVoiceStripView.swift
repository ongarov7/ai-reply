import UIKit

/// What Watch Voice has to say, over the persona pills and in exactly their
/// frame: the keyboard never gets taller for it.
///
/// Status text and, when there is something to do, one action: Generate for
/// an instruction that was waiting, Insert Response for a saved reply.
final class WatchVoiceStripView: UIView {

    var onPrimary: (() -> Void)?
    var onDismiss: (() -> Void)?
    var onPreview: (() -> Void)?

    private let previewButton = UIButton(type: .custom)
    private let label = UILabel()
    private let primaryButton = UIButton(type: .system)
    private let dismissButton = UIButton(type: .system)

    private var theme = KeyboardTheme(isDark: true)
    private var strings = WatchVoiceStrings.forLanguage(.english)
    private var strip: WatchVoiceStrip?

    private let primaryFont = UIFont.systemFont(ofSize: 13, weight: .semibold)
    private let buttonHeight: CGFloat = 32

    init() {
        super.init(frame: .zero)
        layer.cornerRadius = 15
        label.font = .systemFont(ofSize: 13, weight: .medium)
        label.lineBreakMode = .byTruncatingTail
        label.adjustsFontSizeToFitWidth = true
        label.minimumScaleFactor = 0.85
        addSubview(label)

        previewButton.addTarget(self, action: #selector(previewTapped), for: .touchUpInside)
        addSubview(previewButton)

        var primary = UIButton.Configuration.filled()
        primary.cornerStyle = .capsule
        primary.contentInsets = NSDirectionalEdgeInsets(top: 0, leading: 10, bottom: 0, trailing: 10)
        primaryButton.configuration = primary
        primaryButton.addTarget(self, action: #selector(primaryTapped), for: .touchUpInside)
        addSubview(primaryButton)

        var dismiss = UIButton.Configuration.plain()
        dismiss.contentInsets = .zero
        dismiss.image = UIImage(systemName: "xmark", withConfiguration: UIImage.SymbolConfiguration(pointSize: 12, weight: .semibold))
        dismissButton.configuration = dismiss
        dismissButton.addTarget(self, action: #selector(dismissTapped), for: .touchUpInside)
        addSubview(dismissButton)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not used")
    }

    func configure(theme: KeyboardTheme, uiLanguage: AppLanguage) {
        self.theme = theme
        strings = WatchVoiceStrings.forLanguage(uiLanguage)
        backgroundColor = theme.letterKey
        label.textColor = theme.primaryText
        dismissButton.tintColor = theme.secondaryText
        dismissButton.accessibilityLabel = strings.dismiss
        primaryButton.configuration?.baseBackgroundColor = theme.accent
        primaryButton.configuration?.baseForegroundColor = .white
        if let strip { show(strip) }
    }

    func show(_ strip: WatchVoiceStrip) {
        self.strip = strip
        var primary: String?
        var canPreview = false
        var canDismiss = true
        switch strip {
        case .waiting:              label.text = strings.waiting
        case .openWatchApp:         label.text = strings.openWatchApp
        case .installWatchApp:      label.text = strings.installWatchApp
        case .connectionLost:       label.text = strings.connectionLost
        case .fullAccessRequired:   label.text = strings.fullAccessRequired
        case .generating:
            label.text = strings.generating
            canDismiss = false
        case .instructionReceived(let preview):
            label.text = strings.instructionReceived + ": " + preview
            primary = strings.generate
        case .responseReady(let preview):
            label.text = strings.responseReady + ": " + preview
            primary = strings.insertResponse
            canPreview = true
        case .failure(let failure):
            label.text = strings.message(for: failure)
        }

        if let primary {
            var attributes = AttributeContainer()
            attributes.font = primaryFont
            primaryButton.configuration?.attributedTitle = AttributedString(primary, attributes: attributes)
            primaryButton.accessibilityLabel = primary
        }
        primaryButton.isHidden = primary == nil
        dismissButton.isHidden = !canDismiss
        previewButton.isHidden = !canPreview
        previewButton.accessibilityLabel = label.text
        setNeedsLayout()
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        let inset: CGFloat = 10
        var trailing = bounds.width - 4
        if !dismissButton.isHidden {
            dismissButton.frame = CGRect(x: trailing - 30, y: 0, width: 30, height: bounds.height)
            trailing = dismissButton.frame.minX
        }
        if !primaryButton.isHidden {
            let title = primaryButton.accessibilityLabel ?? ""
            let width = ceil((title as NSString).size(withAttributes: [.font: primaryFont]).width) + 22
            let y = ((bounds.height - buttonHeight) / 2).rounded()
            primaryButton.frame = CGRect(x: trailing - width, y: y, width: width, height: buttonHeight)
            trailing = primaryButton.frame.minX - 6
        }
        label.frame = CGRect(x: inset, y: 0, width: max(0, trailing - inset), height: bounds.height)
        previewButton.frame = label.frame
    }

    @objc private func primaryTapped() { onPrimary?() }
    @objc private func dismissTapped() { onDismiss?() }
    @objc private func previewTapped() { onPreview?() }
}
