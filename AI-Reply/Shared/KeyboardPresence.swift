import Foundation

/// The signal the keyboard sends the containing app every time it appears.
///
/// Пернетақта ашылған сайын қолданбаға белгі жібереді: ішінде мәтін жоқ, тек
/// «ашылдым» және толық рұқсат бар-жоғы.
///
/// A Darwin notification carries no payload and needs no shared container, so
/// it gets through even when the keyboard runs without Full Access - exactly
/// the case where the App Group heartbeat (`SharedSettings.markKeyboardActive`)
/// cannot be written. The app hears it only while it is running, so it is what
/// turns the status green at once, never the only evidence it relies on.
enum KeyboardPresence {

    static let fullAccessName = "kz.ai-reply.keyboard.appeared.full"
    static let limitedName = "kz.ai-reply.keyboard.appeared.limited"

    static func name(hasFullAccess: Bool) -> String {
        hasFullAccess ? fullAccessName : limitedName
    }

    /// Cheap and fire-and-forget: if the sandbox drops it, nothing breaks.
    static func post(hasFullAccess: Bool) {
        CFNotificationCenterPostNotification(
            CFNotificationCenterGetDarwinNotifyCenter(),
            CFNotificationName(name(hasFullAccess: hasFullAccess) as CFString),
            nil,
            nil,
            true
        )
    }
}
