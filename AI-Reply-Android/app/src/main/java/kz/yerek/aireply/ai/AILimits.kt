package kz.yerek.aireply.ai

import android.content.SharedPreferences
import kz.yerek.aireply.data.account.ServerConfigDto

/**
 * The character limits the SERVER publishes (`GET /api/v1/config`), which an
 * administrator changes in the admin panel without a release.
 *
 * CHARACTERS, NOT TOKENS, NOT QUOTA. These are the lengths of the incoming
 * message and of the user's instruction, counted in code points exactly as
 * the server counts them. How many replies a plan allows is a separate thing
 * (the quota), and so is how long the model may answer (tokens).
 *
 * The server stays the source of truth: it enforces its own limit on every
 * request, and when a request is refused for length the answer carries the
 * limit, which is stored here too. [FALLBACK] only covers a device that has
 * never reached the server.
 *
 * The same answer carries the request-shaping flags ([AIFeatures]); they are
 * stored here as well, because the keyboard needs them on the same file and at
 * the same moment.
 */
data class AILimits(
    val sourceCharacters: Int,
    val instructionCharacters: Int
) {
    companion object {
        val FALLBACK = AILimits(sourceCharacters = 400, instructionCharacters = 400)

        /** Values outside these are treated as a misconfiguration and ignored. */
        val SOURCE_RANGE = 50..2000
        val INSTRUCTION_RANGE = 50..1000

        /** Refresh at most this often from the keyboard. */
        const val REFRESH_INTERVAL_MS = 6L * 60 * 60 * 1000

        /** Limits from what the server sent, keeping [previous] for anything unusable. */
        fun published(source: Int?, instruction: Int?, previous: AILimits = FALLBACK): AILimits =
            AILimits(
                sourceCharacters = source?.takeIf { it in SOURCE_RANGE } ?: previous.sourceCharacters,
                instructionCharacters = instruction?.takeIf { it in INSTRUCTION_RANGE }
                    ?: previous.instructionCharacters
            )

        // ------------------------------------------------------------ store

        @Volatile
        private var memory: AILimits? = null

        @Volatile
        private var featuresMemory: AIFeatures? = null

        @Volatile
        private var prefs: SharedPreferences? = null

        /** Called once by the Application, before anything reads [current]. */
        fun install(preferences: SharedPreferences) {
            prefs = preferences
            memory = null
            featuresMemory = null
        }

        /** What the keyboard and the app enforce right now. */
        val current: AILimits
            get() = memory ?: load().also { memory = it }

        /** When the server last told us, in wall-clock millis; 0 for never. */
        val lastSyncedAt: Long
            get() = prefs?.getLong(KEY_SYNCED_AT, 0L) ?: 0L

        fun isStale(now: Long = System.currentTimeMillis()): Boolean =
            now - lastSyncedAt > REFRESH_INTERVAL_MS

        /** Which optional request fields the server accepts. [AIFeatures.NONE] until it said. */
        val features: AIFeatures
            get() = featuresMemory ?: loadFeatures().also { featuresMemory = it }

        fun apply(config: ServerConfigDto, now: Long = System.currentTimeMillis()) {
            store(published(config.maxSourceCharacters, config.maxInstructionLength, current), now)
            // A server that sent no features block supports none of them.
            val announced = config.features
            storeFeatures(
                AIFeatures(
                    replyPreferences = announced?.replyPreferences ?: false,
                    senderProfile = announced?.senderProfile ?: false,
                    instructionPolish = announced?.instructionPolish ?: false,
                    productEvents = announced?.productEvents ?: false,
                    preferredLanguage = announced?.preferredLanguage ?: false
                )
            )
        }

        /** The server refused a message and said what its limit is. */
        fun storeSourceLimit(limit: Int) {
            if (limit !in SOURCE_RANGE) return
            store(current.copy(sourceCharacters = limit), lastSyncedAt)
        }

        fun store(limits: AILimits, syncedAt: Long = System.currentTimeMillis()) {
            memory = limits
            prefs?.edit()
                ?.putInt(KEY_SOURCE, limits.sourceCharacters)
                ?.putInt(KEY_INSTRUCTION, limits.instructionCharacters)
                ?.putLong(KEY_SYNCED_AT, syncedAt)
                ?.apply()
        }

        private fun storeFeatures(features: AIFeatures) {
            featuresMemory = features
            prefs?.edit()
                ?.putBoolean(KEY_REPLY_PREFERENCES, features.replyPreferences)
                ?.putBoolean(KEY_SENDER_PROFILE, features.senderProfile)
                ?.putBoolean(KEY_INSTRUCTION_POLISH, features.instructionPolish)
                ?.putBoolean(KEY_PRODUCT_EVENTS, features.productEvents)
                ?.putBoolean(KEY_PREFERRED_LANGUAGE, features.preferredLanguage)
                ?.apply()
        }

        private fun loadFeatures(): AIFeatures {
            val preferences = prefs ?: return AIFeatures.NONE
            return AIFeatures(
                replyPreferences = preferences.getBoolean(KEY_REPLY_PREFERENCES, false),
                senderProfile = preferences.getBoolean(KEY_SENDER_PROFILE, false),
                instructionPolish = preferences.getBoolean(KEY_INSTRUCTION_POLISH, false),
                productEvents = preferences.getBoolean(KEY_PRODUCT_EVENTS, false),
                preferredLanguage = preferences.getBoolean(KEY_PREFERRED_LANGUAGE, false)
            )
        }

        private fun load(): AILimits {
            val preferences = prefs ?: return FALLBACK
            val source = preferences.getInt(KEY_SOURCE, -1).takeIf { it > 0 }
            val instruction = preferences.getInt(KEY_INSTRUCTION, -1).takeIf { it > 0 }
            return published(source, instruction, FALLBACK)
        }

        private const val KEY_SOURCE = "ai.limits.sourceCharacters"
        private const val KEY_INSTRUCTION = "ai.limits.instructionCharacters"
        private const val KEY_SYNCED_AT = "ai.limits.syncedAt"
        private const val KEY_REPLY_PREFERENCES = "ai.features.replyPreferences"
        private const val KEY_SENDER_PROFILE = "ai.features.senderProfile"
        private const val KEY_INSTRUCTION_POLISH = "ai.features.instructionPolish"
        private const val KEY_PRODUCT_EVENTS = "ai.features.productEvents"
        private const val KEY_PREFERRED_LANGUAGE = "ai.features.preferredLanguage"
    }
}
