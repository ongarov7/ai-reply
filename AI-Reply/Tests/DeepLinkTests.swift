import XCTest
@testable import AIReply

/// Links from push notifications: what the app agrees to open, how a push's
/// userInfo becomes a destination, and the router that waits for the sign-in
/// and consent gates instead of going around them.
///
/// Хабарлама сілтемелері: тек рұқсат етілген экрандар мен ai-reply.kz беттері.
final class DeepLinkTests: XCTestCase {

    private static let handoffToken = String(repeating: "a", count: 43)

    private final class FakeWebHandoff: WebPaymentHandoffProviding, @unchecked Sendable {
        var result: Result<WebPaymentHandoff, APIError>
        var holdsAnswers = false
        private(set) var users: [String] = []
        private(set) var locales: [String] = []

        init(url: String = "https://ai-reply.kz/sandbox/payment#handoff=" + DeepLinkTests.handoffToken) {
            result = .success(WebPaymentHandoff(url: url, expiresIn: 60))
        }

        func create(expectedUserID: String, locale: String) async throws -> WebPaymentHandoff {
            users.append(expectedUserID)
            locales.append(locale)
            // Deliberately ignore cancellation: the router must reject a late
            // response even when an underlying transport has already sent it.
            while holdsAnswers { await Task.yield() }
            return try result.get()
        }
    }

    @MainActor
    private func waitForHandoff(_ router: AppRouter) async throws {
        for _ in 0..<200 where router.isOpeningWebOffer { try await Task.sleep(for: .milliseconds(5)) }
        XCTAssertFalse(router.isOpeningWebOffer)
    }


    // MARK: aireply://

    func testEveryKnownScreenOpensItself() {
        for screen in AppScreen.allCases {
            XCTAssertEqual(DeepLink.destination(for: "aireply://\(screen.rawValue)"), .screen(screen), screen.rawValue)
        }
    }

    func testSchemeAndScreenAreCaseInsensitive() {
        XCTAssertEqual(DeepLink.destination(for: "AIREPLY://Subscription"), .screen(.subscription))
        XCTAssertEqual(DeepLink.destination(for: "  aireply://settings  "), .screen(.settings))
    }

    func testUnknownOrMissingScreenOpensHome() {
        XCTAssertEqual(DeepLink.destination(for: "aireply://wallet"), .screen(.home))
        XCTAssertEqual(DeepLink.destination(for: "aireply://"), .screen(.home))
        XCTAssertEqual(DeepLink.destination(for: "aireply:subscription"), .screen(.home))
    }

    // MARK: https://

    func testOurSiteOpensInTheApp() throws {
        XCTAssertEqual(DeepLink.destination(for: "https://ai-reply.kz/offer"),
                       .web(try XCTUnwrap(URL(string: "https://ai-reply.kz/offer"))))
        XCTAssertEqual(DeepLink.destination(for: "https://ai-reply.kz/sandbox/privacy?lang=kk"),
                       .web(try XCTUnwrap(URL(string: "https://ai-reply.kz/sandbox/privacy?lang=kk"))))
    }

    func testEverythingElseOnlyOpensTheApp() {
        let refused = [
            "http://ai-reply.kz/offer",                // not https
            "https://example.com/",                    // another site
            "https://ai-reply.kz.example.com/",        // look-alike suffix
            "https://evil-ai-reply.kz/",               // look-alike prefix
            "https://user@ai-reply.kz/",               // user info
            "https://user:pass@ai-reply.kz/",
            "https://ai-reply.kz@example.com/",        // the host is example.com
            "https://ai-reply.kz:8443/",               // a port
            "javascript:alert(1)",
            "tel:+77010000000",
            "file:///etc/passwd",
            "",
            "   ",
            "https://ai-reply.kz/" + String(repeating: "a", count: 600)
        ]
        for link in refused {
            XCTAssertEqual(DeepLink.destination(for: link), .none, link)
        }
        XCTAssertEqual(DeepLink.destination(for: nil), .none)
    }

