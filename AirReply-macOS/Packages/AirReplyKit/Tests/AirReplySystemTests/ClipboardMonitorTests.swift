import AppKit
import Foundation
import Testing
@testable import AirReplySystem

@MainActor
final class FakePasteboard: PasteboardAccess {
    private(set) var changeCount = 100
    var isAccessDenied = false
    /// Runs inside `readString()`, to replace the contents while they are being read.
    var duringRead: (() -> Void)?
    private var content: String?
    private var currentTypes: [NSPasteboard.PasteboardType] = []
    private(set) var reads = 0
    private(set) var typeReads = 0

    var types: [NSPasteboard.PasteboardType] {
        typeReads += 1
        return currentTypes
    }

    /// Another app copies.
    func copy(_ text: String?, types: [NSPasteboard.PasteboardType] = [.string]) {
        changeCount += 1
        content = text
        currentTypes = types
    }

    func readString() -> String? {
        reads += 1
        duringRead?()
        return content
    }

    func writeString(_ string: String) -> Int {
        copy(string)
        return changeCount
    }
}

@MainActor
final class TestClock {
    var now = Date(timeIntervalSinceReferenceDate: 800_000_000)

    func advance(_ seconds: TimeInterval) {
        now += seconds
    }
}

extension CopiedTextResult {
    var copied: CopiedText? {
        if case .text(let copied) = self { copied } else { nil }
    }
}

@MainActor
@Suite("Clipboard monitor")
struct ClipboardMonitorTests {
    let pasteboard: FakePasteboard
    let clock: TestClock
    let monitor: ClipboardMonitor
    let tenMinutes: TimeInterval = 600

    init() {
        let pasteboard = FakePasteboard()
        let clock = TestClock()
        self.pasteboard = pasteboard
        self.clock = clock
        monitor = ClipboardMonitor(pasteboard: pasteboard, now: { clock.now })
    }

    @Test func freshCopyIsOfferedAsCopiedAndDatedWhenFirstSeen() {
        monitor.start()
        clock.advance(1)
        pasteboard.copy("  Здравствуйте! Документы получили?\n")
        monitor.poll()
        let seenAt = clock.now
        clock.advance(30)
        monitor.poll()
        let expected = CopiedText(text: "  Здравствуйте! Документы получили?\n", changeCount: pasteboard.changeCount, observedAt: seenAt)
        #expect(monitor.takeFreshCopiedText(maxAge: tenMinutes) == .text(expected))
        // Offered again on the next opening until handled, without asking macOS twice.
        #expect(monitor.takeFreshCopiedText(maxAge: tenMinutes) == .text(expected))
        #expect(pasteboard.reads == 1)
    }

    @Test func changeTheTimerHasNotSeenYetIsDatedNow() throws {
        monitor.start()
        clock.advance(42)
        pasteboard.copy("Только что скопировал")
        let first = try #require(monitor.takeFreshCopiedText(maxAge: tenMinutes).copied)
        #expect(first.observedAt == clock.now)
        clock.advance(3)
        monitor.poll()
        #expect(monitor.takeFreshCopiedText(maxAge: tenMinutes).copied?.observedAt == first.observedAt)
    }

    @Test func ownReplyIsNeverOffered() {
        monitor.start()
        monitor.copy("Да, получили, спасибо!")
        #expect(monitor.takeFreshCopiedText(maxAge: tenMinutes) == .none)
        monitor.poll()
        #expect(monitor.takeFreshCopiedText(maxAge: tenMinutes) == .none)
        #expect(pasteboard.reads == 0)
        pasteboard.copy("А когда подпишете?")
        #expect(monitor.takeFreshCopiedText(maxAge: tenMinutes).copied?.text == "А когда подпишете?")
    }

    @Test func handledCopyIsNotOfferedAgain() throws {
        monitor.start()
        pasteboard.copy("Когда встреча?")
        let copied = try #require(monitor.takeFreshCopiedText(maxAge: tenMinutes).copied)
        monitor.markHandled(copied)
        #expect(monitor.takeFreshCopiedText(maxAge: tenMinutes) == .none)
        clock.advance(5)
        monitor.poll()
        #expect(monitor.takeFreshCopiedText(maxAge: tenMinutes) == .none)
        // Copying the same words again is a new copy.
        pasteboard.copy("Когда встреча?")
        #expect(monitor.takeFreshCopiedText(maxAge: tenMinutes).copied?.text == "Когда встреча?")
    }

    @Test func handlingAnOlderCopyKeepsTheNewerOne() throws {
        monitor.start()
        pasteboard.copy("Первое")
        let first = try #require(monitor.takeFreshCopiedText(maxAge: tenMinutes).copied)
        pasteboard.copy("Второе")
        monitor.poll()
        monitor.markHandled(first)
        #expect(monitor.takeFreshCopiedText(maxAge: tenMinutes).copied?.text == "Второе")
    }

    @Test func staleCopyIsNotOfferedOrRead() {
        monitor.start()
        pasteboard.copy("Старое сообщение")
        monitor.poll()
        clock.advance(tenMinutes + 1)
        #expect(monitor.takeFreshCopiedText(maxAge: tenMinutes) == .none)
        #expect(pasteboard.reads == 0)
        #expect(pasteboard.typeReads == 0)
    }

