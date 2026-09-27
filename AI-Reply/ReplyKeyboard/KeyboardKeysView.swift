import UIKit

protocol KeyboardKeysViewDelegate: AnyObject {
    func keysView(_ view: KeyboardKeysView, didType text: String)
    func keysViewDidTapShift(_ view: KeyboardKeysView)
    func keysView(_ view: KeyboardKeysView, didDelete unit: KeyboardKeysView.DeleteUnit)
    func keysViewDidTapSpace(_ view: KeyboardKeysView)
    func keysViewDidTapReturn(_ view: KeyboardKeysView)
    func keysView(_ view: KeyboardKeysView, didSelectPlane plane: KeyboardPlane)
    func keysViewDidTapNextLanguage(_ view: KeyboardKeysView)
    func keysView(_ view: KeyboardKeysView, didPickLanguage language: KeyboardLanguage)
    /// Every touch event on the globe, for `handleInputModeList(from:with:)`.
    func keysView(_ view: KeyboardKeysView, didSendNextKeyboardEvent event: UIEvent, from keyView: UIView)
    /// VoiceOver activation of the globe, which has no touch event to forward.
    func keysViewDidRequestNextKeyboard(_ view: KeyboardKeysView)
    /// Space-bar trackpad: move the caret by this many characters.
    func keysView(_ view: KeyboardKeysView, moveCursorBy offset: Int)
}

/// The key area: draws a page of keys and turns touches into key presses.
///
/// WHY NOT A BUTTON PER KEY. The previous keyboard was rows of `UIButton`s in
/// stack views, so a touch in the 6pt gap between two keys - or in the empty
/// margins either side of a short row - landed on no button at all and was
/// silently lost. Here every touch goes to this one view and is resolved
/// against the page's hit frames (`KeyboardPageLayout`), which tile the whole
/// area: a finger always gets the nearest key.
///
/// Behaviour follows the iOS keyboard:
/// * letters type on release, with the key balloon while the finger is down,
///   and slide-to-correct until then; a second finger arriving commits the
///   first key immediately, so fast two-thumb typing never drops a letter;
/// * a held letter with alternates (`е` → `ё`, `ь` → `ъ`, accented Latin)
///   opens them after a short hold;
/// * shift acts on touch-down; delete deletes on touch-down, repeats after a
///   hold and then accelerates to whole words;
/// * holding the space bar turns the keyboard into a trackpad for the caret;
/// * the layout key cycles on tap and opens a picker on hold;
/// * the globe forwards every touch to the system.
final class KeyboardKeysView: UIView {

    enum DeleteUnit {
        case character
        case word
    }

    weak var delegate: KeyboardKeysViewDelegate?

    /// Points available above this view for balloons and popups, so the top
    /// row's balloon can rise into the bar above instead of being clipped.
    var headroom: CGFloat = 0

    /// Key-down haptics. Set by the controller: on only with Full Access.
    var hapticsEnabled = false

    /// While a request is running the keys have nowhere to type into. They
    /// dim and stay silent, and only the keys that change the keyboard itself
    /// keep working.
    var isInputDimmed = false {
        didSet {
            guard isInputDimmed != oldValue else { return }
            for (index, cap) in caps.enumerated() where layout?.keys[index].action.editsText == true {
                cap.alpha = isInputDimmed ? 0.45 : 1
            }
        }
    }

    private(set) var layout: KeyboardPageLayout?
    private var caps: [KeyCapView] = []
    private var theme = KeyboardTheme(isDark: false)
    private var labels = KeyboardLabels(.english)
    private var shiftMode: ShiftState.Mode = .off
    private var returnFace: KeyboardLabels.ReturnFace = .text("return")
    private var returnProminent = false
    private var returnEnabled = true
    private var spaceOverride: String?
    private var languages: [KeyboardLanguage] = []
    private var currentLanguage: KeyboardLanguage = .english

    private let popup = KeyPopupView()
    private let optionsPanel = KeyOptionsPanel()
    private lazy var haptics = UIImpactFeedbackGenerator(style: .light)

    // MARK: Touch state

    private enum TouchMode {
        case key
        case alternates
        case languagePicker
        case trackpad(lastX: CGFloat)
        case delete
        case nextKeyboard
        /// Already handled (committed by a second finger, or cancelled).
        case finished
    }

    private final class Track {
        var keyIndex: Int
        let startIndex: Int
        var mode: TouchMode
        var timer: Timer?
        var repeats = 0

        init(keyIndex: Int, mode: TouchMode) {
            self.keyIndex = keyIndex
            self.startIndex = keyIndex
            self.mode = mode
        }

