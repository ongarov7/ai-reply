package kz.yerek.aireply.ui.common

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.os.Build
import androidx.annotation.ChecksSdkIntAtLeast
import kz.yerek.aireply.R

/**
 * Copy, for the buttons that say Copy. Only ever on the user's tap, with the
 * app's name as the label and nothing else attached; reading goes through
 * [kz.yerek.aireply.keyboard.input.ContextTextProvider].
 */
object AppClipboard {

    /** True when the text reached the clipboard. */
    fun copy(context: Context, text: String): Boolean {
        val clipboard = context.getSystemService(Context.CLIPBOARD_SERVICE) as? ClipboardManager ?: return false
        return runCatching {
            clipboard.setPrimaryClip(ClipData.newPlainText(context.getString(R.string.app_name), text))
        }.isSuccess
    }

    /**
     * Android 13 and later confirm every copy with their own panel, so the
     * app adds no message of its own there.
     */
    @get:ChecksSdkIntAtLeast(api = Build.VERSION_CODES.TIRAMISU)
    val systemConfirmsCopy: Boolean
        get() = Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU
}
