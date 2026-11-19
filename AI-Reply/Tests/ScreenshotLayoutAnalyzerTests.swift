import CoreGraphics
import XCTest
@testable import AIReply

/// The conversation analyser on recognised lines: what is interface, what is
/// one bubble, who wrote it, names, quotes, times, and how sure it is.
final class ScreenshotLayoutAnalyzerTests: XCTestCase {

    private let whiteBubble = ScreenshotColor(rgb: 255, 255, 255)
    private let greenBubble = ScreenshotColor(rgb: 217, 253, 211)
    private let wallpaper = ScreenshotColor(rgb: 239, 234, 226)
    private let ink = ScreenshotColor(rgb: 17, 27, 33)

    private func line(_ text: String, x: CGFloat, y: CGFloat, width: CGFloat, height: CGFloat = 0.022,
                      background: ScreenshotColor? = nil, foreground: ScreenshotColor? = nil,
                      confidence: Double = 0.95) -> RecognizedLine {
        RecognizedLine(text: text, box: CGRect(x: x, y: y, width: width, height: height), confidence: confidence,
                       background: background, foreground: foreground ?? ink)
    }

    /// A WhatsApp-like screen: status bar, title, three bubbles, the input
    /// field.
    private func whatsAppLines() -> [RecognizedLine] {
        [
            line("9:41", x: 0.08, y: 0.015, width: 0.1, background: wallpaper),
            line("Aigerim", x: 0.38, y: 0.075, width: 0.24, height: 0.028, background: whiteBubble),
            line("online", x: 0.43, y: 0.105, width: 0.14, height: 0.016, background: whiteBubble),
            line("Today", x: 0.45, y: 0.18, width: 0.1, height: 0.016, background: wallpaper),
            line("Hi! Are you available tomorrow?", x: 0.06, y: 0.22, width: 0.55, background: whiteBubble),
            line("12:30", x: 0.55, y: 0.25, width: 0.08, height: 0.014, background: whiteBubble),
            line("I'm not sure yet.", x: 0.6, y: 0.31, width: 0.3, background: greenBubble),
            line("12:31 ✓✓", x: 0.8, y: 0.34, width: 0.12, height: 0.014, background: greenBubble),
            line("We really need your help", x: 0.06, y: 0.40, width: 0.5, background: whiteBubble),
            line("with the project.", x: 0.06, y: 0.425, width: 0.32, background: whiteBubble),
            line("12:33", x: 0.55, y: 0.452, width: 0.08, height: 0.014, background: whiteBubble),
            line("Message", x: 0.14, y: 0.93, width: 0.2, background: whiteBubble),
        ]
    }

    func testReadsAWhatsAppConversationInOrderWithSides() {
        let result = ConversationLayoutAnalyzer().analyze(whatsAppLines())
        let conversation = result.conversation
        XCTAssertEqual(conversation.title, "Aigerim")
        XCTAssertEqual(conversation.messages.map(\.text), [
            "Hi! Are you available tomorrow?", "I'm not sure yet.", "We really need your help\nwith the project.",
        ])
        XCTAssertEqual(conversation.messages.map(\.role), [.other, .me, .other])
        XCTAssertEqual(conversation.messages.map(\.time), ["12:30", "12:31", "12:33"])
        XCTAssertTrue(conversation.messages.allSatisfy { $0.roleConfidence >= 0.9 }, "side and colour agree")
        XCTAssertEqual(conversation.target?.text, "We really need your help\nwith the project.",
                       "the reply answers the other side's latest message, never the person's own")
        XCTAssertEqual(conversation.app, "whatsapp")
        XCTAssertFalse(conversation.needsReview)
        XCTAssertGreaterThanOrEqual(result.ignoredLines, 4, "status bar, presence, date and input field")
    }

    func testTheLatestMessageBeingMineStillAnswersTheOtherSide() {
        var lines = whatsAppLines()
        lines.insert(line("Let me check.", x: 0.66, y: 0.5, width: 0.25, background: greenBubble), at: 11)
        let conversation = ConversationLayoutAnalyzer().analyze(lines).conversation
        XCTAssertEqual(conversation.messages.last?.role, .me)
        XCTAssertEqual(conversation.target?.text, "We really need your help\nwith the project.")
    }