        func stopTimer() {
            timer?.invalidate()
            timer = nil
        }
    }

    private var tracks: [ObjectIdentifier: Track] = [:]
    /// The touch that owns the balloon / options panel right now.
    private var popupOwner: ObjectIdentifier?

    // MARK: Init

    override init(frame: CGRect) {
        super.init(frame: frame)
        isMultipleTouchEnabled = true
        clipsToBounds = false
        popup.isHidden = true
        optionsPanel.isHidden = true
        addSubview(popup)
        addSubview(optionsPanel)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not used")
    }

    deinit {
        for track in tracks.values { track.stopTimer() }
    }

    // MARK: Configuration

    func configure(theme: KeyboardTheme) {
        self.theme = theme
        popup.theme = theme
        optionsPanel.theme = theme
        restyleAll()
    }

    /// Installs a page. Cheap: key caps are plain views positioned by frame,
    /// so a page switch is one pass with no Auto Layout involved.
    func show(
        _ layout: KeyboardPageLayout,
        labels: KeyboardLabels,
        languages: [KeyboardLanguage],
        currentLanguage: KeyboardLanguage
    ) {
        cancelAllTracks()
        self.layout = layout
        self.labels = labels
        self.languages = languages
        self.currentLanguage = currentLanguage

        caps.forEach { $0.removeFromSuperview() }
        caps = layout.keys.map { key in
            let cap = KeyCapView()
            cap.frame = key.frame
            cap.cornerRadius = cornerRadius(for: layout)
            insertSubview(cap, belowSubview: popup)
            return cap
        }
        restyleAll()
        rebuildAccessibility()
    }

    func setShiftMode(_ mode: ShiftState.Mode) {
        guard mode != shiftMode else { return }
        shiftMode = mode
        guard let layout else { return }
        for (index, key) in layout.keys.enumerated() {
            switch key.action {
            case .character, .shift:
                style(caps[index], for: key)
            default:
                continue
            }
        }
        rebuildAccessibility()
    }

    func setReturnKey(_ face: KeyboardLabels.ReturnFace, prominent: Bool, enabled: Bool) {
        guard face != returnFace || prominent != returnProminent || enabled != returnEnabled else { return }
        returnFace = face
        returnProminent = prominent
        returnEnabled = enabled
        restyle { $0 == .returnKey }
    }

    /// Temporarily replaces the space bar's caption - the layout's name right
    /// after a switch, as iOS shows it. nil restores "space".
    func setSpaceCaption(_ caption: String?) {
        guard caption != spaceOverride else { return }
        spaceOverride = caption
        restyle { $0 == .space }
    }

    private func restyle(where matches: (KeyAction) -> Bool) {
        guard let layout else { return }
        for (index, key) in layout.keys.enumerated() where matches(key.action) {
            style(caps[index], for: key)
        }
        rebuildAccessibility()
    }

    private func restyleAll() {
        guard let layout else { return }
        UIView.performWithoutAnimation {
            for (index, key) in layout.keys.enumerated() {
                style(caps[index], for: key)
            }
        }
    }

    private func cornerRadius(for layout: KeyboardPageLayout) -> CGFloat {
        layout.rowMetrics.key >= 40 ? 6 : 5
    }

    // MARK: Key faces

    private var isShifted: Bool { shiftMode != .off }

    private func displayText(for character: String) -> String {
        guard layout?.spec.plane == .letters, isShifted else { return character }
        return character.uppercased()
    }

    private func characterFontSize(for layout: KeyboardPageLayout) -> CGFloat {
        let width = layout.keys.first(where: { $0.action.isCharacter })?.frame.width ?? 32
        let height = layout.rowMetrics.key
        if height < 36 { return 20 }
        return width < 31 ? 22 : 23.5
    }

