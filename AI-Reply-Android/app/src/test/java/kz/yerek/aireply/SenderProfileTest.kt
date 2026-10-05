package kz.yerek.aireply

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kz.yerek.aireply.ai.AIFeatures
import kz.yerek.aireply.ai.AILimits
import kz.yerek.aireply.ai.AccountComposeTransport
import kz.yerek.aireply.ai.AccountReplyTransport
import kz.yerek.aireply.ai.ComposeService
import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.data.account.ServerConfigDto
import kz.yerek.aireply.data.account.ServerFeaturesDto
import kz.yerek.aireply.data.profile.ProfileSync
import kz.yerek.aireply.domain.model.BusinessContext
import kz.yerek.aireply.domain.model.EmojiPolicy
import kz.yerek.aireply.domain.model.GrammaticalGender
import kz.yerek.aireply.domain.model.ReplyLength
import kz.yerek.aireply.domain.model.ReplyTone
import kz.yerek.aireply.domain.model.WorkingHoursBehaviour
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The sender's profile on the wire: the profile block, the gender and the
 * layout language reach only a server that announced them, and the gender
 * follows the user across devices without ever being guessed.
 *
 * Жіберуші профилі: сервер жарияламаған өріс ешқашан жіберілмейді.
 */
class SenderProfileTest {

    @After
    fun forgetServerAnswers() {
        // AILimits is process-wide; the next test class starts from nothing.
        AILimits.install(InMemoryPreferences())
    }

    private fun context(
        description: String = "",
        role: String = "",
        tone: ReplyTone = ReplyTone.NATURAL,
        business: BusinessContext = BusinessContext.EMPTY,
        gender: GrammaticalGender? = null,
        inputLanguage: String? = null
    ) = AccountReplyTransport.RequestContext(
        message = "Ты завтра свободен?",
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
        profileRole = role,
        profileTone = tone,
        profileBusiness = business,
        grammaticalGender = gender,
        inputLanguage = inputLanguage
    )

    private fun reply(context: AccountReplyTransport.RequestContext, features: AIFeatures): JsonObject =
        Json.parseToJsonElement(AccountReplyTransport.encode(context, features)).jsonObject

    private val everything = AIFeatures(replyPreferences = true, senderProfile = true, instructionPolish = false)

    // --------------------------------------------------------------- reply

    @Test
    fun `an older server gets none of the new fields`() {
        val body = reply(
            context(description = "Дизайнер", tone = ReplyTone.FORMAL, gender = GrammaticalGender.MALE, inputLanguage = "ru"),
            AIFeatures.NONE
        )
        assertFalse("profile", "profile" in body.keys)
        assertFalse("input_language", "input_language" in body.keys)
    }

    @Test
    fun `profile edits reach the reply once the server accepts them`() {
        val body = reply(
            context(
                description = "Отвечаю клиентам студии",
                role = "дизайнер",
                tone = ReplyTone.PROFESSIONAL,
                business = BusinessContext(offering = "Логотипы"),
                gender = GrammaticalGender.FEMALE,
                inputLanguage = "ru"
            ),
            everything.copy(senderProfile = false)
        )
        val profile = body["profile"]!!.jsonObject
        assertEquals("Отвечаю клиентам студии", profile["description"]!!.jsonPrimitive.content)
        assertEquals("дизайнер", profile["role"]!!.jsonPrimitive.content)
        assertEquals("professional", profile["preferred_tone"]!!.jsonPrimitive.content)
        assertEquals("Логотипы", profile["business"]!!.jsonObject["offering"]!!.jsonPrimitive.content)
        assertFalse("gender needs its own flag", "grammatical_gender" in profile.keys)
        assertFalse("so does the layout", "input_language" in body.keys)
    }

    @Test
    fun `a gender alone still sends the profile block`() {
        val body = reply(context(gender = GrammaticalGender.MALE, inputLanguage = "kk"), everything)
        val profile = body["profile"]!!.jsonObject
        assertEquals("male", profile["grammatical_gender"]!!.jsonPrimitive.content)
        assertEquals(setOf("grammatical_gender", "preferred_tone"), profile.keys)
        assertEquals("kk", body["input_language"]!!.jsonPrimitive.content)

        val genderOnly = reply(context(gender = GrammaticalGender.UNSPECIFIED), everything.copy(replyPreferences = false))
        assertEquals(setOf("grammatical_gender"), genderOnly["profile"]!!.jsonObject.keys)
        assertEquals("unspecified", genderOnly["profile"]!!.jsonObject["grammatical_gender"]!!.jsonPrimitive.content)
    }

