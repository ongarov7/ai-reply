package kz.yerek.aireply.ui.navigation

/**
 * Every destination, as one closed set.
 *
 * Mirrors the iOS navigation exactly: onboarding runs once, Home is the root
 * afterwards, and everything else is pushed from Home or from Settings.
 */
object Routes {
    const val Onboarding = "onboarding"
    /** The onboarding again, from Settings, without resetting anything. */
    const val Tutorial = "tutorial"
    const val Home = "home"
    const val Compose = "compose"
    const val Profile = "profile"
    const val Templates = "templates"
    const val TemplateEditor = "template"
    const val WorkingHours = "hours"
    const val Settings = "settings"
    const val KeyboardSetup = "setup"
    const val Subscription = "subscription"

    fun templateEditor(id: String) = "$TemplateEditor/$id"
    const val TemplateEditorPattern = "$TemplateEditor/{id}"
    const val TemplateEditorArg = "id"

    /**
     * Settings takes an optional section to scroll to; plain [Settings] still
     * matches, because the argument is optional.
     */
    const val SettingsSectionArg = "section"
    const val SettingsPattern = "$Settings?$SettingsSectionArg={$SettingsSectionArg}"
    const val SectionNotifications = "notifications"

    fun settings(section: String) = "$Settings?$SettingsSectionArg=$section"
}