    private func style(_ cap: KeyCapView, for key: LaidOutKey) {
        guard let layout else { return }
        cap.theme = theme
        cap.alpha = isInputDimmed && key.action.editsText ? 0.45 : 1
        let controlFont = UIFont.systemFont(ofSize: 16, weight: .regular)

        switch key.action {
        case .character(let value):
            cap.style = .letter
            if value.count > 1 {
                cap.setText(value, font: controlFont)
            } else {
                let size = characterFontSize(for: layout)
                cap.setText(displayText(for: value), font: .systemFont(ofSize: size, weight: .regular))
            }
            cap.accessibilityText = displayText(for: value)

        case .shift:
            switch shiftMode {
            case .off:
                cap.style = .special
                cap.setSymbol("shift", pointSize: 18, weight: .regular)
                cap.accessibilityText = labels.shiftAccessibility
            case .once:
                cap.style = .engaged
                cap.setSymbol("shift.fill", pointSize: 18, weight: .regular)
                cap.accessibilityText = labels.shiftAccessibility
            case .locked:
                cap.style = .engaged
                cap.setSymbol("capslock.fill", pointSize: 18, weight: .regular)
                cap.accessibilityText = labels.capsLockAccessibility
            }

        case .delete:
            cap.style = .special
            cap.setSymbol("delete.left", pointSize: 18, weight: .regular)
            cap.accessibilityText = labels.deleteAccessibility

        case .space:
            cap.style = .letter
            cap.setText(spaceOverride ?? labels.space, font: controlFont)
            cap.accessibilityText = labels.space

        case .returnKey:
            cap.style = returnEnabled ? (returnProminent ? .prominent : .special) : .disabled
            switch returnFace {
            case .text(let text):
                cap.setText(text, font: controlFont)
                cap.accessibilityText = text
            case .symbol(let name, let spoken):
                cap.setSymbol(name, pointSize: 18, weight: .regular)
                cap.accessibilityText = spoken
            }

        case .plane(let plane):
            cap.style = .special
            cap.setText(labels.planeKey(plane), font: controlFont)
            cap.accessibilityText = labels.planeKey(plane)

        case .nextLanguage:
            cap.style = .special
            cap.setText(labels.badge, font: .systemFont(ofSize: 14, weight: .semibold))
            cap.accessibilityText = labels.nextLanguageAccessibility

        case .nextKeyboard:
            cap.style = .special
            cap.setSymbol("globe", pointSize: 18, weight: .regular)
            cap.accessibilityText = labels.nextKeyboardAccessibility
        }
    }

    // MARK: Touches

    override func touchesBegan(_ touches: Set<UITouch>, with event: UIEvent?) {
        guard let layout else { return }
        for touch in touches {
            let point = touch.location(in: self)
            guard let index = layout.keyIndex(at: point) else { continue }
            begin(touch: touch, keyIndex: index, event: event)
        }
    }

    override func touchesMoved(_ touches: Set<UITouch>, with event: UIEvent?) {
        guard let layout else { return }
        for touch in touches {
            guard let track = tracks[ObjectIdentifier(touch)] else { continue }
            let point = touch.location(in: self)
            switch track.mode {
            case .key:
                let key = layout.keys[track.startIndex]
                guard key.action.isCharacter, let index = layout.keyIndex(at: point) else { continue }
                // Slide to correct: a letter follows the finger until release.
                if index != track.keyIndex, layout.keys[index].action.isCharacter {
                    caps[track.keyIndex].isPressed = false
                    track.keyIndex = index
                    track.stopTimer()
                    press(index: index, track: track, touch: touch)
                }
            case .alternates, .languagePicker:
                optionsPanel.select(at: convert(point, to: optionsPanel))
            case .trackpad(let lastX):
                let step: CGFloat = 9
                let delta = point.x - lastX
                if abs(delta) >= step {
                    let moves = Int(delta / step)
                    delegate?.keysView(self, moveCursorBy: moves)
                    track.mode = .trackpad(lastX: lastX + CGFloat(moves) * step)
                }
            case .nextKeyboard:
                if let event { delegate?.keysView(self, didSendNextKeyboardEvent: event, from: caps[track.startIndex]) }
            case .delete, .finished:
                continue
            }
        }
    }

    override func touchesEnded(_ touches: Set<UITouch>, with event: UIEvent?) {
        for touch in touches {
            end(touch: touch, event: event, cancelled: false)
        }
    }

    override func touchesCancelled(_ touches: Set<UITouch>, with event: UIEvent?) {
        for touch in touches {
            end(touch: touch, event: event, cancelled: true)
        }
    }

