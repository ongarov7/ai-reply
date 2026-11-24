import UIKit

/// AI Reply Voice while a turn is recorded and answered. It takes the whole
/// keyboard, the way iOS's own dictation used to: the keys step away and
/// come back with the response, which then waits in the row for Insert.
///
/// Built from system pieces only, so it reads as part of iOS: Liquid Glass
/// buttons on iOS 26 (filled and key-coloured ones before), the system text
/// sizes and colours, SF Symbols and their effects.
///
/// * Top: Cancel, what the session is doing, the copied-message toggle.
/// * Middle: the newest recognized words, large, newest last.
/// * Bottom: back to the keys, and Done.
///
/// The keyboard still never records, and nothing is inserted on its own.
final class VoiceDictationPanelView: UIView {
    var onAction: ((KeyboardVoiceAssistantAction) -> Void)?
    /// The keys for the rest of this session; the row's microphone brings
    /// the panel back.
    var onShowKeyboard: (() -> Void)?

    /// Preparing, recording and the work after Done. A response, an error
    /// or an ended session gives the keys back.
    static func shows(_ snapshot: VoiceAssistantSnapshot) -> Bool {
        switch snapshot.phase {
        case .ready, .listening, .speechDetected, .transcribing, .generating: return true
        case .responseReady, .paused, .stopped, .error: return false
        }
    }

    private enum DoneState: Equatable { case enabled, disabled, busy }

    private let cancelButton = UIButton(type: .system)
    private let contextButton = UIButton(type: .system)
    private let keyboardButton = UIButton(type: .system)
    private let doneButton = UIButton(type: .system)
    private let statusImage = UIImageView()
    private let spinner = UIActivityIndicatorView(style: .medium)
    private let titleLabel = UILabel()
    private let subtitleLabel = UILabel()
    private let transcriptLabel = UILabel()

    private var theme = KeyboardTheme(isDark: false)
    private var strings = KeyboardVoiceStrings(.english)
    private var sessionID: String?
    private var phase: VoiceAssistantPhase = .ready
    private var transcript = ""
    private var placeholder: String?
    private var contextAttached = false
    /// "Replying to «…»" while a copied message is attached.
    private var context: String?
    private var notice: String?
    private var noticeReset: DispatchWorkItem?
    /// Done was tapped and the app has not taken it yet.
    private var finishing = false
    private var waveformAnimating = false
    /// What the buttons were last drawn for: live speech redraws the panel
    /// twice a second, the glass is rebuilt only when this changes.
    private var buttonsKey: String?

    // iOS's own sizes: Headline, Footnote, Title 2 and a large button.
    private static let titleFont = UIFont.systemFont(ofSize: 17, weight: .semibold)
    private static let subtitleFont = UIFont.systemFont(ofSize: 13, weight: .regular)
    private static let transcriptFont = UIFont.systemFont(ofSize: 22, weight: .regular)
    private static let buttonFont = UIFont.systemFont(ofSize: 17, weight: .semibold)
    private static let symbol = UIImage.SymbolConfiguration(pointSize: 17, weight: .semibold)

