import CoreGraphics
import Foundation

/// An sRGB colour, 0…1 per channel.
struct ScreenshotColor: Equatable, Sendable {
    var red: Double
    var green: Double
    var blue: Double

    init(_ red: Double, _ green: Double, _ blue: Double) {
        self.red = red
        self.green = green
        self.blue = blue
    }

    /// From 0…255 components.
    init(rgb red: Int, _ green: Int, _ blue: Int) {
        self.init(Double(red) / 255, Double(green) / 255, Double(blue) / 255)
    }

    func distance(to other: ScreenshotColor) -> Double {
        let r = red - other.red, g = green - other.green, b = blue - other.blue
        return (r * r + g * g + b * b).squareRoot()
    }

    var luminance: Double { 0.2126 * red + 0.7152 * green + 0.0722 * blue }

    var saturation: Double {
        let high = max(red, green, blue), low = min(red, green, blue)
        return high <= 0 ? 0 : (high - low) / high
    }

    /// How colourful, independent of brightness: near-black text (#111B21)
    /// has a high HSV saturation but almost no chroma.
    var chroma: Double { max(red, green, blue) - min(red, green, blue) }

    static func average(_ colors: [ScreenshotColor]) -> ScreenshotColor? {
        guard !colors.isEmpty else { return nil }
        let n = Double(colors.count)
        return ScreenshotColor(colors.reduce(0) { $0 + $1.red } / n,
                               colors.reduce(0) { $0 + $1.green } / n,
                               colors.reduce(0) { $0 + $1.blue } / n)
    }
}

/// One line of text as the recogniser saw it.
struct RecognizedLine: Equatable, Sendable {
    var text: String
    /// Normalised 0…1, top-left origin.
    var box: CGRect
    var confidence: Double
    /// The colour around the line (its bubble), when it could be sampled.
    var background: ScreenshotColor?
    /// The colour of the letters, when it could be sampled.
    var foreground: ScreenshotColor?
}

/// Turns recognised lines into a conversation: which lines are the app's own
/// interface, which belong to one bubble, who wrote each bubble, which line
/// is a name or a quote, and in what order the messages came.
///
/// Signals, never rules: the bubble's side, its colour (two colour families,
/// one per side), the lines around it. Where they disagree the message gets a
/// low sender confidence and the composer asks the person to check it.
struct ConversationLayoutAnalyzer {

    struct Result: Equatable {
        var conversation: ScreenshotConversation
        /// Lines left out as interface (status bar, input field, keyboard,
        /// dates, system notices).
        var ignoredLines: Int
    }

    /// fullScreen: a whole phone screenshot, with a status bar, a title
    /// bar and an input field to leave out. A cropped part has none.
    func analyze(_ input: [RecognizedLine], fullScreen: Bool = true) -> Result {
        var lines: [RecognizedLine] = []
        for original in input {
            var line = original
            line.text = Self.clean(line.text)
            if !line.text.isEmpty && line.box.width > 0 && line.box.height > 0 { lines.append(line) }
        }
        lines.sort(by: Self.readingOrder)
        let total = lines.count
        guard !lines.isEmpty else { return Result(conversation: ScreenshotConversation(), ignoredLines: 0) }

        let heights: [CGFloat] = lines.map { (line: RecognizedLine) -> CGFloat in line.box.height }
        let lineHeight: CGFloat = Self.median(heights) ?? 0.02
        let keyboardTop = Self.keyboardTop(lines)
        let title = fullScreen ? Self.title(lines, lineHeight: lineHeight) : nil
        lines.removeAll { line in
            line.box.minY >= keyboardTop
                || (fullScreen && (Self.isStatusBar(line)
                    || Self.isNavigation(line, title: title?.text, lineHeight: lineHeight)
                    || Self.isInputBar(line)))
                || Self.isSeparator(line)
        }

        var bubbles = Self.group(lines, lineHeight: lineHeight)
        Self.classify(&bubbles)
        let messages = bubbles.enumerated().compactMap { index, bubble in
            Self.message(from: bubble, index: index, lineHeight: lineHeight)
        }
        var conversation = ScreenshotConversation(app: Self.app(bubbles), title: title?.text, messages: messages)
        Self.assignGroupSenders(&conversation)
        return Result(conversation: conversation, ignoredLines: total - lines.count)
    }

    // MARK: Bubbles

