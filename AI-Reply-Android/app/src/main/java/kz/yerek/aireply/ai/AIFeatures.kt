package kz.yerek.aireply.ai

/**
 * The request-shaping flags the server announced in `GET /api/v1/config`.
 *
 * Сервер қабылдайтын қосымша өрістер: белгі жоқ болса, өріс жіберілмейді.
 *
 * The server refuses a body with a field it does not know, so a new field is
 * sent only once the flag for it was seen. Every flag is false until then,
 * which keeps a new build working against an older server.
 */
data class AIFeatures(
    /** The `profile` block on a reply: description, role, preferred tone, business. */
    val replyPreferences: Boolean,
    /** `grammatical_gender` and `input_language` on replies and compose, and on `/me`. */
    val senderProfile: Boolean,
    /** `POST /api/v1/ai/polish`. */
    val instructionPolish: Boolean,
    /** `POST /api/v1/analytics/events`: the app's product events. */
    val productEvents: Boolean = false,
    /** `preferred_language` on `/me`: the language of the account's notifications. */
    val preferredLanguage: Boolean = false
) {
    companion object {
        val NONE = AIFeatures(replyPreferences = false, senderProfile = false, instructionPolish = false)
    }
}
