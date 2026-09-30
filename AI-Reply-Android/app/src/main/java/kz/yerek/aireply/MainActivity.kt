package kz.yerek.aireply

import android.content.Context
import android.content.Intent
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
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.design.AIReplyTheme
import kz.yerek.aireply.ui.navigation.AppNavHost
import kz.yerek.aireply.ui.navigation.Routes

/**
 * The only Activity.
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

        val start = when (intent?.getStringExtra(EXTRA_ROUTE)) {
            ROUTE_TEMPLATES -> Routes.Templates
            ROUTE_SETTINGS -> Routes.Settings
            else -> null
        }

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
                    AppNavHost(deepLink = start)
                }
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
    }

    override fun onResume() {
        super.onResume()
        AIReplyApplication.services(this).configuration.reload()
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
