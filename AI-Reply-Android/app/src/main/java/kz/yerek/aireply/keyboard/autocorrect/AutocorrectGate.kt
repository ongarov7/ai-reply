package kz.yerek.aireply.keyboard.autocorrect

import android.text.InputType
import android.view.inputmethod.EditorInfo

/**
 * Which host fields get smart correction (DESIGN §6.5). Everywhere else the
 * keyboard types exactly as it did before: plain `commitText`, no composing
 * text, no suggestions.
 *
 * Ақылды түзету қай өрісте жұмыс істейді: құпиясөз, сілтеме, пошта - жоқ.
 */
object AutocorrectGate {

    /** Text variations that are not prose: secrets, addresses, filters. */
    private val EXCLUDED_VARIATIONS = setOf(
        InputType.TYPE_TEXT_VARIATION_PASSWORD,
        InputType.TYPE_TEXT_VARIATION_VISIBLE_PASSWORD,
        InputType.TYPE_TEXT_VARIATION_WEB_PASSWORD,
        InputType.TYPE_TEXT_VARIATION_URI,
        InputType.TYPE_TEXT_VARIATION_EMAIL_ADDRESS,
        InputType.TYPE_TEXT_VARIATION_WEB_EMAIL_ADDRESS,
        InputType.TYPE_TEXT_VARIATION_FILTER
    )

    /**
     * True when the field described by [inputType] should be corrected, with
     * the user's smart-correction setting [enabled]. `TYPE_NULL` (a terminal,
     * a raw key field), numbers, phones and dates, and fields that ask for no
     * suggestions are left alone.
     */
    fun allows(inputType: Int, enabled: Boolean): Boolean {
        if (!enabled || inputType == InputType.TYPE_NULL) return false
        if (inputType and InputType.TYPE_MASK_CLASS != InputType.TYPE_CLASS_TEXT) return false
        if (inputType and InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS != 0) return false
        return inputType and InputType.TYPE_MASK_VARIATION !in EXCLUDED_VARIATIONS
    }

    /**
     * Names and postal addresses are full of words no dictionary knows, and a
     * surname "corrected" into a common word is worse than a typo: the strip
     * still suggests there, but a separator never replaces the word.
     */
    private val NO_AUTO_REPLACEMENT = setOf(
        InputType.TYPE_TEXT_VARIATION_PERSON_NAME,
        InputType.TYPE_TEXT_VARIATION_POSTAL_ADDRESS
    )

    /** True when a separator may replace the word typed in [inputType]'s field with a correction. */
    fun autoReplaces(inputType: Int): Boolean =
        inputType and InputType.TYPE_MASK_CLASS != InputType.TYPE_CLASS_TEXT ||
            inputType and InputType.TYPE_MASK_VARIATION !in NO_AUTO_REPLACEMENT

    fun autoReplaces(info: EditorInfo?): Boolean = info == null || autoReplaces(info.inputType)

    /** False when the app asks the keyboard not to learn from this field (an incognito tab, say). */
    fun learns(imeOptions: Int): Boolean = imeOptions and EditorInfo.IME_FLAG_NO_PERSONALIZED_LEARNING == 0

    fun allows(info: EditorInfo?, enabled: Boolean): Boolean = info != null && allows(info.inputType, enabled)

    fun learns(info: EditorInfo?): Boolean = info == null || learns(info.imeOptions)
}
