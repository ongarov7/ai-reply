package kz.yerek.aireply.diagnostics

import android.content.Context
import android.content.SharedPreferences
import android.os.Build
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

/**
 * Crash summaries for the operator's alert channel.
 *
 * There is no crash SDK in this app: the keyboard has to stay light. An
 * uncaught exception is reduced to the class of its root cause and the first
 * frame inside our own package, written synchronously before the process dies,
 * and sent to `POST /api/v1/client-errors` on the next start, where the server
 * turns it into a Telegram alert. Never the exception message (it can quote
 * what the person typed), never a token.
 */
object CrashReporter {

    /** Where a pending summary waits for the next start. */
    interface Store {
        fun load(): Pending?
        fun save(pending: Pending)
        fun clear()
    }

    data class Pending(val code: String, val detail: String, val count: Int, val attempts: Int)

    /** Attempts before a summary the server keeps refusing is dropped. */
    const val MAX_ATTEMPTS = 3

    private const val OWN_PACKAGE = "kz.yerek.aireply"

    fun install(context: Context) {
        val previous = Thread.getDefaultUncaughtExceptionHandler()
        if (previous is Handler) return
        Thread.setDefaultUncaughtExceptionHandler(Handler(PreferencesStore(context), previous))
    }

    internal class Handler(
        private val store: Store,
        private val previous: Thread.UncaughtExceptionHandler?
    ) : Thread.UncaughtExceptionHandler {
        override fun uncaughtException(thread: Thread, error: Throwable) {
            runCatching { record(store, error) }
            previous?.uncaughtException(thread, error)
        }
    }

    /** The root cause's class and our first frame: "IllegalStateException", "ReplyKeyboardService.kt:120 onStartInput". */
    fun summary(error: Throwable): Pair<String, String> {
        val chain = generateSequence(error) { it.cause }.take(16).toList()
        val root = chain.last()
        val code = root.javaClass.name.substringAfterLast('.')
            .replace(Regex("[^A-Za-z0-9_.:/-]"), "_").take(80).ifEmpty { "Throwable" }
        val frame = chain.asReversed().asSequence()
            .flatMap { it.stackTrace.asSequence() }
            .firstOrNull { it.className.startsWith(OWN_PACKAGE) }
        val detail = frame?.let { "${it.fileName}:${it.lineNumber} ${it.methodName}" }.orEmpty()
        return code to detail
    }

    fun record(store: Store, error: Throwable) {
        val (code, detail) = summary(error)
        val previous = store.load()
        val count = if (previous != null && previous.code == code) previous.count + 1 else 1
        store.save(Pending(code, detail, count, attempts = 0))
    }

    /** The request body for the pending summary. */
    fun body(pending: Pending, appVersion: String, osVersion: String = Build.VERSION.RELEASE.orEmpty()): String =
        buildJsonObject {
            put("platform", "android")
            put("kind", "crash")
            put("code", pending.code)
            put("detail", pending.detail)
            put("count", pending.count.coerceIn(1, 10000))
            put("app_version", appVersion.filter { it.isLetterOrDigit() || it in " ._()+-" }.take(40))
            put("os_version", osVersion.filter { it.isLetterOrDigit() || it in " ._()+-" }.take(40))
        }.toString()

    /**
     * Sends a pending summary once per start. The summary stays for the next
     * start when the server cannot be reached, and is dropped after
     * [MAX_ATTEMPTS] so a refused report never loops forever.
     */
    suspend fun sendPending(store: Store, appVersion: String, send: suspend (String) -> Unit) {
        val pending = store.load() ?: return
        if (pending.attempts >= MAX_ATTEMPTS) {
            store.clear()
            return
        }
        store.save(pending.copy(attempts = pending.attempts + 1))
        val sent = runCatching { send(body(pending, appVersion)) }.isSuccess
        if (sent) store.clear()
    }

    class PreferencesStore(context: Context) : Store {
        private val prefs: SharedPreferences =
            context.applicationContext.getSharedPreferences("aireply_crash_report", Context.MODE_PRIVATE)

        override fun load(): Pending? {
            val code = prefs.getString("code", null) ?: return null
            return Pending(code, prefs.getString("detail", "").orEmpty(), prefs.getInt("count", 1), prefs.getInt("attempts", 0))
        }

        // commit(): the process is about to die, apply() would lose the write.
        override fun save(pending: Pending) {
            prefs.edit().putString("code", pending.code).putString("detail", pending.detail)
                .putInt("count", pending.count).putInt("attempts", pending.attempts).commit()
        }

        override fun clear() {
            prefs.edit().clear().apply()
        }
    }
}
