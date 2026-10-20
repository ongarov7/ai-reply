package kz.yerek.aireply.core.lang

import android.content.Context
import kz.yerek.aireply.R
import kz.yerek.aireply.domain.model.RelationshipKind
import kz.yerek.aireply.domain.model.ReplyTemplate

/**
 * Turns a template into the name a user sees.
 *
 * Custom names are shown verbatim — a user who typed "Supplier" gets "Supplier"
 * in every UI language, because that is their word, not ours to translate.
 * Built-ins resolve through ordinary string resources, which is why this needs
 * a [Context]: pass one from [LocalizedContext] to name a template in a
 * specific language rather than the device's.
 */
object TemplateNaming {

    fun displayName(context: Context, template: ReplyTemplate): String {
        template.customName?.takeIf { it.isNotBlank() }?.let { return it }
        return context.getString(resource(template.relationship))
    }

    /**
     * The name as the answer to «Кому отвечаем?» - «Другу», «Коллеге»;
     * «Досқа», «Әріптеске» - where the language declines it. A custom name
     * is the user's own word and is never declined.
     *
     * «Кімге жауап береміз?» сұрағына жауап: барыс септігіндегі атау.
     */
    fun recipientName(context: Context, template: ReplyTemplate): String {
        template.customName?.takeIf { it.isNotBlank() }?.let { return it }
        val dative = dativeResource(template.relationship) ?: resource(template.relationship)
        return context.getString(dative)
    }

    fun resource(kind: RelationshipKind): Int = when (kind) {
        RelationshipKind.FRIEND -> R.string.relationship_friend
        RelationshipKind.CLIENT -> R.string.relationship_client
        RelationshipKind.BUSINESS -> R.string.relationship_business
        RelationshipKind.WORK -> R.string.relationship_work
        RelationshipKind.CUSTOM -> R.string.relationship_custom
    }

    /** The built-ins' «to whom» form; null for [RelationshipKind.CUSTOM], which has no fixed name. */
    fun dativeResource(kind: RelationshipKind): Int? = when (kind) {
        RelationshipKind.FRIEND -> R.string.relationship_friend_dative
        RelationshipKind.CLIENT -> R.string.relationship_client_dative
        RelationshipKind.BUSINESS -> R.string.relationship_business_dative
        RelationshipKind.WORK -> R.string.relationship_work_dative
        RelationshipKind.CUSTOM -> null
    }
}
