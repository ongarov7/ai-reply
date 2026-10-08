import UIKit

/// Opting the input view into system key-click feedback. This is the only
/// reason `loadView()` is overridden.
final class ReplyInputView: UIInputView, UIInputViewAudioFeedback {
    var enableInputClicksWhenVisible: Bool { true }
}

final class KeyboardViewController: UIInputViewController {

    // MARK: State

    /// The LAYOUT being typed on.
    private var language = KeyboardLanguageStore.load()
    private var enabledLanguages = KeyboardLanguageStore.enabledLanguages()

    /// The APP's interface language: every product label follows it. Read
    /// when the keyboard appears, never on a keypress.
    private var uiLanguage: AppLanguage = SharedSettings.shared.effectiveAppLanguage

    private var plane: KeyboardPlane = .letters
    private var shift = ShiftState()
    private var theme = KeyboardTheme(isDark: false)

    /// Read after the keyboard is connected to its host; iOS logs a warning
    /// and may answer wrongly before that.
    private var showsGlobe = false

    // MARK: Views

    private let actionBar = KeyboardActionBar()
    private let keysView = KeyboardKeysView()

    private var heightConstraint: NSLayoutConstraint?
    private var barHeightConstraint: NSLayoutConstraint?
    private var keysHeightConstraint: NSLayoutConstraint?

    private var sizing: KeyboardSizing?
    private var shownPage: PageIdentity?
    private var screenHeight: CGFloat = 812

    private struct PageIdentity: Equatable {
        let language: KeyboardLanguage
        let plane: KeyboardPlane
        let options: KeyboardPageOptions
        let sizing: KeyboardSizing
        let areaHeight: CGFloat
    }

    // MARK: Collaborators

    private lazy var replyCoordinator = ReplyFlowCoordinator(service: Self.makeService())
    private lazy var composeCoordinator = ComposeFlowCoordinator(service: Self.makeComposeService())

    /// Smart correction: the strip, the correction a separator applies, Undo.
    private lazy var autocorrect: AutocorrectController = {
        let controller = AutocorrectController(language: language)
        controller.delegate = self
        return controller
    }()

    /// The cleaner version of the instruction, offered after a pause.
    private lazy var polisher: InstructionPolisher = {
        let polisher = InstructionPolisher(service: Self.makePolishService())
        polisher.onChange = { [weak self] chip in self?.actionBar.showPolish(chip) }
        return polisher
    }()

    /// The smart-correction setting, read when the keyboard appears.
    private var smartCorrectionEnabled = true
    /// Whether the server takes reports about a generated text, read when
    /// the keyboard appears.
    private var serverTakesReports = false
    /// Whether the host field wants typing help (not an address, a code or
    /// a password), from its text traits.
    private var hostAllowsCorrection = true
    /// Whether a word is being typed at the caret right now. The strip and a
    /// separator's correction act only then - never on a word that was
    /// already there when the keyboard appeared or the caret moved.
    private var wordTyping = AutocorrectCaret()
    /// A word character right after the host's caret: the caret is inside a
    /// word, where smart correction stays out. Read together with
    /// `hostContext`; the keyboard's own typing never changes what follows
    /// the caret, so no keystroke has to read it.
    private var hostCaretInsideWord = false

    /// Which AI flow owns the composer panel. At most one: Create drops any
    /// reply session when it opens, and the persona row - the only way into a
    /// reply - is hidden while Create is open. Derived, never stored, so it
    /// cannot disagree with the coordinators.
    private enum AIFlow {
        case none
        case reply
        case compose
    }

    private var activeFlow: AIFlow {
        if composeCoordinator.isActive { return .compose }
        if replyCoordinator.isComposing { return .reply }
        return .none
    }

    private var configurationLoadedAt: TimeInterval = 0
    private var lastSpaceTap: TimeInterval = 0
    private var spaceCaptionReset: DispatchWorkItem?

    /// The text before the host's caret, kept locally so auto-capitalisation
    /// and the double-space full stop never need a cross-process read on a
    /// keystroke. Re-read from the proxy only when the host changed it.
    private var hostContext: String?
    private var hostAutocapitalization: UITextAutocapitalizationType = .sentences
    private var lastHostMutation: TimeInterval = 0
    /// The host field the keys type into. Return or Next can move the caret
    /// to another field inside the window where the keyboard trusts its own
    /// mirror; a new field is read at once regardless.
    private var hostField = HostFieldTracker()
    private var lastAppearanceProbe: TimeInterval = 0

    // MARK: Lifecycle

    override func loadView() {
        let inputView = ReplyInputView(frame: .zero, inputViewStyle: .keyboard)
        inputView.allowsSelfSizing = true
        view = inputView
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        view.clipsToBounds = false
        replyCoordinator.delegate = self
        replyCoordinator.uiLanguage = uiLanguage
        composeCoordinator.delegate = self
        composeCoordinator.uiLanguage = uiLanguage
        actionBar.delegate = self
        keysView.delegate = self

        // The persona row from the small App Group summary, so the FIRST
        // frame already shows this user's personas in their language.
        applyChips(from: nil)
        buildHierarchy()
        seedHeightFromCache()
        registerForTraitChanges([UITraitUserInterfaceStyle.self]) { (controller: KeyboardViewController, _) in
            controller.refreshThemeIfNeeded()
        }
        applyTheme(KeyboardTheme.resolve(
            appearance: textDocumentProxy.keyboardAppearance ?? .default,
            traits: traitCollection
        ))
    }

    override func viewWillAppear(_ animated: Bool) {
        super.viewWillAppear(animated)
        lastAppearanceProbe = Date.timeIntervalSinceReferenceDate
        refreshScreenHeight()
        reloadSettings()
        AILimits.reload()
        smartCorrectionEnabled = SharedSettings.shared.smartCorrectionEnabled
        serverTakesReports = AILimits.serverSupportsAIReports
        // A word already in the field is not being typed: no strip over the
        // personas until the user types one.
        wordTyping.reset()
        autocorrect.keyboardWillAppear(settingEnabled: smartCorrectionEnabled, language: language)
        polisher.reset()
        refreshThemeIfNeeded()
        showsGlobe = needsInputModeSwitchKey
        plane = KeyboardFieldKind.startsOnNumbers(textDocumentProxy.keyboardType ?? .default) ? .numbers : .letters
        hostField.reset(to: currentHostField)
        refreshHostContext()
        layoutKeyboard(force: true)
        refreshAutoShift()
        loadConfigurationIfNeeded()

        if let parked = ReplySessionParking.take() {
            replyCoordinator.restore(parked)
        } else if let parked = ComposeSessionParking.take() {
            composeCoordinator.restore(parked)
            showComposePanel()
        }
    }

