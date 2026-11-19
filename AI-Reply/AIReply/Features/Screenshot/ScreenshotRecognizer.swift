import CoreGraphics
import Foundation
import ImageIO
import UniformTypeIdentifiers
import Vision

/// On-device text recognition with Apple's Vision framework. The picture
/// never leaves the iPhone for this step.
enum ScreenshotRecognizer {

    enum Failure: Error, Equatable {
        case unreadableImage
        case recognitionFailed
    }

    /// Vision's languages for this app language, most likely first, among
    /// those this device's Vision supports. Kazakh and Uzbek have no Vision
    /// model of their own on most devices: Russian reads Kazakh Cyrillic and
    /// English reads Uzbek Latin, and the server is told the text may have
    /// recognition errors (Kazakh letters read as look-alikes).
    static func recognitionLanguages(for language: AppLanguage, supported: [String]) -> [String] {
        let wanted: [String]
        switch language {
        case .english: wanted = ["en-US", "ru-RU"]
        case .russian: wanted = ["ru-RU", "en-US"]
        case .kazakh:  wanted = ["kk-KZ", "ru-RU", "en-US"]
        case .uzbek:   wanted = ["uz-UZ", "en-US", "ru-RU"]
        }
        let available = wanted.filter { supported.contains($0) }
        return available.isEmpty ? ["en-US"] : available
    }

    /// The text lines of an image, with their boxes and colours.
    static func lines(in image: CGImage, language: AppLanguage) throws -> [RecognizedLine] {
        let request = VNRecognizeTextRequest()
        request.recognitionLevel = .accurate
        request.usesLanguageCorrection = true
        let supported = (try? request.supportedRecognitionLanguages()) ?? ["en-US"]
        request.recognitionLanguages = recognitionLanguages(for: language, supported: supported)
        let handler = VNImageRequestHandler(cgImage: image, orientation: .up, options: [:])
        do {
            try handler.perform([request])
        } catch {
            throw Failure.recognitionFailed
        }
        let sampler = PixelSampler(image: image)
        return (request.results ?? []).compactMap { observation in
            guard let candidate = observation.topCandidates(1).first else { return nil }
            let vision = observation.boundingBox
            // Vision's origin is bottom-left; the analyser's is top-left.
            let box = CGRect(x: vision.minX, y: 1 - vision.maxY, width: vision.width, height: vision.height)
            let background = sampler?.background(around: box)
            let foreground = background.flatMap { sampler?.foreground(in: box, background: $0) }
            return RecognizedLine(text: candidate.string, box: box, confidence: Double(candidate.confidence),
                                  background: background, foreground: foreground)
        }
    }

    // MARK: Images

    /// A decoded, upright image from JPEG, PNG or HEIC data.
    static func image(from data: Data) -> CGImage? {
        guard let source = CGImageSourceCreateWithData(data as CFData, nil) else { return nil }
        let options: [CFString: Any] = [
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceCreateThumbnailWithTransform: true,
            kCGImageSourceThumbnailMaxPixelSize: 4096,
            kCGImageSourceShouldCacheImmediately: true,
        ]
        return CGImageSourceCreateThumbnailAtIndex(source, 0, options as CFDictionary)
    }

    /// JPEG with its longer side at most `maxSide` (never enlarged).
    static func jpeg(_ image: CGImage, maxSide: Int, quality: Double = 0.82) -> Data? {
        var output = image
        let longest = max(image.width, image.height)
        if longest > maxSide {
            let scale = Double(maxSide) / Double(longest)
            let width = max(1, Int(Double(image.width) * scale)), height = max(1, Int(Double(image.height) * scale))
            guard let context = CGContext(data: nil, width: width, height: height, bitsPerComponent: 8, bytesPerRow: 0,
                                          space: CGColorSpace(name: CGColorSpace.sRGB)!,
                                          bitmapInfo: CGImageAlphaInfo.noneSkipLast.rawValue) else { return nil }
            context.interpolationQuality = .high
            context.draw(image, in: CGRect(x: 0, y: 0, width: width, height: height))
            guard let scaled = context.makeImage() else { return nil }
            output = scaled
        }
        let data = NSMutableData()
        guard let destination = CGImageDestinationCreateWithData(data, UTType.jpeg.identifier as CFString, 1, nil) else { return nil }
        CGImageDestinationAddImage(destination, output, [kCGImageDestinationLossyCompressionQuality: quality] as CFDictionary)
        guard CGImageDestinationFinalize(destination) else { return nil }
        return data as Data
    }

