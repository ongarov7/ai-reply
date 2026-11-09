package kz.yerek.aireply

import kotlinx.coroutines.runBlocking
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kz.yerek.aireply.ai.AIConfiguration
import kz.yerek.aireply.ai.AIFeatures
import kz.yerek.aireply.ai.AILimits
import kz.yerek.aireply.ai.AIReplyService
import kz.yerek.aireply.ai.AccountReplyTransport
import kz.yerek.aireply.ai.GeneratedReply
import kz.yerek.aireply.ai.ReplyPromptBuilder
import kz.yerek.aireply.ai.ReplyTransport
import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.data.profile.ProfileSync
import kz.yerek.aireply.domain.model.EmojiPolicy
import kz.yerek.aireply.domain.model.ReplyConfiguration
import kz.yerek.aireply.domain.model.ReplyLanguagePreference
import kz.yerek.aireply.domain.model.ReplyLength
import kz.yerek.aireply.domain.model.ReplyTemplate
import kz.yerek.aireply.domain.model.ReplyTone
import kz.yerek.aireply.domain.model.UserProfile
import kz.yerek.aireply.domain.model.WorkingHoursBehaviour
import kz.yerek.aireply.ui.feature.compose.tryReplyRequest
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

/**
 * The profile's reply language: stored the way the rest of the profile is, and
 * sent only to a server that said it knows the field.
 *
 * Жауап тілі таңдалмаса, тілді көшірілген хабарлама шешеді.
 */
class ReplyLanguagePreferenceTest {

    private val store = Json { ignoreUnknownKeys = true; encodeDefaults = true; coerceInputValues = true }

    private val everything = AIFeatures(replyPreferences = true, senderProfile = true, instructionPolish = false)

    @Before
    fun startFresh() {
        AILimits.install(InMemoryPreferences())
    }

    @After
    fun forgetServerAnswers() {
        AILimits.install(InMemoryPreferences())
    }

    private fun context(
        language: ReplyLanguagePreference?,
        description: String = ""
    ) = AccountReplyTransport.RequestContext(
        message = "Сәлем! Ертең кездесуге уақытың бар ма?",
        userInstruction = "",
        templateId = "friend",
        templateName = "Друг",
        templateRelationship = "friend",
        templateTone = ReplyTone.FRIENDLY,
        templateInstructions = "",
        templateReplyLength = ReplyLength.SHORT,
        templateEmojiPolicy = EmojiPolicy.ALLOWED,
        templateWorkingHoursBehaviour = WorkingHoursBehaviour.IGNORE,
        templateBusiness = null,
        appLanguage = "ru",
        business = null,
        appVersion = "1.0",
        profileDescription = description,
        replyLanguage = language
    )

    private fun body(context: AccountReplyTransport.RequestContext, features: AIFeatures): JsonObject =
        Json.parseToJsonElement(AccountReplyTransport.encode(context, features)).jsonObject

    /** What the service hands the transport for a reply from an app shown in Russian. */
    private fun sentBy(profile: UserProfile): AccountReplyTransport.RequestContext = runBlocking {
        var context: AccountReplyTransport.RequestContext? = null
        val service = AIReplyService(
            configuration = AIConfiguration { true },
            nameTemplate = { template, _ -> template.id },
            accountTransport = { _, requestContext ->
                context = requestContext
                object : ReplyTransport {
                    override suspend fun generate(prompt: ReplyPromptBuilder.Prompt) = GeneratedReply("ok")
                }
            }
        )
        service.generate(
            tryReplyRequest(
                "Сәлем!",
                ReplyTemplate.defaults.first(),
                ReplyConfiguration(profile = profile),
                AppLanguage.RUSSIAN,
                ""
            )
        )
        requireNotNull(context)
    }

    // ----------------------------------------------------------- the picker

    @Test
    fun `the codes are the ones the server accepts, in the order iOS lists them`() {
        assertEquals(listOf("kk", "ru", "en", "uz"), ReplyLanguagePreference.entries.map { it.raw })
    }

