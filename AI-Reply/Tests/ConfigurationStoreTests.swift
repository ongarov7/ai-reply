import XCTest
@testable import AIReply

/// Template model and persistence.
final class ConfigurationStoreTests: XCTestCase {

    // MARK: Normalization

    func testMissingBuiltInsAreRestored() {
        let configuration = ReplyConfiguration(
            profile: .empty,
            templates: [.builtIn(.friend, sortIndex: 0)]
        ).normalized()

        for kind in RelationshipKind.builtIns {
            XCTAssertTrue(
                configuration.templates.contains { $0.id == kind.rawValue },
                "\(kind.rawValue) must be restored"
            )
        }
    }

    /// Hiding every template would leave an empty keyboard bar with no way back,
    /// so the built-ins come back rather than the user getting stuck.
    func testHidingEverythingRestoresTheBuiltIns() {
        var templates = ReplyTemplate.defaults
        for index in templates.indices { templates[index].isVisible = false }
        let configuration = ReplyConfiguration(profile: .empty, templates: templates).normalized()
        XCTAssertFalse(configuration.visibleTemplates.isEmpty)
    }

    func testSortIndicesAreMadeContiguous() {
        var templates = ReplyTemplate.defaults
        templates[0].sortIndex = 40
        templates[1].sortIndex = 40
        let configuration = ReplyConfiguration(profile: .empty, templates: templates).normalized()
        XCTAssertEqual(configuration.templates.map(\.sortIndex), Array(0..<configuration.templates.count))
    }

    // MARK: Naming

    func testBuiltInNamesAreLocalizedPerKeyboardLanguage() {
        let client = ReplyTemplate.builtIn(.client, sortIndex: 0)
        XCTAssertEqual(client.displayName(language: .english), "Client")
        XCTAssertEqual(client.displayName(language: .russian), "Клиент")
        XCTAssertEqual(client.displayName(language: .kazakh), "Клиент")

        let friend = ReplyTemplate.builtIn(.friend, sortIndex: 0)
        XCTAssertEqual(friend.displayName(language: .russian), "Друг")
        XCTAssertEqual(friend.displayName(language: .kazakh), "Дос")

        let work = ReplyTemplate.builtIn(.work, sortIndex: 0)
        XCTAssertEqual(work.displayName(language: .kazakh), "Жұмыс")
    }

    /// A user's own template name is their word, shown verbatim in every UI
    /// language rather than translated.
    func testCustomNamesAreNotTranslated() {
        let supplier = ReplyTemplate.custom(name: "Supplier", sortIndex: 0)
        XCTAssertEqual(supplier.displayName(language: .russian), "Supplier")
        XCTAssertEqual(supplier.displayName(language: .kazakh), "Supplier")
    }

    func testClearingABuiltInNameRestoresTheLocalizedOne() {
        var client = ReplyTemplate.builtIn(.client, sortIndex: 0)
        client.setName("Buyers")
        XCTAssertEqual(client.displayName(language: .english), "Buyers")
        client.setName("   ")
        XCTAssertEqual(client.displayName(language: .english), "Client")
    }

    // MARK: Length caps

    func testInstructionsAreCappedAtTheStoredLimit() {
        var template = ReplyTemplate.custom(name: "Supplier", sortIndex: 0)
        template.setInstructions(String(repeating: "x", count: 5000))
        XCTAssertEqual(template.instructions.count, ReplyTemplate.maximumInstructionCharacters)
    }

    func testProfileDescriptionIsCapped() {
        var profile = UserProfile.empty
        profile.setDescription(String(repeating: "y", count: 4000))
        XCTAssertEqual(profile.descriptionText.count, UserProfile.maximumDescriptionCharacters)
    }

    // MARK: Persistence

    func testRoundTripThroughTheStore() throws {
        let directory = URL(fileURLWithPath: NSTemporaryDirectory())
            .appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }

        let store = ProfileStore(containerURL: directory)
        var configuration = ReplyConfiguration.initial
        configuration.profile.setDescription("I run a logistics company.")
        configuration.profile.preferredTone = .professional
        configuration.profile.workingHours.isEnabled = true
        configuration.templates.append(.custom(name: "Поставщик", sortIndex: 4))

        XCTAssertTrue(store.save(configuration))

        // A fresh store, so this reads the file rather than the cache.
        let reloaded = ProfileStore(containerURL: directory).load()
        XCTAssertEqual(reloaded.profile.descriptionText, "I run a logistics company.")
        XCTAssertEqual(reloaded.profile.preferredTone, .professional)
        XCTAssertTrue(reloaded.profile.workingHours.isEnabled)
        XCTAssertTrue(reloaded.templates.contains { $0.customName == "Поставщик" })
    }

    /// An older build wrote fewer fields. Decoding must fill the gaps rather
    /// than throwing away everything the user typed.
    func testDecodingToleratesAProfileFromAnOlderBuild() throws {
        let json = #"{"descriptionText":"Kept","hasCompletedOnboarding":true}"#
        let profile = try JSONDecoder().decode(UserProfile.self, from: Data(json.utf8))
        XCTAssertEqual(profile.descriptionText, "Kept")
        XCTAssertTrue(profile.hasCompletedOnboarding)
        XCTAssertEqual(profile.completedOnboardingVersion, 1, "the old yes was the first onboarding")
        XCTAssertNil(profile.grammaticalGender, "never asked")
        XCTAssertEqual(profile.preferredTone, .natural)
        XCTAssertEqual(profile.workingHours.days.count, 7)

        let fresh = try JSONDecoder().decode(UserProfile.self, from: Data(#"{"descriptionText":""}"#.utf8))
        XCTAssertEqual(fresh.completedOnboardingVersion, 0)
        XCTAssertFalse(fresh.hasCompletedOnboarding)
    }

    /// The version and the gender survive a round trip, and the old flag is
    /// still written for any build that only knows it.
    func testOnboardingVersionAndGenderRoundTrip() throws {
        var profile = UserProfile.empty
        profile.completedOnboardingVersion = 2
        profile.grammaticalGender = .female
        let data = try JSONEncoder().encode(profile)

        let json = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
        XCTAssertEqual(json["hasCompletedOnboarding"] as? Bool, true)
        XCTAssertEqual(json["completedOnboardingVersion"] as? Int, 2)
        XCTAssertEqual(json["grammaticalGender"] as? String, "female")

        let decoded = try JSONDecoder().decode(UserProfile.self, from: data)
        XCTAssertEqual(decoded, profile)
    }

    /// A gender value from a newer build reads as "never asked" rather than
    /// failing the whole profile.
    func testUnknownGenderDoesNotWipeTheProfile() throws {
        let json = #"{"descriptionText":"Kept","grammaticalGender":"other","completedOnboardingVersion":2}"#
        let profile = try JSONDecoder().decode(UserProfile.self, from: Data(json.utf8))
        XCTAssertEqual(profile.descriptionText, "Kept")
        XCTAssertNil(profile.grammaticalGender)
        XCTAssertEqual(profile.completedOnboardingVersion, 2)
    }

    /// A missing App Group container must degrade, not crash.
    func testStoreWithoutAContainerStillWorksInMemory() {
        let store = ProfileStore(containerURL: nil)
        XCTAssertFalse(store.isPersistent)
        var configuration = ReplyConfiguration.initial
        configuration.profile.setDescription("in memory")
        XCTAssertFalse(store.save(configuration))
        XCTAssertEqual(store.load().profile.descriptionText, "in memory")
    }
}
