package kz.yerek.aireply

import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.data.settings.DeviceStateStore
import kz.yerek.aireply.keyboard.input.ClipboardMiss
import kz.yerek.aireply.keyboard.input.ContextTextProvider
import kz.yerek.aireply.keyboard.input.ReplyContext
import kz.yerek.aireply.keyboard.input.ReplyContextSource
import kz.yerek.aireply.keyboard.reply.KeyboardPanels.CopiedReply
import kz.yerek.aireply.keyboard.reply.ReplyComposerFlow
import kz.yerek.aireply.keyboard.ui.isClipboardNotice
import kz.yerek.aireply.keyboard.ui.offersRetry
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File
import javax.xml.parsers.DocumentBuilderFactory

/**
 * What the keyboard says when nothing usable came off the clipboard (spec
 * 2.4): a clip marked private is not "copy a message first", none of these
 * notes is a red failure with Try again, a clip whose text is behind another
 * app's provider is read without the keys waiting, and only the times of a
 * working paste and of the keyboard in use are kept.
 *
 * Буфер туралы ескертулер: құпия мәтін бөлек айтылады, «Қайталау» жоқ.
 */
class ClipboardNoticeTest {

    private val h = PanelsHarness()
    private val copied = "Добрый день! Когда сможете приехать?"

    private fun success(text: String = copied) =
        ContextTextProvider.Result.Success(ReplyContext(text, ReplyContextSource.CLIPBOARD))

    private val private = ContextTextProvider.Result.Failure(AIReplyError.ClipboardUnavailable, ClipboardMiss.MARKED_PRIVATE)

    // ------------------------------------------------------------- notices

    @Test
    fun `clipboard notes are grey and never offer Try again, failed requests do`() {
        assertTrue(AIReplyError.NoSourceMessage.isClipboardNotice)
        assertTrue(AIReplyError.ClipboardUnavailable.isClipboardNotice)
        assertFalse(AIReplyError.NoSourceMessage.offersRetry)
        assertFalse(AIReplyError.ClipboardUnavailable.offersRetry)
        assertTrue(AIReplyError.Offline.offersRetry)
        assertTrue(AIReplyError.ServiceUnavailable.offersRetry)
        assertFalse(AIReplyError.Offline.isClipboardNotice)
    }

    @Test
    fun `a failure names its miss, or the miss its error stands for`() {
        assertEquals(ClipboardMiss.NOTHING_COPIED, ContextTextProvider.Result.Failure(AIReplyError.NoSourceMessage).miss)
        assertEquals(ClipboardMiss.UNREADABLE, ContextTextProvider.Result.Failure(AIReplyError.ClipboardUnavailable).miss)
        assertEquals(ClipboardMiss.MARKED_PRIVATE, private.miss)
    }

    @Test
    fun `a private clip opens the composer with its own note`() {
        h.panels.openReply(h.friend) { private }
        val session = h.replies.session!!
        assertEquals("", session.source.text)
        assertEquals(AIReplyError.ClipboardUnavailable, session.flow.error)
        assertEquals(ClipboardMiss.MARKED_PRIVATE, session.clipboardMiss)
        assertEquals("nothing is waiting to be retried", ReplyComposerFlow.Stage.Composing, session.flow.stage)
    }

    @Test
    fun `an empty clipboard opens the composer quietly`() {
        h.panels.openReply(h.friend) { ContextTextProvider.Result.Failure(AIReplyError.NoSourceMessage) }
        val session = h.replies.session!!
        assertNull(session.flow.error)
        assertNull(session.clipboardMiss)
    }

    @Test
    fun `pasting after a private clip clears the note`() {
        h.panels.openReply(h.friend) { private }
        h.replies.replaceSource(copied)
        val session = h.replies.session!!
        assertNull(session.flow.error)
        assertNull(session.clipboardMiss)
        assertEquals(copied, session.source.text)
    }

    @Test
    fun `an explicit Paste with nothing copied says so, and the source stays`() {
        h.openReply(message = copied)
        h.replies.showError(AIReplyError.NoSourceMessage, ClipboardMiss.NOTHING_COPIED)
        val session = h.replies.session!!
        assertEquals(copied, session.source.text)
        assertEquals(AIReplyError.NoSourceMessage, session.flow.error)
        assertEquals(ClipboardMiss.NOTHING_COPIED, session.clipboardMiss)
        assertFalse(session.flow.error!!.offersRetry)
    }

    @Test
    fun `Reply to copied with a private clip keeps Create and says why`() {
        h.openCreate("Поблагодари")
        val outcome = h.panels.replyToCopied(h.friend, secureField = false) { private }
        assertEquals(CopiedReply.NothingCopied(AIReplyError.ClipboardUnavailable), outcome)
        val created = h.compose.session!!
        assertEquals(ClipboardMiss.MARKED_PRIVATE, created.clipboardMiss)
        assertEquals("Поблагодари", created.instruction.text)
        assertNull(h.replies.session)
    }

    // ---------------------------------------------------------- late reads

