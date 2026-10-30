import AppKit
import SwiftUI
import Testing
@testable import AirReplyCore
@testable import AirReplySystem
@testable import AirReplyUI
@testable import AirReplyVoice

/// Renders the quick bar in its main states, light and dark, into one sheet per
/// appearance for a visual review. Opt-in: AIREPLY_SNAPSHOT_DIR=/some/folder swift test --filter SnapshotTests
@Suite("Snapshots", .enabled(if: ProcessInfo.processInfo.environment["AIREPLY_SNAPSHOT_DIR"] != nil))
@MainActor
struct SnapshotTests {
    @Test func quickBarStates() async throws {
        _ = NSApplication.shared
        let folder = URL(fileURLWithPath: ProcessInfo.processInfo.environment["AIREPLY_SNAPSHOT_DIR"]!)
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        let fixture = QuickBarModelTests()

        var states: [(String, QuickBarModel)] = []
        let signedOut = try fixture.makeEnvironment([])
        signedOut.auth.sessionEnded()
        signedOut.model.emailInput = ""
        states.append(("signed-out", signedOut.model))

        let compose = try fixture.makeEnvironment([])
        compose.model.draft = "Напиши клиенту вежливо, что документы получили и завтра ответим"
        states.append(("compose", compose.model))

        let reply = try fixture.makeEnvironment([])
        reply.model.panelDidOpen(copied: .text(CopiedText(text: "Добрый день! Документы дошли? Когда сможете ответить по договору?",
                                                          changeCount: 1, observedAt: .now)))
        states.append(("reply", reply.model))

        let result = try fixture.makeEnvironment([QuickBarModelTests.output(
            "Добрый день! Документы получили, спасибо. Завтра до обеда пришлём ответ по договору.")])
        result.account.apply(usage: try JSONDecoder.api.decode(UsageDTO.self, from: Data(
            #"{"daily_limit":30,"unlimited":false,"remaining_today":27,"credits":0,"remaining_total":27}"#.utf8)))
        result.model.draft = "ответь клиенту"
        result.model.submit()
        for _ in 0..<100 where result.model.phase != .result { try await Task.sleep(for: .milliseconds(10)) }
        states.append(("result", result.model))

        let limit = try fixture.makeEnvironment([.failure(.limitReached(resetsAt: nil))])
        limit.model.draft = "x"
        limit.model.submit()
        for _ in 0..<100 where limit.model.issue == nil { try await Task.sleep(for: .milliseconds(10)) }
        states.append(("limit", limit.model))

        let consent = try fixture.makeEnvironment([], consent: false)
        states.append(("consent", consent.model))

        for dark in [false, true] {
            let sheet = try render(states.map(\.1), dark: dark)
            let name = dark ? "quickbar-dark.png" : "quickbar-light.png"
            try sheet.write(to: folder.appendingPathComponent(name))
        }
    }

    private func render(_ models: [QuickBarModel], dark: Bool) throws -> Data {
        let stack = VStack(spacing: 0) {
            ForEach(Array(models.enumerated()), id: \.offset) { _, model in
                QuickBarView(model: model, pricingURL: URL(string: "https://ai-reply.kz/payment")!, legal: {
                    ServerConfig.Legal(termsVersion: "t", privacyVersion: "p", termsURL: URL(string: "https://ai-reply.kz/offer"),
                                       privacyURL: URL(string: "https://ai-reply.kz/privacy"), supportURL: nil,
                                       accountDeletionURL: nil, aiProvider: "OpenAI")
                }, onHeightChange: { _ in })
            }
        }
        .background(dark ? Color(white: 0.16) : Color(red: 0.86, green: 0.89, blue: 0.93))
        let hosting = NSHostingView(rootView: stack)
        hosting.appearance = NSAppearance(named: dark ? .darkAqua : .aqua)
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 716, height: 200), styleMask: [.borderless],
                              backing: .buffered, defer: false)
        window.contentView = hosting
        window.appearance = hosting.appearance
        hosting.layoutSubtreeIfNeeded()
        hosting.setFrameSize(hosting.fittingSize)
        hosting.layoutSubtreeIfNeeded()
        let rep = try #require(hosting.bitmapImageRepForCachingDisplay(in: hosting.bounds))
        hosting.cacheDisplay(in: hosting.bounds, to: rep)
        return try #require(rep.representation(using: .png, properties: [:]))
    }
}
