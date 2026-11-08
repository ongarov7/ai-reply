package kz.yerek.aireply

import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.domain.model.BusinessContext
import kz.yerek.aireply.domain.model.RelationshipKind
import kz.yerek.aireply.domain.model.ReplyTemplate
import kz.yerek.aireply.domain.model.TimeOfDay
import kz.yerek.aireply.domain.model.WorkingHours
import kz.yerek.aireply.ui.feature.voice.hoursSummary
import kz.yerek.aireply.voice.VoiceConfigurationParser
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The dictation examples in all the languages, the same ones the iOS tests hold.
 *
 * These are regression tests for a feature that is allowed to be imperfect but
 * is never allowed to be WRONG: a suggestion the user can decline is fine, a
 * schedule silently set to the wrong hours is not.
 */
class VoiceConfigurationParserTest {

    private fun parse(text: String, language: AppLanguage) = VoiceConfigurationParser.parse(text, language)

    @Test
    fun `english word hours`() {
        val result = parse(
            "I sell clothes. We work from ten to six. If stock is unknown, don't tell the customer that it's available.",
            AppLanguage.ENGLISH
        )
        assertEquals("10:00", result.start?.formatted)
        assertEquals("18:00", result.end?.formatted)
        assertEquals(1, result.rules.size)
    }

    @Test
    fun `russian word hours with meridiem`() {
        val result = parse("Работаем каждый день с десяти утра до шести вечера.", AppLanguage.RUSSIAN)
        assertEquals("10:00", result.start?.formatted)
        assertEquals("18:00", result.end?.formatted)
        assertEquals(listOf(1, 2, 3, 4, 5, 6, 7), result.weekdays)
    }

    /** The Kazakh case-ending form, which is what a speaker actually says. */
    @Test
    fun `kazakh digit hours with case endings`() {
        val result = parse("Мен киім сатамын. Жұмыс уақыты 10:00-ден 18:00-ге дейін.", AppLanguage.KAZAKH)
        assertEquals("10:00", result.start?.formatted)
        assertEquals("18:00", result.end?.formatted)
    }

    @Test
    fun `russian explicit day range`() {
        val result = parse("Работаем с понедельника по субботу с 10:00 до 18:00", AppLanguage.RUSSIAN)
        assertEquals(listOf(2, 3, 4, 5, 6, 7), result.weekdays)
        assertEquals("10:00", result.start?.formatted)
    }

    @Test
    fun `kazakh rule is extracted`() {
        val result = parse("Тауар бар-жоғын нақты білмесең, бар деп айтпа.", AppLanguage.KAZAKH)
        assertEquals(1, result.rules.size)
        assertTrue(result.rules[0].contains("айтпа"))
    }

    /** A sentence with no schedule in it must not invent one. */
    @Test
    fun `no false positives`() {
        val result = parse("I run a design studio and I like short replies", AppLanguage.ENGLISH)
        assertNull(result.start)
        assertNull(result.end)
        assertNull(result.weekdays)
        assertTrue(result.isEmpty)
    }

    @Test
    fun `suggestion builds schedule only for named days`() {
        val parsed = parse("Работаем с понедельника по пятницу с 9:00 до 17:00", AppLanguage.RUSSIAN)
        val hours = parsed.workingHours(basedOn = WorkingHours.DEFAULT)
        assertEquals(true, hours?.isEnabled)
        assertEquals("monday", true, hours?.schedule(2)?.isEnabled)
        assertEquals("saturday", false, hours?.schedule(7)?.isEnabled)
        assertEquals("09:00", hours?.schedule(2)?.start?.formatted)
        assertEquals("17:00", hours?.schedule(2)?.end?.formatted)
    }

    /** An impossible range is dropped rather than stored inverted. */
    @Test
    fun `inverted range is rejected`() {
        val result = parse("с 18:00 до 18:00", AppLanguage.RUSSIAN)
        assertNull(result.start)
        assertNull(result.end)
    }

    // ------------------------------------------------ the phone's regex engine

    /**
     * `\b` and `\d` mean different things on the JVM and on Android, which is
     * why the parser does not use them. A Cyrillic word must still only match
     * as a whole word on both.
     */
    @Test
    fun `an hour word inside a longer word is not an hour`() {
        assertTrue(parse("Она ведёт семинар", AppLanguage.RUSSIAN).isEmpty)
        assertTrue(parse("tenfold sixty", AppLanguage.ENGLISH).isEmpty)
    }

    @Test
    fun `a whole cyrillic hour word is an hour`() {
        val result = parse("Открыто с девяти до семи.", AppLanguage.RUSSIAN)
        assertEquals("09:00", result.start?.formatted)
        assertEquals("19:00", result.end?.formatted)
    }

    @Test
    fun `a year is not a time`() {
        val result = parse("в 2010 начали, работаем с 9 до 18", AppLanguage.RUSSIAN)
        assertEquals("09:00", result.start?.formatted)
        assertEquals("18:00", result.end?.formatted)
    }

