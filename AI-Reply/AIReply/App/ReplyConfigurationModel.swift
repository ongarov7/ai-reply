import Foundation
import Observation

/// Observable view over `ProfileStore`.
///
/// One instance lives in the environment, so every screen edits the same value
/// and the keyboard sees each change the moment it is written. Writes go
/// through `persist()` rather than a `didSet`, because `@Observable` rewrites
/// stored properties and mixing that with property observers is a subtlety this
/// app has no reason to depend on.
@MainActor
@Observable
final class ReplyConfigurationModel {

    private(set) var configuration: ReplyConfiguration

    @ObservationIgnored private let store: ProfileStore

    init(store: ProfileStore = .shared) {
        self.store = store
        self.configuration = store.load()
    }

    var profile: UserProfile { configuration.profile }
    var templates: [ReplyTemplate] { configuration.templates.sorted { $0.sortIndex < $1.sortIndex } }
    var visibleTemplates: [ReplyTemplate] { configuration.visibleTemplates }

    /// True until this device has been through the current onboarding.
    var needsOnboarding: Bool {
        OnboardingFlow.needsOnboarding(completedVersion: configuration.profile.completedOnboardingVersion)
    }

    /// True when the store could not reach the App Group container, which means
    /// the keyboard will not see anything saved here. Surfaced in Settings
    /// rather than silently tolerated.
    var isPersistent: Bool { store.isPersistent }

    // MARK: Mutation

    func updateProfile(_ mutate: (inout UserProfile) -> Void) {
        var profile = configuration.profile
        mutate(&profile)
        configuration.profile = profile
        persist()
    }

    /// Never lowers the version: a device that has seen a newer onboarding
    /// keeps it.
    func completeOnboarding(version: Int = OnboardingFlow.currentVersion) {
        updateProfile { $0.completedOnboardingVersion = max($0.completedOnboardingVersion, version) }
    }

    func update(_ template: ReplyTemplate) {
        guard let index = configuration.templates.firstIndex(where: { $0.id == template.id }) else { return }
        configuration.templates[index] = template
        persist()
    }

    @discardableResult
    func addCustomTemplate(named name: String) -> ReplyTemplate {
        let template = ReplyTemplate.custom(name: name, sortIndex: configuration.templates.count)
        configuration.templates.append(template)
        persist()
        return template
    }

    /// Built-ins are never destroyed, only hidden, so a user cannot end up with
    /// an empty keyboard bar and no way to get the defaults back.
    func delete(_ template: ReplyTemplate) {
        guard !template.isBuiltIn else { return }
        configuration.templates.removeAll { $0.id == template.id }
        persist()
    }

    func move(fromOffsets source: IndexSet, toOffset destination: Int) {
        var ordered = templates
        ordered.move(fromOffsets: source, toOffset: destination)
        for index in ordered.indices { ordered[index].sortIndex = index }
        configuration.templates = ordered
        persist()
    }

    func setVisible(_ isVisible: Bool, for template: ReplyTemplate) {
        guard let index = configuration.templates.firstIndex(where: { $0.id == template.id }) else { return }
        configuration.templates[index].isVisible = isVisible
        persist()
    }

    private func persist() {
        configuration = configuration.normalized()
        store.save(configuration)
    }
}
