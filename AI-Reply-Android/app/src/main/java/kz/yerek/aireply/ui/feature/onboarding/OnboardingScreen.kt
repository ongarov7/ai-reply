package kz.yerek.aireply.ui.feature.onboarding

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kz.yerek.aireply.R
import kz.yerek.aireply.ServiceLocator
import kz.yerek.aireply.analytics.ProductEvent
import kz.yerek.aireply.analytics.ProductEventValue
import kz.yerek.aireply.analytics.ProductEvents
import kz.yerek.aireply.domain.model.GrammaticalGender
import kz.yerek.aireply.domain.model.RelationshipKind
import kz.yerek.aireply.keyboard.input.KeyboardStatus
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.common.rememberKeyboardStatus
import kz.yerek.aireply.ui.design.AppMark
import kz.yerek.aireply.ui.design.Layout
import kz.yerek.aireply.ui.design.PrimaryButton
import kz.yerek.aireply.ui.design.ReadableColumn
import kz.yerek.aireply.ui.design.Spacing
import kz.yerek.aireply.ui.feature.setup.openKeyboardSettings

/**
 * The first run, and the same screens again from Settings.
 *
 * Онбординг: сәлемдесу, үш қадамды оқыту, пернетақтаны қосу, жыныс.
 *
 * Welcome → (try first) the tutorial, then «Подключи AI Reply»; or (set up
 * the keyboard) the other way round → the gender, when it was never
 * answered. «Позже» leaves it for good (Settings and reminders offer it
 * again); each part can be skipped. A first run remembers its step and its
 * path, so a run that was closed halfway comes back there; finishing it
 * records the version on this device and never touches the replies balance.
 * Which steps exist and in what order is [OnboardingFlow]'s decision. No
 * step moves by itself: the user turns every page.
 *
 * @param reopened opened from Settings or a link rather than at first launch,
 *   for the `trigger` of `onboarding_started`.
 */