    func testOrdinaryWebLinksCannotOpenCheckoutOrUnlistedPages() {
        for link in [
            "https://ai-reply.kz/", "https://help.ai-reply.kz/privacy",
            "https://ai-reply.kz/account", "https://ai-reply.kz/sandbox/account",
            "https://ai-reply.kz/payment", "https://ai-reply.kz/sandbox/payment",
            "https://ai-reply.kz/checkout", "https://ai-reply.kz/privacy/../payment",
            "https://ai-reply.kz/%70rivacy", "https://ai-reply.kz/privacy?redirect=/payment",
            "https://ai-reply.kz/privacy?lang=ru&lang=en", "https://ai-reply.kz/privacy#payment"
        ] {
            XCTAssertEqual(DeepLink.destination(for: link), .none, link)
        }
    }

    func testOnlyDedicatedMarketingOfferCanOpenExactPaymentURL() throws {
        for path in ["/payment", "/sandbox/payment?lang=ru"] {
            let url = try XCTUnwrap(URL(string: "https://ai-reply.kz" + path))
            let payload = NotificationPayload(type: "web_topup_offer", category: "marketing", link: url.absoluteString)
            XCTAssertEqual(payload.destination, .externalWeb(url))
            XCTAssertTrue(payload.isWebTopupOffer)
            XCTAssertEqual(NotificationPayload(type: "news", category: "marketing", link: url.absoluteString).destination, .none)
        }
        for link in ["https://ai-reply.kz/account", "https://ai-reply.kz/payment/", "https://ai-reply.kz/payment?next=bank",
                     "https://ai-reply.kz/payment#x", "https://ai-reply.kz:443/payment", "http://ai-reply.kz/payment",
                     "https://help.ai-reply.kz/payment", "https://example.com/payment", "aireply://purchase"] {
            XCTAssertEqual(NotificationPayload(type: "web_topup_offer", category: "marketing", link: link).destination, .none, link)
        }
        XCTAssertEqual(NotificationPayload(type: "web_topup_offer", category: "account", link: "https://ai-reply.kz/payment").destination, .none)
    }

    // MARK: userInfo

    /// The shape an FCM notification arrives in: the message's data as flat
    /// keys next to `aps`.
    private let pushUserInfo: [AnyHashable: Any] = [
        "aps": ["alert": ["title": "Тариф", "body": "Тариф 3 күннен кейін бітеді"],
                "sound": "default", "thread-id": "subscription"],
        "gcm.message_id": "1720000000000000",
        "google.c.a.e": "1",
        "nid": "0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d",
        "did": "d_4f1c2a9b7d3e5f60",
        "type": "subscription_expiring",
        "category": "subscription",
        "link": "aireply://subscription",
        "campaign_note": "ignored"
    ]

    func testPayloadReadsTheServersKeys() {
        let payload = NotificationPayload(userInfo: pushUserInfo)
        XCTAssertEqual(payload.notificationID, "0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d")
        XCTAssertEqual(payload.deliveryID, "d_4f1c2a9b7d3e5f60")
        XCTAssertEqual(payload.type, "subscription_expiring")
        XCTAssertEqual(payload.category, "subscription")
        XCTAssertEqual(payload.destination, .screen(.subscription))
    }

    func testMissingOrMalformedKeysAreSimplyAbsent() {
        let payload = NotificationPayload(userInfo: [
            "aps": ["alert": "Hi"],
            "nid": 42,
            "did": ["not", "a", "string"],
            "link": ""
        ])
        XCTAssertEqual(payload.notificationID, "42")
        XCTAssertNil(payload.deliveryID)
        XCTAssertNil(payload.type)
        XCTAssertEqual(payload.destination, .none, "no link: the app just opens")
    }

    // MARK: Router

    @MainActor
    func testDestinationWaitsForTheGates() {
        let router = AppRouter()
        router.open(.screen(.subscription))
        XCTAssertEqual(router.path, [], "a gate is showing: nothing is pushed around it")
        XCTAssertEqual(router.pending, .screen(.subscription))

        router.mainInterfaceDidAppear()
        XCTAssertEqual(router.path, [.subscription])
        XCTAssertNil(router.pending)
    }

