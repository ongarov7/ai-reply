package kz.yerek.aireply

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.core.view.WindowCompat
import androidx.lifecycle.lifecycleScope
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch
import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.core.lang.LocalizedContext
import kz.yerek.aireply.data.settings.AppearancePreference
import kz.yerek.aireply.push.AppLink
import kz.yerek.aireply.push.PushPayload
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.design.AIReplyTheme
import kz.yerek.aireply.ui.navigation.AppNavHost
import kz.yerek.aireply.ui.navigation.Routes

/**
 * The only Activity.
 *
 * WHAT OPENS IT. The launcher; the keyboard's "+" chip ([EXTRA_ROUTE]); and a
 * tapped notification, whether the system showed it (the app was in the
 * background: Firebase starts the launcher activity with the push data as
 * String extras) or the app did (in the foreground: the same extras). It is
 * singleTask, so a tap while it exists arrives in [onNewIntent] — handled
 * exactly like a cold start. The destination goes to PendingNavigation, which
 * holds it until every gate (consent, sign-in, onboarding) is behind.
 *
 * LANGUAGE. The interface language is applied in [attachBaseContext] by wrapping
 * the base Context, which is the mechanism that works identically on every API
 * level this app supports and, crucially, is the same mechanism the keyboard
 * uses. One approach, two components, no divergence. When the user changes the
 * language the Activity is recreated, because a Context's locale is fixed once
 * its resources are resolved.
 */
class MainActivity : ComponentActivity() {

    private var attachedLanguage: AppLanguage? = null

    override fun attachBaseContext(newBase: Context) {
        val language = AIReplyApplication.services(newBase).settings.appLanguage
        attachedLanguage = language
        super.attachBaseContext(LocalizedContext.wrap(newBase, language))
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()

        val services = AIReplyApplication.services(this)
        // The keyboard may have run since this screen was last open, and it
        // reads the same file. Re-reading here keeps the two in step.
        services.configuration.reload()

        observeLanguageChanges()

        // Channels in the current language, the push token, the installation.
        services.push.onAppLaunched()

        // A re-created Activity (restored state) must not replay the intent
        // that opened it the first time; a new one arrives in onNewIntent.
        if (savedInstanceState == null) handleExternalIntent(intent)

        setContent {
            // Built once: a Flow made during composition would be rebuilt, and
            // re-subscribed, on every recomposition.
            val appearanceChanges = remember {
                services.settings.changes().map { services.settings.appearance }
            }
            val appearance by appearanceChanges.collectAsState(initial = services.settings.appearance)
            val systemDark = isSystemInDarkTheme()
            val dark = when (appearance) {
                AppearancePreference.SYSTEM -> systemDark
                AppearancePreference.LIGHT -> false
                AppearancePreference.DARK -> true
            }

            SideEffect {
                WindowCompat.getInsetsController(window, window.decorView).apply {
                    isAppearanceLightStatusBars = !dark
                    isAppearanceLightNavigationBars = !dark
                }
            }

            CompositionLocalProvider(LocalServices provides services) {
                AIReplyTheme(appearance = appearance) {
                    AppNavHost()
                }
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        handleExternalIntent(intent)
    }

    override fun onResume() {
        super.onResume()
        val services = AIReplyApplication.services(this)
        services.configuration.reload()
        // The notification permission may have changed in system settings.
        services.push.onResume()
    }

    /**
     * The keyboard's "+" chip or a tapped notification. Handled extras are
     * removed, so a re-creation of this Activity cannot open them again.
     */
    private fun handleExternalIntent(intent: Intent?) {
        if (intent == null) return
        // Reopened from Recents: the extras are the ones already handled.
        if (intent.flags and Intent.FLAG_ACTIVITY_LAUNCHED_FROM_HISTORY != 0) return
        val services = AIReplyApplication.services(this)

        when (intent.getStringExtra(EXTRA_ROUTE)) {
            ROUTE_TEMPLATES -> services.navigation.open(Routes.Templates)
            ROUTE_SETTINGS -> services.navigation.open(Routes.Settings)
        }
        intent.removeExtra(EXTRA_ROUTE)

        val extras = intent.extras ?: return
        if (!extras.containsKey(PushPayload.KEY_NOTIFICATION_ID)) return
        val data = PushPayload.KEYS.associateWith { key -> extras.getString(key) }
        PushPayload.KEYS.forEach { key -> intent.removeExtra(key) }
        when (val link = services.push.onNotificationOpened(data)) {
            is AppLink.Screen -> services.navigation.open(link.screen)
            is AppLink.Web -> openInBrowser(link.url)
            AppLink.None, null -> Unit
        }
    }

    /** A validated https page on ai-reply.kz; see AppLinks. */
    private fun openInBrowser(url: String) {
        val view = Intent(Intent.ACTION_VIEW, Uri.parse(url)).addCategory(Intent.CATEGORY_BROWSABLE)
        try {
            startActivity(view)
        } catch (none: ActivityNotFoundException) {
            // No browser: the app is open, which is the fallback anyway.
        }
    }

    /**
     * A language change cannot be applied to a Context that already resolved its
     * resources, so the screen is rebuilt. Done here rather than in the settings
     * screen so every route gets it, including one reached by a deep link.
     */
    private fun observeLanguageChanges() {
        val settings = AIReplyApplication.services(this).settings
        lifecycleScope.launch {
            settings.changes().collect {
                if (settings.appLanguage != attachedLanguage) recreate()
            }
        }
    }

    companion object {
        const val EXTRA_ROUTE = "kz.yerek.aireply.route"
        const val ROUTE_TEMPLATES = "templates"
        const val ROUTE_SETTINGS = "settings"
    }
}