    override func viewDidAppear(_ animated: Bool) {
        super.viewDidAppear(animated)
        // Re-checked now that the host connection certainly exists.
        if needsInputModeSwitchKey != showsGlobe {
            showsGlobe = needsInputModeSwitchKey
            layoutKeyboard(force: true)
        }
        keysView.hapticsEnabled = SharedSettings.shared.keyboardHapticsActive(hasFullAccess: hasFullAccess)
        reportActivityToContainingApp()
        if hasFullAccess { AILimitsRefresher.refreshIfStale() }
        // Dictionaries load only now, in the background: the first frame
        // never waits for them.
        autocorrect.keyboardDidAppear(lexiconSource: self)
        polisher.isEnabled = smartCorrectionEnabled && serverOffersPolish && canReachNetwork
        refreshSuggestions()
    }

    override func viewWillDisappear(_ animated: Bool) {
        super.viewWillDisappear(animated)
        polisher.stop()
        autocorrect.keepCorrection()
        // An unfinished reply or Create is kept in memory for a few minutes,
        // so a trip to another chat does not cost the instruction. A running
        // request is stopped: no spinner survives the keyboard going away.
        if replyCoordinator.session != nil || composeCoordinator.isActive {
            replyCoordinator.park()
            composeCoordinator.park()
            actionBar.endComposing()
            keysView.isInputDimmed = false
            updateGeometry(animated: false)
        }
    }

    override func viewWillLayoutSubviews() {
        super.viewWillLayoutSubviews()
        refreshScreenHeight()
        let width = view.bounds.width
        guard width > 0 else { return }
        let landscape = KeyboardSizing.isLandscape(width: width, screenHeight: screenHeight)
        let scale = view.window?.windowScene?.screen.scale ?? traitCollection.displayScale
        let next = KeyboardSizing(width: width, isLandscape: landscape, scale: scale)
        if next != sizing {
            sizing = next
            layoutKeyboard(force: false)
        }
    }

    override func textDidChange(_ textInput: UITextInput?) {
        super.textDidChange(textInput)
        let now = Date.timeIntervalSinceReferenceDate
        // Our own keystrokes already updated `hostContext`. Anything else -
        // the user moved the caret, the host cleared the field after sending
        // - is re-read once. So is another field: Return or Next moves the
        // caret there right after the keyboard's own keystroke.
        let fieldChanged = hostField.update(currentHostField)
        let ownMutation = HostFieldTracker.trustsMirror(sinceOwnMutation: now - lastHostMutation,
                                                        fieldChanged: fieldChanged)
        if fieldChanged { hostFieldDidChange() }
        if !ownMutation {
            refreshHostContext()
            // The text moved under the correction: Undo no longer applies,
            // and no word is being typed at the new caret yet.
            autocorrect.keepCorrection()
            wordTyping.reset()
            if now - lastAppearanceProbe > 0.5 {
                lastAppearanceProbe = now
                refreshThemeIfNeeded()
            }
        }
        refreshAutoShift()
        refreshReturnKey()
        refreshSuggestions()
    }

    // MARK: Hierarchy

    private func buildHierarchy() {
        keysView.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(actionBar)
        view.addSubview(keysView)

        let bar = actionBar.heightAnchor.constraint(equalToConstant: actionBar.compactHeight)
        let keys = keysView.heightAnchor.constraint(equalToConstant: 216)
        barHeightConstraint = bar
        keysHeightConstraint = keys

        NSLayoutConstraint.activate([
            actionBar.topAnchor.constraint(equalTo: view.topAnchor),
            actionBar.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            actionBar.trailingAnchor.constraint(equalTo: view.trailingAnchor),
            bar,
            keysView.topAnchor.constraint(equalTo: actionBar.bottomAnchor),
            keysView.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            keysView.trailingAnchor.constraint(equalTo: view.trailingAnchor),
            keys
        ])
    }

    /// Installs the height from the last idle layout before anything is laid
    /// out, so the first frame is already the right size.
    private func seedHeightFromCache() {
        guard heightConstraint == nil, let cached = SharedSettings.shared.keyboardHeight else { return }
        let constraint = view.heightAnchor.constraint(equalToConstant: CGFloat(cached.height))
        constraint.priority = UILayoutPriority(999)
        constraint.isActive = true
        heightConstraint = constraint
    }

    // MARK: Settings

    private func reloadSettings() {
        let settings = SharedSettings.shared
        let appLanguage = settings.effectiveAppLanguage
        if appLanguage != uiLanguage {
            uiLanguage = appLanguage
            replyCoordinator.uiLanguage = appLanguage
            composeCoordinator.uiLanguage = appLanguage
            actionBar.configure(theme: theme, uiLanguage: appLanguage)
            applyChips(from: replyCoordinator.configuration)
        }
        enabledLanguages = KeyboardLanguageStore.enabledLanguages(settings: settings)
        let stored = KeyboardLanguageStore.load(settings: settings)
        if !enabledLanguages.contains(language) || stored != language {
            language = stored
        }
    }

