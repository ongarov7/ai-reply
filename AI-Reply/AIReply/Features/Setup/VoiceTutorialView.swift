import SwiftUI
import AVKit
import UIKit

/// Genuine Simulator footage accompanies the written setup steps. Watching the
/// guide never starts dictation, changes consent or grants a system permission.
struct VoiceTutorialView: View {
    var isInVoiceScreen = false

    @Environment(AppSettings.self) private var settings
    @Environment(VoiceAssistantController.self) private var voice
    @Environment(\.dismiss) private var dismiss
    @Environment(\.openURL) private var openURL

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: DS.Spacing.l) {
                OnboardingStepTitle("tutorial.voice.title", "tutorial.voice.body")

                VStack(alignment: .leading, spacing: DS.Spacing.m) {
                    step(1, "permission")
                    Button("tutorial.voice.openDictation") {
                        if isInVoiceScreen { dismiss() } else { voice.open() }
                    }
                    .buttonStyle(.dsText)
                    .accessibilityIdentifier("voiceTutorial.openDictation")
                }
                .dsCard()

                if #available(iOS 18, *) {
                    VStack(alignment: .leading, spacing: DS.Spacing.m) {
                        Text("tutorial.voice.controlCenter.title")
                            .font(.headline)
                            .accessibilityAddTraits(.isHeader)
                        VoiceControlCenterPracticeLink()
                        VoiceControlCenterSteps(startingAt: 2)
                        step(7, "try")
                    }
                    .dsCard()

                    VoiceTutorialPlayer(language: settings.effectiveLanguage)
                } else {
                    Text("tutorial.voice.ios17")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }

                VStack(alignment: .leading, spacing: DS.Spacing.s) {
                    Text("tutorial.voice.denied")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                    if #available(iOS 18, *) {
                        VStack(alignment: .leading, spacing: DS.Spacing.xxs) {
                            Text("tutorial.voice.activity.title")
                                .font(.footnote.weight(.semibold))
                            Text("tutorial.voice.activity.body")
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                        }
                    }
                    Button("tutorial.voice.settings") {
                        guard let url = URL(string: UIApplication.openSettingsURLString) else { return }
                        openURL(url)
                    }
                    .frame(minHeight: DS.Layout.minimumTouchTarget)
                    .accessibilityIdentifier("voiceTutorial.settings")
                }
                .dsCard()

                Text("tutorial.voice.privacy")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            .padding(DS.Spacing.l)
            .frame(maxWidth: DS.Layout.readableWidth, alignment: .leading)
            .frame(maxWidth: .infinity)
        }
        .background(Color.dsBackground)
        .navigationTitle("tutorial.voice.nav")
        .navigationBarTitleDisplayMode(.inline)
        .accessibilityIdentifier("voiceTutorial.screen")
    }

    private func step(_ number: Int, _ name: String) -> some View {
        let prefix = "tutorial.voice." + name
        return DSStepRow(index: number, text: LocalizedStringKey(prefix + ".title"),
                         detail: LocalizedStringKey(prefix + ".body"))
            .accessibilityIdentifier("voiceTutorial.step.\(name)")
    }
}