    struct Bubble: Equatable {
        var lines: [RecognizedLine]
        var role: ScreenshotSenderRole = .unknown
        var roleConfidence: Double = 0

        var box: CGRect { lines.dropFirst().reduce(lines[0].box) { $0.union($1.box) } }
        var background: ScreenshotColor? {
            ScreenshotColor.average(lines.compactMap(\.background))
        }
        /// Positive: closer to the right edge. Negative: closer to the left.
        var lean: Double { Double(box.minX - (1 - box.maxX)) }
    }

    /// Lines belong to one bubble when they follow closely, line up on the
    /// same side and sit on the same colour.
    static func group(_ lines: [RecognizedLine], lineHeight: CGFloat) -> [Bubble] {
        var bubbles: [Bubble] = []
        for line in lines {
            guard var current = bubbles.last else {
                bubbles.append(Bubble(lines: [line]))
                continue
            }
            let previous = current.lines[current.lines.count - 1]
            let gap = line.box.minY - previous.box.maxY
            let box = current.box
            let sameLeft = abs(line.box.minX - box.minX) < 0.045
            let sameRight = abs(line.box.maxX - box.maxX) < 0.045
            let inside = line.box.minX >= box.minX - 0.03 && line.box.maxX <= box.maxX + 0.03
            let overlap = max(0, min(line.box.maxX, box.maxX) - max(line.box.minX, box.minX)) / max(line.box.width, 0.001)
            var sameColour = true
            if let a = line.background, let b = current.background { sameColour = a.distance(to: b) < 0.09 }
            let similarSize = line.box.height < previous.box.height * 1.6 && line.box.height > previous.box.height * 0.55
            // A time under the text sits at the bubble's right edge, which
            // can reach past the text's own.
            let time = timeOnly(line.text) != nil && line.box.minX >= box.minX - 0.03 && line.box.maxX <= box.maxX + 0.14
            let continues = gap < lineHeight * 0.6 && gap > -lineHeight * 0.5
                && (sameLeft || sameRight || inside || overlap > 0.6 || time)
                && sameColour && (similarSize || time)
            if continues {
                current.lines.append(line)
                bubbles[bubbles.count - 1] = current
            } else {
                bubbles.append(Bubble(lines: [line]))
            }
        }
        return merged(bubbles, lineHeight: lineHeight)
    }

    /// A quote inside a bubble sits on a tint of the bubble's colour, so the
    /// first pass cuts the bubble there; the pieces join again when they
    /// touch, line up and differ only by that tint.
    static func merged(_ input: [Bubble], lineHeight: CGFloat) -> [Bubble] {
        var bubbles = input
        var changed = true
        while changed {
            changed = false
            var index = 0
            while index + 1 < bubbles.count {
                let a = bubbles[index], b = bubbles[index + 1]
                let gap = b.box.minY - a.box.maxY
                let aInsideB = a.box.minX >= b.box.minX - 0.02 && a.box.maxX <= b.box.maxX + 0.05
                let bInsideA = b.box.minX >= a.box.minX - 0.02 && b.box.maxX <= a.box.maxX + 0.05
                let aligned = abs(a.box.minX - b.box.minX) < 0.045 || aInsideB || bInsideA
                var join = false
                if gap < lineHeight * 0.6, gap > -lineHeight * 0.5, aligned,
                   let ca = a.background, let cb = b.background {
                    let distance = ca.distance(to: cb)
                    // Same colour: one bubble cut by its quote. A tint above
                    // the text: the quote itself.
                    join = distance < 0.09 || (distance < 0.3 && aInsideB && a.lines.count <= 3)
                }
                if join {
                    bubbles[index].lines += b.lines
                    bubbles.remove(at: index + 1)
                    changed = true
                } else {
                    index += 1
                }
            }
        }
        return bubbles
    }

