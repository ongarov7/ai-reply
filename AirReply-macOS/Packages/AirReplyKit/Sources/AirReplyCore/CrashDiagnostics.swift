import Foundation
import MetricKit

/// Crash summaries for the operator's alert channel.
///
/// The app has no crash SDK. macOS hands it the crash diagnostics of earlier
/// runs on a later launch (MetricKit); each crash is reduced to its exception
/// type and signal and sent to `POST /api/v1/client-errors`, where the server
/// turns it into a Telegram alert. No call stack, no clipboard or message
/// text, no token: the request is anonymous.
public final class CrashDiagnostics: NSObject, MXMetricManagerSubscriber, @unchecked Sendable {

    private let client: APIClient
    private let lock = NSLock()
    private var started = false

    public init(client: APIClient) {
        self.client = client
    }

    public func start() {
        lock.lock()
        defer { lock.unlock() }
        guard !started else { return }
        started = true
        MXMetricManager.shared.add(self)
    }

    public func didReceive(_ payloads: [MXDiagnosticPayload]) {
        let reports = payloads.flatMap { $0.crashDiagnostics ?? [] }.map {
            CrashReport(exceptionType: $0.exceptionType?.intValue, signal: $0.signal?.intValue,
                        appVersion: $0.metaData.applicationBuildVersion, osVersion: $0.metaData.osVersion)
        }
        guard !reports.isEmpty else { return }
        let client = self.client
        Task.detached(priority: .utility) {
            for report in CrashReport.grouped(reports) {
                _ = try? await client.send(.json("POST", "/api/v1/client-errors", report.body, authenticated: false))
            }
        }
    }
}

/// One crash as the server's client-errors endpoint takes it.
public struct CrashReport: Equatable, Sendable {
    public let exceptionType: Int?
    public let signal: Int?
    public let appVersion: String
    public let osVersion: String
    public var count = 1

    public init(exceptionType: Int?, signal: Int?, appVersion: String, osVersion: String, count: Int = 1) {
        self.exceptionType = exceptionType
        self.signal = signal
        self.appVersion = appVersion
        self.osVersion = osVersion
        self.count = count
    }

    /// "EXC_BAD_ACCESS:SIGSEGV", "EXC_BREAKPOINT:SIGTRAP" (a Swift trap), "EXC_CRASH:SIGABRT".
    public var code: String {
        let exception: String
        switch exceptionType {
        case 1: exception = "EXC_BAD_ACCESS"
        case 2: exception = "EXC_BAD_INSTRUCTION"
        case 3: exception = "EXC_ARITHMETIC"
        case 5: exception = "EXC_SOFTWARE"
        case 6: exception = "EXC_BREAKPOINT"
        case 10: exception = "EXC_CRASH"
        case 11: exception = "EXC_RESOURCE"
        case 12: exception = "EXC_GUARD"
        case let other?: exception = "EXC_\(other)"
        case nil: exception = "EXC_UNKNOWN"
        }
        let name: String
        switch signal {
        case 4: name = "SIGILL"
        case 5: name = "SIGTRAP"
        case 6: name = "SIGABRT"
        case 8: name = "SIGFPE"
        case 9: name = "SIGKILL"
        case 10: name = "SIGBUS"
        case 11: name = "SIGSEGV"
        case let other?: name = "SIG\(other)"
        case nil: return exception
        }
        return exception + ":" + name
    }

    public var body: ClientErrorBody {
        ClientErrorBody(code: code, appVersion: Self.clean(appVersion), osVersion: Self.clean(osVersion),
                        count: min(max(count, 1), 10_000))
    }

    /// The same crash of the same build is one report with a count.
    public static func grouped(_ reports: [CrashReport]) -> [CrashReport] {
        var out: [CrashReport] = []
        for report in reports {
            if let index = out.firstIndex(where: { $0.code == report.code && $0.appVersion == report.appVersion }) {
                out[index].count += report.count
            } else {
                out.append(report)
            }
        }
        return out
    }

    /// Only the characters the server accepts in a version, at most 40.
    private static func clean(_ value: String) -> String {
        let allowed = CharacterSet.alphanumerics.union(CharacterSet(charactersIn: " ._()+-"))
        return String(String(value.unicodeScalars.filter { allowed.contains($0) && $0.isASCII }).prefix(40))
    }
}

public struct ClientErrorBody: Encodable, Sendable, Equatable {
    public var platform = "macos"
    public var kind = "crash"
    public let code: String
    public let appVersion: String
    public let osVersion: String
    public let count: Int

    enum CodingKeys: String, CodingKey {
        case platform, kind, code, count
        case appVersion = "app_version"
        case osVersion = "os_version"
    }
}
