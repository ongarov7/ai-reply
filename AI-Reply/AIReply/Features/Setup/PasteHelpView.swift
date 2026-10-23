import SwiftUI
import AVKit

/// «Разрешить вставку из других приложений»: where iOS keeps the setting
/// and what the user taps there (`aireply://paste_help`).
///
/// Басқа қолданбалардан қоюға рұқсат: баптау қайда және не басу керек.
///
/// The setting belongs to iOS and only the user can change it, so this page
/// explains and opens AI Reply's own page in Settings - it never claims to
/// allow anything. The path and the option are named as the keyboard's
/// refusal names them (`AIReplyStrings.pasteDenied`), in iOS's own words.
struct PasteHelpView: View {

    @State private var sampleText = ""

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: DS.Spacing.l) {
                VStack(alignment: .leading, spacing: DS.Spacing.s) {
                    Text("pasteHelp.title")
                        .font(.title2.weight(.bold))
                        .accessibilityAddTraits(.isHeader)
                    Text("pasteHelp.body")
                        .foregroundStyle(.secondary)
                }

                NavigationLink { PasteTutorialView() } label: {
                    Label("tutorial.paste.title", systemImage: "play.rectangle")
                        .frame(minHeight: DS.Layout.minimumTouchTarget, alignment: .leading)
                }
                .dsCard()

                VStack(alignment: .leading, spacing: DS.Spacing.m) {
                    DSStepRow(index: 1, text: "setup.connect.open", detail: "setup.connect.open.detail")
                    DSStepRow(index: 2, text: "pasteHelp.step.row")
                    DSStepRow(index: 3, text: "pasteHelp.step.allow")
                    Text("pasteHelp.path")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                .dsCard()

                VStack(alignment: .leading, spacing: DS.Spacing.s) {
                    note("person.crop.circle", "pasteHelp.onlyYou")
                    note("keyboard", "pasteHelp.manual")
                    note("questionmark.circle", "pasteHelp.noRow")
                }
                .dsCard()

                VStack(alignment: .leading, spacing: DS.Spacing.s) {
                    Text("tutorial.paste.try").font(.headline)
                    Text("tutorial.paste.try.body").font(.subheadline).foregroundStyle(.secondary)
                    TextField("keyboardCheck.placeholder", text: $sampleText, axis: .vertical)
                        .textFieldStyle(.roundedBorder)
                        .accessibilityIdentifier("pasteTutorial.sample")
                    PasteButton(payloadType: String.self) { values in
                        if let text = values.first { sampleText = text }
                    }
                    .frame(minHeight: DS.Layout.minimumTouchTarget)
                }
                .dsCard()
            }
            .padding(DS.Spacing.l)
            .frame(maxWidth: DS.Layout.readableWidth, alignment: .leading)
            .frame(maxWidth: .infinity)
        }
        .background(Color.dsBackground)
        .safeAreaInset(edge: .bottom) {
            Button("permissions.openSettings", action: OpenKeyboardSettingsButton.openSystemSettings)
                .buttonStyle(.dsPrimary)
                .padding(.horizontal, DS.Spacing.l)
                .padding(.vertical, DS.Spacing.s)
                .frame(maxWidth: DS.Layout.readableWidth)
                .frame(maxWidth: .infinity)
                .background(Color.dsBackground)
        }
        .navigationTitle("pasteHelp.nav")
        .navigationBarTitleDisplayMode(.inline)
        .onAppear { ProductEvents.track(.pasteTutorialViewed) }
    }

    private func note(_ symbol: String, _ text: LocalizedStringKey) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: DS.Spacing.s) {
            Image(systemName: symbol)
                .foregroundStyle(.secondary)
                .frame(width: 22)
                .accessibilityHidden(true)
            Text(text).font(.subheadline)
        }
    }
}


/// Genuine simulator footage recorded separately in each app language.
/// Playback and its progress stay on this device. Watching never grants a permission.
struct PasteTutorialView: View {
    @Environment(AppSettings.self) private var settings
    @Environment(\.scenePhase) private var scenePhase
    @State private var player: AVPlayer?
    @State private var observer: Any?
    @State private var position = 0.0
    @State private var loadedResource: PasteTutorialResource?

