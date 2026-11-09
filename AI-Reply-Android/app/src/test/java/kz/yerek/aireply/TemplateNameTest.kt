package kz.yerek.aireply

import kz.yerek.aireply.domain.model.RelationshipKind
import kz.yerek.aireply.domain.model.ReplyTemplate
import kz.yerek.aireply.ui.feature.templates.withFinishedName
import kz.yerek.aireply.ui.feature.templates.withTypedName
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/**
 * Renaming a template: the field takes what is typed as it is, and the name
 * is put right once, when the editor closes.
 *
 * Үлгі атауы: теру кезінде өзгертілмейді, экран жабылғанда ғана реттеледі.
 */
class TemplateNameTest {

    private val custom = ReplyTemplate.custom("Поставщик", 4)
    private val friend = ReplyTemplate.builtIn(RelationshipKind.FRIEND, 0)

    @Test
    fun `a space stays for the next word`() {
        assertEquals("Мой ", custom.withTypedName("Мой ").customName)
    }

    @Test
    fun `the last letter can be deleted`() {
        assertEquals("", custom.withTypedName("").customName)
        assertEquals("", friend.withTypedName("").customName)
    }

    @Test
    fun `typing stops at the limit`() {
        val name = custom.withTypedName("я".repeat(ReplyTemplate.MAX_NAME + 1)).customName
        assertEquals("я".repeat(ReplyTemplate.MAX_NAME), name)
    }

    @Test
    fun `closing trims what was typed`() {
        val typed = custom.withTypedName("  Мой поставщик ")
        assertEquals("Мой поставщик", typed.withFinishedName(custom).customName)
    }

    @Test
    fun `a template the user made and left empty keeps its name`() {
        assertEquals("Поставщик", custom.withTypedName("").withFinishedName(custom).customName)
        assertEquals("spaces are empty too", "Поставщик", custom.withTypedName("   ").withFinishedName(custom).customName)
    }

    @Test
    fun `a built-in left empty goes back to its own name`() {
        val renamed = friend.copy(customName = "Друзья")
        assertNull(renamed.withTypedName("").withFinishedName(renamed).customName)
    }

    @Test
    fun `a name nobody touched stays as it was`() {
        assertNull(friend.withFinishedName(friend).customName)
        assertEquals(custom, custom.withFinishedName(custom))
    }
}
