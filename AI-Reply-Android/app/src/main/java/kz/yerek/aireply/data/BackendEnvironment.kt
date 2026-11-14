package kz.yerek.aireply.data

import kz.yerek.aireply.BuildConfig

/** Build-time backend selection. A sandbox never reads or migrates production storage. */
enum class BackendEnvironment(val wireName: String, val baseUrl: String, private val storagePrefix: String) {
    PRODUCTION("production", "https://ai-reply.kz", ""),
    SANDBOX("sandbox", "https://ai-reply.kz/sandbox", "sandbox_");

    /** Production names stay compatible with every released app. */
    fun storageName(productionName: String): String = storagePrefix + productionName

    companion object {
        fun fromName(name: String? = null): BackendEnvironment = when (name) {
            null, "production" -> PRODUCTION
            "sandbox" -> SANDBOX
            else -> throw IllegalArgumentException("Unknown backend environment")
        }

        val current: BackendEnvironment = fromName(BuildConfig.BACKEND_ENVIRONMENT).also {
            check(it.baseUrl == BuildConfig.BACKEND_BASE_URL) { "Backend environment and URL disagree" }
        }
    }
}
