import XCTest
@testable import AIReply

/// «Создать ответ»: the recipient is the keyboard's selected persona, both
/// ways, and a message arrives only through the system Paste button.
///
/// «Создать ответ»: таңдалған әңгімелесуші пернетақтамен ортақ.
@MainActor
final class ComposeRecipientTests: XCTestCase {

    private func settings() -> SharedSettings {
        SharedSettings(defaults: UserDefaults(suiteName: "ComposeRecipientTests.\(UUID())")!)
    }

    private let templates = ReplyTemplate.defaults + [ReplyTemplate.custom(name: "Supplier", sortIndex: 4)]

    func testStartsOnTheKeyboardsPersona() {
        let shared = settings()
        shared.setLastTemplateID("work")
        let model = ComposeViewModel(settings: shared)
        model.restoreRecipient(visible: templates)
        XCTAssertEqual(model.selectedTemplateID, "work")
    }

    /// A persona that is hidden now, or none at all: the first one shown,
    /// and the keyboard's choice is left as it was.
    func testFallsBackToTheFirstWithoutTouchingTheKeyboard() {
        let shared = settings()
        shared.setLastTemplateID("business")
        let model = ComposeViewModel(settings: shared)
        model.restoreRecipient(visible: templates.filter { $0.id != "business" })
        XCTAssertEqual(model.selectedTemplateID, "friend")
        XCTAssertEqual(shared.lastTemplateID, "business")

        let fresh = ComposeViewModel(settings: settings())
        fresh.restoreRecipient(visible: templates)
        XCTAssertEqual(fresh.selectedTemplateID, "friend")
    }

    func testChoosingHereIsTheKeyboardsChoiceToo() {
        let shared = settings()
        let model = ComposeViewModel(settings: shared)
        model.restoreRecipient(visible: templates)
        let custom = templates.last!.id
        model.selectRecipient(custom)
        XCTAssertEqual(model.selectedTemplateID, custom)
        XCTAssertEqual(shared.lastTemplateID, custom)

        // Coming back keeps it, and a later keyboard change is not undone.
        model.restoreRecipient(visible: templates)
        XCTAssertEqual(model.selectedTemplateID, custom)
    }

    func testPasteTakesTheTextAndSaysWhenThereIsNone() {
        let model = ComposeViewModel(settings: settings())
        model.paste("  \n ")
        XCTAssertEqual(model.message, "")
        XCTAssertEqual(model.pasteNoticeKey, "compose.paste.empty")

        model.paste(nil)
        XCTAssertEqual(model.pasteNoticeKey, "compose.paste.empty")

        model.paste("  Сколько стоит доставка?\n")
        XCTAssertEqual(model.message, "Сколько стоит доставка?")
        XCTAssertNil(model.pasteNoticeKey)
    }

    /// The grid holds the built-ins in the user's order with their dative
    /// names; templates the user made are rows under it.
    func testGridShowsTheBuiltInsInTheDative() {
        let items = RecipientGrid.items(for: templates, language: .russian)
        XCTAssertEqual(items.map(\.name), ["Другу", "Клиенту", "Коллеге", "Партнёру"])
        XCTAssertEqual(items.map(\.symbol), ["person.2", "person", "briefcase", "person.line.dotted.person"])
        XCTAssertEqual(RecipientGrid.items(kinds: [.client, .friend], language: .kazakh).map(\.name),
                       ["Клиентке", "Досқа"])
    }
}
