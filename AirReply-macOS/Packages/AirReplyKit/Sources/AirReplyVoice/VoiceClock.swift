import Foundation

/// Time for the recording timer; tests drive a manual one.
@MainActor
protocol VoiceClock: AnyObject {
    /// Monotonic seconds.
    var now: TimeInterval { get }
    /// Calls `action` every `interval` until the returned ticker is cancelled or released.
    func every(_ interval: TimeInterval, _ action: @escaping @MainActor @Sendable () -> Void) -> any VoiceTicker
}

@MainActor
protocol VoiceTicker: AnyObject {
    func cancel()
}

/// System uptime and a main-actor sleep loop. Uptime stands still during sleep, but recording
/// is cancelled before the Mac sleeps anyway.
@MainActor
final class SystemVoiceClock: VoiceClock {
    var now: TimeInterval {
        ProcessInfo.processInfo.systemUptime
    }

    func every(_ interval: TimeInterval, _ action: @escaping @MainActor @Sendable () -> Void) -> any VoiceTicker {
        TaskTicker(Task {
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(interval))
                if Task.isCancelled { return }
                action()
            }
        })
    }
}

@MainActor
private final class TaskTicker: VoiceTicker {
    private let task: Task<Void, Never>

    init(_ task: Task<Void, Never>) {
        self.task = task
    }

    deinit {
        task.cancel()
    }

    func cancel() {
        task.cancel()
    }
}
