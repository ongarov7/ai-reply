import Foundation

/// "Is this a real word?" for one language: a Bloom filter of every word form
/// the corpus saw at least twice, read from `<lang>.known`.
///
/// A Bloom filter never says no to a word it holds, and says yes to a word it
/// does not hold about 0.4% of the time. Here a false yes only means one typo
/// is left alone - the safe direction for an autocorrect.
///
/// Format and hashing are fixed by `tools/dictionaries/build_dictionaries.py`
/// and shared with the Android keyboard (little-endian):
///
///     "AIRB", u8 version = 1, u8 k, u16 0, u32 n, u64 m, then m / 8 bytes
///     key   = UTF-8 of the lowercased word
///     h1    = FNV-1a 64, h2 = splitmix64(h1) | 1
///     bit_i = (h1 + i * h2 mod 2^64) mod m, i in 0 ..< k
struct KnownWordsFilter: Sendable {

    /// The two hashes every bit position is derived from. Computed once per
    /// word and reused across the filters of every language a layout consults.
    struct Hashes: Equatable, Sendable {
        let first: UInt64
        let second: UInt64

        init(key: String) {
            var hash: UInt64 = 0xcbf2_9ce4_8422_2325
            for byte in key.utf8 {
                hash ^= UInt64(byte)
                hash = hash &* 0x0000_0100_0000_01b3
            }
            first = hash
            second = KnownWordsFilter.splitMix64(hash) | 1
        }
    }

    static let headerSize = 20
    private static let magic: [UInt8] = Array("AIRB".utf8)

    /// k, the number of bits set per word.
    let hashCount: Int
    /// m, the size of the bit array.
    let bitCount: UInt64
    /// n, how many word forms were inserted.
    let wordCount: Int
    /// The whole file; the bit array starts at `headerSize`. Usually memory
    /// mapped, so only the pages a lookup touches are ever read.
    private let data: Data

    init(data: Data) throws {
        let bytes = [UInt8](data.prefix(Self.headerSize))
        guard bytes.count == Self.headerSize,
              Array(bytes[0..<4]) == Self.magic,
              bytes[4] == 1 else {
            throw AutocorrectDataError.malformed("known-words header")
        }
        func little(_ range: Range<Int>) -> UInt64 {
            range.reversed().reduce(0) { $0 << 8 | UInt64(bytes[$1]) }
        }
        let hashCount = Int(bytes[5])
        let bitCount = little(12..<20)
        guard hashCount > 0,
              bitCount > 0,
              bitCount % 8 == 0,
              UInt64(data.count - Self.headerSize) == bitCount / 8 else {
            throw AutocorrectDataError.malformed("known-words size")
        }
        self.hashCount = hashCount
        self.bitCount = bitCount
        self.wordCount = Int(little(8..<12))
        self.data = data
    }

    init(contentsOf url: URL) throws {
        try self.init(data: Data(contentsOf: url, options: .alwaysMapped))
    }

    /// Whether `key` (already lowercased, see `WordList.key(for:)`) is a word.
    func contains(_ key: String) -> Bool {
        contains(Hashes(key: key))
    }

    func contains(_ hashes: Hashes) -> Bool {
        let base = data.startIndex + Self.headerSize
        for bit in Self.bitIndices(for: hashes, hashCount: hashCount, bitCount: bitCount) {
            let byte = data[base + Int(bit >> 3)]
            if byte & (1 << UInt8(bit & 7)) == 0 { return false }
        }
        return true
    }

    /// The bit positions a word sets, in hash order.
    static func bitIndices(for hashes: Hashes, hashCount: Int, bitCount: UInt64) -> [UInt64] {
        (0..<UInt64(hashCount)).map { index in
            (hashes.first &+ index &* hashes.second) % bitCount
        }
    }

    static func splitMix64(_ value: UInt64) -> UInt64 {
        var z = value &+ 0x9e37_79b9_7f4a_7c15
        z = (z ^ (z >> 30)) &* 0xbf58_476d_1ce4_e5b9
        z = (z ^ (z >> 27)) &* 0x94d0_49bb_1331_11eb
        return z ^ (z >> 31)
    }
}

/// A dictionary file that does not have the shape the generator writes.
enum AutocorrectDataError: Error, Equatable {
    case malformed(String)
}
