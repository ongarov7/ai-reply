package kz.yerek.aireply

import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.domain.model.DaySchedule
import kz.yerek.aireply.domain.model.RelationshipKind
import kz.yerek.aireply.domain.model.ReplyTemplate
import kz.yerek.aireply.domain.model.TimeOfDay
import kz.yerek.aireply.domain.model.WorkingHours
import kz.yerek.aireply.ui.feature.hours.weekdayFullName
import kz.yerek.aireply.ui.feature.hours.weekdayName
import kz.yerek.aireply.ui.feature.hours.weekdaySymbol
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import java.util.Locale

/**
 * A template's own working hours: off by default, seeded from the profile when
 * switched on, and edited through one "from / to" for every open day.
 */
class TemplateHoursTest {

    private val deviceLocale = Locale.getDefault()

    @Before
    fun phoneInGerman() {
        Locale.setDefault(Locale.GERMAN)
    }

    @After
    fun restoreLocale() {
        Locale.setDefault(deviceLocale)
    }

    private val client = ReplyTemplate.builtIn(RelationshipKind.CLIENT, 0)

    /** Mon-Sat 09:00-20:00, with Wednesday shorter: a schedule nobody gets by default. */
    private val profileHours = WorkingHours(
        isEnabled = false,
        days = (1..7).map { weekday ->
            DaySchedule(
                weekday = weekday,
                isEnabled = weekday != 1,
                start = TimeOfDay.of(9, 0),
                end = if (weekday == 4) TimeOfDay.of(13, 0) else TimeOfDay.of(20, 0)
            )
        }
    )

    @Test
    fun `switching on starts from the profile's schedule and enables it`() {
        val override = client.withHoursOverride(true, profileHours).workingHoursOverride!!
        assertTrue(override.isEnabled)
        assertEquals(profileHours.days, override.days)
    }

    @Test
    fun `switching on again keeps what the template already had`() {
        val own = profileHours.withQuickStart(TimeOfDay.of(11, 0))
        val template = client.copy(workingHoursOverride = own.copy(isEnabled = false))
        assertEquals(own.days, template.withHoursOverride(true, WorkingHours.DEFAULT).workingHoursOverride!!.days)
    }

    @Test
    fun `switching off drops the override so the template follows the profile`() {
        val on = client.withHoursOverride(true, profileHours)
        val off = on.withHoursOverride(false, profileHours)
        assertNull(off.workingHoursOverride)
        assertEquals(profileHours, off.effectiveWorkingHours(profileHours))
    }

    @Test
    fun `the times shown are the first open day's`() {
        assertEquals("09:00", profileHours.quickStart.formatted)
        assertEquals("20:00", profileHours.quickEnd.formatted)
    }

    @Test
    fun `with every day closed the editor still has something to show`() {
        val closed = WorkingHours(isEnabled = true, days = profileHours.days.map { it.copy(isEnabled = false) })
        assertEquals("10:00", closed.quickStart.formatted)
        assertEquals("18:00", closed.quickEnd.formatted)
    }

    @Test
    fun `editing a time changes every open day and leaves closed days alone`() {
        val edited = profileHours.withQuickStart(TimeOfDay.of(8, 30)).withQuickEnd(TimeOfDay.of(17, 15))
        WorkingHours.MONDAY_FIRST.filter { it != 1 }.forEach { weekday ->
            assertEquals("08:30", edited.schedule(weekday)!!.start.formatted)
            assertEquals("17:15", edited.schedule(weekday)!!.end.formatted)
        }
        assertEquals("sunday is closed", profileHours.schedule(1), edited.schedule(1))
    }

    @Test
    fun `a weekday button opens and closes only its own day`() {
        val sundayOpen = profileHours.togglingDay(1)
        assertTrue(sundayOpen.schedule(1)!!.isEnabled)
        assertEquals(profileHours.days.filter { it.weekday != 1 }, sundayOpen.days.filter { it.weekday != 1 })
        assertFalse(sundayOpen.togglingDay(1).schedule(1)!!.isEnabled)
    }

    @Test
    fun `weekdays are named in the app's language whatever the phone is set to`() {
        assertEquals("Пн", weekdayName(2, AppLanguage.RUSSIAN))
        assertEquals("Mon", weekdayName(2, AppLanguage.ENGLISH))
        assertEquals("пн", weekdaySymbol(2, AppLanguage.RUSSIAN))
        assertEquals("Mo", weekdaySymbol(2, AppLanguage.ENGLISH))
        assertEquals("понедельник", weekdayFullName(2, AppLanguage.RUSSIAN))
        assertEquals("Dushanba", weekdayFullName(2, AppLanguage.UZBEK).replaceFirstChar { it.uppercase() })
        assertEquals("дүйсенбі", weekdayFullName(2, AppLanguage.KAZAKH))
    }
}
