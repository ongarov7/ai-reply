import Foundation
#if canImport(FoundationXML)
import FoundationXML
#endif
import PDFKit

public struct DocumentImportLimits: Sendable {
    public var maximumFileBytes: Int
    public var maximumExpandedBytes: Int
    public var maximumArchiveEntries: Int
    public var maximumTextCharacters: Int
    public var maximumRows: Int
    public init(maximumFileBytes: Int = 8 * 1024 * 1024, maximumExpandedBytes: Int = 16 * 1024 * 1024,
                maximumArchiveEntries: Int = 2048, maximumTextCharacters: Int = 2_000_000, maximumRows: Int = 50_000) {
        self.maximumFileBytes = max(1, min(64 * 1024 * 1024, maximumFileBytes))
        self.maximumExpandedBytes = max(1, min(64 * 1024 * 1024, maximumExpandedBytes))
        self.maximumArchiveEntries = max(1, min(8192, maximumArchiveEntries))
        self.maximumTextCharacters = max(1, min(8_000_000, maximumTextCharacters))
        self.maximumRows = max(1, min(100_000, maximumRows))
    }
}

public enum DocumentImportError: Error, Equatable, Sendable {
    case unsupportedFormat, tooLarge, malformed, unsafeArchive, unreadable, noReadableText
}

/// Reads only a user-selected local URL and indexes its content away from the
/// main actor. Original files, paths and complete contents are never uploaded.
public struct DocumentContextImporter: Sendable {
    public let limits: DocumentImportLimits
    public init(limits: DocumentImportLimits = DocumentImportLimits()) { self.limits = limits }

    public func parse(url: URL) async throws -> ContextAttachment {
        let limits = limits
        let work = Task.detached(priority: .userInitiated) { try Self.parseSelected(url, limits: limits) }
        return try await withTaskCancellationHandler(operation: { try await work.value }, onCancel: { work.cancel() })
    }

    private static func parseSelected(_ url: URL, limits: DocumentImportLimits) throws -> ContextAttachment {
        let access = url.startAccessingSecurityScopedResource()
        defer { if access { url.stopAccessingSecurityScopedResource() } }
        try Task.checkCancellation()
        guard url.isFileURL else { throw DocumentImportError.unreadable }
        let format = url.pathExtension.lowercased()
        guard ["txt", "csv", "pdf", "docx", "xlsx"].contains(format) else { throw DocumentImportError.unsupportedFormat }
        let attributes = try url.resourceValues(forKeys: [.isRegularFileKey, .isSymbolicLinkKey, .fileSizeKey])
        guard attributes.isRegularFile == true, attributes.isSymbolicLink != true else { throw DocumentImportError.unreadable }
        guard (attributes.fileSize ?? limits.maximumFileBytes + 1) <= limits.maximumFileBytes else { throw DocumentImportError.tooLarge }
        let handle = try FileHandle(forReadingFrom: url)
        defer { try? handle.close() }
        let data = try handle.read(upToCount: limits.maximumFileBytes + 1) ?? Data()
        guard data.count <= limits.maximumFileBytes else { throw DocumentImportError.tooLarge }
        return try parse(data: data, name: url.lastPathComponent, format: format, limits: limits)
    }

