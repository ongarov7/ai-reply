package kz.yerek.aireply.ui.feature.profile

import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kz.yerek.aireply.R
import kz.yerek.aireply.analytics.ProductEvent
import kz.yerek.aireply.analytics.ProductEventValue
import kz.yerek.aireply.analytics.ProductEvents
import kz.yerek.aireply.domain.model.GrammaticalGender
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.design.AppCard
import kz.yerek.aireply.ui.design.AppSection

/**
 * "Replies in Russian speak as": male, female or not specified.
 *
 * Saved the moment it changes — locally first, then to the account, so the
 * user's other devices follow — rather than when the screen closes like the
 * text fields around it. A profile that was never asked shows "not specified",
 * which is also what the server assumes.
 */
@Composable
fun GenderSection() {
    val services = LocalServices.current
    val configuration by services.configuration.configuration.collectAsStateWithLifecycle()
    val current = configuration.profile.grammaticalGender ?: GrammaticalGender.UNSPECIFIED

    AppSection(stringResource(R.string.profile_gender)) {
        AppCard {
            Column(modifier = Modifier.selectableGroup()) {
                GrammaticalGender.entries.forEach { option ->
                    SelectableRow(label = stringResource(genderLabel(option)), selected = current == option) {
                        if (option == configuration.profile.grammaticalGender) return@SelectableRow
                        services.profileSync.setGender(option)
                        ProductEvents.track(
                            ProductEvent.GENDER_SELECTED,
                            mapOf(
                                "source" to ProductEventValue.Code("settings"),
                                "skipped" to ProductEventValue.Flag(!option.isSpecified)
                            )
                        )
                    }
                }
            }
        }
        Footnote(stringResource(R.string.profile_gender_footer))
    }
}

@StringRes
fun genderLabel(gender: GrammaticalGender): Int = when (gender) {
    GrammaticalGender.MALE -> R.string.profile_gender_male
    GrammaticalGender.FEMALE -> R.string.profile_gender_female
    GrammaticalGender.UNSPECIFIED -> R.string.profile_gender_unspecified
}