    private func begin(touch: UITouch, keyIndex index: Int, event: UIEvent?) {
        guard let layout else { return }
        let key = layout.keys[index]
        let id = ObjectIdentifier(touch)

        // Rollover: a new key while a letter is still held commits the held
        // letter now, exactly once, in the order the fingers came down.
        for (otherID, other) in tracks where otherID != id {
            if case .key = other.mode, layout.keys[other.keyIndex].action.isCharacter {
                commitCharacter(of: other)
                other.mode = .finished
            }
        }

        if isInputDimmed, key.action.editsText { return }
        feedback(for: key.action)

        switch key.action {
        case .character:
            let track = Track(keyIndex: index, mode: .key)
            tracks[id] = track
            press(index: index, track: track, touch: touch)

        case .shift:
            caps[index].isPressed = true
            tracks[id] = Track(keyIndex: index, mode: .finished)
            delegate?.keysViewDidTapShift(self)

        case .delete:
            caps[index].isPressed = true
            let track = Track(keyIndex: index, mode: .delete)
            tracks[id] = track
            delegate?.keysView(self, didDelete: .character)
            scheduleDeleteRepeat(track)

        case .space:
            caps[index].isPressed = true
            let track = Track(keyIndex: index, mode: .key)
            tracks[id] = track
            track.timer = scheduledTimer(after: 0.45) { [weak self, weak track] in
                guard let self, let track else { return }
                self.enterTrackpad(track, touch: touch)
            }

        case .nextLanguage:
            caps[index].isPressed = true
            let track = Track(keyIndex: index, mode: .key)
            tracks[id] = track
            if languages.count > 1 {
                track.timer = scheduledTimer(after: 0.4) { [weak self, weak track] in
                    guard let self, let track else { return }
                    self.openLanguagePicker(track, touch: touch)
                }
            }

        case .nextKeyboard:
            caps[index].isPressed = true
            tracks[id] = Track(keyIndex: index, mode: .nextKeyboard)
            if let event { delegate?.keysView(self, didSendNextKeyboardEvent: event, from: caps[index]) }

        case .returnKey, .plane:
            caps[index].isPressed = true
            tracks[id] = Track(keyIndex: index, mode: .key)
        }
    }

    private func end(touch: UITouch, event: UIEvent?, cancelled: Bool) {
        let id = ObjectIdentifier(touch)
        guard let track = tracks.removeValue(forKey: id), let layout else { return }
        track.stopTimer()
        let point = touch.location(in: self)

        defer {
            if track.keyIndex < caps.count { caps[track.keyIndex].isPressed = false }
            if track.startIndex < caps.count { caps[track.startIndex].isPressed = false }
            if popupOwner == id { hidePopups() }
        }

        switch track.mode {
        case .finished:
            return

        case .delete:
            return

        case .nextKeyboard:
            if let event { delegate?.keysView(self, didSendNextKeyboardEvent: event, from: caps[track.startIndex]) }
            return

        case .trackpad:
            setTrackpadAppearance(false)
            return

        case .alternates:
            guard !cancelled, let choice = optionsPanel.selectedOption else { return }
            delegate?.keysView(self, didType: choice)
            return

        case .languagePicker:
            guard !cancelled, let choice = optionsPanel.selectedIndex, languages.indices.contains(choice) else { return }
            delegate?.keysView(self, didPickLanguage: languages[choice])
            return

        case .key:
            guard !cancelled else { return }
            let key = layout.keys[track.keyIndex]
            switch key.action {
            case .character:
                commitCharacter(of: track)
            case .space, .returnKey, .plane, .nextLanguage:
                // Controls fire only if released on themselves: sliding off
                // a key is how a user changes their mind.
                guard layout.keyIndex(at: point) == track.startIndex else { return }
                fire(key.action)
            case .shift, .delete, .nextKeyboard:
                return
            }
        }
    }

    private func fire(_ action: KeyAction) {
        switch action {
        case .space: delegate?.keysViewDidTapSpace(self)
        case .returnKey: delegate?.keysViewDidTapReturn(self)
        case .plane(let plane): delegate?.keysView(self, didSelectPlane: plane)
        case .nextLanguage: delegate?.keysViewDidTapNextLanguage(self)
        case .shift: delegate?.keysViewDidTapShift(self)
        case .delete: delegate?.keysView(self, didDelete: .character)
        case .nextKeyboard: delegate?.keysViewDidRequestNextKeyboard(self)
        case .character(let value): delegate?.keysView(self, didType: value)
        }
    }

    private func commitCharacter(of track: Track) {
        guard let layout, case .character(let value) = layout.keys[track.keyIndex].action else { return }
        track.stopTimer()
        if let owner = popupOwner, tracks[owner] === track { hidePopups() }
        caps[track.keyIndex].isPressed = false
        delegate?.keysView(self, didType: value)
    }

    private func cancelAllTracks() {
        for track in tracks.values { track.stopTimer() }
        tracks.removeAll()
        hidePopups()
        setTrackpadAppearance(false)
        caps.forEach { $0.isPressed = false }
    }

    // MARK: Press, balloon, alternates

