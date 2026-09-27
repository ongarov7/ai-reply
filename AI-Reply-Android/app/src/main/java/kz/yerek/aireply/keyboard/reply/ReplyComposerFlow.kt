package kz.yerek.aireply.keyboard.reply

import kz.yerek.aireply.ai.AIReplyError

/**
 * Every version of the reply one session has produced, and the one on screen.
 *
 * WHY IT EXISTS. Regenerate used to overwrite the draft in place, so a user
 * who had corrected a name in the first answer and then tapped Regenerate
 * "just to see" lost the correction. Now a regeneration ADDS a version: the
 * edited one is still there, one tap back, exactly as the user left it.
 *
 * Lives in memory only, for as long as the composer session does.
 */
data class ReplyDraftHistory(
    val versions: List<Version> = emptyList(),
    val index: Int = 0
) {
    data class Version(
        /** What the model returned. */
        val generated: String,
        /** What the user has made of it. Starts equal to [generated]. */
        val text: String = generated
    ) {
        val isEdited: Boolean get() = text != generated
    }

    val isEmpty: Boolean get() = versions.isEmpty()
    val count: Int get() = versions.size
    val current: Version? get() = versions.getOrNull(index)
    val currentText: String get() = current?.text.orEmpty()

    /** 1-based position for display ("2/3"). */
    val position: Int get() = if (isEmpty) 0 else index + 1

    val canSelectPrevious: Boolean get() = index > 0
    val canSelectNext: Boolean get() = index < versions.size - 1

    /** Adds a freshly generated reply and shows it. */
    fun appending(generated: String): ReplyDraftHistory {
        val grown = versions + Version(generated)
        val trimmed = if (grown.size > CAPACITY) {
            // Drop the oldest version the user has not touched; if every one
            // is edited, the oldest of all.
            val victim = grown.indexOfFirst { !it.isEdited }.takeIf { it >= 0 } ?: 0
            grown.filterIndexed { i, _ -> i != victim }
        } else {
            grown
        }
        return ReplyDraftHistory(trimmed, trimmed.size - 1)
    }

    /** The user edited the version on screen. */
    fun editing(text: String): ReplyDraftHistory {
        if (index !in versions.indices) {
            // Typing into an empty result: the user is writing their own reply.
            return ReplyDraftHistory(listOf(Version(generated = "", text = text)), 0)
        }
        return copy(versions = versions.mapIndexed { i, v -> if (i == index) v.copy(text = text) else v })
    }

    fun selectingPrevious(): ReplyDraftHistory = if (canSelectPrevious) copy(index = index - 1) else this

    fun selectingNext(): ReplyDraftHistory = if (canSelectNext) copy(index = index + 1) else this

    companion object {
        /** Enough to compare a few takes; old unedited ones go first when full. */
        const val CAPACITY = 6
    }
}

/**
 * The AI composer as an explicit state machine: its stages, and the only ways
 * to move between them.
 *
 *     composing ──Generate──▶ generating(composing) ──reply──▶ result
 *         ▲                        │ fail / stop                 │ ▲
 *         │                        ▼                             │ │
 *         └────────── Back ─── composing          Regenerate ────┘ │
 *                                                  generating(result)
 *     result ──Edit / typing──▶ editing ──Done──▶ result
 *     result / editing ──Insert, field not empty──▶ conflict ──Cancel──▶ back where it was
 *
 * Rules it guarantees, and the tests pin:
 *  * at most ONE request at a time - a second Generate while one is running
 *    is refused, so rapid taps cannot double-spend the user's quota;
 *  * Regenerate never destroys a version: it adds one ([ReplyDraftHistory]);
 *  * a failure never throws away typing: the stage goes back to where the
 *    request started, with the error to show;
 *  * Insert always takes the text on screen, edits included.
 *
 * A pure value: no Android, no networking, no clock. Every change returns
 * the next flow, so the caller holds exactly one of them.
 */
