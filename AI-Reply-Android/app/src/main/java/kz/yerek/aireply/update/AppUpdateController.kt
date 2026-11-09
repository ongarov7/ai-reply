package kz.yerek.aireply.update

import android.content.Context
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import com.google.android.play.core.appupdate.AppUpdateManagerFactory
import com.google.android.play.core.install.model.UpdateAvailability
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlin.coroutines.resume

/** What the store says about a newer version. */
sealed interface UpdateCheck {
    /** The store answered: the newer version's code, or null when this one is the latest. */
    data class Answered(val availableVersionCode: Int?) : UpdateCheck
    data object Offline : UpdateCheck
    data object Failed : UpdateCheck
}

fun interface UpdateSource {
    suspend fun check(): UpdateCheck
}

/** What is remembered between launches, on this phone only. */
interface UpdateStore {
    var updateLastCheck: Long?
    var updateOfferedVersion: Int?
    var updateDismissedVersion: Int?
    var updateDismissedAt: Long?
}

/**
 * When the "update available" card shows. Pure, so it is tested on the JVM.
 *
 * Жаңарту картасының ережелері: тәулігіне бір тексеру, «Кейінірек» жеті күнге.
 */
object AppUpdatePolicy {
    const val AUTOMATIC_INTERVAL_MS = 24L * 60 * 60 * 1000
    const val REMIND_LATER_MS = 7L * 24 * 60 * 60 * 1000

    /** At most one automatic check a day, and the time is written before the request. */
    fun isCheckDue(now: Long, lastCheck: Long?): Boolean =
        lastCheck == null || now < lastCheck || now - lastCheck >= AUTOMATIC_INTERVAL_MS

    /** «Позже» hides one version for a week; a newer version shows at once. */
    fun showsCard(offered: Int?, installed: Int, dismissedVersion: Int?, dismissedAt: Long?, now: Long): Boolean {
        if (offered == null || offered <= installed) return false
        if (dismissedVersion == null || dismissedAt == null || dismissedVersion < offered) return true
        return now < dismissedAt || now - dismissedAt >= REMIND_LATER_MS
    }
}

/**
 * Tells the user when a newer version is on Google Play, without nagging: a
 * card on Home after a check a day, and a manual check in Settings. Updating
 * opens the app's page on Google Play, so the store's own screen does the rest.
 *
 * Жаңарту: Google Play-де жаңа нұсқа болса, Басты бетте карточка көрсетіледі.
 */
class AppUpdateController(
    private val source: UpdateSource,
    private val store: UpdateStore,
    private val installedVersion: Int,
    private val enabled: Boolean,
    private val clock: () -> Long = System::currentTimeMillis
) {

    enum class Status { IDLE, CHECKING, CURRENT, OFFLINE, UNAVAILABLE, OPEN_FAILED }

    data class State(val offeredVersion: Int? = null, val showsCard: Boolean = false, val status: Status = Status.IDLE)

    private val _state = MutableStateFlow(snapshot(Status.IDLE))
    val state: StateFlow<State> = _state.asStateFlow()

    private fun offered(): Int? = store.updateOfferedVersion?.takeIf { it > installedVersion }

    private fun snapshot(status: Status) = State(
        offeredVersion = offered(),
        showsCard = AppUpdatePolicy.showsCard(offered(), installedVersion, store.updateDismissedVersion, store.updateDismissedAt, clock()),
        status = status
    )

    /** Home opened or the app came back: asks the store at most once a day. */
    suspend fun checkIfDue() {
        if (!enabled) return
        if (!AppUpdatePolicy.isCheckDue(clock(), store.updateLastCheck)) {
            _state.value = snapshot(_state.value.status)
            return
        }
        run(manual = false)
    }

    /** «Проверить обновления»: now, whatever the last check was. */
    suspend fun checkNow() {
        run(manual = true)
    }

    private suspend fun run(manual: Boolean) {
        if (_state.value.status == Status.CHECKING) return
        val lastCheck = store.updateLastCheck
        store.updateLastCheck = clock()
        _state.update { it.copy(status = Status.CHECKING) }
        val answer = try {
            source.check()
        } catch (cancelled: CancellationException) {
            // Whoever asked has gone: nothing stays "checking", and the day's check is not spent.
            store.updateLastCheck = lastCheck
            _state.value = snapshot(Status.IDLE)
            throw cancelled
        }
        val status = when (answer) {
            is UpdateCheck.Answered -> {
                store.updateOfferedVersion = answer.availableVersionCode
                if (answer.availableVersionCode == null) Status.CURRENT else Status.IDLE
            }
            UpdateCheck.Offline -> Status.OFFLINE
            UpdateCheck.Failed -> Status.UNAVAILABLE
        }
        _state.value = snapshot(if (manual || status == Status.IDLE) status else Status.IDLE)
    }

    /** «Позже»: this version waits a week. */
    fun remindLater() {
        store.updateDismissedVersion = offered() ?: return
        store.updateDismissedAt = clock()
        _state.value = snapshot(Status.IDLE)
    }

    fun storeDidNotOpen() {
        _state.update { it.copy(status = Status.OPEN_FAILED) }
    }
}

/** The answer from Google Play; a sideloaded build gets [UpdateCheck.Failed]. */
class PlayUpdateSource(context: Context) : UpdateSource {

    private val appContext = context.applicationContext
    private val manager by lazy { AppUpdateManagerFactory.create(appContext) }

    override suspend fun check(): UpdateCheck {
        if (!isOnline()) return UpdateCheck.Offline
        return suspendCancellableCoroutine { continuation ->
            manager.appUpdateInfo
                .addOnSuccessListener { info ->
                    val available = info.updateAvailability() == UpdateAvailability.UPDATE_AVAILABLE
                    if (continuation.isActive) continuation.resume(UpdateCheck.Answered(if (available) info.availableVersionCode() else null))
                }
                .addOnFailureListener {
                    if (continuation.isActive) continuation.resume(UpdateCheck.Failed)
                }
        }
    }

    private fun isOnline(): Boolean {
        val manager = appContext.getSystemService(ConnectivityManager::class.java) ?: return true
        val capabilities = manager.getNetworkCapabilities(manager.activeNetwork) ?: return false
        return capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
    }
}