    private func press(index: Int, track: Track, touch: UITouch) {
        guard let layout else { return }
        let key = layout.keys[index]
        guard case .character(let value) = key.action else { return }
        caps[index].isPressed = true
        popupOwner = ObjectIdentifier(touch)
        optionsPanel.isHidden = true
        popup.show(text: displayText(for: value), over: key.frame, in: bounds, headroom: headroom,
                   font: .systemFont(ofSize: value.count > 1 ? 20 : 36, weight: .regular))
        bringSubviewToFront(popup)

        if !key.alternates.isEmpty {
            track.timer = scheduledTimer(after: 0.4) { [weak self, weak track] in
                guard let self, let track else { return }
                self.openAlternates(track, touch: touch)
            }
        }
    }

    private func openAlternates(_ track: Track, touch: UITouch) {
        guard let layout, tracks[ObjectIdentifier(touch)] === track else { return }
        let key = layout.keys[track.keyIndex]
        guard !key.alternates.isEmpty else { return }
        let options = key.alternates.map { displayText(for: $0) }
        track.mode = .alternates
        popup.isHidden = true
        popupOwner = ObjectIdentifier(touch)
        optionsPanel.showHorizontal(options: options, over: key.frame, in: bounds, headroom: headroom,
                                    keyHeight: layout.rowMetrics.key)
        bringSubviewToFront(optionsPanel)
        optionsPanel.select(at: convert(touch.location(in: self), to: optionsPanel))
        feedback(for: .shift)
    }

    private func openLanguagePicker(_ track: Track, touch: UITouch) {
        guard let layout, tracks[ObjectIdentifier(touch)] === track, languages.count > 1 else { return }
        let key = layout.keys[track.keyIndex]
        track.mode = .languagePicker
        popupOwner = ObjectIdentifier(touch)
        let names = languages.map(\.nativeName)
        let selected = languages.firstIndex(of: currentLanguage) ?? 0
        optionsPanel.showVertical(options: names, selected: selected, over: key.frame, in: bounds, headroom: headroom)
        bringSubviewToFront(optionsPanel)
        feedback(for: .shift)
    }

    private func hidePopups() {
        popup.isHidden = true
        optionsPanel.isHidden = true
        popupOwner = nil
    }

    // MARK: Delete repeat

    /// One on touch-down, then after a hold a steady repeat, then whole
    /// words - the pace iOS keeps.
    private func scheduleDeleteRepeat(_ track: Track) {
        track.timer = scheduledTimer(after: 0.45) { [weak self, weak track] in
            guard let self, let track else { return }
            self.repeatDelete(track)
        }
    }

    private func repeatDelete(_ track: Track) {
        guard tracks.values.contains(where: { $0 === track }) else { return }
        track.repeats += 1
        let words = track.repeats > 16
        delegate?.keysView(self, didDelete: words ? .word : .character)
        track.timer = scheduledTimer(after: words ? 0.2 : 0.085) { [weak self, weak track] in
            guard let self, let track else { return }
            self.repeatDelete(track)
        }
    }

    // MARK: Trackpad

    private func enterTrackpad(_ track: Track, touch: UITouch) {
        guard tracks[ObjectIdentifier(touch)] === track else { return }
        track.mode = .trackpad(lastX: touch.location(in: self).x)
        setTrackpadAppearance(true)
        feedback(for: .shift)
    }

    private func setTrackpadAppearance(_ active: Bool) {
        UIView.animate(withDuration: 0.15) {
            for cap in self.caps { cap.setContentHidden(active) }
        }
    }

    // MARK: Feedback

    private func feedback(for action: KeyAction) {
        UIDevice.current.playInputClick()
        guard hapticsEnabled else { return }
        haptics.impactOccurred(intensity: action.isCharacter ? 0.55 : 0.7)
        haptics.prepare()
    }

    private func scheduledTimer(after interval: TimeInterval, _ block: @escaping () -> Void) -> Timer {
        let timer = Timer(timeInterval: interval, repeats: false) { _ in block() }
        RunLoop.main.add(timer, forMode: .common)
        return timer
    }

    // MARK: Accessibility

    private func rebuildAccessibility() {
        guard let layout else {
            accessibilityElements = nil
            return
        }
        accessibilityElements = layout.keys.enumerated().map { index, key in
            let element = KeyAccessibilityElement(accessibilityContainer: self)
            element.accessibilityLabel = caps[index].accessibilityText
            element.accessibilityFrameInContainerSpace = key.frame
            var traits: UIAccessibilityTraits = .keyboardKey
            if key.action == .shift, shiftMode != .off { traits.insert(.selected) }
            element.accessibilityTraits = traits
            element.onActivate = { [weak self] in
                guard let self, !(self.isInputDimmed && key.action.editsText) else { return false }
                self.fire(key.action)
                return true
            }
            return element
        }
    }
}

private final class KeyAccessibilityElement: UIAccessibilityElement {
    var onActivate: (() -> Bool)?

