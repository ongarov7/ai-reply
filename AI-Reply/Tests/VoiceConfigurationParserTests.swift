import XCTest
import AVFoundation
import Speech
import UIKit
@testable import AIReply

/// The dictation examples from the brief, in all three languages.
///
/// These are regression tests for a feature that is allowed to be imperfect but
/// is never allowed to be WRONG: a suggestion the user can decline is fine, a
/// schedule silently set to the wrong hours is not.
final class VoiceConfigurationParserTests: XCTestCase {

    func testEnglishWordHours() {
        let result = VoiceConfigurationParser.parse(
            "I sell clothes. We work from ten to six. If stock is unknown, don't tell the customer that it's available.",
            language: .english
        )
        XCTAssertEqual(result.start?.formatted, "10:00")
        XCTAssertEqual(result.end?.formatted, "18:00")
        XCTAssertEqual(result.rules.count, 1)
    }

    func testRussianWordHoursWithMeridiem() {
        let result = VoiceConfigurationParser.parse(
            "Работаем каждый день с десяти утра до шести вечера.",
            language: .russian
        )
        XCTAssertEqual(result.start?.formatted, "10:00")
        XCTAssertEqual(result.end?.formatted, "18:00")
        XCTAssertEqual(result.weekdays, [1, 2, 3, 4, 5, 6, 7])
    }

    /// The Kazakh case-ending form, which is what a speaker actually says.
    func testKazakhDigitHoursWithCaseEndings() {
        let result = VoiceConfigurationParser.parse(
            "Мен киім сатамын. Жұмыс уақыты 10:00-ден 18:00-ге дейін.",
            language: .kazakh
        )
        XCTAssertEqual(result.start?.formatted, "10:00")
        XCTAssertEqual(result.end?.formatted, "18:00")
    }

    func testRussianExplicitDayRange() {
        let result = VoiceConfigurationParser.parse(
            "Работаем с понедельника по субботу с 10:00 до 18:00",
            language: .russian
        )
        XCTAssertEqual(result.weekdays, [2, 3, 4, 5, 6, 7])
        XCTAssertEqual(result.start?.formatted, "10:00")
    }

    func testKazakhRuleIsExtracted() {
        let result = VoiceConfigurationParser.parse(
            "Тауар бар-жоғын нақты білмесең, бар деп айтпа.",
            language: .kazakh
        )
        XCTAssertEqual(result.rules.count, 1)
        XCTAssertTrue(result.rules[0].contains("айтпа"))
    }

    /// A sentence with no schedule in it must not invent one.
    func testNoFalsePositives() {
        let result = VoiceConfigurationParser.parse(
            "I run a design studio and I like short replies",
            language: .english
        )
        XCTAssertNil(result.start)
        XCTAssertNil(result.end)
        XCTAssertNil(result.weekdays)
        XCTAssertTrue(result.isEmpty)
    }

    func testSuggestionBuildsScheduleOnlyForNamedDays() {
        let parsed = VoiceConfigurationParser.parse(
            "Работаем с понедельника по пятницу с 9:00 до 17:00",
            language: .russian
        )
        let hours = parsed.workingHours(basedOn: .default)
        XCTAssertEqual(hours?.isEnabled, true)
        XCTAssertEqual(hours?.schedule(for: 2)?.isEnabled, true)   // Monday
        XCTAssertEqual(hours?.schedule(for: 7)?.isEnabled, false)  // Saturday
        XCTAssertEqual(hours?.schedule(for: 2)?.start.formatted, "09:00")
        XCTAssertEqual(hours?.schedule(for: 2)?.end.formatted, "17:00")
    }

    /// An impossible range is dropped rather than stored inverted.
    func testInvertedRangeIsRejected() {
        let result = VoiceConfigurationParser.parse("с 18:00 до 18:00", language: .russian)
        XCTAssertNil(result.start)
        XCTAssertNil(result.end)
    }
}

