package kz.yerek.aireply.keyboard

import android.inputmethodservice.InputMethodService
import android.os.Build
import android.os.SystemClock
import android.text.InputType
import android.view.View
import android.view.inputmethod.EditorInfo
import android.view.inputmethod.InputMethodManager
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.ComposeView
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.ViewCompositionStrategy
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExecutorCoroutineDispatcher
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.asCoroutineDispatcher
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kz.yerek.aireply.AIReplyApplication
import kz.yerek.aireply.BuildConfig
import kz.yerek.aireply.R
import kz.yerek.aireply.ServiceLocator
import kz.yerek.aireply.ai.AILimits
import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.ai.AccountPolishTransport
import kz.yerek.aireply.ai.AppStrings
import kz.yerek.aireply.ai.DebugPolishMock
import kz.yerek.aireply.ai.PolishService
import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.core.lang.KeyboardPlane
import kz.yerek.aireply.core.lang.TemplateNaming
import kz.yerek.aireply.data.settings.AppearancePreference
import kz.yerek.aireply.domain.model.ReplyConfiguration
import kz.yerek.aireply.domain.model.TemplateSummary
import kz.yerek.aireply.keyboard.autocorrect.AssetDictionarySource
import kz.yerek.aireply.keyboard.autocorrect.AutocorrectController
import kz.yerek.aireply.keyboard.autocorrect.AutocorrectDictionaries
import kz.yerek.aireply.keyboard.autocorrect.AutocorrectEngine
import kz.yerek.aireply.keyboard.autocorrect.PrefsLearnedWordsStore
import kz.yerek.aireply.keyboard.autocorrect.Suggestion
import kz.yerek.aireply.keyboard.input.ContextTextProvider
import kz.yerek.aireply.keyboard.input.HostField
import kz.yerek.aireply.keyboard.input.KeyboardStatus
import kz.yerek.aireply.keyboard.input.KeyboardTextFieldState
import kz.yerek.aireply.keyboard.layout.AutoCapitalization
import kz.yerek.aireply.keyboard.layout.Capitalization
import kz.yerek.aireply.keyboard.layout.FieldKind
import kz.yerek.aireply.keyboard.layout.KeyboardGeometry
import kz.yerek.aireply.keyboard.layout.KeyboardLabels
import kz.yerek.aireply.keyboard.layout.KeyboardLayout
import kz.yerek.aireply.keyboard.layout.KeyboardSizing
import kz.yerek.aireply.keyboard.layout.PageOptions
import kz.yerek.aireply.keyboard.layout.ReturnFace
import kz.yerek.aireply.keyboard.layout.ShiftState
import kz.yerek.aireply.keyboard.layout.SpaceShortcut
import kz.yerek.aireply.keyboard.reply.ComposeSessionController
import kz.yerek.aireply.keyboard.reply.InstructionPolish
import kz.yerek.aireply.keyboard.reply.ReplyComposerFlow
import kz.yerek.aireply.keyboard.reply.ReplySessionController
import kz.yerek.aireply.keyboard.ui.ComposerActions
import kz.yerek.aireply.keyboard.ui.ComposerModel
import kz.yerek.aireply.keyboard.ui.ComposerPanel
import kz.yerek.aireply.keyboard.ui.CreateActions
import kz.yerek.aireply.keyboard.ui.CreateModel
import kz.yerek.aireply.keyboard.ui.CreatePanel
import kz.yerek.aireply.keyboard.ui.KeySurfaceController
import kz.yerek.aireply.keyboard.ui.KeySurfaceListener
import kz.yerek.aireply.keyboard.ui.KeySurfaceState
import kz.yerek.aireply.keyboard.ui.KeyboardRoot
import kz.yerek.aireply.keyboard.ui.PanelFocus
import kz.yerek.aireply.keyboard.ui.PersonaRow
import kz.yerek.aireply.keyboard.ui.QuickIntent
import kz.yerek.aireply.keyboard.ui.TypingAssist
import kz.yerek.aireply.keyboard.ui.TypingAssistActions
import kz.yerek.aireply.platform.ReplyLog
import kz.yerek.aireply.ui.design.AIReplyTheme
import kz.yerek.aireply.voice.AndroidSpeechRecognitionClient
import kz.yerek.aireply.voice.MicPermission
import kz.yerek.aireply.voice.SpeechRecognitionClient
import kz.yerek.aireply.voice.VoiceState
import java.util.concurrent.Executors

/**
 * The AI Reply keyboard.
 *
 * WHAT IT PROMISES, and what every decision below is in service of:
 *
 *  * Typing never waits for anything. No disk read, no network call and no
 *    JSON parse happens on a key press; the key area is one canvas that a
 *    keystroke only redraws.
 *  * Native layouts (ҚАЗ / РУС / ENG), one keyboard height for all of them,
 *    and no dead zones between keys.
 *  * A reply or a message is only ever requested by the user tapping Reply,
 *    Write, Regenerate or Try again. Appearing, copying and typing start
 *    nothing - with one opt-out exception: after a pause in an instruction
 *    the user is writing, a cleaner version of THAT instruction may be
 *    suggested (smart correction on, server offers it). It spends no quota
 *    and never touches the copied message or a reply.
 *  * Smart correction is local: the dictionaries are on the phone, and the
 *    words typed into other apps never leave it.
 *  * The clipboard is read only on an explicit tap (a persona, or Paste).
 *  * The user's words are never lost: an edited reply survives Regenerate,
 *    a failure keeps the instruction, and Insert puts exactly the text on
 *    screen into the field - after asking, if the field has text already.
 *  * The messenger's own Send button stays under the user's finger. This
 *    service inserts text and does nothing else.
 */
class ReplyKeyboardService : InputMethodService(), KeySurfaceListener {

    private lateinit var services: ServiceLocator
    private lateinit var viewHost: KeyboardViewHost
    private lateinit var hostField: HostField
    private lateinit var feedback: KeyFeedback
    private lateinit var replies: ReplySessionController
    private lateinit var compose: ComposeSessionController
    private lateinit var surface: KeySurfaceController
    private lateinit var autocorrect: AutocorrectController
    private lateinit var polish: InstructionPolish

    /** One thread for suggestions, so typing never waits for them. */
    private val suggestionWorker: ExecutorCoroutineDispatcher =
        Executors.newSingleThreadExecutor { work -> Thread(work, "autocorrect").apply { isDaemon = true } }
            .asCoroutineDispatcher()

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val contextProvider = ContextTextProvider()

