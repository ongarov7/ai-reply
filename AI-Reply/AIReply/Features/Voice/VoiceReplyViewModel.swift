import AVFoundation
import Foundation
import Observation
import Speech
import UIKit

/// Everything this model can be saying, kept as a value rather than a rendered
/// string so that changing the app language re-renders the current status
/// instead of leaving a stale sentence in the previous language on screen.
enum VoiceStatus: Equatable {
    case idle
    case requestingPermission
    case recording
    case stopped
    case stoppedNoSpeech
    case stoppedAtLimit
    case stoppedAtLimitNoSpeech
    case copied
    case microphoneDenied
    case speechDenied
    case speechRestricted
    case speechNotDetermined
    case speechUnavailable
    case speechUnavailableNow
    case languageUnavailable(AppLanguage)
    case recognitionFailed(reason: String, mayNeedNetwork: Bool)

    /// In the language chosen in the app: `String(localized:locale:)` would
    /// read the phone's language instead (see `AppSettings.localized`).
    func message(language: AppLanguage) -> String {
        func localized(_ key: String) -> String {
            AppSettings.localized(key, language: language)
        }

        switch self {
        case .idle:                     return localized("voice.tapToSpeak")
        case .requestingPermission:     return localized("voice.status.requesting")
        case .recording:                return localized("voice.status.recording")
        case .stopped:                  return localized("voice.status.stopped")
        case .stoppedNoSpeech:          return localized("voice.status.noSpeech")
        case .stoppedAtLimit:           return localized("voice.status.stoppedAtLimit")
        case .stoppedAtLimitNoSpeech:   return localized("voice.status.stoppedAtLimitNoSpeech")
        case .copied:                   return localized("voice.status.copied")
        case .microphoneDenied:         return localized("voice.status.microphoneDenied")
        case .speechDenied:             return localized("voice.status.speechDenied")
        case .speechRestricted:         return localized("voice.status.speechRestricted")
        case .speechNotDetermined:      return localized("voice.status.speechNotDetermined")
        case .speechUnavailable:        return localized("voice.status.unavailable")
        case .speechUnavailableNow:     return localized("voice.status.unavailableNow")
        case .languageUnavailable(let language):
            return String(format: localized("voice.status.languageUnavailable"), language.nativeName)
        case .recognitionFailed(let reason, let mayNeedNetwork):
            let key = mayNeedNetwork ? "voice.status.failedNetwork" : "voice.status.failed"
            return String(format: localized(key), reason)
        }
    }
}

@MainActor
@Observable
final class VoiceReplyViewModel {

    static let maximumDuration: TimeInterval = 60

    var transcript: String = ""
    private(set) var isRecording = false
    private(set) var elapsed: TimeInterval = 0
    private(set) var status: VoiceStatus = .idle
    private(set) var isUsingOnDeviceRecognition = false

    /// The language recognition runs in. Previously hard-coded to `en-US`,
    /// which meant dictation simply did not work for a Russian or Kazakh user.
    private(set) var language: AppLanguage = .systemDefault

    @ObservationIgnored private let audioEngine = AVAudioEngine()
    @ObservationIgnored private var speechRecognizer: SFSpeechRecognizer?
    @ObservationIgnored private var recognitionRequest: SFSpeechAudioBufferRecognitionRequest?
    @ObservationIgnored private var recognitionTask: SFSpeechRecognitionTask?
    @ObservationIgnored private var timer: Timer?
    @ObservationIgnored private var stopWorkItem: DispatchWorkItem?
    @ObservationIgnored private var startedAt: Date?
    @ObservationIgnored private var hasRecognitionResult = false
    @ObservationIgnored private var isInputTapInstalled = false

    private var locale: Locale { Locale(identifier: language.localeIdentifier) }

    // MARK: Presentation

    var statusMessage: String { status.message(language: language) }

    var elapsedLabel: String { Self.format(elapsed) }

    var recordingLabel: String {
        String(
            format: AppSettings.localized("voice.recording", language: language),
            elapsedLabel,
            Self.format(Self.maximumDuration)
        )
    }

