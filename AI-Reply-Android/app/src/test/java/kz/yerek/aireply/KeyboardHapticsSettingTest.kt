package kz.yerek.aireply

import android.content.SharedPreferences
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

    /** SharedPreferences without Android: a map, applied immediately. */
    private class InMemoryPreferences : SharedPreferences {
        private val values = HashMap<String, Any?>()

        override fun getAll(): MutableMap<String, *> = HashMap(values)
        override fun getString(key: String?, defValue: String?): String? = values[key] as String? ?: defValue

        @Suppress("UNCHECKED_CAST")
        override fun getStringSet(key: String?, defValues: MutableSet<String>?): MutableSet<String>? =
            (values[key] as Set<String>?)?.toMutableSet() ?: defValues

        override fun getInt(key: String?, defValue: Int): Int = values[key] as Int? ?: defValue
        override fun getLong(key: String?, defValue: Long): Long = values[key] as Long? ?: defValue
        override fun getFloat(key: String?, defValue: Float): Float = values[key] as Float? ?: defValue
        override fun getBoolean(key: String?, defValue: Boolean): Boolean = values[key] as Boolean? ?: defValue
        override fun contains(key: String?): Boolean = values.containsKey(key)
        override fun edit(): SharedPreferences.Editor = Editor()
        override fun registerOnSharedPreferenceChangeListener(
            listener: SharedPreferences.OnSharedPreferenceChangeListener?
        ) = Unit
        override fun unregisterOnSharedPreferenceChangeListener(
            listener: SharedPreferences.OnSharedPreferenceChangeListener?
        ) = Unit

        private inner class Editor : SharedPreferences.Editor {
            private val pending = HashMap<String, Any?>()
            private val removed = HashSet<String>()
            private var clearAll = false

            private fun put(key: String?, value: Any?): SharedPreferences.Editor {
                pending[requireNotNull(key)] = value
                return this
            }

            override fun putString(key: String?, value: String?) = put(key, value)
            override fun putStringSet(key: String?, values: MutableSet<String>?) = put(key, values?.toSet())
            override fun putInt(key: String?, value: Int) = put(key, value)
            override fun putLong(key: String?, value: Long) = put(key, value)
            override fun putFloat(key: String?, value: Float) = put(key, value)
            override fun putBoolean(key: String?, value: Boolean) = put(key, value)

            override fun remove(key: String?): SharedPreferences.Editor {
                removed += requireNotNull(key)
                return this
            }

            override fun clear(): SharedPreferences.Editor {
                clearAll = true
                return this
            }

            override fun commit(): Boolean {
                apply()
                return true
            }

            override fun apply() {
                if (clearAll) values.clear()
                removed.forEach(values::remove)
                values.putAll(pending)
            }
        }
    }
}