    // Internal data entry point allows deterministic malformed-document fixtures.
    static func parse(data: Data, name: String, format: String, limits: DocumentImportLimits) throws -> ContextAttachment {
        guard data.count <= limits.maximumFileBytes else { throw DocumentImportError.tooLarge }
        try Task.checkCancellation()
        var segments: [ReferenceSegment]
        switch format {
        case "txt": segments = try textSegments(decodeText(data), limits: limits)
        case "csv": segments = try tableSegments(rows: csvRows(decodeText(data), limits: limits), section: "CSV", limits: limits)
        case "pdf":
            guard data.starts(with: Data("%PDF-".utf8)), let pdf = PDFDocument(data: data), !pdf.isEncrypted,
                  pdf.pageCount <= 1000 else { throw DocumentImportError.unreadable }
            var output: [ReferenceSegment] = [], total = 0
            for page in 0..<pdf.pageCount {
                try Task.checkCancellation()
                let text = pdf.page(at: page)?.string ?? ""
                total += text.unicodeScalars.count
                guard total <= limits.maximumTextCharacters else { throw DocumentImportError.tooLarge }
                output += try textSegments(text, section: "Page \(page + 1)", limits: limits)
            }
            segments = output
        case "docx":
            let archive = try DocumentArchive(data: data, limits: limits)
            guard let xml = try archive.data("word/document.xml") else { throw DocumentImportError.malformed }
            let reader = OfficeXMLReader(limits: limits, mode: .word)
            try reader.read(xml)
            segments = try textSegments(reader.paragraphs.joined(separator: "\n\n"), limits: limits)
        case "xlsx": segments = try spreadsheet(data, limits: limits)
        default: throw DocumentImportError.unsupportedFormat
        }
        segments = segments.filter { !$0.text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }
        guard !segments.isEmpty else { throw DocumentImportError.noReadableText }
        guard segments.reduce(0, { $0 + $1.text.unicodeScalars.count }) <= limits.maximumTextCharacters else { throw DocumentImportError.tooLarge }
        return ContextAttachment(name: String(name.prefix(200)), format: format, segments: segments)
    }

    private static func decodeText(_ data: Data) throws -> String {
        guard var text = String(data: data, encoding: .utf8) ?? String(data: data, encoding: .utf16),
              !text.unicodeScalars.contains(where: { CharacterSet.controlCharacters.contains($0) && !CharacterSet.whitespacesAndNewlines.contains($0) && $0.value != 0xfeff }) else {
            throw DocumentImportError.unreadable
        }
        if text.hasPrefix("\u{feff}") { text.removeFirst() }
        return text
    }

    private static func textSegments(_ text: String, section: String? = nil, limits: DocumentImportLimits) throws -> [ReferenceSegment] {
        guard text.unicodeScalars.count <= limits.maximumTextCharacters else { throw DocumentImportError.tooLarge }
        var output: [ReferenceSegment] = []
        // Paragraphs remain independent; long paragraphs are split at word
        // boundaries so retrieval can stay inside a small request budget.
        for paragraph in text.components(separatedBy: .newlines) {
            var chunk = ""
            for word in paragraph.split(whereSeparator: \.isWhitespace) {
                guard word.unicodeScalars.count <= 2000 else { throw DocumentImportError.malformed }
                if chunk.unicodeScalars.count + word.unicodeScalars.count + 1 > 1200 {
                    output.append(ReferenceSegment(text: chunk, section: section)); chunk = ""
                }
                if !chunk.isEmpty { chunk += " " }
                chunk += word
            }
            if !chunk.isEmpty { output.append(ReferenceSegment(text: chunk, section: section)) }
            guard output.count <= limits.maximumRows else { throw DocumentImportError.tooLarge }
        }
        return output
    }

    private static func csvRows(_ text: String, limits: DocumentImportLimits) throws -> [[String]] {
        guard text.unicodeScalars.count <= limits.maximumTextCharacters else { throw DocumentImportError.tooLarge }
        let firstLine = text.prefix { !$0.isNewline }
        let separator: Character = firstLine.filter { $0 == ";" }.count > firstLine.filter { $0 == "," }.count ? ";" : ","
        var rows: [[String]] = [], row: [String] = [], field = "", quoted = false, afterQuote = false
        let characters = Array(text)
        var i = 0
        func finishField() throws {
            guard row.count < 256, field.unicodeScalars.count <= 4000 else { throw DocumentImportError.tooLarge }
            row.append(field.trimmingCharacters(in: .whitespacesAndNewlines)); field = ""; afterQuote = false
        }
        func finishRow() throws {
            try finishField()
            if row.contains(where: { !$0.isEmpty }) { rows.append(row) }
            row = []
            guard rows.count <= limits.maximumRows else { throw DocumentImportError.tooLarge }
        }
        while i < characters.count {
            if i.isMultiple(of: 1024) { try Task.checkCancellation() }
            let c = characters[i]
            if quoted {
                if c == "\"" {
                    if i + 1 < characters.count, characters[i + 1] == "\"" { field.append(c); i += 1 } else { quoted = false; afterQuote = true }
                } else { field.append(c) }
            } else if c == "\"" {
                guard field.isEmpty, !afterQuote else { throw DocumentImportError.malformed }
                quoted = true
            } else if c == separator { try finishField() }
            else if c.isNewline {
                try finishRow()
                if c == "\r", i + 1 < characters.count, characters[i + 1] == "\n" { i += 1 }
            } else {
                guard !afterQuote || c.isWhitespace else { throw DocumentImportError.malformed }
                field.append(c)
            }
            guard field.unicodeScalars.count <= 4000 else { throw DocumentImportError.tooLarge }
            i += 1
        }
        guard !quoted else { throw DocumentImportError.malformed }
        if !row.isEmpty || !field.isEmpty || afterQuote { try finishRow() }
        return rows
    }