    var recognitionModeLabel: String {
        let key = isUsingOnDeviceRecognition ? "voice.mode.onDevice" : "voice.mode.server"
        return String(format: AppSettings.localized(key, language: language), language.nativeName)
    }

    var canCopyOrClear: Bool {
        !transcript.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    // MARK: Intent

    /// Changing the language re-renders the CURRENT status in the new language
    /// on its own, because `status` is a value and `statusMessage` is derived
    /// from it. Nothing has to be reset.
    func updateLanguage(_ newLanguage: AppLanguage) {
        guard newLanguage != language else { return }
        language = newLanguage
    }

    func startRecording() {
        guard !isRecording else { return }
        Task { await requestPermissionsAndStart() }
    }

    func stopRecording() {
        stopCapture(reason: .user)
    }

    func clearTranscript() {
        guard !isRecording else { return }
        transcript = ""
        hasRecognitionResult = false
        status = .idle
    }

    func copyTranscript() {
        guard canCopyOrClear else { return }
        UIPasteboard.general.string = transcript
        status = .copied
    }

    // MARK: Capture

    private func requestPermissionsAndStart() async {
        status = .requestingPermission

        guard await requestMicrophonePermission() else {
            status = .microphoneDenied
            return
        }

        let speechStatus = await requestSpeechPermission()
        guard speechStatus == .authorized else {
            status = Self.status(forSpeechAuthorization: speechStatus)
            return
        }

        guard Self.isRecognitionSupported(language) else {
            status = .languageUnavailable(language)
            return
        }

        guard let recognizer = SFSpeechRecognizer(locale: locale) else {
            status = .languageUnavailable(language)
            return
        }

        guard recognizer.isAvailable else {
            status = .speechUnavailableNow
            return
        }

        speechRecognizer = recognizer

        do {
            try startAudioRecognition(with: recognizer)
        } catch {
            stopCapture(reason: .failure(error))
        }
    }

    private func startAudioRecognition(with recognizer: SFSpeechRecognizer) throws {
        recognitionTask?.cancel()
        recognitionTask = nil
        hasRecognitionResult = false

        let request = SFSpeechAudioBufferRecognitionRequest()
        request.shouldReportPartialResults = true
        request.taskHint = .dictation
        request.addsPunctuation = true

        if recognizer.supportsOnDeviceRecognition {
            request.requiresOnDeviceRecognition = true
            isUsingOnDeviceRecognition = true
        } else {
            isUsingOnDeviceRecognition = false
        }

        recognitionRequest = request

        let audioSession = AVAudioSession.sharedInstance()
        try audioSession.setCategory(.record, mode: .measurement, options: [.duckOthers])
        try audioSession.setActive(true, options: .notifyOthersOnDeactivation)

        let inputNode = audioEngine.inputNode
        let recordingFormat = inputNode.outputFormat(forBus: 0)
        if isInputTapInstalled {
            inputNode.removeTap(onBus: 0)
            isInputTapInstalled = false
        }
        inputNode.installTap(onBus: 0, bufferSize: 1024, format: recordingFormat) { [weak request] buffer, _ in
            request?.append(buffer)
        }
        isInputTapInstalled = true

        recognitionTask = recognizer.recognitionTask(with: request) { [weak self] result, error in
            Task { @MainActor in
                self?.handleRecognition(result: result, error: error)
            }
        }

        audioEngine.prepare()
        try audioEngine.start()

        startedAt = Date()
        elapsed = 0
        isRecording = true
        status = .recording
        startTimers()
    }

    private func handleRecognition(result: SFSpeechRecognitionResult?, error: Error?) {
        if let result {
            hasRecognitionResult = true
            transcript = result.bestTranscription.formattedString
            if result.isFinal, !isRecording {
                finalizeStoppedRecognition()
            }
        }

        guard let error else { return }

        let failure = VoiceStatus.recognitionFailed(
            reason: error.localizedDescription,
            mayNeedNetwork: !isUsingOnDeviceRecognition
        )

        if isRecording {
            stopCapture(reason: .failureStatus(failure))
        } else if transcript.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            status = failure
        }
    }