struct VoiceControlCenterHelp: View {
    var body: some View {
        DisclosureGroup {
            VStack(alignment: .leading, spacing: DS.Spacing.m) {
                VoiceControlCenterPracticeLink()
                VoiceControlCenterSteps()
                Text("tutorial.voice.try.body")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            .padding(.top, DS.Spacing.s)
            .padding(.bottom, DS.Spacing.xs)
        } label: {
            Label("tutorial.voice.controlCenter.title", systemImage: "switch.2")
                .font(.subheadline.weight(.semibold))
                .fixedSize(horizontal: false, vertical: true)
                .frame(minHeight: DS.Layout.minimumTouchTarget, alignment: .leading)
        }
    }
}

struct VoiceControlCenterSteps: View {
    var startingAt = 1

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.m) {
            DSStepRow(index: startingAt, text: "tutorial.voice.openControlCenter.title",
                      detail: "tutorial.voice.openControlCenter.body")
                .accessibilityIdentifier("voiceTutorial.step.openControlCenter")
            DSStepRow(index: startingAt + 1, text: "tutorial.voice.openControlGallery.title",
                      detail: "tutorial.voice.openControlGallery.body")
                .accessibilityIdentifier("voiceTutorial.step.openControlGallery")
            DSStepRow(index: startingAt + 2, text: "tutorial.voice.searchControl.title",
                      detail: "tutorial.voice.searchControl.body")
                .accessibilityIdentifier("voiceTutorial.step.searchControl")
            DSStepRow(index: startingAt + 3, text: "tutorial.voice.control.title",
                      detail: "tutorial.voice.control.body")
                .accessibilityIdentifier("voiceTutorial.step.control")
            DSStepRow(index: startingAt + 4, text: "tutorial.voice.finishControl.title",
                      detail: "tutorial.voice.finishControl.body")
                .accessibilityIdentifier("voiceTutorial.step.finishControl")
        }
    }
}

private struct VoiceControlCenterPracticeLink: View {
    @State private var isShowingPractice = false

    var body: some View {
        Button {
            isShowingPractice = true
        } label: {
            Label("tutorial.voice.practice.launch", systemImage: "hand.tap")
                .frame(minHeight: DS.Layout.minimumTouchTarget)
        }
        .buttonStyle(.dsText)
        .accessibilityIdentifier("voiceControlCenter.practice")
        .sheet(isPresented: $isShowingPractice) {
            VoiceControlCenterPractice()
        }
    }
}

private struct VoiceControlCenterPractice: View {
    private enum Step: Int, CaseIterable {
        case open, edit, gallery, search, finish, start, dictation, done
    }

    @Environment(\.dismiss) private var dismiss
    @State private var step = Step.open
    @State private var search = ""

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: DS.Spacing.l) {
                    ProgressView(value: Double(step.rawValue + 1), total: Double(Step.allCases.count))
                    let copy = instruction
                    OnboardingStepTitle(copy.title, copy.body)
                    VStack(alignment: .leading, spacing: DS.Spacing.m) {
                        controls
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .dsCard()
                }
                .padding(DS.Spacing.l)
                .frame(maxWidth: DS.Layout.readableWidth)
                .frame(maxWidth: .infinity)
            }
            .scrollDismissesKeyboard(.interactively)
            .background(Color.dsBackground)
            .navigationTitle("tutorial.voice.practice.title")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("common.close") { dismiss() }
                }
            }
        }
    }

    private var instruction: (title: LocalizedStringKey, body: LocalizedStringKey) {
        switch step {
        case .open: ("tutorial.voice.openControlCenter.title", "tutorial.voice.openControlCenter.body")
        case .edit, .gallery: ("tutorial.voice.openControlGallery.title", "tutorial.voice.openControlGallery.body")
        case .search: ("tutorial.voice.searchControl.title", "tutorial.voice.searchControl.body")
        case .finish: ("tutorial.voice.finishControl.title", "tutorial.voice.finishControl.body")
        case .start: ("tutorial.voice.try.title", "tutorial.voice.try.body")
        case .dictation: ("tutorial.voice.practice.recording.title", "tutorial.voice.practice.recording.body")
        case .done: ("tutorial.voice.practice.done.title", "tutorial.voice.practice.done.body")
        }
    }

    @ViewBuilder
    private var controls: some View {
        switch step {
        case .open:
            action("tutorial.voice.practice.open", icon: "arrow.down", next: .edit)
        case .edit:
            Button {
                step = .gallery
            } label: {
                Image(systemName: "plus")
                    .font(.headline)
                    .frame(width: DS.Layout.minimumTouchTarget, height: DS.Layout.minimumTouchTarget)
                    .background(Color.accentColor.opacity(0.12), in: Circle())
            }
            .buttonStyle(.plain)
            .foregroundStyle(Color.accentColor)
            .accessibilityLabel("tutorial.voice.openControlGallery.title")
            .accessibilityIdentifier("voiceControlCenter.practice.plus")
        case .gallery:
            action("tutorial.voice.practice.add", icon: "plus", next: .search)
        case .search:
            TextField("tutorial.voice.practice.search", text: $search)
                .textFieldStyle(.roundedBorder)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
            let query = search.trimmingCharacters(in: .whitespacesAndNewlines)
            if !query.isEmpty, "AI Reply".localizedCaseInsensitiveContains(query) {
                action("AI Reply Voice", icon: "mic.fill", next: .finish)
            } else {
                Button("tutorial.voice.practice.typeName") { search = "AI Reply" }
                    .buttonStyle(.dsText)
            }
        case .finish:
            Label("AI Reply Voice", systemImage: "mic.fill")
                .font(.headline)
            action("tutorial.voice.practice.finish", icon: "checkmark", next: .start)
        case .start:
            action("AI Reply Voice", icon: "mic.fill", next: .dictation)
        case .dictation:
            action("common.done", icon: "checkmark", next: .done)
        case .done:
            Button("common.close") { dismiss() }
                .buttonStyle(.dsPrimary)
        }
    }

    private func action(_ title: LocalizedStringKey, icon: String, next: Step) -> some View {
        Button {
            step = next
        } label: {
            Label(title, systemImage: icon)
        }
        .buttonStyle(.dsPrimary)
    }
}

