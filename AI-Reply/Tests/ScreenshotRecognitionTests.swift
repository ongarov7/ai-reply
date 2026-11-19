import UIKit
import XCTest
@testable import AIReply

/// The whole on-device path on drawn chat screenshots: Apple Vision reads the
/// text, the sampler reads the bubble colours, the analyser builds the
/// conversation. iPhone-sized (1179 × 2556), light and dark, four languages.
final class ScreenshotRecognitionTests: XCTestCase {

    struct Bubble {
        var text: String
        var mine: Bool
        var sender: String? = nil
        var senderColor: UIColor = .systemPink
        var time = "12:30"
    }

    struct Style {
        var wallpaper: UIColor
        var bar: UIColor
        var incoming: UIColor
        var outgoing: UIColor
        var incomingText: UIColor
        var outgoingText: UIColor
        var barText: UIColor

        static let whatsAppLight = Style(wallpaper: UIColor(red: 0.94, green: 0.92, blue: 0.89, alpha: 1), bar: .white,
                                         incoming: .white, outgoing: UIColor(red: 0.85, green: 0.99, blue: 0.83, alpha: 1),
                                         incomingText: UIColor(red: 0.07, green: 0.11, blue: 0.13, alpha: 1),
                                         outgoingText: UIColor(red: 0.07, green: 0.11, blue: 0.13, alpha: 1), barText: .black)
        static let telegramDark = Style(wallpaper: UIColor(red: 0.06, green: 0.09, blue: 0.13, alpha: 1),
                                        bar: UIColor(red: 0.11, green: 0.14, blue: 0.18, alpha: 1),
                                        incoming: UIColor(red: 0.09, green: 0.15, blue: 0.2, alpha: 1),
                                        outgoing: UIColor(red: 0.17, green: 0.32, blue: 0.47, alpha: 1),
                                        incomingText: .white, outgoingText: .white, barText: .white)
        static let messagesLight = Style(wallpaper: .white, bar: UIColor(white: 0.97, alpha: 1),
                                         incoming: UIColor(red: 0.91, green: 0.91, blue: 0.92, alpha: 1),
                                         outgoing: UIColor(red: 0, green: 0.48, blue: 1, alpha: 1),
                                         incomingText: .black, outgoingText: .white, barText: .black)
    }

