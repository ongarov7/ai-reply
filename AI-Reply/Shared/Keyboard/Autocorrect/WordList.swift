import Foundation

/// The words one language can correct to or complete with, read from
/// `<lang>.words`: one word per line, most frequent first, so a word's rank is
/// its line index (comment lines starting with `#` excluded). Proper nouns are
/// listed capitalised, and English lists `I`.
///
/// MEMORY. Every word is stored once, as one byte per UTF-16 unit: a language
/// uses well under 64 distinct letters, so each unit becomes its index in the
/// sorted alphabet. Words are packed by length, in rank order, into a single
/// buffer - no object per word - which is also the order the candidate search
/// scans them in. About 1.8 MB for 80,000 Russian words.
struct WordList: Sendable {

    /// The words of one length, packed back to back in rank order.
    struct Bucket: Sendable {
        let length: Int
        /// Offset of the bucket's first word in `storage`.
        let start: Int
        let ranks: [Int32]
    }

    /// How every list, filter and query spells a word: lowercased, with the
    /// typographic apostrophe typed as the straight one the lists use.
    static func key(for word: some StringProtocol) -> String {
        let lowercased = word.lowercased()
        guard lowercased.contains("\u{2019}") else { return lowercased }
        return lowercased.replacingOccurrences(of: "\u{2019}", with: "'")
    }

    let count: Int
    /// Code → UTF-16 unit, ascending, so comparing codes compares the words.
    let alphabet: [UInt16]
    private let codes: [UInt16: UInt8]
    private let storage: [UInt8]
    private let buckets: [Bucket?]
    /// Rank → where the word starts in `storage`, and its length.
    private let offsets: [UInt32]
    private let lengths: [UInt8]
    /// Ranks in lexicographic order of their words: the prefix index.
    private let sorted: [Int32]
    /// Rank bitset: the listed form starts with a capital.
    private let capitalized: [UInt64]

    /// The code `encode` gives a unit that no listed word contains.
    var unknownCode: UInt8 { UInt8(alphabet.count) }

    init(contentsOf url: URL) throws {
        try self.init(data: Data(contentsOf: url, options: .alwaysMapped))
    }

    /// Reads the file line by line, so loading never holds a second copy of
    /// the whole list as strings.
    init(data: Data) throws {
        var keys = Keys()
        try data.withUnsafeBytes { (bytes: UnsafeRawBufferPointer) in
            var start = 0
            while start < bytes.count {
                let newline = bytes[start...].firstIndex(of: UInt8(ascii: "\n")) ?? bytes.count
                var end = newline
                if end > start, bytes[end - 1] == UInt8(ascii: "\r") { end -= 1 }
                if end > start, bytes[start] != UInt8(ascii: "#") {
                    try keys.append(String(decoding: UnsafeRawBufferPointer(rebasing: bytes[start..<end]), as: UTF8.self))
                }
                start = newline + 1
            }
        }
        try self.init(keys)
    }

    /// `words` in rank order, as listed.
    init<Words: Sequence>(words: Words) throws where Words.Element: StringProtocol {
        var keys = Keys()
        for word in words where !word.isEmpty {
            try keys.append(word)
        }
        try self.init(keys)
    }

    private init(_ keys: Keys) throws {
        var used = [Bool](repeating: false, count: Int(UInt16.max) + 1)
        for unit in keys.units { used[Int(unit)] = true }
        let alphabet = used.indices.filter { used[$0] }.map(UInt16.init)
        // One code is kept back for units no word contains.
        guard alphabet.count < Int(UInt8.max) else { throw AutocorrectDataError.malformed("alphabet size") }
        var codes: [UInt16: UInt8] = [:]
        var codeOfUnit = [UInt8](repeating: 0, count: used.count)
        for (code, unit) in alphabet.enumerated() {
            codes[unit] = UInt8(code)
            codeOfUnit[Int(unit)] = UInt8(code)
        }

        let lengths = (0..<keys.count).map { UInt8(keys.range(of: $0).count) }
        var ranksByLength: [[Int32]] = Array(repeating: [], count: Int(lengths.max() ?? 0) + 1)
        for (rank, length) in lengths.enumerated() { ranksByLength[Int(length)].append(Int32(rank)) }

        var storage: [UInt8] = []
        storage.reserveCapacity(keys.units.count)
        var buckets: [Bucket?] = []
        var offsets = [UInt32](repeating: 0, count: keys.count)
        for (length, ranks) in ranksByLength.enumerated() {
            guard !ranks.isEmpty else {
                buckets.append(nil)
                continue
            }
            buckets.append(Bucket(length: length, start: storage.count, ranks: ranks))
            for rank in ranks {
                offsets[Int(rank)] = UInt32(storage.count)
                storage.append(contentsOf: keys.units[keys.range(of: Int(rank))].map { codeOfUnit[Int($0)] })
            }
        }

        self.count = keys.count
        self.alphabet = alphabet
        self.codes = codes
        self.storage = storage
        self.buckets = buckets
        self.offsets = offsets
        self.lengths = lengths
        self.capitalized = keys.capitalized
        self.sorted = Self.lexicographicOrder(count: keys.count, storage: storage, offsets: offsets, lengths: lengths)
    }

    /// Every key back to back while a list is being read.
    private struct Keys {
        private(set) var units: [UInt16] = []
        private var ends: [UInt32] = []
        private(set) var capitalized: [UInt64] = []

