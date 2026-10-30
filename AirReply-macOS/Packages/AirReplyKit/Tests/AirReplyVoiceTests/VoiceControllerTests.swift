import AppKit
import Foundation
import Testing
@testable import AirReplyVoice

@Suite("Voice controller")
@MainActor
struct VoiceControllerTests {
    // MARK: Permission

    @Test(arguments: [
        (MicrophoneStatus.denied, VoiceError.permissionDenied),
        (.restricted, .permissionRestricted),
    ])
    func refusedPermissionNeverOpensTheMicrophone(status: MicrophoneStatus, error: VoiceError) async {
        let h = Harness(permission: status)
        await h.controller.start(language: .ru)
        #expect(h.controller.phase == .failed(error))
        #expect(h.permission.requestCount == 0)
        #expect(h.capture.startCount == 0)
        #expect(h.recordings.isEmpty)
    }

    @Test(arguments: [
        (MicrophoneStatus.denied, VoiceError.permissionDenied),
        (.restricted, .permissionRestricted),
    ])
    func refusedPromptNeverOpensTheMicrophone(answer: MicrophoneStatus, error: VoiceError) async {
        let h = Harness(permission: .notDetermined, answer: answer)
        await h.controller.start(language: .ru)
        #expect(h.permission.requestCount == 1)
        #expect(h.controller.phase == .failed(error))
        #expect(h.capture.startCount == 0)
    }

    @Test func grantedPromptStartsRecording() async {
        let h = Harness(permission: .notDetermined, answer: .authorized)
        await h.controller.start(language: .en)
        #expect(h.permission.requestCount == 1)
        #expect(h.controller.phase == .recording)
        #expect(h.capture.isRunning)
        h.controller.cancel()
    }

    @Test func cancelWhileThePromptIsUpNeverOpensTheMicrophone() async throws {
        let h = Harness(permission: .notDetermined, answer: .authorized, holdsPrompt: true)
        let starting = Task { await h.controller.start(language: .auto) }
        for _ in 0..<1_000 where h.permission.requestCount == 0 { await Task.yield() }
        try #require(h.permission.requestCount == 1)
        #expect(h.controller.phase == .requestingPermission)
        h.controller.cancel()
        h.permission.answerPrompt()
        await starting.value
        #expect(h.controller.phase == .idle)
        #expect(h.capture.startCount == 0)
    }

    // MARK: Recording and transcription

    @Test func recordsThenTranscribesAndDeletesTheFile() async throws {
        let h = Harness(result: .success("  Привет, как дела?\n"), holdsTranscription: true)
        await h.controller.start(language: .kk)
        #expect(h.controller.phase == .recording)
        #expect(h.capture.isRunning)
        #expect(h.capture.limit == 40)
        let file = try #require(h.capture.fileURL)
        #expect(file.deletingLastPathComponent().path == h.directory.path)
        #expect(file.pathExtension == "m4a")

        h.clock.advance(by: 1.25)
        #expect(abs(h.controller.elapsed - 1.2) < 1e-9)

        let stopping = Task { await h.controller.stop() }
        await h.transcriber.waitForCall()
        #expect(h.controller.phase == .transcribing)
        #expect(!h.capture.isRunning)
        #expect(h.controller.elapsed == 1.25)
        await h.transcriber.release()
        await stopping.value

        #expect(h.controller.phase == .finished("Привет, как дела?"))
        let calls = await h.transcriber.calls
        try #require(calls.count == 1)
        #expect(calls[0].audio == RecordedAudio(fileURL: file, duration: 1.25, contentType: "audio/mp4", fileName: "voice.m4a"))
        #expect(calls[0].language == .kk)
        #expect(calls[0].fileExisted)
        #expect(!h.fileExists(file))
    }

    @Test func underHalfASecondIsTooShortAndSendsNothing() async throws {
        let h = Harness()
        await h.controller.start(language: .ru)
        let file = try #require(h.capture.fileURL)
        h.clock.advance(by: 0.4)
        await h.controller.stop()
        #expect(h.controller.phase == .failed(.tooShort))
        #expect(await h.transcriber.calls.isEmpty)
        #expect(!h.fileExists(file))
        #expect(!h.capture.isRunning)
    }

    @Test func deviceThatDeliversNoAudioIsAFailureNotTooShort() async {
        let h = Harness()
        await h.controller.start(language: .ru)
        h.capture.recordedDuration = 0
        h.clock.advance(by: 2)
        await h.controller.stop()
        #expect(h.controller.phase == .failed(.recordingFailed))
        #expect(await h.transcriber.calls.isEmpty)
        #expect(h.recordings.isEmpty)
    }

    @Test func stopsByItselfAtMaxDuration() async throws {
        let h = Harness(maxDuration: 3)
        await h.controller.start(language: .ru)
        #expect(h.capture.limit == 3)
        h.clock.advance(by: 2.95)
        #expect(h.controller.phase == .recording)
        #expect(h.capture.isRunning)

        h.clock.advance(by: 0.1)
        #expect(h.controller.phase == .transcribing)
        #expect(!h.capture.isRunning)
        #expect(h.controller.elapsed == 3)

        await h.controller.transcriptionTask?.value
        #expect(h.controller.phase == .finished("Привет"))
        #expect(await h.transcriber.calls.map(\.audio.duration) == [3])
        #expect(h.recordings.isEmpty)
    }

