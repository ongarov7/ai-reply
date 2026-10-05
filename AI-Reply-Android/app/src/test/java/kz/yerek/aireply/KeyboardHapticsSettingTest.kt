package kz.yerek.aireply

import kz.yerek.aireply.data.settings.SettingsStore
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * "Vibration on key press": on by default, and the user's choice sticks. The
 * app and the keyboard read the same preferences file, so what one writes the
 * other sees.
 *
 * Пернетақта дірілі: әдепкі бойынша қосулы, таңдау сақталады.
 */
class KeyboardHapticsSettingTest {

    @Test
    fun `vibration is on until the user switches it off`() {
        val file = InMemoryPreferences()
        assertTrue("existing users keep today's behaviour", SettingsStore(file).keyboardHaptics)

        SettingsStore(file).keyboardHaptics = false
        assertFalse("the keyboard sees the app's choice", SettingsStore(file).keyboardHaptics)

        SettingsStore(file).keyboardHaptics = true
        assertTrue(SettingsStore(file).keyboardHaptics)
    }

    /** Ақылды түзету де әдепкі бойынша қосулы. */
    @Test
    fun `smart correction is on until the user switches it off`() {
        val file = InMemoryPreferences()
        assertTrue("on by default, for existing users too", SettingsStore(file).smartCorrection)

        SettingsStore(file).smartCorrection = false
        assertFalse("the keyboard sees the app's choice", SettingsStore(file).smartCorrection)
        assertTrue("one switch does not touch the other", SettingsStore(file).keyboardHaptics)

        SettingsStore(file).smartCorrection = true
        assertTrue(SettingsStore(file).smartCorrection)
    }
}
