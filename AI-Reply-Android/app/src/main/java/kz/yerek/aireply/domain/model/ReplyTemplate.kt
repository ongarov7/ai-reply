package kz.yerek.aireply.domain.model

import kotlinx.serialization.KSerializer
import kotlinx.serialization.Serializable
import kotlinx.serialization.descriptors.SerialDescriptor
import kotlinx.serialization.encoding.Decoder
import kotlinx.serialization.encoding.Encoder
import kz.yerek.aireply.core.text.clampToCodePoints
import java.util.UUID

/**
 * A communication template: the CONTEXT a reply is written in, never a canned
 * response. Nothing in here is a sentence the user will send; it is all guidance
 * the model applies to the message actually received.
 *
 * The display name is deliberately NOT computed here. On iOS it had to be,
 * because the keyboard extension could not reach the app's localized strings.
 * On Android the built-in names are ordinary resources, so naming lives with
 * the code that has a Context and this type stays a pure value.
 */
@Serializable(with = ReplyTemplateSerializer::class)
data class ReplyTemplate(
    /**
     * Stable identifier. The four built-ins use their relationship name so the
     * backend and the prompt builder can recognise them; custom templates get a
     * UUID string.
     */
    val id: String,

    /**
     * True for the four templates the app ships. Built-ins can be edited and
     * hidden but never deleted, so a user cannot end up with an empty bar and
     * no way back.
     */
    val isBuiltIn: Boolean,

    val relationship: RelationshipKind,

    /**
     * null for a built-in that has not been renamed, which then shows its
     * localized name. A custom template always has one.
     */
    val customName: String?,

    val tone: ReplyTone,

    /**
     * Anything else the user wants this template to know, in their own words.
     * Dictation lands here verbatim.
     */
    val instructions: String,

    val workingHoursBehaviour: WorkingHoursBehaviour,
    val replyLength: ReplyLength,
    val emojiPolicy: EmojiPolicy,

    /**
     * Business facts that apply only to this kind of conversation. null for a
     * template that has none — a Friend template should not carry an empty shop
     * description into every prompt.
     */
    val business: BusinessContext?,

    /**
     * Hours for this template only, when they differ from the profile's. null
     * means "use the profile's working hours", which is what almost everyone
     * wants and what an empty editor leaves behind.
     */
    val workingHoursOverride: WorkingHours?,

    /**
     * Hidden templates stay configured but leave the keyboard bar. This is how
     * a built-in is "removed" without actually being destroyed.
     */
    val isVisible: Boolean,

    /** Position in the keyboard bar and in the settings list. */
    val sortIndex: Int
) {

    fun withInstructions(value: String) =
        copy(instructions = value.clampToCodePoints(MAX_INSTRUCTIONS))

    /**
     * Clearing a built-in's name restores its localized one. Clearing a custom
     * template's name is refused, because it has no fallback.
     */
    fun withName(value: String): ReplyTemplate {
        val trimmed = value.trim()
        if (trimmed.isEmpty()) {
            return if (isBuiltIn) copy(customName = null) else this
        }
        return copy(customName = trimmed.clampToCodePoints(MAX_NAME))
    }

    /**
     * The business facts actually worth sending for this template: the
     * template's own, or nothing. Callers layer the profile's underneath.
     */
    val effectiveBusiness: BusinessContext?
        get() = business?.takeUnless { it.isEmpty }

    /**
     * The hours that govern this template. The override wins when it exists and
     * is switched on; otherwise the profile's own schedule applies.
     */
    fun effectiveWorkingHours(profile: WorkingHours): WorkingHours =
        workingHoursOverride?.takeIf { it.isEnabled } ?: profile

    /**
     * Turning this template's own hours on starts from [profile], so the user
     * edits the schedule they already have. Off drops the override rather than
     * disabling it, so "off" means exactly one thing: follow the profile.
     */
    fun withHoursOverride(isOn: Boolean, profile: WorkingHours): ReplyTemplate = copy(
        workingHoursOverride = if (isOn) (workingHoursOverride ?: profile).copy(isEnabled = true) else null
    )

    /**
     * Ensures the business container exists before an editor writes into it, so
     * a custom template created before this field existed can still be given one.
     */
    fun ensuringBusinessContext(): ReplyTemplate =
        if (business == null) copy(business = BusinessContext.EMPTY) else this

    companion object {
        const val MAX_INSTRUCTIONS = 600
        const val MAX_NAME = 60

        fun builtIn(relationship: RelationshipKind, sortIndex: Int) = ReplyTemplate(
            id = relationship.raw,
            isBuiltIn = true,
            relationship = relationship,
            customName = null,
            tone = relationship.defaultTone,
            // Empty on purpose. The behaviour for the four built-in kinds lives
            // in the prompt builder, which keeps it out of every user's saved
            // data and lets it be improved without a migration. This field is
            // for what THIS user wants to add on top.
            instructions = "",
            workingHoursBehaviour = relationship.defaultWorkingHoursBehaviour,
            replyLength = ReplyLength.SHORT,
            emojiPolicy = relationship.defaultEmojiPolicy,
            business = if (relationship.usesBusinessContext) BusinessContext.EMPTY else null,
            workingHoursOverride = null,
            isVisible = true,
            sortIndex = sortIndex
        )

        fun custom(name: String, sortIndex: Int) = ReplyTemplate(
            id = UUID.randomUUID().toString(),
            isBuiltIn = false,
            relationship = RelationshipKind.CUSTOM,
            customName = name,
            tone = ReplyTone.NATURAL,
            instructions = "",
            workingHoursBehaviour = WorkingHoursBehaviour.MENTION_WHEN_RELEVANT,
            replyLength = ReplyLength.SHORT,
            emojiPolicy = EmojiPolicy.MINIMAL,
            business = BusinessContext.EMPTY,
            workingHoursOverride = null,
            isVisible = true,
            sortIndex = sortIndex
        )

        val defaults: List<ReplyTemplate>
            get() = RelationshipKind.builtIns.mapIndexed { index, kind -> builtIn(kind, index) }
    }
}

