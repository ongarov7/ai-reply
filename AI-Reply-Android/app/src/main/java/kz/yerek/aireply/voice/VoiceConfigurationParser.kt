package kz.yerek.aireply.voice

import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.core.text.clampToCodePoints
import kz.yerek.aireply.core.text.codePointLength
import kz.yerek.aireply.domain.model.BusinessContext
import kz.yerek.aireply.domain.model.DaySchedule
import kz.yerek.aireply.domain.model.ReplyTemplate
import kz.yerek.aireply.domain.model.TimeOfDay
import kz.yerek.aireply.domain.model.WorkingHours

/**
 * Turns a dictated sentence into profile and template settings.
 *
 * WHAT THIS IS FOR. A user says "I sell clothes, we work from ten to six, don't
 * tell a customer something is in stock unless we know". Saving that as one blob
 * of text works, but then the app cannot reason about the schedule and the model
 * has to re-derive "is it after hours" on every single reply. Extracting the
 * schedule here makes the working-hours context deterministic.
 *
 * WHAT IT DELIBERATELY IS NOT. It is not an understanding of the sentence, and
 * it never silently replaces what the user said. Everything it finds is returned
 * as a [Suggestion] the user confirms on screen, and the transcript itself is
 * always kept verbatim. When it recognises nothing, the flow is what it was
 * before: free text the model reads.
 *
 * It runs locally with no network call, so dictating a profile costs nothing and
 * works offline.
 *
 * The same rules as the iOS parser, on purpose: the two apps must take the same
 * things from the same sentence.
 */
object VoiceConfigurationParser {

    data class Suggestion(
        val start: TimeOfDay? = null,
        val end: TimeOfDay? = null,
        /** Calendar weekdays (Sunday = 1), or null when the sentence named no days. */
        val weekdays: List<Int>? = null,
        /** Sentences that read like a policy the reply must respect. */
        val rules: List<String> = emptyList()
    ) {
        val hasWorkingHours: Boolean get() = start != null && end != null

        val isEmpty: Boolean get() = !hasWorkingHours && weekdays == null && rules.isEmpty()

        /**
         * The schedule this describes. With no days named it keeps the days
         * [basedOn] has open, and Monday to Friday if none are.
         */
        fun workingHours(basedOn: WorkingHours): WorkingHours? {
            val from = start ?: return null
            val to = end ?: return null
            if (from >= to) return null

            val days = weekdays ?: basedOn.days.filter { it.isEnabled }.map { it.weekday }
            val active = days.ifEmpty { listOf(2, 3, 4, 5, 6) }.toSet()

            return WorkingHours(
                isEnabled = true,
                days = (1..7).map { weekday ->
                    DaySchedule(weekday = weekday, isEnabled = weekday in active, start = from, end = to)
                }
            )
        }

        /** Only what the user ticked. */
        fun confirmed(applyHours: Boolean, selectedRules: Set<Int>): Suggestion {
            val ticked = rules.filterIndexed { index, _ -> index in selectedRules }
            return if (applyHours) {
                copy(rules = ticked)
            } else {
                Suggestion(rules = ticked)
            }
        }

        /** [template] with this applied, on top of its own hours or else [profileHours]. */
        fun appliedTo(template: ReplyTemplate, profileHours: WorkingHours): ReplyTemplate {
            var result = template
            workingHours(basedOn = template.workingHoursOverride ?: profileHours)?.let {
                result = result.copy(workingHoursOverride = it)
            }
            if (rules.isNotEmpty()) {
                val business = rules.fold(template.business ?: BusinessContext.EMPTY, BusinessContext::addingRule)
                result = result.copy(business = business)
            }
            return result
        }
    }

    fun parse(transcript: String, language: AppLanguage): Suggestion {
        val text = transcript.lowercase()
        val (start, end) = resolveRange(timeCandidates(text))
        return Suggestion(
            start = start,
            end = end,
            weekdays = weekdays(text),
            rules = rules(transcript, language)
        )
    }

    // ----------------------------------------------------------------- times

    private enum class Meridiem { MORNING, AFTERNOON }

