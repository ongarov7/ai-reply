package kz.yerek.aireply.push

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

/**
 * The signed-in user's notification categories, for the Settings switches.
 *
 * Хабарлама санаттары: ауыстырғыш бірден өзгереді, сервер қабылдамаса — кері қайтады.
 *
 * A switch moves the moment it is tapped (optimistic) and moves back if the
 * server refuses or cannot be reached. Changes go out one at a time, each
 * naming only its own category, so two quick taps cannot overwrite each other.
 */
class NotificationPreferencesRepository(private val api: NotificationPreferencesApi) {

    data class State(
        /** What the server last said, or null before the first answer. */
        val server: Map<String, Boolean>? = null,
        val optional: Set<String> = emptySet(),
        /** Taps the server has not confirmed yet; they win over [server] on screen. */
        val pending: Map<String, Boolean> = emptyMap(),
        val loading: Boolean = false,
        /** The last load or change failed (the switch was put back). */
        val failed: Boolean = false
    ) {
        val isLoaded: Boolean get() = server != null

        /** On unless the server or a pending tap says off. */
        fun isOn(category: String): Boolean = pending[category] ?: server?.get(category) ?: true

        /** Security, and anything the server does not list as optional, cannot be switched off. */
        fun isLocked(category: String): Boolean = category !in optional
    }

    private val _state = MutableStateFlow(State())
    val state: StateFlow<State> = _state.asStateFlow()

    private val writes = Mutex()

    suspend fun load() {
        _state.update { it.copy(loading = true, failed = false) }
        try {
            val result = api.load()
            _state.update {
                it.copy(
                    server = result.preferences,
                    optional = result.optional.toSet(),
                    loading = false
                )
            }
        } catch (cancelled: CancellationException) {
            _state.update { it.copy(loading = false) }
            throw cancelled
        } catch (failure: Exception) {
            _state.update { it.copy(loading = false, failed = true) }
        }
    }

    /**
     * Switches [category]; true when the server kept the change. A locked
     * category is refused locally, without a request.
     */
    suspend fun set(category: String, enabled: Boolean): Boolean {
        if (_state.value.isLocked(category)) return false
        _state.update { it.copy(pending = it.pending + (category to enabled), failed = false) }
        return writes.withLock {
            try {
                val result = api.update(mapOf(category to enabled))
                _state.update {
                    it.copy(
                        server = result.preferences,
                        optional = result.optional.toSet().ifEmpty { it.optional },
                        pending = it.pending.withoutIf(category, enabled)
                    )
                }
                true
            } catch (cancelled: CancellationException) {
                _state.update { it.copy(pending = it.pending.withoutIf(category, enabled)) }
                throw cancelled
            } catch (failure: Exception) {
                // Rolled back: the switch shows the server's value again.
                _state.update { it.copy(pending = it.pending.withoutIf(category, enabled), failed = true) }
                false
            }
        }
    }

    /** Signed out: nothing of the previous account stays on screen. */
    fun clear() {
        _state.value = State()
    }

    /**
     * Uses the preferences a signed-in installation registration answered
     * with. Only once loaded: that answer does not say which are optional.
     */
    fun adopt(preferences: Map<String, Boolean>) {
        _state.update { if (it.loading || !it.isLoaded) it else it.copy(server = preferences) }
    }

    private fun Map<String, Boolean>.withoutIf(category: String, value: Boolean): Map<String, Boolean> =
        if (this[category] == value) this - category else this
}
