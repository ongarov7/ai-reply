package kz.yerek.aireply.ui.navigation

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kz.yerek.aireply.push.AppScreen

/**
 * A screen asked for from outside the navigation graph — a tapped
 * notification — kept until the app can show it.
 *
 * Сыртқы сілтеме күте тұрады: кіру, келісім не онбординг аяқталғанша.
 *
 * The request survives the gates: while sign-in, the legal consent, the
 * registration step or onboarding is on screen, it stays pending and is applied
 * the moment Home is reachable, never around a gate. A newer request replaces
 * an older one; applying it consumes it, so it cannot fire twice.
 */
class PendingNavigation {

    private val _route = MutableStateFlow<String?>(null)
    val route: StateFlow<String?> = _route.asStateFlow()

    fun open(route: String) {
        _route.value = route
    }

    fun open(screen: AppScreen) = open(routeFor(screen))

    /** Taken by the navigation host; false if another request replaced it meanwhile. */
    fun consume(route: String): Boolean = _route.compareAndSet(route, null)

    companion object {
        /** Where each `aireply://<screen>` leads. */
        fun routeFor(screen: AppScreen): String = when (screen) {
            AppScreen.HOME -> Routes.Home
            AppScreen.SUBSCRIPTION -> Routes.Subscription
            AppScreen.SETTINGS -> Routes.Settings
            AppScreen.NOTIFICATIONS -> Routes.settings(Routes.SectionNotifications)
            AppScreen.TEMPLATES -> Routes.Templates
            AppScreen.PROFILE -> Routes.Profile
            AppScreen.KEYBOARD -> Routes.KeyboardSetup
            AppScreen.COMPOSE -> Routes.Compose
        }

        /**
         * Whether a pending route may be applied now: only once the graph has
         * Home as its start, i.e. every gate including onboarding is behind.
         */
        fun canApply(startDestination: String): Boolean = startDestination == Routes.Home
    }
}