    private data class TimeCandidate(
        val position: Int,
        val hour: Int,
        val minute: Int,
        /** "утра" / "pm" / "кешкі" and friends, when the speaker said one. */
        val meridiem: Meridiem?
    )

    /**
     * "18:00", "18.00", "18". The trailing boundary keeps "300" out and stops
     * "10" inside "2010" from matching.
     *
     * Spelled with `\p{Nd}` and `[0-9]` because `\d` means different things on
     * the JVM the tests run on and on the phone's regex engine.
     */
    private val digitTime = Regex("""(?<![\p{Nd}:.])([01]?[0-9]|2[0-3])(?:[:.]([0-5][0-9]))?(?!\p{Nd})""")

    /**
     * Hour words, so "from ten to six" and "с десяти до шести" work as well as
     * "10:00-18:00". Only 1-12: nobody dictates "from seventeen hundred".
     */
    private val hourWords: Map<String, Int> = mapOf(
        // English
        "one" to 1, "two" to 2, "three" to 3, "four" to 4, "five" to 5, "six" to 6,
        "seven" to 7, "eight" to 8, "nine" to 9, "ten" to 10, "eleven" to 11, "twelve" to 12,
        // Russian, in the forms speech recognition actually returns after "с"/"до"
        "час" to 1, "часа" to 1, "одного" to 1, "двух" to 2, "трёх" to 3, "трех" to 3, "четырёх" to 4,
        "четырех" to 4, "пяти" to 5, "шести" to 6, "семи" to 7, "восьми" to 8, "девяти" to 9,
        "десяти" to 10, "одиннадцати" to 11, "двенадцати" to 12,
        // Kazakh
        "бір" to 1, "екі" to 2, "үш" to 3, "төрт" to 4, "бес" to 5, "алты" to 6,
        "жеті" to 7, "сегіз" to 8, "тоғыз" to 9, "он" to 10, "он бір" to 11, "он екі" to 12
    )

    private val morningMarkers = listOf("утра", "am", "a.m.", "morning", "таңғы", "таңертең")
    private val afternoonMarkers =
        listOf("вечера", "дня", "pm", "p.m.", "evening", "afternoon", "кешкі", "түстен кейін")

    private const val MERIDIEM_WINDOW = 24

    private fun timeCandidates(text: String): List<TimeCandidate> {
        val found = digitTime.findAll(text).map { match ->
            val (hour, minute) = match.destructured
            TimeCandidate(
                position = match.range.first,
                hour = hour.toInt(),
                minute = minute.toIntOrNull() ?: 0,
                meridiem = meridiem(text, match.range.first + hour.length)
            )
        }.toMutableList()

        // Hour words only when no digits were dictated at all. Mixing the two
        // ("from 10 to six") is rare enough that preferring the unambiguous
        // source is the safer rule.
        if (found.isEmpty()) {
            for ((word, hour) in hourWords) {
                var index = text.indexOf(word)
                while (index >= 0) {
                    val end = index + word.length
                    if (isWholeWord(text, index, end)) {
                        found += TimeCandidate(index, hour, 0, meridiem(text, end))
                    }
                    index = text.indexOf(word, index + 1)
                }
            }
        }

        return found.sortedBy { it.position }
    }

    /** By hand rather than with `\b`, which stops at Cyrillic on the JVM but not on the phone. */
    private fun isWholeWord(text: String, start: Int, end: Int): Boolean =
        (start == 0 || !isWordCharacter(text[start - 1])) && (end == text.length || !isWordCharacter(text[end]))

    private fun isWordCharacter(character: Char): Boolean = character.isLetterOrDigit() || character == '_'

    /** Looks a short distance past the number for "утра" / "pm" / "кешкі". */
    private fun meridiem(text: String, index: Int): Meridiem? {
        val window = text.substring(index, minOf(index + MERIDIEM_WINDOW, text.length))
        return when {
            morningMarkers.any { window.contains(it) } -> Meridiem.MORNING
            afternoonMarkers.any { window.contains(it) } -> Meridiem.AFTERNOON
            else -> null
        }
    }