    @Test
    fun `text behind a provider opens the composer at once and lands when read`() {
        var reads = 0
        val pending = ContextTextProvider.Result.Deferred { reads++; success() }
        val returned = h.panels.openReply(h.friend) { pending }
        assertSame(pending, returned)
        assertEquals("not read on the tap", 0, reads)
        val session = h.replies.session!!
        assertEquals("", session.source.text)
        assertNull(session.flow.error)

        h.panels.lateSource(session, returned!!.read(), replacing = false)
        assertEquals(1, reads)
        assertEquals(copied, session.source.text)
        assertTrue(h.replyRequests.isEmpty())
    }

    @Test
    fun `a late read never overwrites what the user typed, nor reaches another session`() {
        val pending = ContextTextProvider.Result.Deferred { success() }
        h.panels.openReply(h.friend) { pending }
        val first = h.replies.session!!
        first.source.set("набрано вручную")
        h.panels.lateSource(first, pending.read(), replacing = false)
        assertEquals("набрано вручную", first.source.text)

        // Closed and opened again: the old read is about the old session.
        h.panels.closeAll()
        h.panels.openReply(h.friend) { ContextTextProvider.Result.Failure(AIReplyError.NoSourceMessage) }
        h.panels.lateSource(first, pending.read(), replacing = false)
        assertEquals("", h.replies.session!!.source.text)
    }

    @Test
    fun `a late Paste replaces the message, a late failure only says so`() {
        h.openReply(message = "старое")
        val session = h.replies.session!!
        h.panels.lateSource(session, success(), replacing = true)
        assertEquals(copied, session.source.text)

        h.panels.lateSource(session, ContextTextProvider.Result.Failure(AIReplyError.ClipboardUnavailable, ClipboardMiss.UNREADABLE), replacing = true)
        assertEquals(copied, session.source.text)
        assertEquals(ClipboardMiss.UNREADABLE, session.clipboardMiss)
    }

    @Test
    fun `Reply to copied waits for a late read, then carries the request`() {
        h.openCreate("Скажи, что приеду завтра")
        val pending = ContextTextProvider.Result.Deferred { success() }
        val outcome = h.panels.replyToCopied(h.friend, secureField = false) { pending }
        assertTrue(outcome is CopiedReply.Reading)
        assertNotNull("Create stays while the text is read", h.compose.session)
        assertNull(h.compose.session!!.flow.error)

        val late = (outcome as CopiedReply.Reading).read.read()
        assertEquals(CopiedReply.Opened, h.panels.replyToCopied(h.friend, secureField = false) { late })
        assertEquals(copied, h.replies.session!!.source.text)
        assertEquals("Скажи, что приеду завтра", h.replies.session!!.instruction.text)
    }

    // ------------------------------------------------------------ evidence

    @Test
    fun `the paste time is kept, and only the time`() {
        val file = InMemoryPreferences()
        val store = DeviceStateStore(file)
        assertNull(store.lastSuccessfulPasteAt)
        store.notePaste(1_760_000_000_000L)
        assertEquals(1_760_000_000_000L, DeviceStateStore(file).lastSuccessfulPasteAt)
        assertTrue("one value, a number", file.all.values.all { it is Long })
    }

    @Test
    fun `the keyboard's last appearance is kept to ten minutes`() {
        val store = DeviceStateStore(InMemoryPreferences())
        assertNull(store.keyboardLastShownAt)
        val start = 1_760_000_000_000L
        assertTrue(store.noteKeyboardShown(start))
        assertFalse("shown again a minute later", store.noteKeyboardShown(start + 60_000L))
        assertEquals(start, store.keyboardLastShownAt)
        assertTrue(store.noteKeyboardShown(start + DeviceStateStore.KEYBOARD_SHOWN_RESOLUTION_MS))
        assertEquals(start + DeviceStateStore.KEYBOARD_SHOWN_RESOLUTION_MS, store.keyboardLastShownAt)
        // The clock went back: the newer value is not trusted.
        assertTrue(store.noteKeyboardShown(start))
        assertEquals(start, store.keyboardLastShownAt)
    }

    @Test
    fun `the evidence stays on this phone`() {
        val backup = excluded(File("src/main/res/xml/backup_rules.xml"), "full-backup-content")
        val cloud = excluded(File("src/main/res/xml/data_extraction_rules.xml"), "cloud-backup")
        val transfer = excluded(File("src/main/res/xml/data_extraction_rules.xml"), "device-transfer")
        listOf(backup, cloud, transfer).forEach { assertTrue(it.toString(), "aireply_device.xml" in it) }
    }

    @Test
    fun `the keyboard reads the clipboard in one place only`() {
        val readers = File("src/main/java/kz/yerek/aireply/keyboard").walkTopDown()
            .filter { it.isFile && it.extension == "kt" }
            .filter { file -> CLIPBOARD_CALLS.any { it in file.readText() } }
            .map { it.name }
            .toList()
        assertEquals(listOf("ContextTextProvider.kt"), readers)
    }

    private fun excluded(file: File, section: String): Set<String> {
        val document = DocumentBuilderFactory.newInstance().newDocumentBuilder().parse(file)
        val root = document.getElementsByTagName(section).item(0) as org.w3c.dom.Element
        val nodes = root.getElementsByTagName("exclude")
        return (0 until nodes.length).map { (nodes.item(it) as org.w3c.dom.Element).getAttribute("path") }.toSet()
    }

    private companion object {
        val CLIPBOARD_CALLS = listOf("ClipboardManager", "primaryClip", "CLIPBOARD_SERVICE")
    }
}
