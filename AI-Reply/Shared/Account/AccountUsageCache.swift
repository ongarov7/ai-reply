import Foundation

/// Last known quota, shared between the app and the keyboard.
///
/// Соңғы белгілі квота: көрсету үшін ғана, шешімді сервер қабылдайды.
///
/// This is a display cache and nothing more. The server enforces the limit; if
/// this value and the server ever disagree, the server is right. It exists so
/// the keyboard can show "2 left today" the instant it opens, instead of
/// blanking a label until a network call returns.
enum AccountUsageCache {

    private enum Key {
        static let dailyLimit = "account.usage.dailyLimit"
        static let usedToday = "account.usage.usedToday"
        static let remaining = "account.usage.remainingToday"
        static let resetsAt = "account.usage.resetsAt"
        static let planName = "account.usage.planCode"
        static let updatedAt = "account.usage.updatedAt"
    }

    /// A snapshot as the UI wants it.
    struct Snapshot: Equatable, Sendable {
        var dailyLimit: Int
        var usedToday: Int
        var remainingToday: Int
        var resetsAt: String
        var planCode: String
        var updatedAt: Date?

        var isKnown: Bool { dailyLimit > 0 }
        var isExhausted: Bool { dailyLimit > 0 && remainingToday <= 0 }
    }

    static func store(_ usage: AccountAPI.Usage, defaults: UserDefaults = AppGroup.defaults) {
        defaults.set(usage.dailyLimit, forKey: Key.dailyLimit)
        defaults.set(usage.usedToday, forKey: Key.usedToday)
        defaults.set(usage.remainingToday, forKey: Key.remaining)
        defaults.set(usage.resetsAt, forKey: Key.resetsAt)
        defaults.set(Date().timeIntervalSince1970, forKey: Key.updatedAt)
    }

    static func storePlanCode(_ code: String, defaults: UserDefaults = AppGroup.defaults) {
        defaults.set(code, forKey: Key.planName)
    }

    static var current: Snapshot { snapshot() }

    static func snapshot(defaults: UserDefaults = AppGroup.defaults) -> Snapshot {
        let updated = defaults.double(forKey: Key.updatedAt)
        return Snapshot(
            dailyLimit: defaults.integer(forKey: Key.dailyLimit),
            usedToday: defaults.integer(forKey: Key.usedToday),
            remainingToday: defaults.integer(forKey: Key.remaining),
            resetsAt: defaults.string(forKey: Key.resetsAt) ?? "",
            planCode: defaults.string(forKey: Key.planName) ?? "",
            updatedAt: updated > 0 ? Date(timeIntervalSince1970: updated) : nil
        )
    }

    static func clear(defaults: UserDefaults = AppGroup.defaults) {
        for key in [Key.dailyLimit, Key.usedToday, Key.remaining, Key.resetsAt, Key.planName, Key.updatedAt] {
            defaults.removeObject(forKey: key)
        }
    }
}