    /**
     * The first two candidates are the range. A closing hour that lands before
     * the opening one is read as the afternoon, which is what "from ten to six"
     * means to everyone who says it.
     */
    private fun resolveRange(candidates: List<TimeCandidate>): Pair<TimeOfDay?, TimeOfDay?> {
        if (candidates.size < 2) return null to null
        val (first, second) = candidates

        var startHour = first.hour
        var endHour = second.hour
        if (first.meridiem == Meridiem.AFTERNOON && startHour < 12) startHour += 12
        if (second.meridiem == Meridiem.AFTERNOON && endHour < 12) endHour += 12
        if (second.meridiem == null && endHour <= startHour && endHour + 12 <= 23) endHour += 12

        val start = TimeOfDay.of(startHour, first.minute)
        val end = TimeOfDay.of(endHour, second.minute)
        return if (start < end) start to end else null to null
    }

    // ------------------------------------------------------------------ days

    private val everydayMarkers = listOf(
        "каждый день", "ежедневно", "без выходных", "все дни",
        "күн сайын", "әр күні", "демалыссыз",
        "every day", "everyday", "daily", "seven days", "all week"
    )

    private val weekdayMarkers = listOf(
        "по будням", "будни", "будние", "с понедельника по пятницу",
        "жұмыс күндері", "дүйсенбіден жұмаға",
        "weekdays", "monday to friday", "monday through friday", "mon-fri"
    )

    private val saturdayMarkers = listOf(
        "по субботам", "с понедельника по субботу", "включая субботу",
        "сенбіні қоса", "дүйсенбіден сенбіге",
        "monday to saturday", "including saturday", "mon-sat"
    )

    private fun weekdays(text: String): List<Int>? = when {
        everydayMarkers.any { text.contains(it) } -> listOf(1, 2, 3, 4, 5, 6, 7)
        saturdayMarkers.any { text.contains(it) } -> listOf(2, 3, 4, 5, 6, 7)
        weekdayMarkers.any { text.contains(it) } -> listOf(2, 3, 4, 5, 6)
        else -> null
    }

    // ----------------------------------------------------------------- rules

    /**
     * Markers that make a sentence read like a policy rather than a description.
     * Conservative on purpose: a missed rule is a line the user keeps in their
     * own words, while a false one is a line they have to delete.
     */
    private fun ruleMarkers(language: AppLanguage): List<String> {
        val shared = listOf("если ", "егер ", " if ", "never", "always")
        return shared + when (language) {
            AppLanguage.RUSSIAN -> listOf(
                "не говори", "не обещай", "не пиши", "нельзя", "не указывай",
                "не подтверждай", "всегда", "никогда", "обязательно", "скажи"
            )
            AppLanguage.KAZAKH -> listOf(
                "айтпа", "уәде бер", "жазба", "болмайды", "әрқашан",
                "ешқашан", "міндетті", "деп айт", "растама"
            )
            AppLanguage.ENGLISH -> listOf(
                "don't", "do not", "never", "always", "avoid", "make sure",
                "tell them", "must not", "should not"
            )
            AppLanguage.UZBEK -> listOf(
                "aytmang", "va'da bermang", "yozmang", "mumkin emas",
                "har doim", "hech qachon", "albatta", "tasdiqlamang"
            )
        }
    }

    private fun rules(transcript: String, language: AppLanguage): List<String> {
        val markers = ruleMarkers(language)
        val result = ArrayList<String>()

        for (sentence in sentences(transcript)) {
            val lowered = sentence.lowercase()
            if (markers.none { lowered.contains(it) }) continue
            if (sentence.codePointLength() <= MINIMUM_RULE_LENGTH) continue
            result += sentence.clampToCodePoints(BusinessContext.MAX_RULE)
            if (result.size == BusinessContext.MAX_RULES) break
        }
        return result
    }

    private const val MINIMUM_RULE_LENGTH = 8

    /**
     * A sentence split that tolerates dictation, which often produces no
     * punctuation at all: then the whole transcript is one sentence and is
     * offered as a single rule.
     */
    private fun sentences(text: String): List<String> =
        text.split('.', '!', '?', '\n').map { it.trim() }.filter { it.isNotEmpty() }
}
