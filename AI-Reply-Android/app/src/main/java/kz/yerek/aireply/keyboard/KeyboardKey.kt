package kz.yerek.aireply.keyboard

import kz.yerek.aireply.core.lang.KeyboardPlane

/** Everything a key can do. Pure data: the surface decides how it looks. */
sealed interface KeyboardKey {

    /** Types this text. Letters follow shift; everything else is typed as is. */
    data class Character(val value: String) : KeyboardKey
    data object Shift : KeyboardKey
    data object Backspace : KeyboardKey
    /** Letters, ?123 or =\<. */
    data class Plane(val target: KeyboardPlane) : KeyboardKey
    /** Switches to the next system input method; a long press opens the picker. */
    data object Globe : KeyboardKey
    /** AI Reply's own ҚАЗ / РУС / ENG switch. */
    data object Layout : KeyboardKey
    data object Space : KeyboardKey
    data object Return : KeyboardKey

    val isCharacter: Boolean get() = this is Character

    /** Keys that change text (and so wake a reply up for editing). */
    val editsText: Boolean
        get() = this is Character || this == Backspace || this == Space || this == Return
}

/** Which of the four visual treatments a key wears. */
enum class KeyVisualStyle { LETTER, SPECIAL, ENGAGED, PROMINENT }