    init() {
        super.init(frame: .zero)
        backgroundColor = .clear

        statusImage.image = UIImage(systemName: "waveform", withConfiguration: Self.symbol)
        statusImage.contentMode = .center
        statusImage.isAccessibilityElement = false
        spinner.hidesWhenStopped = true

        titleLabel.font = Self.titleFont
        titleLabel.textAlignment = .center
        titleLabel.adjustsFontSizeToFitWidth = true
        titleLabel.minimumScaleFactor = 0.8
        titleLabel.accessibilityTraits = [.header, .updatesFrequently]

        subtitleLabel.font = Self.subtitleFont
        subtitleLabel.textAlignment = .center
        subtitleLabel.numberOfLines = 2
        subtitleLabel.lineBreakMode = .byTruncatingTail
        subtitleLabel.isAccessibilityElement = false

        transcriptLabel.font = Self.transcriptFont
        transcriptLabel.lineBreakMode = .byWordWrapping
        transcriptLabel.accessibilityTraits = [.staticText, .updatesFrequently]

        cancelButton.addTarget(self, action: #selector(cancelTapped), for: .touchUpInside)
        contextButton.addTarget(self, action: #selector(contextTapped), for: .touchUpInside)
        keyboardButton.addTarget(self, action: #selector(keyboardTapped), for: .touchUpInside)
        doneButton.addTarget(self, action: #selector(doneTapped), for: .touchUpInside)

        [statusImage, spinner, titleLabel, subtitleLabel, transcriptLabel,
         cancelButton, contextButton, keyboardButton, doneButton].forEach(addSubview)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) { fatalError("init(coder:) is not used") }

    // MARK: State

    func configure(snapshot: VoiceAssistantSnapshot, theme: KeyboardTheme, language: AppLanguage) {
        if snapshot.sessionID != sessionID {
            sessionID = snapshot.sessionID
            finishing = false
            clearNotice()
        }
        self.theme = theme
        strings = KeyboardVoiceStrings(language)
        overrideUserInterfaceStyle = theme.isDark ? .dark : .light
        tintColor = theme.accent
        phase = snapshot.phase
        // The app took Done: it is transcribing, or the turn is over.
        if !phase.isRecording { finishing = false }
        let source = snapshot.sourceMessage.trimmingCharacters(in: .whitespacesAndNewlines)
        contextAttached = phase.isRecording && !source.isEmpty
        context = source.isEmpty ? nil : strings.replyingTo(source)

        switch snapshot.keyboardCaptureCaption() {
        case .speak?:
            transcript = ""
            placeholder = contextAttached ? strings.speakReply : strings.speak
        case .recognized(_, let fullText)?:
            transcript = fullText
            placeholder = nil
        case nil:
            // Preparing, or the words Done sent while the response is made.
            transcript = snapshot.transcript.trimmingCharacters(in: .whitespacesAndNewlines)
            placeholder = nil
        }
        render()
    }

    /// A short message under the status (a copied message taken or refused);
    /// the panel goes back to normal after `duration`.
    func showNotice(_ message: String, duration: TimeInterval) {
        noticeReset?.cancel()
        notice = message
        render()
        UIAccessibility.post(notification: .announcement, argument: message)
        let reset = DispatchWorkItem { [weak self] in
            self?.notice = nil
            self?.render()
        }
        noticeReset = reset
        DispatchQueue.main.asyncAfter(deadline: .now() + duration, execute: reset)
    }

    private func clearNotice() {
        noticeReset?.cancel()
        noticeReset = nil
        notice = nil
    }

    // MARK: Drawing

    private func render() {
        let recording = phase.isRecording && !finishing
        let working = finishing || phase == .transcribing || phase == .generating

        // Status: a live waveform while listening, a spinner while the
        // microphone gets ready; after Done, Done itself shows the work.
        titleLabel.text = recording ? strings.status(.listening)
            : finishing ? strings.status(.transcribing) : strings.status(phase)
        titleLabel.textColor = .label
        statusImage.tintColor = theme.accent
        statusImage.isHidden = !recording
        if recording, !waveformAnimating {
            statusImage.addSymbolEffect(.variableColor.iterative.dimInactiveLayers, options: .repeating)
            waveformAnimating = true
        } else if !recording, waveformAnimating {
            statusImage.removeAllSymbolEffects()
            waveformAnimating = false
        }
        if phase == .ready { spinner.startAnimating() } else { spinner.stopAnimating() }

        subtitleLabel.text = notice ?? context
        subtitleLabel.textColor = .secondaryLabel
        subtitleLabel.isHidden = subtitleLabel.text == nil

        transcriptLabel.textColor = transcript.isEmpty ? .secondaryLabel : .label
        transcriptLabel.accessibilityLabel = transcript.isEmpty ? placeholder : transcript

        let done: DoneState = recording ? .enabled : working ? .busy : .disabled
        contextButton.isHidden = !recording
        let key = [theme.isDark ? "dark" : "light", contextAttached ? "attached" : "-", "\(done)", strings.language.rawValue]
            .joined(separator: ":")
        if key != buttonsKey {
            buttonsKey = key
            cancelButton.configuration = circle("xmark", prominent: false)
            contextButton.configuration = circle(contextAttached ? "doc.on.clipboard.fill" : "doc.on.clipboard",
                                                 prominent: contextAttached)
            keyboardButton.configuration = circle("keyboard", prominent: false)
            doneButton.configuration = doneConfiguration(done)
        }
        doneButton.isEnabled = done != .disabled
        doneButton.isUserInteractionEnabled = done == .enabled

        cancelButton.accessibilityLabel = strings.stop
        contextButton.accessibilityLabel = strings.useCopiedMessage
        contextButton.accessibilityValue = contextAttached ? strings.attached : nil
        keyboardButton.accessibilityLabel = strings.showKeyboard
        doneButton.accessibilityLabel = strings.finish
        doneButton.accessibilityValue = done == .busy ? titleLabel.text : nil
        titleLabel.accessibilityLabel = [titleLabel.text, subtitleLabel.text].compactMap { $0 }.joined(separator: ". ")
        accessibilityElements = [cancelButton, titleLabel, contextButton, transcriptLabel, keyboardButton, doneButton]
            .filter { !$0.isHidden }
        setNeedsLayout()
    }

    /// The filled accent button for the main actions: white on blue, as in
    /// iOS (prominent glass tints its label instead). The others are Liquid
    /// Glass on iOS 26 and key-coloured before, like the rest of this keyboard.
    private func baseConfiguration(prominent: Bool) -> UIButton.Configuration {
        if #available(iOS 26.0, *), !prominent {
            var configuration = UIButton.Configuration.glass()
            configuration.baseForegroundColor = .label
            return configuration
        }
        var configuration = UIButton.Configuration.filled()
        configuration.baseBackgroundColor = prominent ? theme.accent : theme.letterKey
        configuration.baseForegroundColor = prominent ? .white : .label
        return configuration
    }

    private func circle(_ symbol: String, prominent: Bool) -> UIButton.Configuration {
        var configuration = baseConfiguration(prominent: prominent)
        configuration.cornerStyle = .capsule
        configuration.contentInsets = .zero
        configuration.image = UIImage(systemName: symbol, withConfiguration: Self.symbol)
        return configuration
    }

    private func doneConfiguration(_ state: DoneState) -> UIButton.Configuration {
        var configuration = baseConfiguration(prominent: true)
        configuration.cornerStyle = .capsule
        configuration.buttonSize = .large
        if state == .busy {
            configuration.showsActivityIndicator = true
        } else {
            configuration.image = UIImage(systemName: "checkmark", withConfiguration: Self.symbol)
            configuration.imagePadding = 8
            var attributes = AttributeContainer()
            attributes.font = Self.buttonFont
            configuration.attributedTitle = AttributedString(strings.finishShort, attributes: attributes)
            configuration.titleLineBreakMode = .byTruncatingTail
        }
        return configuration
    }

    // MARK: Layout

    override func layoutSubviews() {
        super.layoutSubviews()
        // Landscape and small phones get the same panel, a little tighter.
        let compact = bounds.height < 236
        let control: CGFloat = compact ? 40 : 44
        let doneHeight: CGFloat = compact ? 44 : 50
        let gap: CGFloat = compact ? 6 : 12
        let contentWidth = min(bounds.width, 600)
        let minX = ((bounds.width - contentWidth) / 2).rounded() + 16
        let maxX = minX + contentWidth - 32
        let top: CGFloat = compact ? 6 : 10
        let bottom = bounds.height - (compact ? 6 : 10)

        cancelButton.frame = CGRect(x: minX, y: top, width: control, height: control)
        contextButton.frame = CGRect(x: maxX - control, y: top, width: control, height: control)

        // The status, centred between them, with the message below it.
        let titleMinX = cancelButton.frame.maxX + 8
        let available = max(0, contextButton.frame.minX - 8 - titleMinX)
        let icon: CGFloat = (!statusImage.isHidden || spinner.isAnimating) ? 28 : 0
        let titleHeight = ceil(Self.titleFont.lineHeight)
        let titleWidth = min(ceil(titleLabel.intrinsicContentSize.width), max(0, available - icon))
        var subtitleSize = CGSize.zero
        if !subtitleLabel.isHidden {
            let fitted = subtitleLabel.sizeThatFits(CGSize(width: available, height: .greatestFiniteMagnitude))
            subtitleSize = CGSize(width: min(ceil(fitted.width), available),
                                  height: min(ceil(fitted.height), ceil(Self.subtitleFont.lineHeight * 2)))
        }
        let blockHeight = titleHeight + (subtitleLabel.isHidden ? 0 : 2 + subtitleSize.height)
        let blockTop = top + max(0, ((control - blockHeight) / 2).rounded())
        let lineX = (titleMinX + (available - icon - titleWidth) / 2).rounded()
        statusImage.frame = CGRect(x: lineX, y: blockTop + (titleHeight - 22) / 2, width: 22, height: 22)
        spinner.center = CGPoint(x: statusImage.frame.midX, y: statusImage.frame.midY)
        titleLabel.frame = CGRect(x: lineX + icon, y: blockTop, width: titleWidth, height: titleHeight)
        subtitleLabel.frame = CGRect(x: (titleMinX + (available - subtitleSize.width) / 2).rounded(),
                                     y: titleLabel.frame.maxY + 2, width: subtitleSize.width, height: subtitleSize.height)
        let headerBottom = max(cancelButton.frame.maxY, subtitleLabel.isHidden ? titleLabel.frame.maxY : subtitleLabel.frame.maxY)

        keyboardButton.frame = CGRect(x: minX, y: bottom - doneHeight, width: doneHeight, height: doneHeight)
        doneButton.frame = CGRect(x: keyboardButton.frame.maxX + 12, y: bottom - doneHeight,
                                  width: max(0, maxX - keyboardButton.frame.maxX - 12), height: doneHeight)

        // As many lines as fit; once the words outgrow them, the newest stay.
        let font = Self.transcriptFont
        let textTop = headerBottom + gap
        let textHeight = max(0, doneButton.frame.minY - gap - textTop)
        let textWidth = max(0, maxX - minX - 8)
        let lines = max(1, Int((textHeight / font.lineHeight).rounded(.down)))
        transcriptLabel.numberOfLines = lines
        transcriptLabel.text = transcript.isEmpty ? placeholder
            : VoiceCaption.newestWords(of: transcript, fitting: textWidth, lines: lines, font: font)
        let fitted = transcriptLabel.sizeThatFits(CGSize(width: textWidth, height: .greatestFiniteMagnitude))
        transcriptLabel.frame = CGRect(x: minX + 4, y: textTop, width: textWidth, height: min(ceil(fitted.height), textHeight))
    }

    // MARK: Actions

    @objc private func cancelTapped() { onAction?(.stop) }
    @objc private func contextTapped() { onAction?(.pasteContext) }
    @objc private func keyboardTapped() { onShowKeyboard?() }

    @objc private func doneTapped() {
        guard phase.isRecording, !finishing else { return }
        // Done answers at once; the app's next update confirms it.
        finishing = true
        render()
        onAction?(.finish)
        // Should the app not take it, Done comes back for another tap.
        let session = sessionID
        DispatchQueue.main.asyncAfter(deadline: .now() + 4) { [weak self] in
            guard let self, self.finishing, self.sessionID == session, self.phase.isRecording else { return }
            self.finishing = false
            self.render()
        }
    }
}

extension KeyboardVoiceStrings {
    var showKeyboard: String {
        switch language {
        case .english: return "Show keyboard"
        case .russian: return "Показать клавиатуру"
        case .kazakh: return "Пернетақтаны көрсету"
        case .uzbek: return "Klaviaturani ko‘rsatish"
        }
    }
}