    private static func tableSegments(rows: [[String]], section: String, limits: DocumentImportLimits) throws -> [ReferenceSegment] {
        guard let headers = rows.first, !headers.isEmpty else { return [] }
        var output: [ReferenceSegment] = [], total = 0
        for (index, row) in rows.dropFirst().enumerated() {
            try Task.checkCancellation()
            let fields = row.enumerated().compactMap { column, value -> String? in
                guard !value.isEmpty else { return nil }
                let header = column < headers.count && !headers[column].isEmpty ? headers[column] : "Column \(column + 1)"
                return "\(header): \(value)"
            }
            let text = fields.joined(separator: "; ")
            total += text.unicodeScalars.count
            guard total <= limits.maximumTextCharacters else { throw DocumentImportError.tooLarge }
            if !text.isEmpty { output.append(ReferenceSegment(text: text, section: "\(section), row \(index + 2)")) }
        }
        return output
    }

    private static func spreadsheet(_ data: Data, limits: DocumentImportLimits) throws -> [ReferenceSegment] {
        let archive = try DocumentArchive(data: data, limits: limits)
        guard let workbook = try archive.data("xl/workbook.xml"), let relationships = try archive.data("xl/_rels/workbook.xml.rels") else {
            throw DocumentImportError.malformed
        }
        let book = OfficeXMLReader(limits: limits, mode: .workbook)
        try book.read(workbook)
        let relations = OfficeXMLReader(limits: limits, mode: .relationships)
        try relations.read(relationships)
        var shared: [String] = []
        if let strings = try archive.data("xl/sharedStrings.xml") {
            let reader = OfficeXMLReader(limits: limits, mode: .strings)
            try reader.read(strings); shared = reader.strings
        }
        var output: [ReferenceSegment] = [], total = 0
        guard book.sheets.count <= 128 else { throw DocumentImportError.tooLarge }
        for sheet in book.sheets {
            try Task.checkCancellation()
            guard let target = relations.relationships[sheet.1],
                  !target.hasPrefix("/"), !target.contains("\\"), !target.contains(":"),
                  !target.split(separator: "/").contains(".."), target.hasPrefix("worksheets/"),
                  let xml = try archive.data("xl/" + target) else { throw DocumentImportError.unsafeArchive }
            let reader = OfficeXMLReader(limits: limits, mode: .sheet, shared: shared)
            try reader.read(xml)
            let segments = try tableSegments(rows: reader.rows, section: sheet.0, limits: limits)
            total += segments.reduce(0) { $0 + $1.text.unicodeScalars.count }
            guard total <= limits.maximumTextCharacters else { throw DocumentImportError.tooLarge }
            output += segments
        }
        return output
    }
}

private final class OfficeXMLReader: NSObject, XMLParserDelegate {
    enum Mode { case word, workbook, relationships, strings, sheet }
    let limits: DocumentImportLimits
    let mode: Mode
    let shared: [String]
    var paragraphs: [String] = [], strings: [String] = [], sheets: [(String, String)] = [], relationships: [String: String] = [:]
    var rows: [[String]] = []
    private var text = "", paragraph = "", cell = "", cellType = "", capture: String?, row: [String] = []
    private var column = 0, depth = 0, nodes = 0, characters = 0, hasFormula = false
    private var failure: DocumentImportError?