    @MainActor
    func testDestinationAppliesAtOnceWhenHomeIsShowing() {
        let router = AppRouter()
        router.mainInterfaceDidAppear()
        router.open(.screen(.templates))
        XCTAssertEqual(router.path, [.templates])
        router.open(.screen(.home))
        XCTAssertEqual(router.path, [], "home pops back to the root")
    }

    @MainActor
    func testSigningOutPutsTheGateBack() {
        let router = AppRouter()
        router.mainInterfaceDidAppear()
        router.mainInterfaceDidDisappear()
        router.open(.screen(.settings))
        XCTAssertEqual(router.path, [])
        XCTAssertEqual(router.pending, .screen(.settings))
    }

    /// Home → Settings, sign out, sign in again (or restart onboarding): Home
    /// opens on Home, not on the screen the last session left.
    @MainActor
    func testAGateComingBackStartsHomeFromItsRootNextTime() throws {
        let router = AppRouter()
        router.mainInterfaceDidAppear()
        router.path = [.settings]
        router.webPage = AppRouter.WebPage(url: try XCTUnwrap(URL(string: "https://ai-reply.kz/offer")))

        router.mainInterfaceDidDisappear()
        XCTAssertEqual(router.path, [], "the next account must not land on the last one's screen")
        XCTAssertNil(router.webPage)

        router.mainInterfaceDidAppear()
        XCTAssertEqual(router.path, [])
    }

    /// A notification tapped while the sign-in gate is up still opens its
    /// screen once the gate is passed.
    @MainActor
    func testADestinationWaitingBehindTheGateSurvivesIt() {
        let router = AppRouter()
        router.mainInterfaceDidAppear()
        router.path = [.settings]
        router.mainInterfaceDidDisappear()
        router.open(.screen(.subscription))
        router.mainInterfaceDidDisappear()
        XCTAssertEqual(router.pending, .screen(.subscription))

        router.mainInterfaceDidAppear()
        XCTAssertEqual(router.path, [.subscription])
    }

    @MainActor
    func testAStaleDestinationIsDropped() {
        var now = Date(timeIntervalSince1970: 1_800_000_000)
        let router = AppRouter(now: { now })
        router.open(.screen(.compose))
        now = now.addingTimeInterval(AppRouter.pendingLifetime + 1)
        router.mainInterfaceDidAppear()
        XCTAssertEqual(router.path, [], "signing in much later must not jump to an old screen")
    }

    @MainActor
    func testWebLinksOpenTheInAppBrowser() throws {
        let router = AppRouter()
        router.mainInterfaceDidAppear()
        let url = try XCTUnwrap(URL(string: "https://ai-reply.kz/offer"))
        router.open(.web(url))
        XCTAssertEqual(router.webPage?.url, url)
        XCTAssertEqual(router.path, [])
    }

    @MainActor
    func testExternalOfferWaitsForAllGatesAndNeverUsesEmbeddedBrowser() async throws {
        var opened: [URL] = []
        let provider = FakeWebHandoff()
        let router = AppRouter(openExternal: { opened.append($0); return true }, handoffProvider: provider,
                               backendBaseURL: { URL(string: "https://ai-reply.kz/sandbox") }, locale: { "kk" })
        let url = try XCTUnwrap(URL(string: "https://ai-reply.kz/sandbox/payment"))
        let payload = NotificationPayload(type: "web_topup_offer", category: "marketing", link: url.absoluteString)
        router.open(payload.destination)
        XCTAssertTrue(opened.isEmpty)
        XCTAssertTrue(provider.users.isEmpty, "no handoff is requested before the gates")
        router.mainInterfaceDidAppear()
        XCTAssertTrue(provider.users.isEmpty, "Home alone cannot replace an authenticated account")
        router.accountDidChange(userID: "account-A", isReady: true)
        try await waitForHandoff(router)
        XCTAssertEqual(provider.users, ["account-A"])
        XCTAssertEqual(provider.locales, ["kk"])
        XCTAssertEqual(opened.map(\.absoluteString), ["https://ai-reply.kz/sandbox/payment#handoff=" + Self.handoffToken])
        XCTAssertNil(router.webPage)
        XCTAssertTrue(router.path.isEmpty)
        router.mainInterfaceDidAppear()
        XCTAssertEqual(opened.count, 1, "the tap is consumed once")
    }