    @Test
    fun `digits win over words when both were said`() {
        val result = parse("с 10 до шести", AppLanguage.RUSSIAN)
        assertNull("one digit time is not a range", result.start)
    }

    // ----------------------------------------------------- what is confirmed

    @Test
    fun `with no days named the days already open stay open`() {
        val parsed = parse("с 9:00 до 17:00", AppLanguage.RUSSIAN)
        val existing = WorkingHours.DEFAULT.togglingDay(7)
        val hours = parsed.workingHours(basedOn = existing)!!
        assertEquals(existing.days.map { it.isEnabled }, hours.days.map { it.isEnabled })
    }

    @Test
    fun `with no days named and none open it falls back to the working week`() {
        val parsed = parse("с 9:00 до 17:00", AppLanguage.RUSSIAN)
        val closed = WorkingHours(days = WorkingHours.DEFAULT.days.map { it.copy(isEnabled = false) })
        val open = parsed.workingHours(basedOn = closed)!!.days.filter { it.isEnabled }.map { it.weekday }
        assertEquals(listOf(2, 3, 4, 5, 6), open)
    }

    @Test
    fun `only what was ticked is confirmed`() {
        val suggestion = VoiceConfigurationParser.Suggestion(
            start = TimeOfDay.of(10, 0),
            end = TimeOfDay.of(18, 0),
            weekdays = listOf(2, 3),
            rules = listOf("first rule", "second rule", "third rule")
        )
        val rulesOnly = suggestion.confirmed(applyHours = false, selectedRules = setOf(0, 2))
        assertFalse(rulesOnly.hasWorkingHours)
        assertNull(rulesOnly.weekdays)
        assertEquals(listOf("first rule", "third rule"), rulesOnly.rules)

        val hoursOnly = suggestion.confirmed(applyHours = true, selectedRules = emptySet())
        assertEquals(suggestion.start, hoursOnly.start)
        assertEquals(suggestion.weekdays, hoursOnly.weekdays)
        assertTrue(hoursOnly.rules.isEmpty())
    }

    @Test
    fun `applying to a template writes its own hours and adds the rules`() {
        val confirmed = VoiceConfigurationParser.Suggestion(
            start = TimeOfDay.of(9, 0),
            end = TimeOfDay.of(17, 0),
            rules = listOf("Не обещай скидки")
        )
        val friend = ReplyTemplate.builtIn(RelationshipKind.FRIEND, 0)
        val applied = confirmed.appliedTo(friend, WorkingHours.DEFAULT)
        assertTrue(applied.workingHoursOverride!!.isEnabled)
        assertEquals("09:00", applied.workingHoursOverride!!.schedule(2)!!.start.formatted)
        assertEquals("a friend gets a container only when a rule needs one", listOf("Не обещай скидки"), applied.business!!.rules)

        val client = ReplyTemplate.builtIn(RelationshipKind.CLIENT, 0)
            .copy(business = BusinessContext(rules = listOf("Старое правило")))
        assertEquals(
            listOf("Старое правило", "Не обещай скидки"),
            confirmed.appliedTo(client, WorkingHours.DEFAULT).business!!.rules
        )
    }

    @Test
    fun `declining everything leaves a template as it was`() {
        val nothing = VoiceConfigurationParser.Suggestion()
        val friend = ReplyTemplate.builtIn(RelationshipKind.FRIEND, 0)
        assertEquals(friend, nothing.appliedTo(friend, WorkingHours.DEFAULT))
    }

    @Test
    fun `the template's own hours are the base, not the profile's`() {
        val own = WorkingHours.DEFAULT.togglingDay(1)
        val template = ReplyTemplate.builtIn(RelationshipKind.CLIENT, 0).copy(workingHoursOverride = own)
        val applied = VoiceConfigurationParser.Suggestion(start = TimeOfDay.of(9, 0), end = TimeOfDay.of(17, 0))
            .appliedTo(template, WorkingHours.DEFAULT)
        assertTrue("sunday was open on the template", applied.workingHoursOverride!!.schedule(1)!!.isEnabled)
    }

    @Test
    fun `the summary names the days to be open, monday first, in the app's language`() {
        val suggestion = VoiceConfigurationParser.Suggestion(
            start = TimeOfDay.of(10, 0),
            end = TimeOfDay.of(18, 0),
            weekdays = listOf(1, 2, 3)
        )
        assertEquals("пн, вт, вс · 10:00-18:00", hoursSummary(suggestion, WorkingHours.DEFAULT, AppLanguage.RUSSIAN))
        assertEquals("Mon, Tue, Sun · 10:00-18:00", hoursSummary(suggestion, WorkingHours.DEFAULT, AppLanguage.ENGLISH))

        val noDays = suggestion.copy(weekdays = null)
        val closed = WorkingHours(days = WorkingHours.DEFAULT.days.map { it.copy(isEnabled = false) })
        assertEquals("10:00-18:00", hoursSummary(noDays, closed, AppLanguage.ENGLISH))
    }
}
