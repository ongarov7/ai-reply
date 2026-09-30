package kz.yerek.aireply.push

import kz.yerek.aireply.telemetry.EventSchema

/**
 * The data keys of one of our pushes, as the server sends them
 * (`internal/notifications/dispatcher.go`, `payloadData`).
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
     * `notification_opened` properties, or null when the ids are not ones the
     * server would accept (then no event is sent at all).
     */
    fun openedEventProperties(): Map<String, Any>? {
        val nid = notificationId?.takeIf(EventSchema::isCode) ?: return null
        val properties = LinkedHashMap<String, Any>()
        properties["notification_id"] = nid
        deliveryId?.takeIf(EventSchema::isCode)?.let { properties["delivery_id"] = it }
        type?.takeIf(EventSchema::isCode)?.let { properties["type"] = it }
        return properties
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