    @Test func stopAfterTheAutomaticStopWaitsForTheSameResult() async {
        let h = Harness(maxDuration: 1)
        await h.controller.start(language: .ru)
        h.clock.advance(by: 1.05)
        #expect(h.controller.phase == .transcribing)
        await h.controller.stop()
        #expect(h.controller.phase == .finished("Привет"))
        #expect(await h.transcriber.calls.count == 1)
    }

    @Test(arguments: [VoiceError.unavailable, .limitReached, .rateLimited, .offline, .server(code: "INVALID_AUDIO")])
    func transcriberErrorFailsAndDeletesTheFile(error: VoiceError) async throws {
        let h = Harness(result: .failure(error))
        await h.controller.start(language: .ru)
        let file = try #require(h.capture.fileURL)
        h.clock.advance(by: 1)
        await h.controller.stop()
        #expect(h.controller.phase == .failed(error))
        #expect(await h.transcriber.calls.count == 1)
        #expect(!h.fileExists(file))
    }

    // MARK: Cancel and reset

    @Test func cancelWhileRecordingDeletesTheFileAndSendsNothing() async throws {
        let h = Harness()
        await h.controller.start(language: .ru)
        let file = try #require(h.capture.fileURL)
        h.capture.send(level: 0.7)
        h.clock.advance(by: 2)
        h.controller.cancel()

        #expect(h.controller.phase == .idle)
        #expect(!h.capture.isRunning)
        #expect(!h.fileExists(file))
        #expect(h.controller.elapsed == 0)
        #expect(h.controller.levels == Array(repeating: 0, count: VoiceController.levelCount))

        h.capture.send(level: 0.8)
        h.clock.advance(by: 60)
        #expect(h.controller.phase == .idle)
        #expect(h.controller.levels.allSatisfy { $0 == 0 })
        #expect(await h.transcriber.calls.isEmpty)
    }

    @Test func cancelWhileTranscribingIgnoresTheLateResult() async throws {
        let h = Harness(holdsTranscription: true)
        await h.controller.start(language: .ru)
        let file = try #require(h.capture.fileURL)
        h.clock.advance(by: 1)
        let stopping = Task { await h.controller.stop() }
        await h.transcriber.waitForCall()

        h.controller.cancel()
        #expect(h.controller.phase == .idle)
        #expect(!h.fileExists(file))

        await h.transcriber.release()
        await stopping.value
        #expect(h.controller.phase == .idle)
    }

    @Test func resetAfterAFailureReturnsToIdle() async {
        let h = Harness(permission: .denied)
        await h.controller.start(language: .ru)
        #expect(h.controller.phase == .failed(.permissionDenied))
        h.controller.reset()
        #expect(h.controller.phase == .idle)
    }

    @Test func resetWhileRecordingClosesTheMicrophone() async {
        let h = Harness()
        await h.controller.start(language: .ru)
        h.clock.advance(by: 1)
        h.controller.reset()
        #expect(h.controller.phase == .idle)
        #expect(!h.capture.isRunning)
        #expect(h.recordings.isEmpty)
    }

    // MARK: Start rules

    @Test func startWhileBusyIsIgnored() async {
        let h = Harness(holdsTranscription: true)
        await h.controller.start(language: .ru)
        await h.controller.start(language: .en)
        #expect(h.capture.startCount == 1)
        #expect(h.controller.phase == .recording)

        h.clock.advance(by: 1)
        let stopping = Task { await h.controller.stop() }
        await h.transcriber.waitForCall()
        await h.controller.start(language: .en)
        #expect(h.capture.startCount == 1)
        #expect(h.controller.phase == .transcribing)

        await h.transcriber.release()
        await stopping.value
        #expect(await h.transcriber.calls.map(\.language) == [.ru])
    }

    @Test func startAfterAResultBeginsAFreshRecording() async {
        let h = Harness()
        await h.controller.start(language: .ru)
        h.capture.send(level: 0.9)
        h.clock.advance(by: 1)
        await h.controller.stop()
        #expect(h.controller.phase == .finished("Привет"))

        await h.controller.start(language: .ru)
        #expect(h.controller.phase == .recording)
        #expect(h.capture.startCount == 2)
        #expect(h.controller.elapsed == 0)
        #expect(h.controller.levels.allSatisfy { $0 == 0 })
        h.controller.cancel()
    }

    @Test(arguments: [VoiceError.noInputDevice, .recordingFailed])
    func captureThatCannotStartFailsAndLeavesNoFile(error: VoiceError) async {
        let h = Harness()
        h.capture.startError = error
        await h.controller.start(language: .ru)
        #expect(h.controller.phase == .failed(error))
        #expect(h.recordings.isEmpty)
        #expect(await h.transcriber.calls.isEmpty)
    }