    override func accessibilityActivate() -> Bool {
        onActivate?() ?? false
    }
}

extension KeyAction {
    /// Keys whose only job is to put text somewhere, or take it away.
    var editsText: Bool {
        switch self {
        case .character, .delete, .space, .returnKey: return true
        case .shift, .plane, .nextLanguage, .nextKeyboard: return false
        }
    }
}

// MARK: - Key cap

/// One drawn key. Not a control: `KeyboardKeysView` owns every touch.
final class KeyCapView: UIView {

    enum Style {
        case letter
        case special
        case engaged
        case prominent
        case disabled
    }

    var theme = KeyboardTheme(isDark: false) { didSet { applyColors() } }
    var style: Style = .letter { didSet { if style != oldValue { applyColors() } } }
    var isPressed = false { didSet { if isPressed != oldValue { applyColors() } } }
    var cornerRadius: CGFloat = 6 {
        didSet {
            layer.cornerRadius = cornerRadius
            setNeedsLayout()
        }
    }

    /// What VoiceOver reads for this key.
    var accessibilityText = ""

    private let label = UILabel()
    private let imageView = UIImageView()
    private var shadowBounds: CGRect = .null

    override init(frame: CGRect) {
        super.init(frame: frame)
        isUserInteractionEnabled = false
        isAccessibilityElement = false
        layer.cornerCurve = .continuous
        layer.cornerRadius = cornerRadius
        layer.shadowOpacity = 1
        layer.shadowRadius = 0
        layer.shadowOffset = CGSize(width: 0, height: 1)

        label.textAlignment = .center
        label.adjustsFontSizeToFitWidth = true
        label.minimumScaleFactor = 0.5
        label.baselineAdjustment = .alignCenters
        label.isHidden = true
        addSubview(label)

        imageView.contentMode = .center
        imageView.isHidden = true
        addSubview(imageView)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not used")
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        label.frame = bounds.insetBy(dx: 2, dy: 0)
        imageView.frame = bounds
        // An explicit shadow path: without one Core Animation derives the
        // shadow from the layer's alpha on every frame, one offscreen pass per
        // key. The shadow is a hard 1pt bottom edge, so the result is identical.
        if bounds != shadowBounds {
            shadowBounds = bounds
            layer.shadowPath = UIBezierPath(roundedRect: bounds, cornerRadius: cornerRadius).cgPath
        }
    }

    func setText(_ text: String, font: UIFont) {
        if label.text != text { label.text = text }
        if label.font != font { label.font = font }
        label.isHidden = false
        imageView.isHidden = true
    }

    func setSymbol(_ name: String, pointSize: CGFloat, weight: UIImage.SymbolWeight) {
        imageView.image = KeySymbolCache.image(name: name, pointSize: pointSize, weight: weight)
        imageView.isHidden = false
        label.isHidden = true
    }

    /// Trackpad mode blanks the keys, as iOS does.
    func setContentHidden(_ hidden: Bool) {
        label.alpha = hidden ? 0 : 1
        imageView.alpha = hidden ? 0 : 1
    }

    private func applyColors() {
        let background: UIColor
        let foreground: UIColor
        switch style {
        case .letter:
            background = isPressed ? theme.letterKeyPressed : theme.letterKey
            foreground = theme.primaryText
        case .special:
            background = isPressed ? theme.specialKeyPressed : theme.specialKey
            foreground = theme.primaryText
        case .engaged:
            background = theme.engagedKey
            foreground = theme.engagedKeyGlyph
        case .prominent:
            background = isPressed ? theme.accent.withAlphaComponent(0.7) : theme.accent
            foreground = .white
        case .disabled:
            background = theme.specialKey
            foreground = theme.secondaryText
        }
        backgroundColor = background
        label.textColor = foreground
        imageView.tintColor = foreground
        layer.shadowColor = theme.keyShadow.cgColor
    }
}

/// SF Symbol images, resolved once per configuration: `UIImage(systemName:)`
/// renders from the asset catalog on every miss.
enum KeySymbolCache {
    private struct Key: Hashable {
        let name: String
        let pointSize: CGFloat
        let weight: Int
    }
    private static var cache: [Key: UIImage] = [:]

    static func image(name: String, pointSize: CGFloat, weight: UIImage.SymbolWeight) -> UIImage? {
        let key = Key(name: name, pointSize: pointSize.rounded(), weight: weight.rawValue)
        if let cached = cache[key] { return cached }
        let configuration = UIImage.SymbolConfiguration(pointSize: pointSize, weight: weight)
        guard let image = UIImage(systemName: name, withConfiguration: configuration) else { return nil }
        cache[key] = image
        return image
    }
}

