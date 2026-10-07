package kz.yerek.aireply.voice

import kz.yerek.aireply.core.lang.KeyboardLanguage
import java.util.Locale

/**
 * Which language the recogniser listens for.
 *
 * THE LAYOUT DECIDES, in the reply composer and in Create alike: on ҚАЗ the
 * user is about to say what they would otherwise type in Kazakh, on РУС in
 * Russian, on ENG in English. The app's own language says nothing about that -
 * a Russian-speaking user with the app in English still dictates in Russian on
 * the Russian layout. This is the one place the mapping lives.
 */
object RecognitionLanguage {

    /** BCP-47 tag for [layout], as `RecognizerIntent.EXTRA_LANGUAGE` expects it. */
    fun tagFor(layout: KeyboardLanguage): String = when (layout) {
        KeyboardLanguage.KAZAKH -> "kk-KZ"
        KeyboardLanguage.RUSSIAN -> "ru-RU"
        KeyboardLanguage.ENGLISH -> "en-US"
    }

    /** The layout a tag was made for, to name the language in a message. */
    fun layoutFor(tag: String?): KeyboardLanguage? {
        val language = tag?.let { Locale.forLanguageTag(it).language }?.takeIf { it.isNotEmpty() } ?: return null
        return KeyboardLanguage.fromCode(language)
    }

    /**
     * Whether a recogniser's language entry covers [tag]: "kk-KZ" matches
     * "kk-KZ", "kk_KZ" and plain "kk". Region variants of the same language
     * count - a recogniser offering en-GB still understands an English speaker.
     */
    fun covers(available: String, tag: String): Boolean {
        val wanted = Locale.forLanguageTag(tag).language
        val offered = Locale.forLanguageTag(available.replace('_', '-')).language
        return wanted.isNotEmpty() && wanted.equals(offered, ignoreCase = true)
    }
}
