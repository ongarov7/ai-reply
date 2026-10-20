package kz.yerek.aireply

import kz.yerek.aireply.keyboard.emoji.EmojiCatalog
import kz.yerek.aireply.keyboard.emoji.EmojiCatalogCache
import kz.yerek.aireply.keyboard.emoji.EmojiCatalogParser
import kz.yerek.aireply.keyboard.emoji.EmojiCategory
import kz.yerek.aireply.keyboard.emoji.EmojiGridLayout
import kz.yerek.aireply.keyboard.emoji.EmojiPageModel
import kz.yerek.aireply.keyboard.emoji.EmojiRecents
import kz.yerek.aireply.keyboard.emoji.EmojiSource
import kz.yerek.aireply.keyboard.emoji.EmojiTab
import kz.yerek.aireply.keyboard.emoji.PrefsEmojiRecentsStore
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File
import java.io.IOException
import java.text.BreakIterator

/**
 * The emoji page's data: the shared catalog as this keyboard reads it, the
 * page built from it with the recent emoji, and the recents themselves.
 *
 * Эмодзи каталогы, беттің құрылымы және соңғы эмодзилер.
 */
class EmojiCatalogTest {

    private val shipped by lazy { EmojiCatalogParser.parse(File("src/main/assets/emoji/emoji-catalog.txt").readText()) }

    private fun graphemes(text: String): Int {
        val iterator = BreakIterator.getCharacterInstance().also { it.setText(text) }
        var count = 0
        while (iterator.next() != BreakIterator.DONE) count++
        return count
    }

    // ----------------------------------------------------------- the file

    @Test
    fun `the shipped catalog has every category, in order`() {
        assertEquals(EmojiCategory.entries, shipped.groups.map { it.category })
        assertTrue("${shipped.size}", shipped.size > 1_800)
        assertTrue(shipped.groups.all { it.entries.isNotEmpty() })
    }

    @Test
    fun `every entry is one emoji, once, in the default skin tone`() {
        val all = shipped.groups.flatMap { group -> group.entries.map { it.emoji } }
        assertEquals("no duplicates", all.size, all.toSet().size)
        all.forEach { emoji ->
            assertEquals(emoji, 1, graphemes(emoji))
            assertFalse("$emoji has a skin tone", emoji.codePoints().anyMatch { it in 0x1F3FB..0x1F3FF })
        }
        // A few the page must have, fully qualified.
        listOf("😀", "❤️", "👍", "🇰🇿", "1️⃣", "🏴󠁧󠁢󠁳󠁣󠁴󠁿").forEach {
            assertTrue(it, it in shipped.all)
        }
        assertFalse("❤ without VS16 is not an entry", "❤" in shipped.all)
    }

    @Test
    fun `every entry carries an Emoji version up to 15_1`() {
        shipped.groups.flatMap { it.entries }.forEach { entry ->
            val version = entry.version.toDoubleOrNull()
            assertTrue("${entry.emoji} ${entry.version}", version != null && version in 0.6..15.1)
        }
    }

    // ------------------------------------------------------------- parser

    @Test
    fun `the parser takes what it understands and skips the rest`() {
        val text = "😶\t1.0\r\n@smileys_emotion\r\n😀\t1.0\r\n\r\n😀\t1.0\n@future_category\n🫨\t15.0\n@flags\n🇰🇿\t2.0\n🏁\n"
        val catalog = EmojiCatalogParser.parse(text)
        assertEquals(listOf(EmojiCategory.SMILEYS_EMOTION, EmojiCategory.FLAGS), catalog.groups.map { it.category })
        assertEquals(listOf("😀"), catalog.groups[0].entries.map { it.emoji })
        assertEquals(listOf("🇰🇿", "🏁"), catalog.groups[1].entries.map { it.emoji })
        assertEquals("", catalog.groups[1].entries[1].version)
        assertEquals(EmojiCatalog.EMPTY.groups, EmojiCatalogParser.parse("").groups)
    }

    @Test
    fun `what the font cannot draw is left out, and an empty category with it`() {
        val catalog = EmojiCatalogParser.parse("@smileys_emotion\n😀\t1.0\n🫠\t14.0\n@flags\n🇨🇶\t16.0\n")
        val filtered = catalog.filtered { it != "🫠" && it != "🇨🇶" }
        assertEquals(listOf(EmojiCategory.SMILEYS_EMOTION), filtered.groups.map { it.category })
        assertEquals(listOf("😀"), filtered.groups.single().entries.map { it.emoji })
    }

    @Test
    fun `the catalog is read once, and a failed read is tried again`() {
        var opens = 0
        val failing = EmojiSource { opens++; throw IOException("no asset") }
        assertEquals(0, EmojiCatalogCache.load(failing) { true }.size)
        val working = EmojiSource { opens++; "@symbols\n✅\t0.6\n".byteInputStream() }
        val first = EmojiCatalogCache.load(working) { true }
        assertEquals(1, first.size)
        assertSame(first, EmojiCatalogCache.load(failing) { true })
        assertEquals(2, opens)
    }

    // --------------------------------------------------------------- page

    private val small = EmojiCatalogParser.parse(
        "@smileys_emotion\n😀\t1.0\n😂\t0.6\n@people_body\n👋\t0.6\n@animals_nature\n🐱\t0.6\n@flags\n🇰🇿\t2.0\n"
    )