data class ReplyComposerFlow(
    val stage: Stage = Stage.Composing,
    val drafts: ReplyDraftHistory = ReplyDraftHistory(),
    /** The last failure, shown inline until the user does something else. */
    val error: AIReplyError? = null
) {
    enum class Origin { COMPOSING, RESULT }

    sealed interface Stage {
        data object Composing : Stage
        data class Generating(val from: Origin) : Stage
        data object Result : Stage
        data object Editing : Stage
        /** The host field already has text. Cancel goes back to [wasEditing] or the result. */
        data class Conflict(val wasEditing: Boolean) : Stage
    }

    sealed interface InsertDecision {
        /** Put this text in the host field now. */
        data class Insert(val text: String) : InsertDecision
        /** Ask Replace / Add / Cancel first. */
        data object AskAboutExistingText : InsertDecision
        /** Nothing to insert. */
        data object NothingToInsert : InsertDecision
    }

    enum class ConflictChoice { REPLACE, APPEND, CANCEL }

    sealed interface ConflictResolution {
        data class Replace(val text: String) : ConflictResolution
        data class Append(val text: String) : ConflictResolution
        data object Cancelled : ConflictResolution
    }

    // ------------------------------------------------------------- derived

    val isGenerating: Boolean get() = stage is Stage.Generating

    /** The stage a request started from, while one is running. */
    val generationOrigin: Origin? get() = (stage as? Stage.Generating)?.from

    val isEditingDraft: Boolean get() = stage == Stage.Editing

    val isConflict: Boolean get() = stage is Stage.Conflict

    /** Stages that show the reply rather than the instruction. */
    val showsReply: Boolean
        get() = when (val s = stage) {
            Stage.Result, Stage.Editing, is Stage.Conflict -> true
            is Stage.Generating -> s.from == Origin.RESULT
            Stage.Composing -> false
        }

    val draftText: String get() = drafts.currentText

    // ---------------------------------------------------------- generation

    /**
     * Generate from the composer, or Regenerate from the result. Null when a
     * request must not start: one is already running, or the stage has no
     * Generate button.
     */
    fun startingGeneration(): ReplyComposerFlow? = when (stage) {
        Stage.Composing -> copy(stage = Stage.Generating(Origin.COMPOSING), error = null)
        Stage.Result, Stage.Editing -> copy(stage = Stage.Generating(Origin.RESULT), error = null)
        is Stage.Generating, is Stage.Conflict -> null
    }

    /**
     * A reply arrived. Ignored unless a request is actually running - a late
     * answer to a request the user stopped must not reappear.
     */
    fun receiving(reply: String): ReplyComposerFlow {
        if (!isGenerating) return this
        return copy(stage = Stage.Result, drafts = drafts.appending(reply), error = null)
    }

    /**
     * The request failed. The user lands back where they started, with the
     * message to read and everything they typed intact.
     */
    fun failing(failure: AIReplyError): ReplyComposerFlow {
        val next = when (val s = stage) {
            is Stage.Generating -> if (s.from == Origin.RESULT && !drafts.isEmpty) Stage.Result else Stage.Composing
            is Stage.Conflict -> return this
            else -> s
        }
        return copy(stage = next, error = if (failure == AIReplyError.Cancelled) null else failure)
    }

    /** Stop. Same as a failure, without the message. */
    fun cancellingGeneration(): ReplyComposerFlow {
        val s = stage as? Stage.Generating ?: return this
        val next = if (s.from == Origin.RESULT && !drafts.isEmpty) Stage.Result else Stage.Composing
        return copy(stage = next, error = null)
    }

    // ---------------------------------------------------------- navigation

    /**
     * Back from the reply to the instruction. The versions are kept: a new
     * Generate adds to them. Back while regenerating stops the request.
     */
    fun goingBack(): ReplyComposerFlow = when (stage) {
        Stage.Result, Stage.Editing, Stage.Generating(Origin.RESULT) ->
            copy(stage = Stage.Composing, error = null)
        else -> this
    }

    fun beginningEditing(): ReplyComposerFlow =
        if (stage == Stage.Result && !drafts.isEmpty) copy(stage = Stage.Editing, error = null) else this

    /**
     * Typing while the reply is on screen: the reply becomes editable and the
     * keystroke lands in it. Null when the keys cannot edit the reply.
     */
    fun beginningEditingForTyping(): ReplyComposerFlow? = when (stage) {
        Stage.Editing -> this
        Stage.Result -> copy(stage = Stage.Editing, error = null)
        else -> null
    }

    fun endingEditing(): ReplyComposerFlow = if (stage == Stage.Editing) copy(stage = Stage.Result) else this

    /** The text of the version on screen changed. */
    fun editingDraft(text: String): ReplyComposerFlow =
        if (stage == Stage.Editing) copy(drafts = drafts.editing(text), error = null) else this

    fun showingPreviousVersion(): ReplyComposerFlow =
        if (stage == Stage.Result || stage == Stage.Editing) copy(drafts = drafts.selectingPrevious()) else this

    fun showingNextVersion(): ReplyComposerFlow =
        if (stage == Stage.Result || stage == Stage.Editing) copy(drafts = drafts.selectingNext()) else this

    /** The source or the instruction changed: an old error no longer describes it. */
    fun clearingError(): ReplyComposerFlow = if (error == null) this else copy(error = null)

    // -------------------------------------------------------------- insert

    /** @param hostHasText whether the host field already has text. */
    fun requestingInsert(hostHasText: Boolean): Pair<ReplyComposerFlow, InsertDecision> {
        if (stage != Stage.Result && stage != Stage.Editing) return this to InsertDecision.NothingToInsert
        val text = drafts.currentText
        if (text.isBlank()) return this to InsertDecision.NothingToInsert
        if (hostHasText) {
            return copy(stage = Stage.Conflict(wasEditing = stage == Stage.Editing), error = null) to
                InsertDecision.AskAboutExistingText
        }
        return this to InsertDecision.Insert(text)
    }

    fun resolvingConflict(choice: ConflictChoice): Pair<ReplyComposerFlow, ConflictResolution> {
        val conflict = stage as? Stage.Conflict ?: return this to ConflictResolution.Cancelled
        val text = drafts.currentText
        return when (choice) {
            ConflictChoice.CANCEL ->
                copy(stage = if (conflict.wasEditing) Stage.Editing else Stage.Result) to ConflictResolution.Cancelled
            ConflictChoice.REPLACE -> this to ConflictResolution.Replace(text)
            ConflictChoice.APPEND -> this to ConflictResolution.Append(text)
        }
    }

    // ------------------------------------------------------------ lifecycle

    /**
     * Reopening a suspended session (the persona was changed): back to the
     * reply if there is one, otherwise to the instruction.
     */
    fun resuming(): ReplyComposerFlow {
        var next = cancellingGeneration()
        if (next.stage is Stage.Conflict || next.stage == Stage.Editing) next = next.copy(stage = Stage.Result)
        if (next.stage == Stage.Result && next.drafts.isEmpty) next = next.copy(stage = Stage.Composing)
        return next.copy(error = null)
    }
}
