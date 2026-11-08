package kz.yerek.aireply.domain.model

import kotlinx.serialization.Serializable
import java.time.DayOfWeek
import java.time.LocalDateTime
import java.time.format.TextStyle
import java.util.Calendar
import java.util.Locale

/**
 * A time of day, as minutes since midnight.
 *
 * Stored as an integer rather than an instant on purpose. "10:00" is a
 * wall-clock fact about the user's week; persisting a timestamp would bake in
 * whatever timezone and calendar date happened to be current when they set it,
 * and the schedule would quietly shift when they travelled.
 */
@Serializable
data class TimeOfDay(val minutes: Int = 0) : Comparable<TimeOfDay> {

    val hour: Int get() = clamped / 60
    val minute: Int get() = clamped % 60

    private val clamped: Int get() = minutes.coerceIn(0, 24 * 60 - 1)

    override fun compareTo(other: TimeOfDay): Int = clamped.compareTo(other.clamped)

    /**
     * 24-hour "HH:mm". Used both in the UI and in the model context, so the two
     * can never disagree about what the user configured.
     */
    val formatted: String get() = String.format(Locale.ROOT, "%02d:%02d", hour, minute)

    companion object {
        fun of(hour: Int, minute: Int) = TimeOfDay(hour * 60 + minute)
    }
}

/**
 * One day's availability. [weekday] follows [Calendar]'s convention where
 * Sunday is 1, matching iOS, so the on-disk JSON is identical on both
 * platforms and there is no translation layer to get wrong.
 */
@Serializable
data class DaySchedule(
    val weekday: Int,
    val isEnabled: Boolean,
    val start: TimeOfDay,
    val end: TimeOfDay
) {
    /** Guards against a schedule that can never be inside itself. */
    val isValid: Boolean get() = isEnabled && start < end
}

/**
 * The user's optional working-hours configuration.
 *
 * TIMEZONE. Everything is evaluated against the device's own clock. No timezone
 * is stored, no timezone is hard-coded, and the backend is never told where the
 * user is — only whether it is currently inside their hours and when the next
 * window opens.
 */