    private var speech: SpeechRecognitionClient? = null
    private var voiceJob: Job? = null
    private var permissionJob: Job? = null

    private var inputView: ComposeView? = null

    // ------------------------------------------------------------------ state
    // Compose state, read by the composition. Small, and - except for shift -
    // unchanged by ordinary typing.

    private var language by mutableStateOf(KeyboardLanguage.KAZAKH)
    private var enabledLanguages by mutableStateOf(KeyboardLanguage.CYCLE_ORDER)
    private var plane by mutableStateOf(KeyboardPlane.LETTERS)
    private val shift = ShiftState()
    private var shiftMode by mutableStateOf(ShiftState.Mode.OFF)
    private var uiLanguage by mutableStateOf(AppLanguage.ENGLISH)
    private var appearance by mutableStateOf(AppearancePreference.SYSTEM)
    private var chips by mutableStateOf<List<TemplateSummary>>(emptyList())
    private var selectedTemplateId by mutableStateOf<String?>(null)
    private var focus by mutableStateOf(PanelFocus.INSTRUCTION)
    private var sourceExpanded by mutableStateOf(false)
    private var voiceState by mutableStateOf<VoiceState>(VoiceState.Idle)
    private var showsGlobeKey by mutableStateOf(false)
    private var isSecureField by mutableStateOf(false)
    private var returnFace by mutableStateOf(ReturnFace.NEWLINE)
    private var fieldKind by mutableStateOf(FieldKind.TEXT)

    private var hostCapitalization = Capitalization.SENTENCES
    private var smartCorrection = true
    private var configurationLoadedAt = 0L
    private var lastSpaceTap = 0L

    // -------------------------------------------------------------- lifecycle

    override fun onCreate() {
        super.onCreate()
        services = AIReplyApplication.services(this)
        viewHost = KeyboardViewHost(this)
        viewHost.onCreate()
        hostField = HostField { currentInputConnection }
        feedback = KeyFeedback(this)
        feedback.hapticsEnabled = services.settings.keyboardHaptics
        replies = ReplySessionController(scope, services.replyService, services.draftNormalizer)
        replies.onAsyncChange = { refreshAutoShift() }
        compose = ComposeSessionController(scope, services.composeService, services.draftNormalizer)
        compose.onAsyncChange = { refreshAutoShift() }
        surface = KeySurfaceController(this)
        autocorrect = AutocorrectController(
            engine = AutocorrectEngine(
                AutocorrectDictionaries(AssetDictionarySource(assets)),
                PrefsLearnedWordsStore(this)
            ),
            host = hostField,
            scope = scope,
            worker = suggestionWorker
        )
        polish = InstructionPolish(scope, polishService()).apply {
            isAllowed = {
                smartCorrection && AILimits.features.instructionPolish && isComposing &&
                    activeFlow.stage == ReplyComposerFlow.Stage.Composing
            }
            inputLanguage = { language }
        }

        enabledLanguages = services.settings.enabledKeyboardLanguages
        language = services.settings.keyboardLanguage
        uiLanguage = services.settings.effectiveAppLanguage
        appearance = services.settings.appearance
        chips = cachedChips()
        selectedTemplateId = services.settings.lastTemplateId

        replies.uiLanguage = uiLanguage
        compose.uiLanguage = uiLanguage
        replies.inputLanguage = { language }
        compose.inputLanguage = { language }
        replies.configuration = ReplyConfiguration.INITIAL
    }

    /**
     * Built once and reused: `onCreateInputView` is called again after a
     * configuration change, not every time the keyboard is shown, so the
     * second appearance is a layout pass rather than a construction.
     */
    override fun onCreateInputView(): View {
        inputView?.let { existing ->
            (existing.parent as? android.view.ViewGroup)?.removeView(existing)
            return existing
        }
        val view = ComposeView(this).apply {
            setViewCompositionStrategy(ViewCompositionStrategy.DisposeOnDetachedFromWindow)
            setContent { KeyboardContent() }
        }
        viewHost.attachTo(view)
        // Compose also looks the owners up from the window's decor view.
        window?.window?.decorView?.let { viewHost.attachTo(it) }
        inputView = view
        return view
    }

    override fun onStartInputView(info: EditorInfo?, restarting: Boolean) {
        super.onStartInputView(info, restarting)
        hostField.startInput(info, restarting)
        isSecureField = hostField.isSecureField
        readField(info)

        refreshSettings()
        autocorrect.startInput(info, smartCorrection, language)
        if (!restarting) polish.reset()
        refreshGlobeKey()
        loadConfigurationIfNeeded()
        refreshLimitsIfStale()

        val numeric = when ((info?.inputType ?: 0) and InputType.TYPE_MASK_CLASS) {
            InputType.TYPE_CLASS_NUMBER, InputType.TYPE_CLASS_PHONE, InputType.TYPE_CLASS_DATETIME -> true
            else -> false
        }
        if (!restarting) {
            plane = if (numeric) KeyboardPlane.NUMBERS else KeyboardPlane.LETTERS
            shift.reset()
        }
        replies.restoreIfRecent(SystemClock.uptimeMillis())
        compose.restoreIfRecent(SystemClock.uptimeMillis())
        if (isSecureField && replies.session != null) replies.suspend()
        // Nothing AI-written goes into a password field.
        if (isSecureField) compose.clear()
        refreshAutoShift()
        viewHost.onShown()
    }

    override fun onFinishInputView(finishingInput: Boolean) {
        super.onFinishInputView(finishingInput)
        viewHost.onHidden()
        surface.cancelAll()
        // The word being typed stays exactly as typed.
        autocorrect.endWord()
        polish.dismiss()
        // An unfinished reply or Create is kept in memory for a few minutes,
        // so a trip to another chat does not cost the instruction. A running
        // request is stopped: no spinner survives the keyboard going away.
        replies.park(SystemClock.uptimeMillis())
        compose.park(SystemClock.uptimeMillis())
        // Release the microphone rather than holding it across every app.
        releaseSpeech()
    }

