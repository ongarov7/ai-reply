import Foundation

/// Block observers that are removed when this object goes away, so their owner needs no deinit.
final class NotificationObservers {
    private let registrations: [(center: NotificationCenter, token: any NSObjectProtocol)]

    init(_ registrations: [(center: NotificationCenter, token: any NSObjectProtocol)]) {
        self.registrations = registrations
    }

    deinit {
        for registration in registrations {
            registration.center.removeObserver(registration.token)
        }
    }
}

/// Runs `body` on the main actor: right away when called on the main thread, otherwise on its next turn.
func performOnMain(_ body: @escaping @MainActor @Sendable () -> Void) {
    if Thread.isMainThread {
        MainActor.assumeIsolated(body)
    } else {
        DispatchQueue.main.async {
            MainActor.assumeIsolated(body)
        }
    }
}
