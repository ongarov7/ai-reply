package kz.yerek.aireply.ui.feature.onboarding

import androidx.activity.compose.BackHandler
import androidx.compose.animation.AnimatedContent
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.layout.Layout
import androidx.compose.ui.layout.Placeable
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.Constraints
import androidx.compose.ui.unit.Dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kz.yerek.aireply.R
import kz.yerek.aireply.analytics.ProductEvent
import kz.yerek.aireply.analytics.ProductEventValue
import kz.yerek.aireply.analytics.ProductEvents
import kz.yerek.aireply.domain.model.GrammaticalGender
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.design.LocalExtraColors
import kz.yerek.aireply.ui.design.PrimaryButton
import kz.yerek.aireply.ui.design.ReadableColumn
import kz.yerek.aireply.ui.design.SecondaryButton
import kz.yerek.aireply.ui.design.Spacing

/**
 * The first run, and the same screens again as "How to use AI Reply".
 *
 * Онбординг: әр қадамды өткізіп жіберуге болады.
 *
 * Every step can be skipped, and the whole run with "Skip tutorial". A first
 * run remembers its step, so a process killed halfway comes back to it, and
 * finishing records the version on this device. The tutorial changes nothing.
 * Which steps exist and in what order is [OnboardingFlow]'s decision.
 */
@Composable
fun OnboardingScreen(mode: OnboardingMode, onFinished: () -> Unit) {
    val services = LocalServices.current
    val configuration by services.configuration.configuration.collectAsStateWithLifecycle()
    val profileGender = configuration.profile.grammaticalGender

    val asksGender = rememberSaveable {
        OnboardingFlow.asksGender(
            mode = mode,
            genderAnswered = services.configuration.profile.grammaticalGender != null,
            startedAsking = services.deviceState.onboardingAsksGender
        )
    }
    val flow = remember(asksGender) { OnboardingFlow(mode, asksGender) }
    var step by rememberSaveable {
        mutableStateOf(flow.start(if (flow.resumes) services.deviceState.onboardingResumeStep else null))
    }
    var gender by rememberSaveable { mutableStateOf(profileGender?.takeIf { it.isSpecified }) }

    // Read before the first step is saved: a run that comes back after the
    // process was killed was started already.
    var started by rememberSaveable {
        mutableStateOf(!OnboardingFlow.startsRun(mode, services.deviceState.onboardingResumeStep))
    }
    var viewed by rememberSaveable { mutableStateOf<OnboardingStep?>(null) }
    LaunchedEffect(Unit) {
        if (started) return@LaunchedEffect
        started = true
        ProductEvents.track(
            ProductEvent.ONBOARDING_STARTED,
            mapOf(
                "version" to ProductEventValue.Number(OnboardingFlow.CURRENT_VERSION),
                "trigger" to ProductEventValue.Code(if (mode == OnboardingMode.FIRST_RUN) "auto" else "settings")
            )
        )
    }
    LaunchedEffect(step) {
        if (flow.resumes) {
            services.deviceState.onboardingResumeStep = step.id
            services.deviceState.onboardingAsksGender = flow.asksGender
        }
        if (viewed == step) return@LaunchedEffect
        viewed = step
        trackViewed(step)
    }

    fun finish(skipped: Boolean) {
        if (mode == OnboardingMode.FIRST_RUN) {
            services.deviceState.completeOnboarding(OnboardingFlow.CURRENT_VERSION)
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

    fun advance() {
        flow.next(step)?.let { step = it } ?: finish(skipped = false)
    }

    fun skip() {
        if (step == OnboardingStep.GENDER) saveGender(GrammaticalGender.UNSPECIFIED)
        advance()
    }

    fun proceed() {
        if (step == OnboardingStep.GENDER) gender?.let(::saveGender)
        advance()
    }

    // A Surface, not a bare background: it also sets the content colour, so
    // text follows the dark theme the way it does inside AppScreen's Scaffold.
    Surface(modifier = Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        OnboardingLayout(
            flow = flow,
            step = step,
            gender = gender,
            profileGender = profileGender,
            onGender = { gender = it },
            onStep = { step = it },
            onSkip = ::skip,
            onProceed = ::proceed,
            onFinish = ::finish
        )
    }
}

@Composable
private fun OnboardingLayout(
    flow: OnboardingFlow,
    step: OnboardingStep,
    gender: GrammaticalGender?,
    profileGender: GrammaticalGender?,
    onGender: (GrammaticalGender) -> Unit,
    onStep: (OnboardingStep) -> Unit,
    onSkip: () -> Unit,
    onProceed: () -> Unit,
    onFinish: (skipped: Boolean) -> Unit
) {
    val previous = flow.previous(step)
    BackHandler(enabled = previous != null) { previous?.let(onStep) }

    Column(modifier = Modifier.fillMaxSize().windowInsetsPadding(WindowInsets.safeDrawing)) {
        flow.number(step)?.let { number -> Progress(number, flow.numberedCount) }

        AnimatedContent(targetState = step, label = "onboarding", modifier = Modifier.weight(1f)) { current ->
            Column(modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
                ReadableColumn(verticalArrangement = Arrangement.spacedBy(Spacing.l)) {
                    when (current) {
                        OnboardingStep.WELCOME -> WelcomeStep(onSkipTutorial = { onFinish(true) })
                        OnboardingStep.GENDER -> GenderStep(selected = gender, onSelect = onGender)
                        OnboardingStep.KEYBOARD -> KeyboardStep(onSkipTutorial = { onFinish(true) })
                        OnboardingStep.COPY_REPLY -> CopyReplyStep(profileGender)
                        OnboardingStep.PRACTICE -> PracticeStep(profileGender)
                    }
                }
            }
        }

        StepBar(
            showsBack = previous != null,
            showsSkip = step != OnboardingStep.WELCOME && !flow.isLast(step),
            primary = stringResource(
                when {
                    step == OnboardingStep.WELCOME -> R.string.onboarding_start
                    flow.isLast(step) -> R.string.onboarding_finish
                    else -> R.string.onboarding_next
                }
            ),
            primaryEnabled = step != OnboardingStep.GENDER || gender != null,
            onBack = { previous?.let(onStep) },
            onSkip = onSkip,
            onPrimary = onProceed
        )
    }
}

private fun trackViewed(step: OnboardingStep) {
    ProductEvents.track(
        ProductEvent.ONBOARDING_STEP_VIEWED,
        mapOf("step" to ProductEventValue.Code(step.id))
    )
    when (step) {
        OnboardingStep.KEYBOARD -> ProductEvents.track(ProductEvent.ONBOARDING_KEYBOARD_STEP_VIEWED)
        OnboardingStep.COPY_REPLY -> ProductEvents.track(ProductEvent.PASTE_TUTORIAL_VIEWED)
        else -> Unit
    }
}

@Composable
private fun Progress(number: Int, count: Int) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(start = Spacing.l, top = Spacing.s, end = Spacing.l),
        horizontalAlignment = Alignment.CenterHorizontally
    ) {
        LinearProgressIndicator(
            progress = { number.toFloat() / count },
            modifier = Modifier.fillMaxWidth()
        )
        Text(
            stringResource(R.string.onboarding_step, number, count),
            style = MaterialTheme.typography.labelSmall,
            color = LocalExtraColors.current.textSecondary,
            modifier = Modifier.padding(top = Spacing.xxs)
        )
    }
}

