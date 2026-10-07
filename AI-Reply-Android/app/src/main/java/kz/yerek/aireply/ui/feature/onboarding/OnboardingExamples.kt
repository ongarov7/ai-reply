package kz.yerek.aireply.ui.feature.onboarding

import androidx.annotation.StringRes
import kz.yerek.aireply.R
import kz.yerek.aireply.domain.model.GrammaticalGender

/**
 * The example texts of the tutorial and the practice run, as the user's own
 * replies would read.
 *
 * Only Russian has gendered forms («занят/занята», «рад/рада»), so only the
 * Russian resources differ; the other languages point all three at one text.
 * Without a choice the neutral wording is used, exactly as the server does.
 */
internal object OnboardingExamples {

    @StringRes
    fun tutorialInstruction(gender: GrammaticalGender?): Int = when (gender) {
        GrammaticalGender.MALE -> R.string.onboarding_tutorial_instruction_male
        GrammaticalGender.FEMALE -> R.string.onboarding_tutorial_instruction_female
        GrammaticalGender.UNSPECIFIED, null -> R.string.onboarding_tutorial_instruction
    }

    @StringRes
    fun tutorialReply(gender: GrammaticalGender?): Int = when (gender) {
        GrammaticalGender.MALE -> R.string.onboarding_tutorial_reply_male
        GrammaticalGender.FEMALE -> R.string.onboarding_tutorial_reply_female
        GrammaticalGender.UNSPECIFIED, null -> R.string.onboarding_tutorial_reply
    }

    @StringRes
    fun practiceReply(gender: GrammaticalGender?): Int = when (gender) {
        GrammaticalGender.MALE -> R.string.onboarding_practice_reply_male
        GrammaticalGender.FEMALE -> R.string.onboarding_practice_reply_female
        GrammaticalGender.UNSPECIFIED, null -> R.string.onboarding_practice_reply
    }
}
