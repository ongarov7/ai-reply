import UIKit

/// What the keyboard's screenshot strip says.
enum KeyboardScreenshotStrip: Equatable {
    /// A screenshot is waiting: Reply opens the composer on its message.
    case ready(app: String?, messages: Int)
    /// The app already wrote a reply for it: Insert.
    case draft(preview: String)
    /// The screenshot had no chat text.
    case noText
}

/// The latest screenshot, as the keyboard sees it. It only reads the App
/// Group; it never captures, never opens an app and never sends anything by
/// itself — Reply and Insert are the person's taps.
@MainActor
final class KeyboardScreenshot {
    var onChange: ((KeyboardScreenshotStrip?) -> Void)?
    var hasFullAccess: (() -> Bool)?

    private let store: ScreenshotContextStore
    private let defaults: UserDefaults
    private var observer: ScreenshotSignalObserver?
    private var visible = false
    private(set) var context: ScreenshotContext?
    private var shown: KeyboardScreenshotStrip?

    /// Screenshots and drafts closed with ✕ or already inserted, shared by
    /// every keyboard instance. Identifiers only, no text.
    private static let handledKey = "screenshot.keyboard.handled.v1"

    init(store: ScreenshotContextStore = .shared, defaults: UserDefaults = .standard) {
        self.store = store
        self.defaults = defaults
    }

    func appear() {
        visible = true
        observer = ScreenshotSignalObserver { [weak self] in
            Task { @MainActor in self?.refresh() }
        }
        refresh()
    }

    func disappear() {
        visible = false
        observer = nil
    }

    func refresh() {
        guard visible else { return }
        // Without Full Access the keyboard cannot open the shared container.
        let stored = hasFullAccess?() == true ? store.read() : nil
        context = stored
        let next = stored.flatMap(strip(for:))
        guard next != shown else { return }
        shown = next
        onChange?(next)
    }

    private func strip(for context: ScreenshotContext) -> KeyboardScreenshotStrip? {
        if let draft = context.draft, !isHandled("draft:" + draft.requestID) {
            let flat = draft.text.split(whereSeparator: \.isWhitespace).joined(separator: " ")
            return .draft(preview: flat.count > 60 ? String(flat.prefix(60)) + "…" : flat)
        }
        guard !isHandled("context:" + context.id), context.draft == nil else { return nil }
        if context.hasNoText || context.conversation.target == nil { return .noText }
        return .ready(app: context.conversation.appName, messages: context.conversation.messages.count)
    }

    /// ✕: this screenshot (or its draft) stays closed in every keyboard.
    func dismiss() {
        guard let context else { return }
        if let draft = context.draft { record("draft:" + draft.requestID) }
        record("context:" + context.id)
        refresh()
    }

    /// The draft was inserted (or opened for review): it does not come back.
    func draftHandled() {
        guard let draft = context?.draft else { return }
        record("draft:" + draft.requestID)
        record("context:" + (context?.id ?? ""))
        refresh()
    }

    /// The composer opened on this screenshot: the strip steps aside.
    func contextOpened() {
        guard let context else { return }
        record("context:" + context.id)
        refresh()
    }

    private func isHandled(_ key: String) -> Bool {
        (defaults.dictionary(forKey: Self.handledKey) ?? [:])[key] != nil
    }

    private func record(_ key: String) {
        let now = Date().timeIntervalSince1970
        var values = (defaults.dictionary(forKey: Self.handledKey) as? [String: Double]) ?? [:]
        values = values.filter { now - $0.value < ScreenshotContext.lifetime * 2 }
        values[key] = now
        defaults.set(values, forKey: Self.handledKey)
    }
}

/// The screenshot strip, over the persona pills and in exactly their frame,
/// like the Watch and Voice strips: the keyboard never gets taller for it.
final class ScreenshotStripView: UIView {
    var onPrimary: (() -> Void)?
    var onDismiss: (() -> Void)?

    private let icon = UIImageView(image: UIImage(systemName: "camera.viewfinder"))
    private let label = UILabel()
    private let primaryButton = UIButton(type: .system)
    private let dismissButton = UIButton(type: .system)
    private let primaryFont = UIFont.systemFont(ofSize: 13, weight: .semibold)
    private let buttonHeight: CGFloat = 32
    private var strings = ScreenshotKeyboardStrings(.english)
    private var strip: KeyboardScreenshotStrip?

    init() {
        super.init(frame: .zero)
        layer.cornerRadius = 15
        icon.contentMode = .scaleAspectFit
        icon.preferredSymbolConfiguration = UIImage.SymbolConfiguration(pointSize: 13, weight: .semibold)
        icon.isAccessibilityElement = false
        addSubview(icon)
        label.font = .systemFont(ofSize: 13, weight: .medium)
        label.lineBreakMode = .byTruncatingTail
        label.adjustsFontSizeToFitWidth = true
        label.minimumScaleFactor = 0.85
        addSubview(label)

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
        strings = ScreenshotKeyboardStrings(uiLanguage)
        backgroundColor = theme.letterKey
        label.textColor = theme.primaryText
        icon.tintColor = theme.accent
        dismissButton.tintColor = theme.secondaryText
        dismissButton.accessibilityLabel = strings.dismiss
        primaryButton.configuration?.baseBackgroundColor = theme.accent
        primaryButton.configuration?.baseForegroundColor = .white
        if let strip { show(strip) }
    }

    func show(_ strip: KeyboardScreenshotStrip) {
        self.strip = strip
        var primary: String?
        switch strip {
        case .ready(let app, let messages):
            label.text = strings.ready(app: app, messages: messages)
            primary = strings.reply
        case .draft(let preview):
            label.text = strings.draftReady(preview)
            primary = strings.insert
        case .noText:
            label.text = strings.noText
        }
        if let primary {
            var attributes = AttributeContainer()
            attributes.font = primaryFont
            primaryButton.configuration?.attributedTitle = AttributedString(primary, attributes: attributes)
            primaryButton.accessibilityLabel = primary
        }
        primaryButton.isHidden = primary == nil
        label.accessibilityLabel = label.text
        setNeedsLayout()
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        icon.frame = CGRect(x: 10, y: (bounds.height - 16) / 2, width: 16, height: 16)
        var trailing = bounds.width - 4
        dismissButton.frame = CGRect(x: trailing - 30, y: 0, width: 30, height: bounds.height)
        trailing = dismissButton.frame.minX
        if !primaryButton.isHidden {
            let title = primaryButton.accessibilityLabel ?? ""
            let width = ceil((title as NSString).size(withAttributes: [.font: primaryFont]).width) + 22
            primaryButton.frame = CGRect(x: trailing - width, y: ((bounds.height - buttonHeight) / 2).rounded(),
                                         width: width, height: buttonHeight)
            trailing = primaryButton.frame.minX - 6
        }
        let leading = icon.frame.maxX + 6
        label.frame = CGRect(x: leading, y: 0, width: max(0, trailing - leading), height: bounds.height)
    }

    @objc private func primaryTapped() { onPrimary?() }
    @objc private func dismissTapped() { onDismiss?() }
}
