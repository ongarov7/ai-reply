import UIKit

/// A text field the KEYBOARD edits, without ever becoming first responder.
///
/// WHY. The previous composer called `becomeFirstResponder()` on its text
/// views and relied on UIKit's caret. Inside a keyboard extension that is not
/// something the system promises: when it was refused, Edit showed a reply
/// that could not be edited, and when it was granted the host app's field
/// could lose its connection to the keyboard. Here the keys edit the text
/// directly (`insert`, `deleteBackward`, …), the caret is drawn by this view,
/// and a tap places it with TextKit - so editing any part of the reply works
/// the same in every host app.
final class ComposerTextView: UITextView {

    /// Caret position, in UTF-16 offsets of `text`.
    private(set) var caret = 0

    var showsCaret = false {
        didSet {
            guard showsCaret != oldValue else { return }
            updateCaret()
        }
    }

    var placeholder = "" {
        didSet { placeholderLabel.text = placeholder }
    }

    /// How many lines the placeholder may wrap to. One - shrinking to fit -
    /// by default; the Create field's placeholder carries an example and
    /// wraps instead of being cut.
    var placeholderLines = 1 {
        didSet {
            guard placeholderLines != oldValue else { return }
            placeholderLabel.numberOfLines = placeholderLines
            placeholderLabel.adjustsFontSizeToFitWidth = placeholderLines == 1
            setNeedsLayout()
        }
    }

    var caretColor: UIColor = .systemBlue {
        didSet { caretView.backgroundColor = caretColor }
    }

    var placeholderColor: UIColor = .secondaryLabel {
        didSet { placeholderLabel.textColor = placeholderColor }
    }

    private let caretView = UIView()
    private let placeholderLabel = UILabel()

    var currentText: String { text ?? "" }

    // MARK: Init

    init(font: UIFont, inset: UIEdgeInsets) {
        // TextKit 1, explicitly: caret and tap positions come from
        // `NSLayoutManager`, which works whether or not the view is editable.
        let storage = NSTextStorage()
        let layoutManager = NSLayoutManager()
        let container = NSTextContainer(size: CGSize(width: 0, height: CGFloat.greatestFiniteMagnitude))
        container.widthTracksTextView = true
        container.lineFragmentPadding = 0
        layoutManager.addTextContainer(container)
        storage.addLayoutManager(layoutManager)
        super.init(frame: .zero, textContainer: container)

        self.font = font
        textContainerInset = inset
        backgroundColor = .clear
        isEditable = false
        isSelectable = false
        isScrollEnabled = true
        alwaysBounceVertical = false
        showsHorizontalScrollIndicator = false
        dataDetectorTypes = []
        isAccessibilityElement = true
        accessibilityTraits = .staticText

        caretView.isUserInteractionEnabled = false
        caretView.layer.cornerRadius = 1
        caretView.isHidden = true
        addSubview(caretView)

        placeholderLabel.font = font
        placeholderLabel.numberOfLines = 1
        placeholderLabel.adjustsFontSizeToFitWidth = true
        placeholderLabel.minimumScaleFactor = 0.75
        placeholderLabel.isUserInteractionEnabled = false
        addSubview(placeholderLabel)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not used")
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        let inset = textContainerInset
        let width = max(0, bounds.width - inset.left - inset.right)
        let lineHeight = (font ?? .systemFont(ofSize: 16)).lineHeight
        var height = lineHeight
        if placeholderLines > 1 {
            // Top-aligned with the caret: only as tall as the text it wraps to.
            let room = min(lineHeight * CGFloat(placeholderLines), max(lineHeight, bounds.height - inset.top - inset.bottom))
            height = min(room, ceil(placeholderLabel.sizeThatFits(CGSize(width: width, height: room)).height))
        }
        placeholderLabel.frame = CGRect(x: inset.left, y: inset.top, width: width, height: height)
        updateCaret()
    }

    // MARK: Content

    /// Replaces the whole text (a paste, a new version). The caret goes to the
    /// end unless told otherwise.
    func setText(_ value: String, caretAtEnd: Bool = true) {
        if currentText != value { text = value }
        let length = (value as NSString).length
        caret = caretAtEnd ? length : min(caret, length)
        refreshPlaceholder()
        updateCaret()
    }

    /// Inserts at the caret. `allow` sees the resulting text and can refuse
    /// it, which is how a hard length limit is enforced at the keystroke
    /// rather than silently at request time.
    @discardableResult
    func insert(_ string: String, allow: ((String) -> Bool)? = nil) -> Bool {
        let current = currentText as NSString
        let location = clampedCaret(in: current)
        let next = current.replacingCharacters(in: NSRange(location: location, length: 0), with: string)
        if let allow, !allow(next) { return false }
        text = next
        caret = location + (string as NSString).length
        didEdit()
        return true
    }

    /// Deletes the character before the caret - a whole emoji or combined
    /// letter, never half of one.
    @discardableResult
    func deleteBackward() -> Bool {
        let current = currentText as NSString
        let location = clampedCaret(in: current)
        guard location > 0 else { return false }
        let range = current.rangeOfComposedCharacterSequence(at: location - 1)
        text = current.replacingCharacters(in: range, with: "")
        caret = range.location
        didEdit()
        return true
    }