/**
 * Wire form. Every field is optional except the id, so a configuration written
 * by a build that did not have one of them still loads.
 */
@Serializable
private data class ReplyTemplateSurrogate(
    val id: String,
    val isBuiltIn: Boolean = false,
    val relationship: RelationshipKind = RelationshipKind.CUSTOM,
    val customName: String? = null,
    val tone: ReplyTone? = null,
    val instructions: String = "",
    val workingHoursBehaviour: WorkingHoursBehaviour? = null,
    val replyLength: ReplyLength = ReplyLength.SHORT,
    val emojiPolicy: EmojiPolicy? = null,
    val business: BusinessContext? = null,
    val workingHoursOverride: WorkingHours? = null,
    val isVisible: Boolean = true,
    val sortIndex: Int = 0
)

/**
 * Exists because the defaults have to be per-relationship rather than blanket:
 * a Friend decoded from an old file must not acquire a business container it
 * never had, and a Client must not lose one. A plain `@Serializable` data class
 * can only express one constant default per field, which would get both wrong.
 */
object ReplyTemplateSerializer : KSerializer<ReplyTemplate> {

    override val descriptor: SerialDescriptor = ReplyTemplateSurrogate.serializer().descriptor

    override fun deserialize(decoder: Decoder): ReplyTemplate {
        val s = ReplyTemplateSurrogate.serializer().deserialize(decoder)
        val kind = s.relationship
        return ReplyTemplate(
            id = s.id,
            isBuiltIn = s.isBuiltIn,
            relationship = kind,
            customName = s.customName,
            tone = s.tone ?: kind.defaultTone,
            instructions = s.instructions,
            workingHoursBehaviour = s.workingHoursBehaviour ?: kind.defaultWorkingHoursBehaviour,
            replyLength = s.replyLength,
            emojiPolicy = s.emojiPolicy ?: kind.defaultEmojiPolicy,
            business = s.business
                ?: if (kind.usesBusinessContext) BusinessContext.EMPTY else null,
            workingHoursOverride = s.workingHoursOverride,
            isVisible = s.isVisible,
            sortIndex = s.sortIndex
        )
    }

    override fun serialize(encoder: Encoder, value: ReplyTemplate) {
        ReplyTemplateSurrogate.serializer().serialize(
            encoder,
            ReplyTemplateSurrogate(
                id = value.id,
                isBuiltIn = value.isBuiltIn,
                relationship = value.relationship,
                customName = value.customName,
                tone = value.tone,
                instructions = value.instructions,
                workingHoursBehaviour = value.workingHoursBehaviour,
                replyLength = value.replyLength,
                emojiPolicy = value.emojiPolicy,
                business = value.business,
                workingHoursOverride = value.workingHoursOverride,
                isVisible = value.isVisible,
                sortIndex = value.sortIndex
            )
        )
    }
}