    /// Profile and personas: read once per appearance, off the main thread,
    /// never during a keypress.
    private func loadConfigurationIfNeeded() {
        let now = Date.timeIntervalSinceReferenceDate
        guard now - configurationLoadedAt > 1.0 else { return }
        configurationLoadedAt = now
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            let configuration = ProfileStore.shared.load()
            DispatchQueue.main.async {
                guard let self else { return }
                self.replyCoordinator.configuration = configuration
                self.composeCoordinator.grammaticalGender = configuration.profile.grammaticalGender
                self.applyChips(from: configuration)
            }
        }
    }

    /// Row chips from the full configuration, or - before it has loaded - from
    /// the compact summary the app keeps in the App Group. Only the personas
    /// the user shows in the keyboard; hidden ones stay in the app.
    private func applyChips(from configuration: ReplyConfiguration?) {
        let selected = SharedSettings.shared.lastTemplateID
        if let configuration {
            actionBar.setChips(
                configuration.visibleTemplates.map { TemplateChip(id: $0.id, name: $0.displayName(appLanguage: uiLanguage)) },
                selectedID: selected
            )
        } else if let summaries = SharedSettings.shared.templateSummaries {
            actionBar.setChips(
                summaries.map { TemplateChip(id: $0.id, name: $0.name(for: uiLanguage)) },
                selectedID: selected
            )
        } else {
            let defaults = ReplyConfiguration.initial.visibleTemplates
            actionBar.setChips(
                defaults.map { TemplateChip(id: $0.id, name: $0.displayName(appLanguage: uiLanguage)) },
                selectedID: selected
            )
        }
    }

    /// Lets the containing app show a truthful keyboard status: a throttled
    /// App Group heartbeat (it only lands with Full Access), then a Darwin
    /// notification, which gets through either way. Off the main thread, and
    /// in that order, so the app reads the fresh heartbeat when it is woken.
    private func reportActivityToContainingApp() {
        let fullAccess = hasFullAccess
        let settings = SharedSettings.shared
        let isStale = settings.keyboardLastSeen.map { Date().timeIntervalSince($0) > 300 } ?? true
        let needsHeartbeat = isStale || settings.keyboardHasFullAccess != fullAccess
        DispatchQueue.global(qos: .utility).async {
            if needsHeartbeat { settings.markKeyboardActive(hasFullAccess: fullAccess) }
            KeyboardPresence.post(hasFullAccess: fullAccess)
        }
    }

    // MARK: Theme

    private func refreshThemeIfNeeded() {
        let resolved = KeyboardTheme.resolve(
            appearance: textDocumentProxy.keyboardAppearance ?? .default,
            traits: traitCollection
        )
        guard resolved != theme else { return }
        applyTheme(resolved)
    }

    private func applyTheme(_ newTheme: KeyboardTheme) {
        theme = newTheme
        view.backgroundColor = theme.background
        view.tintColor = theme.primaryText
        actionBar.configure(theme: theme, uiLanguage: uiLanguage)
        keysView.configure(theme: theme)
    }

    // MARK: Layout

    private func refreshScreenHeight() {
        guard let bounds = view.window?.windowScene?.screen.bounds, bounds.height > 0 else { return }
        screenHeight = bounds.height
    }

    /// One height for every page of every enabled layout: switching between
    /// letters, 123 and #+=, or between ҚАЗ, РУС and ENG, never moves the
    /// host app's content.
    private var keyAreaHeight: CGFloat {
        guard let sizing else { return 216 }
        return sizing.keyAreaHeight(maximumRows: KeyboardLayout.maximumRowCount(languages: enabledLanguages))
    }

    private var pageOptions: KeyboardPageOptions {
        KeyboardPageOptions(
            showsNextKeyboardKey: showsGlobe,
            showsLanguageKey: enabledLanguages.count > 1,
            // The composer's fields are plain text whatever the host field is.
            field: actionBar.isComposing ? .text : KeyboardFieldKind(textDocumentProxy.keyboardType ?? .default)
        )
    }

    private func layoutKeyboard(force: Bool) {
        guard let sizing else { return }
        let identity = PageIdentity(
            language: language,
            plane: plane,
            options: pageOptions,
            sizing: sizing,
            areaHeight: keyAreaHeight
        )
        if force || identity != shownPage {
            shownPage = identity
            let spec = KeyboardLayout.page(language: language, plane: plane, options: identity.options)
            let layout = KeyboardGeometry.layout(page: spec, sizing: sizing, areaHeight: identity.areaHeight)
            UIView.performWithoutAnimation {
                keysView.show(layout, labels: KeyboardLabels(language), languages: enabledLanguages, currentLanguage: language)
            }
        }
        keysView.setShiftMode(plane == .letters ? shift.mode : .off)
        refreshReturnKey()
        updateGeometry(animated: false)
    }

    /// The tallest the AI area may become. The keys never give up height;
    /// the keyboard grows instead, but the conversation above it must stay
    /// legible, so the ceiling comes from the live screen height.
    private func maximumComposerHeight(keysHeight: CGFloat) -> CGFloat {
        var fraction: CGFloat
        switch screenHeight {
        case 850...: fraction = 0.56
        case 800..<850: fraction = 0.57
        case 700..<800: fraction = 0.61
        default: fraction = 0.66
        }
        if actionBar.wantsExpandedContext { fraction = min(0.72, fraction + 0.08) }
        return max(actionBar.compactHeight, (screenHeight * fraction - keysHeight).rounded(.down))
    }

    private func updateGeometry(animated: Bool) {
        guard let sizing else { return }
        let keysHeight = keyAreaHeight
        keysHeightConstraint?.constant = keysHeight

        // Width first, then the ceiling, then read what the bar settled on.
        actionBar.layout(forWidth: sizing.width)
        actionBar.setMaximumComposerHeight(maximumComposerHeight(keysHeight: keysHeight))
        let barHeight = actionBar.preferredHeight
        barHeightConstraint?.constant = barHeight
        keysView.headroom = barHeight

        let total = barHeight + keysHeight
        if !actionBar.isComposing {
            // Only the idle height is cached: seeding a launch with the
            // composer's height would open the keyboard oversized.
            let cached = SharedSettings.shared.keyboardHeight
            if cached?.height != Double(total) || cached?.width != Double(sizing.width) {
                let height = Double(total)
                let width = Double(sizing.width)
                DispatchQueue.global(qos: .utility).async {
                    SharedSettings.shared.setKeyboardHeight(height, width: width)
                }
            }
        }

        let changed: Bool
        if let constraint = heightConstraint {
            changed = abs(constraint.constant - total) > 0.5
            if changed { constraint.constant = total }
        } else {
            let constraint = view.heightAnchor.constraint(equalToConstant: total)
            // The system installs its own height constraint on the input
            // view; 999 wins against it without becoming unsatisfiable.
            constraint.priority = UILayoutPriority(999)
            constraint.isActive = true
            heightConstraint = constraint
            changed = true
        }

        if animated, changed {
            UIView.animate(withDuration: 0.18) { self.view.layoutIfNeeded() }
        }
    }

    // MARK: Input routing

    /// Where a keystroke goes. The keyboard never guesses between the host
    /// field and the composer: while the composer is open the keys edit ITS
    /// focused field, and while a request is running they go nowhere.
    private enum Target {
        case host
        case composer
        case nowhere
    }

    private var target: Target {
        guard actionBar.isComposing else { return .host }
        return actionBar.acceptsTextInput ? .composer : .nowhere
    }

    /// With a reply on screen, typing means "let me change it": the reply
    /// becomes editable and the key lands in it.
    private func prepareComposerForTyping() {
        // The report panel is not the reply: its keys type nothing.
        guard actionBar.isComposing, !actionBar.acceptsTextInput, !actionBar.isReporting else { return }
        switch activeFlow {
        case .compose: _ = composeCoordinator.beginEditingForTyping()
        case .reply: _ = replyCoordinator.beginEditingForTyping()
        case .none: break
        }
    }

    private func insert(_ text: String) {
        switch target {
        case .host:
            textDocumentProxy.insertText(text)
            var context = hostContext ?? ""
            context.append(text)
            hostContext = String(context.suffix(200))
            lastHostMutation = Date.timeIntervalSinceReferenceDate
        case .composer:
            actionBar.insertText(text)
        case .nowhere:
            break
        }
    }

    private func deleteCharacter() {
        switch target {
        case .host:
            textDocumentProxy.deleteBackward()
            if var context = hostContext, !context.isEmpty {
                context.removeLast()
                hostContext = context
            } else {
                hostContext = nil
            }
            lastHostMutation = Date.timeIntervalSinceReferenceDate
        case .composer:
            actionBar.deleteBackward()
        case .nowhere:
            break
        }
    }

    private func deleteWord() {
        switch target {
        case .host:
            // One fresh read per word: holding delete is a rare, deliberate
            // gesture, and deleting against a stale context would eat the
            // wrong amount of text.
            let context = textDocumentProxy.documentContextBeforeInput ?? ""
            let count = TextDeletion.wordLength(before: context)
            guard count > 0 else { return }
            for _ in 0..<count { textDocumentProxy.deleteBackward() }
            hostContext = String(context.dropLast(count).suffix(200))
            lastHostMutation = Date.timeIntervalSinceReferenceDate
        case .composer:
            actionBar.deleteWordBackward()
        case .nowhere:
            break
        }
    }

    private var textBeforeCursor: String? {
        switch target {
        case .host: return hostContext
        // With a reply on screen the next keystroke starts editing it, so
        // the reply's text decides the shift state, not "start of text".
        case .composer, .nowhere: return actionBar.textBeforeCursor
        }
    }

    private func refreshHostContext() {
        hostContext = (textDocumentProxy.documentContextBeforeInput).map { String($0.suffix(200)) }
        hostCaretInsideWord = AutocorrectCaret.continuesWord(textDocumentProxy.documentContextAfterInput)
        hostAutocapitalization = textDocumentProxy.autocapitalizationType ?? .sentences
        hostAllowsCorrection = AutocorrectFieldPolicy.allowsCorrection(in: textDocumentProxy)
    }

    private var currentHostField: HostFieldIdentity {
        HostFieldIdentity(documentIdentifier: textDocumentProxy.documentIdentifier,
                          keyboardType: textDocumentProxy.keyboardType)
    }

    /// The caret is in another host field now. Its text and traits are
    /// re-read by `textDidChange` like any change from elsewhere; here the
    /// strip and the double-space shortcut forget the old field, and -
    /// unless the composer owns the keys - the page starts where the new
    /// field wants it (digits for a code). The page is rebuilt only if it
    /// really changed.
    private func hostFieldDidChange() {
        autocorrect.restart()
        lastSpaceTap = 0
        guard !actionBar.isComposing else { return }
        let startsOnNumbers = KeyboardFieldKind.startsOnNumbers(textDocumentProxy.keyboardType ?? .default)
        plane = startsOnNumbers ? .numbers : .letters
        if startsOnNumbers { shift.reset() }
        layoutKeyboard(force: false)
    }

    // MARK: Smart correction

    /// Whether the keys are typing where smart correction may help: a host
    /// field that is not an address, a code or a password, or the composer's
    /// instruction - never the copied message or a reply being edited. And
    /// only for a word being typed right now, with nothing of it after the
    /// caret: correcting the half before the caret would split the word.
    private var correctionAllowed: Bool {
        switch target {
        case .host:
            return hostAllowsCorrection && wordTyping.allowsCorrection(caretInsideWord: hostCaretInsideWord)
        case .composer:
            return actionBar.correctsFocusedField
                && wordTyping.allowsCorrection(caretInsideWord: AutocorrectCaret.continuesWord(actionBar.textAfterCursor))
        case .nowhere:
            return false
        }
    }

    /// Asks for the strip of the word now before the caret. Cheap when that
    /// word did not change; the dictionaries are searched off the main thread.
    private func refreshSuggestions() {
        autocorrect.textDidChange(before: textBeforeCursor, allowed: correctionAllowed)
    }

    /// At a separator: the word before the caret swapped for its correction,
    /// when one is ready. Returns it, so a backspace can take it back.
    private func correctWordBeforeSeparator() -> AutocorrectCorrection? {
        guard let correction = autocorrect.correction(before: textBeforeCursor, allowed: correctionAllowed),
              replaceBeforeCursor(correction.original, with: correction.replacement, endsWord: true) else { return nil }
        return correction
    }

    /// Replaces `old`, which ends right at the caret, with `new` - through
    /// the same paths as typing, so the host mirror stays in step. With
    /// `endsWord`, `old` is a word that must end at the caret.
    ///
    /// In another app's field the live text is read once more first: the
    /// mirror can be a moment behind (a quick tap elsewhere, a field that
    /// turned Return into an action), and deleting against it would delete
    /// the wrong text. One read, only when a correction, an Undo or a strip
    /// pick is about to change the text - never on a plain keystroke.
    @discardableResult
    private func replaceBeforeCursor(_ old: String, with new: String, endsWord: Bool) -> Bool {
        switch target {
        case .host:
            guard let live = confirmedHostText(endingWith: old, endsWord: endsWord) else { return false }
            for _ in 0..<old.count { textDocumentProxy.deleteBackward() }
            textDocumentProxy.insertText(new)
            hostContext = String((String(live.dropLast(old.count)) + new).suffix(200))
            lastHostMutation = Date.timeIntervalSinceReferenceDate
            return true
        case .composer:
            return actionBar.replaceBeforeCursor(length: (old as NSString).length, with: new)
        case .nowhere:
            return false
        }
    }

    /// The host's live text before the caret, when it still ends with
    /// `expected` (and, for `endsWord`, no word character follows the
    /// caret). Otherwise nil, and the keyboard catches up with what the user
    /// did: the mirror is re-read, the pending Undo dropped, and no word is
    /// being typed at this caret.
    private func confirmedHostText(endingWith expected: String, endsWord: Bool) -> String? {
        let before = textDocumentProxy.documentContextBeforeInput
        let after = endsWord ? textDocumentProxy.documentContextAfterInput : nil
        if AutocorrectCaret.liveText(before: before, after: after, endsWith: expected, requiresWordEnd: endsWord),
           let before {
            return before
        }
        refreshHostContext()
        autocorrect.keepCorrection()
        wordTyping.reset()
        autocorrect.restart()
        return nil
    }

    /// Typed after a word, these end it and apply its correction.
    private static let wordSeparators: Set<String> = [".", ",", "!", "?", ";", ":"]

    /// The flag the server published, or the Simulator's mock.
    private var serverOffersPolish: Bool {
        #if DEBUG
        if DebugReplyMock.isEnabled { return true }
        #endif
        return AILimits.serverSupportsInstructionPolish
    }

    /// The instruction changed: the polish suggestion starts over.
    private func instructionDidChange(_ text: String, limit: Int) {
        polisher.instructionDidChange(text, inputLanguage: language, limit: limit)
    }

    // MARK: Shift and return

    /// Auto-capitalisation: shift turns itself on at a sentence start and an
    /// automatic shift turns itself off when the caret leaves one. A shift the
    /// user set by hand is never overridden.
    private func refreshAutoShift() {
        guard plane == .letters else { return }
        let before = textBeforeCursor
        // While nothing can be typed (a request is running) the keys keep
        // their case instead of flipping to capitals.
        if target == .nowhere, before == nil { return }
        let type: UITextAutocapitalizationType = target == .host ? hostAutocapitalization : .sentences
        let should = AutoCapitalization.shouldCapitalize(before: before, type: type)
        shift.applyAutomatic(should)
        keysView.setShiftMode(shift.mode)
    }

    private func refreshReturnKey() {
        let labels = KeyboardLabels(language)
        if actionBar.isComposing {
            keysView.setReturnKey(labels.returnKey(for: .default), prominent: false, enabled: target == .composer)
            return
        }
        let type = textDocumentProxy.returnKeyType ?? .default
        // `enablesReturnKeyAutomatically`: iOS greys return out while the
        // field is empty, and so does this keyboard.
        let enabled = !(textDocumentProxy.enablesReturnKeyAutomatically ?? false) || textDocumentProxy.hasText
        keysView.setReturnKey(
            labels.returnKey(for: type),
            prominent: KeyboardLabels.returnKeyIsProminent(type),
            enabled: enabled
        )
    }

    // MARK: Layout switching

    private func switchLanguage(to next: KeyboardLanguage) {
        guard next != language || plane != .letters else { return }
        language = next
        KeyboardLanguageStore.saveAsync(next)
        plane = .letters
        shift.reset()
        autocorrect.switchLanguage(to: next)
        layoutKeyboard(force: true)
        refreshAutoShift()
        refreshSuggestions()
        flashLanguageName()
    }

    /// The layout's own name on the space bar for a moment, the way iOS
    /// confirms a switch.
    private func flashLanguageName() {
        spaceCaptionReset?.cancel()
        keysView.setSpaceCaption(language.nativeName)
        let reset = DispatchWorkItem { [weak self] in self?.keysView.setSpaceCaption(nil) }
        spaceCaptionReset = reset
        DispatchQueue.main.asyncAfter(deadline: .now() + 1.2, execute: reset)
    }

    // MARK: Composer

    private var aiStrings: AIReplyStrings { AIReplyStrings.forLanguage(uiLanguage) }

    private func renderComposer() {
        if let compose = composeCoordinator.session {
            renderCompose(compose)
            return
        }
        guard let session = replyCoordinator.session, replyCoordinator.isComposing else { return }
        let flow = session.flow
        let message = flow.error.map { aiStrings.message(for: $0) }
        actionBar.render(ReplyComposerView.Content(
            personaName: session.template.displayName(appLanguage: uiLanguage),
            source: session.sourceMessage,
            instruction: session.instruction,
            flow: flow,
            errorMessage: (message?.isEmpty ?? true) ? nil : message,
            sourceLimit: AILimits.current.sourceCharacters,
            instructionLimit: ReplyInstruction.maximumCharacters,
            offersReport: offersReport
        ))
        keysView.isInputDimmed = flow.isGenerating || actionBar.isReporting
        if flow.stage != .composing { polisher.stop() }
        refreshReturnKey()
        updateGeometry(animated: true)
        restartSuggestions()
    }

    /// The field being typed into may have changed: the strip is asked for
    /// again where the keys now type, once a word is typed there.
    private func restartSuggestions() {
        wordTyping.reset()
        autocorrect.restart()
        refreshSuggestions()
    }

    /// Create: no persona and no message, only the instruction and the
    /// versions - the same panel in its compose mode.
    private func renderCompose(_ session: ComposeSession) {
        let flow = session.flow
        let notice = session.notice(aiStrings)
        actionBar.render(ReplyComposerView.Content(
            personaName: "",
            source: "",
            instruction: session.instruction,
            flow: flow,
            errorMessage: notice?.text,
            sourceLimit: AILimits.current.sourceCharacters,
            instructionLimit: AILimits.current.instructionCharacters,
            mode: .compose,
            errorOffersRetry: notice?.offersRetry ?? true,
            offersReport: offersReport
        ))
        keysView.isInputDimmed = flow.isGenerating || actionBar.isReporting
        if flow.stage != .composing { polisher.stop() }
        refreshReturnKey()
        updateGeometry(animated: true)
        restartSuggestions()
    }

    private func showComposePanel() {
        actionBar.beginComposing()
        shift.reset()
        layoutKeyboard(force: false)
        renderComposer()
        refreshAutoShift()
    }

    private func closeComposer() {
        polisher.stop()
        replyCoordinator.clear()
        composeCoordinator.clear()
        actionBar.endComposing()
        keysView.isInputDimmed = false
        layoutKeyboard(force: false)
        updateGeometry(animated: true)
        refreshHostContext()
        refreshAutoShift()
        restartSuggestions()
    }

    private func insertReply() {
        let hostHasText = hostFieldHasText()
        let decision = activeFlow == .compose
            ? composeCoordinator.requestInsert(hostHasText: hostHasText)
            : replyCoordinator.requestInsert(hostHasText: hostHasText)
        switch decision {
        case .insert(let text):
            finishInsert(text)
        case .askAboutExistingText, .nothing:
            break
        }
    }

    private func resolveConflict(_ choice: ReplyComposerFlow.ConflictChoice) {
        let resolution = activeFlow == .compose
            ? composeCoordinator.resolveConflict(choice)
            : replyCoordinator.resolveConflict(choice)
        switch resolution {
        case .replace(let text):
            clearHostField()
            finishInsert(text)
        case .append(let text):
            finishInsert(separatorForAppend() + text)
        case .cancelled:
            break
        }
    }

    /// The one place a reply reaches the host application. It is typed into
    /// the field and nothing more: sending stays the user's decision.
    private func finishInsert(_ text: String) {
        textDocumentProxy.insertText(text)
        lastHostMutation = Date.timeIntervalSinceReferenceDate
        closeComposer()
    }

    /// Conservative: a host can legitimately report nothing, and treating
    /// "no context" as "empty" only means inserting into a field that was
    /// already empty.
    private func hostFieldHasText() -> Bool {
        let before = textDocumentProxy.documentContextBeforeInput ?? ""
        let after = textDocumentProxy.documentContextAfterInput ?? ""
        return !(before + after).trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    private func separatorForAppend() -> String {
        guard let last = (textDocumentProxy.documentContextBeforeInput ?? "").last else { return "" }
        return last.isWhitespace ? "" : " "
    }

    /// Empties the host field for Replace.
    ///
    /// `deleteBackward()` is the only deletion a keyboard has, so the caret is
    /// first moved to the end - otherwise the text after it would survive -
    /// and the context is re-read once per batch, not once per character. The
    /// round limit is deliberate: a runaway loop in another app's field is
    /// worse than a few characters left behind.
    private func clearHostField() {
        let after = textDocumentProxy.documentContextAfterInput ?? ""
        if !after.isEmpty {
            textDocumentProxy.adjustTextPosition(byCharacterOffset: (after as NSString).length)
        }
        var rounds = 0
        while rounds < 12 {
            let before = textDocumentProxy.documentContextBeforeInput ?? ""
            if before.isEmpty { break }
            for _ in 0..<min(before.count, 500) { textDocumentProxy.deleteBackward() }
            rounds += 1
        }
        hostContext = ""
    }

    // MARK: Debug

    private static func makeService() -> AIReplyService {
        #if DEBUG
        if DebugReplyMock.isEnabled {
            return AIReplyService(transportOverride: { request, _ in DebugReplyMock(instruction: request.instruction) })
        }
        #endif
        return AIReplyService()
    }

    private static func makeComposeService() -> ComposeService {
        #if DEBUG
        if DebugReplyMock.isEnabled {
            return ComposeService(transportOverride: { _ in DebugComposeMock() })
        }
        #endif
        return ComposeService()
    }

    private static func makePolishService() -> PolishService {
        #if DEBUG
        if DebugReplyMock.isEnabled {
            return PolishService(transportOverride: DebugPolishMock())
        }
        #endif
        return PolishService()
    }

    /// Without Full Access a keyboard has no network at all, so a request
    /// would only fail as "offline". Said plainly instead, before any request.
    private var canReachNetwork: Bool {
        #if DEBUG
        if DebugReplyMock.isEnabled { return true }
        #endif
        return hasFullAccess
    }

    // MARK: Reports

    /// Report under a result: a server that takes reports, and a network
    /// to send one over.
    private var offersReport: Bool {
        #if DEBUG
        if DebugReplyMock.isEnabled { return true }
        #endif
        return serverTakesReports && canReachNetwork
    }

    /// Sends a report about the text on screen; the panel says how it went.
    private func sendReport(_ reason: AIReport.Reason, text: String?, mode: AIReport.Mode) {
        #if DEBUG
        if DebugReplyMock.isEnabled {
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.4) { [weak self] in
                self?.actionBar.reportDidFinish(sent: true)
            }
            return
        }
        #endif
        let report = AIReport(mode: mode, reason: reason, text: text)
        Task { [weak self] in
            let sent = (try? await AccountService().reportAIOutput(report)) != nil
            self?.actionBar.reportDidFinish(sent: sent)
        }
    }

    /// The report panel opened or closed: the keys are dimmed and type
    /// nothing while it is open.
    private func reportingDidChange() {
        let generating = activeFlow == .compose ? composeCoordinator.flow.isGenerating : replyCoordinator.flow.isGenerating
        keysView.isInputDimmed = generating || actionBar.isReporting
        refreshReturnKey()
        refreshAutoShift()
        restartSuggestions()
    }
}

