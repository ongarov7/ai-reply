package kz.yerek.aireply

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kz.yerek.aireply.ai.AIConfiguration
import kz.yerek.aireply.ai.AILimits
import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.ai.AIReplyException
import kz.yerek.aireply.ai.AccountTranscriptionTransport
import kz.yerek.aireply.ai.DebugTranscribeMock
import kz.yerek.aireply.ai.Transcript
import kz.yerek.aireply.ai.TranscriptionTransport
import kz.yerek.aireply.ai.VoiceTranscriptionService
import kz.yerek.aireply.data.account.AccountService
import kz.yerek.aireply.data.account.ApiClient
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.DeviceDescriptor
import kz.yerek.aireply.data.account.MultipartBody
import kz.yerek.aireply.data.account.ServerConfigDto
import kz.yerek.aireply.data.account.ServerFeaturesDto
import kz.yerek.aireply.support.FakeCredentials
import kz.yerek.aireply.support.FakeResponse
import kz.yerek.aireply.support.FakeServer
import kz.yerek.aireply.support.sessionOn
import kz.yerek.aireply.support.tokenPair
import kz.yerek.aireply.voice.RecordedAudio
import kz.yerek.aireply.voice.TranscriptionPurpose
import kz.yerek.aireply.voice.TranscriptionRequest
import org.junit.After
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.ByteArrayOutputStream
import java.io.File
import java.io.IOException
import java.net.SocketTimeoutException
import java.net.UnknownHostException

