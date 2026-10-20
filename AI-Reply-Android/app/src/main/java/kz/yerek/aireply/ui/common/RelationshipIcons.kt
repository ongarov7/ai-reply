package kz.yerek.aireply.ui.common

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.BusinessCenter
import androidx.compose.material.icons.outlined.Group
import androidx.compose.material.icons.outlined.Handshake
import androidx.compose.material.icons.outlined.Person
import androidx.compose.ui.graphics.vector.ImageVector
import kz.yerek.aireply.domain.model.RelationshipKind

/**
 * The picture next to a recipient's name, the same on every screen and in the
 * keyboard: Friend - two people, Client - one person, Colleague - a briefcase,
 * Partner - a handshake. Outlined Material symbols, so they sit quietly next
 * to the text instead of competing with it.
 *
 * A custom template has none: its name is the user's own word, and an icon
 * picked for it would be a guess.
 *
 * Әр алушының белгішесі: дос, клиент, әріптес, серіктес. Өз үлгіңізде белгіше жоқ.
 */
object RelationshipIcons {

    fun of(kind: RelationshipKind): ImageVector? = when (kind) {
        RelationshipKind.FRIEND -> Icons.Outlined.Group
        RelationshipKind.CLIENT -> Icons.Outlined.Person
        RelationshipKind.WORK -> Icons.Outlined.BusinessCenter
        RelationshipKind.BUSINESS -> Icons.Outlined.Handshake
        RelationshipKind.CUSTOM -> null
    }

    /**
     * For a template id, as the keyboard's chip cache stores it: a built-in's
     * id is its relationship's raw value; a custom template's is a UUID and
     * gets no icon.
     */
    fun forTemplateId(id: String): ImageVector? =
        RelationshipKind.fromRaw(id)?.takeIf { it != RelationshipKind.CUSTOM }?.let(::of)
}
