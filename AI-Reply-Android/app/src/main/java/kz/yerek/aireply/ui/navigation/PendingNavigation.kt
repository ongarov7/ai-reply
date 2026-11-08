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
 * an older one; applying it consumes it, so it cannot fire twice. One that
 * waited longer than [LIFETIME_MS] is dropped: signing in much later must not
 * jump to a screen the user has forgotten asking for.
 */
class PendingNavigation(private val clock: () -> Long = System::currentTimeMillis) {

    private val _route = MutableStateFlow<String?>(null)
    val route: StateFlow<String?> = _route.asStateFlow()

    @Volatile
    private var openedAt = 0L

    fun open(route: String) {
        openedAt = clock()
        _route.value = route
    }

    fun open(screen: AppScreen) = open(routeFor(screen))

    /**
     * Taken by the navigation host; false if another request replaced it
     * meanwhile, or it waited too long and is gone.
     */
    fun consume(route: String): Boolean =
        _route.compareAndSet(route, null) && clock() - openedAt <= LIFETIME_MS

    /**
     * The first run has just ended: a link to the guide that waited behind it
     * has been answered by the guide itself. Any other link goes on waiting.
     */
    fun firstRunFinished() {
        consume(Routes.Tutorial)
    }

    companion object {
        /** How long a link waits behind the gates: fifteen minutes. */
        const val LIFETIME_MS = 15L * 60 * 1000

        /** Where each `aireply://<screen>` leads. */
        fun routeFor(screen: AppScreen): String = when (screen) {
            AppScreen.HOME -> Routes.Home
            AppScreen.SUBSCRIPTION -> Routes.Subscription
            AppScreen.SETTINGS -> Routes.Settings
            AppScreen.NOTIFICATIONS -> Routes.settings(Routes.SectionNotifications)
            AppScreen.TEMPLATES -> Routes.Templates
            AppScreen.PROFILE -> Routes.Profile
            AppScreen.KEYBOARD, AppScreen.PERMISSIONS -> Routes.Permissions
            AppScreen.COMPOSE -> Routes.Compose
            AppScreen.ONBOARDING -> Routes.Tutorial
            AppScreen.PASTE_HELP -> Routes.PasteHelp
            // With the chat off the screen says so and offers the support page.
            AppScreen.SUPPORT -> Routes.Support
            // With purchases off the screen says so and leads to the plans: never Home.
            AppScreen.PURCHASE -> Routes.Purchase
        }

        /**
         * Whether a pending route may be applied now: only once the graph has
         * Home as its start, i.e. every gate including onboarding is behind.
         */
        fun canApply(startDestination: String): Boolean = startDestination == Routes.Home
    }
}
