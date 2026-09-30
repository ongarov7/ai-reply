package kz.yerek.aireply.data.account

import java.security.SecureRandom
import java.util.Base64
import java.util.Locale

/**
 * E-mail input as the app sees it. The server normalizes and validates for
 * real; this only decides when Continue can be tapped.
 *
 * Пошта: бос орынсыз, кіші әріппен. Нақты тексеру — серверде.
 */
object EmailAddress {

    /** Trimmed and lowercased, the same rule the server applies. */
    fun normalized(raw: String): String = raw.trim().lowercase(Locale.ROOT)

    /** Looks like one address: something@domain.tld, no spaces. */
    fun isPlausible(raw: String): Boolean {
        val value = normalized(raw)
        if (value.isEmpty() || value.length > 254 || value.any(Char::isWhitespace)) return false
        val parts = value.split('@')
        if (parts.size != 2 || parts[0].isEmpty()) return false
        val domain = parts[1]
        return domain.contains('.') && !domain.startsWith('.') && !domain.endsWith('.') && !domain.contains("..")
    }
}

/**
 * The e-mail code: exactly four ASCII digits.
 *
 * Код — тек 4 ASCII цифр.
 */
object OtpCode {

    const val LENGTH = 4

    /**
     * The first four ASCII digits of whatever was typed, pasted or autofilled:
     * "Code: 4821" becomes "4821", and a leading zero stays. Other scripts'
     * digits ("٤٨٢١") are refused here because the server refuses them too.
     */
    fun sanitize(raw: String): String = raw.filter { it in '0'..'9' }.take(LENGTH)
}

/**
 * The countdown next to "Resend code". Display only: the server enforces the
 * wait and says how long it is.
 */
object ResendCountdown {

    /** Whole seconds until a new code may be requested, rounded up; 0 once it may. */
    fun secondsRemaining(untilMillis: Long, nowMillis: Long): Int {
        val left = untilMillis - nowMillis
        return if (left <= 0) 0 else ((left + 999) / 1000).toInt()
    }
}

/**
 * The single-use value a Google ID token is bound to.
 *
 * Every attempt gets a fresh one; the server rejects a token whose `nonce`
 * claim is not the one the app sends next to it, so a token lifted from
 * somewhere else cannot be replayed.
 */
object SignInNonce {

    private val random = SecureRandom()

    /** 32 random bytes, URL-safe base64 without padding (43 characters). */
    fun make(): String {
        val bytes = ByteArray(32).also(random::nextBytes)
        return Base64.getUrlEncoder().withoutPadding().encodeToString(bytes)
    }
}
