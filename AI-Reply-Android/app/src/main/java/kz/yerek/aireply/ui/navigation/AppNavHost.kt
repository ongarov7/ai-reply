package kz.yerek.aireply.ui.navigation

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavHostController
import androidx.navigation.NavType
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import androidx.navigation.navArgument
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
import kz.yerek.aireply.ui.feature.onboarding.OnboardingScreen
import kz.yerek.aireply.ui.feature.profile.ProfileScreen
import kz.yerek.aireply.ui.feature.settings.SettingsScreen
import kz.yerek.aireply.ui.feature.setup.KeyboardSetupScreen
import kz.yerek.aireply.ui.feature.templates.TemplateEditorScreen
import kz.yerek.aireply.ui.feature.templates.TemplateListScreen

/**
 * The gates, then the graph.
 *
 * A screen asked for from outside — a tapped notification, the keyboard's "+"
 * chip — waits in [PendingNavigation] while any gate (legal consent, sign-in,
 * the registration step, onboarding) is showing, and is opened on top of Home
 * once they are all behind. It never skips one.
 */
@Composable
fun AppNavHost() {
    val services = LocalServices.current
    val configuration by services.configuration.configuration.collectAsStateWithLifecycle()
    val navController = rememberNavController()

    val accountState by services.account.state.collectAsStateWithLifecycle()
    var completingRegistration by rememberSaveable { mutableStateOf(false) }

    LaunchedEffect(Unit) { services.account.bootstrap() }

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

    // Onboarding runs once. `hasCompletedOnboarding` lives with the profile, so
    // it survives relaunches, and Settings can put the user back through it
    // without losing a single answer.
    val start = if (configuration.profile.hasCompletedOnboarding) Routes.Home else Routes.Onboarding

    NavHost(navController = navController, startDestination = start) {

        composable(Routes.Onboarding) {
            OnboardingScreen(onFinished = { services.configuration.completeOnboarding() })
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

        composable(
            route = Routes.SettingsPattern,
            arguments = listOf(
                navArgument(Routes.SettingsSectionArg) {
                    type = NavType.StringType
                    nullable = true
                    defaultValue = null
                }
            )
        ) { entry ->
            SettingsScreen(
                onBack = navController::popBackStack,
                onOpen = { route -> navController.navigate(route) },
                focusSection = entry.arguments?.getString(Routes.SettingsSectionArg)
            )
        }
    }

    // Reached only with every gate behind: a pending link opens now. During
    // onboarding the start is not Home yet, and the link keeps waiting.
    val pending by services.navigation.route.collectAsStateWithLifecycle()
    LaunchedEffect(pending, start) {
        val route = pending ?: return@LaunchedEffect
        if (!PendingNavigation.canApply(start)) return@LaunchedEffect
        if (services.navigation.consume(route)) navController.openFromOutside(route)
    }
}

/** Opens [route] on top of Home: the back button then leads to Home, never out of the app. */
private fun NavHostController.openFromOutside(route: String) {
    if (route == Routes.Home) {
        popBackStack(Routes.Home, inclusive = false)
        return
    }
    navigate(route) {
        popUpTo(Routes.Home) { inclusive = false }
        launchSingleTop = true
    }
}