    func testAWideBubbleTakesItsSideFromItsColour() {
        let wide = String(repeating: "очень длинное сообщение ", count: 3)
        let lines = [
            line("Привет", x: 0.06, y: 0.2, width: 0.2, background: whiteBubble),
            line("Хорошо", x: 0.7, y: 0.26, width: 0.2, background: greenBubble),
            // Spans the screen: no side of its own.
            line(wide, x: 0.05, y: 0.33, width: 0.9, background: greenBubble),
        ]
        let messages = ConversationLayoutAnalyzer().analyze(lines, fullScreen: false).conversation.messages
        XCTAssertEqual(messages.map(\.role), [.other, .me, .me])
        XCTAssertEqual(messages[2].roleConfidence, 0.7, accuracy: 0.001)
    }

    func testDisagreementIsShownNotHidden() {
        let lines = [
            line("Привет", x: 0.06, y: 0.2, width: 0.2, background: whiteBubble),
            line("Хорошо", x: 0.7, y: 0.26, width: 0.2, background: greenBubble),
            // Right-aligned but white like the other side: ask the person.
            line("Странно", x: 0.7, y: 0.33, width: 0.2, background: whiteBubble),
        ]
        let messages = ConversationLayoutAnalyzer().analyze(lines, fullScreen: false).conversation.messages
        XCTAssertEqual(messages[2].role, .me)
        XCTAssertLessThan(messages[2].roleConfidence, ScreenshotMessage.reviewThreshold)
        XCTAssertTrue(messages[2].needsReview)
    }

    func testWithoutColoursOrSideTheSenderIsUnknown() {
        let lines = [line("Centred text that is wide", x: 0.1, y: 0.3, width: 0.8)]
        let message = ConversationLayoutAnalyzer().analyze(lines, fullScreen: false).conversation.messages.first
        XCTAssertEqual(message?.role, .unknown)
        XCTAssertEqual(ConversationLayoutAnalyzer().analyze(lines, fullScreen: false).conversation.target?.role, .unknown)
    }

    func testGroupChatNamesAndQuotes() {
        let nameColour = ScreenshotColor(rgb: 31, 168, 85)
        let quoteTint = ScreenshotColor(rgb: 236, 236, 236)
        let lines = [
            line("Asel", x: 0.06, y: 0.2, width: 0.12, background: whiteBubble, foreground: nameColour),
            line("Кто идёт завтра?", x: 0.06, y: 0.225, width: 0.4, background: whiteBubble),
            line("Marat", x: 0.06, y: 0.3, width: 0.13, background: whiteBubble, foreground: ScreenshotColor(rgb: 200, 60, 130)),
            line("Кто идёт завтра?", x: 0.08, y: 0.325, width: 0.36, background: quoteTint),
            line("Я иду!", x: 0.06, y: 0.352, width: 0.16, background: whiteBubble),
        ]
        let messages = ConversationLayoutAnalyzer().analyze(lines, fullScreen: false).conversation.messages
        XCTAssertEqual(messages.count, 2)
        XCTAssertEqual(messages[0].sender, "Asel")
        XCTAssertEqual(messages[0].text, "Кто идёт завтра?")
        XCTAssertEqual(messages[1].sender, "Marat")
        XCTAssertEqual(messages[1].quote, "Кто идёт завтра?")
        XCTAssertEqual(messages[1].text, "Я иду!")
    }

    func testTheIOSKeyboardIsNotAMessage() {
        var lines = whatsAppLines().filter { $0.text != "Message" }
        let keys = Array("йцукенгшщзх")
        for (index, key) in keys.enumerated() {
            lines.append(line(String(key), x: 0.02 + CGFloat(index) * 0.09, y: 0.62, width: 0.05, height: 0.03))
        }
        let conversation = ConversationLayoutAnalyzer().analyze(lines).conversation
        XCTAssertEqual(conversation.messages.count, 3)
        XCTAssertFalse(conversation.messages.contains { $0.text.count == 1 })
    }