/// Session ownership and cancelled system prompts must be verifiable without
/// opening a real microphone or changing the test runner's audio session.
@MainActor
final class DictationAudioLifecycleTests: XCTestCase {
    private enum Failure: Error { case unavailable }

    @MainActor
    private final class SessionProbe {
        var current = DictationAudioSession.Configuration(category: .ambient, mode: .default, options: [.mixWithOthers])
        var events: [String] = []
        var activationOptions: AVAudioSession.SetActiveOptions?
        var deactivationOptions: AVAudioSession.SetActiveOptions?
        var failActivation = false
        var failDeactivation = false

        lazy var session = DictationAudioSession(configuration: { self.current }, setCategory: {
            self.current = $0
            self.events.append($0.category == .record ? "record" : "restore")
        }, setActive: { active, options in
            self.events.append(active ? "activate" : "deactivate")
            if active {
                self.activationOptions = options
                if self.failActivation { throw Failure.unavailable }
            } else {
                self.deactivationOptions = options
                if self.failDeactivation { throw Failure.unavailable }
            }
        })
    }

    func testRecordingUsesSupportedOptionsThenReleasesAndRestoresPreviousSession() throws {
        let probe = SessionProbe()
        try probe.session.begin()
        XCTAssertEqual(probe.current.category, .record)
        XCTAssertEqual(probe.current.mode, .measurement)
        XCTAssertEqual(probe.current.options, [])
        XCTAssertEqual(probe.activationOptions, [])

        try probe.session.end()
        XCTAssertEqual(probe.events, ["record", "activate", "deactivate", "restore"])
        XCTAssertEqual(probe.deactivationOptions, .notifyOthersOnDeactivation)
        XCTAssertEqual(probe.current.category, .ambient)
        XCTAssertEqual(probe.current.mode, .default)
        XCTAssertEqual(probe.current.options, [.mixWithOthers])

        try probe.session.end()
        XCTAssertEqual(probe.events.count, 4, "Repeated stop must not deactivate another player's session")
    }

    func testFailedActivationRestoresTheConfiguration() {
        let probe = SessionProbe()
        probe.failActivation = true
        XCTAssertThrowsError(try probe.session.begin())
        XCTAssertEqual(probe.events, ["record", "activate", "deactivate", "restore"])
        XCTAssertEqual(probe.current.category, .ambient)
    }

    func testFailedDeactivationKeepsOwnershipForRetry() throws {
        let probe = SessionProbe()
        try probe.session.begin()
        probe.failDeactivation = true
        XCTAssertThrowsError(try probe.session.end())
        XCTAssertEqual(probe.current.category, .record)
        probe.failDeactivation = false
        try probe.session.end()
        XCTAssertEqual(probe.events, ["record", "activate", "deactivate", "deactivate", "restore"])
        XCTAssertEqual(probe.current.category, .ambient)
    }

    func testStoppingIdleModelDoesNotTouchAnotherAudioSession() {
        let probe = SessionProbe()
        let model = VoiceReplyViewModel(recordingSession: probe.session, notificationCenter: NotificationCenter())
        model.stopRecording()
        model.stopRecording()
        XCTAssertTrue(probe.events.isEmpty)
    }

    func testImmediateDismissalBeforeStartupTaskRunsDoesNotRequestPermissions() async {
        let probe = SessionProbe()
        var microphoneRequests = 0
        let model = VoiceReplyViewModel(recordingSession: probe.session, microphonePermission: {
            microphoneRequests += 1
            return false
        }, notificationCenter: NotificationCenter())
        model.startRecording()
        model.stopRecording()
        for _ in 0..<10 { await Task.yield() }
        XCTAssertEqual(microphoneRequests, 0)
        XCTAssertEqual(model.status, .stoppedNoSpeech)
        XCTAssertTrue(probe.events.isEmpty)
    }

