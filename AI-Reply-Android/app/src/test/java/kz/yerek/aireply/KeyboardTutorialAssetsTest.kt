package kz.yerek.aireply

import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.ui.feature.setup.KeyboardTutorialAssets
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File
import java.security.MessageDigest
import java.nio.ByteBuffer
import javax.xml.parsers.DocumentBuilderFactory

/** Prevent a missing locale recording or a single reused file silently shipping in all languages. */
class KeyboardTutorialAssetsTest {
    @Test
    fun `each app language bundles a distinct compact AVC tutorial`() {
        val hashes = AppLanguage.entries.map { language ->
            val file = File("src/main/res/raw", KeyboardTutorialAssets.assetName(language))
            assertTrue("Missing ${language.code} tutorial", file.isFile)
            val bytes = file.readBytes()
            assertTrue("${language.code} file size", bytes.size in 100_000 until 6_000_000)
            assertEquals("${language.code} MP4 signature", "ftyp", bytes.copyOfRange(4, 8).toString(Charsets.US_ASCII))
            assertTrue("${language.code} AVC video", bytes.toString(Charsets.ISO_8859_1).contains("avc1"))
            MessageDigest.getInstance("SHA-256").digest(bytes).toList()
        }
        assertEquals("Each recording must show its own localized interface", AppLanguage.entries.size, hashes.toSet().size)
        assertEquals(AppLanguage.entries.size, AppLanguage.entries.map(KeyboardTutorialAssets::resource).toSet().size)
    }

    @Test
    fun `all recordings have one portrait video track and no audio`() {
        AppLanguage.entries.forEach { language ->
            val bytes = File("src/main/res/raw", KeyboardTutorialAssets.assetName(language)).readBytes()
            val root = bytes.boxes(0, bytes.size)
            val movie = root.single { it.type == "moov" }
            assertTrue("${language.code} fast start", movie.end <= root.single { it.type == "mdat" }.payload)
            val tracks = bytes.boxes(movie).filter { it.type == "trak" }
            assertEquals("${language.code} only video, no audio", 1, tracks.size)
            val track = tracks.single()
            val media = bytes.boxes(track).single { it.type == "mdia" }
            val handler = bytes.boxes(media).single { it.type == "hdlr" }
            assertEquals("${language.code} video handler", "vide", bytes.text(handler.payload + 8, 4))
            val header = bytes.boxes(track).single { it.type == "tkhd" }
            val width = bytes.uint32(header.end - 8) / 65_536.0
            val height = bytes.uint32(header.end - 4) / 65_536.0
            assertTrue("${language.code} portrait dimensions", width >= 360 && height > width)
            val timing = bytes.boxes(media).single { it.type == "mdhd" }
            val version = bytes[timing.payload].toInt()
            val timescale = bytes.uint32(timing.payload + if (version == 1) 20 else 12)
            val duration = if (version == 1) ByteBuffer.wrap(bytes, timing.payload + 24, 8).long.toDouble()
                else bytes.uint32(timing.payload + 16).toDouble()
            val seconds = duration / timescale
            assertTrue("${language.code} expected a 20–90 second guide, got $seconds", seconds in 20.0..90.1)
        }
    }

    @Test
    fun `every guide and caption is explicitly translated in all four languages`() {
        fun strings(folder: String): Map<String, String> {
            val document = DocumentBuilderFactory.newInstance().newDocumentBuilder()
                .parse(File("src/main/res/$folder/strings_tutorials.xml"))
            val nodes = document.getElementsByTagName("string")
            return (0 until nodes.length).associate { index ->
                val element = nodes.item(index) as org.w3c.dom.Element
                element.getAttribute("name") to element.textContent.trim()
            }
        }
        val english = strings("values")
        listOf("ru", "kk", "uz").forEach { language ->
            val localized = strings("values-$language")
            assertEquals("$language guide is complete", english.keys, localized.keys)
            english.forEach { (key, value) ->
                assertTrue("$language $key is empty", localized.getValue(key).isNotBlank())
                assertTrue("$language $key uses English", localized.getValue(key) != value)
            }
        }
    }

    private data class Box(val type: String, val payload: Int, val end: Int)

    private fun ByteArray.boxes(parent: Box): List<Box> = boxes(parent.payload, parent.end)

    /** Read container boundaries so random compressed bytes cannot be mistaken for audio metadata. */
    private fun ByteArray.boxes(start: Int, end: Int): List<Box> {
        val result = mutableListOf<Box>()
        var offset = start
        while (offset < end) {
            require(end - offset >= 8) { "Truncated MP4 box" }
            val size = uint32(offset).let { if (it == 0L) (end - offset).toLong() else it }
            require(size in 8L..(end - offset).toLong()) { "Invalid MP4 box size $size" }
            val next = offset + size.toInt()
            result += Box(text(offset + 4, 4), offset + 8, next)
            offset = next
        }
        return result
    }

    private fun ByteArray.uint32(offset: Int): Long = ByteBuffer.wrap(this, offset, 4).int.toLong() and 0xffff_ffffL
    private fun ByteArray.text(offset: Int, size: Int): String = copyOfRange(offset, offset + size).toString(Charsets.US_ASCII)
}