    @Test
    fun `every language is named in itself`() {
        assertEquals(
            listOf(
                AppLanguage.KAZAKH.nativeName,
                AppLanguage.RUSSIAN.nativeName,
                AppLanguage.ENGLISH.nativeName,
                AppLanguage.UZBEK.nativeName
            ),
            ReplyLanguagePreference.entries.map { it.nativeName }
        )
    }

    // ------------------------------------------------------------- the file

    @Test
    fun `a new profile and one from an older build are on Automatic`() {
        assertNull(UserProfile.EMPTY.replyLanguage)
        val older = store.decodeFromString<ReplyConfiguration>("""{"profile":{"role":"Дизайнер"}}""")
        assertNull(older.profile.replyLanguage)
        assertEquals("Дизайнер", older.profile.role)
    }

    @Test
    fun `the choice survives the file under the iOS key`() {
        val chosen = ReplyConfiguration(profile = UserProfile(replyLanguage = ReplyLanguagePreference.KAZAKH))
        val encoded = store.encodeToString(chosen)
        assertTrue(encoded, encoded.contains("\"replyLanguage\":\"kk\""))
        assertEquals(chosen, store.decodeFromString<ReplyConfiguration>(encoded))
    }

    @Test
    fun `a language from a newer build reads as Automatic and keeps the profile`() {
        val newer = store.decodeFromString<ReplyConfiguration>(
            """{"profile":{"role":"Дизайнер","replyLanguage":"fr"}}"""
        )
        assertNull(newer.profile.replyLanguage)
        assertEquals("Дизайнер", newer.profile.role)
    }

    @Test
    fun `a deleted account leaves no fixed language for the next one`() {
        val left = ProfileSync.withoutAccount(UserProfile(role = "Дизайнер", replyLanguage = ReplyLanguagePreference.KAZAKH))
        assertNull(left.replyLanguage)
        assertEquals("", left.role)
    }

    // ------------------------------------------------------------- the wire

    @Test
    fun `the language goes only to a server that announced it`() {
        val older = body(context(ReplyLanguagePreference.KAZAKH, description = "Florist in Almaty"), AIFeatures.NONE)
        assertFalse("reply_language" in older.toString())

        val genderOnly = body(
            context(ReplyLanguagePreference.KAZAKH, description = "Florist in Almaty"),
            everything.copy(replyPreferences = false)
        )
        assertFalse("sender_profile alone is not enough", "reply_language" in genderOnly.toString())

        val current = body(context(ReplyLanguagePreference.KAZAKH, description = "Florist in Almaty"), everything)
        assertEquals("kk", current["profile"]!!.jsonObject["reply_language"]!!.jsonPrimitive.content)
    }

    @Test
    fun `a language alone still sends the profile block`() {
        val body = body(context(ReplyLanguagePreference.UZBEK), everything)
        assertEquals(setOf("preferred_tone", "reply_language"), body["profile"]!!.jsonObject.keys)
        assertEquals("uz", body["profile"]!!.jsonObject["reply_language"]!!.jsonPrimitive.content)
    }

    @Test
    fun `an empty profile on an older server sends no block for the language`() {
        val body = body(context(ReplyLanguagePreference.RUSSIAN), everything.copy(replyPreferences = false))
        assertFalse("profile" in body.keys)
    }

    @Test
    fun `Automatic names no language and the copied message goes as it is`() {
        val body = body(context(null, description = "Дизайнер"), everything)
        assertFalse("reply_language" in body["profile"]!!.jsonObject.keys)
        assertEquals(
            "Сәлем! Ертең кездесуге уақытың бар ма?",
            body["source_text"]!!.jsonPrimitive.content
        )
    }

    @Test
    fun `the service sends the language the profile holds and nothing the app guessed`() {
        assertEquals(
            ReplyLanguagePreference.ENGLISH,
            sentBy(UserProfile(replyLanguage = ReplyLanguagePreference.ENGLISH)).replyLanguage
        )
        assertNull("the app is in Russian, the profile is on Automatic", sentBy(UserProfile.EMPTY).replyLanguage)
    }
}
