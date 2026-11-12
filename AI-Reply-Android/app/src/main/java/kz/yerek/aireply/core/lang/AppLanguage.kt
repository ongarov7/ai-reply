package kz.yerek.aireply.core.lang

import androidx.core.os.ConfigurationCompat
import android.content.res.Resources
import java.util.Locale

/**
 * The language the CONTAINING APP's interface is presented in.
 *
 * Deliberately separate from [KeyboardLanguage], which is the language of the
 * currently selected keyboard LAYOUT. The two are different by design: someone
 * with a Russian phone can be typing Kazakh, and the keyboard's own key caps
 * have to follow the layout while the product UI follows the app. Collapsing
 * them into one type would force one of those to be wrong.
 */
enum class AppLanguage(val code: String) {
    ENGLISH("en"),
    RUSSIAN("ru"),
    KAZAKH("kk"),
    UZBEK("uz");

    /** Locale used for speech recognition and for formatting. */
    val locale: Locale
        get() = when (this) {
            ENGLISH -> Locale("en", "US")
            RUSSIAN -> Locale("ru", "RU")
            KAZAKH -> Locale("kk", "KZ")
            UZBEK -> Locale("uz", "UZ")
        }

    /** BCP-47 tag, which is what [android.speech.RecognizerIntent] wants. */
    val languageTag: String
        get() = when (this) {
            ENGLISH -> "en-US"
            RUSSIAN -> "ru-RU"
            KAZAKH -> "kk-KZ"
            UZBEK -> "uz-UZ"
        }

    /**
     * The language's name in that language. Never localized into another one: a
     * language picker that says "Kazakh" to a Kazakh speaker is a worse picker
     * than one that says "Қазақша".
     */
    val nativeName: String
        get() = when (this) {
            ENGLISH -> "English"
            RUSSIAN -> "Русский"
            KAZAKH -> "Қазақша"
            UZBEK -> "Oʻzbekcha"
        }

    /**
     * The keyboard LAYOUT that corresponds to this interface language.
     *
     * A mapping, not an equivalence: the app language never changes which
     * layout the user is typing on.
     */
    val keyboardLanguage: KeyboardLanguage
        get() = when (this) {
            ENGLISH -> KeyboardLanguage.ENGLISH
            RUSSIAN -> KeyboardLanguage.RUSSIAN
            KAZAKH -> KeyboardLanguage.KAZAKH
            UZBEK -> KeyboardLanguage.ENGLISH
        }

    companion object {
        fun fromCode(code: String?): AppLanguage? =
            entries.firstOrNull { it.code == code }

        /**
         * The app language implied by the device's own preferences, used when
         * the user has not chosen an explicit override.
         */
        fun systemDefault(): AppLanguage {
            val locales = ConfigurationCompat.getLocales(Resources.getSystem().configuration)
            return fromPreferredLocales((0 until locales.size()).mapNotNull { locales[it] })
        }

        /** Preserve the phone's preference order, skipping languages the app does not ship. */
        fun fromPreferredLocales(locales: Iterable<Locale>): AppLanguage {
            for (candidate in locales) {
                fromCode(candidate.language.lowercase(Locale.ROOT))?.let { return it }
            }
            return ENGLISH
        }
    }
}
