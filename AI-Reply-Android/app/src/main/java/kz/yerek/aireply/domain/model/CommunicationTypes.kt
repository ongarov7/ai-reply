package kz.yerek.aireply.domain.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * How a reply should sound. Kept deliberately short: five presets a user can
 * hold in their head beat twenty sliders nobody touches.
 *
 * The `@SerialName` values are the iOS raw values. They are on the wire to the
 * backend and in the on-disk configuration, so they are part of the contract,
 * not an implementation detail of this enum's spelling.
 */
@Serializable
enum class ReplyTone(val raw: String) {
    @SerialName("natural") NATURAL("natural"),
    @SerialName("friendly") FRIENDLY("friendly"),
    @SerialName("professional") PROFESSIONAL("professional"),
    @SerialName("formal") FORMAL("formal"),
    @SerialName("short") SHORT("short")
}

/**
 * How long a reply should be. Two values, because the real constraint is
 * "messenger short", and the model already knows what that means.
 */
@Serializable
enum class ReplyLength(val raw: String) {
    @SerialName("short") SHORT("short"),
    @SerialName("medium") MEDIUM("medium")
}

/**
 * How much emoji a reply may use. A Friend template that answers with no emoji
 * at all reads as cold; a Client template that answers with three reads as
 * unserious. One setting, three values, is enough to separate them.
 */
@Serializable
enum class EmojiPolicy(val raw: String) {
    @SerialName("allowed") ALLOWED("allowed"),
    @SerialName("minimal") MINIMAL("minimal"),
    @SerialName("none") NONE("none")
}

/** What a template should do with the user's working hours. */
@Serializable
enum class WorkingHoursBehaviour(val raw: String) {
    /** Never bring hours up. Right for the Friend template. */
    @SerialName("ignore") IGNORE("ignore"),

    /**
     * Mention them only when the incoming message asks for something
     * time-sensitive that falls outside them. The sane default.
     */
    @SerialName("mention_when_relevant") MENTION_WHEN_RELEVANT("mention_when_relevant"),

    /** Acknowledge them whenever they are relevant at all. */
    @SerialName("always_mention") ALWAYS_MENTION("always_mention")
}

/**
 * The relationship a template describes. Drives the built-in behaviour the
 * product specifies for each one.
 *
 * The raw values are older than the names users see: `work` is shown as
 * Colleague (Коллега) and `business` as Partner (Партнёр). The server already
 * reads them that way, and the values are in every saved configuration and on
 * the wire, so only the labels moved - never the values.
 *
 * Сақталған мәндер өзгермейді: work - әріптес, business - серіктес.
 */
@Serializable
enum class RelationshipKind(val raw: String) {
    @SerialName("friend") FRIEND("friend"),
    @SerialName("client") CLIENT("client"),
    @SerialName("business") BUSINESS("business"),
    @SerialName("work") WORK("work"),
    @SerialName("custom") CUSTOM("custom");

    /** Sensible starting point for a template of this kind. */
    val defaultTone: ReplyTone
        get() = when (this) {
            FRIEND -> ReplyTone.FRIENDLY
            CLIENT, BUSINESS -> ReplyTone.PROFESSIONAL
            WORK, CUSTOM -> ReplyTone.NATURAL
        }

    val defaultEmojiPolicy: EmojiPolicy
        get() = when (this) {
            FRIEND -> EmojiPolicy.ALLOWED
            CLIENT, WORK, CUSTOM -> EmojiPolicy.MINIMAL
            BUSINESS -> EmojiPolicy.NONE
        }

    /**
     * Whether this kind of conversation has business facts worth configuring.
     * A friend does not: asking someone what they sell before they can set up a
     * casual template would be a worse product, not a more capable one.
     */
    val usesBusinessContext: Boolean
        get() = this != FRIEND

    val defaultWorkingHoursBehaviour: WorkingHoursBehaviour
        get() = if (this == FRIEND) {
            WorkingHoursBehaviour.IGNORE
        } else {
            WorkingHoursBehaviour.MENTION_WHEN_RELEVANT
        }

    companion object {
        /**
         * The four the app ships with, in bar order for a new install: Friend,
         * Client, Colleague, Partner. `CUSTOM` is not one of them. A saved
         * configuration keeps its own order - this only seeds a fresh one.
         */
        val builtIns: List<RelationshipKind> = listOf(FRIEND, CLIENT, WORK, BUSINESS)

        fun fromRaw(raw: String?): RelationshipKind? = entries.firstOrNull { it.raw == raw }
    }
}
