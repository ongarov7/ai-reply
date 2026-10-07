import XCTest
@testable import AIReply

/// The Bloom filter reader must hash exactly like the generator
/// (`tools/dictionaries/build_dictionaries.py`) and the Android keyboard, or
/// every real word reads as a typo.
final class KnownWordsFilterTests: XCTestCase {

    // MARK: Hashing

    func testHashVectorsMatchTheGenerator() {
        let hello = KnownWordsFilter.Hashes(key: "hello")
        XCTAssertEqual(hello.first, 0xa430_d846_80aa_bd0b)
        XCTAssertEqual(hello.second, 0xf3e8_eec5_eb46_e501)
        XCTAssertEqual(
            KnownWordsFilter.bitIndices(for: hello, hashCount: 8, bitCount: 64_000_192),
            [16_701_707, 31_912_268, 47_122_829, 62_333_390, 13_543_759, 28_754_320, 43_964_881, 59_175_442]
        )

        let privet = KnownWordsFilter.Hashes(key: "привет")
        XCTAssertEqual(privet.first, 0x1bd8_a912_173e_871f)
        XCTAssertEqual(privet.second, 0x94b9_fd45_94fb_dd15)
        XCTAssertEqual(
            KnownWordsFilter.bitIndices(for: privet, hashCount: 8, bitCount: 64_000_192),
            [53_506_719, 35_078_196, 5_298_953, 50_870_622, 21_091_379, 55_312_328, 36_883_805, 7_104_562]
        )

        let empty = KnownWordsFilter.Hashes(key: "")
        XCTAssertEqual(empty.first, 0xcbf2_9ce4_8422_2325, "FNV-1a offset basis")
        XCTAssertEqual(empty.second, 0xc381_7c01_6ba4_ff31)
    }

    // MARK: Format

    func testReadsAHandBuiltFilter() throws {
        let filter = try KnownWordsFilter(data: Self.filterData(words: ["сәлем", "hello"], hashCount: 3, bitCount: 256))
        XCTAssertEqual(filter.hashCount, 3)
        XCTAssertEqual(filter.bitCount, 256)
        XCTAssertEqual(filter.wordCount, 2)
        XCTAssertTrue(filter.contains("сәлем"))
        XCTAssertTrue(filter.contains("hello"))
    }

    func testRejectsDamagedFiles() {
        let good = Self.filterData(words: ["a"], hashCount: 2, bitCount: 64)
        var badMagic = good
        badMagic[0] = UInt8(ascii: "X")
        var badVersion = good
        badVersion[4] = 2
        for data in [Data(), good.prefix(19), badMagic, badVersion, good.dropLast()] {
            XCTAssertThrowsError(try KnownWordsFilter(data: Data(data)))
        }
    }

    // MARK: Real files

    /// The generator sizes every filter for about 0.4% false positives with
    /// eight hashes: about 11.5 bits per word, rounded up to whole 64-bit
    /// words. Exact counts change with every dictionary build, so only the
    /// shape is pinned here.
    func testRealFileHeaders() throws {
        for language in KeyboardLanguage.allCases {
            let filter = try Self.realFilter(language)
            XCTAssertEqual(filter.hashCount, 8, language.rawValue)
            XCTAssertGreaterThan(filter.wordCount, 100_000, language.rawValue)
            XCTAssertEqual(filter.bitCount % 64, 0, language.rawValue)
            let bitsPerWord = Double(filter.bitCount) / Double(filter.wordCount)
            XCTAssertEqual(bitsPerWord, 11.5, accuracy: 0.1, language.rawValue)
        }
    }

    func testRealFileMembership() throws {
        let russian = try Self.realFilter(.russian)
        let kazakh = try Self.realFilter(.kazakh)
        let english = try Self.realFilter(.english)

        XCTAssertTrue(russian.contains("привет"))
        XCTAssertTrue(russian.contains("созвониться"))
        XCTAssertTrue(kazakh.contains("қалайсың"))
        XCTAssertTrue(kazakh.contains("калайсын"), "Kazakh typed without Kazakh letters is known")
        XCTAssertTrue(english.contains("tomorrow"))

        XCTAssertFalse(russian.contains("превт"))
        XCTAssertFalse(russian.contains("zzqx"))
        XCTAssertFalse(english.contains("zzqx"))
    }

    // MARK: Helpers

    static func realFilter(_ language: KeyboardLanguage) throws -> KnownWordsFilter {
        try KnownWordsFilter(contentsOf: AutocorrectFixtures.dictionaries.appendingPathComponent("\(language.rawValue).known"))
    }

    /// Writes a filter the way the generator does.
    static func filterData(words: [String], hashCount: Int, bitCount: UInt64) -> Data {
        var bits = [UInt8](repeating: 0, count: Int(bitCount / 8))
        for word in words {
            let hashes = KnownWordsFilter.Hashes(key: word)
            for bit in KnownWordsFilter.bitIndices(for: hashes, hashCount: hashCount, bitCount: bitCount) {
                bits[Int(bit >> 3)] |= 1 << UInt8(bit & 7)
            }
        }
        var data = Data("AIRB".utf8)
        data.append(contentsOf: [1, UInt8(hashCount), 0, 0])
        withUnsafeBytes(of: UInt32(words.count).littleEndian) { data.append(contentsOf: $0) }
        withUnsafeBytes(of: bitCount.littleEndian) { data.append(contentsOf: $0) }
        data.append(contentsOf: bits)
        return data
    }
}