        var count: Int { ends.count }

        func range(of rank: Int) -> Range<Int> {
            (rank == 0 ? 0 : Int(ends[rank - 1]))..<Int(ends[rank])
        }

        mutating func append(_ word: some StringProtocol) throws {
            let start = units.count
            units.append(contentsOf: WordList.key(for: word).utf16)
            guard units.count - start <= Int(UInt8.max) else { throw AutocorrectDataError.malformed("word length") }
            if count % 64 == 0 { capitalized.append(0) }
            if word.first?.isUppercase == true {
                capitalized[count / 64] |= 1 << UInt64(count % 64)
            }
            ends.append(UInt32(units.count))
        }
    }

    // MARK: Reading

    /// The word at `rank` as listed: `Москва`, `I`, `привет`.
    func word(at rank: Int) -> String {
        let key = self.key(at: rank)
        guard capitalized[rank / 64] & (1 << UInt64(rank % 64)) != 0 else { return key }
        return key.prefix(1).uppercased() + key.dropFirst()
    }

    /// The word at `rank`, lowercased.
    func key(at rank: Int) -> String {
        let start = Int(offsets[rank])
        let units = storage[start..<start + Int(lengths[rank])].map { alphabet[Int($0)] }
        return String(decoding: units, as: UTF16.self)
    }

    /// `key` as codes. Units outside the alphabet become `unknownCode`, which
    /// matches nothing.
    func encode(_ key: String) -> [UInt8] {
        key.utf16.map { codes[$0] ?? unknownCode }
    }

    /// The rank of a lowercased word, or nil when it is not listed.
    func rank(of key: String) -> Int? {
        let word = encode(key)
        guard !word.contains(unknownCode) else { return nil }
        let index = lowerBound(of: word)
        guard index < sorted.count else { return nil }
        let rank = Int(sorted[index])
        return comparePrefix(rank: rank, with: word) == 0 && Int(lengths[rank]) == word.count ? rank : nil
    }

    /// Ranks of the words that start with `key` and are longer than it, most
    /// frequent first, among those `accept` lets through.
    func completionRanks(of key: String, limit: Int, accept: (Int) -> Bool = { _ in true }) -> [Int] {
        let prefix = encode(key)
        guard limit > 0, !prefix.isEmpty, !prefix.contains(unknownCode) else { return [] }
        var best: [Int] = []
        var index = lowerBound(of: prefix)
        while index < sorted.count {
            let rank = Int(sorted[index])
            guard comparePrefix(rank: rank, with: prefix) == 0 else { break }
            index += 1
            guard Int(lengths[rank]) > prefix.count else { continue }
            if best.count == limit {
                guard rank < best[limit - 1] else { continue }
            }
            guard accept(rank) else { continue }
            if best.count == limit {
                best.removeLast()
            }
            best.insert(rank, at: best.firstIndex { $0 > rank } ?? best.count)
        }
        return best
    }

    /// Calls `body` with the ranks of every word `length` units long, in rank
    /// order, and the words' codes packed back to back. Nil when no word has
    /// that length.
    @discardableResult
    func withWords<Result>(
        length: Int,
        _ body: (_ ranks: UnsafeBufferPointer<Int32>, _ codes: UnsafeBufferPointer<UInt8>) throws -> Result
    ) rethrows -> Result? {
        guard length > 0, length < buckets.count, let bucket = buckets[length] else { return nil }
        return try bucket.ranks.withUnsafeBufferPointer { ranks in
            try storage.withUnsafeBufferPointer { all in
                let codes = UnsafeBufferPointer(rebasing: all[bucket.start..<bucket.start + ranks.count * length])
                return try body(ranks, codes)
            }
        }
    }

    // MARK: Prefix index

    /// The first position in `sorted` whose word is not below `prefix`.
    private func lowerBound(of prefix: [UInt8]) -> Int {
        var low = 0
        var high = sorted.count
        while low < high {
            let middle = (low + high) / 2
            if comparePrefix(rank: Int(sorted[middle]), with: prefix) < 0 {
                low = middle + 1
            } else {
                high = middle
            }
        }
        return low
    }

    /// Compares the word at `rank`, cut to `prefix.count` units, with `prefix`.
    /// A word shorter than the prefix that matches it as far as it goes sorts
    /// before it.
    private func comparePrefix(rank: Int, with prefix: [UInt8]) -> Int {
        let start = Int(offsets[rank])
        let length = Int(lengths[rank])
        for index in 0..<min(length, prefix.count) {
            let code = storage[start + index]
            if code != prefix[index] { return code < prefix[index] ? -1 : 1 }
        }
        return length < prefix.count ? -1 : 0
    }

    private static func lexicographicOrder(count: Int, storage: [UInt8], offsets: [UInt32], lengths: [UInt8]) -> [Int32] {
        storage.withUnsafeBufferPointer { storage in
            (0..<Int32(count)).sorted { left, right in
                let leftStart = Int(offsets[Int(left)])
                let rightStart = Int(offsets[Int(right)])
                let leftLength = Int(lengths[Int(left)])
                let rightLength = Int(lengths[Int(right)])
                for index in 0..<min(leftLength, rightLength) {
                    let a = storage[leftStart + index]
                    let b = storage[rightStart + index]
                    if a != b { return a < b }
                }
                return leftLength < rightLength
            }
        }
    }
}