    // MARK: Capture ending by itself

    @Test func deviceChangeFinishesAndTranscribesWhatWasSaid() async {
        let h = Harness()
        await h.controller.start(language: .ru)
        h.clock.advance(by: 2.5)
        h.capture.interrupt(.deviceChanged(inputAvailable: true))
        #expect(h.controller.phase == .transcribing)
        await h.controller.transcriptionTask?.value
        #expect(h.controller.phase == .finished("Привет"))
        #expect(await h.transcriber.calls.map(\.audio.duration) == [2.5])
        #expect(h.recordings.isEmpty)
    }

    @Test func microphoneUnpluggedRightAwayFailsWithoutSending() async {
        let h = Harness()
        await h.controller.start(language: .ru)
        h.clock.advance(by: 0.2)
        h.capture.interrupt(.deviceChanged(inputAvailable: false))
        #expect(h.controller.phase == .failed(.noInputDevice))
        #expect(await h.transcriber.calls.isEmpty)
        #expect(h.recordings.isEmpty)
    }

    @Test func failedWriteFailsWithoutSending() async {
        let h = Harness()
        await h.controller.start(language: .ru)
        h.clock.advance(by: 3)
        h.capture.recordedDuration = 0
        h.capture.interrupt(.failed)
        #expect(h.controller.phase == .failed(.recordingFailed))
        #expect(await h.transcriber.calls.isEmpty)
        #expect(h.recordings.isEmpty)
    }

    @Test func sleepAndScreenLockCancelRecording() async {
        let h = Harness()
        await h.controller.start(language: .ru)
        h.clock.advance(by: 1)
        h.workspace.post(name: NSWorkspace.willSleepNotification, object: nil)
        #expect(h.controller.phase == .idle)
        #expect(!h.capture.isRunning)
        #expect(h.recordings.isEmpty)

        await h.controller.start(language: .ru)
        h.clock.advance(by: 1)
        h.distributed.post(name: Notification.Name("com.apple.screenIsLocked"), object: nil)
        #expect(h.controller.phase == .idle)
        #expect(!h.capture.isRunning)
        #expect(await h.transcriber.calls.isEmpty)
    }

    @Test func screenLockLeavesARunningTranscriptionAlone() async {
        let h = Harness(holdsTranscription: true)
        await h.controller.start(language: .ru)
        h.clock.advance(by: 1)
        let stopping = Task { await h.controller.stop() }
        await h.transcriber.waitForCall()
        h.distributed.post(name: Notification.Name("com.apple.screenIsLocked"), object: nil)
        #expect(h.controller.phase == .transcribing)
        await h.transcriber.release()
        await stopping.value
        #expect(h.controller.phase == .finished("Привет"))
    }

    // MARK: Levels and files

    @Test func levelsKeepFortyValuesOldestFirstWithinZeroToOne() async {
        let h = Harness()
        #expect(h.controller.levels == Array(repeating: 0, count: 40))
        await h.controller.start(language: .ru)

        let sent = (1...45).map { Float($0) / 100 }
        sent.forEach { h.capture.send(level: $0) }
        #expect(h.controller.levels == Array(sent.suffix(40)))

        for level: Float in [-3, 7, .nan, .infinity, -.infinity, 0.5] {
            h.capture.send(level: level)
        }
        #expect(h.controller.levels.count == 40)
        #expect(h.controller.levels.allSatisfy { (0...1).contains($0) })
        #expect(Array(h.controller.levels.suffix(6)) == [0, 1, 0, 1, 0, 0.5])

        h.clock.advance(by: 1)
        await h.controller.stop()
        h.capture.send(level: 0.9)
        #expect(h.controller.levels.last == 0.5)
        #expect(h.controller.levels.count == 40)
    }

    @Test func leftoversOlderThanAnHourAreRemovedAtLaunch() throws {
        let directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("AirReplyVoiceTests-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let old = directory.appendingPathComponent("old.m4a")
        let fresh = directory.appendingPathComponent("fresh.m4a")
        for (file, age) in [(old, 2.0 * 3600), (fresh, 600.0)] {
            FileManager.default.createFile(atPath: file.path, contents: Data("x".utf8))
            try FileManager.default.setAttributes([.modificationDate: Date().addingTimeInterval(-age)], ofItemAtPath: file.path)
        }

        let clock = ManualClock()
        _ = VoiceController(transcriber: FakeTranscriber(.success("")), permission: FakePermission(.authorized),
                            capture: FakeCapture(clock: clock), clock: clock, directory: directory)
        #expect(!FileManager.default.fileExists(atPath: old.path))
        #expect(FileManager.default.fileExists(atPath: fresh.path))
    }

    @Test func publicInitStartsIdle() {
        let controller = VoiceController(transcriber: FakeTranscriber(.success("")), permission: FakePermission(.denied))
        #expect(controller.phase == .idle)
        #expect(controller.levels == Array(repeating: 0, count: VoiceController.levelCount))
        #expect(controller.elapsed == 0)
        #expect(controller.maxDuration == 40)
    }
}