private struct VoiceTutorialPlayer: View {
    let language: AppLanguage
    @Environment(\.scenePhase) private var scenePhase
    @State private var player: AVPlayer?
    @State private var observer: Any?
    @State private var loadedResource: VoiceTutorialResource?
    @State private var position = 0.0
    @State private var aspectRatio = 720.0 / 1560.0
    @State private var showsFullScreen = false
    @State private var startsFullScreenPlayback = false

    private var resource: VoiceTutorialResource? {
        VoiceTutorialResource.resolve(for: language)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.s) {
            if let resource {
                Group {
                    if showsFullScreen {
                        Color.black
                    } else {
                        VideoPlayer(player: player)
                    }
                }
                    .aspectRatio(aspectRatio, contentMode: .fit)
                    .frame(maxHeight: 450)
                    .clipShape(RoundedRectangle(cornerRadius: DS.Radius.medium))
                    .accessibilityLabel("tutorial.voice.player")
                    .accessibilityIdentifier("voiceTutorial.player")
                Button {
                    guard let player else { return }
                    if let loadedResource {
                        VoiceTutorialPlayback.savePosition(0, for: loadedResource.language)
                    }
                    position = 0
                    player.seek(to: .zero)
                    startsFullScreenPlayback = true
                    showsFullScreen = true
                } label: {
                    Label(position > 0 ? "tutorial.replay" : "tutorial.watch",
                          systemImage: position > 0 ? "arrow.counterclockwise" : "play.fill")
                        .frame(minHeight: DS.Layout.minimumTouchTarget)
                }
                .disabled(player == nil)
                .accessibilityIdentifier("voiceTutorial.play")
                Text("tutorial.voice.silent")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                if resource.language != language {
                    Text("tutorial.voice.russianFallback")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .accessibilityIdentifier("voiceTutorial.languageFallback")
                }
            } else {
                Text("tutorial.unavailable")
            }
        }
        .task(id: language) { await prepare() }
        .fullScreenCover(isPresented: $showsFullScreen, onDismiss: {
            startsFullScreenPlayback = false
            pauseAndSave()
        }) {
            fullScreenPlayback
        }
        .onDisappear {
            // A full-screen presentation can hide its presenting view. Keep
            // the same player and observer alive until the guide actually exits.
            if !showsFullScreen { stop() }
        }
        .onChange(of: scenePhase) { _, phase in
            if phase != .active { pauseAndSave() }
        }
    }

    private var fullScreenPlayback: some View {
        VStack(spacing: 0) {
            HStack {
                Spacer()
                Button { showsFullScreen = false } label: {
                    Label("common.close", systemImage: "xmark")
                        .font(.body.weight(.semibold))
                        .padding(.horizontal, DS.Spacing.m)
                        .frame(minWidth: DS.Layout.minimumTouchTarget,
                               minHeight: DS.Layout.minimumTouchTarget)
                }
                .buttonStyle(.plain)
                .foregroundStyle(.white)
                .accessibilityIdentifier("voiceTutorial.closeVideo")
            }
            .padding(.horizontal, DS.Spacing.s)
            VideoPlayer(player: player)
                .aspectRatio(aspectRatio, contentMode: .fit)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .accessibilityLabel("tutorial.voice.player")
                .accessibilityIdentifier("voiceTutorial.fullscreenPlayer")
        }
        .background(Color.black.ignoresSafeArea())
        .onAppear {
            // This cover is presented only by the explicit Watch/Replay button.
            guard startsFullScreenPlayback else { return }
            startsFullScreenPlayback = false
            if scenePhase == .active { player?.play() }
        }
    }

    private func prepare() async {
        // Reappearing after full-screen playback keeps its player and progress.
        if loadedResource == resource, player != nil { return }
        stop()
        guard let resource else { return }
        let asset = AVURLAsset(url: resource.url)
        let duration = (try? await asset.load(.duration))?.seconds ?? 60
        let tracks = try? await asset.loadTracks(withMediaType: .video)
        let size = try? await tracks?.first?.load(.naturalSize)
        guard !Task.isCancelled else { return }
        if let size, size.width > 0, size.height > 0 {
            aspectRatio = Double(size.width / size.height)
        }
        let next = AVPlayer(playerItem: AVPlayerItem(asset: asset))
        next.isMuted = true
        let start = PasteTutorialPlayback.resumePosition(
            VoiceTutorialPlayback.savedPosition(for: resource.language), duration: duration)
        _ = await next.seek(to: CMTime(seconds: start, preferredTimescale: 600))
        guard !Task.isCancelled else { return }
        position = start
        player = next
        loadedResource = resource
        observer = next.addPeriodicTimeObserver(
            forInterval: CMTime(seconds: 0.5, preferredTimescale: 600), queue: .main
        ) { [weak next] time in
            guard let next, player === next, time.seconds.isFinite else { return }
            position = time.seconds
            VoiceTutorialPlayback.savePosition(time.seconds, for: resource.language)
        }
    }

    private func savePosition() {
        guard let loadedResource, let value = player?.currentTime().seconds, value.isFinite else { return }
        VoiceTutorialPlayback.savePosition(value, for: loadedResource.language)
    }

    private func pauseAndSave() {
        player?.pause()
        savePosition()
    }

    private func stop() {
        savePosition()
        player?.pause()
        if let observer { player?.removeTimeObserver(observer) }
        observer = nil
        player = nil
        loadedResource = nil
        position = 0
    }
}