@Composable
fun OnboardingScreen(mode: OnboardingMode, onFinished: () -> Unit, reopened: Boolean = false) {
    val services = LocalServices.current
    val context = LocalContext.current
    val device = services.deviceState
    val configuration by services.configuration.configuration.collectAsStateWithLifecycle()
    val profileGender = configuration.profile.grammaticalGender

    // Read before the first step is saved: a run that comes back was started already.
    val savedStep = remember { if (mode == OnboardingMode.FIRST_RUN) device.onboardingResumeStep else null }
    val asksGender = rememberSaveable {
        OnboardingFlow.asksGender(
            mode = mode,
            genderAnswered = services.configuration.profile.grammaticalGender != null,
            startedAsking = device.onboardingAsksGender
        )
    }
    var path by rememberSaveable {
        mutableStateOf(OnboardingPath.fromId(if (mode == OnboardingMode.FIRST_RUN) device.onboardingPath else null))
    }
    val flow = remember(asksGender, path) { OnboardingFlow(mode, asksGender, path) }
    var step by rememberSaveable { mutableStateOf(flow.start(savedStep)) }
    var gender by rememberSaveable { mutableStateOf(profileGender?.takeIf { it.isSpecified }) }
    var recipient by rememberSaveable { mutableStateOf<RelationshipKind?>(null) }
    var messageCopied by rememberSaveable { mutableStateOf(false) }
    var replyCopied by rememberSaveable { mutableStateOf(false) }
    val keyboard by rememberKeyboardStatus()

    var announced by rememberSaveable { mutableStateOf(false) }
    var viewed by rememberSaveable { mutableStateOf<OnboardingStep?>(null) }
    LaunchedEffect(Unit) {
        if (announced) return@LaunchedEffect
        announced = true
        if (OnboardingFlow.startsRun(mode, savedStep)) {
            ProductEvents.track(
                ProductEvent.ONBOARDING_STARTED,
                mapOf(
                    "version" to ProductEventValue.Number(OnboardingFlow.CURRENT_VERSION),
                    "trigger" to ProductEventValue.Code(if (mode == OnboardingMode.FIRST_RUN && !reopened) "auto" else "settings")
                )
            )
        } else {
            ProductEvents.track(ProductEvent.ONBOARDING_RESUMED)
        }
    }
    LaunchedEffect(step, path) {
        if (flow.resumes) {
            device.onboardingResumeStep = step.id
            device.onboardingAsksGender = flow.asksGender
            device.onboardingPath = path.id
        }
        if (viewed == step) return@LaunchedEffect
        viewed = step
        trackViewed(step)
    }

    fun finish(skipped: Boolean) {
        if (mode == OnboardingMode.FIRST_RUN) {
            device.completeOnboarding(OnboardingFlow.CURRENT_VERSION, skipped = skipped)
            services.profileSync.reportOnboardingCompleted(OnboardingFlow.CURRENT_VERSION)
            ProductEvents.track(
                ProductEvent.ONBOARDING_COMPLETED,
                mapOf(
                    "version" to ProductEventValue.Number(OnboardingFlow.CURRENT_VERSION),
                    "skipped" to ProductEventValue.Flag(skipped)
                )
            )
        }
        onFinished()
    }

    fun go(next: OnboardingStep?) {
        if (next != null) step = next else finish(skipped = false)
    }

    fun skipped(at: OnboardingStep) {
        ProductEvents.track(ProductEvent.ONBOARDING_SKIPPED, mapOf("step" to ProductEventValue.Code(at.id)))
    }

    // Coming back to this step and continuing with the same answer is not a new choice.
    fun saveGender(choice: GrammaticalGender) {
        if (choice == services.configuration.profile.grammaticalGender) return
        services.profileSync.setGender(choice)
        ProductEvents.track(
            ProductEvent.GENDER_SELECTED,
            mapOf(
                "source" to ProductEventValue.Code("onboarding"),
                "skipped" to ProductEventValue.Flag(!choice.isSpecified)
            )
        )
    }

    val actions = OnboardingActions(
        later = {
            skipped(OnboardingStep.WELCOME)
            finish(skipped = true)
        },
        choosePath = { chosen ->
            path = chosen
            go(OnboardingFlow(mode, asksGender, chosen).next(OnboardingStep.WELCOME))
        },
        next = { go(flow.next(step)) },
        skipTutorial = {
            skipped(step)
            go(flow.afterTutorial)
        },
        startUsing = {
            ProductEvents.track(ProductEvent.ONBOARDING_PRACTICE_COMPLETED)
            go(flow.next(step))
        },
        openKeyboardSettings = { openKeyboardSettings(context) },
        chooseKeyboard = { KeyboardStatus.showKeyboardPicker(context) },
        setUpLater = {
            skipped(OnboardingStep.PERMISSIONS)
            go(flow.next(step))
        },
        saveGender = {
            gender?.let(::saveGender)
            go(flow.next(step))
        },
        skipGender = {
            saveGender(GrammaticalGender.UNSPECIFIED)
            go(flow.next(step))
        }
    )

    val previous = flow.previous(step)
    BackHandler(enabled = previous != null) { previous?.let { step = it } }

    // A Surface, not a bare background: it also sets the content colour, so
    // text follows the dark theme the way it does inside AppScreen's Scaffold.
    Surface(modifier = Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        Column(modifier = Modifier.fillMaxSize().windowInsetsPadding(WindowInsets.safeDrawing)) {
            TopBar(
                step = step,
                flow = flow,
                onBack = previous?.let { { step = it } },
                onLater = { if (mode == OnboardingMode.FIRST_RUN) actions.later() else onFinished() }
            )
            // Every page starts at its top.
            key(step) {
                Column(modifier = Modifier.weight(1f).verticalScroll(rememberScrollState())) {
                    ReadableColumn(verticalArrangement = Arrangement.spacedBy(Spacing.l)) {
                        when (step) {
                            OnboardingStep.WELCOME -> WelcomeStep(profileGender)
                            OnboardingStep.TUTORIAL_COPY -> TutorialCopyStep(messageCopied) { messageCopied = true }
                            OnboardingStep.TUTORIAL_RECIPIENT -> TutorialRecipientStep(recipient) { recipient = it }
                            OnboardingStep.TUTORIAL_REPLY -> TutorialReplyStep(
                                selected = recipient ?: OnboardingExamples.defaultRecipient,
                                onSelect = {
                                    recipient = it
                                    replyCopied = false
                                },
                                copied = replyCopied,
                                onCopied = { replyCopied = true }
                            )
                            OnboardingStep.PERMISSIONS -> ConnectStep(keyboard)
                            OnboardingStep.GENDER -> GenderStep(selected = gender, onSelect = { gender = it })
                        }
                    }
                }
            }
            BottomBar(step, flow, keyboard, gender, actions)
        }
    }
}

/** One button of the bottom bar. */
private class Action(val label: String, val onClick: () -> Unit, val enabled: Boolean = true)

/**
 * Where this device is with the onboarding: the stored version (or the old
 * profile flag), how it ended, and a saved step.
 */
internal fun onboardingStatus(services: ServiceLocator): OnboardingStatus = OnboardingFlow.status(
    completedVersion = OnboardingFlow.completedVersion(
        stored = services.deviceState.completedOnboardingVersion,
        legacyCompleted = services.configuration.profile.hasCompletedOnboarding
    ),
    storedStatus = services.deviceState.onboardingStatus,
    resumeStep = services.deviceState.onboardingResumeStep
)

/** What the buttons of the bars do; the screen owns the state they change. */
private class OnboardingActions(
    val later: () -> Unit,
    val choosePath: (OnboardingPath) -> Unit,
    val next: () -> Unit,
    val skipTutorial: () -> Unit,
    val startUsing: () -> Unit,
    val openKeyboardSettings: () -> Unit,
    val chooseKeyboard: () -> Unit,
    val setUpLater: () -> Unit,
    val saveGender: () -> Unit,
    val skipGender: () -> Unit
)

