package kz.yerek.aireply.push

import java.net.URI
import java.net.URISyntaxException
import java.util.Locale

/** The app screens a notification may open. [key] is the `aireply://<key>` name. */
enum class AppScreen(val key: String) {
    HOME("home"),
    SUBSCRIPTION("subscription"),
    SETTINGS("settings"),
    /** The Notifications section of Settings. */
    NOTIFICATIONS("notifications"),
    TEMPLATES("templates"),
    PROFILE("profile"),
    /** The keyboard setup screen. */
    KEYBOARD("keyboard"),
    COMPOSE("compose");

    companion object {
        fun fromKey(key: String): AppScreen? = entries.firstOrNull { it.key == key }
    }
}

/** Where a notification's `link` leads, after validation. */
sealed interface AppLink {
    /** A screen of this app. */
    data class Screen(val screen: AppScreen) : AppLink

    /** A page on ai-reply.kz, opened in the browser. [url] is normalized https. */
    data class Web(val url: String) : AppLink

    /** No link, or one the app will not follow: the app just opens. */
    data object None : AppLink
}

/**
 * Validates notification links. The server checks them before sending; the app
 * checks again, because a push payload is input like any other.
 *
 * Сілтемені қосымша өзі де тексереді: тек aireply:// экрандары және ai-reply.kz.
 *
 * - `aireply://<screen>` — one of [AppScreen]; an unknown screen, or anything
 *   more than the bare screen name (a path, a query, a port), opens Home.
 * - `https://ai-reply.kz/…` or a subdomain — no user info, no port — opens in
 *   the browser.
 * - Anything else — http, other hosts, intent: or javascript: URLs — is never
 *   opened; the app simply comes to the front.
 */
object AppLinks {

    const val SCHEME = "aireply"
    const val WEB_HOST = "ai-reply.kz"
    private const val MAX_LENGTH = 512

    fun parse(raw: String?): AppLink {
        val value = raw?.trim().orEmpty()
        if (value.isEmpty() || value.length > MAX_LENGTH) return AppLink.None
        if (value.any { it.isWhitespace() || it.isISOControl() || it == '\\' }) return AppLink.None
        val uri = try {
            URI(value)
        } catch (malformed: URISyntaxException) {
            return AppLink.None
        }
        return when (uri.scheme?.lowercase(Locale.ROOT)) {
            SCHEME -> AppLink.Screen(screenOf(uri))
            "https" -> web(uri)
            else -> AppLink.None
        }
    }

    private fun screenOf(uri: URI): AppScreen {
        if (uri.isOpaque) return AppScreen.HOME
        val bare = uri.rawUserInfo == null && uri.port == -1 &&
            (uri.rawPath.isNullOrEmpty() || uri.rawPath == "/") &&
            uri.rawQuery == null && uri.rawFragment == null
        if (!bare) return AppScreen.HOME
        val name = (uri.host ?: uri.rawAuthority)?.lowercase(Locale.ROOT) ?: return AppScreen.HOME
        return AppScreen.fromKey(name) ?: AppScreen.HOME
    }

    private fun web(uri: URI): AppLink {
        if (uri.isOpaque) return AppLink.None
        val host = uri.host?.lowercase(Locale.ROOT) ?: return AppLink.None
        if (uri.rawUserInfo != null || uri.port != -1) return AppLink.None
        if (host != WEB_HOST && !host.endsWith(".$WEB_HOST")) return AppLink.None
        // Rebuilt with a lower-case scheme and host: Android matches intent
        // filters on the scheme case-sensitively.
        val url = buildString {
            append("https://").append(host)
            append(uri.rawPath.orEmpty())
            uri.rawQuery?.let { append('?').append(it) }
            uri.rawFragment?.let { append('#').append(it) }
        }
        return AppLink.Web(url)
    }
}
