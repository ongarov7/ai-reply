import Foundation
import Observation

/// What the app can honestly say about its keyboard right now.
///
/// iOS has no public API that tells an app whether its own keyboard is added
/// or has Full Access. Everything here is evidence the keyboard left itself:
/// the App Group heartbeat (written only with Full Access) and the Darwin
/// notification it posts on every appearance (`KeyboardPresence`).
struct KeyboardStatus: Equatable {

    enum FullAccess: Equatable {
        case on
        case off
        /// The keyboard has not run since the app could last tell.
        case unknown
    }

    var isEnabled: Bool
    var fullAccess: FullAccess

    /// - Parameters:
    ///   - lastSeen: the keyboard's last heartbeat.
    ///   - heartbeatFullAccess: the Full Access flag written with it.
    ///   - limitedSeenAt: the last time the keyboard said it ran WITHOUT Full
    ///     Access. Only the app can store that: without Full Access the
    ///     keyboard cannot write the shared container at all.
    static func resolve(lastSeen: Date?, heartbeatFullAccess: Bool, limitedSeenAt: Date?) -> KeyboardStatus {
        let fullAccess: FullAccess
        if let limitedSeenAt, limitedSeenAt > (lastSeen ?? .distantPast) {
            fullAccess = .off
        } else if lastSeen != nil {
            fullAccess = heartbeatFullAccess ? .on : .off
        } else {
            fullAccess = .unknown
        }
        return KeyboardStatus(isEnabled: lastSeen != nil || limitedSeenAt != nil, fullAccess: fullAccess)
    }
}

/// Keeps `KeyboardStatus` current while the app runs.
///
/// Пернетақта күйін бақылайды: Darwin хабарламасы, App Group белгісі және
/// қажет кезде секунд сайын қайта оқу.
///
/// It re-reads on `refresh()` - scene activation, "Check again" - every second
/// while an onboarding step follows it (`followLiveUpdates()`), and the moment
/// the keyboard announces itself through `KeyboardPresence`. Nothing is polled
/// otherwise.
@MainActor
@Observable
final class KeyboardStatusMonitor {

    private(set) var status: KeyboardStatus

    @ObservationIgnored private let settings: SharedSettings
    @ObservationIgnored private let defaults: UserDefaults

    private enum Key {
        /// App-only: the keyboard cannot write the App Group without Full Access.
        static let limitedSeenAt = "keyboard.limitedSeenAt"
    }

    /// - Parameter observesKeyboard: false in tests, which drive
    ///   `keyboardDidAppear(hasFullAccess:)` directly.
    init(settings: SharedSettings = .shared, defaults: UserDefaults = .standard, observesKeyboard: Bool = true) {
        self.settings = settings
        self.defaults = defaults
        self.status = Self.read(settings: settings, defaults: defaults)
        if observesKeyboard { startObserving() }
    }

    deinit {
        CFNotificationCenterRemoveEveryObserver(
            CFNotificationCenterGetDarwinNotifyCenter(),
            Unmanaged.passUnretained(self).toOpaque()
        )
    }

    func refresh() {
        let next = Self.read(settings: settings, defaults: defaults)
        // Assigned only on a change, so a once-a-second refresh does not
        // redraw every screen that shows the status.
        if next != status { status = next }
        if status.isEnabled { ProductEvents.trackOnce(.keyboardEnabledDetected) }
        if status.fullAccess == .on { ProductEvents.trackOnce(.fullAccessEnabledDetected) }
    }

    /// Re-reads every second until the calling task is cancelled - a
    /// SwiftUI `.task` on a step that waits for the user to come back from
    /// iOS Settings, which ends when the step leaves the screen.
    func followLiveUpdates() async {
        while !Task.isCancelled {
            refresh()
            try? await Task.sleep(for: .seconds(1))
        }
    }

    /// The keyboard just appeared somewhere and said whether it has Full
    /// Access. "Without" is remembered here, because the keyboard could not
    /// write it; "with" means the heartbeat it wrote first is the truth.
    func keyboardDidAppear(hasFullAccess: Bool, now: Date = Date()) {
        if hasFullAccess {
            defaults.removeObject(forKey: Key.limitedSeenAt)
        } else {
            defaults.set(now.timeIntervalSince1970, forKey: Key.limitedSeenAt)
        }
        refresh()
    }

    private static func read(settings: SharedSettings, defaults: UserDefaults) -> KeyboardStatus {
        let limited = defaults.double(forKey: Key.limitedSeenAt)
        return KeyboardStatus.resolve(
            lastSeen: settings.keyboardLastSeen,
            heartbeatFullAccess: settings.keyboardHasFullAccess,
            limitedSeenAt: limited > 0 ? Date(timeIntervalSince1970: limited) : nil
        )
    }

    // MARK: Darwin notifications

    /// One app-lifetime instance lives in the environment, so the unretained
    /// observer pointer never outlives it; `deinit` removes it all the same.
    private func startObserving() {
        let center = CFNotificationCenterGetDarwinNotifyCenter()
        let observer = Unmanaged.passUnretained(self).toOpaque()
        for name in [KeyboardPresence.fullAccessName, KeyboardPresence.limitedName] {
            CFNotificationCenterAddObserver(
                center,
                observer,
                { _, observer, name, _, _ in
                    guard let observer, let name else { return }
                    let hasFullAccess = name.rawValue as String == KeyboardPresence.fullAccessName
                    let monitor = Unmanaged<KeyboardStatusMonitor>.fromOpaque(observer).takeUnretainedValue()
                    Task { @MainActor in monitor.keyboardDidAppear(hasFullAccess: hasFullAccess) }
                },
                name as CFString,
                nil,
                .deliverImmediately
            )
        }
    }
}
