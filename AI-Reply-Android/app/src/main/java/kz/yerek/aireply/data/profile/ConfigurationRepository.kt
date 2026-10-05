package kz.yerek.aireply.data.profile

import android.content.Context
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.core.lang.LocalizedContext
import kz.yerek.aireply.core.lang.TemplateNaming
import kz.yerek.aireply.data.settings.SettingsStore
import kz.yerek.aireply.domain.model.ReplyConfiguration
import kz.yerek.aireply.domain.model.ReplyTemplate
import kz.yerek.aireply.domain.model.TemplateSummary
import kz.yerek.aireply.domain.model.UserProfile

/**
 * The single observable view over [ProfileStore].
 *
 * One instance lives in the [kz.yerek.aireply.ServiceLocator], so every screen
 * edits the same value and the keyboard sees each change the moment it is
 * written. The iOS project's `ReplyConfigurationModel` is the same idea with
 * `@Observable` instead of a `StateFlow`.
 *
 * Writes are synchronous because every caller is a settings screen being left,
 * never anything on a typing path.
 */
class ConfigurationRepository(
    private val appContext: Context,
    private val store: ProfileStore,
    private val settings: SettingsStore,
    private val scope: CoroutineScope
) {

    private val _configuration = MutableStateFlow(store.load())
    val configuration: StateFlow<ReplyConfiguration> = _configuration.asStateFlow()

    val current: ReplyConfiguration get() = _configuration.value

    val profile: UserProfile get() = current.profile

    // ------------------------------------------------------------- mutations

    fun updateProfile(transform: (UserProfile) -> UserProfile) {
        persist(current.copy(profile = transform(current.profile)))
    }

    fun update(template: ReplyTemplate) {
        val index = current.templates.indexOfFirst { it.id == template.id }
        if (index < 0) return
        val updated = current.templates.toMutableList().also { it[index] = template }
        persist(current.copy(templates = updated))
    }

    fun addCustomTemplate(name: String): ReplyTemplate {
        val template = ReplyTemplate.custom(name, current.templates.size)
        persist(current.copy(templates = current.templates + template))
        return template
    }

    /**
     * Built-ins are never destroyed, only hidden, so a user cannot end up with
     * an empty keyboard bar and no way to get the defaults back.
     */
    fun delete(template: ReplyTemplate) {
        if (template.isBuiltIn) return
        persist(current.copy(templates = current.templates.filterNot { it.id == template.id }))
    }

    fun move(fromIndex: Int, toIndex: Int) {
        val ordered = current.orderedTemplates.toMutableList()
        if (fromIndex !in ordered.indices || toIndex !in ordered.indices) return
        ordered.add(toIndex, ordered.removeAt(fromIndex))
        persist(current.copy(templates = ordered.mapIndexed { i, t -> t.copy(sortIndex = i) }))
    }

    fun setVisible(isVisible: Boolean, template: ReplyTemplate) {
        val index = current.templates.indexOfFirst { it.id == template.id }
        if (index < 0) return
        val updated = current.templates.toMutableList()
        updated[index] = updated[index].copy(isVisible = isVisible)
        persist(current.copy(templates = updated))
    }

    /** Re-reads from disk. Used when returning to the app after the keyboard ran. */
    fun reload() {
        _configuration.value = store.load()
    }

    // --------------------------------------------------------------- writing

    private fun persist(configuration: ReplyConfiguration) {
        val normalized = configuration.normalized()
        _configuration.value = normalized
        store.save(normalized)

        // The keyboard draws its chip row from this summary before the full file
        // is read, so it is refreshed with every save rather than derived later.
        // Resolving the three names needs three localized Contexts, which is
        // real work, so it happens off the caller's thread.
        scope.launch(Dispatchers.Default) {
            settings.templateSummaries = normalized.visibleTemplates.map { template ->
                TemplateSummary(
                    id = template.id,
                    names = AppLanguage.entries.associate { language ->
                        language.code to TemplateNaming.displayName(
                            LocalizedContext.wrap(appContext, language),
                            template
                        )
                    }
                )
            }
        }
    }
}
