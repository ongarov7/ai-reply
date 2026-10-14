package kz.yerek.aireply

import kz.yerek.aireply.voice.VoiceFiles
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File

/**
 * Recordings left behind by a keyboard that was killed mid-upload are swept
 * away when it starts again - and nothing else is touched.
 */
class VoiceFilesTest {

    @get:Rule
    val folder = TemporaryFolder()

    private val now = 1_800_000_000_000L
    private val hour = 60 * 60 * 1000L

    @Test
    fun `old recordings go, fresh ones and other files stay`() {
        val dir = folder.newFolder("voice")
        val oldM4a = file(dir, "voice-a.m4a", ageMs = 2 * hour)
        val oldWav = file(dir, "voice-b.wav", ageMs = hour)
        val fresh = file(dir, "voice-c.m4a", ageMs = hour - 1_000)
        val other = file(dir, "notes.txt", ageMs = 5 * hour)

        assertEquals(2, VoiceFiles.sweep(dir, nowMs = now))
        assertFalse(oldM4a.exists())
        assertFalse(oldWav.exists())
        assertTrue(fresh.exists())
        assertTrue(other.exists())
    }

    @Test
    fun `the age is the caller's to choose`() {
        val dir = folder.newFolder("voice")
        val recent = file(dir, "voice-a.m4a", ageMs = 10_000)
        assertEquals(0, VoiceFiles.sweep(dir, nowMs = now, olderThanMs = 20_000))
        assertTrue(recent.exists())
        assertEquals(1, VoiceFiles.sweep(dir, nowMs = now, olderThanMs = 5_000))
        assertFalse(recent.exists())
    }

    @Test
    fun `a folder of the same name is left alone`() {
        val dir = folder.newFolder("voice")
        val nested = File(dir, "voice-folder").apply { mkdirs() }
        nested.setLastModified(now - 3 * hour)
        assertEquals(0, VoiceFiles.sweep(dir, nowMs = now))
        assertTrue(nested.isDirectory)
    }

    @Test
    fun `a missing folder or a file instead of one never throws`() {
        assertEquals(0, VoiceFiles.sweep(File(folder.root, "missing"), nowMs = now))
        assertEquals(0, VoiceFiles.sweep(folder.newFile("plain"), nowMs = now))
    }

    @Test
    fun `new files are named voice-uuid with the extension`() {
        val dir = folder.newFolder("voice")
        val a = VoiceFiles.newFile(dir, "m4a")
        val b = VoiceFiles.newFile(dir, "m4a")
        assertEquals(dir, a.parentFile)
        assertTrue(a.name.matches(Regex("voice-[0-9a-f-]{36}\\.m4a")))
        assertTrue(a.name != b.name)
    }

    private fun file(dir: File, name: String, ageMs: Long): File =
        File(dir, name).apply {
            writeBytes(ByteArray(8))
            setLastModified(now - ageMs)
        }
}
