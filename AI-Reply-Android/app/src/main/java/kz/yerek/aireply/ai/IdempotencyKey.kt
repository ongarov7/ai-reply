package kz.yerek.aireply.ai

import java.util.UUID

/**
 * One key per user action - Reply, Write, Regenerate, Try again - so a
 * request the server already charged for is never charged twice (contract
 * §3.3).
 *
 * Әр әрекетке бір кілт: қайталанған сұраныс екі рет есептелмейді.
 *
 * The services make a fresh key for every call, which is every tap. The key
 * travels in the `Idempotency-Key` header, never in the body, and only to a
 * server that announced [AIFeatures.idempotentGeneration]. The one automatic
 * retry there is - a 401 that refreshes the token and sends again - sends the
 * same key, because the header is built once, outside that retry.
 */
object IdempotencyKey {

    const val HEADER = "Idempotency-Key"

    /** What the server accepts; a UUID always matches. */
    private val FORMAT = Regex("^[A-Za-z0-9-]{8,64}$")

    fun create(): String = UUID.randomUUID().toString()

    fun isValid(key: String): Boolean = FORMAT.matches(key)

    /** The header for [key], or none: no key, a server without the feature, or a key it would refuse. */
    fun headers(key: String?, features: AIFeatures): Map<String, String> =
        if (key != null && features.idempotentGeneration && isValid(key)) mapOf(HEADER to key) else emptyMap()
}
