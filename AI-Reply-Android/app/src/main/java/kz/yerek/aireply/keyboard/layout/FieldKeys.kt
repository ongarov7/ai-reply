package kz.yerek.aireply.keyboard.layout

import android.text.InputType
import kz.yerek.aireply.core.lang.KeyboardPlane

/**
 * What a host field's `EditorInfo.inputType` asks of the keys: the page it
 * opens on, the bottom row's own key, what its number pad offers, and
 * whether it is a password.
 *
 * Gboard's and Samsung's reading of the same flags:
 *
 *  * `TYPE_CLASS_PHONE` - the phone keypad.
 *  * `TYPE_CLASS_NUMBER` - the number pad, with `-` only for
 *    `TYPE_NUMBER_FLAG_SIGNED` and `.` only for `TYPE_NUMBER_FLAG_DECIMAL`.
 *    A PIN (`TYPE_NUMBER_VARIATION_PASSWORD`) is digits like any other.
 *  * `TYPE_CLASS_DATETIME` - the date pad, one for dates, times and both:
 *    `.` `/` `-` for a date, `:` for a time, space between them.
 *  * `TYPE_CLASS_TEXT` - the letters; `@` or `/` beside space in e-mail and
 *    URL fields, and a row of digits on top in a password field.
 *  * Anything else, `TYPE_NULL` included (a terminal, a raw key field) -
 *    the letters.
 *
 * Pure, so the choice is tested without a running keyboard.
 *
 * Өрістің түрі: сан, телефон, күн - сандық панель; құпиясөз - әріптердің
 * үстінде сандар қатары.
 */
data class FieldKeys(
    /** The page the field opens on. */
    val plane: KeyboardPlane = KeyboardPlane.LETTERS,
    /** The bottom row's key beside space, on the letters and ?123. */
    val kind: FieldKind = FieldKind.TEXT,
    /** The number pad's `-`. */
    val signed: Boolean = false,
    /** The number pad's `.`. */
    val decimal: Boolean = false,
    /** A text password: digits over the letters, where they fit. */
    val password: Boolean = false
) {

    /** A field for numbers, phone numbers or dates: it opens on a pad. */
    val isNumeric: Boolean get() = plane.isPad

    /**
     * The page to show as this field starts, [current] being the page up now
     * and [previous] the field before this one.
     *
     *  * A field opens on its own page: its pad, or the letters.
     *  * A restart of the same kind of field - the app re-applying its
     *    EditorInfo while the user types, a messenger clearing the field
     *    after Send - keeps the page the user chose: ?123 in a chat, or the
     *    letters they went to from a pad. A restart that changed what the
     *    field takes (text to a number, say) opens on the new field's page.
     *  * While an AI panel is open the keys type into the panel's fields,
     *    which are text: the letters, or the page the user picked there -
     *    never a pad.
     */
    fun startPlane(current: KeyboardPlane, previous: FieldKeys, restarting: Boolean, composing: Boolean): KeyboardPlane =
        when {
            composing -> if (restarting && !current.isPad) current else KeyboardPlane.LETTERS
            restarting && previous.plane == plane -> current
            else -> plane
        }

    companion object {

        fun of(inputType: Int): FieldKeys {
            val variation = inputType and InputType.TYPE_MASK_VARIATION
            return when (inputType and InputType.TYPE_MASK_CLASS) {
                InputType.TYPE_CLASS_PHONE -> FieldKeys(plane = KeyboardPlane.PHONE_PAD)
                InputType.TYPE_CLASS_NUMBER -> FieldKeys(
                    plane = KeyboardPlane.NUMBER_PAD,
                    signed = inputType and InputType.TYPE_NUMBER_FLAG_SIGNED != 0,
                    decimal = inputType and InputType.TYPE_NUMBER_FLAG_DECIMAL != 0
                )
                InputType.TYPE_CLASS_DATETIME -> FieldKeys(plane = KeyboardPlane.DATE_PAD)
                InputType.TYPE_CLASS_TEXT -> FieldKeys(
                    kind = when (variation) {
                        InputType.TYPE_TEXT_VARIATION_EMAIL_ADDRESS,
                        InputType.TYPE_TEXT_VARIATION_WEB_EMAIL_ADDRESS -> FieldKind.EMAIL
                        InputType.TYPE_TEXT_VARIATION_URI -> FieldKind.URL
                        else -> FieldKind.TEXT
                    },
                    password = variation == InputType.TYPE_TEXT_VARIATION_PASSWORD ||
                        variation == InputType.TYPE_TEXT_VARIATION_VISIBLE_PASSWORD ||
                        variation == InputType.TYPE_TEXT_VARIATION_WEB_PASSWORD
                )
                else -> FieldKeys()
            }
        }
    }
}