    @MainActor
    func testRouterRejectsPaymentInEmbeddedBrowserAndArbitraryExternalURL() throws {
        var opened: [URL] = []
        let router = AppRouter(openExternal: { opened.append($0); return true })
        router.mainInterfaceDidAppear()
        router.open(.web(try XCTUnwrap(URL(string: "https://ai-reply.kz/payment"))))
        router.open(.externalWeb(try XCTUnwrap(URL(string: "https://ai-reply.kz/account"))))
        router.open(.externalWeb(try XCTUnwrap(URL(string: "https://example.com/payment"))))
        XCTAssertTrue(opened.isEmpty)
        XCTAssertNil(router.webPage)
        XCTAssertNil(router.pending)
    }

    @MainActor
    func testStaleExternalOfferDoesNotOpenAfterSignIn() throws {
        var opened: [URL] = []
        var now = Date(timeIntervalSince1970: 1_800_000_000)
        let router = AppRouter(now: { now }, openExternal: { opened.append($0); return true })
        router.open(.externalWeb(try XCTUnwrap(URL(string: "https://ai-reply.kz/payment"))))
        now = now.addingTimeInterval(AppRouter.pendingLifetime + 1)
        router.mainInterfaceDidAppear()
        XCTAssertTrue(opened.isEmpty)
        XCTAssertNil(router.pending)
    }

    @MainActor
    func testAccountSwitchRejectsLateHandoffResponse() async throws {
        var opened: [URL] = []
        let provider = FakeWebHandoff()
        provider.holdsAnswers = true
        let router = AppRouter(openExternal: { opened.append($0); return true }, handoffProvider: provider,
                               backendBaseURL: { URL(string: "https://ai-reply.kz/sandbox") })
        router.accountDidChange(userID: "account-A", isReady: true)
        router.mainInterfaceDidAppear()
        router.open(.externalWeb(try XCTUnwrap(URL(string: "https://ai-reply.kz/sandbox/payment"))))
        for _ in 0..<200 where provider.users.isEmpty { try await Task.sleep(for: .milliseconds(5)) }
        XCTAssertEqual(provider.users, ["account-A"])
        router.accountDidChange(userID: "account-B", isReady: true)
        provider.holdsAnswers = false
        try await Task.sleep(for: .milliseconds(30))
        XCTAssertTrue(opened.isEmpty)
        XCTAssertFalse(router.webOfferOpeningFailed)
        router.retryWebOffer()
        XCTAssertEqual(provider.users, ["account-A"], "B cannot retry A's request")
    }

    @MainActor
    func testGateReturningCancelsHandoffWithoutOpeningTheBrowser() async throws {
        var opened: [URL] = []
        let provider = FakeWebHandoff()
        provider.holdsAnswers = true
        let router = AppRouter(openExternal: { opened.append($0); return true }, handoffProvider: provider,
                               backendBaseURL: { URL(string: "https://ai-reply.kz/sandbox") })
        router.accountDidChange(userID: "account-A", isReady: true)
        router.mainInterfaceDidAppear()
        router.open(.externalWeb(try XCTUnwrap(URL(string: "https://ai-reply.kz/sandbox/payment"))))
        for _ in 0..<200 where provider.users.isEmpty { try await Task.sleep(for: .milliseconds(5)) }
        router.mainInterfaceDidDisappear()
        provider.holdsAnswers = false
        try await Task.sleep(for: .milliseconds(30))
        XCTAssertTrue(opened.isEmpty)
        XCTAssertFalse(router.webOfferOpeningFailed)
    }