    override fun onUpdateSelection(
        oldSelStart: Int, oldSelEnd: Int, newSelStart: Int, newSelEnd: Int,
        candidatesStart: Int, candidatesEnd: Int
    ) {
        super.onUpdateSelection(oldSelStart, oldSelEnd, newSelStart, newSelEnd, candidatesStart, candidatesEnd)
        // An echo of the keyboard's own edit, or the caret moved in the host
        // (a tap, a paste, the app cleared the field after sending) - which
        // ends the word being typed where it stands.
        autocorrect.hostSelectionChanged(newSelStart, newSelEnd, candidatesStart, candidatesEnd)
        // Shift follows the caret, as Gboard's does.
        if (target == Target.HOST) refreshAutoShift()
    }

    override fun onFinishInput() {
        super.onFinishInput()
        // The connection is going away: forget the field, send nothing.
        hostField.endInput()
        autocorrect.clear()
    }

    /**
     * The system's own language switcher picked one of the three subtypes
     * declared in method.xml: follow it with the matching layout.
     */
    override fun onCurrentInputMethodSubtypeChanged(newSubtype: android.view.inputmethod.InputMethodSubtype?) {
        super.onCurrentInputMethodSubtypeChanged(newSubtype)
        @Suppress("DEPRECATION")
        val locale = newSubtype?.languageTag?.takeIf { it.isNotEmpty() } ?: newSubtype?.locale ?: return
        val picked = KeyboardLanguage.fromCode(locale.take(2).lowercase()) ?: return
        if (picked in enabledLanguages) switchLanguage(picked)
    }

    override fun onDestroy() {
        releaseSpeech()
        permissionJob?.cancel()
        polish.dismiss()
        autocorrect.clear()
        replies.clear()
        compose.clear()
        viewHost.onDestroy()
        scope.cancel()
        suggestionWorker.close()
        inputView = null
        super.onDestroy()
    }

    /** Never take over the whole screen in landscape: it would hide the chat. */
    override fun onEvaluateFullscreenMode(): Boolean = false

    // ------------------------------------------------------------ composition

    @Composable
    private fun KeyboardContent() {
        val configuration = LocalConfiguration.current
        val density = LocalDensity.current.density

        val theme = KeyboardTheme.resolve(
            systemIsDark = (configuration.uiMode and android.content.res.Configuration.UI_MODE_NIGHT_MASK) ==
                android.content.res.Configuration.UI_MODE_NIGHT_YES,
            override = when (appearance) {
                AppearancePreference.SYSTEM -> null
                AppearancePreference.LIGHT -> false
                AppearancePreference.DARK -> true
            }
        )

        val width = configuration.screenWidthDp.toFloat()
        val screenHeight = configuration.screenHeightDp.toFloat()
        val sizing = KeyboardSizing(width, KeyboardSizing.isLandscape(width, screenHeight), density)
        // ONE height for every page of every enabled layout.
        val areaHeight = sizing.keyAreaHeight(KeyboardLayout.maximumRowCount(enabledLanguages), screenHeight)
        val composing = isComposing
        val options = PageOptions(
            showsGlobeKey = showsGlobeKey,
            showsLanguageKey = enabledLanguages.size > 1,
            // The composer's fields are plain text whatever the host field is.
            field = if (composing) FieldKind.TEXT else fieldKind
        )
        val page = remember(language, plane, options, sizing, areaHeight) {
            KeyboardGeometry.layout(KeyboardLayout.page(language, plane, options), sizing, areaHeight)
        }

        val strings = remember(uiLanguage) { services.strings(uiLanguage) }
        val intents = remember(uiLanguage) { quickIntents(strings) }
        val createIntents = remember(uiLanguage) { composeIntents(strings) }
        val flow = activeFlow
        val keys = KeySurfaceState(
            layout = page,
            labels = KeyboardLabels(language),
            shiftMode = shiftMode,
            // Inside the composer return is a newline; it never reaches the
            // messenger's Send while a reply is open.
            returnFace = if (composing) ReturnFace.NEWLINE else returnFace,
            returnProminent = !composing && returnFace != ReturnFace.NEWLINE,
            // With a reply on screen, return starts editing it like any key.
            returnEnabled = !composing || target == Target.COMPOSER ||
                flow.stage == ReplyComposerFlow.Stage.Result,
            spaceCaption = language.nativeName,
            dimmed = composing && flow.isGenerating,
            languages = enabledLanguages
        )

        AIReplyTheme(appearance = appearance) {
            KeyboardRoot(
                theme = theme,
                keys = keys,
                controller = surface,
                onAccessibilityKey = { key ->
                    if (key == KeyboardKey.Backspace) onDelete(word = false) else onKeyCommit(key)
                },
                top = {
                    val session = replies.session
                    val created = compose.session
                    if (created != null) {
                        CreatePanel(
                            model = CreateModel(
                                session = created,
                                intents = createIntents,
                                instructionLines = if (screenHeight >= 700f) 4 else 3,
                                maxFieldLines = if (screenHeight >= 700f) 7 else 4,
                                assist = typingAssist(created.instruction)
                            ),
                            actions = createActions,
                            strings = strings,
                            theme = theme
                        )
                    } else if (session != null && composing) {
                        ComposerPanel(
                            model = ComposerModel(
                                personaName = TemplateNaming.displayName(services.localized(uiLanguage), session.template),
                                session = session,
                                focus = focus,
                                sourceExpanded = sourceExpanded,
                                sourceLimit = AILimits.current.sourceCharacters,
                                voice = voiceState,
                                intents = intents,
                                maxFieldLines = if (screenHeight >= 700f) 5 else 3,
                                assist = typingAssist(session.instruction)
                            ),
                            actions = composerActions,
                            strings = strings,
                            theme = theme,
                            modifier = Modifier
                        )
                    } else {
                        PersonaRow(
                            chips = chips,
                            languageCode = uiLanguage.code,
                            selectedId = selectedTemplateId,
                            notice = if (isSecureField) strings[R.string.kb_secure_field] else null,
                            theme = theme,
                            strings = strings,
                            onSelect = ::startReply,
                            onCreate = ::startCompose,
                            suggestions = if (autocorrect.place == AutocorrectController.Place.HOST) {
                                autocorrect.suggestions
                            } else {
                                emptyList()
                            },
                            onPick = ::pickSuggestion
                        )
                    }
                }
            )
        }
    }

