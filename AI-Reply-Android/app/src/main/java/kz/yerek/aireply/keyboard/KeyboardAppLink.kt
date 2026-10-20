package kz.yerek.aireply.keyboard

import android.content.Context
import android.content.Intent
import kz.yerek.aireply.MainActivity
import kz.yerek.aireply.push.PushPayload

/**
 * The keyboard opening a screen of the app - today only the purchase screen,
 * from «Лимит ответов исчерпан».
 *
 * It goes through the very extras a tapped notification carries (`nid`,
 * `link`), so MainActivity validates the link with [kz.yerek.aireply.push.AppLinks]
 * and routes it like any push link: one way in, one allow-list. The id is
 * [SOURCE], not a notification's - with no delivery id nothing is reported to
 * the server as opened. A build that does not know `aireply://purchase` yet
 * opens Home, the same fallback as for any unknown screen.
 *
 * Пернетақтадан қосымшаның экранын push сілтемесі арқылы ашу.
 */
object KeyboardAppLink {

    /** The app's screen for buying replies or changing the plan (contract §3.5). */
    const val PURCHASE = "aireply://purchase"

    /** Stands in for a notification id: this link came from the keyboard. */
    const val SOURCE = "keyboard"

    /** The extras MainActivity reads, for [link]. */
    fun extras(link: String): Map<String, String> = mapOf(
        PushPayload.KEY_NOTIFICATION_ID to SOURCE,
        PushPayload.KEY_LINK to link
    )

    fun intent(context: Context, link: String): Intent =
        Intent(context, MainActivity::class.java)
            // From a service: a task of its own, or the one the app already has.
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            .apply { extras(link).forEach { (key, value) -> putExtra(key, value) } }

    /** Opens the app on [link]; false when the system refused. */
    fun open(context: Context, link: String): Boolean =
        runCatching { context.startActivity(intent(context, link)) }.isSuccess
}