    /// The part of an image inside a normalised (top-left origin) rectangle.
    static func crop(_ image: CGImage, to rect: CGRect) -> CGImage? {
        let bounds = CGRect(x: 0, y: 0, width: image.width, height: image.height)
        let pixels = CGRect(x: rect.minX * bounds.width, y: rect.minY * bounds.height,
                            width: rect.width * bounds.width, height: rect.height * bounds.height)
            .integral.intersection(bounds)
        guard pixels.width >= 32, pixels.height >= 32 else { return nil }
        return image.cropping(to: pixels)
    }
}

/// Colours of a downscaled copy of the screenshot: the bubble around a line
/// and the colour of its letters.
final class PixelSampler {
    private let width: Int
    private let height: Int
    private var pixels: [UInt8]

    init?(image: CGImage, maxWidth: Int = 480) {
        let scale = min(1, Double(maxWidth) / Double(max(image.width, 1)))
        width = max(1, Int(Double(image.width) * scale))
        height = max(1, Int(Double(image.height) * scale))
        pixels = [UInt8](repeating: 0, count: width * height * 4)
        let drawn = pixels.withUnsafeMutableBytes { buffer -> Bool in
            guard let context = CGContext(data: buffer.baseAddress, width: width, height: height, bitsPerComponent: 8,
                                          bytesPerRow: width * 4, space: CGColorSpace(name: CGColorSpace.sRGB)!,
                                          bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue) else { return false }
            context.interpolationQuality = .medium
            context.draw(image, in: CGRect(x: 0, y: 0, width: width, height: height))
            return true
        }
        guard drawn else { return nil }
    }

    /// Normalised, top-left origin (the bitmap's rows already run top-down).
    func color(x: Double, y: Double) -> ScreenshotColor? {
        let px = Int(x * Double(width)), py = Int(y * Double(height))
        guard px >= 0, py >= 0, px < width, py < height else { return nil }
        let index = (py * width + px) * 4
        return ScreenshotColor(rgb: Int(pixels[index]), Int(pixels[index + 1]), Int(pixels[index + 2]))
    }

    /// The median colour of a ring just outside the line: its bubble.
    func background(around box: CGRect) -> ScreenshotColor? {
        let padY = Double(box.height) * 0.45
        let padX = padY * Double(height) / Double(max(width, 1))
        var samples: [ScreenshotColor] = []
        for step in 0...8 {
            let x = Double(box.minX) + Double(box.width) * Double(step) / 8
            for y in [Double(box.minY) - padY, Double(box.maxY) + padY] {
                if let c = color(x: x, y: y) { samples.append(c) }
            }
        }
        for y in [Double(box.midY) - Double(box.height) * 0.25, Double(box.midY), Double(box.midY) + Double(box.height) * 0.25] {
            for x in [Double(box.minX) - padX, Double(box.maxX) + padX] {
                if let c = color(x: x, y: y) { samples.append(c) }
            }
        }
        guard samples.count >= 6 else { return nil }
        func median(_ values: [Double]) -> Double { values.sorted()[values.count / 2] }
        return ScreenshotColor(median(samples.map(\.red)), median(samples.map(\.green)), median(samples.map(\.blue)))
    }