    @MainActor
    func testHandoffFailureHasNoPlainURLFallbackAndCanBeRetriedExplicitly() async throws {
        var opened: [URL] = []
        let provider = FakeWebHandoff()
        provider.result = .failure(.offline)
        let router = AppRouter(openExternal: { opened.append($0); return true }, handoffProvider: provider,
                               backendBaseURL: { URL(string: "https://ai-reply.kz/sandbox") })
        router.accountDidChange(userID: "account-A", isReady: true)
        router.mainInterfaceDidAppear()
        router.open(.externalWeb(try XCTUnwrap(URL(string: "https://ai-reply.kz/sandbox/payment"))))
        try await waitForHandoff(router)
        XCTAssertTrue(opened.isEmpty)
        XCTAssertTrue(router.webOfferOpeningFailed)
        XCTAssertEqual(provider.users.count, 1)
        provider.result = .success(WebPaymentHandoff(url: "https://ai-reply.kz/sandbox/payment#handoff=" + Self.handoffToken,
                                                    expiresIn: 60))
        router.retryWebOffer()
        try await waitForHandoff(router)
        XCTAssertEqual(opened.count, 1)
        XCTAssertEqual(provider.users, ["account-A", "account-A"])
        XCTAssertFalse(router.webOfferOpeningFailed)
    }

    @MainActor
    func testTamperedOrExpiredHandoffNeverOpens() async throws {
        for response in [
            WebPaymentHandoff(url: "https://example.com/sandbox/payment#handoff=" + Self.handoffToken, expiresIn: 60),
            WebPaymentHandoff(url: "https://ai-reply.kz/payment#handoff=" + Self.handoffToken, expiresIn: 60),
            WebPaymentHandoff(url: "https://ai-reply.kz/sandbox/payment", expiresIn: 60),
            WebPaymentHandoff(url: "https://ai-reply.kz/sandbox/payment#handoff=" + Self.handoffToken, expiresIn: 0)
        ] {
            var opened: [URL] = []
            let provider = FakeWebHandoff()
            provider.result = .success(response)
            let router = AppRouter(openExternal: { opened.append($0); return true }, handoffProvider: provider,
                                   backendBaseURL: { URL(string: "https://ai-reply.kz/sandbox") })
            router.accountDidChange(userID: "account-A", isReady: true)
            router.mainInterfaceDidAppear()
            router.open(.externalWeb(try XCTUnwrap(URL(string: "https://ai-reply.kz/sandbox/payment"))))
            try await waitForHandoff(router)
            XCTAssertTrue(opened.isEmpty)
            XCTAssertTrue(router.webOfferOpeningFailed)
        }
    }

    @MainActor
    func testHandoffThatSpentItsLifetimeInFlightIsRejected() async throws {
        var opened: [URL] = []
        var now = Date(timeIntervalSince1970: 1_800_000_000)
        let provider = FakeWebHandoff()
        provider.holdsAnswers = true
        let router = AppRouter(now: { now }, openExternal: { opened.append($0); return true }, handoffProvider: provider,
                               backendBaseURL: { URL(string: "https://ai-reply.kz/sandbox") })
        router.accountDidChange(userID: "account-A", isReady: true)
        router.mainInterfaceDidAppear()
        router.open(.externalWeb(try XCTUnwrap(URL(string: "https://ai-reply.kz/sandbox/payment"))))
        for _ in 0..<200 where provider.users.isEmpty { try await Task.sleep(for: .milliseconds(5)) }
        now = now.addingTimeInterval(61)
        provider.holdsAnswers = false
        try await waitForHandoff(router)
        XCTAssertTrue(opened.isEmpty)
        XCTAssertTrue(router.webOfferOpeningFailed)
    }