    /// Deletes the word before the caret, as a held delete key does.
    @discardableResult
    func deleteWordBackward() -> Bool {
        let before = textBeforeCaret
        let characters = TextDeletion.wordLength(before: before)
        guard characters > 0 else { return false }
        let removed = String(before.suffix(characters))
        let length = (removed as NSString).length
        let current = currentText as NSString
        let location = clampedCaret(in: current)
        let start = max(0, location - length)
        text = current.replacingCharacters(in: NSRange(location: start, length: location - start), with: "")
        caret = start
        didEdit()
        return true
    }

    /// Moves the caret by whole characters, for the space-bar trackpad.
    func moveCaret(by offset: Int) {
        let current = currentText as NSString
        var location = clampedCaret(in: current)
        if offset < 0 {
            for _ in 0..<(-offset) where location > 0 {
                location = current.rangeOfComposedCharacterSequence(at: location - 1).location
            }
        } else {
            for _ in 0..<offset where location < current.length {
                let range = current.rangeOfComposedCharacterSequence(at: location)
                location = range.location + range.length
            }
        }
        caret = location
        updateCaret()
        scrollCaretIntoView()
    }

    var textBeforeCaret: String {
        let current = currentText as NSString
        return current.substring(to: clampedCaret(in: current))
    }

    /// Puts the caret where the user tapped.
    func placeCaret(at point: CGPoint) {
        caret = characterIndex(at: point)
        updateCaret()
    }

    // MARK: Geometry

    private func clampedCaret(in string: NSString) -> Int {
        min(max(caret, 0), string.length)
    }

    private func characterIndex(at point: CGPoint) -> Int {
        let current = currentText as NSString
        guard current.length > 0 else { return 0 }
        layoutManager.ensureLayout(for: textContainer)
        let local = CGPoint(x: point.x - textContainerInset.left, y: point.y - textContainerInset.top)
        let used = layoutManager.usedRect(for: textContainer)
        if local.y > used.maxY { return current.length }
        if local.y < used.minY { return 0 }

        var fraction: CGFloat = 0
        let index = layoutManager.characterIndex(
            for: local,
            in: textContainer,
            fractionOfDistanceBetweenInsertionPoints: &fraction
        )
        guard index < current.length else { return current.length }
        // Tapping past the end of a line lands on its newline: the caret goes
        // before it, not onto the next line.
        if current.character(at: index) == 0x0A { return index }
        let target = min(index + (fraction > 0.5 ? 1 : 0), current.length)
        guard target < current.length else { return target }
        return current.rangeOfComposedCharacterSequence(at: target).location
    }

    private func caretRect() -> CGRect {
        let current = currentText as NSString
        let location = clampedCaret(in: current)
        let lineHeight = (font ?? .systemFont(ofSize: 16)).lineHeight
        layoutManager.ensureLayout(for: textContainer)

        var rect: CGRect
        if current.length == 0 {
            rect = CGRect(x: 0, y: 0, width: 2, height: lineHeight)
        } else if location >= current.length {
            let extra = layoutManager.extraLineFragmentRect
            if !extra.isEmpty {
                rect = CGRect(x: extra.minX, y: extra.minY, width: 2, height: extra.height)
            } else {
                let glyph = layoutManager.glyphIndexForCharacter(at: current.length - 1)
                let line = layoutManager.lineFragmentRect(forGlyphAt: glyph, effectiveRange: nil)
                let glyphRect = layoutManager.boundingRect(forGlyphRange: NSRange(location: glyph, length: 1), in: textContainer)
                rect = CGRect(x: glyphRect.maxX, y: line.minY, width: 2, height: line.height)
            }
        } else {
            let glyph = layoutManager.glyphIndexForCharacter(at: location)
            let line = layoutManager.lineFragmentRect(forGlyphAt: glyph, effectiveRange: nil)
            let position = layoutManager.location(forGlyphAt: glyph)
            rect = CGRect(x: line.minX + position.x, y: line.minY, width: 2, height: line.height)
        }
        // One text line tall, centred on the line fragment.
        let height = min(rect.height, lineHeight + 2)
        rect.origin.y += (rect.height - height) / 2
        rect.size.height = height
        rect.origin.x = min(rect.origin.x, max(0, textContainer.size.width - 2))
        return rect.offsetBy(dx: textContainerInset.left, dy: textContainerInset.top)
    }

    private func updateCaret() {
        guard showsCaret else {
            caretView.isHidden = true
            caretView.layer.removeAllAnimations()
            return
        }
        caretView.frame = caretRect()
        caretView.isHidden = false
        caretView.layer.removeAllAnimations()
        caretView.layer.opacity = 1
        let blink = CABasicAnimation(keyPath: "opacity")
        blink.fromValue = 1
        blink.toValue = 0
        blink.duration = 0.5
        blink.beginTime = CACurrentMediaTime() + 0.5
        blink.autoreverses = true
        blink.repeatCount = .infinity
        blink.timingFunction = CAMediaTimingFunction(name: .easeInEaseOut)
        caretView.layer.add(blink, forKey: "blink")
    }

    func scrollCaretIntoView() {
        guard showsCaret else { return }
        let rect = caretRect().insetBy(dx: 0, dy: -4)
        scrollRectToVisible(rect, animated: false)
    }

    private func didEdit() {
        refreshPlaceholder()
        updateCaret()
        scrollCaretIntoView()
    }

    private func refreshPlaceholder() {
        placeholderLabel.isHidden = !currentText.isEmpty
    }
}