    /// The side says most. Colour settles the bubbles too wide to have a
    /// side: the side whose bubbles share a colour gives that colour its
    /// role. Disagreement lowers the confidence instead of hiding it.
    static func classify(_ bubbles: inout [Bubble]) {
        let sideMargin = 0.06
        var rightColours: [ScreenshotColor] = [], leftColours: [ScreenshotColor] = []
        for bubble in bubbles {
            guard let colour = bubble.background else { continue }
            if bubble.lean > sideMargin { rightColours.append(colour) }
            if bubble.lean < -sideMargin { leftColours.append(colour) }
        }
        let right = ScreenshotColor.average(rightColours), left = ScreenshotColor.average(leftColours)
        // Colour is a usable signal only when the two sides differ.
        let coloursDiffer = right.flatMap { r in left.map { r.distance(to: $0) > 0.1 } } ?? false

        for index in bubbles.indices {
            let bubble = bubbles[index]
            let lean = bubble.lean
            var geometric: ScreenshotSenderRole = .unknown
            if lean > sideMargin { geometric = .me }
            if lean < -sideMargin { geometric = .other }

            var chromatic: ScreenshotSenderRole = .unknown
            if coloursDiffer, let colour = bubble.background, let right, let left {
                let toRight = colour.distance(to: right), toLeft = colour.distance(to: left)
                if min(toRight, toLeft) < 0.12 && abs(toRight - toLeft) > 0.05 {
                    chromatic = toRight < toLeft ? .me : .other
                }
            }

            switch (geometric, chromatic) {
            case (.unknown, .unknown):
                bubbles[index].role = .unknown
                bubbles[index].roleConfidence = 0.3
            case (let side, .unknown):
                bubbles[index].role = side
                // A strong lean is clear on its own; a slight one less so.
                bubbles[index].roleConfidence = abs(lean) > 0.2 ? 0.85 : 0.7
            case (.unknown, let colour):
                bubbles[index].role = colour
                bubbles[index].roleConfidence = 0.7
            case (let side, let colour) where side == colour:
                bubbles[index].role = side
                bubbles[index].roleConfidence = 0.95
            case (let side, _):
                // Side and colour disagree: keep the side, ask the person.
                bubbles[index].role = side
                bubbles[index].roleConfidence = 0.45
            }
        }
    }

    // MARK: Messages

