package kz.yerek.aireply.ui.feature.compose

import android.content.Context
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kz.yerek.aireply.keyboard.input.ClipboardMiss
import kz.yerek.aireply.keyboard.input.ContextTextProvider

/** Why Paste brought nothing: each case has its own sentence. */
enum class PasteNotice { EMPTY, PRIVATE, UNREADABLE }

/** What one tap on Paste ended with. */
sealed interface PasteOutcome {
    data class Text(val text: String) : PasteOutcome
    data class Notice(val notice: PasteNotice) : PasteOutcome
}

/**
 * Paste in «Создать ответ»: the keyboard's clipboard rules, applied to the
 * app's own button.
 *
 * Қою: тек басқанда, құпия белгіленген мәтін оқылмайды, бос буфер айтылады.
 *
 * Read only on the tap, through [ContextTextProvider] - the item's own text
 * first, a clip flagged EXTRA_IS_SENSITIVE never opened - and an empty or
 * private clipboard says so instead of doing nothing.
 */
object ComposePaste {

    fun outcome(result: ContextTextProvider.Result): PasteOutcome? = when (result) {
        is ContextTextProvider.Result.Success -> PasteOutcome.Text(result.context.text)
        is ContextTextProvider.Result.Failure -> PasteOutcome.Notice(
            when (result.miss) {
                ClipboardMiss.MARKED_PRIVATE -> PasteNotice.PRIVATE
                ClipboardMiss.UNREADABLE -> PasteNotice.UNREADABLE
                ClipboardMiss.NOTHING_COPIED, ClipboardMiss.SECURE_FIELD -> PasteNotice.EMPTY
            }
        )
        // Text behind another app's provider: read by [read], off the main thread.
        is ContextTextProvider.Result.Deferred -> null
    }

    /** One user tap. Text behind another app's provider is fetched on the IO dispatcher. */
    suspend fun read(context: Context, provider: ContextTextProvider = ContextTextProvider()): PasteOutcome {
        val first = provider.acquireClipboard(context)
        outcome(first)?.let { return it }
        val late = withContext(Dispatchers.IO) { (first as ContextTextProvider.Result.Deferred).read() }
        return outcome(late) ?: PasteOutcome.Notice(PasteNotice.UNREADABLE)
    }
}