    /// The average of the pixels inside the line that differ clearly from
    /// its background: the letters.
    func foreground(in box: CGRect, background: ScreenshotColor) -> ScreenshotColor? {
        var ink: [ScreenshotColor] = []
        let columns = max(8, Int(Double(box.width) * Double(width)))
        for row in [0.35, 0.5, 0.65] {
            let y = Double(box.minY) + Double(box.height) * row
            for step in 0..<columns {
                let x = Double(box.minX) + Double(box.width) * (Double(step) + 0.5) / Double(columns)
                if let c = color(x: x, y: y), c.distance(to: background) > 0.25 { ink.append(c) }
            }
        }
        guard ink.count >= 3 else { return nil }
        return ScreenshotColor.average(ink)
    }
}

/// From a picture to the stored latest screenshot: decode, recognise, store.
enum ScreenshotIntake {

    enum Failure: Error, Equatable {
        case unreadableImage
        case storageUnavailable
    }

    /// The stored picture's longer side; enough to read the smallest chat text.
    static let storedMaxSide = 2048
    /// The longer side sent for image analysis (the model reads ~768 px wide).
    static let uploadMaxSide = 2218

    /// A new screenshot replaces the previous one, on the iPhone only.
    static func ingest(_ data: Data, source: ScreenshotSource, instruction: String? = nil,
                       language: AppLanguage, store: ScreenshotContextStore = .shared) async throws -> ScreenshotContext {
        let (context, image) = try await Task.detached(priority: .userInitiated) {
            guard let image = ScreenshotRecognizer.image(from: data),
                  let jpeg = ScreenshotRecognizer.jpeg(image, maxSide: storedMaxSide) else {
                throw Failure.unreadableImage
            }
            let conversation = try recognize(image, language: language, fullScreen: true)
            let instruction = instruction?.trimmingCharacters(in: .whitespacesAndNewlines)
            let context = ScreenshotContext(
                id: UUID().uuidString, createdAt: Date(), source: source,
                imageWidth: image.width, imageHeight: image.height, conversation: conversation,
                hasNoText: conversation.isEmpty,
                instruction: instruction?.isEmpty == false ? instruction : nil)
            return (context, jpeg)
        }.value
        guard store.save(context, image: image) else { throw Failure.storageUnavailable }
        return context
    }

    /// The same screenshot cut to the part the person chose, read again. The
    /// cut replaces the stored picture: what is not in it is gone.
    static func crop(_ context: ScreenshotContext, to rect: CGRect, language: AppLanguage,
                     store: ScreenshotContextStore = .shared) async throws -> ScreenshotContext {
        guard let data = store.imageData(for: context) else { throw Failure.unreadableImage }
        let (updated, image) = try await Task.detached(priority: .userInitiated) {
            guard let full = ScreenshotRecognizer.image(from: data),
                  let cropped = ScreenshotRecognizer.crop(full, to: rect),
                  let jpeg = ScreenshotRecognizer.jpeg(cropped, maxSide: storedMaxSide) else {
                throw Failure.unreadableImage
            }
            var updated = context
            updated.conversation = try recognize(cropped, language: language, fullScreen: false)
            updated.conversation.title = context.conversation.title
            updated.hasNoText = updated.conversation.isEmpty
            updated.imageWidth = cropped.width
            updated.imageHeight = cropped.height
            updated.draft = nil
            return (updated, jpeg)
        }.value
        guard store.save(updated, image: image) else { throw Failure.storageUnavailable }
        return updated
    }

    static func recognize(_ image: CGImage, language: AppLanguage, fullScreen: Bool) throws -> ScreenshotConversation {
        let lines = try ScreenshotRecognizer.lines(in: image, language: language)
        return ConversationLayoutAnalyzer().analyze(lines, fullScreen: fullScreen).conversation
    }

    /// The picture as sent for image analysis, and its size.
    static func uploadImage(for context: ScreenshotContext, store: ScreenshotContextStore = .shared) -> (Data, Int, Int)? {
        guard let data = store.imageData(for: context), let image = ScreenshotRecognizer.image(from: data) else { return nil }
        let longest = max(image.width, image.height)
        let scale = min(1, Double(uploadMaxSide) / Double(longest))
        guard let jpeg = ScreenshotRecognizer.jpeg(image, maxSide: uploadMaxSide, quality: 0.75) else { return nil }
        return (jpeg, Int(Double(image.width) * scale), Int(Double(image.height) * scale))
    }
}