    static func message(from bubble: Bubble, index: Int, lineHeight: CGFloat) -> ScreenshotMessage? {
        var lines = bubble.lines
        var time: String?
        // A time on a line of its own, or at the end of the last line.
        if let last = lines.last, let match = Self.timeOnly(last.text) {
            time = match
            lines.removeLast()
        } else if let last = lines.last, let (text, match) = Self.trailingTime(last.text) {
            time = match
            lines[lines.count - 1].text = text
        }

        var sender: String?
        var quote: [String] = []
        if lines.count >= 2 {
            let body = lines.dropFirst()
            // A coloured first line over plain text: a name in a group chat.
            if bubble.role != .me, let first = lines.first, let colour = first.foreground,
               colour.chroma > 0.25, Self.looksLikeName(first.text),
               body.compactMap(\.foreground).allSatisfy({ $0.chroma < 0.15 }) {
                sender = first.text
                lines.removeFirst()
            }
        }
        // Lines on a different colour at the top of the bubble: a quote of an
        // earlier message (the reply's own text is on the bubble's colour).
        if lines.count >= 2, let main = lines.last?.background {
            while lines.count >= 2, let colour = lines.first?.background, colour.distance(to: main) > 0.06 {
                quote.append(lines.removeFirst().text)
            }
        }
        let text = lines.map(\.text).joined(separator: "\n").trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return nil }
        let confidence = bubble.lines.map(\.confidence).reduce(0, +) / Double(max(bubble.lines.count, 1))
        let box = bubble.box
        return ScreenshotMessage(
            id: "m\(index + 1)", role: bubble.role, sender: sender, text: text,
            quote: quote.isEmpty ? nil : quote.joined(separator: " "), time: time,
            frame: box, textConfidence: confidence, roleConfidence: bubble.roleConfidence)
    }

    /// In a group chat the other side has several people: a name a bubble
    /// showed is kept; nothing is invented for the bubbles without one.
    static func assignGroupSenders(_ conversation: inout ScreenshotConversation) {
        let names = Set(conversation.messages.compactMap(\.sender))
        guard names.count == 1, let title = conversation.title, !title.isEmpty else { return }
        // One name equal to the chat's title is a 1:1 chat label, not news.
        if names.first?.caseInsensitiveCompare(title) == .orderedSame {
            for index in conversation.messages.indices { conversation.messages[index].sender = nil }
        }
    }

    // MARK: Interface

    /// The top of the iOS keyboard, when one is on screen: rows of single
    /// letters in the lower part.
    static func keyboardTop(_ lines: [RecognizedLine]) -> CGFloat {
        let keys = lines.filter { $0.box.minY > 0.5 && $0.text.count <= 2 && $0.box.width < 0.09 }
        guard keys.count >= 8, let top = keys.map(\.box.minY).min() else { return 2 }
        return top - 0.07
    }

    static func isStatusBar(_ line: RecognizedLine) -> Bool {
        guard line.box.maxY < 0.065 else { return false }
        let text = line.text
        return text.count <= 12 || timeOnly(text) != nil
    }

    /// The chat's name: the largest text in the top band that is not the
    /// status bar or a navigation word.
    static func title(_ lines: [RecognizedLine], lineHeight: CGFloat) -> RecognizedLine? {
        lines.filter { $0.box.minY < 0.16 && $0.box.maxY > 0.04 && !isStatusBar($0)
            && !isNavigationWord($0.text) && !isPresence($0.text) && $0.text.count <= 60 }
            .max { $0.box.height < $1.box.height }
            .flatMap { $0.box.midX > 0.15 && $0.box.midX < 0.85 ? $0 : nil }
    }

    static func isNavigation(_ line: RecognizedLine, title: String?, lineHeight: CGFloat) -> Bool {
        guard line.box.minY < 0.16 else { return false }
        if line.text == title { return true }
        return isNavigationWord(line.text) || isPresence(line.text)
            || (line.box.maxY < 0.13 && line.text.count <= 24)
    }

    static func isInputBar(_ line: RecognizedLine) -> Bool {
        guard line.box.minY > 0.84 else { return false }
        let lower = line.text.lowercased()
        return placeholders.contains { lower.hasPrefix($0) } || (line.box.minY > 0.93 && line.text.count <= 2)
    }

    /// Dates between messages, "unread" bars and system notices: centred,
    /// narrow and made of known words.
    static func isSeparator(_ line: RecognizedLine) -> Bool {
        let centred = abs(line.box.midX - 0.5) < 0.12 && line.box.width < 0.6
        guard centred else { return false }
        let lower = line.text.lowercased()
        if dayWords.contains(where: { lower == $0 || lower.hasPrefix($0 + " ") || lower.hasPrefix($0 + ",") }) { return true }
        if lower.range(of: #"^\d{1,2}[ .]"#, options: .regularExpression) != nil,
           monthWords.contains(where: { lower.contains($0) }) { return true }
        if monthWords.contains(where: { lower.hasPrefix($0) }), lower.count <= 24 { return true }
        return systemPhrases.contains { lower.contains($0) }
    }

    static func isNavigationWord(_ text: String) -> Bool {
        let lower = text.lowercased().trimmingCharacters(in: CharacterSet(charactersIn: "<‹ "))
        return navigationWords.contains(lower)
    }

    static func isPresence(_ text: String) -> Bool {
        let lower = text.lowercased()
        return presencePhrases.contains { lower.hasPrefix($0) || lower.contains($0) }
    }

    static func looksLikeName(_ text: String) -> Bool {
        guard text.count >= 2, text.count <= 32, !text.contains("?"), !text.contains("!"),
              !text.hasSuffix(".") else { return false }
        let words = text.split(separator: " ")
        return words.count <= 4
    }

    // MARK: Time

    private static let timePattern = #"(?:[01]?\d|2[0-3])[:.][0-5]\d(?:\s?[AaPp][Mm])?"#

    /// "12:30", "9:41 PM", "12:30 ✓✓" — a time and nothing else.
    static func timeOnly(_ text: String) -> String? {
        let trimmed = text.trimmingCharacters(in: CharacterSet(charactersIn: " ✓✔/√"))
        guard let range = trimmed.range(of: "^(?:edited |изменено |өзгертілді )?" + timePattern + "$",
                                        options: [.regularExpression, .caseInsensitive]) else { return nil }
        return String(trimmed[range]).replacingOccurrences(of: #"^\D+ "#, with: "", options: .regularExpression)
    }

    /// "Ok, see you 12:30 ✓✓" → ("Ok, see you", "12:30").
    static func trailingTime(_ text: String) -> (String, String)? {
        let trimmed = text.trimmingCharacters(in: CharacterSet(charactersIn: " ✓✔/√"))
        guard let range = trimmed.range(of: #"\s+"# + timePattern + "$", options: .regularExpression) else { return nil }
        let rest = String(trimmed[..<range.lowerBound]).trimmingCharacters(in: .whitespaces)
        guard !rest.isEmpty else { return nil }
        return (rest, String(trimmed[range]).trimmingCharacters(in: .whitespaces))
    }

    // MARK: App

    /// The messenger, only when the outgoing colour matches one closely.
    static func app(_ bubbles: [Bubble]) -> String? {
        let outgoing = bubbles.filter { $0.role == .me && $0.roleConfidence >= 0.7 }.compactMap(\.background)
        guard outgoing.count >= 1, let colour = ScreenshotColor.average(outgoing) else { return nil }
        let palette: [(String, ScreenshotColor)] = [
            ("whatsapp", ScreenshotColor(rgb: 217, 253, 211)), ("whatsapp", ScreenshotColor(rgb: 220, 248, 198)),
            ("whatsapp", ScreenshotColor(rgb: 0, 92, 75)), ("whatsapp", ScreenshotColor(rgb: 20, 77, 55)),
            ("imessage", ScreenshotColor(rgb: 0, 122, 255)), ("imessage", ScreenshotColor(rgb: 10, 132, 255)),
            ("imessage", ScreenshotColor(rgb: 52, 199, 89)), ("imessage", ScreenshotColor(rgb: 48, 209, 88)),
            ("telegram", ScreenshotColor(rgb: 239, 253, 222)), ("telegram", ScreenshotColor(rgb: 225, 254, 198)),
            ("telegram", ScreenshotColor(rgb: 43, 82, 120)),
            ("instagram", ScreenshotColor(rgb: 55, 151, 240)), ("instagram", ScreenshotColor(rgb: 120, 60, 240)),
        ]
        let nearest = palette.min { colour.distance(to: $0.1) < colour.distance(to: $1.1) }
        guard let nearest, colour.distance(to: nearest.1) < 0.035 else { return nil }
        return nearest.0
    }

    // MARK: Words

    private static let navigationWords: Set<String> = [
        "back", "назад", "артқа", "orqaga", "chats", "чаты", "чаттар", "chatlar", "messages", "сообщения",
        "edit", "изменить", "done", "готово", "video", "audio", "call", "info", "инфо", "search", "поиск",
    ]
    private static let presencePhrases = [
        "online", "в сети", "желіде", "onlayn", "last seen", "был в сети", "была в сети", "был(а)",
        "typing", "печатает", "жазып жатыр", "yozmoqda", "tap here for contact info", "нажмите здесь",
        "recently", "недавно", "members", "участник", "қатысушы", "a'zo",
    ]
    private static let placeholders = [
        "message", "сообщение", "хабарлама", "xabar", "imessage", "type a message", "написать",
        "write a message", "жазу", "send message", "отправить",
    ]
    private static let dayWords: Set<String> = [
        "today", "yesterday", "сегодня", "вчера", "бүгін", "кеше", "bugun", "kecha",
        "monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday",
        "понедельник", "вторник", "среда", "четверг", "пятница", "суббота", "воскресенье",
        "дүйсенбі", "сейсенбі", "сәрсенбі", "бейсенбі", "жұма", "сенбі", "жексенбі",
    ]
    private static let monthWords = [
        "jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec",
        "янв", "фев", "мар", "апр", "мая", "май", "июн", "июл", "авг", "сен", "окт", "ноя", "дек",
        "қаң", "ақп", "наурыз", "сәуір", "мамыр", "маусым", "шілде", "тамыз", "қыркүйек", "қазан", "қараша", "желтоқсан",
    ]
    private static let systemPhrases = [
        "end-to-end encrypted", "сквозным шифрованием", "шифрованием", "unread message", "непрочитан",
        "created group", "создал(а) группу", "added", "добавил", "joined", "присоединил", "left the group",
        "missed call", "пропущенный", "this message was deleted", "сообщение удалено",
    ]

    // MARK: Helpers

    static func clean(_ text: String) -> String {
        text.replacingOccurrences(of: "\u{200B}", with: "")
            .split(whereSeparator: \.isNewline).map { $0.trimmingCharacters(in: .whitespaces) }
            .joined(separator: " ")
            .trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// Top to bottom, then left to right.
    static func readingOrder(_ a: RecognizedLine, _ b: RecognizedLine) -> Bool {
        if a.box.minY != b.box.minY { return a.box.minY < b.box.minY }
        return a.box.minX < b.box.minX
    }

    static func median(_ values: [CGFloat]) -> CGFloat? {
        guard !values.isEmpty else { return nil }
        let sorted = values.sorted()
        return sorted[sorted.count / 2]
    }
}