// MARK: - Keys

extension KeyboardViewController: KeyboardKeysViewDelegate {

    func keysView(_ view: KeyboardKeysView, didType text: String) {
        prepareComposerForTyping()
        autocorrect.keepCorrection()
        let isLetter = plane == .letters && text.count == 1 && (text.first?.isLetter ?? false)
        let corrected = Self.wordSeparators.contains(text) ? correctWordBeforeSeparator() : nil
        insert(isLetter ? shift.apply(to: text) : text)
        if target != .nowhere { wordTyping.typed(text) }
        if let corrected { autocorrect.didApply(corrected, separator: text) }
        if plane == .letters {
            shift.characterTyped()
        } else if text == "'" {
            // As on iOS: an apostrophe from 123 goes back to letters, so
            // "don't" does not need a second switch.
            plane = .letters
            layoutKeyboard(force: false)
        }
        refreshAutoShift()
        refreshSuggestions()
    }

    func keysViewDidTapShift(_ view: KeyboardKeysView) {
        guard plane == .letters else { return }
        shift.tap(at: Date.timeIntervalSinceReferenceDate)
        keysView.setShiftMode(shift.mode)
    }

    func keysView(_ view: KeyboardKeysView, didDelete unit: KeyboardKeysView.DeleteUnit) {
        prepareComposerForTyping()
        // Backspace right after a correction takes it back: the word returns
        // as typed, without the separator, and is not corrected again. When
        // the live text no longer ends with the correction - a single-line
        // field that turned Return into an action, a caret moved a moment
        // ago - it is an ordinary backspace.
        if unit == .character, autocorrect.takeBackCorrection(before: textBeforeCursor, replace: { applied in
            self.replaceBeforeCursor(applied.correction.replacement + applied.separator,
                                     with: applied.restoredText, endsWord: false)
        }) {
            refreshAutoShift()
            refreshSuggestions()
            return
        }
        autocorrect.keepCorrection()
        switch unit {
        case .character: deleteCharacter()
        case .word: deleteWord()
        }
        refreshAutoShift()
        refreshSuggestions()
    }

