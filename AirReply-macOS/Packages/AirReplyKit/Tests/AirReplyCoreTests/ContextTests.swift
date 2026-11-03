import Foundation
import Testing
import CoreGraphics
import CoreText
@testable import AirReplyCore

@Suite("Private local context")
@MainActor
struct ContextProfileTests {
    @Test func profilesAndCachesPersistEncryptedAndCanBeRemoved() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let keychain = InMemorySecureStore()
        let store = ContextProfileStore(store: keychain, directory: directory)
        let privateText = "Confidential Product Alpha price is 123456 KZT"
        let attachment = ContextAttachment(name: "price.csv", format: "csv", segments: [.init(text: privateText)])
        var business = ContextProfile(name: "Sales", role: .business, company: "Private Example Company", attachments: [attachment])
        try store.save(business)
        let file = directory.appendingPathComponent("profiles-v1.aicontext")
        let encrypted = try Data(contentsOf: file)
        #expect(!encrypted.contains(Data(privateText.utf8)))
        #expect(!encrypted.contains(Data(business.company.utf8)))
        let restored = ContextProfileStore(store: keychain, directory: directory)
        try restored.load()
        #expect(restored.profiles == [business])
        try restored.removeAttachment(profileID: business.id, attachmentID: attachment.id)
        #expect(restored.profiles.first?.attachments.isEmpty == true)
        business.attachments = []
        let again = ContextProfileStore(store: keychain, directory: directory)
        try again.load()
        #expect(again.profiles == [business])
        try again.remove(business.id)
        #expect(again.profiles.isEmpty)
        try again.removeAll()
        #expect(!FileManager.default.fileExists(atPath: file.path))
    }

    @Test func asyncConcurrentWritesKeepTheNewestSnapshot() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let keychain = InMemorySecureStore()
        let store = ContextProfileStore(store: keychain, directory: directory)
        async let first: Void = store.saveAsync(ContextProfile(name: "First"))
        async let second: Void = store.saveAsync(ContextProfile(name: "Second"))
        try await first
        try await second
        let restored = ContextProfileStore(store: keychain, directory: directory)
        try await restored.loadAsync()
        #expect(Set(restored.profiles.map(\.name)) == ["First", "Second"])
        try await restored.removeAllAsync()
        #expect(restored.profiles.isEmpty)
    }

    @Test func tamperedCacheFailsAuthentication() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let keychain = InMemorySecureStore()
        try ContextProfileStore(store: keychain, directory: directory).save(ContextProfile(name: "Private"))
        let file = directory.appendingPathComponent("profiles-v1.aicontext")
        var bytes = try Data(contentsOf: file)
        bytes[bytes.count - 1] ^= 1
        try bytes.write(to: file)
        let other = ContextProfileStore(store: keychain, directory: directory)
        #expect(throws: (any Error).self) { try other.load() }
        #expect(other.profiles.isEmpty)
    }
}

@Suite("Bounded local retrieval")
struct ContextRetrievalTests {
    private var priceList: ContextAttachment {
        ContextAttachment(name: "prices.xlsx", format: "xlsx", segments: [
            .init(text: "Product: Product A; Price: 24990 KZT; Stock: 3", section: "Retail row 2"),
            .init(text: "Product: Product B; Price: 100 KZT; Stock: 9", section: "Retail row 3"),
            .init(text: "Delivery: Almaty courier costs 2000 KZT", section: "Policies row 1")])
    }

    @Test func exactProductLookupKeepsRowAndHeadersOnly() throws {
        let profile = ContextProfile(name: "Sales", attachments: [priceList])
        let result = try #require(ContextRetriever.context(profile: profile, query: "Hello, what is the price of Product A?"))
        #expect(result.references.count == 1)
        #expect(result.references[0].contains("Price: 24990 KZT"))
        #expect(result.references[0].contains("Retail row 2"))
        #expect(!result.references.joined().contains("100 KZT"))
        #expect(ContextRetriever.context(profile: profile, query: "Price of Product C?") == nil)
    }

