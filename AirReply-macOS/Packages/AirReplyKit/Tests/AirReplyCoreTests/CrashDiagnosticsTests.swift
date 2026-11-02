import Foundation
import Testing
@testable import AirReplyCore

struct CrashDiagnosticsTests {

    @Test func codeNamesTheExceptionAndSignal() {
        #expect(CrashReport(exceptionType: 1, signal: 11, appVersion: "7", osVersion: "macOS 15.1").code == "EXC_BAD_ACCESS:SIGSEGV")
        #expect(CrashReport(exceptionType: 6, signal: 5, appVersion: "7", osVersion: "").code == "EXC_BREAKPOINT:SIGTRAP")
        #expect(CrashReport(exceptionType: nil, signal: nil, appVersion: "7", osVersion: "").code == "EXC_UNKNOWN")
    }

    @Test func theSameCrashOfTheSameBuildIsOneReport() {
        let crash = CrashReport(exceptionType: 1, signal: 11, appVersion: "7", osVersion: "macOS 15.1")
        let grouped = CrashReport.grouped([crash, crash, CrashReport(exceptionType: 10, signal: 6, appVersion: "7", osVersion: "")])
        #expect(grouped.count == 2)
        #expect(grouped.first?.count == 2)
    }

    @Test func theBodyMatchesTheServerContract() throws {
        let report = CrashReport(exceptionType: 1, signal: 11, appVersion: "7", osVersion: "macOS 15.1 (24B83)\u{1F600}")
        let json = try #require(JSONSerialization.jsonObject(with: JSONEncoder.api.encode(report.body)) as? [String: Any])
        #expect(json["platform"] as? String == "macos")
        #expect(json["kind"] as? String == "crash")
        #expect(json["code"] as? String == "EXC_BAD_ACCESS:SIGSEGV")
        #expect(json["os_version"] as? String == "macOS 15.1 (24B83)")
        #expect(Set(json.keys) == ["platform", "kind", "code", "app_version", "os_version", "count"])
    }
}
