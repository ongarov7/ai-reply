package kz.yerek.aireply

import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.keyboard.reply.ReplyComposerFlow
import kz.yerek.aireply.keyboard.reply.ReplyComposerFlow.ConflictChoice
import kz.yerek.aireply.keyboard.reply.ReplyComposerFlow.ConflictResolution
import kz.yerek.aireply.keyboard.reply.ReplyComposerFlow.InsertDecision
import kz.yerek.aireply.keyboard.reply.ReplyComposerFlow.Origin
import kz.yerek.aireply.keyboard.reply.ReplyComposerFlow.Stage
import kz.yerek.aireply.keyboard.reply.ReplyDraftHistory
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The composer's state machine: Generate, Edit, Regenerate, Insert, Stop,
 * failures - and the promise that no tap loses the user's work.
 */
class ReplyComposerFlowTest {

    private fun withReply(text: String = "Иә, ертең жеткіземіз 🚚"): ReplyComposerFlow =
        ReplyComposerFlow().startingGeneration()!!.receiving(text)

    @Test
    fun `generate shows the reply`() {
        val generating = ReplyComposerFlow().startingGeneration()!!
        assertEquals(Stage.Generating(Origin.COMPOSING), generating.stage)
        val done = generating.receiving("Hello")
        assertEquals(Stage.Result, done.stage)
        assertEquals("Hello", done.draftText)
    }

    @Test
    fun `only one request at a time`() {
        val generating = ReplyComposerFlow().startingGeneration()!!
        assertNull(generating.startingGeneration())
    }

    @Test
    fun `a failure goes back to where the request started and keeps the reply`() {
        val composing = ReplyComposerFlow().startingGeneration()!!.failing(AIReplyError.Offline)
        assertEquals(Stage.Composing, composing.stage)
        assertEquals(AIReplyError.Offline, composing.error)

        val regenerating = withReply().startingGeneration()!!.failing(AIReplyError.RateLimited)
        assertEquals(Stage.Result, regenerating.stage)
        assertEquals("Иә, ертең жеткіземіз 🚚", regenerating.draftText)
    }

    @Test
    fun `stop keeps everything and a late answer never lands`() {
        val stopped = withReply("First").startingGeneration()!!.cancellingGeneration()
        assertEquals(Stage.Result, stopped.stage)
        val late = stopped.receiving("Late")
        assertEquals(1, late.drafts.count)
        assertEquals("First", late.draftText)
    }

    /** The heart of it: Regenerate never destroys an edit. */
    @Test
    fun `regenerate adds a version and keeps the edited one`() {
        var flow = withReply("Сәлем! Ертең болады.")
        flow = flow.beginningEditingForTyping()!!.editingDraft("Сәлем, Айгүл! Ертең болады.").endingEditing()
        flow = flow.startingGeneration()!!
        assertEquals(Stage.Generating(Origin.RESULT), flow.stage)
        assertTrue("the reply stays on screen while the new one is made", flow.showsReply)
        flow = flow.receiving("Second version")
        assertEquals(2, flow.drafts.count)
        assertEquals("Second version", flow.draftText)
        flow = flow.showingPreviousVersion()
        assertEquals("Сәлем, Айгүл! Ертең болады.", flow.draftText)
        assertTrue(flow.drafts.current!!.isEdited)
    }

    @Test
    fun `typing on a reply starts editing, but not in the composer`() {
        assertEquals(Stage.Editing, withReply().beginningEditingForTyping()!!.stage)
        assertNull(ReplyComposerFlow().beginningEditingForTyping())
    }

    @Test
    fun `back keeps the versions and stops a regeneration`() {
        val back = withReply("A").goingBack()
        assertEquals(Stage.Composing, back.stage)
        val again = back.startingGeneration()!!.receiving("B")
        assertEquals(2, again.drafts.count)
        assertEquals(Stage.Composing, withReply().startingGeneration()!!.goingBack().stage)
    }

    @Test
    fun `insert takes the edited text exactly`() {
        val edited = "Қайырлы кеш! 😊\nЕртең 15:00-де."
        val flow = withReply("Original").beginningEditingForTyping()!!.editingDraft(edited)
        assertEquals(InsertDecision.Insert(edited), flow.requestingInsert(hostHasText = false).second)
    }

    @Test
    fun `a field with text asks first, and cancel returns exactly where the user was`() {
        val editing = withReply("Reply").beginningEditingForTyping()!!
        val (asking, decision) = editing.requestingInsert(hostHasText = true)
        assertEquals(InsertDecision.AskAboutExistingText, decision)
        assertEquals(Stage.Conflict(wasEditing = true), asking.stage)
        assertTrue(asking.showsReply)
        val (cancelled, resolution) = asking.resolvingConflict(ConflictChoice.CANCEL)
        assertEquals(ConflictResolution.Cancelled, resolution)
        assertEquals(Stage.Editing, cancelled.stage)

        assertEquals(ConflictResolution.Append("Reply"), asking.resolvingConflict(ConflictChoice.APPEND).second)
        assertEquals(ConflictResolution.Replace("Reply"), asking.resolvingConflict(ConflictChoice.REPLACE).second)
    }

    @Test
    fun `nothing to insert`() {
        assertEquals(InsertDecision.NothingToInsert, ReplyComposerFlow().requestingInsert(false).second)
        val blank = withReply("Text").beginningEditingForTyping()!!.editingDraft("   \n ")
        assertEquals(InsertDecision.NothingToInsert, blank.requestingInsert(false).second)
    }

    @Test
    fun `resuming after a persona change`() {
        val asking = withReply("A").requestingInsert(hostHasText = true).first
        assertEquals(Stage.Result, asking.resuming().stage)
        assertEquals(Stage.Composing, ReplyComposerFlow().startingGeneration()!!.resuming().stage)
    }

    @Test
    fun `the history drops the oldest unedited version when full`() {
        var history = ReplyDraftHistory().appending("0").editing("0 edited")
        for (index in 1..ReplyDraftHistory.CAPACITY) history = history.appending("$index")
        assertEquals(ReplyDraftHistory.CAPACITY, history.count)
        assertEquals("an edited version is kept", "0 edited", history.versions.first().text)
        assertEquals("${ReplyDraftHistory.CAPACITY}", history.currentText)
    }

    @Test
    fun `version navigation stops at the ends`() {
        var flow = withReply("A").startingGeneration()!!.receiving("B")
        flow = flow.showingNextVersion()
        assertEquals("B", flow.draftText)
        flow = flow.showingPreviousVersion().showingPreviousVersion()
        assertEquals("A", flow.draftText)
        assertEquals(1, flow.drafts.position)
    }
}
