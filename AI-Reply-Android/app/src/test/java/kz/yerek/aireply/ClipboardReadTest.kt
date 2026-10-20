package kz.yerek.aireply

import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.keyboard.input.ClipboardMiss
import kz.yerek.aireply.keyboard.input.ContextTextProvider
import kz.yerek.aireply.keyboard.input.ContextTextProvider.Result
import kz.yerek.aireply.keyboard.input.ReplyContext
import kz.yerek.aireply.keyboard.input.ReplyContextSource
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * What a tap finds on the clipboard (spec 2.4), decided without a phone: a
 * password field and a clip marked private are never opened, the text a clip
 * carries is read at once, and text that is only HTML or behind another app's
 * provider is read later, capped.
 *
 * Буферді оқу шешімдері: құпия клип ашылмайды, баяу оқу кейінге қалады.
 */
class ClipboardReadTest {

    private class Item(
        val text: CharSequence? = null,
        val html: String? = null,
        /** Text behind the content URI; null with a URI means "not readable text". */
        val content: String? = null,
        val uri: Boolean = content != null,
        val failing: Boolean = false
    ) : ContextTextProvider.CopiedClip.Item {
        var uriReads = 0
        var limit = -1
        override fun text(): CharSequence? = text
        override fun htmlText(): String? = html
        override fun hasUri(): Boolean = uri
        override fun readUri(limit: Int): String? {
            uriReads++
            this.limit = limit
            if (failing) throw SecurityException("provider gone")
            // More than asked for: the cap must not depend on the reader.
            return content
        }
    }

    private class Clip(
        val types: List<String> = listOf("text/plain"),
        val sensitive: () -> Boolean = { false },
        val item: () -> Item? = { Item(text = "Добрый день!") }
    ) : ContextTextProvider.CopiedClip {
        var itemReads = 0
        override fun hasMimeType(mimeType: String): Boolean = mimeType in types
        override fun isMarkedSensitive(): Boolean = sensitive()
        override fun firstItem(): ContextTextProvider.CopiedClip.Item? {
            itemReads++
            return item()
        }
    }

    private var htmlIn = 0
    private val provider = ContextTextProvider(htmlText = { html ->
        htmlIn = html.length
        html.replace(Regex("<[^>]*>"), "")
    })

    private fun read(clip: Clip?, secure: Boolean = false, selection: CharSequence? = null): Result =
        provider.read(secure, { selection }, { clip })

    private fun copied(text: String) = Result.Success(ReplyContext(text, ReplyContextSource.CLIPBOARD))
    private val nothing = Result.Failure(AIReplyError.NoSourceMessage, ClipboardMiss.NOTHING_COPIED)
    private val unreadable = Result.Failure(AIReplyError.ClipboardUnavailable, ClipboardMiss.UNREADABLE)

    private fun Result.late(): Result = (this as Result.Deferred).read()

    // ------------------------------------------------------------- refusals

    @Test
    fun `a password field refuses before anything is looked at`() {
        var looked = 0
        val result = provider.read(true, { looked++; "выделено" }, { looked++; Clip() })
        assertEquals(Result.Failure(AIReplyError.NoSourceMessage, ClipboardMiss.SECURE_FIELD), result)
        assertEquals(0, looked)
    }

    @Test
    fun `a selection in the field wins, and the clipboard is not consulted`() {
        var consulted = 0
        val result = provider.read(false, { "  Когда приедете?  " }, { consulted++; Clip() })
        assertEquals(Result.Success(ReplyContext("Когда приедете?", ReplyContextSource.EDITABLE_SELECTION)), result)
        assertEquals(0, consulted)
        assertEquals("a blank selection is no selection", copied("Добрый день!"), read(Clip(), selection = "   "))
    }

    @Test
    fun `no clipboard at all is unreadable, not empty`() {
        assertEquals(unreadable, read(null))
    }

    @Test
    fun `nothing copied, or nothing that is text, is copy a message first`() {
        for (types in listOf(emptyList(), listOf("image/png"), listOf("text/uri-list"))) {
            val clip = Clip(types = types)
            assertEquals("$types", nothing, read(clip))
            assertEquals("$types: the item is not opened", 0, clip.itemReads)
        }
        assertEquals("HTML is text too", copied("Добрый день!"), read(Clip(types = listOf("text/html"))))
    }

