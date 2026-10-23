package kz.yerek.aireply

import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.ui.feature.setup.KeyboardTutorialAssets
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File
import java.security.MessageDigest

/** Prevent a missing locale recording or a single reused file silently shipping in all languages. */
class KeyboardTutorialAssetsTest {
    @Test
    fun `each app language bundles a distinct compact AVC tutorial`() {
        val hashes = AppLanguage.entries.map { language ->
            val file = File("src/main/res/raw", KeyboardTutorialAssets.assetName(language))
            assertTrue("Missing ${language.code} tutorial", file.isFile)
            val bytes = file.readBytes()
            assertTrue("${language.code} file size", bytes.size in 100_000 until 2_000_000)
            assertEquals("${language.code} MP4 signature", "ftyp", bytes.copyOfRange(4, 8).toString(Charsets.US_ASCII))
            assertTrue("${language.code} AVC video", bytes.toString(Charsets.ISO_8859_1).contains("avc1"))
            MessageDigest.getInstance("SHA-256").digest(bytes).toList()
        }
        assertEquals("Each recording must show its own localized interface", AppLanguage.entries.size, hashes.toSet().size)
        assertEquals(AppLanguage.entries.size, AppLanguage.entries.map(KeyboardTutorialAssets::resource).toSet().size)
    }
}
