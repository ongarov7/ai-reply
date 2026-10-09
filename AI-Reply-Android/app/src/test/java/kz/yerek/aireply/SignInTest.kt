package kz.yerek.aireply

import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.ApiException
import kz.yerek.aireply.data.account.EmailAddress
import kz.yerek.aireply.data.account.GoogleSignInClient
import kz.yerek.aireply.data.account.OtpCode
import kz.yerek.aireply.data.account.ResendCountdown
import kz.yerek.aireply.data.account.SignInNonce
import kz.yerek.aireply.ui.feature.account.AccountController
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * E-mail and Google sign-in: the rules the screens rely on.
 *
 * Кіру ережелері — желісіз тексеріледі. Сервер бәрібір өзі тексереді;
 * бұл тек экранның шешімдері.
 */
class SignInTest {

    // ------------------------------------------------------------ e-mail

    @Test
    fun `email is trimmed and lowercased like the server does`() {
        assertEquals("aigerim.s+tag@mail.kz", EmailAddress.normalized("  Aigerim.S+tag@Mail.KZ \n"))
    }

    @Test
    fun `continue needs something that looks like one address`() {
        listOf("user@example.com", " User@Example.com ", "a@b.co", "x7k2@privaterelay.appleid.com").forEach {
            assertTrue(it, EmailAddress.isPlausible(it))
        }
        listOf(
            "", "   ", "user", "user@", "@example.com", "user@localhost", "us er@example.com",
            "user@example..com", "user@.example.com", "user@example.com.", "a@b@c.com"
        ).forEach {
            assertFalse(it, EmailAddress.isPlausible(it))
        }
    }

    // -------------------------------------------------------------- code

    @Test
    fun `code field keeps four ascii digits`() {
        assertEquals("leading zeros are part of the code", "0384", OtpCode.sanitize("0384"))
        assertEquals("4821", OtpCode.sanitize("48 21"))
        assertEquals("a pasted sentence still yields the code", "4821", OtpCode.sanitize("Your code: 4821"))
        assertEquals("1234", OtpCode.sanitize("123456"))
        assertEquals("12", OtpCode.sanitize("12a"))
        assertEquals("other scripts' digits are refused like the server does", "", OtpCode.sanitize("٤٨٢١"))
        assertEquals(4, OtpCode.LENGTH)
    }

    @Test
    fun `resend countdown rounds up and stops at zero`() {
        val now = 1_000_000L
        assertEquals(32, ResendCountdown.secondsRemaining(now + 32_000, now))
        assertEquals(1, ResendCountdown.secondsRemaining(now + 200, now))
        assertEquals(0, ResendCountdown.secondsRemaining(now, now))
        assertEquals(0, ResendCountdown.secondsRemaining(now - 5_000, now))
    }

    // ------------------------------------------------------------ google

    @Test
    fun `every google attempt gets a fresh url safe nonce`() {
        val nonces = List(50) { SignInNonce.make() }
        assertEquals(nonces.size, nonces.toSet().size)
        nonces.forEach { nonce ->
            assertEquals(43, nonce.length)
            assertTrue(nonce, nonce.none { it in "+/= " })
        }
    }

    @Test
    fun `google is offered only with a real client id`() {
        assertTrue(GoogleSignInClient.isValidClientId("1234-abc.apps.googleusercontent.com"))
        assertFalse("an empty build setting hides the button", GoogleSignInClient.isValidClientId(""))
        assertFalse(GoogleSignInClient.isValidClientId(".apps.googleusercontent.com"))
        assertFalse(GoogleSignInClient.isValidClientId("1234-abc"))
        assertFalse(GoogleSignInClient.isValidClientId("1234 abc.apps.googleusercontent.com"))
    }

    // ---------------------------------------------------------- messages

    @Test
    fun `every sign-in failure has its own message`() {
        val errors = listOf(
            ApiError.InvalidOtp(2), ApiError.OtpExpired, ApiError.OtpAlreadyUsed, ApiError.OtpAttemptsExceeded,
            ApiError.ResendCooldown(10), ApiError.InvalidEmail, ApiError.EmailDeliveryFailed,
            ApiError.EmailInUse, ApiError.InvalidIdToken, ApiError.AuthProviderUnavailable,
            ApiError.RateLimited(30)
        )
        val messages = errors.map { AccountController.messageFor(ApiException(it)) }
        assertEquals("each failure needs its own wording: $messages", messages.size, messages.toSet().size)
        assertFalse(messages.contains(R.string.account_error_generic))
    }

    @Test
    fun `a token the server refused reads apart from a failure on the phone`() {
        assertEquals(R.string.account_error_provider_rejected,
            AccountController.messageFor(ApiException(ApiError.InvalidIdToken)))
    }

    @Test
    fun `only a spent code empties the field`() {
        listOf(ApiError.InvalidOtp(1), ApiError.OtpExpired, ApiError.OtpAlreadyUsed, ApiError.OtpAttemptsExceeded)
            .forEach { assertTrue("$it", AccountController.codeIsSpent(ApiException(it))) }
        listOf(ApiError.Offline, ApiError.TimedOut, ApiError.Server, ApiError.RateLimited(null))
            .forEach { assertFalse("a network failure must keep the typed code", AccountController.codeIsSpent(ApiException(it))) }
    }
}
