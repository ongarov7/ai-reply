package kz.yerek.aireply

import kz.yerek.aireply.push.ClientContext
import kz.yerek.aireply.push.InstallationIdStore
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File

/**
 * The installation id: random, made once, never restored from a backup.
 *
 * Орнату идентификаторы бір рет жасалады, қайта орнатқанда — жаңасы.
 */
class InstallationIdTest {

    @get:Rule
    val folder = TemporaryFolder()

    @Test
    fun `the installation id is made once and kept`() {
        val first = InstallationIdStore { folder.root }.id()
        assertTrue(first, Regex("^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$").matches(first))
        assertTrue(InstallationIdStore.isValid(first))
        assertEquals("the same process", first, InstallationIdStore { folder.root }.id())
        assertEquals("the file is the source", first, File(folder.root, InstallationIdStore.FILE_NAME).readText())
    }

    @Test
    fun `a reinstall or a restore gets a new installation id`() {
        val original = InstallationIdStore { folder.newFolder("first") }.id()
        val restored = InstallationIdStore { folder.newFolder("second") }.id()
        assertNotEquals(original, restored)
    }

    @Test
    fun `a damaged id file is replaced by a valid id`() {
        val directory = folder.newFolder("damaged")
        File(directory, InstallationIdStore.FILE_NAME).writeText("not an id; rm -rf")
        val id = InstallationIdStore { directory }.id()
        assertTrue(InstallationIdStore.isValid(id))
        assertEquals(id, InstallationIdStore { directory }.id())
    }

    @Test
    fun `an unwritable directory still gives one stable id for the process`() {
        val missing = File(folder.newFile("plain-file"), "cannot-be-a-directory")
        val store = InstallationIdStore { missing }
        val id = store.id()
        assertTrue(InstallationIdStore.isValid(id))
        assertEquals(id, store.id())
    }

    @Test
    fun `versions keep only what the server stores`() {
        val context = ClientContext(
            installationIds = InstallationIdStore { folder.root },
            appVersion = " 1.3.2-beta (Ω) ",
            appBuild = "142",
            osVersion = "15",
            manufacturer = "samsung",
            deviceModel = "SM-S928B".repeat(10),
            language = { "kk" },
            timeZone = { "Asia/Almaty" }
        )
        assertEquals("1.3.2-beta ()", context.appVersion)
        assertEquals("142", context.appBuild)
        assertEquals(64, context.deviceModel.length)
        assertEquals("android", context.platform)
        assertEquals("Android", context.osName)
        assertEquals("kk", context.locale)
    }
}
