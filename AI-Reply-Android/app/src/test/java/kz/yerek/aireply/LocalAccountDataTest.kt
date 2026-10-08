package kz.yerek.aireply

import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.data.settings.SettingsStore
import kz.yerek.aireply.keyboard.autocorrect.AppliedCorrection
import kz.yerek.aireply.keyboard.autocorrect.LearnedWords
import kz.yerek.aireply.keyboard.autocorrect.LearnedWordsStore
import kz.yerek.aireply.keyboard.autocorrect.PrefsLearnedWordsStore
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * What this phone keeps for an account: the session's values in a file that
 * backups leave out (moved there from where older builds kept them), and the
 * learned words, which go when the account is deleted.
 *
 * Аккаунт деректері: сақтық көшірмеге түспейді, жойылғанда тазаланады.
 */
class LocalAccountDataTest {

    @Test
    fun `account values written by an older build move to the account file`() {
        val settings = InMemoryPreferences()
        val account = InMemoryPreferences()
        settings.edit()
            .putString("account.identifier", "a***@mail.kz")
            .putString("account.deviceId", "device-42")
            .putLong("account.accessTokenExpiry", 1_800_000_000_000L)
            .putInt("account.usage.dailyLimit", 20)
            .putString("account.usage.planCode", "free")
            .putString("shared.appLanguage", "kk")
            .apply()

        val store = SettingsStore(settings, account)

        assertEquals("a***@mail.kz", store.accountIdentifier)
        assertEquals("device-42", store.accountDeviceId)
        assertEquals(1_800_000_000_000L, store.accessTokenExpiry)
        assertEquals(20, store.cachedDailyLimit)
        assertEquals("free", store.cachedPlanCode)
        assertEquals("device-42", account.getString("account.deviceId", null))
        assertFalse("gone from the backed-up file", settings.all.keys.any { it.startsWith("account.") })
        assertEquals("other settings stay where they are", "kk", settings.getString("shared.appLanguage", null))
    }

    @Test
    fun `a value already in the account file wins over an old copy`() {
        val settings = InMemoryPreferences()
        val account = InMemoryPreferences()
        settings.edit().putString("account.deviceId", "old").apply()
        account.edit().putString("account.deviceId", "current").apply()

        assertEquals("current", SettingsStore(settings, account).accountDeviceId)
        assertFalse(settings.contains("account.deviceId"))
    }

    @Test
    fun `new values go only to the account file`() {
        val settings = InMemoryPreferences()
        val account = InMemoryPreferences()
        val store = SettingsStore(settings, account)
        store.accountIdentifier = "user@mail.kz"
        store.cachedRemainingToday = 3

        assertEquals("user@mail.kz", account.getString("account.identifier", null))
        assertEquals(3, account.getInt("account.usage.remainingToday", 0))
        assertTrue(settings.all.isEmpty())

        store.clearUsageCache()
        assertFalse(account.contains("account.usage.remainingToday"))
        assertEquals("the session stays", "user@mail.kz", store.accountIdentifier)

        store.clearAccountState()
        assertNull(store.accountIdentifier)
    }

    // --------------------------------------------------------- learned words

    @Test
    fun `clearing the learned words empties every layout`() {
        val prefs = InMemoryPreferences()
        val store = PrefsLearnedWordsStore(prefs)
        KeyboardLanguage.entries.forEach { store.write(it, LearnedWords.EMPTY.adding("ерек").encoded()) }
        prefs.edit().putString("unrelated", "kept").apply()
        val before = store.generation

        store.clear()

        KeyboardLanguage.entries.forEach { assertNull(store.read(it)) }
        assertEquals("kept", prefs.getString("unrelated", null))
        assertTrue("the keyboard's copy in memory is out of date now", store.generation != before)
    }

    @Test
    fun `the keyboard forgets learned words the app cleared, and never writes them back`() {
        val store = MemoryLearnedWordsStore()
        val engine = AutocorrectTestDictionaries.engine(store)
        val russian = KeyboardLanguage.RUSSIAN
        engine.undo(AppliedCorrection("сегодян", "сегодня", " ", russian), learn = true)
        assertTrue(engine.isKnown("сегодян", russian))

        // The app deletes the account while the keyboard's engine is alive.
        store.clear()

        assertFalse(engine.isKnown("сегодян", russian))
        engine.learn("ерекше", russian)
        assertEquals(listOf("ерекше"), LearnedWords.decode(store.values[russian]).words)
        assertEquals("autocorrect.learned.ru", LearnedWordsStore.key(russian))
    }
}
