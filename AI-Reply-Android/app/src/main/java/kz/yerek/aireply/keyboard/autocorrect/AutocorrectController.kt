package kz.yerek.aireply.keyboard.autocorrect

import android.view.inputmethod.EditorInfo
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.keyboard.input.HostField
import kz.yerek.aireply.keyboard.input.KeyboardTextFieldState
import kz.yerek.aireply.platform.ReplyLog
import java.io.IOException

/**
 * Smart correction while typing: follows the word being typed, keeps the
 * suggestion strip up to date, applies a correction on a separator and takes
 * it back on an immediate backspace (DESIGN §6.5, §7.5-§7.7).
 *
 * Теру кезіндегі ақылды түзету: ұсыныстар жолағы, түзету, кері қайтару.
 *
 * In the host's field the word is composing text ([HostField.compose]); in
 * the keyboard's own fields it is the letters before the caret. Suggestions are
 * worked out on [worker], one thread, and only the latest request's answer is
 * shown; nothing waits for them - a separator uses the answer already worked
 * out for exactly that word, and when it has not landed yet the word ends as
 * typed: the dictionaries are never searched on the main thread (iOS does the
 * same). Nothing typed is logged, stored (apart from the words the user chose
 * to keep) or sent.
 *
 * A strip pick adds a space after the word. Like Gboard and iOS, that space
 * is the keyboard's, not the user's: a punctuation mark typed next takes its
 * place ("слово, " rather than "слово ,"), and a space typed next is not a
 * second one.
 *
 * Where smart correction is off - the setting, or a field it does not belong
 * in ([AutocorrectGate]) - typing in the host is the plain `commitText` it
 * always was: no composing text, no strip, nothing looked up.
 */