    private val composerActions by lazy {
        ComposerActions(
            onPersona = {
                leaveFields()
                replies.suspend()
                refreshAutoShift()
            },
            onClose = ::closeComposer,
            onPaste = ::pasteSource,
            onToggleSource = {
                autocorrect.clear()
                sourceExpanded = !sourceExpanded
                if (!sourceExpanded && focus == PanelFocus.SOURCE) focus = PanelFocus.INSTRUCTION
                polish.focusMoved(activeField)
                refreshAutoShift()
            },
            onClearSource = {
                autocorrect.clear()
                replies.session?.source?.clear()
                replies.composerEdited()
                sourceExpanded = false
                focus = PanelFocus.INSTRUCTION
                refreshAutoShift()
            },
            onFieldTap = ::tapField,
            onPrimary = {
                leaveFields()
                if (replies.flow.isGenerating) replies.stop() else replies.generate()
                refreshAutoShift()
            },
            onIntent = ::applyIntent,
            onBack = {
                replies.back()
                focus = PanelFocus.INSTRUCTION
                refreshAutoShift()
            },
            onRegenerate = {
                leaveFields()
                if (replies.flow.generationOrigin == ReplyComposerFlow.Origin.RESULT) replies.stop() else replies.generate()
                refreshAutoShift()
            },
            onEdit = {
                leaveFields()
                replies.toggleEditing()
                if (replies.flow.isEditingDraft) focus = PanelFocus.DRAFT
                refreshAutoShift()
            },
            onInsert = ::insertReply,
            onPreviousVersion = replies::showPreviousVersion,
            onNextVersion = replies::showNextVersion,
            onConflict = ::resolveConflict,
            onMic = ::toggleMicrophone,
            assist = assistActions
        )
    }

    private val createActions by lazy {
        CreateActions(
            onClose = ::closeComposer,
            onNew = {
                leaveFields()
                compose.reset()
                focus = PanelFocus.INSTRUCTION
                refreshAutoShift()
            },
            onFieldTap = ::tapField,
            onPrimary = {
                leaveFields()
                if (compose.flow.isGenerating) compose.stop() else compose.generate()
                refreshAutoShift()
            },
            onIntent = ::applyIntent,
            onBack = {
                compose.back()
                focus = PanelFocus.INSTRUCTION
                refreshAutoShift()
            },
            onRegenerate = {
                leaveFields()
                if (compose.flow.generationOrigin == ReplyComposerFlow.Origin.RESULT) compose.stop() else compose.generate()
                refreshAutoShift()
            },
            onEdit = {
                leaveFields()
                compose.toggleEditing()
                if (compose.flow.isEditingDraft) focus = PanelFocus.DRAFT
                refreshAutoShift()
            },
            onInsert = ::insertReply,
            onPreviousVersion = compose::showPreviousVersion,
            onNextVersion = compose::showNextVersion,
            onConflict = ::resolveConflict,
            assist = assistActions
        )
    }

    private val assistActions by lazy {
        TypingAssistActions(
            onPick = ::pickSuggestion,
            onAcceptPolish = {
                autocorrect.clear()
                polish.accept()?.let { field -> fieldEdited(field, byUser = false) }
                refreshAutoShift()
            },
            onUndoPolish = {
                autocorrect.clear()
                polish.undo()?.let { field -> fieldEdited(field, byUser = false) }
                refreshAutoShift()
            }
        )
    }

    /**
     * What replaces the intents while [instruction]'s panel is composing: the
     * word suggestions, or the polish chip / its Undo for that instruction -
     * only while the instruction is the field being typed in, so editing the
     * copied message shows that field's own suggestions.
     */
    private fun typingAssist(instruction: KeyboardTextFieldState): TypingAssist {
        val typingIn = activeField?.takeIf { it === instruction }
        return TypingAssist(
            suggestions = if (autocorrect.place == AutocorrectController.Place.COMPOSER) autocorrect.suggestions else emptyList(),
            polished = polish.offerFor(typingIn),
            canUndoPolish = polish.canUndo(typingIn)
        )
    }

    /** Leaving the fields for an action (a request, a stage change): no strip, no chip, nothing pending. */
    private fun leaveFields() {
        autocorrect.clear()
        polish.dismiss()
    }

    /**
     * An AI panel is open: the reply composer, or Create. At most one - Create
     * drops any reply session when it opens, and a persona chip drops Create.
     */
    private val isComposing: Boolean get() = compose.isActive || replies.isComposing

    /** The stages of whichever panel is open. */
    private val activeFlow: ReplyComposerFlow get() = if (compose.isActive) compose.flow else replies.flow

    // -------------------------------------------------------------- key input

    /**
     * Where a keystroke goes. Exactly three destinations and no guessing:
     * the host's field, the composer field in focus, or nowhere (while a
     * request runs, or while the "field is not empty" question is up).
     */
    private enum class Target { HOST, COMPOSER, NOWHERE }

    private val target: Target
        get() = when {
            !isComposing -> Target.HOST
            activeField != null -> Target.COMPOSER
            else -> Target.NOWHERE
        }

    private val activeField: KeyboardTextFieldState?
        get() {
            compose.session?.let { created ->
                return when (created.flow.stage) {
                    ReplyComposerFlow.Stage.Composing -> created.instruction
                    ReplyComposerFlow.Stage.Editing -> created.draft
                    else -> null
                }
            }
            val session = replies.session ?: return null
            if (replies.isSuspended) return null
            return when (session.flow.stage) {
                ReplyComposerFlow.Stage.Composing ->
                    if (focus == PanelFocus.SOURCE && sourceExpanded) session.source else session.instruction
                ReplyComposerFlow.Stage.Editing -> session.draft
                else -> null
            }
        }

    /** With a reply on screen, typing means "let me change it". */
    private fun prepareComposerForTyping() {
        if (!isComposing || activeField != null) return
        val editing = if (compose.isActive) compose.beginEditingForTyping() else replies.beginEditingForTyping()
        if (editing) focus = PanelFocus.DRAFT
    }

    override fun onKeyDown(key: KeyboardKey) {
        feedback.onKeyPress(inputView)
    }

    override fun onKeyCommit(key: KeyboardKey) {
        when (key) {
            is KeyboardKey.Character -> typeCharacter(key.value)
            KeyboardKey.Shift -> {
                shift.tap(SystemClock.uptimeMillis())
                shiftMode = shift.mode
            }
            KeyboardKey.Backspace -> onDelete(word = false)
            KeyboardKey.Space -> typeSpace()
            KeyboardKey.Return -> pressReturn()
            is KeyboardKey.Plane -> {
                plane = key.target
                if (key.target != KeyboardPlane.LETTERS) shift.reset()
                refreshAutoShift()
            }
            KeyboardKey.Layout -> switchLanguage(language.next(enabledLanguages))
            KeyboardKey.Globe -> switchToNextKeyboard()
        }
    }