#if DEBUG
import UIKit

/// A drawn chat, read through the real pipeline, for reviewing the
/// screenshot screen in the simulator. DEBUG only.
enum DebugScreenshotSample {
    static func ingest(_ sample: String) async {
        let chats: [String: (String, [(String, Bool)])] = [
            "ru": ("Марат", [("Сможешь выйти в субботу?", false), ("Пока не знаю", true), ("Ответь до вечера, пожалуйста", false)]),
            "kk": ("Әсел", [("Сәлем! Ертең кездесейік пе?", false), ("Иә, кешке боламын", true), ("Онда сағат жетіде күтемін", false)]),
            "en": ("Aigerim", [("Hi! Are you available tomorrow?", false), ("I'm not sure yet.", true),
                               ("We really need your help with the project.", false)]),
            "uz": ("Dilnoza", [("Salom! Ertaga uchrashamizmi?", false), ("Ha, kechqurun bo'shman", true), ("Unda soat yettida kutaman", false)]),
        ]
        let dark = sample == "dark"
        let (title, bubbles) = chats[dark ? "ru" : sample] ?? chats["ru"]!
        let size = CGSize(width: 1179, height: 2556)
        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        let image = UIGraphicsImageRenderer(size: size, format: format).image { context in
            (dark ? UIColor(red: 0.06, green: 0.09, blue: 0.13, alpha: 1) : UIColor(red: 0.94, green: 0.92, blue: 0.89, alpha: 1)).setFill()
            context.fill(CGRect(origin: .zero, size: size))
            let text: UIColor = dark ? .white : .black
            ("9:41" as NSString).draw(at: CGPoint(x: 90, y: 50), withAttributes: [.font: UIFont.systemFont(ofSize: 48, weight: .semibold), .foregroundColor: text])
            let titleFont = UIFont.systemFont(ofSize: 54, weight: .semibold)
            let width = (title as NSString).size(withAttributes: [.font: titleFont]).width
            (title as NSString).draw(at: CGPoint(x: (size.width - width) / 2, y: 175), withAttributes: [.font: titleFont, .foregroundColor: text])
            var y: CGFloat = 380
            for (message, mine) in bubbles {
                let font = UIFont.systemFont(ofSize: 51)
                let box = (message as NSString).boundingRect(with: CGSize(width: 760, height: 2000), options: [.usesLineFragmentOrigin],
                                                             attributes: [.font: font], context: nil).integral
                let rect = CGRect(x: mine ? size.width - box.width - 120 : 48, y: y, width: box.width + 72, height: box.height + 100)
                (mine ? (dark ? UIColor(red: 0.17, green: 0.32, blue: 0.47, alpha: 1) : UIColor(red: 0.85, green: 0.99, blue: 0.83, alpha: 1))
                      : (dark ? UIColor(red: 0.09, green: 0.15, blue: 0.2, alpha: 1) : .white)).setFill()
                UIBezierPath(roundedRect: rect, cornerRadius: 42).fill()
                (message as NSString).draw(with: CGRect(x: rect.minX + 36, y: y + 24, width: 760, height: box.height),
                                           options: [.usesLineFragmentOrigin], attributes: [.font: font, .foregroundColor: text], context: nil)
                ("12:30" as NSString).draw(at: CGPoint(x: rect.maxX - 120, y: rect.maxY - 60),
                                           withAttributes: [.font: UIFont.systemFont(ofSize: 33), .foregroundColor: text.withAlphaComponent(0.6)])
                y = rect.maxY + 36
            }
        }
        guard let data = image.pngData() else { return }
        _ = try? await ScreenshotIntake.ingest(data, source: .photos, language: SharedSettings.shared.effectiveAppLanguage)
    }
}
#endif
