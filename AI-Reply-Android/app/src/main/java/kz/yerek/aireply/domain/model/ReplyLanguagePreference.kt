package kz.yerek.aireply.domain.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kz.yerek.aireply.core.lang.AppLanguage

/**
 * A fixed reply language, chosen in the profile. Structured on purpose: the
 * server accepts only these codes and ignores anything else, so a preference
 * can never smuggle free text into the prompt.
 *
 * A profile holds `null` until the user picks one, and `null` means the
 * language of the copied message. Nothing sets this on the user's behalf: not
 * the phone's language, not the app's, not the keyboard layout.
 *
 * The raw values are the wire values (`reply_language`) and the iOS ones.
 */
@Serializable
enum class ReplyLanguagePreference(val raw: String, private val language: AppLanguage) {
    @SerialName("kk") KAZAKH("kk", AppLanguage.KAZAKH),
    @SerialName("ru") RUSSIAN("ru", AppLanguage.RUSSIAN),
    @SerialName("en") ENGLISH("en", AppLanguage.ENGLISH),
    @SerialName("uz") UZBEK("uz", AppLanguage.UZBEK);

    /** Shown in its own language, like every language picker in the app. */
    val nativeName: String get() = language.nativeName
}
