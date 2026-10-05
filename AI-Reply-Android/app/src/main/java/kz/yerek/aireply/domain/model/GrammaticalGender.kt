package kz.yerek.aireply.domain.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * How Russian replies speak about the user: «рад» or «рада», «сделал» or
 * «сделала». Kazakh and English have no such forms, so nothing changes there.
 *
 * Жіберушінің грамматикалық жынысы: тек орыс тілі үшін.
 *
 * Only ever what the user chose. It is never guessed from a name, an e-mail,
 * a Google account or a conversation. [UNSPECIFIED] is a real answer (the user
 * skipped or declined), which is why a profile that was never asked holds
 * `null` instead.
 *
 * The raw values are the wire values (`grammatical_gender`) and the iOS ones.
 */
@Serializable
enum class GrammaticalGender(val raw: String) {
    @SerialName("male") MALE("male"),
    @SerialName("female") FEMALE("female"),
    @SerialName("unspecified") UNSPECIFIED("unspecified");

    /** A choice that changes the wording; [UNSPECIFIED] asks for neutral phrasing instead. */
    val isSpecified: Boolean get() = this != UNSPECIFIED

    companion object {
        fun fromRaw(raw: String?): GrammaticalGender? = entries.firstOrNull { it.raw == raw }
    }
}
