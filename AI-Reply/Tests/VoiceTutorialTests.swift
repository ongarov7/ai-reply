import XCTest
import AVFoundation
@testable import AIReply

final class VoiceTutorialTests: XCTestCase {
    func testMissingLocalizedFootageUsesTheRussianRecordingAndIdentifiesItsLanguage() throws {
        let russian = URL(fileURLWithPath: "/tutorial-tests/voice-setup-tutorial-ru.mp4")
        for language in AppLanguage.allCases {
            let resource = try XCTUnwrap(VoiceTutorialResource.resolve(for: language) {
                $0 == "voice-setup-tutorial-ru" ? russian : nil
            })
            XCTAssertEqual(resource.language, .russian)
            XCTAssertEqual(resource.url, russian)
        }
        XCTAssertNil(VoiceTutorialResource.resolve(for: .kazakh, lookup: { _ in nil }))
        let localized = URL(fileURLWithPath: "/tutorial-tests/voice-setup-tutorial-kk.mp4")
        let resource = try XCTUnwrap(VoiceTutorialResource.resolve(for: .kazakh) {
            $0 == "voice-setup-tutorial-kk" ? localized : russian
        })
        XCTAssertEqual(resource.language, .kazakh)
        XCTAssertEqual(resource.url, localized)
    }

    func testVoicePlaybackDoesNotOverwriteOtherTutorialProgress() throws {
        let suite = "VoiceTutorialPlaybackTests.\(UUID())"
        let defaults = try XCTUnwrap(UserDefaults(suiteName: suite))
        defer { defaults.removePersistentDomain(forName: suite) }
        PasteTutorialPlayback.savePosition(10, for: .russian, defaults: defaults)
        KeyboardTutorialPlayback.savePosition(12, for: .russian, defaults: defaults)
        VoiceTutorialPlayback.savePosition(20, for: .russian, defaults: defaults)
        XCTAssertEqual(VoiceTutorialPlayback.savedPosition(for: .russian, defaults: defaults), 20)
        XCTAssertEqual(PasteTutorialPlayback.savedPosition(for: .russian, defaults: defaults), 10)
        XCTAssertEqual(KeyboardTutorialPlayback.savedPosition(for: .russian, defaults: defaults), 12)
        VoiceTutorialPlayback.savePosition(.nan, for: .russian, defaults: defaults)
        XCTAssertEqual(VoiceTutorialPlayback.savedPosition(for: .russian, defaults: defaults), 0)
    }

    @MainActor
    func testRecordedVoiceGuideIsBundledPortraitSilentH264() async throws {
        let resource = try XCTUnwrap(VoiceTutorialResource.resolve(for: .russian))
        XCTAssertEqual(resource.language, .russian)
        let asset = AVURLAsset(url: resource.url)
        let duration = try await asset.load(.duration).seconds
        XCTAssertGreaterThanOrEqual(duration, 15)
        XCTAssertLessThanOrEqual(duration, 150)
        let audio = try await asset.loadTracks(withMediaType: .audio)
        XCTAssertTrue(audio.isEmpty)
        let tracks = try await asset.loadTracks(withMediaType: .video)
        let track = try XCTUnwrap(tracks.first)
        let size = try await track.load(.naturalSize)
        XCTAssertGreaterThan(size.height, size.width)
        let formats = try await track.load(.formatDescriptions)
        XCTAssertEqual(formats.map { CMFormatDescriptionGetMediaSubType($0) }, [kCMVideoCodecType_H264])
    }

    func testSetupInstructionsAndRussianVideoNoticeAreTranslatedInEveryAppLanguage() throws {
        let names = ["title", "nav", "body", "settings", "denied", "openDictation", "ios17", "privacy", "silent",
                     "player", "russianFallback", "controlCenter.title", "practice.title", "practice.launch",
                     "practice.open", "practice.add", "practice.search", "practice.typeName", "practice.finish"]
            + ["permission", "openControlCenter", "openControlGallery", "searchControl", "control", "finishControl",
               "activity", "try", "practice.recording", "practice.done"]
                .flatMap { ["\($0).title", "\($0).body"] }
        var english: [String: String] = [:]
        for language in AppLanguage.allCases {
            let path = try XCTUnwrap(Bundle.main.path(forResource: "Localizable", ofType: "strings",
                                                      inDirectory: "\(language.rawValue).lproj"))
            let table = try XCTUnwrap(NSDictionary(contentsOfFile: path) as? [String: String])
            for name in names {
                let key = "tutorial.voice.\(name)"
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