    @Test func copyExactlyMaxAgeOldIsStillFresh() {
        monitor.start()
        pasteboard.copy("Ровно десять минут")
        monitor.poll()
        clock.advance(tenMinutes)
        #expect(monitor.takeFreshCopiedText(maxAge: tenMinutes).copied?.text == "Ровно десять минут")
    }

    @Test func copiesBeforeStartAreNeverOffered() {
        pasteboard.copy("Скопировано до запуска")
        #expect(monitor.takeFreshCopiedText(maxAge: tenMinutes) == .none)
        monitor.start()
        monitor.poll()
        #expect(monitor.takeFreshCopiedText(maxAge: tenMinutes) == .none)
        #expect(pasteboard.reads == 0)
    }

    @Test(arguments: [
        "org.nspasteboard.ConcealedType", "org.nspasteboard.TransientType",
        "org.nspasteboard.AutoGeneratedType", "com.agilebits.onepassword",
    ])
    func concealedItemsAreNeverRead(marker: String) {
        monitor.start()
        pasteboard.copy("correct horse battery staple", types: [.string, NSPasteboard.PasteboardType(marker)])
        #expect(monitor.takeFreshCopiedText(maxAge: tenMinutes) == .concealed)
        // Remembered as handled: the next opening shows nothing.
        #expect(monitor.takeFreshCopiedText(maxAge: tenMinutes) == .none)
        #expect(pasteboard.reads == 0)
    }

    @Test func deniedPasteboardIsNeverRead() {
        monitor.start()
        pasteboard.isAccessDenied = true
        pasteboard.copy("Секрет")
        #expect(monitor.takeFreshCopiedText(maxAge: tenMinutes) == .denied)
        #expect(pasteboard.reads == 0)
        #expect(pasteboard.typeReads == 0)
        // Allowed again in System Settings.
        pasteboard.isAccessDenied = false
        #expect(monitor.takeFreshCopiedText(maxAge: tenMinutes).copied?.text == "Секрет")
    }

    @Test func blankTextIsNotOffered() {
        monitor.start()
        pasteboard.copy(" \n\t\u{3000} ")
        #expect(monitor.takeFreshCopiedText(maxAge: tenMinutes) == .none)
    }

    @Test func itemsWithoutPlainTextAreNotRead() {
        monitor.start()
        pasteboard.copy(nil, types: [.png, .tiff])
        #expect(monitor.takeFreshCopiedText(maxAge: tenMinutes) == .none)
        // A file copied in Finder: its name is not a message.
        pasteboard.copy("Договор.pdf", types: [.fileURL, .string])
        #expect(monitor.takeFreshCopiedText(maxAge: tenMinutes) == .none)
        #expect(pasteboard.reads == 0)
    }

    @Test func refusedReadIsNotRetriedForTheSameCopy() {
        monitor.start()
        // Plain text is announced, but reading returns nothing (e.g. the user declined the paste prompt).
        pasteboard.copy(nil, types: [.string])
        #expect(monitor.takeFreshCopiedText(maxAge: tenMinutes) == .none)
        #expect(monitor.takeFreshCopiedText(maxAge: tenMinutes) == .none)
        #expect(pasteboard.reads == 1)
    }

    @Test func contentsReplacedWhileReadingAreDropped() {
        monitor.start()
        pasteboard.copy("Обычный текст")
        let concealed = NSPasteboard.PasteboardType("org.nspasteboard.ConcealedType")
        pasteboard.duringRead = { [pasteboard] in
            pasteboard.duringRead = nil
            pasteboard.copy("пароль", types: [.string, concealed])
        }
        #expect(monitor.takeFreshCopiedText(maxAge: tenMinutes) == .none)
        #expect(monitor.takeFreshCopiedText(maxAge: tenMinutes) == .concealed)
    }

    @Test func startSchedulesOneTimerAndStopEndsOffering() throws {
        monitor.start()
        let timer = try #require(monitor.timer)
        #expect(timer.isValid)
        #expect(timer.timeInterval == 1.0)
        #expect(timer.tolerance == 0.5)
        monitor.start()
        #expect(monitor.timer === timer)
        pasteboard.copy("Привет")
        monitor.stop()
        #expect(!timer.isValid)
        #expect(monitor.timer == nil)
        #expect(monitor.takeFreshCopiedText(maxAge: tenMinutes) == .none)
        // A restart offers only what is copied after it.
        monitor.start()
        #expect(monitor.takeFreshCopiedText(maxAge: tenMinutes) == .none)
        pasteboard.copy("Новое")
        #expect(monitor.takeFreshCopiedText(maxAge: tenMinutes).copied?.text == "Новое")
        monitor.stop()
    }

    @Test func releasingTheMonitorStopsItsTimer() throws {
        var released: ClipboardMonitor? = ClipboardMonitor(pasteboard: pasteboard, now: { .now })
        released?.start()
        let timer = try #require(released?.timer)
        released = nil
        #expect(!timer.isValid)
    }
}