    func testHandoffBodyAndResponseContract() throws {
        let request = BackendWebPaymentHandoffProvider.Request(expectedUserID: "account-A", locale: "ru")
        let body = try XCTUnwrap(JSONSerialization.jsonObject(with: JSONEncoder().encode(request)) as? [String: String])
        XCTAssertEqual(body, ["expected_user_id": "account-A", "locale": "ru"])
        let response = try JSONDecoder().decode(WebPaymentHandoff.self,
            from: Data(#"{"url":"https://ai-reply.kz/payment#handoff=opaque","expires_in":60}"#.utf8))
        XCTAssertEqual(response.expiresIn, 60)
    }

    func testHandoffURLValidationHasExactEnvironmentAndTokenShape() throws {
        let prod = try XCTUnwrap(URL(string: "https://ai-reply.kz"))
        let sandbox = try XCTUnwrap(URL(string: "https://ai-reply.kz/sandbox"))
        let suffix = "#handoff=" + Self.handoffToken
        XCTAssertNotNil(DeepLink.handoffURL("https://ai-reply.kz/payment" + suffix, backendBaseURL: prod))
        XCTAssertNotNil(DeepLink.handoffURL("https://ai-reply.kz/sandbox/payment?lang=uz" + suffix, backendBaseURL: sandbox))
        for raw in ["https://ai-reply.kz/payment" + suffix,
                    "https://ai-reply.kz/sandbox/payment#handoff=short",
                    "https://ai-reply.kz/sandbox/payment" + suffix + "&next=x",
                    "https://ai-reply.kz/sandbox/payment" + suffix + "=",
                    "https://ai-reply.kz/sandbox/payment?next=bank" + suffix,
                    "https://ai-reply.kz/sandbox/payment?lang=ru&lang=en" + suffix,
                    "https://ai-reply.kz/sandbox/%70ayment" + suffix,
                    "https://ai-reply.kz:443/sandbox/payment" + suffix,
                    "https://user@ai-reply.kz/sandbox/payment" + suffix,
                    "https://help.ai-reply.kz/sandbox/payment" + suffix,
                    "http://ai-reply.kz/sandbox/payment" + suffix] {
            XCTAssertNil(DeepLink.handoffURL(raw, backendBaseURL: sandbox), raw)
            XCTAssertEqual(NotificationPayload(type: "web_topup_offer", category: "marketing", link: raw).destination, .none)
        }
        XCTAssertNil(DeepLink.handoffURL("https://ai-reply.kz/sandbox/payment" + suffix, backendBaseURL: prod))
    }

    @MainActor
    func testNothingToOpenChangesNothing() {
        let router = AppRouter()
        router.open(.none)
        XCTAssertNil(router.pending)
        router.mainInterfaceDidAppear()
        XCTAssertEqual(router.path, [])
        XCTAssertNil(router.webPage)
    }

    func testEveryScreenHasARoute() {
        XCTAssertEqual(AppRouter.path(for: .home), [])
        XCTAssertEqual(AppRouter.path(for: .subscription), [.subscription])
        XCTAssertEqual(AppRouter.path(for: .settings), [.settings])
        XCTAssertEqual(AppRouter.path(for: .notifications), [.notificationSettings])
        XCTAssertEqual(AppRouter.path(for: .templates), [.templates])
        XCTAssertEqual(AppRouter.path(for: .profile), [.profile])
        XCTAssertEqual(AppRouter.path(for: .keyboard), [.permissions], "the old link opens the new section")
        XCTAssertEqual(AppRouter.path(for: .permissions), [.permissions])
        XCTAssertEqual(AppRouter.path(for: .pasteHelp), [.pasteHelp])
        XCTAssertEqual(AppRouter.path(for: .compose), [.compose])
        XCTAssertEqual(AppRouter.path(for: .onboarding), [])
        XCTAssertEqual(AppRouter.path(for: .support), [], "no chat on this server: Home")
        XCTAssertEqual(AppRouter.path(for: .support, availability: .init(supportChat: true)), [.support])
        XCTAssertEqual(AppRouter.path(for: .purchase), [.subscription], "purchases closed: the plans")
        XCTAssertEqual(AppRouter.path(for: .purchase, availability: .init(purchases: true)), [.subscription])
        for screen in AppScreen.allCases {
            XCTAssertEqual(AppRouter.presentsTutorial(screen), screen == .onboarding, screen.rawValue)
        }
    }

    /// An operator's reply opens the chat: by its `conversation_id`, by its
    /// type, or by its link.
    @MainActor
    func testAnOperatorsReplyOpensTheChat() {
        let payload = NotificationPayload(userInfo: [
            "aps": ["alert": ["title": "Ответ поддержки", "body": "Оператор ответил на ваш вопрос."]],
            "nid": "n1", "did": "d1", "type": "support_reply", "category": "account",
            "link": "aireply://support", "conversation_id": "c_42"
        ])
        XCTAssertEqual(payload.conversationID, "c_42")
        XCTAssertTrue(payload.isSupportReply)
        XCTAssertEqual(payload.destination, .screen(.support))
        XCTAssertEqual(NotificationPayload(link: "", conversationID: "c_42").destination, .screen(.support))
        XCTAssertEqual(NotificationPayload(type: "subscription_expiring", link: "aireply://subscription").destination,
                       .screen(.subscription))

        let router = AppRouter()
        router.availability = AppRouter.Availability(supportChat: true)
        router.mainInterfaceDidAppear()
        router.open(payload.destination)
        XCTAssertEqual(router.path, [.support])
    }

    /// Older purchase links always open the neutral balance screen.
    @MainActor
    func testPurchaseLinkCannotEnableCommerceWithAServerSwitch() {
        let router = AppRouter()
        router.mainInterfaceDidAppear()
        router.open(.screen(.purchase))
        XCTAssertEqual(router.path, [.subscription])

        router.availability = AppRouter.Availability(purchases: true)
        router.open(.screen(.purchase))
        XCTAssertEqual(router.path, [.subscription])
    }

    /// The app's screens are the server's `domain.LinkScreens`, spelled alike.
    func testScreensMatchTheServersList() {
        XCTAssertEqual(AppScreen.allCases.map(\.rawValue),
                       ["home", "subscription", "settings", "notifications", "templates", "profile", "keyboard",
                        "compose", "onboarding", "permissions", "paste_help", "support", "purchase"])
        XCTAssertEqual(DeepLink.destination(for: "aireply://paste_help"), .screen(.pasteHelp))
    }

    /// `aireply://onboarding` shows the guide over Home, once the gates are
    /// passed; a sign-out takes it down with everything else.
    @MainActor
    func testOnboardingLinkShowsTheGuideOverHome() {
        let router = AppRouter()
        router.open(.screen(.onboarding))
        XCTAssertFalse(router.isShowingTutorial, "not around the gates")

        router.mainInterfaceDidAppear()
        XCTAssertTrue(router.isShowingTutorial)
        XCTAssertEqual(router.path, [])

        router.mainInterfaceDidDisappear()
        XCTAssertFalse(router.isShowingTutorial)
    }

    /// The link waited behind the first-run guide, which the user has just
    /// gone through: it is not shown a second time.
    @MainActor
    func testFinishingTheFirstRunAnswersAWaitingOnboardingLink() {
        let router = AppRouter()
        router.open(.screen(.onboarding))
        router.firstRunOnboardingDidFinish()
        router.mainInterfaceDidAppear()
        XCTAssertFalse(router.isShowingTutorial)

        router.mainInterfaceDidDisappear()
        router.open(.screen(.subscription))
        router.firstRunOnboardingDidFinish()
        router.mainInterfaceDidAppear()
        XCTAssertEqual(router.path, [.subscription], "other links still open")
    }

    #if DEBUG
    /// The build watcher strips ":" and "/" from launch arguments.
    func testDebugLinkArgumentAcceptsABareScreenName() {
        XCTAssertEqual(DebugLaunchOptions.expandLink("subscription"), "aireply://subscription")
        XCTAssertEqual(DebugLaunchOptions.expandLink("web"), "https://ai-reply.kz/")
        XCTAssertEqual(DebugLaunchOptions.expandLink("aireply://settings"), "aireply://settings")
    }
    #endif
}