    @Test func unrelatedProfilesAreNotAppliedAndUnrelatedParagraphsAreOmitted() throws {
        #expect(ContextRetriever.context(profile: nil, query: "price") == nil)
        let profile = ContextProfile(name: "Delivery", company: "My Company", attachments: [priceList])
        let result = try #require(ContextRetriever.context(profile: profile, query: "Delivery in Almaty?"))
        #expect(result.profile?.contains("My Company") == true)
        #expect(result.references.count == 1)
        #expect(result.references[0].contains("2000 KZT"))
    }

    @Test func unicodeAndTokenBoundRemainConservative() throws {
        let profile = ContextProfile(name: "Бизнес", description: String(repeating: "Қазақша сипаттама ", count: 200),
                                     attachments: [ContextAttachment(name: "terms.txt", format: "txt", segments:
                                        (0..<30).map { _ in .init(text: String(repeating: "Delivery details ", count: 40)) })])
        let result = try #require(ContextRetriever.context(profile: profile, query: "Delivery"))
        #expect((result.profile?.unicodeScalars.count ?? 0) <= 1500)
        #expect((result.profile?.utf8.count ?? 0) + result.references.reduce(0, { $0 + $1.utf8.count }) <= 3072)
        #expect(result.references.count <= 8)
        #expect(!result.references.contains { $0.hasSuffix("Deliv") })
    }
}

@Suite("Local document parsing")
struct DocumentContextTests {
    private let limits = DocumentImportLimits()
    private func parse(_ text: String, format: String) throws -> ContextAttachment {
        try DocumentContextImporter.parse(data: Data(text.utf8), name: "reference." + format, format: format, limits: limits)
    }

    @Test func textAndCsvSupportUnicodeQuotesAndMultilineCells() throws {
        let text = try parse("Доставка по Алматы.\n\nСәлем! Company rules.", format: "txt")
        #expect(text.segments.count == 2)
        let csv = try parse("Product,Price,Description\r\n\"Product A\",24990,\"First line\nSecond \"\"quoted\"\" line\"\r\n", format: "csv")
        #expect(csv.segments.count == 1)
        #expect(csv.segments[0].text.contains("Price: 24990"))
        #expect(csv.segments[0].text.contains("Second \"quoted\" line"))
        #expect(throws: DocumentImportError.malformed) { try parse("Name,Price\n\"unfinished,10", format: "csv") }
    }

    @Test func docxParagraphsAreLocallyExtracted() throws {
        let xml = #"<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Delivery in Almaty costs 2000 KZT.</w:t></w:r></w:p><w:p><w:r><w:t>Return policy: 14 days.</w:t></w:r></w:p></w:body></w:document>"#
        let data = fixtureZip([("word/document.xml", Data(xml.utf8))])
        let document = try DocumentContextImporter.parse(data: data, name: "policy.docx", format: "docx", limits: limits)
        #expect(document.segments.count == 2)
        #expect(document.segments[0].text.contains("2000 KZT"))
    }

    @Test func realDeflatedOfficeArchiveUsesBoundedSystemZlib() throws {
        let base64 = "UEsDBBQAAAAIAJeSY1176GqCUwAAAG8AAAARAAAAd29yZC9kb2N1bWVudC54bWyzKbdKyU8uzU3NK1GoyM3JK7Yqt1UqV7KzKbdKyk+pBNEFIKIIRJTYuaTmZJalFlUqJOcXlxQrGBkYGCh4R4Xo2eiDZEFkEZgsAJMQE/QRVtgBAFBLAQIUAxQAAAAIAJeSY1176GqCUwAAAG8AAAARAAAAAAAAAAAAAACAAQAAAAB3b3JkL2RvY3VtZW50LnhtbFBLBQYAAAAAAQABAD8AAACCAAAAAAA="
        let data = try #require(Data(base64Encoded: base64))
        let document = try DocumentContextImporter.parse(data: data, name: "terms.docx", format: "docx", limits: limits)
        #expect(document.segments.first?.text == "Delivery costs 2000 KZT.")
        var damaged = data
        damaged[50] ^= 0x80
        #expect(throws: (any Error).self) { try DocumentContextImporter.parse(data: damaged, name: "terms.docx", format: "docx", limits: limits) }
    }