    override fun onTextCommit(text: String) {
        prepareComposerForTyping()
        insert(text)
        shift.characterTyped()
        refreshAutoShift()
    }

    override fun onDelete(word: Boolean) {
        prepareComposerForTyping()
        when (target) {
            Target.HOST -> when {
                word -> {
                    autocorrect.endWord()
                    hostField.deleteWordBackward()
                }
                // Undoes the correction just applied, or edits the word being typed.
                autocorrect.deleteInHost() -> Unit
                else -> hostField.deleteBackward()
            }
            Target.COMPOSER -> activeField?.let { field ->
                val limit = limitFor(field)
                val corrects = correctsField(field)
                val changed = when {
                    word -> field.deleteWordBackward()
                    corrects && autocorrect.deleteInField(field, limit) -> true
                    else -> field.deleteBackward()
                }
                if (changed) {
                    if (corrects) autocorrect.fieldChanged(field, limit)
                    fieldEdited(field)
                }
            }
            Target.NOWHERE -> Unit
        }
        refreshAutoShift()
    }

    override fun onMoveCursor(offset: Int) {
        when (target) {
            Target.HOST -> {
                autocorrect.endWord()
                hostField.moveCursorBy(offset)
            }
            Target.COMPOSER -> activeField?.let { field ->
                field.moveCursorBy(offset)
                if (correctsField(field)) autocorrect.fieldChanged(field, limitFor(field)) else autocorrect.clear()
            }
            Target.NOWHERE -> {
                // Moving the caret in a reply on screen starts editing it.
                prepareComposerForTyping()
                activeField?.moveCursorBy(offset)
            }
        }
        refreshAutoShift()
    }

    override fun onShowInputMethodPicker() = showKeyboardPicker()

    override fun onLanguagePicked(language: KeyboardLanguage) = switchLanguage(language)

    private fun typeCharacter(value: String) {
        prepareComposerForTyping()
        val text = if (plane == KeyboardPlane.LETTERS) shift.apply(value) else value
        insert(text)
        if (plane == KeyboardPlane.LETTERS) shift.characterTyped()
        refreshAutoShift()
    }

    private fun typeSpace() {
        prepareComposerForTyping()
        val now = SystemClock.uptimeMillis()
        if (SpaceShortcut.shouldInsertPeriod(textBeforeCursor(), now - lastSpaceTap)) {
            // ". " - the system keyboards' behaviour.
            when (target) {
                Target.HOST -> hostField.batch {
                    hostField.deleteBackward()
                    insert(". ")
                }
                Target.COMPOSER -> {
                    activeField?.deleteBackward()
                    insert(". ")
                }
                Target.NOWHERE -> Unit
            }
            lastSpaceTap = 0
        } else {
            insert(" ")
            lastSpaceTap = now
        }
        // Back to letters after a space on ?123, as Gboard does.
        if (plane != KeyboardPlane.LETTERS) plane = KeyboardPlane.LETTERS
        refreshAutoShift()
    }

    private fun pressReturn() {
        prepareComposerForTyping()
        when (target) {
            // Inside the composer return is a newline in the field; it never
            // reaches the messenger's Send action while a reply is open.
            Target.COMPOSER -> insert("\n")
            // A new line ends the word like a space; Send takes the word as typed.
            Target.HOST -> if (hostField.returnIsNewline) {
                insert("\n")
            } else {
                autocorrect.endWord()
                hostField.sendReturn()
            }
            Target.NOWHERE -> Unit
        }
        refreshAutoShift()
    }

    private fun insert(text: String) {
        when (target) {
            Target.HOST -> autocorrect.typeInHost(text)
            Target.COMPOSER -> activeField?.let { field ->
                val limit = limitFor(field)
                val edited = if (correctsField(field)) {
                    autocorrect.typeInField(field, text, limit)
                } else {
                    field.insert(text, limit)
                }
                if (edited) fieldEdited(field)
            }
            Target.NOWHERE -> Unit
        }
    }

    /** The instruction has the server's character limit; the other fields have none. */
    private fun limitFor(field: KeyboardTextFieldState): Int? {
        val instruction = compose.session?.instruction ?: replies.session?.instruction
        return if (field === instruction) AILimits.current.instructionCharacters else null
    }

    /**
     * Smart correction in the keyboard's own fields: while composing (the
     * intents' row is there for the strip), not in a reply being edited.
     */
    private fun correctsField(field: KeyboardTextFieldState): Boolean =
        autocorrect.correctsFields && field !== compose.session?.draft && field !== replies.session?.draft &&
            activeFlow.correctsTyping

    /**
     * A composer field changed. [byUser]: typed, dictated or an intent - which
     * also asks for a polish of the instruction after a pause; a polish taken
     * or undone is not.
     */
    private fun fieldEdited(field: KeyboardTextFieldState, byUser: Boolean = true) {
        compose.session?.let { created ->
            if (field === created.draft) compose.draftEdited() else compose.instructionEdited()
            if (byUser && field === created.instruction) polish.edited(field)
            return
        }
        val session = replies.session ?: return
        if (field === session.draft) replies.draftEdited() else replies.composerEdited()
        if (byUser && field === session.instruction) polish.edited(field)
    }

    /** A tap on the strip: the word being typed becomes the suggestion. */
    private fun pickSuggestion(suggestion: Suggestion) {
        val field = if (autocorrect.place == AutocorrectController.Place.COMPOSER) activeField else null
        if (autocorrect.pick(suggestion) && field != null) fieldEdited(field)
        // The space after the picked word is the keyboard's: a space typed
        // next is not the start of the double-space full stop (as on iOS).
        lastSpaceTap = 0
        refreshAutoShift()
    }

    private fun textBeforeCursor(): String? = when (target) {
        Target.HOST -> hostField.textBeforeCursor(AUTOSHIFT_WINDOW)
        Target.COMPOSER -> activeField?.textBeforeCursor()
        // A reply on screen: the next keystroke edits it at its caret.
        Target.NOWHERE -> compose.session
            ?.takeIf { it.flow.stage == ReplyComposerFlow.Stage.Result }
            ?.draft?.textBeforeCursor()
            ?: replies.session
                ?.takeIf { it.flow.stage == ReplyComposerFlow.Stage.Result }
                ?.draft?.textBeforeCursor()
    }