private fun trackViewed(step: OnboardingStep) {
    ProductEvents.track(
        ProductEvent.ONBOARDING_STEP_VIEWED,
        mapOf("step" to ProductEventValue.Code(step.id))
    )
    when (step) {
        OnboardingStep.PERMISSIONS -> ProductEvents.track(ProductEvent.ONBOARDING_KEYBOARD_STEP_VIEWED)
        OnboardingStep.TUTORIAL_COPY -> ProductEvents.track(ProductEvent.PASTE_TUTORIAL_VIEWED)
        else -> Unit
    }
}

/**
 * The welcome screen's mark and «Позже»; elsewhere Back and, in the
 * tutorial, «Как пользоваться · 1 из 3».
 */
@Composable
private fun TopBar(step: OnboardingStep, flow: OnboardingFlow, onBack: (() -> Unit)?, onLater: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .heightIn(min = 56.dp)
            .padding(horizontal = Spacing.xxs),
        verticalAlignment = Alignment.CenterVertically
    ) {
        if (step == OnboardingStep.WELCOME) {
            Spacer(Modifier.width(Spacing.m))
            AppMark(size = 28.dp)
            Text(
                stringResource(R.string.app_name),
                style = MaterialTheme.typography.titleMedium,
                modifier = Modifier.padding(start = Spacing.xs)
            )
            Spacer(Modifier.weight(1f))
            TextButton(onClick = onLater) { Text(stringResource(R.string.onboarding_later)) }
        } else {
            if (onBack != null) {
                IconButton(onClick = onBack) {
                    Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = stringResource(R.string.cd_back))
                }
            } else {
                Spacer(Modifier.width(Spacing.m))
            }
            flow.tutorialNumber(step)?.let { number ->
                Text(
                    stringResource(R.string.onboarding_tutorial_title, number, flow.tutorialCount),
                    style = MaterialTheme.typography.titleLarge,
                    modifier = Modifier.semantics { heading() }
                )
            }
        }
    }
}

/** The page dots in the tutorial, the step's main action, and its quiet one under it. */
@Composable
private fun BottomBar(
    step: OnboardingStep,
    flow: OnboardingFlow,
    keyboard: KeyboardStatus.Snapshot,
    gender: GrammaticalGender?,
    actions: OnboardingActions
) {
    val last = flow.isLast(step)
    val continueLabel = stringResource(if (last) R.string.onboarding_finish else R.string.onboarding_next)

    val (primary, secondary) = when (step) {
        OnboardingStep.WELCOME ->
            Action(stringResource(R.string.onboarding_setup_keyboard), { actions.choosePath(OnboardingPath.SETUP_FIRST) }) to
                Action(stringResource(R.string.onboarding_try_first), { actions.choosePath(OnboardingPath.TRY_FIRST) })
        OnboardingStep.TUTORIAL_COPY, OnboardingStep.TUTORIAL_RECIPIENT ->
            Action(stringResource(R.string.onboarding_next), actions.next) to
                Action(stringResource(R.string.onboarding_skip), actions.skipTutorial)
        OnboardingStep.TUTORIAL_REPLY ->
            Action(stringResource(R.string.onboarding_start_using), actions.startUsing) to null
        OnboardingStep.PERMISSIONS -> when {
            !keyboard.isEnabled ->
                Action(stringResource(R.string.permissions_action_keyboard_settings), actions.openKeyboardSettings) to
                    Action(stringResource(R.string.onboarding_connect_later), actions.setUpLater)
            !keyboard.isSelected ->
                Action(stringResource(R.string.permissions_action_choose), actions.chooseKeyboard) to
                    Action(stringResource(R.string.onboarding_connect_later), actions.setUpLater)
            else -> Action(continueLabel, actions.next) to null
        }
        OnboardingStep.GENDER ->
            Action(continueLabel, actions.saveGender, enabled = gender != null) to
                Action(stringResource(R.string.onboarding_skip), actions.skipGender)
    }

    Box(modifier = Modifier.fillMaxWidth(), contentAlignment = Alignment.Center) {
        Column(
            modifier = Modifier
                .widthIn(max = Layout.readableWidth)
                .fillMaxWidth()
                .padding(start = Spacing.l, end = Spacing.l, top = Spacing.s, bottom = Spacing.xs),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(Spacing.xs)
        ) {
            flow.tutorialNumber(step)?.let { number ->
                ProgressDots(flow.tutorialCount, number, Modifier.padding(bottom = Spacing.xs))
            }
            PrimaryButton(text = primary.label, enabled = primary.enabled, onClick = primary.onClick)
            if (secondary != null) {
                TextButton(onClick = secondary.onClick, modifier = Modifier.heightIn(min = Layout.minimumTouchTarget)) {
                    Text(secondary.label)
                }
            } else {
                // The same height either way, so the main action never jumps between pages.
                Spacer(Modifier.heightIn(min = Layout.minimumTouchTarget))
            }
        }
    }
}
