package kz.yerek.aireply.ui.navigation

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import kz.yerek.aireply.BuildConfig
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.feature.account.AccountController
import kz.yerek.aireply.ui.feature.account.EmailSignInScreen
import kz.yerek.aireply.ui.feature.account.RegistrationStepScreen
import kz.yerek.aireply.ui.feature.account.LegalConsentScreen
import kz.yerek.aireply.ui.feature.account.SignInScreen
import kz.yerek.aireply.ui.feature.account.SubscriptionScreen
import kz.yerek.aireply.ui.feature.account.VerifyCodeScreen
import kz.yerek.aireply.ui.feature.compose.ComposeScreen
import kz.yerek.aireply.ui.feature.home.HomeScreen
import kz.yerek.aireply.ui.feature.hours.WorkingHoursScreen
import kz.yerek.aireply.ui.feature.onboarding.OnboardingFlow
import kz.yerek.aireply.ui.feature.onboarding.OnboardingMode
import kz.yerek.aireply.ui.feature.onboarding.OnboardingScreen
import kz.yerek.aireply.ui.feature.profile.ProfileScreen
import kz.yerek.aireply.ui.feature.settings.SettingsScreen
import kz.yerek.aireply.ui.feature.setup.KeyboardSetupScreen
import kz.yerek.aireply.ui.feature.templates.TemplateEditorScreen
import kz.yerek.aireply.ui.feature.templates.TemplateListScreen

/**
 * @param debugOnboarding DEBUG builds only: open the first run straight away,
 *   before sign-in, so it can be reviewed on an emulator without an account.
 */
@Composable
fun AppNavHost(debugOnboarding: Boolean = false) {
    val services = LocalServices.current
    val navController = rememberNavController()

    val accountState by services.account.state.collectAsStateWithLifecycle()
    var completingRegistration by rememberSaveable { mutableStateOf(false) }
    var reviewingOnboarding by rememberSaveable { mutableStateOf(BuildConfig.DEBUG && debugOnboarding) }

    // Onboarding is per device: the keyboard is enabled per phone, so the
    // version lives in device storage that is never restored from a backup.
    // A profile that finished the old onboarding counts as version 1.
    var needsOnboarding by remember {
        mutableStateOf(
            OnboardingFlow.needsOnboarding(
                OnboardingFlow.completedVersion(
                    stored = services.deviceState.completedOnboardingVersion,
                    legacyCompleted = services.configuration.profile.hasCompletedOnboarding
                )
            )
        )
    }

    LaunchedEffect(Unit) { services.account.bootstrap() }

    if (reviewingOnboarding) {
        OnboardingScreen(mode = OnboardingMode.FIRST_RUN, onFinished = {
            reviewingOnboarding = false
            needsOnboarding = false
        })
        return
    }

    when {
            !accountState.bootstrapComplete -> {
                Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                    CircularProgressIndicator()
                }
                return
            }
            !accountState.hasAcceptedLegal -> {
                LegalConsentScreen()
                return
            }
            completingRegistration -> {
                RegistrationStepScreen(onFinished = { completingRegistration = false })
                return
            }
            accountState.phase is AccountController.Phase.SignedOut -> {
                SignInScreen(onSignedIn = { isNewUser -> completingRegistration = isNewUser })
                return
            }
            accountState.phase is AccountController.Phase.EnteringEmail -> {
                EmailSignInScreen()
                return
            }
            accountState.phase is AccountController.Phase.AwaitingCode -> {
                val phase = accountState.phase as AccountController.Phase.AwaitingCode
                VerifyCodeScreen(
                    phase = phase,
                    onVerified = { isNewUser -> completingRegistration = isNewUser }
                )
                return
            }
    }

    val start = if (needsOnboarding) Routes.Onboarding else Routes.Home

    NavHost(navController = navController, startDestination = start) {

        composable(Routes.Onboarding) {
            OnboardingScreen(mode = OnboardingMode.FIRST_RUN, onFinished = { needsOnboarding = false })
        }

        composable(Routes.Tutorial) {
            OnboardingScreen(mode = OnboardingMode.TUTORIAL, onFinished = navController::popBackStack)
        }

        composable(Routes.Home) {
            HomeScreen(
                onOpen = { route -> navController.navigate(route) }
            )
        }

        composable(Routes.Compose) { ComposeScreen(onBack = navController::popBackStack) }
        composable(Routes.Profile) { ProfileScreen(onBack = navController::popBackStack) }
        composable(Routes.WorkingHours) { WorkingHoursScreen(onBack = navController::popBackStack) }
        composable(Routes.KeyboardSetup) { KeyboardSetupScreen(onBack = navController::popBackStack) }
        composable(Routes.Subscription) { SubscriptionScreen(onBack = navController::popBackStack) }

        composable(Routes.Templates) {
            TemplateListScreen(
                onBack = navController::popBackStack,
                onEdit = { id -> navController.navigate(Routes.templateEditor(id)) }
            )
        }

        composable(Routes.TemplateEditorPattern) { entry ->
            val id = entry.arguments?.getString(Routes.TemplateEditorArg).orEmpty()
            TemplateEditorScreen(templateId = id, onBack = navController::popBackStack)
        }

        composable(Routes.Settings) {
            SettingsScreen(
                onBack = navController::popBackStack,
                onOpen = { route -> navController.navigate(route) }
            )
        }
    }
}
