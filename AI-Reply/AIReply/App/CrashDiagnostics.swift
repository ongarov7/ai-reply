import Foundation
import MetricKit

/// Crash summaries for the operator's alert channel.
///
/// The app has no crash SDK. iOS hands it the crash diagnostics of earlier
/// runs on a later launch (MetricKit, at most once a day); each crash is
/// reduced to its exception type and signal and sent to
/// `POST /api/v1/client-errors`, where the server turns it into a Telegram
/// alert. No call stack, no user text, no token: the request is anonymous.
final class CrashDiagnostics: NSObject, MXMetricManagerSubscriber {

    static let shared = CrashDiagnostics()

    private var started = false

    /// Called once at launch; MetricKit delivers on a background queue later.
    func start() {
        guard !started else { return }
        started = true
        MXMetricManager.shared.add(self)
    }

    func didReceive(_ payloads: [MXDiagnosticPayload]) {
        let reports = payloads.flatMap { $0.crashDiagnostics ?? [] }.map {
            CrashReport(exceptionType: $0.exceptionType?.intValue, signal: $0.signal?.intValue,
                        appVersion: $0.metaData.applicationBuildVersion, osVersion: $0.metaData.osVersion)
        }
        guard !reports.isEmpty, let baseURL = AIConfiguration.shared.backendBaseURL else { return }
        let client = APIClient(baseURL: baseURL)
        Task.detached(priority: .utility) {
            for report in CrashReport.grouped(reports) {
                let _: APIClient.Empty? = try? await client.post("/api/v1/client-errors", body: report.body)
            }
        }
    }
}

/// One crash as the server's client-errors endpoint takes it.
struct CrashReport: Equatable, Sendable {
    let exceptionType: Int?
    let signal: Int?
    let appVersion: String
    let osVersion: String
    var count = 1

    /// "EXC_BAD_ACCESS:SIGSEGV", "EXC_BREAKPOINT:SIGTRAP" (a Swift trap), "EXC_CRASH:SIGABRT".
    var code: String {
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

    var body: ClientErrorBody {
        ClientErrorBody(code: code, appVersion: Self.clean(appVersion), osVersion: Self.clean(osVersion),
                        count: min(max(count, 1), 10_000))
    }

    /// The same crash of the same build is one report with a count.
    static func grouped(_ reports: [CrashReport]) -> [CrashReport] {
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

struct ClientErrorBody: Encodable, Sendable, Equatable {
    var platform = "ios"
    var kind = "crash"
    let code: String
    let appVersion: String
    let osVersion: String
    let count: Int

    enum CodingKeys: String, CodingKey {
        case platform, kind, code, count
        case appVersion = "app_version"
        case osVersion = "os_version"
    }
}