    private func startTimers() {
        timer?.invalidate()
        let timer = Timer(timeInterval: 0.25, repeats: true) { [weak self] _ in
            Task { @MainActor in
                self?.updateElapsed()
            }
        }
        RunLoop.main.add(timer, forMode: .common)
        self.timer = timer

        stopWorkItem?.cancel()
        let workItem = DispatchWorkItem { [weak self] in
            Task { @MainActor in
                self?.stopCapture(reason: .limit)
            }
        }
        stopWorkItem = workItem
        DispatchQueue.main.asyncAfter(deadline: .now() + Self.maximumDuration, execute: workItem)
    }

    private func stopCapture(reason: StopReason) {
        updateElapsed()

        timer?.invalidate()
        timer = nil
        stopWorkItem?.cancel()
        stopWorkItem = nil

        if audioEngine.isRunning {
            audioEngine.stop()
        }
        if isInputTapInstalled {
            audioEngine.inputNode.removeTap(onBus: 0)
            isInputTapInstalled = false
        }
        recognitionRequest?.endAudio()
        recognitionTask?.finish()
        recognitionRequest = nil
        isRecording = false

        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)

        switch reason {
        case .user:
            status = canCopyOrClear ? .stopped : .stoppedNoSpeech
        case .limit:
            elapsed = Self.maximumDuration
            status = canCopyOrClear ? .stoppedAtLimit : .stoppedAtLimitNoSpeech
        case .failure(let error):
            status = .recognitionFailed(
                reason: error.localizedDescription,
                mayNeedNetwork: !isUsingOnDeviceRecognition
            )
        case .failureStatus(let failure):
            status = failure
        }
    }

    private func finalizeStoppedRecognition() {
        if !hasRecognitionResult || transcript.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            status = .stoppedNoSpeech
        }
    }

    private func updateElapsed() {
        guard let startedAt else { return }
        elapsed = min(Self.maximumDuration, Date().timeIntervalSince(startedAt))
    }

    // MARK: Permissions

    private func requestMicrophonePermission() async -> Bool {
        switch AVCaptureDevice.authorizationStatus(for: .audio) {
        case .authorized:
            return true
        case .notDetermined:
            return await withCheckedContinuation { continuation in
                AVCaptureDevice.requestAccess(for: .audio) { granted in
                    continuation.resume(returning: granted)
                }
            }
        case .denied, .restricted:
            return false
        @unknown default:
            return false
        }
    }

    private func requestSpeechPermission() async -> SFSpeechRecognizerAuthorizationStatus {
        let current = SFSpeechRecognizer.authorizationStatus()
        guard current == .notDetermined else { return current }

        return await withCheckedContinuation { continuation in
            SFSpeechRecognizer.requestAuthorization { status in
                continuation.resume(returning: status)
            }
        }
    }

    // MARK: Helpers

    /// Apple's on-device and server recognition do not cover every language.
    /// Kazakh in particular may be missing, and saying so plainly is better
    /// than letting the user hold a button that can never produce text.
    private static func isRecognitionSupported(_ language: AppLanguage) -> Bool {
        let code = language.rawValue
        return SFSpeechRecognizer.supportedLocales().contains { locale in
            locale.language.languageCode?.identifier == code
        }
    }

    private static func status(
        forSpeechAuthorization status: SFSpeechRecognizerAuthorizationStatus
    ) -> VoiceStatus {
        switch status {
        case .denied:           return .speechDenied
        case .restricted:       return .speechRestricted
        case .notDetermined:    return .speechNotDetermined
        case .authorized:       return .idle
        @unknown default:       return .speechUnavailable
        }
    }

    private static func format(_ time: TimeInterval) -> String {
        let seconds = min(Int(time.rounded(.down)), Int(maximumDuration))
        return String(format: "%02d:%02d", seconds / 60, seconds % 60)
    }
}

private enum StopReason {
    case user
    case limit
    case failure(Error)
    case failureStatus(VoiceStatus)
}
