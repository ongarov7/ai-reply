package kz.yerek.aireply.keyboard.autocorrect

import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.keyboard.layout.KeyboardLayout
import kotlin.math.abs

/**
 * Which letter keys touch on a layout: hitting a neighbour is the commonest
 * typo, so autocorrect charges less for it.
 *
 * Пернетақтада қатар тұрған әріптер: көрші пернені басу — ең жиі қате.
 *
 * Built from [KeyboardLayout]'s own rows, so it cannot drift from what the
 * user sees. Every letter row is centred on the widest one, which is exactly
 * how the pages place them: the English middle row inset by half a key, the
 * bottom rows between shift and delete, the Kazakh row centred over ЙЦУКЕН.
 * Two keys touch when they are on the same or neighbouring rows and their
 * centres are at most one key apart. A long-press letter (`ё` behind `е`,
 * `ъ` behind `ь`) sits on its key.
 */
class KeyProximity private constructor(private val neighbours: Map<Char, String>) {

    fun areAdjacent(typed: Char, other: Char): Boolean =
        neighbours[typed]?.indexOf(other)?.let { it >= 0 } == true

    companion object {

        private val byLanguage: Map<KeyboardLanguage, KeyProximity> by lazy {
            KeyboardLanguage.entries.associateWith { language ->
                fromRows(KeyboardLayout.letters(language)) { key -> KeyboardLayout.alternates(key, language) }
            }
        }

        fun of(language: KeyboardLanguage): KeyProximity = byLanguage.getValue(language)

        /**
         * [rows] are the letter rows top to bottom; [alternates] gives the
         * long-press characters of a key.
         */
        internal fun fromRows(rows: List<List<String>>, alternates: (String) -> List<String>): KeyProximity {
            // Centres in half-key units, so every position is a whole number.
            class Position(val row: Int, val centre: Int)

            val widest = rows.maxOf { it.size }
            val positions = LinkedHashMap<Char, Position>()
            rows.forEachIndexed { row, keys ->
                keys.forEachIndexed { index, key ->
                    val letter = key.singleOrNull() ?: return@forEachIndexed
                    val position = Position(row, widest - keys.size + 2 * index + 1)
                    positions[letter] = position
                    alternates(key)
                        .mapNotNull { it.singleOrNull()?.takeIf(Char::isLetter) }
                        .forEach { positions.putIfAbsent(it, position) }
                }
            }

            val neighbours = positions.mapValues { (letter, position) ->
                positions.entries
                    .filter { (other, at) ->
                        other != letter && abs(at.row - position.row) <= 1 && abs(at.centre - position.centre) <= 2
                    }
                    .joinToString("") { it.key.toString() }
            }
            return KeyProximity(neighbours)
        }
    }
}