// MARK: - Balloon

/// The key balloon: the pressed letter, large, above the finger.
final class KeyPopupView: UIView {

    var theme = KeyboardTheme(isDark: false)

    private let shape = CAShapeLayer()
    private let label = UILabel()

    override init(frame: CGRect) {
        super.init(frame: frame)
        isUserInteractionEnabled = false
        layer.addSublayer(shape)
        shape.shadowOpacity = 0.28
        shape.shadowRadius = 3
        shape.shadowOffset = CGSize(width: 0, height: 1)
        label.textAlignment = .center
        label.adjustsFontSizeToFitWidth = true
        label.minimumScaleFactor = 0.5
        addSubview(label)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not used")
    }

    /// - Parameters:
    ///   - key: the pressed key, in the coordinate space of the parent.
    ///   - container: the parent's bounds; the balloon stays inside them
    ///     horizontally.
    ///   - headroom: how far above the parent the balloon may reach.
    func show(text: String, over key: CGRect, in container: CGRect, headroom: CGFloat, font: UIFont) {
        let topWidth = min(max(key.width * 1.5, key.width + 18), key.width + 30)
        var topHeight = key.height + 6
        let neck: CGFloat = 10

        // Keep inside the keyboard: on an edge key the balloon leans inwards.
        var topMinX = key.midX - topWidth / 2
        topMinX = max(container.minX + 1, min(topMinX, container.maxX - 1 - topWidth))

        var topMinY = key.minY - neck - topHeight
        let ceiling = container.minY - headroom + 1
        if topMinY < ceiling {
            topHeight = max(key.height * 0.8, topHeight - (ceiling - topMinY))
            topMinY = max(ceiling, key.minY - neck - topHeight)
        }

        let top = CGRect(x: topMinX, y: topMinY, width: topWidth, height: topHeight)
        let outer = top.union(key)
        frame = outer

        let local = { (rect: CGRect) in rect.offsetBy(dx: -outer.minX, dy: -outer.minY) }
        shape.path = Self.path(top: local(top), key: local(key), neck: neck).cgPath
        shape.fillColor = theme.letterKey.cgColor
        shape.shadowColor = UIColor.black.cgColor
        shape.frame = bounds

        label.font = font
        label.textColor = theme.primaryText
        label.text = text
        label.frame = local(top).insetBy(dx: 2, dy: 2)
        isHidden = false
    }

    private static func path(top: CGRect, key: CGRect, neck: CGFloat) -> UIBezierPath {
        let radius: CGFloat = 9
        let keyRadius: CGFloat = 6
        let path = UIBezierPath()
        let neckBottom = key.minY + min(neck, key.height / 3)

        path.move(to: CGPoint(x: top.minX, y: top.minY + radius))
        path.addArc(withCenter: CGPoint(x: top.minX + radius, y: top.minY + radius), radius: radius,
                    startAngle: .pi, endAngle: 1.5 * .pi, clockwise: true)
        path.addLine(to: CGPoint(x: top.maxX - radius, y: top.minY))
        path.addArc(withCenter: CGPoint(x: top.maxX - radius, y: top.minY + radius), radius: radius,
                    startAngle: 1.5 * .pi, endAngle: 0, clockwise: true)
        path.addLine(to: CGPoint(x: top.maxX, y: top.maxY))
        path.addCurve(to: CGPoint(x: key.maxX, y: neckBottom),
                      controlPoint1: CGPoint(x: top.maxX, y: top.maxY + (neckBottom - top.maxY) * 0.6),
                      controlPoint2: CGPoint(x: key.maxX, y: top.maxY + (neckBottom - top.maxY) * 0.4))
        path.addLine(to: CGPoint(x: key.maxX, y: key.maxY - keyRadius))
        path.addArc(withCenter: CGPoint(x: key.maxX - keyRadius, y: key.maxY - keyRadius), radius: keyRadius,
                    startAngle: 0, endAngle: 0.5 * .pi, clockwise: true)
        path.addLine(to: CGPoint(x: key.minX + keyRadius, y: key.maxY))
        path.addArc(withCenter: CGPoint(x: key.minX + keyRadius, y: key.maxY - keyRadius), radius: keyRadius,
                    startAngle: 0.5 * .pi, endAngle: .pi, clockwise: true)
        path.addLine(to: CGPoint(x: key.minX, y: neckBottom))
        path.addCurve(to: CGPoint(x: top.minX, y: top.maxY),
                      controlPoint1: CGPoint(x: key.minX, y: top.maxY + (neckBottom - top.maxY) * 0.4),
                      controlPoint2: CGPoint(x: top.minX, y: top.maxY + (neckBottom - top.maxY) * 0.6))
        path.close()
        return path
    }
}