    /// A chat screen: status bar, title bar, bubbles, input field.
    func draw(title: String, bubbles: [Bubble], style: Style) -> CGImage {
        let size = CGSize(width: 1179, height: 2556)
        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        let image = UIGraphicsImageRenderer(size: size, format: format).image { context in
            style.wallpaper.setFill()
            context.fill(CGRect(origin: .zero, size: size))
            style.bar.setFill()
            context.fill(CGRect(x: 0, y: 0, width: size.width, height: 300))
            context.fill(CGRect(x: 0, y: size.height - 250, width: size.width, height: 250))
            let barFont = UIFont.systemFont(ofSize: 48, weight: .semibold)
            ("9:41" as NSString).draw(at: CGPoint(x: 90, y: 50), withAttributes: [.font: barFont, .foregroundColor: style.barText])
            let titleFont = UIFont.systemFont(ofSize: 54, weight: .semibold)
            let titleWidth = (title as NSString).size(withAttributes: [.font: titleFont]).width
            (title as NSString).draw(at: CGPoint(x: (size.width - titleWidth) / 2, y: 175),
                                     withAttributes: [.font: titleFont, .foregroundColor: style.barText])
            let field = CGRect(x: 120, y: size.height - 210, width: size.width - 240, height: 110)
            UIColor(white: 0.5, alpha: 0.15).setFill()
            UIBezierPath(roundedRect: field, cornerRadius: 55).fill()
            ("Message" as NSString).draw(at: CGPoint(x: 170, y: size.height - 185),
                                         withAttributes: [.font: UIFont.systemFont(ofSize: 51), .foregroundColor: UIColor.gray])

            let textFont = UIFont.systemFont(ofSize: 51)
            let nameFont = UIFont.systemFont(ofSize: 45, weight: .semibold)
            let timeFont = UIFont.systemFont(ofSize: 33)
            var y: CGFloat = 380
            for bubble in bubbles {
                let maxText: CGFloat = 760
                let paragraph = NSMutableParagraphStyle()
                paragraph.lineBreakMode = .byWordWrapping
                let color = bubble.mine ? style.outgoingText : style.incomingText
                let attributes: [NSAttributedString.Key: Any] = [.font: textFont, .foregroundColor: color, .paragraphStyle: paragraph]
                let textBox = (bubble.text as NSString).boundingRect(with: CGSize(width: maxText, height: 2000),
                                                                     options: [.usesLineFragmentOrigin], attributes: attributes, context: nil).integral
                let nameHeight: CGFloat = bubble.sender == nil ? 0 : 60
                let width = max(textBox.width, 260) + 72
                let height = textBox.height + nameHeight + 100
                let x: CGFloat = bubble.mine ? size.width - width - 48 : 48
                let rect = CGRect(x: x, y: y, width: width, height: height)
                (bubble.mine ? style.outgoing : style.incoming).setFill()
                UIBezierPath(roundedRect: rect, cornerRadius: 42).fill()
                if let sender = bubble.sender {
                    (sender as NSString).draw(at: CGPoint(x: x + 36, y: y + 24),
                                              withAttributes: [.font: nameFont, .foregroundColor: bubble.senderColor])
                }
                (bubble.text as NSString).draw(with: CGRect(x: x + 36, y: y + 24 + nameHeight, width: maxText, height: textBox.height),
                                               options: [.usesLineFragmentOrigin], attributes: attributes, context: nil)
                (bubble.time as NSString).draw(at: CGPoint(x: rect.maxX - 120, y: rect.maxY - 60),
                                               withAttributes: [.font: timeFont, .foregroundColor: color.withAlphaComponent(0.6)])
                y = rect.maxY + 36
            }
        }
        return image.cgImage!
    }

    private func read(_ image: CGImage, language: AppLanguage) throws -> ScreenshotConversation {
        try ScreenshotIntake.recognize(image, language: language, fullScreen: true)
    }

    func testEnglishWhatsAppScreenshot() throws {
        let image = draw(title: "Aigerim", bubbles: [
            Bubble(text: "Hi! Are you available tomorrow?", mine: false),
            Bubble(text: "I'm not sure yet.", mine: true),
            Bubble(text: "We really need your help with the project.", mine: false),
        ], style: .whatsAppLight)
        let conversation = try read(image, language: .english)
        XCTAssertEqual(conversation.messages.map(\.role), [.other, .me, .other], "\(conversation.messages.map(\.text))")
        XCTAssertTrue(conversation.target?.text.contains("help with the") == true
                      && conversation.target?.text.contains("project") == true,
                      conversation.messages.map { "\($0.role.rawValue): \($0.text) | \($0.time ?? "-")" }.joined(separator: " / "))
        XCTAssertEqual(conversation.title, "Aigerim")
        XCTAssertFalse(conversation.messages.contains { $0.text.contains("Message") || $0.text.contains("9:41") })
        XCTAssertEqual(conversation.messages.first?.time, "12:30")
    }

    func testRussianTelegramDarkScreenshot() throws {
        let image = draw(title: "Марат", bubbles: [
            Bubble(text: "Сможешь выйти в субботу?", mine: false),
            Bubble(text: "Пока не знаю", mine: true),
            Bubble(text: "Ответь до вечера, пожалуйста", mine: false),
            Bubble(text: "Хорошо, напишу", mine: true),
        ], style: .telegramDark)
        let conversation = try read(image, language: .russian)
        XCTAssertEqual(conversation.messages.map(\.role), [.other, .me, .other, .me], "\(conversation.messages.map(\.text))")
        XCTAssertTrue(conversation.target?.text.contains("до вечера") == true, "the last message from the other side")
        XCTAssertEqual(conversation.app, "telegram")
    }

