package kz.yerek.aireply

import kotlinx.serialization.json.Json
import kotlinx.serialization.encodeToString
import kz.yerek.aireply.domain.model.BusinessContext
import kz.yerek.aireply.domain.model.EmojiPolicy
import kz.yerek.aireply.domain.model.GrammaticalGender
import kz.yerek.aireply.domain.model.RelationshipKind
import kz.yerek.aireply.domain.model.ReplyConfiguration
import kz.yerek.aireply.domain.model.ReplyTemplate
import kz.yerek.aireply.domain.model.ReplyTone
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ConfigurationTest {

    private val json = Json { ignoreUnknownKeys = true; encodeDefaults = true; coerceInputValues = true }

    @Test
    fun `normalization restores missing built-ins`() {
        val stripped = ReplyConfiguration(templates = listOf(ReplyTemplate.builtIn(RelationshipKind.FRIEND, 0)))
        val repaired = stripped.normalized()
        RelationshipKind.builtIns.forEach { kind ->
            assertNotNull(kind.raw, repaired.template(kind.raw))
        }
    }

    @Test
    fun `normalization never leaves every template hidden`() {
        val allHidden = ReplyConfiguration(
            templates = ReplyTemplate.defaults.map { it.copy(isVisible = false) }
        )
        assertTrue(allHidden.normalized().visibleTemplates.isNotEmpty())
    }

    @Test
    fun `normalization re-indexes duplicates`() {
        val duplicated = ReplyConfiguration(
            templates = ReplyTemplate.defaults.map { it.copy(sortIndex = 0) }
        )
        val indices = duplicated.normalized().templates.map { it.sortIndex }
        assertEquals(indices.distinct(), indices)
        assertEquals(listOf(0, 1, 2, 3), indices)
    }

    @Test
    fun `a template written by an older build decodes with per-relationship defaults`() {
        // Only the two fields the very first build had.
        val legacy = """{"profile":{},"templates":[{"id":"friend","relationship":"friend"}]}"""
        val decoded = json.decodeFromString<ReplyConfiguration>(legacy).normalized()
        val friend = decoded.template("friend")!!

        assertEquals(ReplyTone.FRIENDLY, friend.tone)
        assertEquals(EmojiPolicy.ALLOWED, friend.emojiPolicy)
        // A Friend must NOT acquire a business container it never had.
        assertNull(friend.business)
    }

    @Test
    fun `a client decoded from an old file does get a business container`() {
        val legacy = """{"profile":{},"templates":[{"id":"client","relationship":"client"}]}"""
        val decoded = json.decodeFromString<ReplyConfiguration>(legacy)
        assertEquals(BusinessContext.EMPTY, decoded.template("client")!!.business)
        assertEquals(EmojiPolicy.MINIMAL, decoded.template("client")!!.emojiPolicy)
    }

    @Test
    fun `round trip preserves everything`() {
        val original = ReplyConfiguration.INITIAL.normalized()
        val restored = json.decodeFromString<ReplyConfiguration>(json.encodeToString(original))
        assertEquals(original, restored)
    }

    /** Жыныс сұралмаған профиль null болып қалады; белгісіз мән профильді өшірмейді. */
    @Test
    fun `the grammatical gender decodes tolerantly`() {
        val older = json.decodeFromString<ReplyConfiguration>("""{"profile":{"role":"дизайнер"}}""")
        assertNull("never asked", older.profile.grammaticalGender)
        assertEquals("дизайнер", older.profile.role)

        val chosen = json.decodeFromString<ReplyConfiguration>("""{"profile":{"grammaticalGender":"female"}}""")
        assertEquals(GrammaticalGender.FEMALE, chosen.profile.grammaticalGender)

        val newer = json.decodeFromString<ReplyConfiguration>(
            """{"profile":{"role":"дизайнер","grammaticalGender":"other"}}"""
        )
        assertNull("a value from a newer build", newer.profile.grammaticalGender)
        assertEquals("the rest of the profile survives", "дизайнер", newer.profile.role)

        val skipped = ReplyConfiguration.INITIAL.copy(
            profile = ReplyConfiguration.INITIAL.profile.copy(grammaticalGender = GrammaticalGender.UNSPECIFIED)
        )
        val encoded = json.encodeToString(skipped)
        assertTrue(encoded.contains("\"grammaticalGender\":\"unspecified\""))
        assertEquals(skipped, json.decodeFromString<ReplyConfiguration>(encoded))
    }

    @Test
    fun `wire values match the iOS raw values`() {
        val encoded = json.encodeToString(ReplyTemplate.builtIn(RelationshipKind.CLIENT, 0))
        assertTrue(encoded.contains("\"mention_when_relevant\""))
        assertTrue(encoded.contains("\"client\""))
    }

    @Test
    fun `business rules are cleaned for the prompt but kept for the editor`() {
        val business = BusinessContext(rules = listOf("  Be brief ", "", "be brief", "No prices"))
        assertEquals(listOf("Be brief", "No prices"), business.cleanRules)
        assertEquals(4, business.rules.size)
    }

    @Test
    fun `rules are capped`() {
        var business = BusinessContext.EMPTY
        repeat(20) { business = business.addingRule("rule $it") }
        assertEquals(BusinessContext.MAX_RULES, business.rules.size)
    }
}
