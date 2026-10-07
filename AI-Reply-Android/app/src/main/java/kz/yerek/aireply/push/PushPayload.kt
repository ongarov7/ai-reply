package kz.yerek.aireply.push

/**
 * The data keys of one of our pushes, as the server sends them: `nid`, `did`,
 * `type`, `category`, `link`.
 *
 * Push деректері: nid, did, type, category, link.
 *
 * The same map arrives two ways: as `RemoteMessage.data` when the app is in the
 * foreground, and as String extras of the launch intent when the system showed
 * the notification and the user tapped it. Parsing it is pure Kotlin so both
 * paths, and the tests, share one reading.
 */
data class PushPayload(
    val notificationId: String?,
    val deliveryId: String?,
    val type: String?,
    val category: String?,
    val link: AppLink
) {

    /** The channel the server named for this category; the app uses the same rule. */
    val channelId: String get() = channelFor(category)

    val isImportant: Boolean get() = channelId == CHANNEL_IMPORTANT

    /**
     * The body that records this tap, or null when there is no delivery id the
     * server could have sent (then nothing is sent at all).
     */
    fun openedRequest(installationId: String): NotificationOpenedRequest? {
        if (notificationId.isNullOrEmpty()) return null
        val delivery = deliveryId?.takeIf(ID_PATTERN::matches) ?: return null
        return NotificationOpenedRequest(installationId = installationId, deliveryId = delivery)
    }

    companion object {
        const val KEY_NOTIFICATION_ID = "nid"
        const val KEY_DELIVERY_ID = "did"
        const val KEY_TYPE = "type"
        const val KEY_CATEGORY = "category"
        const val KEY_LINK = "link"

        /** The keys the app reads; everything else in the payload is ignored. */
        val KEYS = listOf(KEY_NOTIFICATION_ID, KEY_DELIVERY_ID, KEY_TYPE, KEY_CATEGORY, KEY_LINK)

        const val CHANNEL_GENERAL = "general"
        const val CHANNEL_IMPORTANT = "important"

        private val IMPORTANT_CATEGORIES = setOf("account", "subscription", "security")

        /** The server's ids are UUIDs; anything else is not worth a request. */
        private val ID_PATTERN = Regex("^[A-Za-z0-9-]{1,64}$")

        fun channelFor(category: String?): String =
            if (category in IMPORTANT_CATEGORIES) CHANNEL_IMPORTANT else CHANNEL_GENERAL

        /** Null unless [data] is one of our notifications (it carries a notification id). */
        fun from(data: Map<String, String?>): PushPayload? {
            val nid = data[KEY_NOTIFICATION_ID]?.trim()
            if (nid.isNullOrEmpty()) return null
            return PushPayload(
                notificationId = nid,
                deliveryId = data[KEY_DELIVERY_ID]?.trim()?.takeIf(String::isNotEmpty),
                type = data[KEY_TYPE]?.trim()?.takeIf(String::isNotEmpty),
                category = data[KEY_CATEGORY]?.trim()?.takeIf(String::isNotEmpty),
                link = AppLinks.parse(data[KEY_LINK])
            )
        }
    }
}