/// The first recording uses Russian iOS. Other locales visibly identify that
/// fallback while their written instructions remain in the selected app language.
struct VoiceTutorialResource: Equatable {
    let language: AppLanguage
    let url: URL

    static func name(for language: AppLanguage) -> String { "voice-setup-tutorial-\(language.rawValue)" }

    static func resolve(for language: AppLanguage, bundle: Bundle = .main) -> Self? {
        resolve(for: language) { bundle.url(forResource: $0, withExtension: "mp4") }
    }

    static func resolve(for language: AppLanguage, lookup: (String) -> URL?) -> Self? {
        if let url = lookup(name(for: language)) { return Self(language: language, url: url) }
        if language != .russian, let url = lookup(name(for: .russian)) {
            return Self(language: .russian, url: url)
        }
        return nil
    }
}

enum VoiceTutorialPlayback {
    static func savedPosition(for language: AppLanguage, defaults: UserDefaults = .standard) -> Double {
        defaults.double(forKey: positionKey(for: language))
    }

    static func savePosition(_ position: Double, for language: AppLanguage, defaults: UserDefaults = .standard) {
        defaults.set(position.isFinite ? max(0, position) : 0, forKey: positionKey(for: language))
    }

    private static func positionKey(for language: AppLanguage) -> String {
        "tutorial.voice.position.\(language.rawValue)"
    }
}
