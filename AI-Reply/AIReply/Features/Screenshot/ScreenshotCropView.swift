import SwiftUI

/// Choosing the part of the screenshot to keep: drag the corners or the
/// frame. The rest is cut away on the iPhone before anything is read again.
struct ScreenshotCropView: View {
    let image: UIImage
    let onCrop: (CGRect) -> Void

    @Environment(\.dismiss) private var dismiss
    /// Normalised, top-left origin.
    @State private var crop = CGRect(x: 0, y: 0.08, width: 1, height: 0.84)
    @State private var dragStart: CGRect?

    private let minimum: CGFloat = 0.12

    var body: some View {
        NavigationStack {
            GeometryReader { proxy in
                let frame = fittedFrame(in: proxy.size)
                ZStack(alignment: .topLeading) {
                    Image(uiImage: image)
                        .resizable()
                        .scaledToFit()
                        .frame(width: frame.width, height: frame.height)
                        .offset(x: frame.minX, y: frame.minY)
                        .accessibilityLabel(Text("screenshot.preview"))
                    // Dim what will be cut away.
                    Path { path in
                        path.addRect(frame)
                        path.addRect(rect(in: frame))
                    }
                    .fill(Color.black.opacity(0.5), style: FillStyle(eoFill: true))
                    .allowsHitTesting(false)
                    let selection = rect(in: frame)
                    Rectangle()
                        .strokeBorder(Color.white, lineWidth: 2)
                        .frame(width: selection.width, height: selection.height)
                        .offset(x: selection.minX, y: selection.minY)
                        .contentShape(Rectangle())
                        .gesture(DragGesture().onChanged { value in move(by: value.translation, in: frame) }
                            .onEnded { _ in dragStart = nil })
                        .accessibilityLabel(Text("screenshot.crop.area"))
                    ForEach(Corner.allCases, id: \.self) { corner in
                        let point = corner.point(in: selection)
                        Circle()
                            .fill(Color.white)
                            .frame(width: 22, height: 22)
                            .shadow(radius: 2)
                            .frame(width: 44, height: 44)
                            .contentShape(Rectangle())
                            .offset(x: point.x - 22, y: point.y - 22)
                            .gesture(DragGesture().onChanged { value in resize(corner, by: value.translation, in: frame) }
                                .onEnded { _ in dragStart = nil })
                            .accessibilityHidden(true)
                    }
                }
            }
            .padding(DS.Spacing.m)
            .background(Color.black)
            .navigationTitle("screenshot.crop")
            .navigationBarTitleDisplayMode(.inline)
            .toolbarBackground(.visible, for: .navigationBar)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("common.cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("screenshot.crop.apply") {
                        onCrop(crop)
                        dismiss()
                    }
                    .accessibilityIdentifier("screenshot.crop.apply")
                }
                ToolbarItem(placement: .bottomBar) {
                    Button("screenshot.crop.reset") {
                        crop = CGRect(x: 0, y: 0, width: 1, height: 1)
                    }
                }
            }
        }
    }

    private enum Corner: CaseIterable {
        case topLeft, topRight, bottomLeft, bottomRight

        func point(in rect: CGRect) -> CGPoint {
            switch self {
            case .topLeft:     return CGPoint(x: rect.minX, y: rect.minY)
            case .topRight:    return CGPoint(x: rect.maxX, y: rect.minY)
            case .bottomLeft:  return CGPoint(x: rect.minX, y: rect.maxY)
            case .bottomRight: return CGPoint(x: rect.maxX, y: rect.maxY)
            }
        }
    }

    private func fittedFrame(in size: CGSize) -> CGRect {
        let ratio = image.size.width / max(image.size.height, 1)
        var width = size.width, height = width / ratio
        if height > size.height { height = size.height; width = height * ratio }
        return CGRect(x: (size.width - width) / 2, y: (size.height - height) / 2, width: width, height: height)
    }

    private func rect(in frame: CGRect) -> CGRect {
        CGRect(x: frame.minX + crop.minX * frame.width, y: frame.minY + crop.minY * frame.height,
               width: crop.width * frame.width, height: crop.height * frame.height)
    }

    private func move(by translation: CGSize, in frame: CGRect) {
        let start = dragStart ?? crop
        dragStart = start
        var next = start
        next.origin.x = min(max(0, start.minX + translation.width / frame.width), 1 - start.width)
        next.origin.y = min(max(0, start.minY + translation.height / frame.height), 1 - start.height)
        crop = next
    }

    private func resize(_ corner: Corner, by translation: CGSize, in frame: CGRect) {
        let start = dragStart ?? crop
        dragStart = start
        let dx = translation.width / frame.width, dy = translation.height / frame.height
        var minX = start.minX, minY = start.minY, maxX = start.maxX, maxY = start.maxY
        switch corner {
        case .topLeft:     minX += dx; minY += dy
        case .topRight:    maxX += dx; minY += dy
        case .bottomLeft:  minX += dx; maxY += dy
        case .bottomRight: maxX += dx; maxY += dy
        }
        minX = min(max(0, minX), maxX - minimum)
        minY = min(max(0, minY), maxY - minimum)
        maxX = max(min(1, maxX), minX + minimum)
        maxY = max(min(1, maxY), minY + minimum)
        crop = CGRect(x: minX, y: minY, width: maxX - minX, height: maxY - minY)
    }
}
