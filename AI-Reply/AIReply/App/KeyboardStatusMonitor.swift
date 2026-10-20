import Foundation
import Observation

/// A permission's state as the diagnostics API takes it (contract §3.4).
///
/// Рұқсат күйі: тек дәлел бойынша, «қазір қосулы» деп ешқашан айтпайды.
///
/// Every value is what something was SEEN to be, never what it is now: iOS
/// tells an app nothing about its own keyboard, so the app only knows what
/// the keyboard reported, and when.
enum PermissionEvidence: String, Codable, CaseIterable, Sendable {
    /// Nothing was ever seen either way.
    case unknown
    /// It could have been seen and was not.
    case notVerified = "not_verified"
    case verifiedEnabled = "verified_enabled"
    case observedDenied = "observed_denied"
    /// The user said so; never derived on iOS.
    case userReportedDisabled = "user_reported_disabled"
    /// The platform has no way to tell.
    case unsupported
}

/// What the app can honestly say about its keyboard: dated evidence.
///
/// Everything here is evidence the keyboard left itself - the App Group
/// heartbeat and paste stamps (written only with Full Access, because
/// without it the shared container is read-only to the keyboard) and the
/// Darwin notification it posts on every appearance (`KeyboardPresence`),
/// which the app hears only while it runs. Nothing is a flag that stays on:
/// a newer sighting of the keyboard without Full Access wins over an older
/// one with it, and a status says when it was seen, never that it still is.
struct KeyboardStatus: Equatable {

    /// The last time the keyboard was seen at all.
    let keyboardLastSeenAt: Date?
    /// The last time it was seen running WITH Full Access.
    let fullAccessVerifiedAt: Date?
    /// The last time it was seen running WITHOUT Full Access.
    let limitedSeenAt: Date?
    /// The last paste the keyboard managed on the user's tap.
    let pasteSucceededAt: Date?
    /// The last paste iOS refused it.
    let pasteDeniedAt: Date?

    /// How long a sighting counts as current. Past it, the screens say when
    /// the keyboard was last seen instead of showing a green tick, and the
    /// server treats the evidence as stale (reminders use 14 days too).
    static let freshness: TimeInterval = 14 * 24 * 60 * 60

    static let none = KeyboardStatus(keyboardLastSeenAt: nil, fullAccessVerifiedAt: nil, limitedSeenAt: nil,
                                     pasteSucceededAt: nil, pasteDeniedAt: nil)

    /// - Parameters:
    ///   - heartbeatAt: the keyboard's last App Group heartbeat.
    ///   - heartbeatFullAccess: the flag written with it. The current
    ///     keyboard writes the heartbeat only with Full Access; `false` comes
    ///     from an older build and counts as a sighting without it.
    ///   - fullAccessSignalAt / limitedSignalAt: the last Darwin notification
    ///     of each kind the app heard (only the app can store these).
    ///   - pasteSucceededAt / pasteDeniedAt: the keyboard's paste stamps. A
    ///     paste is read only with Full Access, so either one is also a
    ///     sighting with it.
    static func resolve(
        heartbeatAt: Date?,
        heartbeatFullAccess: Bool,
        fullAccessSignalAt: Date?,
        limitedSignalAt: Date?,
        pasteSucceededAt: Date?,
        pasteDeniedAt: Date?
    ) -> KeyboardStatus {
        let verified = latest(heartbeatFullAccess ? heartbeatAt : nil, fullAccessSignalAt, pasteSucceededAt, pasteDeniedAt)
        let limited = latest(heartbeatFullAccess ? nil : heartbeatAt, limitedSignalAt)
        return KeyboardStatus(
            keyboardLastSeenAt: latest(verified, limited),
            fullAccessVerifiedAt: verified,
            limitedSeenAt: limited,
            pasteSucceededAt: pasteSucceededAt,
            pasteDeniedAt: pasteDeniedAt
        )
    }

    // MARK: Diagnostics (contract §3.4)

    /// `verified_enabled` once the keyboard has reported itself at all.
    var keyboardSetupStatus: PermissionEvidence {
        keyboardLastSeenAt == nil ? .notVerified : .verifiedEnabled
    }

    /// iOS never tells which keyboard is in use.
    var keyboardSelectedStatus: PermissionEvidence { .unsupported }

    /// Whichever was seen last: the keyboard with Full Access, or without it.
    var fullAccessStatus: PermissionEvidence {
        switch (fullAccessVerifiedAt, limitedSeenAt) {
        case (nil, nil):
            return .notVerified
        case (.some, nil):
            return .verifiedEnabled
        case (nil, .some):
            return .observedDenied
        case let (verified?, limited?):
            return limited > verified ? .observedDenied : .verifiedEnabled
        }
    }

    /// The last paste decides: a refusal after a success means it is refused
    /// now, a success after a refusal that it was allowed again.
    var clipboardStatus: PermissionEvidence {
        switch (pasteSucceededAt, pasteDeniedAt) {
        case (nil, nil):
            return .unknown
        case (.some, nil):
            return .verifiedEnabled
        case (nil, .some):
            return .observedDenied
        case let (succeeded?, denied?):
            return denied > succeeded ? .observedDenied : .verifiedEnabled
        }
    }

    var lastKeyboardCheckAt: Date? { keyboardLastSeenAt }
    var lastSuccessfulPasteAt: Date? { pasteSucceededAt }

    /// No sighting at all, or none within `freshness`: whatever the statuses
    /// say is history, and is shown as "last seen …".
    func isStale(now: Date = Date()) -> Bool {
        guard let last = lastKeyboardCheckAt else { return true }
        return now.timeIntervalSince(last) > Self.freshness
    }

    /// The keyboard has reported itself at least once.
    var isEnabled: Bool { keyboardSetupStatus == .verifiedEnabled }

    private static func latest(_ dates: Date?...) -> Date? {
        dates.compactMap { $0 }.max()
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

    /// App-only: the Darwin notifications carry nothing and the keyboard
    /// cannot write the App Group without Full Access, so the app keeps when
    /// it heard each one.
    private enum Key {
        static let limitedSeenAt = "keyboard.limitedSeenAt"
        static let fullAccessSeenAt = "keyboard.fullAccessSeenAt"
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
        if status.fullAccessStatus == .verifiedEnabled { ProductEvents.trackOnce(.fullAccessEnabledDetected) }
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
    /// Access. Both are remembered with the time: the heartbeat behind "with"
    /// is throttled, and "without" could not be written by the keyboard at all.
    func keyboardDidAppear(hasFullAccess: Bool, now: Date = Date()) {
        defaults.set(now.timeIntervalSince1970, forKey: hasFullAccess ? Key.fullAccessSeenAt : Key.limitedSeenAt)
        refresh()
    }

    private static func read(settings: SharedSettings, defaults: UserDefaults) -> KeyboardStatus {
        KeyboardStatus.resolve(
            heartbeatAt: settings.keyboardLastSeen,
            heartbeatFullAccess: settings.keyboardHasFullAccess,
            fullAccessSignalAt: date(defaults, Key.fullAccessSeenAt),
            limitedSignalAt: date(defaults, Key.limitedSeenAt),
            pasteSucceededAt: settings.keyboardPasteSucceededAt,
            pasteDeniedAt: settings.keyboardPasteDeniedAt
        )
    }

    private static func date(_ defaults: UserDefaults, _ key: String) -> Date? {
        let stamp = defaults.double(forKey: key)
        return stamp > 0 ? Date(timeIntervalSince1970: stamp) : nil
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