/** Back, Skip and the step's main action, laid out by [StepBarLayout]. */
@Composable
private fun StepBar(
    showsBack: Boolean,
    showsSkip: Boolean,
    primary: String,
    primaryEnabled: Boolean,
    onBack: () -> Unit,
    onSkip: () -> Unit,
    onPrimary: () -> Unit
) {
    StepBarLayout(
        spacing = Spacing.s,
        modifier = Modifier
            .fillMaxWidth()
            .padding(Spacing.l)
    ) {
        if (showsBack) SecondaryButton(stringResource(R.string.onboarding_back), onClick = onBack)
        if (showsSkip) SecondaryButton(stringResource(R.string.onboarding_skip), onClick = onSkip)
        PrimaryButton(text = primary, enabled = primaryEnabled, onClick = onPrimary)
    }
}

/**
 * One line when every label fits on one line there, the main action (the
 * last child) taking the room that is left. Otherwise the quiet buttons share
 * a line above a full-width main action, and when even they do not fit side by
 * side, each button gets a line of its own. A label never breaks inside a
 * word, whatever the language and the text size.
 */
@Composable
private fun StepBarLayout(spacing: Dp, modifier: Modifier = Modifier, content: @Composable () -> Unit) {
    Layout(content = content, modifier = modifier) { measurables, constraints ->
        val gap = spacing.roundToPx()
        val natural = measurables.map { it.maxIntrinsicWidth(Constraints.Infinity) }
        val main = measurables.lastIndex
        val quiet = measurables.indices.filter { it != main }
        val oneLine = natural.sum() + gap * main
        val width = if (constraints.hasBoundedWidth) constraints.maxWidth else oneLine

        // Each line is a list of (child, width) pairs.
        val lines: List<List<Pair<Int, Int>>> = when {
            oneLine <= width -> listOf(
                quiet.map { it to natural[it] } + (main to width - quiet.sumOf { natural[it] } - gap * quiet.size)
            )
            quiet.isEmpty() -> listOf(listOf(main to width))
            else -> {
                val quietLine = quiet.sumOf { natural[it] } + gap * (quiet.size - 1)
                if (quietLine <= width) {
                    // The room left over is shared out, so the line is as wide as the main action.
                    val extra = (width - quietLine) / quiet.size
                    listOf(quiet.map { it to natural[it] + extra }, listOf(main to width))
                } else {
                    measurables.indices.map { listOf(it to width) }
                }
            }
        }

        val placeables = arrayOfNulls<Placeable>(measurables.size)
        val lineHeights = lines.map { line ->
            line.maxOf { (index, lineWidth) ->
                val itemWidth = lineWidth.coerceAtLeast(0)
                measurables[index]
                    .measure(constraints.copy(minWidth = itemWidth, maxWidth = itemWidth, minHeight = 0))
                    .also { placeables[index] = it }
                    .height
            }
        }
        val height = lineHeights.sum() + gap * (lines.size - 1)

        layout(width, height) {
            var y = 0
            lines.forEachIndexed { row, line ->
                var x = 0
                line.forEach { (index, _) ->
                    val placeable = requireNotNull(placeables[index])
                    placeable.placeRelative(x, y + (lineHeights[row] - placeable.height) / 2)
                    x += placeable.width + gap
                }
                y += lineHeights[row] + gap
            }
        }
    }
}

/** A step's title and the sentence under it. */
@Composable
internal fun StepHeader(heading: String, prompt: String) {
    Column(verticalArrangement = Arrangement.spacedBy(Spacing.xs)) {
        Text(heading, style = MaterialTheme.typography.headlineMedium)
        Footnote(prompt)
    }
}

/** Ends the run from an early step, for someone who already knows the app. */
@Composable
internal fun ColumnScope.SkipTutorialButton(onClick: () -> Unit) {
    TextButton(onClick = onClick, modifier = Modifier.align(Alignment.CenterHorizontally)) {
        Text(stringResource(R.string.onboarding_skip_tutorial))
    }
}
