// Generates PDF fixtures with CoreGraphics/CoreText. Usage: make_pdfs <fixtures/case09 dir>
import CoreGraphics
import CoreText
import Foundation
import PDFKit

let out = URL(fileURLWithPath: CommandLine.arguments[1], isDirectory: true)
let page = CGRect(x: 0, y: 0, width: 595, height: 842)

func line(_ ctx: CGContext, _ s: String, x: CGFloat, y: CGFloat, size: CGFloat = 11, bold: Bool = false) {
    let font = CTFontCreateWithName((bold ? "Helvetica-Bold" : "Helvetica") as CFString, size, nil)
    let attributed = NSAttributedString(string: s, attributes: [NSAttributedString.Key(kCTFontAttributeName as String): font])
    ctx.textPosition = CGPoint(x: x, y: y)
    CTLineDraw(CTLineCreateWithAttributedString(attributed), ctx)
}

func makePDF(_ name: String, info: [CFString: Any]? = nil, draw: (CGContext) -> Void) {
    var box = page
    let url = out.appendingPathComponent(name)
    guard let ctx = CGContext(url as CFURL, mediaBox: &box, info as CFDictionary?) else { fatalError("no context for \(name)") }
    ctx.beginPDFPage(nil)
    draw(ctx)
    ctx.endPDFPage()
    ctx.closePDF()
}

// 1. Newspaper-style two-column price list: both columns share baselines.
let left = [("iPhone 15", "449 990 ₸"), ("iPhone 15 Pro", "599 990 ₸"), ("iPhone 15 Pro Max", "699 990 ₸"), ("AirPods Pro 2", "119 990 ₸")]
let right = [("Кофе Арабика 250 г", "3 490 ₸"), ("Кофе Арабика 1 кг", "11 990 ₸"), ("Кофе Робуста 1 кг", "8 990 ₸"), ("Капсулы Nespresso", "2 990 ₸")]
func drawTwoColumns(_ ctx: CGContext) {
    line(ctx, "Прайс-лист, ноябрь 2026", x: 40, y: 790, size: 16, bold: true)
    line(ctx, "Смартфоны", x: 40, y: 750, size: 12, bold: true)
    line(ctx, "Кофе", x: 310, y: 750, size: 12, bold: true)
    for i in 0..<4 {
        let y = 725 - CGFloat(i) * 20
        line(ctx, left[i].0, x: 40, y: y); line(ctx, left[i].1, x: 200, y: y)
        line(ctx, right[i].0, x: 310, y: y); line(ctx, right[i].1, x: 490, y: y)
    }
}
makePDF("price_two_column.pdf", draw: drawTwoColumns)

// 2. Simple single table: name, price and stock as separately positioned runs.
let table = [("Диван угловой «Комфорт»", "289 990 ₸", "2 шт"), ("Диван-кровать «Ника»", "159 990 ₸", "под заказ"),
             ("Кресло «Релакс»", "89 990 ₸", "5 шт"), ("Шкаф-купе 2-дверный", "139 990 ₸", "нет")]
func drawTable(_ ctx: CGContext) {
    line(ctx, "ТОО «Уют Мебель» — прайс-лист от 01.11.2026", x: 40, y: 790, size: 14, bold: true)
    line(ctx, "Наименование", x: 40, y: 750, bold: true); line(ctx, "Цена", x: 380, y: 750, bold: true); line(ctx, "Наличие", x: 490, y: 750, bold: true)
    for (i, row) in table.enumerated() {
        let y = 728 - CGFloat(i) * 20
        line(ctx, row.0, x: 40, y: y); line(ctx, row.1, x: 380, y: y); line(ctx, row.2, x: 490, y: y)
    }
}
makePDF("price_table.pdf", draw: drawTable)

// 3. Image-only ("scan"): the same table rasterised into a bitmap, no text layer.
func drawScan(_ ctx: CGContext) {
    let scale: CGFloat = 2
    let width = Int(page.width * scale), height = Int(page.height * scale)
    let bitmap = CGContext(data: nil, width: width, height: height, bitsPerComponent: 8, bytesPerRow: 0,
                           space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
    bitmap.setFillColor(CGColor(red: 1, green: 1, blue: 1, alpha: 1)); bitmap.fill(CGRect(x: 0, y: 0, width: width, height: height))
    bitmap.scaleBy(x: scale, y: scale)
    drawTable(bitmap)
    ctx.draw(bitmap.makeImage()!, in: page)
}
makePDF("scan_image_only.pdf", draw: drawScan)

// 4. Password-protected (user password) and 5. owner-password-only ("protected from editing") PDFs.
makePDF("encrypted_user_password.pdf", info: [kCGPDFContextUserPassword: "1234", kCGPDFContextOwnerPassword: "owner-secret"], draw: drawTable)
makePDF("owner_password_only.pdf", info: [kCGPDFContextOwnerPassword: "owner-secret", kCGPDFContextAllowsCopying: false], draw: drawTable)

for name in ["price_two_column.pdf", "price_table.pdf", "scan_image_only.pdf", "encrypted_user_password.pdf", "owner_password_only.pdf"] {
    let url = out.appendingPathComponent(name)
    guard let doc = PDFDocument(url: url) else { print(name, "PDFKit cannot open"); continue }
    let text = doc.page(at: 0)?.string ?? "<nil>"
    print("== \(name): isEncrypted=\(doc.isEncrypted) isLocked=\(doc.isLocked) allowsCopying=\(doc.allowsCopying) pages=\(doc.pageCount)")
    print(text.debugDescription)
}