    func keysViewDidTapSpace(_ view: KeyboardKeysView) {
        prepareComposerForTyping()
        autocorrect.keepCorrection()
        let now = Date.timeIntervalSinceReferenceDate
        if SpaceShortcut.shouldInsertPeriod(before: textBeforeCursor, secondsSinceLastSpace: now - lastSpaceTap) {
            deleteCharacter()
            insert(". ")
            lastSpaceTap = 0
        } else {
            let corrected = correctWordBeforeSeparator()
            insert(" ")
            if let corrected { autocorrect.didApply(corrected, separator: " ") }
            lastSpaceTap = now
        }
        refreshAutoShift()
        refreshSuggestions()
    }

    func keysViewDidTapReturn(_ view: KeyboardKeysView) {
        prepareComposerForTyping()
        autocorrect.keepCorrection()
        let corrected = correctWordBeforeSeparator()
        insert("\n")
        if let corrected { autocorrect.didApply(corrected, separator: "\n") }
        refreshAutoShift()
        refreshSuggestions()
    }

    func keysView(_ view: KeyboardKeysView, didSelectPlane selected: KeyboardPlane) {
        plane = selected
        if selected != .letters { shift.reset() }
        layoutKeyboard(force: false)
        refreshAutoShift()
    }

    func keysViewDidTapNextLanguage(_ view: KeyboardKeysView) {
        switchLanguage(to: language.next(in: enabledLanguages))
    }

