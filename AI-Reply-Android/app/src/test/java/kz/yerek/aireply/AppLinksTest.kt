package kz.yerek.aireply

import kz.yerek.aireply.push.AppLink
import kz.yerek.aireply.push.AppLinks
import kz.yerek.aireply.push.AppScreen
import kz.yerek.aireply.push.PushPayload
import kz.yerek.aireply.telemetry.EventSchema
import kz.yerek.aireply.ui.navigation.PendingNavigation
import kz.yerek.aireply.ui.navigation.Routes
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Notification links and payloads: what a tap may open, and nothing else.
 *
 * Хабарлама сілтемесі: тек aireply:// экрандары және ai-reply.kz беттері.
 */
class AppLinksTest {

    // ------------------------------------------------------------ aireply://

    @Test
    fun `every app screen the server may name opens that screen`() {
        AppScreen.entries.forEach { screen ->
            assertEquals(screen.key, AppLink.Screen(screen), AppLinks.parse("aireply://${screen.key}"))
        }
        assertEquals(AppLink.Screen(AppScreen.SETTINGS), AppLinks.parse("AIREPLY://Settings"))
        assertEquals(AppLink.Screen(AppScreen.SUBSCRIPTION), AppLinks.parse("  aireply://subscription/  "))
    }

    @Test
    fun `an unknown or decorated app link opens home`() {
        listOf(
            "aireply://payments",
            "aireply://settings/notifications",
            "aireply://settings?tab=1",
            "aireply://settings#top",
            "aireply://user@settings",
            "aireply://settings:80",
            "aireply:home"
        ).forEach { link ->
            assertEquals(link, AppLink.Screen(AppScreen.HOME), AppLinks.parse(link))
        }
    }

    // ------------------------------------------------------------- https://

    @Test
    fun `pages on ai-reply kz open in the browser`() {
        assertEquals(AppLink.Web("https://ai-reply.kz/pricing"), AppLinks.parse("https://ai-reply.kz/pricing"))
        assertEquals(AppLink.Web("https://ai-reply.kz"), AppLinks.parse("https://ai-reply.kz"))
        assertEquals(
            AppLink.Web("https://help.ai-reply.kz/a/b?lang=kk#faq"),
            AppLinks.parse("https://help.ai-reply.kz/a/b?lang=kk#faq")
        )
        assertEquals(
            "scheme and host are normalized: Android matches schemes case-sensitively",
            AppLink.Web("https://ai-reply.kz/Offer"),
            AppLinks.parse("HTTPS://AI-REPLY.KZ/Offer")
        )
    }

    @Test
    fun `anything else is never opened`() {
        listOf(
            null, "", "   ",
            "http://ai-reply.kz/pricing",
            "https://ai-reply.kz.evil.com/",
            "https://evilai-reply.kz/",
            "https://ai-reply.kz@evil.com/",
            "https://user@ai-reply.kz/",
            "https://ai-reply.kz:8443/",
            "https://ai-reply.kz\\@evil.com/",
            "https://ai-reply.kz/a b",
            "javascript:alert(1)",
            "intent://settings#Intent;scheme=aireply;end",
            "file:///data/data/kz.yerek.aireply/",
            "content://kz.yerek.aireply/",
            "market://details?id=kz.yerek.aireply",
            "https://" + "a".repeat(600) + ".ai-reply.kz/"
        ).forEach { link ->
            assertEquals(link.toString(), AppLink.None, AppLinks.parse(link))
        }
    }

    // --------------------------------------------------------------- payload

    @Test
    fun `a push payload becomes a destination and an open event`() {
        val payload = PushPayload.from(
            mapOf(
                "nid" to "0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d",
                "did" to "5e55a0b1d2c3",
                "type" to "subscription_expiring",
                "category" to "subscription",
                "link" to "aireply://subscription",
                // What Firebase adds to the launch intent is ignored.
                "google.message_id" to "0:1234",
                "from" to "1234567890"
            )
        )!!

        assertEquals(AppLink.Screen(AppScreen.SUBSCRIPTION), payload.link)
        assertEquals(PushPayload.CHANNEL_IMPORTANT, payload.channelId)
        val properties = payload.openedEventProperties()!!
        assertEquals(
            mapOf(
                "notification_id" to "0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d",
                "delivery_id" to "5e55a0b1d2c3",
                "type" to "subscription_expiring"
            ),
            properties
        )
        assertTrue(EventSchema.isValid(EventSchema.NOTIFICATION_OPENED, properties))
    }

    @Test
    fun `an intent without a notification id is not one of ours`() {
        assertNull(PushPayload.from(emptyMap()))
        assertNull(PushPayload.from(mapOf("link" to "aireply://settings")))
        assertNull(PushPayload.from(mapOf("nid" to "  ")))
    }

    @Test
    fun `no link just opens the app, and ids the server would refuse send no event`() {
        val plain = PushPayload.from(mapOf("nid" to "n-1", "did" to "has space", "type" to "promo"))!!
        assertEquals(AppLink.None, plain.link)
        assertEquals("the bad delivery id is left out", mapOf("notification_id" to "n-1", "type" to "promo"),
            plain.openedEventProperties())

        val refused = PushPayload.from(mapOf("nid" to "x".repeat(65)))!!
        assertNull(refused.openedEventProperties())
    }

    @Test
    fun `channels follow the server's rule`() {
        listOf("account", "subscription", "security").forEach {
            assertEquals(it, PushPayload.CHANNEL_IMPORTANT, PushPayload.channelFor(it))
        }
        listOf("system", "marketing", "future", null).forEach {
            assertEquals(it.toString(), PushPayload.CHANNEL_GENERAL, PushPayload.channelFor(it))
        }
    }

    // ------------------------------------------------------------ navigation

    @Test
    fun `each screen maps to its route`() {
        val expected = mapOf(
            AppScreen.HOME to Routes.Home,
            AppScreen.SUBSCRIPTION to Routes.Subscription,
            AppScreen.SETTINGS to Routes.Settings,
            AppScreen.NOTIFICATIONS to "settings?section=notifications",
            AppScreen.TEMPLATES to Routes.Templates,
            AppScreen.PROFILE to Routes.Profile,
            AppScreen.KEYBOARD to Routes.KeyboardSetup,
            AppScreen.COMPOSE to Routes.Compose
        )
        assertEquals(AppScreen.entries.toSet(), expected.keys)
        expected.forEach { (screen, route) -> assertEquals(screen.key, route, PendingNavigation.routeFor(screen)) }
        assertEquals("settings?section={section}", Routes.SettingsPattern)
    }

    @Test
    fun `a pending destination waits for the gates and is used once`() {
        val navigation = PendingNavigation()
        navigation.open(AppScreen.SUBSCRIPTION)
        assertEquals(Routes.Subscription, navigation.route.value)

        assertFalse("onboarding is showing: keep waiting", PendingNavigation.canApply(Routes.Onboarding))
        assertTrue(PendingNavigation.canApply(Routes.Home))

        // A newer tap replaces an older one that was never applied.
        navigation.open(Routes.Templates)
        assertFalse(navigation.consume(Routes.Subscription))
        assertTrue(navigation.consume(Routes.Templates))
        assertNull(navigation.route.value)
        assertFalse("applied once only", navigation.consume(Routes.Templates))
    }
}