class AutocorrectController(
    private val engine: AutocorrectEngine,
    private val host: HostField,
    /** The keyboard's main-thread scope. */
    private val scope: CoroutineScope,
    /** One background thread for suggestions. */
    private val worker: CoroutineDispatcher
) {

    /** Where the strip belongs: over the keys, or in the open panel's action row. */
    enum class Place { HOST, COMPOSER }

    /** The strip's items in display order; empty hides it. */
    var suggestions: List<Suggestion> by mutableStateOf(emptyList())
        private set

    /** Where [suggestions] belong; null while there are none. */
    var place: Place? by mutableStateOf(null)
        private set

    /** Smart correction applies to the host's field. */
    var correctsHost: Boolean = false
        private set

    /** Smart correction applies to the keyboard's own fields (the user's setting). */
    var correctsFields: Boolean = false
        private set

    private var language = KeyboardLanguage.KAZAKH
    private var learns = true

    private val hostSurface = HostSurface(host)

    /** Where the word the strip is about is being typed; null when there is none. */
    private var surface: TypingSurface? = null

    /** The correction the last separator applied, until the next key. */
    private var undo: Undo? = null

    private class Undo(val applied: AppliedCorrection, val before: String, val surface: TypingSurface)

    /**
     * Where the space right before the caret was added by the keyboard (a
     * strip pick, or punctuation that took that space's place), until the
     * next key; null otherwise.
     */
    private var autoSpace: TypingSurface? = null

    /** The latest answer, kept for the separator that usually follows it. */
    private var latest: Analysis? = null

    private class Analysis(val word: String, val before: String, val language: KeyboardLanguage, val result: AutocorrectAnalysis)

    private var job: Job? = null

    /** Bumped by every request, so only the latest answer is shown. */
    private var generation = 0

    private val loading = HashMap<KeyboardLanguage, Job>()

    // --------------------------------------------------------- configuration

    /**
     * A field starts: what applies to it, read once ([smartCorrection] is the
     * user's setting). Learning follows the field's wish for privacy in the
     * keyboard's own fields too, since what is written there goes to it.
     */
    fun startInput(info: EditorInfo?, smartCorrection: Boolean, language: KeyboardLanguage) {
        correctsHost = AutocorrectGate.allows(info, smartCorrection)
        correctsFields = smartCorrection
        learns = AutocorrectGate.learns(info)
        clear()
        switchLanguage(language)
    }

    /** The layout changed. The word typed so far is kept as it is. */
    fun switchLanguage(language: KeyboardLanguage) {
        endWord()
        this.language = language
        load(language)
    }

    /**
     * Ends the word being typed in the host as typed and hides the strip:
     * before any edit that is not typing, before a panel opens, when the
     * keyboard goes away.
     */
    fun endWord() {
        host.finishComposing()
        clear()
    }

    /** Forgets the word, the strip and the undo; the text stays as it is. */
    fun clear() {
        undo = null
        autoSpace = null
        latest = null
        surface = null
        generation++
        job?.cancel()
        show(null, emptyList())
    }

    private fun load(language: KeyboardLanguage) {
        if (!correctsHost && !correctsFields) return
        if (engine.isReady(language) || loading[language]?.isActive == true) return
        loading[language] = scope.launch {
            try {
                engine.load(language)
            } catch (failure: IOException) {
                // The layout simply types without suggestions; a later start retries.
                ReplyLog.warn(failure) { "autocorrect: ${language.code} dictionaries unavailable" }
            }
        }
    }

    // ------------------------------------------------------------- the host

    /** A key or a long-press alternate in the host's field. */
    fun typeInHost(text: String) {
        if (correctsHost) type(hostSurface, Place.HOST, text) else host.commitText(text)
    }

    /**
     * Backspace in the host's field: undoes the correction just applied, or
     * edits the word being typed. False when it is an ordinary delete, left
     * to the caller.
     */
    fun deleteInHost(): Boolean = correctsHost && delete(hostSurface, Place.HOST)

    /** The host reported its selection; see [HostField.selectionChanged]. */
    fun hostSelectionChanged(selectionStart: Int, selectionEnd: Int, composingStart: Int, composingEnd: Int) {
        if (!host.selectionChanged(selectionStart, selectionEnd, composingStart, composingEnd)) return
        // The caret moved or the text changed under it: no word, nothing to
        // undo, and the space before the caret is not known to be ours.
        undo = null
        autoSpace = null
        if (surface === hostSurface) clear()
    }

    // ----------------------------------------------------- the keyboard's fields

    /**
     * A key in one of the keyboard's own fields. False when nothing changed:
     * the field refused the text (its limit), or a space was not doubled.
     */
    fun typeInField(field: KeyboardTextFieldState, text: String, limit: Int?): Boolean =
        type(FieldSurface(field, limit), Place.COMPOSER, text, limit)

    /** Backspace in one of the keyboard's own fields: true when it undid a correction. */
    fun deleteInField(field: KeyboardTextFieldState, limit: Int?): Boolean = delete(FieldSurface(field, limit), Place.COMPOSER)

    /** The field changed some other way (a delete, a tap): the strip follows its caret. */
    fun fieldChanged(field: KeyboardTextFieldState, limit: Int?) {
        undo = null
        autoSpace = null
        refresh(FieldSurface(field, limit), Place.COMPOSER)
    }

    // ----------------------------------------------------------------- strip

    /**
     * A tap on the strip: the word becomes [suggestion] and a space follows.
     * The quoted typed word is kept and learned. False when there is no word
     * any more to replace.
     */
    fun pick(suggestion: Suggestion): Boolean {
        val surface = surface ?: return false
        val word = surface.word
        if (word.isEmpty()) return false
        undo = null
        autoSpace = null
        val picked = surface.endWord(suggestion.text, " ")
        if (picked) autoSpace = surface
        if (picked && suggestion.kind == Suggestion.Kind.TYPED && learns) engine.learn(word, language)
        refresh(surface, place ?: Place.HOST)
        return picked
    }

    // ------------------------------------------------------------- internals

    private fun type(surface: TypingSurface, place: Place, text: String, limit: Int? = null): Boolean {
        undo = null
        val spaced = autoSpace?.takeIf { it.sameAs(surface) }
        autoSpace = null
        if (spaced != null) {
            afterAutoSpace(surface, text, limit)?.let { edited ->
                refresh(surface, place)
                return edited
            }
        }
        val word = surface.word
        val edited = when {
            WordText.continuesWord(text, word) -> surface.extendWord(text) || surface.type(text)
            WordText.endsWordWithCorrection(text) && word.isNotEmpty() -> endWord(surface, word, text)
            else -> surface.type(text)
        }
        refresh(surface, place)
        return edited
    }

    /**
     * The key right after a space the keyboard added: a punctuation mark
     * takes that space's place and the space moves after it ("слово, "); a
     * space is not doubled. Null when [text] is something else, or that space
     * is no longer right before the caret: typed as usual. Otherwise whether
     * the text changed.
     */
    private fun afterAutoSpace(surface: TypingSurface, text: String, limit: Int?): Boolean? {
        val punctuation = text.length == 1 && text[0] in PUNCTUATION_AFTER_WORD
        if (!punctuation && text != " ") return null
        if (surface.word.isNotEmpty()) return null
        return when (surface) {
            is HostSurface -> {
                if (host.textBeforeCursor(1) != " ") return null
                if (!punctuation) return false
                host.batch {
                    host.deleteBefore(1)
                    host.commitText("$text ")
                }
                autoSpace = surface
                true
            }
            is FieldSurface -> {
                val state = surface.state
                if (!state.textBeforeCursor().endsWith(" ")) return null
                if (!punctuation) return false
                when {
                    state.replaceBeforeCursor(1, "$text ", limit) -> {
                        autoSpace = surface
                        true
                    }
                    // At the field's limit the mark still replaces the space.
                    else -> state.replaceBeforeCursor(1, text, limit)
                }
            }
            else -> null
        }
    }

    /** The separator [separator] ends [word]: corrected when the engine is sure, as typed otherwise. */
    private fun endWord(surface: TypingSurface, word: String, separator: String): Boolean {
        val before = surface.textBeforeWord
        val correction = correctionFor(word, before)
        if (correction != null && correction != word && surface.endWord(correction, separator)) {
            undo = Undo(AppliedCorrection(word, correction, separator, language), before, surface)
            return true
        }
        return surface.endWord(word, separator)
    }

    private fun delete(surface: TypingSurface, place: Place): Boolean {
        val pending = undo
        undo = null
        autoSpace = null
        if (pending != null && pending.surface.sameAs(surface) && surface.restore(pending.applied, pending.before)) {
            engine.undo(pending.applied, learn = learns)
            refresh(surface, place)
            return true
        }
        if (!surface.shortenWord()) return false
        refresh(surface, place)
        return true
    }

    private fun TypingSurface.sameAs(other: TypingSurface): Boolean =
        this === other || this is FieldSurface && other is FieldSurface && state === other.state

    /**
     * The correction worked out in the background for exactly this word, or
     * none. A separator never searches the dictionaries itself: that would
     * be on the main thread, inside the key press. A word typed faster than
     * its analysis simply ends as typed (iOS does the same).
     */
    private fun correctionFor(word: String, before: String): String? {
        val lookup = WordText.lookupWord(word)
        if (lookup.isEmpty()) return null
        val known = latest ?: return null
        if (known.word != lookup || known.before != before || known.language != language) return null
        return known.result.correction
    }

    /** Works out the strip for the word now before the caret, latest request wins. */
    private fun refresh(surface: TypingSurface, place: Place) {
        val mine = ++generation
        job?.cancel()
        val word = WordText.lookupWord(surface.word)
        if (word.isEmpty()) {
            this.surface = null
            show(null, emptyList())
            return
        }
        this.surface = surface
        val before = surface.textBeforeWord
        val language = language
        job = scope.launch {
            val result = withContext(worker) { engine.analyze(word, before, language) }
            if (mine != generation) return@launch
            latest = Analysis(word, before, language, result)
            show(place, result.suggestions)
        }
    }

    private fun show(place: Place?, items: List<Suggestion>) {
        suggestions = items
        this.place = if (items.isEmpty()) null else place
    }

    private companion object {
        /** Marks written right after a word, never after a space: they take a picked word's space. */
        const val PUNCTUATION_AFTER_WORD = ".,!?;:"
    }
}
