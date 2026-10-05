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
}
