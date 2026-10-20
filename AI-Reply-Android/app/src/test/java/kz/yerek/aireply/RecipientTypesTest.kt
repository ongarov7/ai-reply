package kz.yerek.aireply

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.BusinessCenter
import androidx.compose.material.icons.outlined.Group
import androidx.compose.material.icons.outlined.Handshake
import androidx.compose.material.icons.outlined.Person
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kz.yerek.aireply.core.lang.TemplateNaming
import kz.yerek.aireply.data.settings.SettingsStore
import kz.yerek.aireply.domain.model.RelationshipKind
import kz.yerek.aireply.domain.model.ReplyConfiguration
import kz.yerek.aireply.domain.model.TemplateSummary
import kz.yerek.aireply.ui.common.RelationshipIcons
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File
import javax.xml.parsers.DocumentBuilderFactory

/**
 * Friend, Client, Colleague, Partner (contract §3.1): new names and icons on
 * top of the old stored values, which never change.
 *
 * Алушылар: Дос, Клиент, Әріптес, Серіктес. Сақталған мәндер өзгермейді.
 */
class RecipientTypesTest {

    private val json = Json { ignoreUnknownKeys = true; encodeDefaults = true; coerceInputValues = true }

    @Test
    fun `a new install lists friend, client, colleague, partner`() {
        assertEquals(
            listOf(RelationshipKind.FRIEND, RelationshipKind.CLIENT, RelationshipKind.WORK, RelationshipKind.BUSINESS),
            RelationshipKind.builtIns
        )
        assertEquals(listOf("friend", "client", "work", "business"), ReplyConfiguration.INITIAL.visibleTemplates.map { it.id })
    }

    @Test
    fun `the stored values are the old ones`() {
        assertEquals(
            listOf("\"friend\"", "\"client\"", "\"business\"", "\"work\"", "\"custom\""),
            listOf(
                RelationshipKind.FRIEND, RelationshipKind.CLIENT, RelationshipKind.BUSINESS,
                RelationshipKind.WORK, RelationshipKind.CUSTOM
            ).map { json.encodeToString(it) }
        )
    }

    @Test
    fun `an existing configuration keeps its own order`() {
        // Written by an older build: Friend, Client, Business, Work.
        val stored = """{"profile":{"activeRelationships":["friend","client","business","work"]},"templates":[
            {"id":"friend","isBuiltIn":true,"relationship":"friend","sortIndex":0},
            {"id":"client","isBuiltIn":true,"relationship":"client","sortIndex":1},
            {"id":"business","isBuiltIn":true,"relationship":"business","sortIndex":2},
            {"id":"work","isBuiltIn":true,"relationship":"work","sortIndex":3}]}"""
        val decoded = json.decodeFromString<ReplyConfiguration>(stored).normalized()
        assertEquals(listOf("friend", "client", "business", "work"), decoded.visibleTemplates.map { it.id })
        assertEquals(RelationshipKind.BUSINESS, decoded.template("business")!!.relationship)
        assertEquals(RelationshipKind.builtIns.toSet(), decoded.profile.activeRelationships)
    }

    @Test
    fun `the names follow the contract in every language`() {
        val expected = mapOf(
            "values" to listOf("Friend", "Client", "Colleague", "Partner"),
            "values-ru" to listOf("Друг", "Клиент", "Коллега", "Партнёр"),
            "values-kk" to listOf("Дос", "Клиент", "Әріптес", "Серіктес"),
            "values-uz" to listOf("Doʻst", "Mijoz", "Hamkasb", "Hamkor")
        )
        val dative = mapOf(
            "values" to listOf("Friend", "Client", "Colleague", "Partner"),
            "values-ru" to listOf("Другу", "Клиенту", "Коллеге", "Партнёру"),
            "values-kk" to listOf("Досқа", "Клиентке", "Әріптеске", "Серіктеске"),
            "values-uz" to listOf("Doʻstga", "Mijozga", "Hamkasbga", "Hamkorga")
        )
        val keys = listOf("friend", "client", "work", "business")
        for ((folder, names) in expected) {
            val strings = strings(folder)
            assertEquals(folder, names, keys.map { strings["relationship_$it"] })
            assertEquals(folder, dative[folder], keys.map { strings["relationship_${it}_dative"] })
            // The tutorial line names the four as the row does.
            val line = strings.getValue("onboarding_usage_step_template")
            names.forEach { assertTrue("$folder: $line", line.contains(it)) }
        }
    }

    @Test
    fun `every built-in has a dative name, a custom one none`() {
        RelationshipKind.builtIns.forEach { kind -> assertTrue(TemplateNaming.dativeResource(kind) != null) }
        assertNull(TemplateNaming.dativeResource(RelationshipKind.CUSTOM))
        assertEquals(4, RelationshipKind.builtIns.map { TemplateNaming.dativeResource(it) }.distinct().size)
    }

    @Test
    fun `the icons are the contract's`() {
        assertSame(Icons.Outlined.Group, RelationshipIcons.of(RelationshipKind.FRIEND))
        assertSame(Icons.Outlined.Person, RelationshipIcons.of(RelationshipKind.CLIENT))
        assertSame(Icons.Outlined.BusinessCenter, RelationshipIcons.of(RelationshipKind.WORK))
        assertSame(Icons.Outlined.Handshake, RelationshipIcons.of(RelationshipKind.BUSINESS))
        assertNull(RelationshipIcons.of(RelationshipKind.CUSTOM))
        // The keyboard's cache knows templates by id: built-in ids are the raw values.
        assertSame(Icons.Outlined.Handshake, RelationshipIcons.forTemplateId("business"))
        assertNull(RelationshipIcons.forTemplateId("custom"))
        assertNull(RelationshipIcons.forTemplateId("6f1c2a52-6a8e-4f7c-9a0e-3c1f0e6b7d21"))
    }

    @Test
    fun `chip names cached by an older build are stale until rewritten`() {
        val file = InMemoryPreferences()
        assertTrue("nothing cached, nothing stale", SettingsStore(file).templateSummariesAreCurrent)

        // An older build wrote the cache and no version.
        file.edit().putString(
            "shared.templateSummaries",
            json.encodeToString(listOf(TemplateSummary("business", mapOf("ru" to "Бизнес"))))
        ).apply()
        assertFalse(SettingsStore(file).templateSummariesAreCurrent)

        SettingsStore(file).templateSummaries = listOf(TemplateSummary("business", mapOf("ru" to "Партнёр")))
        assertTrue(SettingsStore(file).templateSummariesAreCurrent)
        assertEquals("Партнёр", SettingsStore(file).templateSummaries!!.single().names["ru"])

        SettingsStore(file).templateSummaries = null
        assertTrue(SettingsStore(file).templateSummariesAreCurrent)
    }

    private fun strings(folder: String): Map<String, String> {
        val result = HashMap<String, String>()
        listOf("strings.xml", "strings_android.xml").forEach { name ->
            val file = File("src/main/res/$folder/$name")
            if (!file.exists()) return@forEach
            val nodes = DocumentBuilderFactory.newInstance().newDocumentBuilder().parse(file).getElementsByTagName("string")
            for (index in 0 until nodes.length) {
                val element = nodes.item(index) as org.w3c.dom.Element
                result[element.getAttribute("name")] = element.textContent
            }
        }
        return result
    }
}
