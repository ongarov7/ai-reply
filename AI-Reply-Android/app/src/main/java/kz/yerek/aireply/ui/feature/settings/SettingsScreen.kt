package kz.yerek.aireply.ui.feature.settings

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Keyboard
import androidx.compose.material.icons.filled.Schedule
import androidx.compose.material.icons.outlined.ChatBubbleOutline
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.outlined.Lock
import androidx.compose.material.icons.outlined.PersonOutline
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kz.yerek.aireply.BuildConfig
import kz.yerek.aireply.R
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.ui.feature.account.AccountSection
import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.data.settings.AppearancePreference
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.common.AppScreen
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.common.NavigationRow
import kz.yerek.aireply.ui.common.RowDividerIndented
import kz.yerek.aireply.ui.common.RowGroup
import kz.yerek.aireply.ui.design.AppCard
import kz.yerek.aireply.ui.design.AppSection
import kz.yerek.aireply.ui.design.ReadableColumn
import kz.yerek.aireply.ui.design.Spacing
import kz.yerek.aireply.ui.navigation.Routes

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsScreen(onBack: () -> Unit, onOpen: (String) -> Unit) {
    val services = LocalServices.current
    val uriHandler = LocalUriHandler.current
    val accountState by services.account.state.collectAsStateWithLifecycle()

    var appearance by remember { mutableStateOf(services.settings.appearance) }
    var language by remember { mutableStateOf(services.settings.appLanguage) }
    var layouts by remember { mutableStateOf(services.settings.enabledKeyboardLanguages) }
    var haptics by remember { mutableStateOf(services.settings.keyboardHaptics) }
    var mockReplies by remember { mutableStateOf(services.settings.debugMockReplies) }

    AppScreen(title = stringResource(R.string.settings_title), onBack = onBack) {
        ReadableColumn {

            AppSection(stringResource(R.string.home_profile_title)) {
                RowGroup {
                    NavigationRow(
                        Icons.Outlined.PersonOutline,
                        stringResource(R.string.home_profile_edit)
                    ) { onOpen(Routes.Profile) }
                    RowDividerIndented()
                    NavigationRow(
                        Icons.Outlined.ChatBubbleOutline,
                        stringResource(R.string.home_profile_templates)
                    ) { onOpen(Routes.Templates) }
                    RowDividerIndented()
                    NavigationRow(
                        Icons.Filled.Schedule,
                        stringResource(R.string.home_profile_hours)
                    ) { onOpen(Routes.WorkingHours) }
                }
            }

            AppSection(stringResource(R.string.settings_setup)) {
                RowGroup {
                    NavigationRow(
                        Icons.Filled.Keyboard,
                        stringResource(R.string.settings_setup_guide)
                    ) { onOpen(Routes.KeyboardSetup) }
                }
                Footnote(stringResource(R.string.settings_setup_footer))
            }

            AppSection(stringResource(R.string.settings_keyboard)) {
                RowGroup {
                    KeyboardLanguage.CYCLE_ORDER.forEachIndexed { index, option ->
                        if (index > 0) RowDividerIndented()
                        val on = option in layouts
                        // The last layout that is on cannot be switched off.
                        SwitchRow(label = option.nativeName, checked = on, enabled = !(on && layouts.size == 1)) { checked ->
                            val next = KeyboardLanguage.CYCLE_ORDER.filter {
                                if (it == option) checked else it in layouts
                            }
                            if (next.isNotEmpty()) {
                                layouts = next
                                services.settings.enabledKeyboardLanguages = next
                            }
                        }
                    }
                    RowDividerIndented()
                    SwitchRow(
                        label = stringResource(R.string.settings_keyboard_haptics),
                        checked = haptics,
                        enabled = true
                    ) { checked ->
                        haptics = checked
                        services.settings.keyboardHaptics = checked
                    }
                }
                Footnote(stringResource(R.string.settings_keyboard_layouts_footer))
            }

            AccountSection(onOpenSubscription = { onOpen(Routes.Subscription) })

            AppSection(stringResource(R.string.settings_appearance)) {
                SingleChoiceSegmentedButtonRow(modifier = Modifier.fillMaxWidth()) {
                    AppearancePreference.entries.forEachIndexed { index, option ->
                        SegmentedButton(
                            selected = appearance == option,
                            onClick = {
                                appearance = option
                                services.settings.appearance = option
                            },
                            shape = SegmentedButtonDefaults.itemShape(
                                index = index,
                                count = AppearancePreference.entries.size
                            )
                        ) {
                            Text(
                                stringResource(
                                    when (option) {
                                        AppearancePreference.SYSTEM -> R.string.settings_appearance_system
                                        AppearancePreference.LIGHT -> R.string.settings_appearance_light
                                        AppearancePreference.DARK -> R.string.settings_appearance_dark
                                    }
                                )
                            )
                        }
                    }
                }
            }

            AppSection(stringResource(R.string.settings_language)) {
                RowGroup {
                    LanguageRow(
                        label = stringResource(R.string.common_system),
                        selected = language == null
                    ) {
                        language = null
                        services.settings.appLanguage = null
                    }
                    AppLanguage.entries.forEach { option ->
                        RowDividerIndented()
                        // Always shown in its own language: a Kazakh speaker
                        // looking for Kazakh should see "Қазақша".
                        LanguageRow(label = option.nativeName, selected = language == option) {
                            language = option
                            services.settings.appLanguage = option
                        }
                    }
                }
                Footnote(stringResource(R.string.settings_language_footer))
            }

            AppSection(stringResource(R.string.settings_privacy_title)) {
                AppCard { Footnote(stringResource(R.string.settings_privacy_body)) }
                RowGroup {
                    NavigationRow(
                        Icons.Outlined.Description,
                        stringResource(R.string.legal_terms)
                    ) {
                        uriHandler.openUri(
                            "${accountState.legalConfig.termsUrl}?lang=${services.settings.effectiveAppLanguage.code}"
                        )
                    }
                    RowDividerIndented()
                    NavigationRow(
                        Icons.Outlined.Lock,
                        stringResource(R.string.legal_privacy)
                    ) {
                        uriHandler.openUri(
                            "${accountState.legalConfig.privacyUrl}?lang=${services.settings.effectiveAppLanguage.code}"
                        )
                    }
                }
            }

            AppSection(stringResource(R.string.settings_setup_restart)) {
                AppCard {
                    TextButton(onClick = { services.configuration.restartOnboarding() }) {
                        Text(stringResource(R.string.settings_setup_restart))
                    }
                }
                Footnote(stringResource(R.string.settings_setup_restart_footer))
            }

            // Debug builds only: release builds do not have this section.
            if (BuildConfig.DEBUG) {
                AppSection(stringResource(R.string.settings_developer)) {
                    RowGroup {
                        SwitchRow(
                            label = stringResource(R.string.settings_debug_mock_replies),
                            checked = mockReplies,
                            enabled = true
                        ) { checked ->
                            mockReplies = checked
                            services.settings.debugMockReplies = checked
                        }
                    }
                    Footnote(stringResource(R.string.settings_debug_mock_replies_footer))
                }
            }
        }
    }
}

@Composable
private fun LanguageRow(label: String, selected: Boolean, onClick: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .padding(horizontal = Spacing.m, vertical = Spacing.s),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(Spacing.s)
    ) {
        Text(label, style = MaterialTheme.typography.bodyLarge)
        Spacer(Modifier.weight(1f))
        if (selected) {
            Icon(
                Icons.Filled.Check,
                contentDescription = null,
                tint = MaterialTheme.colorScheme.primary,
                modifier = Modifier.size(20.dp)
            )
        }
    }
}

@Composable
private fun SwitchRow(label: String, checked: Boolean, enabled: Boolean, onChange: (Boolean) -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(enabled = enabled) { onChange(!checked) }
            .padding(horizontal = Spacing.m, vertical = Spacing.xs),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(Spacing.s)
    ) {
        Text(label, style = MaterialTheme.typography.bodyLarge, modifier = Modifier.weight(1f))
        Switch(checked = checked, onCheckedChange = onChange, enabled = enabled)
    }
}