    func keysView(_ view: KeyboardKeysView, didPickLanguage picked: KeyboardLanguage) {
        switchLanguage(to: picked)
    }

    /// The system globe: tap for the next keyboard, hold for the list. iOS
    /// needs every touch event to tell the two apart.
    func keysView(_ view: KeyboardKeysView, didSendNextKeyboardEvent event: UIEvent, from keyView: UIView) {
        handleInputModeList(from: keyView, with: event)
    }

    func keysViewDidRequestNextKeyboard(_ view: KeyboardKeysView) {
        advanceToNextInputMode()
    }

    func keysView(_ view: KeyboardKeysView, moveCursorBy offset: Int) {
        autocorrect.keepCorrection()
        wordTyping.reset()
        switch target {
        case .host:
            // The context is re-read once the host reports the move; the
            // strip goes now - it was for the word at the old caret.
            textDocumentProxy.adjustTextPosition(byCharacterOffset: offset)
            lastHostMutation = 0
            refreshSuggestions()
        case .composer:
            actionBar.moveCaret(by: offset)
            refreshSuggestions()
        case .nowhere:
            break
        }
    }
}

// MARK: - Action bar

extension KeyboardViewController: KeyboardActionBarDelegate {

    /// Picking a persona OPENS THE COMPOSER. It is not a network request.
    func actionBar(_ bar: KeyboardActionBar, didSelectTemplateID id: String) {
        guard let template = resolveTemplate(id: id) else { return }
        composeCoordinator.clear()
        DispatchQueue.global(qos: .utility).async {
            SharedSettings.shared.setLastTemplateID(id)
        }
        replyCoordinator.open(template: template, proxy: textDocumentProxy, hasFullAccess: hasFullAccess)
    }