    func testStopWhileMicrophonePromptIsPendingCannotStartCaptureAfterDismissal() async throws {
        let probe = SessionProbe()
        var pending: CheckedContinuation<Bool, Never>?
        var speechRequests = 0
        let model = VoiceReplyViewModel(recordingSession: probe.session, microphonePermission: {
            await withCheckedContinuation { pending = $0 }
        }, speechPermission: {
            speechRequests += 1
            return .denied
        }, notificationCenter: NotificationCenter())
        model.startRecording()
        for _ in 0..<100 where pending == nil { await Task.yield() }
        let prompt = try XCTUnwrap(pending)
        model.stopRecording()
        prompt.resume(returning: true)
        for _ in 0..<10 { await Task.yield() }
        XCTAssertEqual(speechRequests, 0)
        XCTAssertEqual(model.status, .stoppedNoSpeech)
        XCTAssertFalse(model.isRecording)
        XCTAssertTrue(probe.events.isEmpty)
    }

    func testStopWhileSpeechPromptIsPendingIgnoresItsLateResult() async throws {
        let probe = SessionProbe()
        var pending: CheckedContinuation<SFSpeechRecognizerAuthorizationStatus, Never>?
        let model = VoiceReplyViewModel(recordingSession: probe.session, microphonePermission: { true }, speechPermission: {
            await withCheckedContinuation { pending = $0 }
        }, notificationCenter: NotificationCenter())
        model.startRecording()
        for _ in 0..<100 where pending == nil { await Task.yield() }
        let prompt = try XCTUnwrap(pending)
        model.stopRecording()
        prompt.resume(returning: .denied)
        for _ in 0..<10 { await Task.yield() }
        XCTAssertEqual(model.status, .stoppedNoSpeech)
        XCTAssertFalse(model.isRecording)
        XCTAssertTrue(probe.events.isEmpty)
    }

    func testRepeatedStartDuringPermissionPromptCreatesOnlyOneRequest() async throws {
        var requests = 0
        var pending: CheckedContinuation<Bool, Never>?
        let model = VoiceReplyViewModel(microphonePermission: {
            requests += 1
            return await withCheckedContinuation { pending = $0 }
        }, notificationCenter: NotificationCenter())
        model.startRecording()
        model.startRecording()
        for _ in 0..<100 where pending == nil { await Task.yield() }
        let prompt = try XCTUnwrap(pending)
        XCTAssertEqual(requests, 1)
        model.stopRecording()
        prompt.resume(returning: false)
        for _ in 0..<10 { await Task.yield() }
        XCTAssertEqual(model.status, .stoppedNoSpeech)
    }

    func testBackgroundAndInterruptionCancelPendingCapture() async throws {
        for notification in [
            Notification(name: UIApplication.didEnterBackgroundNotification),
            Notification(name: AVAudioSession.interruptionNotification,
                         userInfo: [AVAudioSessionInterruptionTypeKey: AVAudioSession.InterruptionType.began.rawValue])
        ] {
            let center = NotificationCenter()
            let probe = SessionProbe()
            var pending: CheckedContinuation<Bool, Never>?
            var speechRequests = 0
            let model = VoiceReplyViewModel(recordingSession: probe.session, microphonePermission: {
                await withCheckedContinuation { pending = $0 }
            }, speechPermission: {
                speechRequests += 1
                return .denied
            }, notificationCenter: center)
            model.startRecording()
            for _ in 0..<100 where pending == nil { await Task.yield() }
            let prompt = try XCTUnwrap(pending)
            center.post(notification)
            for _ in 0..<100 where model.status != .stoppedNoSpeech { await Task.yield() }
            XCTAssertEqual(model.status, .stoppedNoSpeech)
            prompt.resume(returning: true)
            for _ in 0..<10 { await Task.yield() }
            XCTAssertEqual(speechRequests, 0)
            XCTAssertTrue(probe.events.isEmpty)
        }
    }
}
