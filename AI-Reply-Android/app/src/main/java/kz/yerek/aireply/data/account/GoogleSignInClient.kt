package kz.yerek.aireply.data.account

import android.annotation.SuppressLint
import android.app.Activity
import android.content.Context
import androidx.credentials.ClearCredentialStateRequest
import androidx.credentials.CredentialManager
import androidx.credentials.CustomCredential
import androidx.credentials.GetCredentialRequest
import androidx.credentials.exceptions.ClearCredentialException
import androidx.credentials.exceptions.GetCredentialCancellationException
import androidx.credentials.exceptions.GetCredentialException
import androidx.credentials.exceptions.NoCredentialException
import com.google.android.libraries.identity.googleid.GetSignInWithGoogleOption
import com.google.android.libraries.identity.googleid.GoogleIdTokenCredential
import com.google.android.libraries.identity.googleid.GoogleIdTokenParsingException

/**
 * "Continue with Google" through Android's Credential Manager.
 *
 * Google арқылы кіру: ID token-ді сервер Google кілттерімен тексереді.
 *
 * The app trusts nothing Google's sheet returns. It forwards the ID token and
 * the nonce it asked Google to embed, and the server checks signature,
 * issuer, audience, expiry and nonce against Google's published keys.
 *
 * [webClientId] is the OAuth client of type "Web application" from Google Cloud
 * Console (not a secret; it ships inside every APK). It becomes the token's
 * audience, which is why the server lists it in GOOGLE_CLIENT_ID_WEB. Empty
 * means this build was made without one, and the button stays hidden.
 */
class GoogleSignInClient(
    private val appContext: Context,
    private val webClientId: String
) {

    sealed interface Result {
        data class Success(val idToken: String, val nonce: String) : Result
        /** The user closed Google's sheet. Not an error. */
        data object Cancelled : Result
        /** This build has no OAuth client id. */
        data object NotConfigured : Result
        /** No Google account can be used here (none added, or no Google Play services). */
        data object Unavailable : Result
        data object Failed : Result
    }

    val isConfigured: Boolean get() = isValidClientId(webClientId)

    /**
     * Shows Google's account picker and returns an ID token bound to a fresh
     * nonce. [activity] hosts the picker.
     *
     * The lint check below looks for a Java-style reference to
     * GoogleIdTokenCredential and misses the Kotlin companion access; the
     * credential is parsed with GoogleIdTokenCredential.createFrom right here.
     */
    @SuppressLint("CredentialManagerSignInWithGoogle")
    suspend fun requestIdToken(activity: Activity): Result {
        if (!isConfigured) return Result.NotConfigured
        val nonce = SignInNonce.make()
        val option = GetSignInWithGoogleOption.Builder(webClientId)
            .setNonce(nonce)
            .build()
        val request = GetCredentialRequest.Builder()
            .addCredentialOption(option)
            .build()

        return try {
            val credential = CredentialManager.create(activity).getCredential(activity, request).credential
            if (credential is CustomCredential && credential.type in GOOGLE_ID_TOKEN_TYPES) {
                val google: GoogleIdTokenCredential = GoogleIdTokenCredential.createFrom(credential.data)
                if (google.idToken.isBlank()) Result.Failed else Result.Success(google.idToken, nonce)
            } else {
                Result.Failed
            }
        } catch (cancelled: GetCredentialCancellationException) {
            Result.Cancelled
        } catch (none: NoCredentialException) {
            Result.Unavailable
        } catch (failure: GetCredentialException) {
            Result.Failed
        } catch (malformed: GoogleIdTokenParsingException) {
            Result.Failed
        }
    }

    /**
     * Forgets the Google account chosen last time, so the next sign-in asks
     * again instead of silently reusing it. Best effort.
     */
    suspend fun signOut() {
        if (!isConfigured) return
        try {
            CredentialManager.create(appContext).clearCredentialState(ClearCredentialStateRequest())
        } catch (ignored: ClearCredentialException) {
            // Nothing to clear, or no provider on this device.
        }
    }

    companion object {
        private const val SUFFIX = ".apps.googleusercontent.com"

        /** The button flow may mark its credential with the "Sign in with Google" subtype. */
        private val GOOGLE_ID_TOKEN_TYPES = setOf(
            GoogleIdTokenCredential.TYPE_GOOGLE_ID_TOKEN_CREDENTIAL,
            GoogleIdTokenCredential.TYPE_GOOGLE_ID_TOKEN_SIWG_CREDENTIAL
        )

        fun isValidClientId(value: String): Boolean =
            value.length > SUFFIX.length && value.endsWith(SUFFIX) && value.none(Char::isWhitespace)
    }
}
