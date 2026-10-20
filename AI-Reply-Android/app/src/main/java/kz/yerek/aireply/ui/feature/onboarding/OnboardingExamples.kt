package kz.yerek.aireply.ui.feature.onboarding

import androidx.annotation.StringRes
import kz.yerek.aireply.R
import kz.yerek.aireply.domain.model.GrammaticalGender
import kz.yerek.aireply.domain.model.RelationshipKind

/**
 * The example texts of the onboarding, as the user's own replies would read.
 * All of them are resources: nothing is generated, sent or counted.
 *
 * Онбординг мысалдары: жергілікті мәтіндер, сервер мен квотасыз.
 *
 * Only Russian has gendered forms («рад/рада»), so only the Russian welcome
 * reply differs by gender; without a choice the neutral wording is used,
 * exactly as the server does. The tutorial's replies need no gender at all.
 */
internal object OnboardingExamples {

    @StringRes
    fun welcomeReply(gender: GrammaticalGender?): Int = when (gender) {
        GrammaticalGender.MALE -> R.string.onboarding_welcome_reply_male
        GrammaticalGender.FEMALE -> R.string.onboarding_welcome_reply_female
        GrammaticalGender.UNSPECIFIED, null -> R.string.onboarding_welcome_reply
    }

    /** The answer to «Добрый день! Можно узнать подробнее?» for each recipient: the tone moves with it. */
    @StringRes
    fun demoReply(kind: RelationshipKind): Int = when (kind) {
        RelationshipKind.FRIEND -> R.string.onboarding_demo_reply_friend
        RelationshipKind.CLIENT, RelationshipKind.CUSTOM -> R.string.onboarding_demo_reply_client
        RelationshipKind.WORK -> R.string.onboarding_demo_reply_colleague
        RelationshipKind.BUSINESS -> R.string.onboarding_demo_reply_partner
    }

    /** The tutorial's recipient before one is chosen: the demo message is a client's question. */
    val defaultRecipient: RelationshipKind = RelationshipKind.CLIENT
}
