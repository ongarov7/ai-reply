import XCTest
@testable import AIReply

final class CrashDiagnosticsTests: XCTestCase {

    func testCodeNamesTheExceptionAndSignal() {
        XCTAssertEqual(CrashReport(exceptionType: 1, signal: 11, appVersion: "16", osVersion: "iPhone OS 18.1").code,
                       "EXC_BAD_ACCESS:SIGSEGV")
        XCTAssertEqual(CrashReport(exceptionType: 6, signal: 5, appVersion: "16", osVersion: "").code, "EXC_BREAKPOINT:SIGTRAP")
        XCTAssertEqual(CrashReport(exceptionType: nil, signal: nil, appVersion: "16", osVersion: "").code, "EXC_UNKNOWN")
        XCTAssertEqual(CrashReport(exceptionType: 99, signal: 31, appVersion: "16", osVersion: "").code, "EXC_99:SIG31")
    }

    func testTheSameCrashOfTheSameBuildIsOneReportWithACount() {
        let crash = CrashReport(exceptionType: 1, signal: 11, appVersion: "16", osVersion: "iPhone OS 18.1 (22B83)")
        let other = CrashReport(exceptionType: 10, signal: 6, appVersion: "16", osVersion: "iPhone OS 18.1 (22B83)")
        let grouped = CrashReport.grouped([crash, other, crash])
        XCTAssertEqual(grouped.count, 2)
        XCTAssertEqual(grouped.first?.count, 2)
    }

    func testTheBodyMatchesTheServerContract() throws {
        let report = CrashReport(exceptionType: 1, signal: 11, appVersion: "16", osVersion: "iPhone OS 18.1 (22B83)\u{1F600}")
        let json = try XCTUnwrap(JSONSerialization.jsonObject(with: JSONEncoder().encode(report.body)) as? [String: Any])
        XCTAssertEqual(json["platform"] as? String, "ios")
        XCTAssertEqual(json["kind"] as? String, "crash")
        XCTAssertEqual(json["code"] as? String, "EXC_BAD_ACCESS:SIGSEGV")
        XCTAssertEqual(json["app_version"] as? String, "16")
        XCTAssertEqual(json["os_version"] as? String, "iPhone OS 18.1 (22B83)")
        XCTAssertEqual(json["count"] as? Int, 1)
        XCTAssertEqual(Set(json.keys), ["platform", "kind", "code", "app_version", "os_version", "count"])
    }
}