    init(limits: DocumentImportLimits, mode: Mode, shared: [String] = []) { self.limits = limits; self.mode = mode; self.shared = shared }

    func read(_ data: Data) throws {
        // Office files use UTF-8 XML here. Refusing DTD/entity declarations
        // before parsing also prevents internal entity expansion attacks.
        guard let xml = String(data: data, encoding: .utf8), !xml.contains("\0"),
              !xml.lowercased().contains("<!doctype"), !xml.lowercased().contains("<!entity") else {
            throw DocumentImportError.unsafeArchive
        }
        let parser = XMLParser(data: data)
        parser.shouldResolveExternalEntities = false
        parser.delegate = self
        guard parser.parse(), failure == nil else { throw failure ?? DocumentImportError.malformed }
    }

    private func local(_ name: String) -> String { String(name.split(separator: ":").last ?? "") }
    private func fail(_ parser: XMLParser, _ error: DocumentImportError) { failure = error; parser.abortParsing() }
    func parser(_ parser: XMLParser, didStartElement elementName: String, namespaceURI: String?, qualifiedName qName: String?, attributes a: [String: String] = [:]) {
        depth += 1; nodes += 1
        guard depth <= 64, nodes <= 500_000, !Task.isCancelled else { fail(parser, .tooLarge); return }
        let element = local(elementName)
        switch mode {
        case .word:
            if element == "p" { paragraph = "" }
            if element == "t" { capture = "t" }
            if element == "tab" || element == "br" { paragraph += " " }
        case .strings:
            if element == "si" { text = "" }
            if element == "t" { capture = "t" }
        case .workbook:
            if element == "sheet", let name = a["name"], let id = a["r:id"] { sheets.append((name, id)) }
        case .relationships:
            if element == "Relationship", let id = a["Id"], let target = a["Target"], a["TargetMode"] != "External" {
                if relationships[id] != nil { fail(parser, .malformed); return }
                relationships[id] = target
            }
        case .sheet:
            if element == "row" { row = [] }
            if element == "c" {
                cell = ""; cellType = a["t"] ?? ""; hasFormula = false
                if let reference = a["r"] {
                    var number = 0
                    for scalar in reference.uppercased().unicodeScalars.prefix(4) {
                        guard scalar.value >= 65 && scalar.value <= 90 else { break }
                        number = number * 26 + Int(scalar.value - 64)
                    }
                    column = max(0, number - 1)
                } else { column = row.count }
                guard column < 256 else { fail(parser, .tooLarge); return }
            }
            if element == "f" { hasFormula = true }
            if element == "v" || element == "t" { capture = element }
        }
    }

    func parser(_ parser: XMLParser, foundCharacters string: String) {
        characters += string.unicodeScalars.count
        guard characters <= limits.maximumTextCharacters else { fail(parser, .tooLarge); return }
        guard capture != nil else { return }
        switch mode {
        case .word: paragraph += string
        case .strings: text += string
        case .sheet: cell += string
        default: break
        }
        if cell.unicodeScalars.count > 4000 || text.unicodeScalars.count > 4000 { fail(parser, .tooLarge) }
    }

    func parser(_ parser: XMLParser, didEndElement elementName: String, namespaceURI: String?, qualifiedName qName: String?) {
        let element = local(elementName)
        if element == capture { capture = nil }
        switch mode {
        case .word:
            if element == "p", !paragraph.isEmpty { paragraphs.append(paragraph); paragraph = "" }
        case .strings:
            if element == "si" { strings.append(text); text = "" }
        case .sheet:
            if element == "c" {
                // Formula text is never used or evaluated; cached values can
                // be stale, so omit formula cells and ask for confirmation.
                var value = hasFormula ? "" : cell
                if !hasFormula, cellType == "s" {
                    guard let index = Int(cell), index >= 0, index < shared.count else { fail(parser, .malformed); return }
                    value = shared[index]
                }
                while row.count <= column { row.append("") }
                row[column] = value
            }
            if element == "row", row.contains(where: { !$0.isEmpty }) {
                rows.append(row)
                if rows.count > limits.maximumRows { fail(parser, .tooLarge) }
            }
        default: break
        }
        depth -= 1
    }
}