    func testKazakhMessagesScreenshot() throws {
        let image = draw(title: "Әсел", bubbles: [
            Bubble(text: "Сәлем! Ертең кездесейік пе?", mine: false),
            Bubble(text: "Иә, кешке боламын", mine: true),
            Bubble(text: "Онда сағат жетіде күтемін", mine: false),
        ], style: .messagesLight)
        let conversation = try read(image, language: .kazakh)
        XCTAssertEqual(conversation.messages.map(\.role), [.other, .me, .other], "\(conversation.messages.map(\.text))")
        // Kazakh letters may be read as look-alikes; the words around them are not.
        let target = conversation.target?.text.lowercased() ?? ""
        XCTAssertTrue(target.contains("онда") || target.contains("күтемін") || target.contains("кутемин"), target)
        XCTAssertEqual(conversation.app, "imessage")
    }

    func testUzbekLatinScreenshot() throws {
        let image = draw(title: "Dilnoza", bubbles: [
            Bubble(text: "Salom! Ertaga uchrashamizmi?", mine: false),
            Bubble(text: "Ha, kechqurun bo'shman", mine: true),
            Bubble(text: "Unda soat yettida kutaman", mine: false),
        ], style: .whatsAppLight)
        let conversation = try read(image, language: .uzbek)
        XCTAssertEqual(conversation.messages.map(\.role), [.other, .me, .other], "\(conversation.messages.map(\.text))")
        XCTAssertTrue(conversation.target?.text.contains("kutaman") == true, conversation.target?.text ?? "nil")
    }

    func testGroupChatKeepsTheSendersNames() throws {
        let image = draw(title: "Команда", bubbles: [
            Bubble(text: "Кто завтра на встрече?", mine: false, sender: "Асель", senderColor: .systemGreen),
            Bubble(text: "Я буду", mine: false, sender: "Марат", senderColor: .systemPurple),
            Bubble(text: "Я тоже", mine: true),
        ], style: .whatsAppLight)
        let conversation = try read(image, language: .russian)
        XCTAssertEqual(conversation.messages.map(\.role), [.other, .other, .me], "\(conversation.messages.map(\.text))")
        XCTAssertEqual(conversation.messages.first?.sender, "Асель")
        XCTAssertEqual(conversation.messages.dropFirst().first?.sender, "Марат")
        XCTAssertEqual(conversation.target?.text, "Я буду")
    }

    func testAScreenshotWithoutChatTextSaysSo() throws {
        let size = CGSize(width: 600, height: 1200)
        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        let blank = UIGraphicsImageRenderer(size: size, format: format).image { context in
            UIColor.systemTeal.setFill()
            context.fill(CGRect(origin: .zero, size: size))
        }.cgImage!
        let conversation = try read(blank, language: .english)
        XCTAssertTrue(conversation.isEmpty)
        XCTAssertNil(conversation.target)
    }

    func testIngestStoresTheScreenshotForTheKeyboard() async throws {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent("shot-\(UUID().uuidString)")
        let store = ScreenshotContextStore(rootURL: root)
        defer { try? FileManager.default.removeItem(at: root) }
        let image = draw(title: "Aigerim", bubbles: [Bubble(text: "Can you call me?", mine: false)], style: .whatsAppLight)
        let png = UIImage(cgImage: image).pngData()!
        let context = try await ScreenshotIntake.ingest(png, source: .photos, instruction: " Say yes ", language: .english, store: store)
        XCTAssertEqual(context.instruction, "Say yes")
        XCTAssertEqual(store.read()?.id, context.id)
        XCTAssertNotNil(store.imageData(for: context))
        XCTAssertTrue(context.conversation.target?.text.contains("call me") == true)

        let cropped = try await ScreenshotIntake.crop(context, to: CGRect(x: 0, y: 0.1, width: 1, height: 0.4), language: .english, store: store)
        XCTAssertLessThan(cropped.imageHeight, context.imageHeight)
        XCTAssertEqual(store.read()?.imageHeight, cropped.imageHeight, "the cut replaces the stored picture")
    }
}