    private fun refreshAutoShift() {
        if (plane == KeyboardPlane.LETTERS) {
            val before = textBeforeCursor()
            // While nothing can be typed the keys keep their case.
            if (before != null || target != Target.NOWHERE) {
                val mode = if (target == Target.HOST) hostCapitalization else Capitalization.SENTENCES
                shift.applyAutomatic(AutoCapitalization.shouldCapitalize(before, mode))
            }
        }
        shiftMode = shift.mode
    }

    private fun switchLanguage(next: KeyboardLanguage) {
        if (next == language && plane == KeyboardPlane.LETTERS) return
        language = next
        autocorrect.switchLanguage(next)
        plane = KeyboardPlane.LETTERS
        // Off the main thread: it only matters to the next launch.
        scope.launch(Dispatchers.IO) { services.settings.keyboardLanguage = next }
        refreshAutoShift()
    }

    // ------------------------------------------------------------ host field

    /** What the host field is and wants, from its EditorInfo. */
    private fun readField(info: EditorInfo?) {
        val type = info?.inputType ?: 0
        val variation = type and InputType.TYPE_MASK_VARIATION
        val textClass = (type and InputType.TYPE_MASK_CLASS) == InputType.TYPE_CLASS_TEXT
        fieldKind = when {
            !textClass -> FieldKind.TEXT
            variation == InputType.TYPE_TEXT_VARIATION_EMAIL_ADDRESS ||
                variation == InputType.TYPE_TEXT_VARIATION_WEB_EMAIL_ADDRESS -> FieldKind.EMAIL
            variation == InputType.TYPE_TEXT_VARIATION_URI -> FieldKind.URL
            else -> FieldKind.TEXT
        }
        hostCapitalization = when {
            !textClass || isSecureField || fieldKind != FieldKind.TEXT -> Capitalization.NONE
            type and InputType.TYPE_TEXT_FLAG_CAP_CHARACTERS != 0 -> Capitalization.CHARACTERS
            type and InputType.TYPE_TEXT_FLAG_CAP_WORDS != 0 -> Capitalization.WORDS
            // Most messengers set no flag at all but expect sentence case.
            else -> Capitalization.SENTENCES
        }

        val options = info?.imeOptions ?: 0
        val multiline = type and InputType.TYPE_TEXT_FLAG_MULTI_LINE != 0
        val noEnterAction = options and EditorInfo.IME_FLAG_NO_ENTER_ACTION != 0
        returnFace = if (multiline || noEnterAction) {
            ReturnFace.NEWLINE
        } else {
            when (options and EditorInfo.IME_MASK_ACTION) {
                EditorInfo.IME_ACTION_SEND -> ReturnFace.SEND
                EditorInfo.IME_ACTION_SEARCH -> ReturnFace.SEARCH
                EditorInfo.IME_ACTION_GO -> ReturnFace.GO
                EditorInfo.IME_ACTION_NEXT -> ReturnFace.NEXT
                EditorInfo.IME_ACTION_PREVIOUS -> ReturnFace.PREVIOUS
                EditorInfo.IME_ACTION_DONE -> ReturnFace.DONE
                else -> ReturnFace.NEWLINE
            }
        }
    }

    // -------------------------------------------------------------- reply flow

    /** A persona chip: opens the composer, or resumes the suspended session. */
    private fun startReply(templateId: String) {
        if (isSecureField) return
        val template = resolveTemplate(templateId) ?: return
        // The word being typed in the chat stays exactly as typed.
        autocorrect.endWord()
        polish.dismiss()
        compose.clear()
        selectedTemplateId = templateId
        scope.launch(Dispatchers.IO) { services.settings.lastTemplateId = templateId }

        if (replies.session != null && replies.isSuspended) {
            replies.resume(template)
            refreshAutoShift()
            return
        }

        // The message: a selection in the field, else what the user copied.
        // Opening never generates - it only shows the composer.
        when (val result = contextProvider.acquire(this, currentInputConnection, isSecureField)) {
            is ContextTextProvider.Result.Success ->
                replies.open(template, result.context.text, result.context.source, null)
            is ContextTextProvider.Result.Failure ->
                replies.open(template, "", null, result.error)
        }
        focus = PanelFocus.INSTRUCTION
        sourceExpanded = false
        plane = KeyboardPlane.LETTERS
        shift.reset()
        refreshAutoShift()
    }

    /**
     * "✨" on the persona row: a fresh Create panel. The clipboard is not read,
     * and a reply the user had put aside (persona row over a suspended
     * session) is dropped rather than carried over.
     */
    private fun startCompose() {
        if (isSecureField) return
        autocorrect.endWord()
        polish.dismiss()
        replies.clear()
        cancelSpeech()
        compose.open()
        focus = PanelFocus.INSTRUCTION
        sourceExpanded = false
        plane = KeyboardPlane.LETTERS
        shift.reset()
        refreshAutoShift()
    }

    private fun resolveTemplate(id: String) =
        replies.configuration.template(id) ?: run {
            val loaded = services.configuration.current
            replies.configuration = loaded
            loaded.template(id)
        }

    /** Explicit Paste inside the composer: the clipboard, never the selection. */
    private fun pasteSource() {
        when (val result = contextProvider.acquire(this, null, isSecureField)) {
            is ContextTextProvider.Result.Success -> {
                replies.replaceSource(result.context.text)
                sourceExpanded = false
            }
            is ContextTextProvider.Result.Failure -> replies.showError(result.error)
        }
    }

