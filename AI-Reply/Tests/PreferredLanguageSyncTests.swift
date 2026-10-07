import XCTest
@testable import AIReply

/// The account's notification language: when the app sends it, what it
/// sends, and when it leaves the server's value alone.
///
/// Хабарламалар тілі: қашан жіберіледі, қашан серверде қалады.
@MainActor
final class PreferredLanguageSyncTests: XCTestCase {

    /// The account as the sync sees it, and every value that reached it.
    private final class FakeAccount {
        var isSignedIn = true
        /// "" for an account without a language, nil while unknown.
        var serverLanguage: String? = ""
        var appLanguage = "kk"
        var accepts = true
        private(set) var sent: [String] = []

        func send(_ code: String) -> Bool {
            sent.append(code)
            if accepts { serverLanguage = code }
            return accepts
        }
    }

    private func freshDefaults() -> UserDefaults {
        UserDefaults(suiteName: "PreferredLanguageSyncTests.\(UUID())")!
    }

    private func sync(_ account: FakeAccount, defaults: UserDefaults) -> PreferredLanguageSync {
        PreferredLanguageSync(defaults: defaults,
                              isSignedIn: { account.isSignedIn },
                              serverLanguage: { account.serverLanguage },
                              language: { account.appLanguage },
                              send: { code in account.send(code) })
    }

    /// Rule 2: an account without a language gets this app's.
    func testAnAccountWithoutALanguageGetsTheAppsOne() async {
        let account = FakeAccount()
        let defaults = freshDefaults()
        await sync(account, defaults: defaults).reconcile()
        XCTAssertEqual(account.sent, ["kk"])
        XCTAssertFalse(sync(account, defaults: defaults).hasPendingChange)
    }

    /// Rule 3: a language chosen on another device is not fought over.
    func testALanguageTheAccountHasIsLeftAlone() async {
        let account = FakeAccount()
        account.serverLanguage = "ru"
        await sync(account, defaults: freshDefaults()).reconcile()
        XCTAssertTrue(account.sent.isEmpty)
    }

    /// A server without the field (nil) is never sent it.
    func testAServerWithoutTheFieldIsNeverSentIt() async {
        let account = FakeAccount()
        account.serverLanguage = nil
        await sync(account, defaults: freshDefaults()).reconcile()
        XCTAssertTrue(account.sent.isEmpty)
    }

    /// Rule 1: a choice in Settings is sent even over the account's value.
    func testAChoiceInSettingsIsSent() async {
        let account = FakeAccount()
        account.serverLanguage = "ru"
        account.appLanguage = "uz"
        let defaults = freshDefaults()
        sync(account, defaults: defaults).languageChosen()
        await PreferredLanguageSync.waitForPushes(defaults: defaults)
        XCTAssertEqual(account.sent, ["uz"])
        XCTAssertEqual(account.serverLanguage, "uz")
    }

    /// Signed out, there is no account to tell, and nothing is kept for the
    /// next one to sign in.
    func testAChoiceWhileSignedOutIsNotKept() async {
        let account = FakeAccount()
        account.isSignedIn = false
        let defaults = freshDefaults()
        sync(account, defaults: defaults).languageChosen()
        await PreferredLanguageSync.waitForPushes(defaults: defaults)
        XCTAssertFalse(sync(account, defaults: defaults).hasPendingChange)

        account.isSignedIn = true
        account.serverLanguage = "en"
        await sync(account, defaults: defaults).reconcile()
        XCTAssertTrue(account.sent.isEmpty, "the new account keeps its own language")
    }

    /// A failed send stays pending across launches and goes out on the next
    /// foreground, with the language in effect then.
    func testAFailedSendIsRetriedLater() async {
        let account = FakeAccount()
        account.accepts = false
        let defaults = freshDefaults()
        sync(account, defaults: defaults).languageChosen()
        await PreferredLanguageSync.waitForPushes(defaults: defaults)
        XCTAssertTrue(sync(account, defaults: defaults).hasPendingChange)

        account.accepts = true
        account.appLanguage = "en"
        await sync(account, defaults: defaults).reconcile()
        XCTAssertEqual(account.sent, ["kk", "en"])
        XCTAssertFalse(sync(account, defaults: defaults).hasPendingChange)
    }

    func testSignOutDropsAnUnsentLanguage() async {
        let account = FakeAccount()
        account.accepts = false
        let defaults = freshDefaults()
        sync(account, defaults: defaults).languageChosen()
        await PreferredLanguageSync.waitForPushes(defaults: defaults)

        PreferredLanguageSync.discardPendingChange(defaults: defaults)
        XCTAssertFalse(sync(account, defaults: defaults).hasPendingChange)
    }

    /// The language and the gender keep separate pending marks: confirming
    /// one never clears the other.
    func testTheLanguageAndTheGenderArePendingSeparately() {
        let defaults = freshDefaults()
        let gender = PendingProfileChange(defaults: defaults)
        let language = PendingProfileChange(defaults: defaults, field: .preferredLanguage)
        gender.markChanged()
        let revision = language.markChanged()
        language.confirm(revision: revision)
        XCTAssertTrue(gender.isPending)
        XCTAssertFalse(language.isPending)
    }
}