    private func resolveTemplate(id: String) -> ReplyTemplate? {
        if let template = replyCoordinator.configuration.template(id: id) { return template }
        // The narrow race where a chip is tapped before the configuration
        // finished loading: one file read, on a tap, never on a keystroke.
        let configuration = ProfileStore.shared.load()
        replyCoordinator.configuration = configuration
        return configuration.template(id: id)
    }

    /// Create: a fresh, empty composer for writing a new message. The
    /// clipboard is not read, and a reply the user had put aside (persona
    /// row over a suspended session) is dropped rather than carried over.
    func actionBarDidRequestCompose(_ bar: KeyboardActionBar) {
        replyCoordinator.clear()
        composeCoordinator.open()
        showComposePanel()
    }

    func actionBarDidChangeHeight(_ bar: KeyboardActionBar) {
        updateGeometry(animated: true)
    }

    /// A word of the strip: it replaces the word being typed, then a space.
    /// The quoted word keeps what was typed and teaches it to the keyboard.
    ///
    /// Only for the word the strip was offered for, still being typed at the
    /// caret: a strip left over from before a caret move, or one a newer
    /// answer is about to replace, must not replace another word.
    func actionBar(_ bar: KeyboardActionBar, didPick suggestion: AutocorrectSuggestion) {
        guard correctionAllowed,
              let word = textBeforeCursor.flatMap(TypedWord.init(before:)),
              autocorrect.offersSuggestions(for: word) else {
            autocorrect.restart()
            refreshSuggestions()
            return
        }
        autocorrect.keepCorrection()
        switch suggestion.kind {
        case .typed:
            if target == .host {
                // Nothing is replaced, but a word is learned and a space
                // typed: only for the word really at the caret.
                guard let live = confirmedHostText(endingWith: word.text, endsWord: true) else {
                    refreshAutoShift()
                    refreshSuggestions()
                    return
                }
                hostContext = String(live.suffix(200))
            }
            autocorrect.keep(word.text)
        case .correction, .word:
            guard replaceBeforeCursor(word.text, with: suggestion.text, endsWord: true) else {
                refreshAutoShift()
                refreshSuggestions()
                return
            }
        }
        UIDevice.current.playInputClick()
        insert(" ")
        // A space typed right after this is a space, not the full-stop shortcut.
        lastSpaceTap = 0
        refreshAutoShift()
        refreshSuggestions()
    }

    func actionBarDidAcceptPolish(_ bar: KeyboardActionBar) {
        guard let text = polisher.accept(replacing: bar.instructionText) else { return }
        bar.replaceInstruction(with: text)
        // A whole new text, not a word being typed: no strip for its last word.
        wordTyping.reset()
        refreshAutoShift()
        refreshSuggestions()
    }

