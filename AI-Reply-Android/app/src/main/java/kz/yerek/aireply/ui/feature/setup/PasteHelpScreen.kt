package kz.yerek.aireply.ui.feature.setup

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.outlined.Lock
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material.icons.outlined.NotificationsNone
import androidx.compose.material.icons.outlined.ToggleOff
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import kz.yerek.aireply.R
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.common.AppScreen
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.common.evidenceTime
import kz.yerek.aireply.ui.design.AppCard
import kz.yerek.aireply.ui.design.AppSection
import kz.yerek.aireply.ui.design.LocalExtraColors
import kz.yerek.aireply.ui.design.ReadableColumn
import kz.yerek.aireply.ui.design.Spacing
import kz.yerek.aireply.ui.design.StepRow

/**
 * «Вставка из других приложений» on Android (aireply://paste_help).
 *
 * Android-та көшірілген мәтін қалай оқылады: тек экрандағы қолданба немесе
 * негізгі пернетақта, құпия мәтін көрсетілмейді, қосатын ауыстырғыш жоқ.
 *
 * There is no switch to send the user to, unlike iOS: Android lets the app
 * on screen and the default keyboard read the clipboard, hides clips another
 * app marked private, and may say so with its own notice. The page explains
 * exactly that, and shows the last paste this phone has seen.
 */
@Composable
fun PasteHelpScreen(onBack: () -> Unit) {
    val services = LocalServices.current
    val lastPaste = remember { services.deviceState.lastSuccessfulPasteAt }

    AppScreen(title = stringResource(R.string.paste_help_title), onBack = onBack) {
        ReadableColumn {
            Footnote(stringResource(R.string.paste_help_intro))

            AppSection(stringResource(R.string.paste_help_steps_title)) {
                AppCard {
                    Column(verticalArrangement = Arrangement.spacedBy(Spacing.s)) {
                        StepRow(1, stringResource(R.string.paste_help_step_copy))
                        StepRow(2, stringResource(R.string.paste_help_step_switch))
                        StepRow(3, stringResource(R.string.paste_help_step_recipient))
                    }
                }
            }

            AppSection(stringResource(R.string.paste_help_rules_title)) {
                AppCard {
                    Column(verticalArrangement = Arrangement.spacedBy(Spacing.m)) {
                        Rule(Icons.Outlined.Info, R.string.paste_help_rule_focus)
                        Rule(Icons.Outlined.Lock, R.string.paste_help_rule_private)
                        Rule(Icons.Outlined.NotificationsNone, R.string.paste_help_rule_notice)
                        Rule(Icons.Outlined.ToggleOff, R.string.paste_help_rule_no_switch)
                    }
                }
            }

            AppCard {
                Row(
                    horizontalArrangement = Arrangement.spacedBy(Spacing.s),
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    if (lastPaste != null) {
                        Icon(
                            Icons.Filled.CheckCircle,
                            contentDescription = null,
                            tint = LocalExtraColors.current.success,
                            modifier = Modifier.size(22.dp)
                        )
                    }
                    Text(
                        if (lastPaste != null) {
                            stringResource(R.string.paste_help_last, evidenceTime(lastPaste))
                        } else {
                            stringResource(R.string.paste_help_never)
                        },
                        style = MaterialTheme.typography.bodyMedium
                    )
                }
            }
        }
    }
}

@Composable
private fun Rule(icon: ImageVector, text: Int) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(Spacing.s),
        verticalAlignment = Alignment.Top
    ) {
        Icon(
            icon,
            contentDescription = null,
            tint = MaterialTheme.colorScheme.primary,
            modifier = Modifier.padding(top = 1.dp).size(20.dp)
        )
        Text(stringResource(text), style = MaterialTheme.typography.bodyMedium)
    }
}
