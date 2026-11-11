import XCTest
import AVFoundation
@testable import AIReply

final class KeyboardTutorialTests: XCTestCase {
    func testEachAppLanguageSelectsItsOwnRecording() throws {
        for language in AppLanguage.allCases {
            let resource = try XCTUnwrap(KeyboardTutorialResource.resolve(for: language) {
                URL(fileURLWithPath: "/tutorial-tests/\($0).mp4")
            })
            XCTAssertEqual(resource.language, language)
            XCTAssertEqual(resource.url.lastPathComponent, "keyboard-setup-\(language.rawValue).mp4")
        }
    }

    func testMissingRecordingHasAnExplicitEnglishFallbackOrNoPlayer() throws {
        let english = URL(fileURLWithPath: "/tutorial-tests/keyboard-setup-en.mp4")
        for language in AppLanguage.allCases where language != .english {
            let resource = try XCTUnwrap(KeyboardTutorialResource.resolve(for: language) {
                $0 == "keyboard-setup-en" ? english : nil
            })
            XCTAssertEqual(resource.language, .english)
            XCTAssertEqual(resource.url, english)
        }
        XCTAssertNil(KeyboardTutorialResource.resolve(for: .russian, lookup: { _ in nil }))
    }

    func testPlaybackProgressIsSeparateFromPasteGuideAndOtherLanguages() throws {
        let suite = "KeyboardTutorialPlaybackTests.\(UUID())"
        let defaults = try XCTUnwrap(UserDefaults(suiteName: suite))
        defer { defaults.removePersistentDomain(forName: suite) }
        PasteTutorialPlayback.savePosition(12, for: .russian, defaults: defaults)
        for (index, language) in AppLanguage.allCases.enumerated() {
            XCTAssertEqual(KeyboardTutorialPlayback.savedPosition(for: language, defaults: defaults), 0)
            KeyboardTutorialPlayback.savePosition(Double(index + 5), for: language, defaults: defaults)
        }
        for (index, language) in AppLanguage.allCases.enumerated() {
            XCTAssertEqual(KeyboardTutorialPlayback.savedPosition(for: language, defaults: defaults), Double(index + 5))
        }
        KeyboardTutorialPlayback.savePosition(0, for: .russian, defaults: defaults)
        XCTAssertEqual(PasteTutorialPlayback.savedPosition(for: .russian, defaults: defaults), 12)
        XCTAssertEqual(KeyboardTutorialPlayback.savedPosition(for: .kazakh, defaults: defaults), 7)
        for value in [Double.nan, .infinity, -1] {
            KeyboardTutorialPlayback.savePosition(value, for: .english, defaults: defaults)
            XCTAssertEqual(KeyboardTutorialPlayback.savedPosition(for: .english, defaults: defaults), 0)
        }
    }

    @MainActor
    func testAllKeyboardGuideVideosAreBundledPortraitSilentH264() async throws {
        for language in AppLanguage.allCases {
            let resource = try XCTUnwrap(KeyboardTutorialResource.resolve(for: language), language.rawValue)
            XCTAssertEqual(resource.language, language, "a production locale must have its own captioned video")
            XCTAssertTrue(resource.url.isFileURL)
            let asset = AVURLAsset(url: resource.url)
            let duration = try await asset.load(.duration).seconds
            XCTAssertGreaterThanOrEqual(duration, 20, language.rawValue)
            XCTAssertLessThanOrEqual(duration, 90, language.rawValue)
            let audio = try await asset.loadTracks(withMediaType: .audio)
            XCTAssertTrue(audio.isEmpty, language.rawValue)
            let tracks = try await asset.loadTracks(withMediaType: .video)
            let track = try XCTUnwrap(tracks.first, language.rawValue)
            let size = try await track.load(.naturalSize)
            XCTAssertGreaterThan(size.height, size.width, language.rawValue)
            let formats = try await track.load(.formatDescriptions)
            XCTAssertEqual(formats.map { CMFormatDescriptionGetMediaSubType($0) }, [kCMVideoCodecType_H264], language.rawValue)
        }
    }

    func testWrittenGuideIsTranslatedInEveryAppLanguage() throws {
        let requiredKeys = ["title", "nav", "body", "silent", "player", "connect", "toolbar", "reply",
                            "paste", "paste.body", "practice", "practice.body", "permissions", "secureFields"]
            + ["enable", "access", "switch", "copy", "generate", "insert"].flatMap { ["\($0).title", "\($0).body"] }
        var english: [String: String] = [:]
        for language in AppLanguage.allCases {
            let path = try XCTUnwrap(Bundle.main.path(forResource: "Localizable", ofType: "strings",
                                                      inDirectory: "\(language.rawValue).lproj"))
            let table = try XCTUnwrap(NSDictionary(contentsOfFile: path) as? [String: String])
            for name in requiredKeys {
                let key = "tutorial.keyboard.\(name)"
                let value = try XCTUnwrap(table[key], "\(key) in \(language.rawValue)")
                XCTAssertFalse(value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                if language == .english {
                    english[key] = value
                } else {
                    XCTAssertNotEqual(value, english[key], "\(key) is untranslated in \(language.rawValue)")
                }
            }
        }
    }
}