/**
 * Voice input through the server: the multipart body byte for byte, the
 * answer, the closed error set, when the microphone is offered, and the
 * DEBUG stand-in.
 *
 * Дауыс енгізу: сұраныс денесі, жауап, қателер және DEBUG үлгісі.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class VoiceTranscriptionTest {

    @get:Rule
    val temp = TemporaryFolder()

    @After
    fun forgetServerAnswers() {
        // AILimits is process-wide; the next test class starts from nothing.
        AILimits.install(InMemoryPreferences())
    }

    private fun recording(
        bytes: ByteArray = "AAC-FRAMES".toByteArray(),
        name: String = "voice-1.m4a",
        durationMs: Long = 12_345
    ): RecordedAudio {
        // A folder each: a test may record more than once under the same name.
        val file = File(temp.newFolder(), name).apply { writeBytes(bytes) }
        return RecordedAudio(file = file, mimeType = "audio/mp4", durationMs = durationMs)
    }

    private fun request(
        audio: RecordedAudio = recording(),
        languages: List<String> = listOf("kk", "ru"),
        purpose: TranscriptionPurpose = TranscriptionPurpose.DICTATION
    ) = TranscriptionRequest(audio = audio, requestId = "req-1", languages = languages, purpose = purpose)

    private fun transport(
        server: FakeServer,
        credentials: FakeCredentials = FakeCredentials(access = "a", refresh = "r")
    ) = AccountTranscriptionTransport(
        baseUrl = "https://example.test",
        session = sessionOn(server, credentials),
        clientFactory = { baseUrl -> ApiClient(baseUrl, openConnection = server::open) }
    )

    // ------------------------------------------------------------ the body

    @Test
    fun `the body is the documented multipart, byte for byte`() {
        // CRLF, a dash pair and a byte that is not UTF-8: audio is bytes, not text.
        val audio = byteArrayOf(0x00, 0x0D, 0x0A, 0x2D, 0x2D, 0xFF.toByte(), 0x7F)
        val file = temp.newFile("rec.m4a").apply { writeBytes(audio) }
        val body = MultipartBody(
            parts = listOf(
                MultipartBody.Part.Field("request_id", "req-1"),
                MultipartBody.Part.Field("note", "сәлем"),
                MultipartBody.Part.FilePart("audio", file, contentType = "audio/mp4")
            ),
            boundary = "BOUNDARY"
        )

        val expected = ByteArrayOutputStream().apply {
            write(
                (
                    "--BOUNDARY\r\n" +
                        "Content-Disposition: form-data; name=\"request_id\"\r\n" +
                        "Content-Type: text/plain; charset=utf-8\r\n" +
                        "\r\n" +
                        "req-1\r\n" +
                        "--BOUNDARY\r\n" +
                        "Content-Disposition: form-data; name=\"note\"\r\n" +
                        "Content-Type: text/plain; charset=utf-8\r\n" +
                        "\r\n" +
                        "сәлем\r\n" +
                        "--BOUNDARY\r\n" +
                        "Content-Disposition: form-data; name=\"audio\"; filename=\"rec.m4a\"\r\n" +
                        "Content-Type: audio/mp4\r\n" +
                        "\r\n"
                    ).toByteArray(Charsets.UTF_8)
            )
            write(audio)
            write("\r\n--BOUNDARY--\r\n".toByteArray(Charsets.UTF_8))
        }.toByteArray()

        val written = ByteArrayOutputStream().also(body::writeTo).toByteArray()
        assertArrayEquals(expected, written)
        assertEquals("the length is known before writing", expected.size.toLong(), body.contentLength)
        assertEquals("multipart/form-data; boundary=BOUNDARY", body.contentType)

        val again = ByteArrayOutputStream().also(body::writeTo).toByteArray()
        assertArrayEquals("a retry sends the same bytes", expected, again)
    }

    @Test
    fun `the boundary is random per body and a name cannot break its header`() {
        val first = MultipartBody(emptyList()).boundary
        val second = MultipartBody(emptyList()).boundary
        assertNotEquals(first, second)
        assertTrue(first.length <= 70)
        assertTrue(first.all { it.isLetterOrDigit() || it == '-' })

        assertEquals("a%22b%0D%0Ac.m4a", MultipartBody.quoted("a\"b\r\nc.m4a"))
        val file = temp.newFile("plain.m4a")
        val body = MultipartBody(
            listOf(MultipartBody.Part.FilePart("audio", file, contentType = "audio/mp4", filename = "x\"\r\ny.m4a")),
            boundary = "B"
        )
        val text = ByteArrayOutputStream().also(body::writeTo).toString(Charsets.UTF_8.name())
        assertTrue(text.contains("filename=\"x%22%0D%0Ay.m4a\"\r\n"))
    }

    @Test
    fun `the form carries the four fields and the audio, nothing else`() {
        val audio = recording()
        val form = AccountTranscriptionTransport.form(
            request(audio, languages = listOf("KK", " ru ", "kk", "", "kaz")),
            boundary = "B"
        )
        assertEquals(listOf("request_id", "duration_ms", "languages", "purpose", "audio"), form.parts.map { it.name })
        val fields = form.parts.filterIsInstance<MultipartBody.Part.Field>().associate { it.name to it.value }
        assertEquals("req-1", fields["request_id"])
        assertEquals("12345", fields["duration_ms"])
        assertEquals("two-letter codes, each once, in order", "kk,ru", fields["languages"])
        assertEquals("dictation", fields["purpose"])
        val file = form.parts.filterIsInstance<MultipartBody.Part.FilePart>().single()
        assertEquals(audio.file, file.file)
        assertEquals("voice-1.m4a", file.filename)
        assertEquals("audio/mp4", file.contentType)

        val noHints = AccountTranscriptionTransport.form(
            request(audio, languages = emptyList(), purpose = TranscriptionPurpose.INSTRUCTION)
        )
        assertFalse("an empty hint is not sent", noHints.parts.any { it.name == "languages" })
        assertEquals(
            "instruction",
            noHints.parts.filterIsInstance<MultipartBody.Part.Field>().single { it.name == "purpose" }.value
        )
    }

    // ------------------------------------------------------------- the wire

    @Test
    fun `the request is an authenticated multipart POST and the answer is read`() = runBlocking {
        val server = FakeServer {
            FakeResponse(200, """{"text":"  Сәлем, қалайсыз?  ","languages":["kk"],"duration_ms":12345,"model":"x"}""")
        }

        val transcript = transport(server).transcribe(request())

        assertEquals("the transport hands over what came", "  Сәлем, қалайсыз?  ", transcript.text)
        assertEquals(listOf("kk"), transcript.languages)
        assertEquals(12_345L, transcript.durationMs)

        val sent = server.requests.single()
        assertEquals("POST", sent.method)
        assertEquals("/api/v1/ai/transcribe", sent.path)
        assertEquals("Bearer a", sent.header("Authorization"))
        assertEquals("application/json", sent.header("Accept"))
        val type = sent.header("Content-Type").orEmpty()
        assertTrue(type, type.startsWith("multipart/form-data; boundary=AIReply-"))
        val boundary = type.substringAfter("boundary=")
        assertTrue(sent.body.startsWith("--$boundary\r\nContent-Disposition: form-data; name=\"request_id\"\r\n"))
        assertTrue(sent.body.endsWith("\r\n--$boundary--\r\n"))
        assertTrue(sent.body.contains("name=\"languages\"\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nkk,ru\r\n"))
        assertTrue(
            sent.body.contains(
                "name=\"audio\"; filename=\"voice-1.m4a\"\r\nContent-Type: audio/mp4\r\n\r\nAAC-FRAMES\r\n"
            )
        )
    }

    @Test
    fun `a rejected token is refreshed once and the whole recording goes again`() = runBlocking {
        val server = FakeServer { request ->
            when (request.path) {
                "/api/v1/auth/refresh" -> tokenPair("new", "r2")
                "/api/v1/ai/transcribe" ->
                    if (request.header("Authorization") == "Bearer new") FakeResponse(200, """{"text":"ok","languages":[]}""")
                    else FakeResponse(401, FakeServer.envelope("UNAUTHORIZED"))
                else -> FakeResponse(404, FakeServer.envelope("NOT_FOUND"))
            }
        }

        val transcript = transport(server, FakeCredentials(access = "old", refresh = "r1")).transcribe(request())

        assertEquals("ok", transcript.text)
        assertEquals(emptyList<String>(), transcript.languages)
        assertNull(transcript.durationMs)
        assertEquals(1, server.count("/api/v1/auth/refresh"))
        val attempts = server.requests.filter { it.path == "/api/v1/ai/transcribe" }
        assertEquals(listOf("Bearer old", "Bearer new"), attempts.map { it.header("Authorization") })
        assertEquals("the same body both times", attempts[0].body, attempts[1].body)
        assertTrue(attempts[1].body.contains("AAC-FRAMES"))
    }

    @Test
    fun `the detected languages are read in either shape and never fail the words`() {
        val openAiShape = AccountTranscriptionTransport.decode("""{"text":"hi","languages":[{"code":"EN"}]}""")
        assertEquals(listOf("en"), openAiShape?.languages)
        assertEquals("hi", AccountTranscriptionTransport.decode("""{"text":"hi","languages":"en"}""")?.text)
        assertEquals("", AccountTranscriptionTransport.decode("""{"text":""}""")?.text)
        assertNull("no text is not a transcript", AccountTranscriptionTransport.decode("""{"languages":["kk"]}"""))
        assertNull(AccountTranscriptionTransport.decode("""{"text":null}"""))
        assertNull(AccountTranscriptionTransport.decode("<html>Bad gateway</html>"))
    }

    // --------------------------------------------------------------- errors

    private fun failureFor(response: FakeResponse): AIReplyError = failureFrom(FakeServer { request ->
        if (request.path == "/api/v1/auth/refresh") tokenPair("a2", "r2") else response
    })

    private fun failureFrom(server: FakeServer): AIReplyError = runBlocking {
        try {
            transport(server).transcribe(request())
            fail("expected a failure")
            error("unreachable")
        } catch (exception: AIReplyException) {
            exception.error
        }
    }

    @Test
    fun `server answers become the closed error set`() {
        assertEquals(
            "the daily voice cap is its own error",
            AIReplyError.VoiceLimitReached,
            failureFor(FakeResponse(429, FakeServer.envelope("VOICE_LIMIT_REACHED")))
        )
        assertEquals(AIReplyError.RateLimited, failureFor(FakeResponse(429, FakeServer.envelope("RATE_LIMITED"))))
        assertEquals("a bare 429", AIReplyError.RateLimited, failureFor(FakeResponse(429, "")))
        assertEquals(AIReplyError.ConsentRequired, failureFor(FakeResponse(403, FakeServer.envelope("CONSENT_REQUIRED"))))
        assertEquals(AIReplyError.AuthenticationFailed, failureFor(FakeResponse(403, FakeServer.envelope("ACCOUNT_DISABLED"))))
        assertEquals("401 after the refresh", AIReplyError.AuthenticationFailed, failureFor(FakeResponse(401, "")))
        assertEquals(
            "an older server without the endpoint",
            AIReplyError.ServiceUnavailable,
            failureFor(FakeResponse(404, "404 page not found"))
        )
        assertEquals(AIReplyError.ServiceUnavailable, failureFor(FakeResponse(405, "")))
        assertEquals(
            "audio the server could not read",
            AIReplyError.ServiceUnavailable,
            failureFor(FakeResponse(400, FakeServer.envelope("INVALID_REQUEST")))
        )
        assertEquals(AIReplyError.ServiceUnavailable, failureFor(FakeResponse(502, "")))
        assertEquals(AIReplyError.TimedOut, failureFor(FakeResponse(504, FakeServer.envelope("AI_TIMEOUT"))))
        assertEquals(
            "a 200 that is not a transcript",
            AIReplyError.ServiceUnavailable,
            failureFor(FakeResponse(200, """{"reply":"x"}"""))
        )
    }

    @Test
    fun `no answer at all is offline, a timeout or unavailable`() {
        assertEquals(AIReplyError.Offline, failureFrom(FakeServer { throw UnknownHostException("example.test") }))
        assertEquals(AIReplyError.TimedOut, failureFrom(FakeServer { throw SocketTimeoutException("read") }))
        assertEquals(AIReplyError.ServiceUnavailable, failureFrom(FakeServer { throw IOException("reset") }))
    }

    @Test
    fun `signed out, nothing is sent`() = runBlocking {
        val server = FakeServer { FakeResponse(200, """{"text":"x"}""") }
        try {
            transport(server, FakeCredentials()).transcribe(request())
            fail("expected a failure")
        } catch (exception: AIReplyException) {
            assertEquals(AIReplyError.AuthenticationFailed, exception.error)
        }
        assertTrue(server.requests.isEmpty())
    }

    @Test
    fun `the codes map once, in the client`() {
        val body = """{"error":{"code":"VOICE_LIMIT_REACHED","message":"x","details":{"resets_at":"2026-10-15T00:00:00+05:00"}}}"""
        assertEquals(ApiError.VoiceLimitReached, ApiClient.mapServerError(429, body))
        assertEquals(AIReplyError.VoiceLimitReached, AccountTranscriptionTransport.map(ApiError.VoiceLimitReached))
        assertEquals(AIReplyError.ServiceUnavailable, AccountTranscriptionTransport.map(ApiError.NotFound))
        assertEquals(AIReplyError.ServiceUnavailable, AccountTranscriptionTransport.map(ApiError.InvalidRequest))
        assertEquals(AIReplyError.ServiceUnavailable, AccountTranscriptionTransport.map(ApiError.MalformedResponse))
        assertEquals(AIReplyError.RateLimited, AccountTranscriptionTransport.map(ApiError.RateLimited(5)))
        assertEquals(AIReplyError.Cancelled, AccountTranscriptionTransport.map(ApiError.Cancelled))
    }

    // -------------------------------------------------------------- service

    private class ScriptedTransport(private val answer: suspend () -> Transcript) : TranscriptionTransport {
        val requests = mutableListOf<TranscriptionRequest>()

        override suspend fun transcribe(request: TranscriptionRequest): Transcript {
            requests += request
            return answer()
        }
    }

    private var signedIn = true
    private var consent = true
    private var consentHandedOver = 0
    private val configuration = AIConfiguration(
        hasLegalConsent = { consent },
        onConsentRequired = { consentHandedOver++ },
        isAccountSignedIn = { signedIn }
    )

    private fun service(
        transport: TranscriptionTransport,
        override: (() -> TranscriptionTransport?)? = null
    ) = VoiceTranscriptionService(
        configuration = configuration,
        accountTransport = { transport },
        transportOverride = override
    )

    private fun failureOf(block: suspend () -> Unit): AIReplyError = runBlocking {
        try {
            block()
            fail("expected a failure")
            error("unreachable")
        } catch (exception: AIReplyException) {
            exception.error
        }
    }

    @Test
    fun `offered only when the server announced it, or a DEBUG mock stands in`() {
        AILimits.install(InMemoryPreferences())
        val account = ScriptedTransport { Transcript("x") }
        assertFalse(service(account).isOffered)

        AILimits.apply(ServerConfigDto(features = ServerFeaturesDto(voiceTranscription = true)))
        assertTrue(service(account).isOffered)

        AILimits.apply(ServerConfigDto())
        assertFalse("a server without a features block offers none", service(account).isOffered)
        assertTrue(service(account) { DebugTranscribeMock() }.isOffered)
        assertFalse("the developer switch is off", service(account) { null }.isOffered)
    }

    @Test
    fun `readiness says what stands in the way without throwing`() {
        val account = ScriptedTransport { Transcript("x") }
        assertNull(service(account).readiness())

        consent = false
        assertEquals(AIReplyError.ConsentRequired, service(account).readiness())
        assertEquals("the app is told to ask again", 1, consentHandedOver)

        signedIn = false
        assertEquals("sign-in comes first", AIReplyError.AuthenticationFailed, service(account).readiness())
        assertEquals(1, consentHandedOver)

        assertNull("a DEBUG mock needs neither", service(account) { DebugTranscribeMock() }.readiness())
    }

    @Test
    fun `the words come back trimmed, as sent`() = runBlocking {
        val account = ScriptedTransport { Transcript("  Ответь клиенту вежливо.\n", listOf("ru"), 4_000) }
        val request = request(languages = listOf("ru"))

        assertEquals("Ответь клиенту вежливо.", service(account).transcribe(request))
        assertSame(request, account.requests.single())
        assertEquals("", service(ScriptedTransport { Transcript(" \n ") }).transcribe(request))
    }

    @Test
    fun `nothing is sent before the account is ready`() {
        val account = ScriptedTransport { Transcript("x") }
        signedIn = false
        assertEquals(AIReplyError.AuthenticationFailed, failureOf { service(account).transcribe(request()) })

        signedIn = true
        consent = false
        assertEquals(AIReplyError.ConsentRequired, failureOf { service(account).transcribe(request()) })
        assertEquals(1, consentHandedOver)
        assertTrue(account.requests.isEmpty())
    }

    @Test
    fun `failures are reported like every other request`() {
        val refused = ScriptedTransport { throw AIReplyException(AIReplyError.ConsentRequired) }
        assertEquals(AIReplyError.ConsentRequired, failureOf { service(refused).transcribe(request()) })
        assertEquals("the server's refusal reaches the app", 1, consentHandedOver)

        val capped = ScriptedTransport { throw AIReplyException(AIReplyError.VoiceLimitReached) }
        assertEquals(AIReplyError.VoiceLimitReached, failureOf { service(capped).transcribe(request()) })
        assertEquals(1, consentHandedOver)

        val offline = ScriptedTransport { throw UnknownHostException("example.test") }
        assertEquals(AIReplyError.Offline, failureOf { service(offline).transcribe(request()) })
        val broken = ScriptedTransport { throw IllegalStateException("bug") }
        assertEquals(AIReplyError.ServiceUnavailable, failureOf { service(broken).transcribe(request()) })
    }

    @Test
    fun `cancelling abandons the request and reports nothing`() = runTest {
        val started = CompletableDeferred<Unit>()
        val waiting = ScriptedTransport {
            started.complete(Unit)
            awaitCancellation()
        }
        consent = true
        val call = async { service(waiting).transcribe(request()) }
        runCurrent()
        assertTrue(started.isCompleted)

        call.cancel()
        advanceUntilIdle()
        assertTrue(call.isCancelled)
        try {
            call.await()
            fail("expected the cancellation")
        } catch (expected: CancellationException) {
            // Not an AIReplyException: Stop is not an error to show.
        }
        assertEquals(0, consentHandedOver)
    }

    // ----------------------------------------------------------- DEBUG mock

    @Test
    fun `the DEBUG mock answers in the first hint's language`() = runTest {
        val mock = DebugTranscribeMock()
        val audio = recording()
        val texts = listOf(listOf("kk", "ru"), listOf("ru"), listOf("en", "kk"), listOf("uz"), emptyList())
            .map { hints -> mock.transcribe(request(audio, languages = hints)).text }
        assertEquals(
            listOf(
                "Сәлеметсіз бе! Бүгін кездесуге келе алмаймын, кешіріңіз.",
                "Ответь клиенту вежливо и коротко, что заказ будет готов завтра.",
                "Reply politely that I cannot attend the meeting today.",
                "Ответь клиенту вежливо и коротко, что заказ будет готов завтра.",
                "Ответь клиенту вежливо и коротко, что заказ будет готов завтра."
            ),
            texts
        )
    }

    @Test
    fun `the DEBUG mock takes about a second and can be stopped`() = runTest {
        val mock = DebugTranscribeMock()
        val audio = recording()

        val answer = async { mock.transcribe(request(audio, languages = listOf("en"))) }
        advanceTimeBy(DebugTranscribeMock.DELAY_MS - 1)
        runCurrent()
        assertFalse(answer.isCompleted)
        advanceTimeBy(1)
        runCurrent()
        assertEquals(listOf("en"), answer.await().languages)
        assertEquals(1_200L, DebugTranscribeMock.DELAY_MS)

        val stopped = async { mock.transcribe(request(audio)) }
        runCurrent()
        stopped.cancel()
        advanceUntilIdle()
        assertTrue(stopped.isCancelled)
    }

    @Test
    fun `the DEBUG mock stands in without an account`() = runBlocking {
        signedIn = false
        val account = ScriptedTransport { Transcript("never") }
        val text = service(account) { DebugTranscribeMock() }.transcribe(request(languages = listOf("kk")))
        assertEquals("Сәлеметсіз бе! Бүгін кездесуге келе алмаймын, кешіріңіз.", text)
        assertTrue(account.requests.isEmpty())
    }

    // ------------------------------------------------------------- the flag

    @Test
    fun `the flag comes from config, unknown keys and all, and is kept`() = runBlocking {
        val server = FakeServer {
            FakeResponse(
                200,
                """{"locales":["kk","ru","en","uz"],"timezone":"Asia/Almaty","max_source_characters":400,""" +
                    """"max_instruction_length":400,"payment_mode":"off","voice":{"max_seconds":40},""" +
                    """"features":{"email_otp":true,"instruction_polish":true,"voice_transcription":true,""" +
                    """"voice_max_seconds":40,"something_new":{"nested":[1,2]}}}"""
            )
        }
        val accountService = AccountService(
            session = sessionOn(server, FakeCredentials()),
            baseUrlProvider = { "https://example.test" },
            deviceDescriptor = { DeviceDescriptor("device-1", "android", "1.0", "15", "en", "Asia/Almaty") },
            clientFactory = { baseUrl -> ApiClient(baseUrl, openConnection = server::open) }
        )

        val config = accountService.serverConfig()
        assertTrue(config.features!!.voiceTranscription)
        assertTrue(config.features!!.instructionPolish)
        assertFalse(ServerFeaturesDto().voiceTranscription)

        val file = InMemoryPreferences()
        AILimits.install(file)
        assertFalse(AILimits.features.voiceTranscription)

        AILimits.apply(config)
        AILimits.install(file)
        assertTrue("read back from the file", AILimits.features.voiceTranscription)

        AILimits.apply(ServerConfigDto())
        assertFalse(AILimits.features.voiceTranscription)
    }
}