@Serializable
data class WorkingHours(
    val isEnabled: Boolean = false,
    val days: List<DaySchedule> = defaultDays()
) {

    fun schedule(weekday: Int): DaySchedule? = days.firstOrNull { it.weekday == weekday }

    fun withDay(weekday: Int, transform: (DaySchedule) -> DaySchedule): WorkingHours {
        val updated = days.map { if (it.weekday == weekday) transform(it) else it }
        return copy(days = updated)
    }

    fun togglingDay(weekday: Int): WorkingHours =
        withDay(weekday) { it.copy(isEnabled = !it.isEnabled) }

    /** The start the quick editor shows: the first open day's, or 10:00 with every day closed. */
    val quickStart: TimeOfDay
        get() = days.firstOrNull { it.isEnabled }?.start ?: TimeOfDay.of(10, 0)

    /** The end the quick editor shows: the first open day's, or 18:00 with every day closed. */
    val quickEnd: TimeOfDay
        get() = days.firstOrNull { it.isEnabled }?.end ?: TimeOfDay.of(18, 0)

    /**
     * Editing one time applies it to every open day, which is what a single
     * "from / to" control implies. Per-day differences stay possible in the
     * full editor.
     */
    fun withQuickStart(value: TimeOfDay): WorkingHours =
        copy(days = days.map { if (it.isEnabled) it.copy(start = value) else it })

    fun withQuickEnd(value: TimeOfDay): WorkingHours =
        copy(days = days.map { if (it.isEnabled) it.copy(end = value) else it })

    /**
     * Everything the model needs to reason about time, and nothing more.
     *
     * Note what is absent: no timezone name, no city, no coordinates, no UTC
     * offset. A wall-clock time and two booleans answer every question the
     * reply actually needs to answer.
     */
    data class Context(
        val isEnabled: Boolean,
        val isWithinWorkingHours: Boolean,
        val currentLocalTime: String,
        val nextWorkingPeriod: String?,
        val weeklySchedule: String?
    )

    /**
     * @param now injectable so the behaviour is testable without waiting for
     *   18:30 to come round.
     */
    fun context(now: LocalDateTime = LocalDateTime.now()): Context {
        val time = TimeOfDay(now.hour * 60 + now.minute)

        if (!isEnabled) {
            return Context(
                isEnabled = false,
                isWithinWorkingHours = true,
                currentLocalTime = time.formatted,
                nextWorkingPeriod = null,
                weeklySchedule = null
            )
        }

        val weekday = calendarWeekday(now.dayOfWeek)
        val today = schedule(weekday)
        val isWithin = today != null && today.isValid && time >= today.start && time < today.end

        return Context(
            isEnabled = true,
            isWithinWorkingHours = isWithin,
            currentLocalTime = time.formatted,
            nextWorkingPeriod = if (isWithin) null else nextPeriodDescription(weekday, time),
            weeklySchedule = weeklySummary()
        )
    }

    /**
     * A short phrase such as "today 14:00", "tomorrow 10:00" or "Monday 10:00".
     *
     * Deliberately English and deliberately machine-ish: it is model context,
     * not UI. The model renders it into the conversation's own language, which
     * is what makes a Kazakh reply say "ертең 10:00" without this file needing
     * to know how to decline a Kazakh weekday.
     *
     * Pinned to [Locale.ENGLISH] rather than the device locale so the same
     * schedule produces the same prompt on every phone.
     */
    private fun nextPeriodDescription(currentWeekday: Int, time: TimeOfDay): String? {
        schedule(currentWeekday)?.let { today ->
            if (today.isValid && time < today.start) return "today ${today.start.formatted}"
        }

        // The next seven days, so a schedule with exactly one open day still
        // resolves rather than returning null.
        for (offset in 1..7) {
            val weekday = ((currentWeekday - 1 + offset) % 7) + 1
            val day = schedule(weekday) ?: continue
            if (!day.isValid) continue
            if (offset == 1) return "tomorrow ${day.start.formatted}"
            val name = dayOfWeek(weekday).getDisplayName(TextStyle.FULL_STANDALONE, Locale.ENGLISH)
            return "$name ${day.start.formatted}"
        }
        return null
    }

    /**
     * Compact description of the whole week, grouping consecutive days that
     * share the same hours: "Mon-Fri 10:00-16:00, Sat 11:00-14:00".
     */
    private fun weeklySummary(): String? {
        // Monday-first ordering reads correctly regardless of whether the
        // user's locale starts its week on Sunday.
        val ordered = MONDAY_FIRST.mapNotNull { schedule(it) }.filter { it.isValid }
        if (ordered.isEmpty()) return null

        val groups = ArrayList<String>()
        var index = 0
        while (index < ordered.size) {
            val first = ordered[index]
            var last = index
            while (last + 1 < ordered.size &&
                ordered[last + 1].start == first.start &&
                ordered[last + 1].end == first.end &&
                isAdjacent(ordered[last].weekday, ordered[last + 1].weekday)
            ) {
                last++
            }
            val range = if (last > index) {
                "${shortName(first.weekday)}-${shortName(ordered[last].weekday)}"
            } else {
                shortName(first.weekday)
            }
            groups.add("$range ${first.start.formatted}-${first.end.formatted}")
            index = last + 1
        }
        return groups.joinToString(", ")
    }

    private fun shortName(weekday: Int): String =
        dayOfWeek(weekday).getDisplayName(TextStyle.SHORT_STANDALONE, Locale.ENGLISH)

    /** Adjacency in Monday-first order, so Sat and Sun group and Sun never joins Mon. */
    private fun isAdjacent(lhs: Int, rhs: Int): Boolean {
        fun position(weekday: Int) = if (weekday == 1) 6 else weekday - 2
        return position(rhs) == position(lhs) + 1
    }

    companion object {
        /** Calendar order, Monday first. */
        val MONDAY_FIRST = listOf(2, 3, 4, 5, 6, 7, 1)

        /**
         * Monday to Friday, 10:00-16:00, weekend off. A reasonable starting
         * point that the user then edits.
         */
        val DEFAULT = WorkingHours(isEnabled = false, days = defaultDays())

        private fun defaultDays(): List<DaySchedule> = (1..7).map { weekday ->
            DaySchedule(
                weekday = weekday,
                isEnabled = weekday in 2..6,
                start = TimeOfDay.of(10, 0),
                end = TimeOfDay.of(16, 0)
            )
        }

        /** [Calendar] weekday (Sunday = 1) for a [DayOfWeek]. */
        fun calendarWeekday(day: DayOfWeek): Int = (day.value % 7) + 1

        /** [DayOfWeek] for a [Calendar] weekday (Sunday = 1). */
        fun dayOfWeek(weekday: Int): DayOfWeek =
            DayOfWeek.of(if (weekday == 1) 7 else weekday - 1)
    }
}
