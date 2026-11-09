package kz.yerek.aireply

import kz.yerek.aireply.data.settings.SettingsStore
import kz.yerek.aireply.domain.model.ReplyConfiguration
import kz.yerek.aireply.domain.model.ReplyTemplate
import kz.yerek.aireply.keyboard.input.ContextTextProvider
import kz.yerek.aireply.ui.feature.compose.ComposePaste
import kz.yerek.aireply.ui.feature.compose.PasteNotice
import kz.yerek.aireply.ui.feature.compose.PasteOutcome
import kz.yerek.aireply.ui.feature.compose.initialRecipient
import kz.yerek.aireply.ui.feature.compose.recipientGroups
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * «Создать ответ»: the recipient is the keyboard's, and Paste either brings the
 * copied text or says why not - private clips are never opened.
 *
 * Жауап жасау: алушы пернетақтамен ортақ, қою себебін айтады.
 */
class CreateReplyTest {

    private class Clip(
        private val text: String? = "Добрый день! Можно узнать подробнее?",
        private val types: List<String> = listOf("text/plain"),
        private val sensitive: Boolean = false
    ) : ContextTextProvider.CopiedClip {
        var opened = false
        override fun hasMimeType(mimeType: String): Boolean = mimeType in types
        override fun isMarkedSensitive(): Boolean = sensitive
        override fun firstItem(): ContextTextProvider.CopiedClip.Item? {
            opened = true
            val value = text ?: return null
            return object : ContextTextProvider.CopiedClip.Item {
                override fun text(): CharSequence = value
                override fun htmlText(): String? = null
                override fun hasUri(): Boolean = false
                override fun readUri(limit: Int): String? = null
            }
        }
    }

    private val provider = ContextTextProvider(htmlText = { it })

    private fun paste(clip: Clip?): PasteOutcome? =
        ComposePaste.outcome(provider.read(isSecureField = false, selection = { null }, clip = { clip }))

    @Test
    fun `the keyboard's selected recipient is the one create starts with`() {
        val shown = listOf("friend", "client", "work", "business", "c0ffee00-custom")
        assertEquals("client", initialRecipient(shown, lastTemplateId = "client"))
        assertEquals("a custom one too", "c0ffee00-custom", initialRecipient(shown, "c0ffee00-custom"))
        assertEquals("hidden or deleted: the first one shown", "friend", initialRecipient(shown, "gone"))
        assertEquals("friend", initialRecipient(shown, null))
        assertNull(initialRecipient(emptyList(), "client"))
    }

    @Test
    fun `the built-ins go in the grid and the user's own under it, each still choosable`() {
        val custom = ReplyTemplate.custom("Шеф", sortIndex = 4)
        val shown = ReplyConfiguration.INITIAL.visibleTemplates + custom

        val (builtIns, own) = recipientGroups(shown)
        assertEquals(listOf("friend", "client", "work", "business"), builtIns.map { it.id })
        assertEquals(listOf(custom.id), own.map { it.id })
        assertEquals("still the keyboard's selection", custom.id, initialRecipient(shown.map { it.id }, custom.id))
    }

    @Test
    fun `the app and the keyboard keep the recipient in one key`() {
        val file = InMemoryPreferences()
        SettingsStore(file).lastTemplateId = "business"
        assertEquals("business", SettingsStore(file).lastTemplateId)
    }

    @Test
    fun `paste brings the copied text`() {
        assertEquals(PasteOutcome.Text("Добрый день! Можно узнать подробнее?"), paste(Clip()))
    }

    @Test
    fun `an empty clipboard and one without text say so`() {
        assertEquals(PasteOutcome.Notice(PasteNotice.EMPTY), paste(Clip(text = null)))
        assertEquals(PasteOutcome.Notice(PasteNotice.EMPTY), paste(Clip(types = listOf("image/png"))))
        assertEquals(PasteOutcome.Notice(PasteNotice.EMPTY), paste(Clip(text = "   ")))
    }

    @Test
    fun `a clip marked private is never opened`() {
        val secret = Clip(text = "1234 5678", sensitive = true)
        assertEquals(PasteOutcome.Notice(PasteNotice.PRIVATE), paste(secret))
        assertEquals(false, secret.opened)
    }

    @Test
    fun `no clipboard at all is unreadable, not empty`() {
        assertEquals(PasteOutcome.Notice(PasteNotice.UNREADABLE), paste(null))
    }

    /** The state is the screen's own; what it does on each way of bringing a message is pinned here. */
    @Test
    fun `a pasted, example or dictated message takes the old error away`() {
        val screen = File("src/main/java/kz/yerek/aireply/ui/feature/compose/ComposeScreen.kt").readText()
        fun clears(after: String) =
            assertTrue(after, "forgetError()" in screen.substringAfter(after).take(160))

        clears("message = outcome.text")
        clears("message = context.getString(R.string.compose_example_message)")
        clears("onDismiss = { dictatingMessage = false },")
    }
}
