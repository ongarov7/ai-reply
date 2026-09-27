package kz.yerek.aireply.keyboard

import android.content.Intent
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
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kz.yerek.aireply.AIReplyApplication
import kz.yerek.aireply.MainActivity
import kz.yerek.aireply.R
import kz.yerek.aireply.ServiceLocator
import kz.yerek.aireply.ai.AILimits
import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.ai.AppStrings
import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.core.lang.KeyboardPlane
import kz.yerek.aireply.core.lang.TemplateNaming
import kz.yerek.aireply.data.settings.AppearancePreference
import kz.yerek.aireply.domain.model.ReplyConfiguration
import kz.yerek.aireply.domain.model.TemplateSummary
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
import kz.yerek.aireply.keyboard.reply.ReplyComposerFlow
import kz.yerek.aireply.keyboard.reply.ReplySessionController
import kz.yerek.aireply.keyboard.ui.ComposerActions
import kz.yerek.aireply.keyboard.ui.ComposerModel
import kz.yerek.aireply.keyboard.ui.ComposerPanel
import kz.yerek.aireply.keyboard.ui.KeySurfaceController
import kz.yerek.aireply.keyboard.ui.KeySurfaceListener
import kz.yerek.aireply.keyboard.ui.KeySurfaceState
import kz.yerek.aireply.keyboard.ui.KeyboardRoot
import kz.yerek.aireply.keyboard.ui.PanelFocus
import kz.yerek.aireply.keyboard.ui.PersonaRow
import kz.yerek.aireply.keyboard.ui.QuickIntent
import kz.yerek.aireply.platform.ReplyLog
import kz.yerek.aireply.ui.design.AIReplyTheme
import kz.yerek.aireply.voice.AndroidSpeechRecognitionClient
import kz.yerek.aireply.voice.MicPermission
import kz.yerek.aireply.voice.SpeechRecognitionClient
import kz.yerek.aireply.voice.VoiceState
import kotlin.math.max

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
 *  * A request is only ever started by the user tapping Reply, Regenerate or
 *    Try again. Appearing, copying and typing start nothing.
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
    private lateinit var surface: KeySurfaceController

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val contextProvider = ContextTextProvider()

    private var speech: SpeechRecognitionClient? = null
    private var voiceJob: Job? = null
    private var permissionJob: Job? = null
    private var noticeJob: Job? = null

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
    private var notice by mutableStateOf<String?>(null)

    private var hostCapitalization = Capitalization.SENTENCES
    private var hostSelectionEnd = -1
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
        replies = ReplySessionController(scope, services.replyService, services.draftNormalizer)
        replies.onAsyncChange = { refreshAutoShift() }
        surface = KeySurfaceController(this)

        enabledLanguages = services.settings.enabledKeyboardLanguages
        language = services.settings.keyboardLanguage
        uiLanguage = services.settings.effectiveAppLanguage
        appearance = services.settings.appearance
        chips = cachedChips()
        selectedTemplateId = services.settings.lastTemplateId

        replies.uiLanguage = uiLanguage
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
        hostField.editorInfo = info
        isSecureField = hostField.isSecureField
        hostSelectionEnd = info?.initialSelEnd ?: -1
        readField(info)

        refreshSettings()
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
        if (isSecureField && replies.session != null) replies.suspend()
        refreshAutoShift()
        viewHost.onShown()
    }

    override fun onFinishInputView(finishingInput: Boolean) {
        super.onFinishInputView(finishingInput)
        viewHost.onHidden()
        surface.cancelAll()
        // An unfinished reply is kept in memory for a few minutes, so a trip
        // to another chat to copy a message does not cost the instruction.
        replies.park(SystemClock.uptimeMillis())
        // Release the microphone rather than holding it across every app.
        releaseSpeech()
    }

    override fun onUpdateSelection(
        oldSelStart: Int, oldSelEnd: Int, newSelStart: Int, newSelEnd: Int,
        candidatesStart: Int, candidatesEnd: Int
    ) {
        super.onUpdateSelection(oldSelStart, oldSelEnd, newSelStart, newSelEnd, candidatesStart, candidatesEnd)
        hostSelectionEnd = newSelEnd
        // The caret moved in the host (a tap, a paste, the app cleared the
        // field after sending): shift follows it, as Gboard's does.
        if (target == Target.HOST) refreshAutoShift()
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
        noticeJob?.cancel()
        permissionJob?.cancel()
        replies.clear()
        viewHost.onDestroy()
        scope.cancel()
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
        val composing = replies.isComposing
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
        val flow = replies.flow
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
                    if (session != null && composing) {
                        ComposerPanel(
                            model = ComposerModel(
                                personaName = TemplateNaming.displayName(services.localized(uiLanguage), session.template),
                                session = session,
                                focus = focus,
                                sourceExpanded = sourceExpanded,
                                sourceLimit = AILimits.current.sourceCharacters,
                                voice = voiceState,
                                intents = intents,
                                maxFieldLines = if (screenHeight >= 700f) 5 else 3
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
                            notice = notice ?: if (isSecureField) strings[R.string.kb_secure_field] else null,
                            theme = theme,
                            strings = strings,
                            onSelect = ::startReply,
                            onAdd = ::openTemplateEditor
                        )
                    }
                }
            )
        }
    }

    private val composerActions by lazy {
        ComposerActions(
            onPersona = {
                replies.suspend()
                refreshAutoShift()
            },
            onClose = ::closeComposer,
            onPaste = ::pasteSource,
            onToggleSource = {
                sourceExpanded = !sourceExpanded
                if (!sourceExpanded && focus == PanelFocus.SOURCE) focus = PanelFocus.INSTRUCTION
                refreshAutoShift()
            },
            onClearSource = {
                replies.session?.source?.clear()
                replies.composerEdited()
                sourceExpanded = false
                focus = PanelFocus.INSTRUCTION
                refreshAutoShift()
            },
            onFieldTap = ::tapField,
            onPrimary = {
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
                if (replies.flow.generationOrigin == ReplyComposerFlow.Origin.RESULT) replies.stop() else replies.generate()
                refreshAutoShift()
            },
            onEdit = {
                replies.toggleEditing()
                if (replies.flow.isEditingDraft) focus = PanelFocus.DRAFT
                refreshAutoShift()
            },
            onInsert = ::insertReply,
            onPreviousVersion = replies::showPreviousVersion,
            onNextVersion = replies::showNextVersion,
            onConflict = ::resolveConflict,
            onMic = ::toggleMicrophone
        )
    }

    // -------------------------------------------------------------- key input

    /**
     * Where a keystroke goes. Exactly three destinations and no guessing:
     * the host's field, the composer field in focus, or nowhere (while a
     * request runs, or while the "field is not empty" question is up).
     */
    private enum class Target { HOST, COMPOSER, NOWHERE }

    private val target: Target
        get() = when {
            !replies.isComposing -> Target.HOST
            activeField != null -> Target.COMPOSER
            else -> Target.NOWHERE
        }

    private val activeField: KeyboardTextFieldState?
        get() {
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
        if (!replies.isComposing || activeField != null) return
        if (replies.beginEditingForTyping()) focus = PanelFocus.DRAFT
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
            Target.HOST -> if (word) hostField.deleteWordBackward() else hostField.deleteBackward()
            Target.COMPOSER -> activeField?.let { field ->
                val changed = if (word) field.deleteWordBackward() else field.deleteBackward()
                if (changed) composerFieldEdited(field)
            }
            Target.NOWHERE -> Unit
        }
        refreshAutoShift()
    }

    override fun onMoveCursor(offset: Int) {
        when (target) {
            Target.HOST -> hostField.moveCursorBy(offset, hostSelectionEnd)
            Target.COMPOSER -> activeField?.moveCursorBy(offset)
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
                Target.HOST -> hostField.deleteBackward()
                Target.COMPOSER -> activeField?.deleteBackward()
                Target.NOWHERE -> Unit
            }
            insert(". ")
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
            Target.HOST -> hostField.sendReturn()
            Target.NOWHERE -> Unit
        }
        refreshAutoShift()
    }

    private fun insert(text: String) {
        when (target) {
            Target.HOST -> hostField.commitText(text)
            Target.COMPOSER -> activeField?.let { field ->
                val session = replies.session
                val limit = if (session != null && field === session.instruction) {
                    AILimits.current.instructionCharacters
                } else {
                    null
                }
                if (field.insert(text, limit)) composerFieldEdited(field)
            }
            Target.NOWHERE -> Unit
        }
    }

    private fun composerFieldEdited(field: KeyboardTextFieldState) {
        val session = replies.session ?: return
        if (field === session.draft) replies.draftEdited() else replies.composerEdited()
    }

    private fun textBeforeCursor(): String? = when (target) {
        Target.HOST -> hostField.textBeforeCursor(AUTOSHIFT_WINDOW)
        Target.COMPOSER -> activeField?.textBeforeCursor()
        // A reply on screen: the next keystroke edits it at its caret.
        Target.NOWHERE -> replies.session
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
        refreshAutoShift()
    }

    /** Intents WRITE INTO THE INSTRUCTION: readable, editable, stackable. */
    private fun applyIntent(intent: QuickIntent) {
        val session = replies.session ?: return
        if (session.flow.stage != ReplyComposerFlow.Stage.Composing) return
        val current = session.instruction.text.trim()
        val combined = if (current.isEmpty()) intent.phrase else "$current ${intent.phrase}"
        val limit = AILimits.current.instructionCharacters
        session.instruction.set(
            if (combined.codePointCount(0, combined.length) <= limit) {
                combined
            } else {
                combined.substring(0, combined.offsetByCodePoints(0, limit))
            }
        )
        focus = PanelFocus.INSTRUCTION
        replies.composerEdited()
        refreshAutoShift()
    }

    private fun closeComposer() {
        replies.clear()
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
        when (val decision = replies.requestInsert(hostField.appearsToHaveText())) {
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
        when (val resolution = replies.resolveConflict(choice)) {
            is ReplyComposerFlow.ConflictResolution.Replace -> {
                hostField.clear()
                hostField.commitText(resolution.text)
                finishInsert()
            }
            is ReplyComposerFlow.ConflictResolution.Append -> {
                hostField.moveCaretToEnd()
                hostField.commitText(hostField.separatorForAppend() + resolution.text)
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

    private fun showNotice(message: String) {
        notice = message
        noticeJob?.cancel()
        noticeJob = scope.launch {
            delay(NOTICE_MS)
            notice = null
        }
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
        replies.composerEdited()
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
            chips = cachedChips()
        }
        appearance = services.settings.appearance
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
        val manager = getSystemService(INPUT_METHOD_SERVICE) as? InputMethodManager
        runCatching { manager?.showInputMethodPicker() }
    }

    /**
     * "+" opens the template editor in the app. An Android keyboard can start
     * an Activity, so it does; the notice says what is happening meanwhile.
     */
    private fun openTemplateEditor() {
        val intent = Intent(this, MainActivity::class.java)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            .putExtra(MainActivity.EXTRA_ROUTE, MainActivity.ROUTE_TEMPLATES)
        runCatching { startActivity(intent) }
            .onSuccess { showNotice(services.strings(uiLanguage)[R.string.kb_add_template_hint]) }
            .onFailure { ReplyLog.warn(it) { "could not open the template editor" } }
    }

    private companion object {
        const val NOTICE_MS = 3_200L
        const val CONFIG_RELOAD_MS = 1_000L
        /** Enough text to decide capitalization; each read crosses processes. */
        const val AUTOSHIFT_WINDOW = 48
    }
}
