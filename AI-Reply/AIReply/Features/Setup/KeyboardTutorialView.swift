import SwiftUI
import AVKit
import UIKit

/// The complete keyboard guide stays available after onboarding. Video playback
/// is local and silent; only the keyboard's live signal can verify setup.
struct KeyboardTutorialView: View {
    @Environment(AppSettings.self) private var settings
    @Environment(KeyboardStatusMonitor.self) private var keyboard
    @Environment(\.scenePhase) private var scenePhase
    @FocusState private var isChecking: Bool
    @State private var didCopy = false

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: DS.Spacing.l) {
                OnboardingStepTitle("tutorial.keyboard.title", "tutorial.keyboard.body")

                KeyboardTutorialPlayer(language: settings.effectiveLanguage)

                VStack(alignment: .leading, spacing: DS.Spacing.m) {
                    sectionTitle("tutorial.keyboard.connect")
                    step(1, "enable")
                    step(2, "access")
                    step(3, "switch")
                    Text("tutorial.keyboard.toolbar")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                    OpenKeyboardSettingsButton()
                        .accessibilityIdentifier("keyboardTutorial.settings")
                }
                .dsCard()

                VStack(alignment: .leading, spacing: DS.Spacing.m) {
                    sectionTitle("tutorial.keyboard.reply")
                    step(4, "copy")
                    step(5, "generate")
                    step(6, "insert")
                }
                .dsCard()

                VStack(alignment: .leading, spacing: DS.Spacing.m) {
                    sectionTitle("tutorial.keyboard.paste")
                    Text("tutorial.keyboard.paste.body")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                    NavigationLink { PasteHelpView() } label: {
                        Label("permissions.paste.howTo", systemImage: "doc.on.clipboard")
                            .frame(minHeight: DS.Layout.minimumTouchTarget)
                    }
                    NavigationLink { PasteTutorialView() } label: {
                        Label("tutorial.paste.title", systemImage: "play.rectangle")
                            .frame(minHeight: DS.Layout.minimumTouchTarget)
                    }
                }
                .dsCard()

                VStack(alignment: .leading, spacing: DS.Spacing.m) {
                    sectionTitle("tutorial.keyboard.practice")
                    Text("tutorial.keyboard.practice.body")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                    Text(settings.localized("onboarding.demo.message"))
                        .font(.body)
                        .textSelection(.enabled)
                    Button {
                        UIPasteboard.general.string = settings.localized("onboarding.demo.message")
                        didCopy = true
                    } label: {
                        Label(didCopy ? "compose.copied" : "common.copy",
                              systemImage: didCopy ? "checkmark" : "doc.on.doc")
                    }
                    .buttonStyle(.dsOutline)
                    .accessibilityIdentifier("keyboardTutorial.copy")
                    KeyboardCheckField(focus: $isChecking)
                }
                .dsCard()

                Text("tutorial.keyboard.permissions")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                Text("tutorial.keyboard.secureFields")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            .padding(DS.Spacing.l)
            .frame(maxWidth: DS.Layout.readableWidth, alignment: .leading)
            .frame(maxWidth: .infinity)
        }
        .background(Color.dsBackground)
        .scrollDismissesKeyboard(.interactively)
        .navigationTitle("tutorial.keyboard.nav")
        .navigationBarTitleDisplayMode(.inline)
        .task { await keyboard.followLiveUpdates() }
        .onChange(of: scenePhase) { _, phase in
            if phase == .active { keyboard.refresh() }
        }
        .onChange(of: settings.effectiveLanguage) { didCopy = false }
    }

    private func sectionTitle(_ key: LocalizedStringKey) -> some View {
        Text(key)
            .font(.headline)
            .accessibilityAddTraits(.isHeader)
    }

    private func step(_ number: Int, _ name: String) -> some View {
        let prefix = "tutorial.keyboard." + name
        return DSStepRow(index: number, text: LocalizedStringKey(prefix + ".title"),
                  detail: LocalizedStringKey(prefix + ".body"))
            .accessibilityIdentifier("keyboardTutorial.step.\(name)")
    }
}

/// A native local-file player. Language changes load a different recording and
/// progress key. Pausing or leaving never completes onboarding or grants access.
private struct KeyboardTutorialPlayer: View {
    let language: AppLanguage
    @Environment(\.scenePhase) private var scenePhase
    @State private var player: AVPlayer?
    @State private var observer: Any?
    @State private var loadedResource: KeyboardTutorialResource?
    @State private var position = 0.0
    @State private var aspectRatio = 720.0 / 1766.0

    private var resource: KeyboardTutorialResource? {
        KeyboardTutorialResource.resolve(for: language)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.s) {
            if resource != nil {
                VideoPlayer(player: player)
                    .aspectRatio(aspectRatio, contentMode: .fit)
                    .frame(maxHeight: 450)
                    .clipShape(RoundedRectangle(cornerRadius: DS.Radius.medium))
                    .accessibilityLabel("tutorial.keyboard.player")
                    .accessibilityIdentifier("keyboardTutorial.player")
                Button {
                    guard let player else { return }
                    if let loadedResource {
                        KeyboardTutorialPlayback.savePosition(0, for: loadedResource.language)
                    }
                    position = 0
                    player.seek(to: .zero)
                    player.play()
                } label: {
                    Label(position > 0 ? "tutorial.replay" : "tutorial.watch",
                          systemImage: position > 0 ? "arrow.counterclockwise" : "play.fill")
                        .frame(minHeight: DS.Layout.minimumTouchTarget)
                }
                .disabled(player == nil)
                .accessibilityIdentifier("keyboardTutorial.play")
                Text("tutorial.keyboard.silent")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                if let resource, resource.language != language {
                    Text("tutorial.paste.videoLanguageFallback")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            } else {
                Text("tutorial.unavailable")
            }
        }
        .task(id: language) { await prepare() }
        .onDisappear(perform: stop)
        .onChange(of: scenePhase) { _, phase in
            if phase != .active {
                player?.pause()
                savePosition()
            }
        }
    }

    private func prepare() async {
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
            KeyboardTutorialPlayback.savedPosition(for: resource.language), duration: duration)
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
            KeyboardTutorialPlayback.savePosition(time.seconds, for: resource.language)
        }
    }

    private func savePosition() {
        guard let loadedResource, let value = player?.currentTime().seconds, value.isFinite else { return }
        KeyboardTutorialPlayback.savePosition(value, for: loadedResource.language)
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

struct KeyboardTutorialResource: Equatable {
    let language: AppLanguage
    let url: URL

    static func name(for language: AppLanguage) -> String { "keyboard-setup-\(language.rawValue)" }

    static func resolve(for language: AppLanguage, bundle: Bundle = .main) -> Self? {
        resolve(for: language) { bundle.url(forResource: $0, withExtension: "mp4") }
    }

    static func resolve(for language: AppLanguage, lookup: (String) -> URL?) -> Self? {
        if let url = lookup(name(for: language)) { return Self(language: language, url: url) }
        if language != .english, let url = lookup(name(for: .english)) {
            return Self(language: .english, url: url)
        }
        return nil
    }
}

enum KeyboardTutorialPlayback {
    static func savedPosition(for language: AppLanguage, defaults: UserDefaults = .standard) -> Double {
        defaults.double(forKey: positionKey(for: language))
    }

    static func savePosition(_ position: Double, for language: AppLanguage, defaults: UserDefaults = .standard) {
        defaults.set(position.isFinite ? max(0, position) : 0, forKey: positionKey(for: language))
    }

    private static func positionKey(for language: AppLanguage) -> String {
        "tutorial.keyboard.position.\(language.rawValue)"
    }
}