// MARK: - Options panel

/// Long-press alternates (a row) and the layout picker (a column). The finger
/// that opened it chooses by sliding; lifting it picks the highlighted option.
final class KeyOptionsPanel: UIView {

    var theme = KeyboardTheme(isDark: false)

    private var cells: [UILabel] = []
    private(set) var selectedIndex: Int?
    private(set) var options: [String] = []
    private var isVertical = false

    var selectedOption: String? {
        selectedIndex.flatMap { options.indices.contains($0) ? options[$0] : nil }
    }

    override init(frame: CGRect) {
        super.init(frame: frame)
        isUserInteractionEnabled = false
        layer.cornerRadius = 9
        layer.cornerCurve = .continuous
        layer.shadowOpacity = 0.3
        layer.shadowRadius = 4
        layer.shadowOffset = CGSize(width: 0, height: 1)
        layer.shadowColor = UIColor.black.cgColor
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not used")
    }

    func showHorizontal(options: [String], over key: CGRect, in container: CGRect, headroom: CGFloat, keyHeight: CGFloat) {
        isVertical = false
        let cellWidth = max(key.width, 32)
        let cellHeight = min(max(keyHeight, 38), 50)
        let padding: CGFloat = 4
        let width = CGFloat(options.count) * cellWidth + padding * 2
        let height = cellHeight + padding * 2

        // Start over the key and grow towards the side with more room.
        var minX = key.minX - padding
        if minX + width > container.maxX - 1 { minX = key.maxX + padding - width }
        minX = max(container.minX + 1, min(minX, container.maxX - 1 - width))
        let minY = max(container.minY - headroom + 1, key.minY - height - 4)

        frame = CGRect(x: minX, y: minY, width: width, height: height)
        build(options: options, font: .systemFont(ofSize: options.contains { $0.count > 1 } ? 17 : 26))
        for (index, cell) in cells.enumerated() {
            cell.frame = CGRect(x: padding + CGFloat(index) * cellWidth, y: padding, width: cellWidth, height: cellHeight)
        }
        select(index: 0)
        isHidden = false
    }

    func showVertical(options: [String], selected: Int, over key: CGRect, in container: CGRect, headroom: CGFloat) {
        isVertical = true
        let rowHeight: CGFloat = 40
        let padding: CGFloat = 5
        let width: CGFloat = 150
        let height = CGFloat(options.count) * rowHeight + padding * 2

        var minX = key.minX
        minX = max(container.minX + 1, min(minX, container.maxX - 1 - width))
        let minY = max(container.minY - headroom + 1, key.minY - height - 6)

        frame = CGRect(x: minX, y: minY, width: width, height: height)
        build(options: options, font: .systemFont(ofSize: 17, weight: .regular))
        for (index, cell) in cells.enumerated() {
            cell.frame = CGRect(x: padding, y: padding + CGFloat(index) * rowHeight, width: width - padding * 2, height: rowHeight)
            cell.textAlignment = .left
            cell.text = "  " + options[index]
        }
        select(index: selected)
        isHidden = false
    }

    /// `point` in this view's coordinates.
    func select(at point: CGPoint) {
        guard !cells.isEmpty else { return }
        var best = 0
        var bestDistance = CGFloat.greatestFiniteMagnitude
        for (index, cell) in cells.enumerated() {
            let distance = isVertical ? abs(cell.frame.midY - point.y) : abs(cell.frame.midX - point.x)
            if distance < bestDistance {
                best = index
                bestDistance = distance
            }
        }
        select(index: best)
    }

    private func select(index: Int) {
        selectedIndex = cells.indices.contains(index) ? index : nil
        for (position, cell) in cells.enumerated() {
            let selected = position == selectedIndex
            cell.backgroundColor = selected ? theme.accent : .clear
            cell.textColor = selected ? .white : theme.primaryText
        }
    }

    private func build(options: [String], font: UIFont) {
        self.options = options
        backgroundColor = theme.letterKey
        cells.forEach { $0.removeFromSuperview() }
        cells = options.map { option in
            let cell = UILabel()
            cell.text = option
            cell.font = font
            cell.textAlignment = .center
            cell.adjustsFontSizeToFitWidth = true
            cell.minimumScaleFactor = 0.6
            cell.layer.cornerRadius = 7
            cell.layer.cornerCurve = .continuous
            cell.clipsToBounds = true
            addSubview(cell)
            return cell
        }
        selectedIndex = nil
    }
}