    func testDatesAndSystemNoticesAreLeftOut() {
        let lines = [
            line("Yesterday", x: 0.42, y: 0.2, width: 0.16, background: wallpaper),
            line("12 ноября", x: 0.42, y: 0.25, width: 0.16, background: wallpaper),
            line("Messages and calls are end-to-end encrypted.", x: 0.22, y: 0.3, width: 0.56, background: wallpaper),
            line("Сәлем! Ертең кездесейік", x: 0.06, y: 0.4, width: 0.5, background: whiteBubble),
        ]
        let conversation = ConversationLayoutAnalyzer().analyze(lines, fullScreen: false).conversation
        XCTAssertEqual(conversation.messages.map(\.text), ["Сәлем! Ертең кездесейік"])
    }

    func testACroppedPartKeepsItsTopAndBottomLines() {
        let lines = [
            line("Ok", x: 0.06, y: 0.02, width: 0.1, background: whiteBubble),
            line("Message me later", x: 0.6, y: 0.95, width: 0.3, background: greenBubble),
        ]
        let full = ConversationLayoutAnalyzer().analyze(lines, fullScreen: true).conversation.messages
        let cropped = ConversationLayoutAnalyzer().analyze(lines, fullScreen: false).conversation.messages
        XCTAssertLessThan(full.count, 2, "on a full screen these bands are interface")
        XCTAssertEqual(cropped.map(\.text), ["Ok", "Message me later"])
    }

    func testTimesAreReadNotSent() {
        XCTAssertEqual(ConversationLayoutAnalyzer.timeOnly("12:31 ✓✓"), "12:31")
        XCTAssertEqual(ConversationLayoutAnalyzer.timeOnly("9:41 PM"), "9:41 PM")
        XCTAssertNil(ConversationLayoutAnalyzer.timeOnly("Встреча в 12:30"))
        XCTAssertEqual(ConversationLayoutAnalyzer.trailingTime("See you 18:05")?.0, "See you")
        XCTAssertEqual(ConversationLayoutAnalyzer.trailingTime("See you 18:05")?.1, "18:05")
        XCTAssertNil(ConversationLayoutAnalyzer.trailingTime("18:05"))
    }

    func testDarkModeColoursWork() {
        let darkIncoming = ScreenshotColor(rgb: 32, 44, 51)
        let darkOutgoing = ScreenshotColor(rgb: 0, 92, 75)
        let lines = [
            line("Сен келесің бе?", x: 0.06, y: 0.3, width: 0.4, background: darkIncoming, foreground: .init(rgb: 233, 237, 239)),
            line("Иә, келемін", x: 0.62, y: 0.36, width: 0.3, background: darkOutgoing, foreground: .init(rgb: 233, 237, 239)),
            line("Жақсы, күтемін", x: 0.06, y: 0.42, width: 0.36, background: darkIncoming, foreground: .init(rgb: 233, 237, 239)),
        ]
        let conversation = ConversationLayoutAnalyzer().analyze(lines, fullScreen: false).conversation
        XCTAssertEqual(conversation.messages.map(\.role), [.other, .me, .other])
        XCTAssertEqual(conversation.app, "whatsapp")
        XCTAssertEqual(conversation.target?.text, "Жақсы, күтемін")
    }

    func testLowRecognitionConfidenceAsksForACheck() {
        let lines = [line("H3l1o?? wh@t", x: 0.06, y: 0.3, width: 0.3, background: whiteBubble, confidence: 0.3),
                     line("ok", x: 0.7, y: 0.36, width: 0.1, background: greenBubble)]
        let message = ConversationLayoutAnalyzer().analyze(lines, fullScreen: false).conversation.messages[0]
        XCTAssertLessThan(message.textConfidence, 0.5)
        XCTAssertTrue(message.needsReview)
    }
}
