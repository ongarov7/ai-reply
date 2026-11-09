import Foundation
import Observation

/// What the Apple Watch screen in Settings shows.
///
/// Read from what `WatchCompanion` publishes, and only said as what is known:
/// paired is not installed, and installed is not reachable.
@MainActor
@Observable
final class WatchLinkModel {

    enum TestResult: Equatable {
        case idle
        case running
        case answered(milliseconds: Int)
        case noAnswer
    }

    private(set) var connection: WatchConnectionState = .activating
    private(set) var test = TestResult.idle

    @ObservationIgnored private let store: WatchVoiceStore

    init(store: WatchVoiceStore = .shared) {
        self.store = store
    }

    func start() {
        refresh()
        WatchCompanion.shared.onLinkChange = { [weak self] in
            Task { @MainActor in self?.refresh() }
        }
    }

    func refresh() {
        connection = store.readLink()?.connectionState() ?? .activating
    }

    /// Really asks the watch. It can only answer with AI Reply open: iOS
    /// offers no way to open it from the iPhone.
    func testConnection() async {
        test = .running
        if let seconds = await WatchCompanion.shared.ping() {
            test = .answered(milliseconds: Int(seconds * 1000))
        } else {
            test = .noAnswer
        }
        refresh()
    }
}
