package kz.yerek.aireply.domain.model

import kotlinx.serialization.Serializable
import kz.yerek.aireply.core.text.clampToCodePoints

/**
 * The lightweight personal communication profile built during onboarding.
 *
 * It never leaves the device except as part of an AI request the user
 * explicitly triggered, and even then only the fields the model actually needs
 * are sent.
 *
 * Every field carries a default, which is how a profile written by an older
 * build still loads after an update instead of resetting what the user typed.
 */
@Serializable
data class UserProfile(
    /**
     * Free text: who the user is and how they usually communicate. Typed or
     * dictated. This is the "About me" answer.
     */
    val descriptionText: String = "",

    /**
     * What the user does for a living, in their words: "online clothing store
     * owner", "интернет-маркетолог", "дизайнер".
     */
    val role: String = "",

    /**
     * What the user provides, and the rules that hold across every
     * conversation. Per-template context is layered on top of this, never
     * instead of it.
     */
    val business: BusinessContext = BusinessContext.EMPTY,

    /** Default register, used when a template does not override it. */
    val preferredTone: ReplyTone = ReplyTone.NATURAL,

    /**
     * Which conversation kinds the user said they care about. Used to preselect
     * which templates appear in the keyboard bar, never sent to the model.
     */
    val activeRelationships: Set<RelationshipKind> = RelationshipKind.builtIns.toSet(),

    val workingHours: WorkingHours = WorkingHours.DEFAULT,

    /**
     * The first-run flag of builds before onboarding versions. No longer
     * written: the completed version is device-local now
     * ([kz.yerek.aireply.data.settings.DeviceStateStore]) and this is read only
     * to migrate a profile that already finished the old onboarding.
     */
    val hasCompletedOnboarding: Boolean = false,

    /**
     * How Russian replies speak about the user. `null` until the user was
     * asked; an unknown value from a newer build decodes to `null` too.
     */
    val grammaticalGender: GrammaticalGender? = null
) {

    fun withRole(value: String) = copy(role = value.clampToCodePoints(MAX_ROLE))

    fun withDescription(value: String) =
        copy(descriptionText = value.clampToCodePoints(MAX_DESCRIPTION))

    /** What actually goes in an AI request. Trimmed, never the whole object. */
    val promptDescription: String get() = descriptionText.trim()

    /**
     * True once the user has told us anything at all about themselves. Used to
     * decide whether the home screen should still be nudging them to.
     */
    val hasAnyContext: Boolean
        get() = promptDescription.isNotEmpty() || role.isNotBlank() || !business.isEmpty

    companion object {
        /**
         * Maximum stored length for the free-text answer. The product asks for
         * roughly 500-1000 characters; 1000 is the cap and the editor shows a
         * live counter well before it.
         */
        const val MAX_DESCRIPTION = 1000

        /**
         * Short single-line answer. A role is "online clothing store owner",
         * not an essay, and a cap that small keeps the per-request prompt cheap.
         */
        const val MAX_ROLE = 120

        val EMPTY = UserProfile()
    }
}