    @Test
    fun `a clip marked private is said as private and never opened`() {
        val clip = Clip(sensitive = { true })
        assertEquals(Result.Failure(AIReplyError.ClipboardUnavailable, ClipboardMiss.MARKED_PRIVATE), read(clip))
        assertEquals(0, clip.itemReads)
    }

    @Test
    fun `a private flag that cannot be read counts as none`() {
        assertEquals(copied("Добрый день!"), read(Clip(sensitive = { throw SecurityException("extras") })))
    }

    // --------------------------------------------------------------- inline

    @Test
    fun `the text a clip carries is read at once, trimmed, and nothing else is opened`() {
        val item = Item(text = "\n  Добрый день!  ", html = "<b>другое</b>", content = "из провайдера")
        assertEquals(copied("Добрый день!"), read(Clip(item = { item })))
        assertEquals(0, item.uriReads)
        assertEquals(0, htmlIn)
    }

    @Test
    fun `blank text, no text and no item are all nothing copied`() {
        assertEquals(nothing, read(Clip(item = { Item(text = "  \n ") })))
        assertEquals(nothing, read(Clip(item = { Item() })))
        assertEquals(nothing, read(Clip(item = { null })))
        assertEquals("an item that cannot be read is no item", nothing, read(Clip(item = { throw IllegalStateException() })))
    }

    @Test
    fun `inline text is not cut, the composer's counter says it is too long`() {
        val long = "а".repeat(ContextTextProvider.MAX_DEFERRED_CHARACTERS + 500)
        assertEquals(copied(long), read(Clip(item = { Item(text = long) })))
    }

    // ----------------------------------------------------------------- late

    @Test
    fun `an HTML-only clip is read later, as its text`() {
        val result = read(Clip(types = listOf("text/html"), item = { Item(html = "<p>Добрый <b>день</b>!</p>") }))
        assertTrue(result is Result.Deferred)
        assertEquals("not parsed on the tap", 0, htmlIn)
        assertEquals(copied("Добрый день!"), result.late())
    }

    @Test
    fun `HTML is capped going in and coming out`() {
        val markup = "<p>" + "д".repeat(ContextTextProvider.MAX_DEFERRED_CHARACTERS * 5) + "</p>"
        val late = read(Clip(item = { Item(html = markup) })).late() as Result.Success
        assertTrue("$htmlIn", htmlIn <= ContextTextProvider.MAX_DEFERRED_CHARACTERS * 4)
        assertEquals(ContextTextProvider.MAX_DEFERRED_CHARACTERS, late.context.text.length)
    }

    @Test
    fun `markup that does not parse is unreadable`() {
        val failing = ContextTextProvider(htmlText = { throw IllegalArgumentException() })
        val clip = Clip(item = { Item(html = "<p>") })
        assertEquals(unreadable, (failing.read(false, { null }, { clip }) as Result.Deferred).read())
        val empty = ContextTextProvider(htmlText = { null })
        assertEquals(unreadable, (empty.read(false, { null }, { clip }) as Result.Deferred).read())
    }

    @Test
    fun `text behind a provider is read later, at most 20,000 characters`() {
        val item = Item(content = "я".repeat(50_000))
        val result = read(Clip(item = { item }))
        assertTrue(result is Result.Deferred)
        assertEquals("the provider is not touched on the tap", 0, item.uriReads)
        val late = result.late() as Result.Success
        assertEquals(1, item.uriReads)
        assertEquals(20_000, ContextTextProvider.MAX_DEFERRED_CHARACTERS)
        assertEquals(ContextTextProvider.MAX_DEFERRED_CHARACTERS, item.limit)
        assertEquals(ContextTextProvider.MAX_DEFERRED_CHARACTERS, late.context.text.length)
        assertEquals(ReplyContextSource.CLIPBOARD, late.context.source)
    }

    @Test
    fun `a provider without readable text is unreadable, with blank text nothing copied`() {
        assertEquals(unreadable, read(Clip(item = { Item(uri = true) })).late())
        assertEquals(unreadable, read(Clip(item = { Item(content = "x", failing = true) })).late())
        assertEquals(nothing, read(Clip(item = { Item(content = "  \n") })).late())
    }
}