    private fun tapField(field: PanelFocus, offset: Int) {
        // The caret jumps: the word the strip was about is behind it.
        autocorrect.clear()
        compose.session?.let { created ->
            val stage = created.flow.stage
            when (field) {
                PanelFocus.INSTRUCTION -> {
                    if (stage != ReplyComposerFlow.Stage.Composing) return
                    focus = field
                    created.instruction.moveCursor(offset)
                }
                PanelFocus.DRAFT -> {
                    if (stage == ReplyComposerFlow.Stage.Result) compose.beginEditingForTyping()
                    if (compose.flow.stage != ReplyComposerFlow.Stage.Editing) return
                    focus = PanelFocus.DRAFT
                    created.draft.moveCursor(offset)
                }
                PanelFocus.SOURCE -> return
            }
            // A polish offer belongs to the instruction: it goes when typing moves elsewhere.
            polish.focusMoved(activeField)
            refreshAutoShift()
            return
        }
        val session = replies.session ?: return
        val stage = session.flow.stage
        when (field) {
            PanelFocus.SOURCE, PanelFocus.INSTRUCTION -> {
                if (stage != ReplyComposerFlow.Stage.Composing) return
                focus = field
                (if (field == PanelFocus.SOURCE) session.source else session.instruction).moveCursor(offset)
            }
            PanelFocus.DRAFT -> {
                // Tapping the reply means "let me change this": editable, with
                // the caret where the finger was.
                if (stage == ReplyComposerFlow.Stage.Result) replies.beginEditingForTyping()
                if (replies.flow.stage != ReplyComposerFlow.Stage.Editing) return
                focus = PanelFocus.DRAFT
                session.draft.moveCursor(offset)
            }
        }
        // Typing in the copied message or the reply: the instruction's polish offer goes.
        polish.focusMoved(activeField)
        refreshAutoShift()
    }

    /** Intents WRITE INTO THE INSTRUCTION: readable, editable, stackable. */
    private fun applyIntent(intent: QuickIntent) {
        val field = compose.session?.takeIf { it.flow.stage == ReplyComposerFlow.Stage.Composing }?.instruction
            ?: replies.session?.takeIf { it.flow.stage == ReplyComposerFlow.Stage.Composing }?.instruction
            ?: return
        val current = field.text.trim()
        val combined = if (current.isEmpty()) intent.phrase else "$current ${intent.phrase}"
        val limit = AILimits.current.instructionCharacters
        field.set(
            if (combined.codePointCount(0, combined.length) <= limit) {
                combined
            } else {
                combined.substring(0, combined.offsetByCodePoints(0, limit))
            }
        )
        focus = PanelFocus.INSTRUCTION
        autocorrect.clear()
        fieldEdited(field)
        refreshAutoShift()
    }

    private fun closeComposer() {
        leaveFields()
        replies.clear()
        compose.clear()
        focus = PanelFocus.INSTRUCTION
        sourceExpanded = false
        cancelSpeech()
        refreshAutoShift()
    }

    /**
     * The one place a reply reaches the host application: exactly the text on
     * screen, edits included. Nothing is ever sent.
     */
    private fun insertReply() {
        val hostHasText = hostField.appearsToHaveText()
        val decision = if (compose.isActive) compose.requestInsert(hostHasText) else replies.requestInsert(hostHasText)
        when (decision) {
            is ReplyComposerFlow.InsertDecision.Insert -> {
                hostField.commitText(decision.text)
                finishInsert()
            }
            // The composer now asks Replace / Add / Cancel.
            ReplyComposerFlow.InsertDecision.AskAboutExistingText -> Unit
            ReplyComposerFlow.InsertDecision.NothingToInsert -> Unit
        }
    }

    private fun resolveConflict(choice: ReplyComposerFlow.ConflictChoice) {
        val resolution = if (compose.isActive) compose.resolveConflict(choice) else replies.resolveConflict(choice)
        when (resolution) {
            is ReplyComposerFlow.ConflictResolution.Replace -> {
                hostField.batch {
                    hostField.clear()
                    hostField.commitText(resolution.text)
                }
                finishInsert()
            }
            is ReplyComposerFlow.ConflictResolution.Append -> {
                hostField.batch {
                    hostField.moveCaretToEnd()
                    hostField.commitText(hostField.separatorForAppend() + resolution.text)
                }
                finishInsert()
            }
            // Back to the reply exactly as it was.
            ReplyComposerFlow.ConflictResolution.Cancelled -> Unit
        }
    }

    private fun finishInsert() {
        ReplyLog.event { "reply inserted" }
        closeComposer()
    }

    // ------------------------------------------------------------------ voice

    private fun toggleMicrophone() {
        val client = speech ?: AndroidSpeechRecognitionClient(this).also {
            speech = it
            observeVoice(it)
        }
        when (voiceState) {
            is VoiceState.Listening, is VoiceState.Starting -> client.stop()
            is VoiceState.Processing -> Unit
            is VoiceState.PermissionDenied -> KeyboardStatus.openAppSettings(this)
            is VoiceState.PermissionRequired -> requestMicrophone()
            else -> if (!MicPermission.isGranted(this)) {
                requestMicrophone()
            } else {
                client.reset()
                client.start(uiLanguage.languageTag)
            }
        }
    }

    /**
     * The collector starts BEFORE the dialog: the result flow has no replay,
     * and a fast answer would otherwise be lost.
     */
    private fun requestMicrophone() {
        permissionJob?.cancel()
        permissionJob = scope.launch {
            val outcome = async { MicPermission.results.first() }
            MicPermission.request(this@ReplyKeyboardService)
            when (outcome.await()) {
                MicPermission.Outcome.GRANTED -> {
                    speech?.reset()
                    speech?.start(uiLanguage.languageTag)
                }
                MicPermission.Outcome.PERMANENTLY_DENIED -> voiceState = VoiceState.PermissionDenied
                MicPermission.Outcome.DENIED -> voiceState = VoiceState.PermissionRequired
            }
        }
    }

    private fun observeVoice(client: SpeechRecognitionClient) {
        voiceJob?.cancel()
        voiceJob = scope.launch {
            client.state.collect { state ->
                voiceState = state
                if (state is VoiceState.Done) {
                    // Dictation lands in the INSTRUCTION, appended.
                    appendDictation(state.text)
                    client.reset()
                }
            }
        }
    }

    private fun appendDictation(text: String) {
        val addition = text.trim()
        val session = replies.session ?: return
        if (addition.isEmpty()) return
        val field = session.instruction
        val separator = if (field.text.isEmpty() || field.text.endsWith(" ")) "" else " "
        field.set(field.text + separator + addition)
        focus = PanelFocus.INSTRUCTION
        autocorrect.clear()
        fieldEdited(field)
    }

    private fun cancelSpeech() {
        speech?.cancel()
        voiceState = VoiceState.Idle
    }

