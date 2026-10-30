// Builds the macOS AppIcon set from the iOS 1024 px icon.
// The iOS artwork already has its own rounded square on a white field, so the
// square is found, cut out and redrawn on the macOS grid (824 px tile with a
// soft shadow inside a transparent 1024 px canvas).
//
//   swift scripts/make-icons.swift ../AI-Reply/AIReply/Resources/Assets.xcassets/AppIcon.appiconset/AppIcon-1024.png AirReply/Assets.xcassets/AppIcon.appiconset
import AppKit

let arguments = CommandLine.arguments
guard arguments.count == 3,
      let source = NSImage(contentsOfFile: arguments[1]),
      let artwork = source.cgImage(forProposedRect: nil, context: nil, hints: nil) else {
    FileHandle.standardError.write(Data("usage: make-icons <AppIcon-1024.png> <AppIcon.appiconset>\n".utf8))
    exit(1)
}
let output = URL(fileURLWithPath: arguments[2], isDirectory: true)
let space = CGColorSpace(name: CGColorSpace.sRGB)!

func pixels(of image: CGImage) -> (UnsafeMutablePointer<UInt8>, Int, Int) {
    let width = image.width, height = image.height
    let buffer = UnsafeMutablePointer<UInt8>.allocate(capacity: width * height * 4)
    let context = CGContext(data: buffer, width: width, height: height, bitsPerComponent: 8, bytesPerRow: width * 4,
                            space: space, bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
    context.draw(image, in: CGRect(x: 0, y: 0, width: width, height: height))
    return (buffer, width, height)
}

// The tile is the first clearly non-white pixel along the middle row and column.
func tileBounds(of image: CGImage) -> CGRect {
    let (data, width, height) = pixels(of: image)
    defer { data.deallocate() }
    func isArtwork(_ x: Int, _ y: Int) -> Bool {
        let i = (y * width + x) * 4
        let darkest = min(data[i], data[i + 1], data[i + 2])
        return darkest < 200
    }
    let midY = height / 2, midX = width / 2
    let left = (0..<width).first { isArtwork($0, midY) } ?? 0
    let right = (0..<width).reversed().first { isArtwork($0, midY) } ?? width - 1
    let top = (0..<height).first { isArtwork(midX, $0) } ?? 0
    let bottom = (0..<height).reversed().first { isArtwork(midX, $0) } ?? height - 1
    let side = min(right - left, bottom - top)
    return CGRect(x: left, y: top, width: side, height: side)
}

let bounds = tileBounds(of: artwork)
guard let tile = artwork.cropping(to: bounds) else { exit(1) }

func render(_ size: Int) -> Data {
    let context = CGContext(data: nil, width: size, height: size, bitsPerComponent: 8, bytesPerRow: 0,
                            space: space, bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
    context.interpolationQuality = .high
    let scale = CGFloat(size) / 1024
    let rect = CGRect(x: 100 * scale, y: 100 * scale, width: 824 * scale, height: 824 * scale)
    let shape = CGPath(roundedRect: rect, cornerWidth: 185.4 * scale, cornerHeight: 185.4 * scale, transform: nil)

    context.saveGState()
    context.setShadow(offset: CGSize(width: 0, height: -10 * scale), blur: 22 * scale,
                      color: CGColor(gray: 0, alpha: 0.30))
    context.addPath(shape)
    context.setFillColor(CGColor(red: 0.04, green: 0.09, blue: 0.35, alpha: 1))
    context.fillPath()
    context.restoreGState()

    // Drawn slightly larger than the tile so its own white corners stay outside the clip.
    context.saveGState()
    context.addPath(shape)
    context.clip()
    context.draw(tile, in: rect.insetBy(dx: -rect.width * 0.035, dy: -rect.height * 0.035))
    context.restoreGState()

    let rep = NSBitmapImageRep(cgImage: context.makeImage()!)
    return rep.representation(using: .png, properties: [:])!
}

let files: [(String, Int)] = [
    ("icon_16x16.png", 16), ("icon_16x16@2x.png", 32),
    ("icon_32x32.png", 32), ("icon_32x32@2x.png", 64),
    ("icon_128x128.png", 128), ("icon_128x128@2x.png", 256),
    ("icon_256x256.png", 256), ("icon_256x256@2x.png", 512),
    ("icon_512x512.png", 512), ("icon_512x512@2x.png", 1024),
]
try FileManager.default.createDirectory(at: output, withIntermediateDirectories: true)
for (name, size) in files {
    try render(size).write(to: output.appendingPathComponent(name))
}
print("tile \(bounds.integral) -> \(files.count) icons in \(output.path)")