    func actionBarDidUndoPolish(_ bar: KeyboardActionBar) {
        guard let text = polisher.undo() else { return }
        bar.replaceInstruction(with: text)
        wordTyping.reset()
        refreshAutoShift()
        refreshSuggestions()
    }

    /// A tap put the composer's caret somewhere else (or in another field):
    /// the strip was for the word at the old caret, and Undo for a correction
    /// there no longer applies.
    func actionBarDidMoveComposerCaret(_ bar: KeyboardActionBar) {
        autocorrect.keepCorrection()
        wordTyping.reset()
        autocorrect.restart()
        refreshAutoShift()
        refreshSuggestions()
    }

    func actionBar(_ bar: KeyboardActionBar, didSend event: ComposerEvent) {
        if activeFlow == .compose {
            handleCompose(event)
            return
        }
        switch event {
        case .close:
            closeComposer()
        case .changePersona:
            // Keep everything; show the persona row so another can be picked.
            polisher.stop()
            replyCoordinator.suspend()
            actionBar.endComposing()
            keysView.isInputDimmed = false
            applyChips(from: replyCoordinator.configuration)
            updateGeometry(animated: true)
            restartSuggestions()
        case .paste:
            replyCoordinator.pasteSource(proxy: textDocumentProxy, hasFullAccess: hasFullAccess)
        case .generate, .regenerate:
            // A selection can fill the message without Full Access, but the
            // request still needs the network: say so, not "offline".
            guard canReachNetwork else {
                replyCoordinator.showError(.fullAccessRequired)
                return
            }
            polisher.stop()
            replyCoordinator.inputLanguage = language
            replyCoordinator.generate()
        case .stop:
            replyCoordinator.cancelGeneration()
        case .back:
            replyCoordinator.back()
        case .toggleEditing:
            if replyCoordinator.flow.stage == .editing {
                replyCoordinator.endEditing()
            } else {
                replyCoordinator.beginEditing()
            }
        case .tapReply:
            replyCoordinator.beginEditing()
        case .insert:
            insertReply()
        case .previousVersion:
            replyCoordinator.showPreviousVersion()
        case .nextVersion:
            replyCoordinator.showNextVersion()
        case .resolveConflict(let choice):
            resolveConflict(choice)
        case .edited(let field, let text):
            switch field {
            case .source: replyCoordinator.updateSource(text)
            case .instruction:
                replyCoordinator.updateInstruction(text)
                instructionDidChange(text, limit: ReplyInstruction.maximumCharacters)
            case .draft: replyCoordinator.updateDraft(text)
            case .none: break
            }
        case .report(let reason, let text):
            sendReport(reason, text: text, mode: .reply)
        case .reportingChanged:
            reportingDidChange()
        case .reset, .replyToCopied:
            break
        }
    }

    private func handleCompose(_ event: ComposerEvent) {
        switch event {
        case .close:
            closeComposer()
        case .reset:
            polisher.stop()
            composeCoordinator.reset()
        case .generate, .regenerate:
            guard canReachNetwork else {
                composeCoordinator.showError(.fullAccessRequired)
                return
            }
            polisher.stop()
            composeCoordinator.inputLanguage = language
            composeCoordinator.generate()
        case .stop:
            composeCoordinator.cancelGeneration()
        case .back:
            composeCoordinator.back()
        case .toggleEditing:
            if composeCoordinator.flow.stage == .editing {
                composeCoordinator.endEditing()
            } else {
                composeCoordinator.beginEditing()
            }
        case .tapReply:
            composeCoordinator.beginEditing()
        case .insert:
            insertReply()
        case .previousVersion:
            composeCoordinator.showPreviousVersion()
        case .nextVersion:
            composeCoordinator.showNextVersion()
        case .resolveConflict(let choice):
            resolveConflict(choice)
        case .edited(let field, let text):
            switch field {
            case .instruction:
                composeCoordinator.updateInstruction(text)
                instructionDidChange(text, limit: AILimits.current.instructionCharacters)
            case .draft: composeCoordinator.updateDraft(text)
            case .source, .none: break
            }
        case .replyToCopied:
            replyToCopiedFromCreate()
        case .report(let reason, let text):
            sendReport(reason, text: text, mode: .compose)
        case .reportingChanged:
            reportingDidChange()
        case .changePersona, .paste:
            // Create has no persona and no copied message.
            break
        }
    }

    /// "Reply to copied" in Create: the copied message is read now - this
    /// tap is the user's gesture, as a persona tap is - and the panel
    /// becomes the reply composer with it, the persona used last and the
    /// instruction typed here. Nothing is generated: Reply is still the
    /// user's tap. Nothing copied, or no Full Access: Create stays and
    /// says so.
    private func replyToCopiedFromCreate() {
        let lastUsed = SharedSettings.shared.lastTemplateID
        // A persona the configuration has not loaded yet is looked up
        // once, on this tap.
        if let lastUsed { _ = resolveTemplate(id: lastUsed) }
        let persona = replyCoordinator.configuration.personaForCopiedReply(lastUsedID: lastUsed)
        let proxy = textDocumentProxy
        let fullAccess = hasFullAccess
        guard let reply = composeCoordinator.replyToCopied(
            persona: persona,
            instructionLimit: ReplyInstruction.maximumCharacters,
            read: { replyCoordinator.readCopiedMessage(proxy: proxy, hasFullAccess: fullAccess) }
        ) else { return }
        polisher.stop()
        replyCoordinator.open(handedOff: reply)
    }
}

// MARK: - Reply flow

extension KeyboardViewController: ReplyFlowCoordinatorDelegate {

    func coordinator(_ coordinator: ReplyFlowCoordinator, didOpen session: ReplySession) {
        actionBar.beginComposing()
        shift.reset()
        layoutKeyboard(force: false)
        renderComposer()
        refreshAutoShift()
    }

    func coordinatorDidChange(_ coordinator: ReplyFlowCoordinator) {
        renderComposer()
        refreshAutoShift()
    }
}

// MARK: - Create flow

extension KeyboardViewController: ComposeFlowCoordinatorDelegate {

    func composeCoordinatorDidChange(_ coordinator: ComposeFlowCoordinator) {
        renderComposer()
        refreshAutoShift()
    }
}

// MARK: - Smart correction

extension KeyboardViewController: AutocorrectControllerDelegate {

    func autocorrect(_ controller: AutocorrectController, didUpdate suggestions: [AutocorrectSuggestion]) {
        actionBar.showSuggestions(suggestions)
    }
}