    private fun releaseSpeech() {
        voiceJob?.cancel()
        voiceJob = null
        speech?.release()
        speech = null
        voiceState = VoiceState.Idle
    }

    // ----------------------------------------------------------- configuration

    /**
     * Profile and templates are read once per appearance, off the main
     * thread, never during a key press.
     */
    private fun loadConfigurationIfNeeded() {
        val now = SystemClock.uptimeMillis()
        if (now - configurationLoadedAt < CONFIG_RELOAD_MS) return
        configurationLoadedAt = now
        scope.launch {
            val loaded = withContext(Dispatchers.IO) {
                services.configuration.reload()
                services.configuration.current
            }
            replies.configuration = loaded
            chips = loaded.visibleTemplates.map { template ->
                TemplateSummary(
                    id = template.id,
                    names = AppLanguage.entries.associate { language ->
                        language.code to TemplateNaming.displayName(services.localized(language), template)
                    }
                )
            }
        }
    }

    /** The server's character limits, at most every few hours. */
    private fun refreshLimitsIfStale() {
        if (!AILimits.isStale() || !services.accountCredentials.isSignedIn) return
        scope.launch(Dispatchers.IO) {
            runCatching { services.accountService.serverConfig() }.getOrNull()?.let { AILimits.apply(it) }
        }
    }

    private fun cachedChips(): List<TemplateSummary> =
        services.settings.templateSummaries
            ?: ReplyConfiguration.INITIAL.visibleTemplates.map { template ->
                TemplateSummary(
                    id = template.id,
                    names = AppLanguage.entries.associate { language ->
                        language.code to TemplateNaming.displayName(services.localized(language), template)
                    }
                )
            }

    /** Picks up what the user changed in the app while the keyboard was away. */
    private fun refreshSettings() {
        val language = services.settings.effectiveAppLanguage
        if (language != uiLanguage) {
            uiLanguage = language
            replies.uiLanguage = language
            compose.uiLanguage = language
            chips = cachedChips()
        }
        appearance = services.settings.appearance
        feedback.hapticsEnabled = services.settings.keyboardHaptics
        smartCorrection = services.settings.smartCorrection
        enabledLanguages = services.settings.enabledKeyboardLanguages
        val stored = services.settings.keyboardLanguage
        if (stored != this.language) this.language = stored
        selectedTemplateId = services.settings.lastTemplateId
    }

    private fun quickIntents(strings: AppStrings): List<QuickIntent> = listOf(
        QuickIntent("agree", strings[R.string.kb_intent_agree], strings[R.string.kb_intent_agree_phrase]),
        QuickIntent("decline", strings[R.string.kb_intent_decline], strings[R.string.kb_intent_decline_phrase]),
        QuickIntent("details", strings[R.string.kb_intent_details], strings[R.string.kb_intent_details_phrase]),
        QuickIntent("brief", strings[R.string.kb_intent_brief], strings[R.string.kb_intent_brief_phrase]),
        QuickIntent("professional", strings[R.string.kb_intent_professional], strings[R.string.kb_intent_professional_phrase]),
        QuickIntent("friendly", strings[R.string.kb_intent_friendly], strings[R.string.kb_intent_friendly_phrase]),
        QuickIntent("thanks", strings[R.string.kb_intent_thanks], strings[R.string.kb_intent_thanks_phrase]),
        QuickIntent("reschedule", strings[R.string.kb_intent_reschedule], strings[R.string.kb_intent_reschedule_phrase])
    )

    /** Create's one-tap additions: occasions first, then tones. */
    private fun composeIntents(strings: AppStrings): List<QuickIntent> = listOf(
        QuickIntent("congratulate", strings[R.string.kb_compose_intent_congratulate], strings[R.string.kb_compose_intent_congratulate_phrase]),
        QuickIntent("short", strings[R.string.kb_compose_intent_short], strings[R.string.kb_compose_intent_short_phrase]),
        QuickIntent("formal", strings[R.string.kb_compose_intent_formal], strings[R.string.kb_compose_intent_formal_phrase]),
        QuickIntent("friendly", strings[R.string.kb_compose_intent_friendly], strings[R.string.kb_compose_intent_friendly_phrase]),
        QuickIntent("emoji", strings[R.string.kb_compose_intent_emoji], strings[R.string.kb_compose_intent_emoji_phrase]),
        QuickIntent("thanks", strings[R.string.kb_compose_intent_thanks], strings[R.string.kb_compose_intent_thanks_phrase]),
        QuickIntent("decline", strings[R.string.kb_compose_intent_decline], strings[R.string.kb_compose_intent_decline_phrase])
    )

    // -------------------------------------------------------- system keyboards

    private fun refreshGlobeKey() {
        showsGlobeKey = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            shouldOfferSwitchingToNextInputMethod()
        } else {
            @Suppress("DEPRECATION")
            val token = window?.window?.attributes?.token
            val manager = getSystemService(INPUT_METHOD_SERVICE) as? InputMethodManager
            token != null && manager?.shouldOfferSwitchingToNextInputMethod(token) == true
        }
    }

    private fun switchToNextKeyboard() {
        autocorrect.endWord()
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            switchToNextInputMethod(false)
        } else {
            @Suppress("DEPRECATION")
            val token = window?.window?.attributes?.token ?: return
            val manager = getSystemService(INPUT_METHOD_SERVICE) as? InputMethodManager
            manager?.switchToNextInputMethod(token, false)
        }
    }

    private fun showKeyboardPicker() {
        autocorrect.endWord()
        val manager = getSystemService(INPUT_METHOD_SERVICE) as? InputMethodManager
        runCatching { manager?.showInputMethodPicker() }
    }

    /**
     * The instruction polish: the account's endpoint, or in DEBUG builds the
     * canned stand-in under the developer switch that serves canned replies.
     */
    private fun polishService(): PolishService = PolishService(
        configuration = services.aiConfiguration,
        accountTransport = { baseUrl ->
            AccountPolishTransport(baseUrl, services.accountSession, BuildConfig.VERSION_NAME)
        },
        transportOverride = if (BuildConfig.DEBUG) {
            { if (services.settings.debugMockReplies) DebugPolishMock() else null }
        } else {
            null
        }
    )

    private companion object {
        const val CONFIG_RELOAD_MS = 1_000L
        /** Enough text to decide capitalization; each read crosses processes. */
        const val AUTOSHIFT_WINDOW = 48
    }
}
