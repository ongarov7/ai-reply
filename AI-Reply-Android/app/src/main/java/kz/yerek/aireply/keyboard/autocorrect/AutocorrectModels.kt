package kz.yerek.aireply.keyboard.autocorrect

import kz.yerek.aireply.core.lang.KeyboardLanguage

/** A dictionary word close to what was typed (DESIGN §7.4). */
data class Candidate(
    /** The word list's display form; the typed word's case is applied later. */
    val word: String,
    val rank: Int,
    /** The weighted edit distance in tenths, exact for threshold checks. */
    internal val tenths: Int,
    /** distance + 0.15·log10(rank + 1): lower is better. */
    val score: Double
) {
    val distance: Double get() = tenths / 10.0
}

/** One slot of the suggestion strip. */
data class Suggestion(val text: String, val kind: Kind) {

    enum class Kind {
        /** What the user typed, shown in quotes: tapping keeps and learns it. */
        TYPED,

        /** The correction the next separator applies, shown as the default. */
        CORRECTION,

        /** The Kazakh spelling of a word typed with plain letters; never applied by itself. */
        HINT,

        /** A dictionary word close to the typed one. */
        CANDIDATE,

        /** A longer word that starts with the typed one. */
        COMPLETION
    }
}

/**
 * Everything the keyboard needs about the word being typed, worked out off
 * the main thread after each keystroke.
 *
 * Теріліп жатқан сөз туралы бәрі: түзету және ұсыныстар жолағы.
 */
data class AutocorrectAnalysis(
    /** What a separator turns the word into; null keeps it as typed. */
    val correction: String?,
    /** Up to three strip items in display order (§7.6); empty hides the strip. */
    val suggestions: List<Suggestion>
) {
    companion object {
        val NONE = AutocorrectAnalysis(correction = null, suggestions = emptyList())
    }
}

/**
 * A correction a separator just applied, kept until the next key: backspace
 * right away takes it back (§7.7).
 */
data class AppliedCorrection(
    val original: String,
    val correction: String,
    /** What was typed after the word: a space, or one of `.,!?;:` and the like. */
    val separator: String,
    val language: KeyboardLanguage
) {
    /**
     * UTF-16 units before the caret that undoing replaces with [original]
     * (without a separator): the correction and its separator.
     */
    val undoLength: Int get() = correction.length + separator.length
}