    private var resource: PasteTutorialResource? {
        PasteTutorialResource.resolve(for: settings.effectiveLanguage)
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: DS.Spacing.m) {
                Text("tutorial.paste.title")
                    .font(.title2.bold())
                    .accessibilityAddTraits(.isHeader)
                if resource != nil {
                    VideoPlayer(player: player)
                        .aspectRatio(720.0 / 1766.0, contentMode: .fit)
                        .frame(maxHeight: 450)
                        .clipShape(RoundedRectangle(cornerRadius: DS.Radius.medium))
                        .accessibilityLabel("tutorial.paste.player")
                    Text(LocalizedStringKey(captionKey))
                        .font(.body.weight(.medium))
                        .frame(maxWidth: .infinity, minHeight: 72, alignment: .leading)
                        .accessibilityIdentifier("pasteTutorial.caption")
                    Button {
                        if let loadedResource { PasteTutorialPlayback.savePosition(0, for: loadedResource.language) }
                        position = 0
                        player?.seek(to: .zero)
                        player?.play()
                    } label: {
                        Label(position > 0 ? "tutorial.replay" : "tutorial.watch", systemImage: position > 0 ? "arrow.counterclockwise" : "play.fill")
                    }
                    .frame(minHeight: DS.Layout.minimumTouchTarget)
                } else {
                    Text("tutorial.unavailable")
                }
                if let resource, resource.language != settings.effectiveLanguage {
                    Text("tutorial.paste.videoLanguageFallback")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                Text("tutorial.paste.limitation")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                Text("tutorial.paste.distinction")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                Button("permissions.openSettings", action: OpenKeyboardSettingsButton.openSystemSettings)
                    .buttonStyle(.dsPrimary)
            }
            .padding(DS.Spacing.l)
            .frame(maxWidth: DS.Layout.readableWidth)
            .frame(maxWidth: .infinity)
        }
        .background(Color.dsBackground)
        .navigationTitle("tutorial.videos")
        .navigationBarTitleDisplayMode(.inline)
        .task(id: settings.effectiveLanguage) { await prepare() }
        .onDisappear(perform: stop)
        .onChange(of: scenePhase) { _, phase in
            if phase != .active {
                player?.pause()
                savePosition()
            }
        }
    }

    private var captionKey: String {
        switch position {
        case ..<4: return "tutorial.paste.caption.setup"
        case ..<7: return "tutorial.paste.caption.settings"
        case ..<14: return "tutorial.paste.caption.allow"
        default: return "tutorial.paste.caption.try"
        }
    }

    private func prepare() async {
        stop()
        guard let resource else { return }
        let asset = AVURLAsset(url: resource.url)
        let duration = (try? await asset.load(.duration))?.seconds ?? 24
        guard !Task.isCancelled else { return }
        let next = AVPlayer(playerItem: AVPlayerItem(asset: asset))
        next.isMuted = true
        // A completed tutorial opens at the beginning; unfinished playback resumes.
        let saved = PasteTutorialPlayback.savedPosition(for: resource.language)
        let start = PasteTutorialPlayback.resumePosition(saved, duration: duration)
        position = start
        _ = await next.seek(to: CMTime(seconds: start, preferredTimescale: 600))
        guard !Task.isCancelled else { return }
        player = next
        loadedResource = resource
        observer = next.addPeriodicTimeObserver(forInterval: CMTime(seconds: 0.5, preferredTimescale: 600), queue: .main) { [weak next] time in
            guard let next, player === next else { return }
            let value = time.seconds
            guard value.isFinite else { return }
            position = value
            PasteTutorialPlayback.savePosition(value, for: resource.language)
        }
        // Each recording already contains its own visible captions.
        // User starts playback through the native accessible controls.
    }

    private func savePosition() {
        guard let loadedResource, let value = player?.currentTime().seconds, value.isFinite else { return }
        PasteTutorialPlayback.savePosition(value, for: loadedResource.language)
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


/// The app language selects the recording, including when it follows iOS.
/// A missing localized asset may fall back only to English, with a visible
/// notice in the player. Release tests require all four exact assets.
struct PasteTutorialResource: Equatable {
    let language: AppLanguage
    let url: URL

    static func name(for language: AppLanguage) -> String { "paste-permission-\(language.rawValue)" }

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


/// A finished video restarts on replay; interruption resumes without implying consent.
enum PasteTutorialPlayback {
    static func resumePosition(_ saved: Double, duration: Double = 24) -> Double {
        guard saved.isFinite, duration.isFinite, duration > 0.5, saved >= 0, saved < duration - 0.5 else { return 0 }
        return saved
    }

    static func savedPosition(for language: AppLanguage, defaults: UserDefaults = .standard) -> Double {
        defaults.double(forKey: positionKey(for: language))
    }

    static func savePosition(_ position: Double, for language: AppLanguage, defaults: UserDefaults = .standard) {
        defaults.set(position.isFinite ? max(0, position) : 0, forKey: positionKey(for: language))
    }

    private static func positionKey(for language: AppLanguage) -> String { "tutorial.paste.position.\(language.rawValue)" }
}
