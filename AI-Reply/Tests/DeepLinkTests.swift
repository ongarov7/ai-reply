import XCTest
@testable import AIReply

/// Links from push notifications: what the app agrees to open, how a push's
/// userInfo becomes a destination and an event, and the router that waits for
/// the sign-in and consent gates instead of going around them.
///
/// Хабарлама сілтемелері: тек рұқсат етілген экрандар мен ai-reply.kz беттері.
final class DeepLinkTests: XCTestCase {

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
        XCTAssertEqual(DeepLink.destination(for: "https://help.ai-reply.kz/push?lang=kk"),
                       .web(try XCTUnwrap(URL(string: "https://help.ai-reply.kz/push?lang=kk"))))
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

    // MARK: userInfo

    /// The shape the server's APNs payload actually has.
    private let pushUserInfo: [AnyHashable: Any] = [
        "aps": ["alert": ["title": "Тариф", "body": "Тариф 3 күннен кейін бітеді"],
                "sound": "default", "thread-id": "subscription"],
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

    func testTapReportsNotificationOpenedWithTheIDs() {
        let event = NotificationPayload(userInfo: pushUserInfo).openedEvent
        XCTAssertEqual(event.name, "notification_opened")
        XCTAssertEqual(event.properties, [
            "notification_id": .string("0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d"),
            "delivery_id": .string("d_4f1c2a9b7d3e5f60"),
            "type": .string("subscription_expiring")
        ])
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
        XCTAssertEqual(payload.openedEvent.properties, ["notification_id": .string("42")])
    }

    /// One bad value would make the server refuse the whole event; it is left
    /// out instead.
    func testIDsTheServerWouldRefuseAreLeftOut() {
        let event = NotificationPayload(notificationID: "has spaces", deliveryID: "ok-1",
                                        type: String(repeating: "x", count: 65)).openedEvent
        XCTAssertEqual(event.properties, ["delivery_id": .string("ok-1")])
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
        XCTAssertEqual(AppRouter.path(for: .keyboard), [.keyboardSetup])
        XCTAssertEqual(AppRouter.path(for: .compose), [.compose])
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
