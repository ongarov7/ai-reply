package kz.yerek.aireply.data.account

/**
 * Account transitions, for the parts of the app that follow them (the push
 * installation, diagnostics). Every method has a default, so an observer
 * implements only what it needs; all are called on the caller's thread and
 * must return quickly.
 */
interface AccountObserver {

    /** `GET /api/v1/config` answered; null features means an old server. */
    fun onServerFeatures(features: ServerFeaturesDto?) {}

    /** A sign-in completed (e-mail code or Google). */
    fun onSignedIn(userId: String) {}

    /** The account was loaded again (`GET /api/v1/me`). */
    fun onAccountLoaded(userId: String) {}

    /**
     * The session ended. [userInitiated]: the user tapped Sign out, as opposed
     * to a session the server revoked.
     */
    fun onSignedOut(userInitiated: Boolean) {}

    /**
     * A sign-in failed inside the provider's own SDK, before anything reached
     * the server. [errorCode] is a short machine code.
     */
    fun onSignInFailedLocally(method: String, errorCode: String) {}
}