    @Test
    fun `nothing to say sends no profile block`() {
        val body = reply(context(), everything)
        assertFalse("profile" in body.keys)
        assertFalse("no layout known, no field", "input_language" in body.keys)

        val toneOnly = reply(context(tone = ReplyTone.SHORT), everything)
        assertEquals(setOf("preferred_tone"), toneOnly["profile"]!!.jsonObject.keys)
    }

    // ------------------------------------------------------------- compose

    @Test
    fun `compose carries the gender and the layout only for a server that accepts them`() {
        val request = ComposeService.Request(
            instruction = "Поздравь коллегу",
            uiLanguage = AppLanguage.RUSSIAN,
            inputLanguage = KeyboardLanguage.RUSSIAN
        )
        val older = Json.parseToJsonElement(
            AccountComposeTransport.encode(request, "1.0", GrammaticalGender.FEMALE, AIFeatures.NONE)
        ).jsonObject
        assertEquals(setOf("instruction", "language", "regenerate", "platform", "app_version"), older.keys)

        val current = Json.parseToJsonElement(
            AccountComposeTransport.encode(request, "1.0", GrammaticalGender.FEMALE, everything)
        ).jsonObject
        assertEquals("female", current["profile"]!!.jsonObject["grammatical_gender"]!!.jsonPrimitive.content)
        assertEquals("ru", current["input_language"]!!.jsonPrimitive.content)

        val neverAsked = Json.parseToJsonElement(
            AccountComposeTransport.encode(request, "1.0", null, everything)
        ).jsonObject
        assertFalse("profile" in neverAsked.keys)
    }

    // ----------------------------------------------------------- the flags

    @Test
    fun `request-shaping flags are off unless the server says otherwise`() {
        val missing = ServerFeaturesDto()
        assertFalse(missing.replyPreferences)
        assertFalse(missing.senderProfile)
        assertFalse(missing.instructionPolish)
        assertTrue("sign-in flags keep assuming yes", missing.emailOtp && missing.googleSignIn)
    }

    @Test
    fun `announced flags are kept for the keyboard`() {
        val file = InMemoryPreferences()
        AILimits.install(file)
        assertEquals(AIFeatures.NONE, AILimits.features)

        AILimits.apply(ServerConfigDto(features = ServerFeaturesDto(replyPreferences = true, senderProfile = true)))
        AILimits.install(file)
        assertEquals("read back from the file", everything, AILimits.features)

        AILimits.apply(ServerConfigDto())
        assertEquals("a server without a features block supports none", AIFeatures.NONE, AILimits.features)
    }

    // ------------------------------------------------------- across devices

    @Test
    fun `a choice from another device is taken over`() {
        assertEquals(GrammaticalGender.FEMALE, ProfileSync.adoptedGender(null, "female", pendingSync = false))
        assertEquals(
            GrammaticalGender.MALE,
            ProfileSync.adoptedGender(GrammaticalGender.UNSPECIFIED, "male", pendingSync = false)
        )
        assertEquals(
            GrammaticalGender.MALE,
            ProfileSync.adoptedGender(GrammaticalGender.FEMALE, "male", pendingSync = false)
        )
    }

    @Test
    fun `this device's unsent choice and the server default are never taken over`() {
        assertNull("local change still unsent", ProfileSync.adoptedGender(GrammaticalGender.MALE, "female", pendingSync = true))
        assertNull("the database default for everyone", ProfileSync.adoptedGender(null, "unspecified", pendingSync = false))
        assertNull("already the same", ProfileSync.adoptedGender(GrammaticalGender.MALE, "male", pendingSync = false))
        assertNull("an older server", ProfileSync.adoptedGender(null, null, pendingSync = false))
        assertNull("a value this build does not know", ProfileSync.adoptedGender(null, "other", pendingSync = false))
    }
}
