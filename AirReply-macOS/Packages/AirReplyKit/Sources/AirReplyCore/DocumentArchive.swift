import Foundation
import AirReplyArchive

/// Bounded in-memory ZIP reader for Office Open XML. It never extracts paths,
/// follows links, runs code, evaluates formulas or opens external relationships.
/// ZIP64, encryption, split archives and unknown compression are rejected.
struct DocumentArchive {
    struct Entry {
        let name: String
        let method: Int
        let flags: Int
        let compressed: Int
        let expanded: Int
        let crc: UInt32
        let offset: Int
    }
    private let bytes: [UInt8]
    private(set) var entries: [String: Entry] = [:]

    init(data: Data, limits: DocumentImportLimits) throws {
        bytes = Array(data)
        guard bytes.count >= 22 else { throw DocumentImportError.malformed }
        let start = max(0, bytes.count - 65_557)
        guard let end = stride(from: bytes.count - 22, through: start, by: -1).first(where: {
            value32($0) == 0x06054b50 && $0 + 22 + value16($0 + 20) == bytes.count
        }) else { throw DocumentImportError.malformed }
        let count = value16(end + 10), directorySize = value32(end + 12), directoryOffset = value32(end + 16)
        guard value16(end + 4) == 0, value16(end + 6) == 0, value16(end + 8) == count,
              count < 65_535, count <= limits.maximumArchiveEntries, directoryOffset + directorySize == end else {
            throw DocumentImportError.unsafeArchive
        }
        var cursor = directoryOffset, expandedTotal = 0
        for _ in 0..<count {
            guard cursor >= 0, cursor + 46 <= end, value32(cursor) == 0x02014b50 else { throw DocumentImportError.malformed }
            let flags = value16(cursor + 8), method = value16(cursor + 10)
            let compressed = value32(cursor + 20), expanded = value32(cursor + 24)
            let nameLength = value16(cursor + 28), extraLength = value16(cursor + 30), commentLength = value16(cursor + 32)
            let next = cursor + 46 + nameLength + extraLength + commentLength
            guard next <= end, nameLength > 0, flags & 0x41 == 0, method == 0 || method == 8,
                  value16(cursor + 34) == 0, value32(cursor + 42) < directoryOffset,
                  compressed < 0xffff_ffff, expanded < 0xffff_ffff else { throw DocumentImportError.unsafeArchive }
            guard let name = String(bytes: bytes[(cursor + 46)..<(cursor + 46 + nameLength)], encoding: .utf8),
                  !name.hasPrefix("/"), !name.contains("\\"), !name.contains(":"), !name.contains("\0"),
                  !name.split(separator: "/", omittingEmptySubsequences: false).contains(".."),
                  !name.split(separator: "/", omittingEmptySubsequences: false).contains("."),
                  entries[name] == nil else { throw DocumentImportError.unsafeArchive }
            // Unix file mode in external attributes: disallow symlinks and devices.
            let kind = (value32(cursor + 38) >> 16) & 0xf000
            guard kind == 0 || kind == 0x8000 || kind == 0x4000 else { throw DocumentImportError.unsafeArchive }
            expandedTotal += expanded
            guard expandedTotal <= limits.maximumExpandedBytes else { throw DocumentImportError.tooLarge }
            entries[name] = Entry(name: name, method: method, flags: flags, compressed: compressed,
                                  expanded: expanded, crc: UInt32(value32(cursor + 16)), offset: value32(cursor + 42))
            cursor = next
        }
        guard cursor == end else { throw DocumentImportError.malformed }
        // Macro-enabled and embedded executable parts are not supported, even
        // when renamed with a harmless-looking .docx/.xlsx extension.
        guard !entries.keys.contains(where: { $0.lowercased().contains("vbaproject") || $0.lowercased().hasSuffix(".exe") }) else {
            throw DocumentImportError.unsafeArchive
        }
    }

    func data(_ name: String) throws -> Data? {
        guard let entry = entries[name] else { return nil }
        let offset = entry.offset
        guard offset >= 0, offset + 30 <= bytes.count, value32(offset) == 0x04034b50,
              value16(offset + 6) == entry.flags, value16(offset + 8) == entry.method else { throw DocumentImportError.malformed }
        let nameLength = value16(offset + 26), extraLength = value16(offset + 28)
        let start = offset + 30 + nameLength + extraLength
        guard start <= bytes.count, entry.compressed <= bytes.count - start,
              String(bytes: bytes[(offset + 30)..<(offset + 30 + nameLength)], encoding: .utf8) == entry.name else {
            throw DocumentImportError.malformed
        }
        let compressed = Array(bytes[start..<(start + entry.compressed)])
        let output: [UInt8]
        if entry.method == 0 {
            guard entry.expanded == entry.compressed else { throw DocumentImportError.malformed }
            output = compressed
        } else {
            var buffer = [UInt8](repeating: 0, count: max(1, entry.expanded))
            let valid = compressed.withUnsafeBufferPointer { input in
                buffer.withUnsafeMutableBufferPointer { out in
                    air_reply_inflate(input.baseAddress, input.count, out.baseAddress, entry.expanded)
                }
            }
            guard valid == 1 else { throw DocumentImportError.malformed }
            output = Array(buffer.prefix(entry.expanded))
        }
        let crc = output.withUnsafeBufferPointer { air_reply_crc32($0.baseAddress, $0.count) }
        guard crc == entry.crc else { throw DocumentImportError.malformed }
        return Data(output)
    }

    private func value16(_ offset: Int) -> Int {
        guard offset >= 0, offset <= bytes.count - 2 else { return -1 }
        return Int(bytes[offset]) | (Int(bytes[offset + 1]) << 8)
    }
    private func value32(_ offset: Int) -> Int {
        guard offset >= 0, offset <= bytes.count - 4 else { return -1 }
        return Int(bytes[offset]) | (Int(bytes[offset + 1]) << 8) | (Int(bytes[offset + 2]) << 16) | (Int(bytes[offset + 3]) << 24)
    }
}
