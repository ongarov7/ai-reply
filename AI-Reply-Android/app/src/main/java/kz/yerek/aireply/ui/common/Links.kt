package kz.yerek.aireply.ui.common

import android.app.Activity
import android.content.Context
import android.content.Intent
import android.widget.Toast
import androidx.core.net.toUri
import kz.yerek.aireply.R

/**
 * Opens a web page (the terms, the privacy policy, support) in the browser,
 * with `lang` set to the app's language when [language] is given.
 *
 * A phone with no browser, or a work profile that blocks one, throws
 * ActivityNotFoundException here; the user is told instead of the app
 * crashing.
 */
fun Context.openWebPage(url: String, language: String? = null) {
    val opened = runCatching {
        val uri = url.toUri().buildUpon()
            .apply { if (language != null) appendQueryParameter("lang", language) }
            .build()
        val intent = Intent(Intent.ACTION_VIEW, uri)
        if (this !is Activity) intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        startActivity(intent)
    }.isSuccess
    if (!opened) Toast.makeText(this, R.string.common_link_failed, Toast.LENGTH_SHORT).show()
}