    @Test
    fun `the page lists the recent emoji first, then every tab with a header`() {
        val page = EmojiPageModel.of(small, listOf("🐱", "😀"))
        assertEquals(listOf(EmojiTab.RECENT, EmojiTab.SMILEYS_PEOPLE, EmojiTab.ANIMALS_NATURE, EmojiTab.FLAGS), page.tabs)
        assertEquals(
            listOf("header:RECENT", "RECENT:🐱", "RECENT:😀", "header:SMILEYS_PEOPLE", "SMILEYS_PEOPLE:😀",
                "SMILEYS_PEOPLE:😂", "SMILEYS_PEOPLE:👋", "header:ANIMALS_NATURE", "ANIMALS_NATURE:🐱",
                "header:FLAGS", "FLAGS:🇰🇿"),
            page.items.map { it.key }
        )
        assertEquals("keys are unique", page.items.size, page.items.map { it.key }.toSet().size)
    }

    @Test
    fun `smileys and people share a tab, as on the system keyboards`() {
        assertEquals(listOf(EmojiCategory.SMILEYS_EMOTION, EmojiCategory.PEOPLE_BODY), EmojiTab.SMILEYS_PEOPLE.categories)
        val covered = EmojiTab.entries.flatMap { it.categories }
        assertEquals("every category has exactly one tab", EmojiCategory.entries, covered)
    }

    @Test
    fun `no recent emoji, no recent tab`() {
        val page = EmojiPageModel.of(small, emptyList())
        assertEquals(EmojiTab.SMILEYS_PEOPLE, page.tabs.first())
        assertNull(page.startOf(EmojiTab.RECENT))
    }

    @Test
    fun `a recent emoji the phone does not offer is left out`() {
        val page = EmojiPageModel.of(small, listOf("🫨", "😂"))
        assertEquals(listOf("RECENT:😂"), page.items.filter { it.key.startsWith("RECENT:") }.map { it.key })
    }

    @Test
    fun `the tabs follow the scroll and a tab leads to its header`() {
        val page = EmojiPageModel.of(small, listOf("🐱"))
        assertEquals(0, page.startOf(EmojiTab.RECENT))
        assertEquals(2, page.startOf(EmojiTab.SMILEYS_PEOPLE))
        assertEquals(EmojiTab.RECENT, page.tabAt(1))
        assertEquals(EmojiTab.SMILEYS_PEOPLE, page.tabAt(2))
        assertEquals(EmojiTab.SMILEYS_PEOPLE, page.tabAt(5))
        assertEquals(EmojiTab.FLAGS, page.tabAt(page.items.lastIndex))
        assertTrue(page.items[page.startOf(EmojiTab.FLAGS)!!] is EmojiPageModel.Header)
        assertNull(EmojiPageModel.EMPTY.tabAt(0))
    }

    @Test
    fun `the shipped page has all nine tabs with recents, eight without`() {
        assertEquals(EmojiTab.entries, EmojiPageModel.of(shipped, listOf("😀")).tabs)
        assertEquals(EmojiTab.entries - EmojiTab.RECENT, EmojiPageModel.of(shipped, emptyList()).tabs)
    }

    @Test
    fun `the grid has big enough cells on every phone`() {
        assertEquals(7, EmojiGridLayout.columns(314f))
        assertEquals(8, EmojiGridLayout.columns(354f))
        assertEquals(9, EmojiGridLayout.columns(387f))
        assertEquals(9, EmojiGridLayout.columns(405f))
        for (width in listOf(314f, 354f, 387f, 405f, 474f, 900f)) {
            val columns = EmojiGridLayout.columns(width)
            assertTrue("w=$width", width / columns >= EmojiGridLayout.MIN_CELL_WIDTH || columns == EmojiGridLayout.MIN_COLUMNS)
        }
    }

    // ------------------------------------------------------------ recents

    @Test
    fun `a tapped emoji goes to the front, once`() {
        val recents = EmojiRecents(PrefsEmojiRecentsStore(InMemoryPreferences()))
        recents.record("😀")
        recents.record("👍")
        recents.record("😀")
        assertEquals(listOf("😀", "👍"), recents.items)
    }

    @Test
    fun `thirty at most, the oldest dropped`() {
        val recents = EmojiRecents(PrefsEmojiRecentsStore(InMemoryPreferences()))
        val many = shipped.groups.first().entries.take(35).map { it.emoji }
        many.forEach(recents::record)
        assertEquals(30, EmojiRecents.MAX)
        assertEquals(many.takeLast(30).reversed(), recents.items)
    }

    @Test
    fun `the recents survive the keyboard, and junk in the file is ignored`() {
        val file = InMemoryPreferences()
        EmojiRecents(PrefsEmojiRecentsStore(file)).apply {
            record("🇰🇿")
            record("👨‍👩‍👧")
        }
        val again = EmojiRecents(PrefsEmojiRecentsStore(file)).also { it.load() }
        assertEquals(listOf("👨‍👩‍👧", "🇰🇿"), again.items)

        val junk = InMemoryPreferences()
        junk.edit().putString("emoji.recents", "😀\n\n   \n😀\n" + "x".repeat(200) + "\n👍").apply()
        assertEquals(listOf("😀", "👍"), EmojiRecents(PrefsEmojiRecentsStore(junk)).also { it.load() }.items)

        val empty = EmojiRecents(PrefsEmojiRecentsStore(InMemoryPreferences())).also { it.load() }
        assertTrue(empty.items.isEmpty())
    }

    @Test
    fun `the recents stay on this phone`() {
        val backup = File("src/main/res/xml/backup_rules.xml").readText()
        val extraction = File("src/main/res/xml/data_extraction_rules.xml").readText()
        assertTrue(backup.contains("path=\"aireply_emoji.xml\""))
        assertEquals("cloud backup and device transfer", 2, Regex("path=\"aireply_emoji.xml\"").findAll(extraction).count())
    }
}
