package kz.yerek.aireply.ui.common

import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import kz.yerek.aireply.R

/**
 * What leaves the phone, and when, named with the keyboard's own button
 * labels so the sentence matches what the user taps. Home and Settings show
 * the same words.
 */
@Composable
fun privacyStatement(): String = stringResource(
    R.string.settings_privacy_body,
    stringResource(R.string.kb_generate),
    stringResource(R.string.kb_compose_write)
)
