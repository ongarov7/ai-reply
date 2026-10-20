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
        /// The whole balance (plan + pass + welcome + credits); absent from
        /// an older server.
        static let remainingTotal = "account.usage.remainingTotal"
        static let welcomeRemaining = "account.usage.welcomeRemaining"
        static let welcomeTotal = "account.usage.welcomeTotal"
        static let credits = "account.usage.credits"
        /// The last limit refusal and when it came.
        static let limitRefusal = "account.limit.refusal"
        static let limitRefusedAt = "account.limit.refusedAt"

        static let all = [dailyLimit, usedToday, remaining, resetsAt, planName, updatedAt,
                          remainingTotal, welcomeRemaining, welcomeTotal, credits, limitRefusal, limitRefusedAt]
    }

    /// A snapshot as the UI wants it.
    struct Snapshot: Equatable, Sendable {
        var dailyLimit: Int
        var usedToday: Int
        var remainingToday: Int
        var resetsAt: String
        var planCode: String
        var updatedAt: Date?
        /// Everything usable now, where the server sends the balance.
        var remainingTotal: Int? = nil
        var welcomeRemaining: Int? = nil
        var welcomeTotal: Int? = nil
        var credits: Int? = nil

        var isKnown: Bool { dailyLimit > 0 || remainingTotal != nil }
        /// Nothing at all is left: the plan's day, and - where the server
        /// sends them - the free, purchased and pass replies too.
        var isExhausted: Bool {
            if let remainingTotal { return remainingTotal <= 0 }
            return dailyLimit > 0 && remainingToday <= 0
        }
    }

    static func store(_ usage: AccountAPI.Usage, defaults: UserDefaults = AppGroup.defaults) {
        defaults.set(usage.dailyLimit, forKey: Key.dailyLimit)
        defaults.set(usage.usedToday, forKey: Key.usedToday)
        defaults.set(usage.remainingToday, forKey: Key.remaining)
        defaults.set(usage.resetsAt, forKey: Key.resetsAt)
        defaults.set(Date().timeIntervalSince1970, forKey: Key.updatedAt)
        store(usage.remainingTotal, forKey: Key.remainingTotal, defaults: defaults)
        store(usage.welcome?.remaining, forKey: Key.welcomeRemaining, defaults: defaults)
        store(usage.welcome?.total, forKey: Key.welcomeTotal, defaults: defaults)
        store(usage.credits, forKey: Key.credits, defaults: defaults)
    }

    /// A value the server did not send is removed, not left from before.
    private static func store(_ value: Int?, forKey key: String, defaults: UserDefaults) {
        if let value {
            defaults.set(value, forKey: key)
        } else {
            defaults.removeObject(forKey: key)
        }
    }

    private static func integer(_ key: String, _ defaults: UserDefaults) -> Int? {
        (defaults.object(forKey: key) as? NSNumber)?.intValue
    }

    /// A generation was refused for a spent limit: what the server said
    /// about it, so the screen that asked can offer what fits.
    static func storeLimitRefusal(_ refusal: LimitRefusal, now: Date = Date(), defaults: UserDefaults = AppGroup.defaults) {
        guard let data = try? JSONEncoder().encode(refusal) else { return }
        defaults.set(data, forKey: Key.limitRefusal)
        defaults.set(now.timeIntervalSince1970, forKey: Key.limitRefusedAt)
    }

    /// The last limit refusal, if it came within `maxAge`: the answer to the
    /// request that just failed, not one from yesterday.
    static func lastLimitRefusal(maxAge: TimeInterval = 120, now: Date = Date(),
                                 defaults: UserDefaults = AppGroup.defaults) -> LimitRefusal? {
        let at = defaults.double(forKey: Key.limitRefusedAt)
        guard at > 0, now.timeIntervalSince1970 - at <= maxAge, now.timeIntervalSince1970 >= at - 5,
              let data = defaults.data(forKey: Key.limitRefusal) else { return nil }
        return try? JSONDecoder().decode(LimitRefusal.self, from: data)
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
            updatedAt: updated > 0 ? Date(timeIntervalSince1970: updated) : nil,
            remainingTotal: integer(Key.remainingTotal, defaults),
            welcomeRemaining: integer(Key.welcomeRemaining, defaults),
            welcomeTotal: integer(Key.welcomeTotal, defaults),
            credits: integer(Key.credits, defaults)
        )
    }

    static func clear(defaults: UserDefaults = AppGroup.defaults) {
        for key in Key.all {
            defaults.removeObject(forKey: key)
        }
    }
}