    @Test func xlsxDetectsSheetsSharedStringsHeadersSparseCellsAndOmitsFormulas() throws {
        let data = fixtureZip([
            ("xl/workbook.xml", Data(#"<workbook xmlns:r="r"><sheets><sheet name="Retail" r:id="rId1"/><sheet name="Wholesale" r:id="rId2"/></sheets></workbook>"#.utf8)),
            ("xl/_rels/workbook.xml.rels", Data(#"<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Target="worksheets/sheet2.xml"/></Relationships>"#.utf8)),
            ("xl/sharedStrings.xml", Data(#"<sst><si><t>Product</t></si><si><r><t>Pri</t></r><r><t>ce</t></r></si><si><t>Stock</t></si><si><t>Product A</t></si></sst>"#.utf8)),
            ("xl/worksheets/sheet1.xml", Data(#"<worksheet><sheetData><row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c><c r="C1" t="s"><v>2</v></c></row><row r="2"><c r="A2" t="s"><v>3</v></c><c r="B2"><v>24990</v></c><c r="C2"><f>SUM(1,2)</f><v>3</v></c></row></sheetData></worksheet>"#.utf8)),
            ("xl/worksheets/sheet2.xml", Data(#"<worksheet><sheetData><row><c r="A1" t="inlineStr"><is><t>Product</t></is></c><c r="C1" t="inlineStr"><is><t>Price</t></is></c></row><row><c r="A2" t="inlineStr"><is><t>Product B</t></is></c><c r="C2"><v>19990</v></c></row></sheetData></worksheet>"#.utf8))])
        let document = try DocumentContextImporter.parse(data: data, name: "prices.xlsx", format: "xlsx", limits: limits)
        #expect(document.segments.count == 2)
        #expect(document.segments[0].section?.contains("Retail") == true)
        #expect(document.segments[0].text == "Product: Product A; Price: 24990")
        #expect(document.segments[1].section?.contains("Wholesale") == true)
        #expect(document.segments[1].text.contains("Price: 19990"))
    }

    @Test func zipBombTraversalEncryptionAndEntityDeclarationsAreRejected() throws {
        let content = Data("safe".utf8)
        #expect(throws: DocumentImportError.unsafeArchive) { try DocumentArchive(data: fixtureZip([("../escape.xml", content)]), limits: limits) }
        var encrypted = fixtureZip([("word/document.xml", content)])
        encrypted[6] = 1
        // The central directory flags are authoritative and must agree locally.
        let central = 30 + "word/document.xml".utf8.count + content.count
        encrypted[central + 8] = 1
        #expect(throws: DocumentImportError.unsafeArchive) { try DocumentArchive(data: encrypted, limits: limits) }
        var bomb = fixtureZip([("word/document.xml", content)])
        replace32(&bomb, at: central + 24, value: UInt32(limits.maximumExpandedBytes + 1))
        #expect(throws: DocumentImportError.tooLarge) { try DocumentArchive(data: bomb, limits: limits) }
        let entity = fixtureZip([("word/document.xml", Data(#"<!DOCTYPE x [<!ENTITY secret SYSTEM "file:///etc/passwd">]><x>&secret;</x>"#.utf8))])
        #expect(throws: DocumentImportError.unsafeArchive) { try DocumentContextImporter.parse(data: entity, name: "x.docx", format: "docx", limits: limits) }
        var badCRC = fixtureZip([("word/document.xml", content)])
        badCRC[30 + "word/document.xml".utf8.count] ^= 1
        #expect(throws: DocumentImportError.malformed) { try DocumentArchive(data: badCRC, limits: limits).data("word/document.xml") }
    }

    @Test func sizeAndFormatValidationAndActualSelectedFile() async throws {
        let limits = DocumentImportLimits(maximumFileBytes: 16)
        #expect(throws: DocumentImportError.tooLarge) { try DocumentContextImporter.parse(data: Data(repeating: 65, count: 17), name: "large.txt", format: "txt", limits: limits) }
        #expect(throws: DocumentImportError.unsupportedFormat) { try DocumentContextImporter.parse(data: Data(), name: "macro.xlsm", format: "xlsm", limits: limits) }
        let file = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".txt")
        try Data("Delivery: 2000 KZT".utf8).write(to: file)
        defer { try? FileManager.default.removeItem(at: file) }
        let imported = try await DocumentContextImporter().parse(url: file)
        #expect(imported.segments.first?.text == "Delivery: 2000 KZT")
    }

    @Test func pdfReadableTextIsExtractedWithoutNetwork() throws {
        let data = NSMutableData()
        let consumer = try #require(CGDataConsumer(data: data))
        var rectangle = CGRect(x: 0, y: 0, width: 600, height: 800)
        let context = try #require(CGContext(consumer: consumer, mediaBox: &rectangle, nil))
        context.beginPDFPage(nil)
        context.textPosition = CGPoint(x: 40, y: 700)
        let attributed = NSAttributedString(string: "Delivery costs 2000 KZT", attributes: [
            NSAttributedString.Key(kCTFontAttributeName as String): CTFontCreateWithName("Helvetica" as CFString, 14, nil)])
        CTLineDraw(CTLineCreateWithAttributedString(attributed), context)
        context.endPDFPage(); context.closePDF()
        let document = try DocumentContextImporter.parse(data: data as Data, name: "terms.pdf", format: "pdf", limits: limits)
        #expect(document.segments.map(\.text).joined().contains("2000 KZT"))
    }
}

// Small, deterministic STORED ZIP fixture builder. Production handles method 8
// with system zlib; these fixtures exercise bounds and Office parsing directly.
private func fixtureZip(_ files: [(String, Data)]) -> Data {
    var bytes = Data(), directory = Data()
    for (name, body) in files {
        let nameData = Data(name.utf8), offset = UInt32(bytes.count), crc = fixtureCRC(body)
        append32(&bytes, 0x04034b50); append16(&bytes, 20); append16(&bytes, 0); append16(&bytes, 0)
        append16(&bytes, 0); append16(&bytes, 0); append32(&bytes, crc)
        append32(&bytes, UInt32(body.count)); append32(&bytes, UInt32(body.count)); append16(&bytes, UInt16(nameData.count)); append16(&bytes, 0)
        bytes.append(nameData); bytes.append(body)
        append32(&directory, 0x02014b50); append16(&directory, 20); append16(&directory, 20); append16(&directory, 0); append16(&directory, 0)
        append16(&directory, 0); append16(&directory, 0); append32(&directory, crc)
        append32(&directory, UInt32(body.count)); append32(&directory, UInt32(body.count)); append16(&directory, UInt16(nameData.count))
        for _ in 0..<4 { append16(&directory, 0) }
        append32(&directory, 0); append32(&directory, offset); directory.append(nameData)
    }
    let offset = UInt32(bytes.count)
    bytes.append(directory)
    append32(&bytes, 0x06054b50); append16(&bytes, 0); append16(&bytes, 0)
    append16(&bytes, UInt16(files.count)); append16(&bytes, UInt16(files.count))
    append32(&bytes, UInt32(directory.count)); append32(&bytes, offset); append16(&bytes, 0)
    return bytes
}
private func append16(_ data: inout Data, _ value: UInt16) { data.append(UInt8(value & 255)); data.append(UInt8(value >> 8)) }
private func append32(_ data: inout Data, _ value: UInt32) { append16(&data, UInt16(value & 65535)); append16(&data, UInt16(value >> 16)) }
private func replace32(_ data: inout Data, at: Int, value: UInt32) { var bytes = Data(); append32(&bytes, value); data.replaceSubrange(at..<(at + 4), with: bytes) }
private func fixtureCRC(_ data: Data) -> UInt32 {
    var crc: UInt32 = 0xffff_ffff
    for byte in data {
        crc ^= UInt32(byte)
        for _ in 0..<8 { crc = (crc >> 1) ^ (crc & 1 == 1 ? 0xedb8_8320 : 0) }
    }
    return crc ^ 0xffff_ffff
}

@Suite("Context generation wire contract")
struct ContextGenerationTests {
    private func generator(_ transport: FakeTransport) throws -> ServerGenerator {
        let sessions = SessionStore(store: InMemorySecureStore())
        try sessions.save(Fixtures.tokens(expiresAt: .distantFuture))
        return ServerGenerator(client: APIClient(configuration: Fixtures.configuration, transport: transport, sessions: sessions), configuration: Fixtures.configuration)
    }

    @Test func optionsSendOnlySelectedEvidenceAndPreserveOneIdempotencyKey() async throws {
        let transport = FakeTransport { _ in (200, #"{"text":"Product A costs 24990 KZT."}"#, [:]) }
        let options = GenerationOptions(context: DesktopGenerationContext(profile: "Company: Example", references: ["Product: A; Price: 24990 KZT"]), outputCharacterLimit: 600, recipient: .client)
        let result = try await generator(transport).compose("Reply to the customer", regenerate: false, inputLanguage: "en", idempotencyKey: "context-once-123", options: options)
        #expect(result.text.contains("24990"))
        #expect(transport.requests.count == 1)
        let request = try #require(transport.requests.first)
        let body = Fixtures.bodyJSON(request)
        #expect(body["output_char_limit"] as? Int == 600)
        #expect(body["template_id"] as? String == "client")
        #expect(request.value(forHTTPHeaderField: "Idempotency-Key") == "context-once-123")
        let context = try #require(body["context"] as? [String: Any])
        #expect(Set(context.keys) == ["profile", "references"])
        #expect(context["references"] as? [String] == options.context?.references)
        #expect(!body.keys.contains("files"))
        #expect(!body.keys.contains("attachments"))
    }

    @Test func oversizedContextNeverLeavesTheMac() async throws {
        let transport = FakeTransport { _ in (200, #"{"text":"unused"}"#, [:]) }
        let options = GenerationOptions(context: DesktopGenerationContext(references: [String(repeating: "private", count: 1000)]), outputCharacterLimit: 400)
        await #expect(throws: GenerationError.server) {
            try await generator(transport).compose("Reply", regenerate: false, inputLanguage: nil, idempotencyKey: "context-once-456", options: options)
        }
        #expect(transport.requests.isEmpty)
    }

    @Test(arguments: [("PRO_DEVICE_LIMIT_REACHED", GenerationError.proDeviceLimit), ("PRO_DEVICE_IDENTITY_REQUIRED", GenerationError.proDeviceIdentity), ("PRO_DEVICE_REVOKED", GenerationError.proDeviceRevoked)])
    func dedicatedProWarningsDoNotRefreshOrDiscardTheAccount(code: String, expected: GenerationError) async throws {
        let transport = FakeTransport { _ in (403, Fixtures.error(code), [:]) }
        await #expect(throws: expected) {
            try await generator(transport).compose("Reply", regenerate: false, inputLanguage: nil, idempotencyKey: "context-once-789")
        }
        #expect(transport.requests.count == 1)
        #expect(transport.requests(to: "/auth/refresh").isEmpty)
    }
}
