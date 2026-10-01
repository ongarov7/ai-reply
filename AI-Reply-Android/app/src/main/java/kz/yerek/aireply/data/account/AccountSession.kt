package kz.yerek.aireply.data.account

import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

/**
 * The stored session as [AccountSession] needs it. [AccountCredentials] is the
 * real one; tests use a map.
 */
interface SessionCredentials {
    val accessToken: String?
    val refreshToken: String?
    val isSignedIn: Boolean
    val isAccessTokenFresh: Boolean
    var deviceId: String
    var displayIdentifier: String?
    fun store(accessToken: String, refreshToken: String, expiresInSeconds: Int)
    fun clear()
}

/** Authenticated calls with one refresh-and-retry, for code outside this package. */
interface SessionAuth {
    val isSignedIn: Boolean

    /** Runs [work] with a valid access token; a 401 refreshes once and retries once. */
    suspend fun <T> authenticated(work: suspend (String) -> T): T
}

/**
 * Owns the token pair and hands out a usable access token.
 *
 * Access токен 15 минут жарамды; ескіргенде бір-ақ рет жаңартылады.
 *
 * WHY THE MUTEX. The app and the keyboard live in one process and can both ask
 * for a token at the same moment. Two parallel refreshes would rotate the
 * refresh token twice, and the server treats the second use of a rotated token
 * as theft and kills the whole session. One lock, one in-flight refresh, and
 * that cannot happen.
 */
class AccountSession(
    private val credentials: SessionCredentials,
    private val baseUrlProvider: () -> String?,
    private val deviceDescriptor: () -> DeviceDescriptor,
    /** How a request is sent; a test seam. */
    private val clientFactory: (baseUrl: String, scope: HeaderScope) -> ApiClient =
        { baseUrl, scope -> ApiClient(baseUrl, scope = scope) }
) : SessionAuth {

    private val refreshMutex = Mutex()

    override val isSignedIn: Boolean get() = credentials.isSignedIn

    /** A token that is valid right now, refreshing first when needed. */
    suspend fun accessToken(): String {
        credentials.accessToken?.let { token ->
            if (credentials.isAccessTokenFresh) return token
        }
        return refreshAccessToken(rejected = null)
    }

    /** The stored access token if it is still fresh; never refreshes. */
    fun freshAccessTokenOrNull(): String? =
        credentials.accessToken?.takeIf { credentials.isAccessTokenFresh }

    /**
     * A new access token.
     *
     * [rejected] is the token the server just answered 401 to. It must never be
     * handed out again, however fresh its expiry says it is: returning it made
     * the one retry in [authenticated] fail the same way. A different fresh
     * token means another coroutine refreshed while this one waited for the
     * lock, and taking its result is both correct and one fewer rotation.
     */
    suspend fun refreshAccessToken(rejected: String?): String = refreshMutex.withLock {
        credentials.accessToken?.let { token ->
            if (credentials.isAccessTokenFresh && token != rejected) return@withLock token
        }

        val baseUrl = baseUrlProvider() ?: ApiError.InvalidRequest.raise()
        val refreshToken = credentials.refreshToken ?: ApiError.Unauthorized.raise()
        // The keyboard refreshes too, so this carries no installation or
        // session id.
        val client = clientFactory(baseUrl, HeaderScope.KEYBOARD)

        val payload = client.json.encodeToString(
            RefreshRequest.serializer(),
            RefreshRequest(refreshToken, deviceDescriptor())
        )

        val body = try {
            client.request("POST", "api/v1/auth/refresh", payload)
        } catch (exception: ApiException) {
            // The refresh token is gone, rotated or revoked. Nothing local can
            // fix that, so the session is cleared and the UI asks for sign-in —
            // unless another session was stored meanwhile, which is not ours
            // to clear.
            if (exception.error is ApiError.Unauthorized && credentials.refreshToken == refreshToken) {
                credentials.clear()
            }
            throw exception
        }

        val tokens = runCatching {
            client.json.decodeFromString(TokenPairDto.serializer(), body)
        }.getOrElse { ApiError.MalformedResponse.raise() }

        // The session ended or was replaced while the request was out (a
        // sign-out, a revoked session, another account signing in). Storing
        // the new pair now would sign a signed-out app back in, so it is
        // dropped and the caller is told the session is gone. Nothing is
        // cleared here: whatever is stored now is not this session's.
        if (credentials.refreshToken != refreshToken) ApiError.Unauthorized.raise()

        credentials.store(tokens.accessToken, tokens.refreshToken, tokens.expiresIn)
        tokens.accessToken
    }

    /** Stores a freshly issued session. */
    fun adopt(session: AccountSessionDto) {
        credentials.store(session.accessToken, session.refreshToken, session.expiresIn)
        if (session.deviceId.isNotEmpty()) credentials.deviceId = session.deviceId
        credentials.displayIdentifier = session.user.identifier
    }

    /**
     * Ends the session. The server call is best effort: the local tokens are
     * dropped either way, so a user on a plane can still sign out.
     *
     * The tokens are read and cleared under the refresh lock: a refresh in
     * flight finishes first, so the logout names the refresh token that is
     * current (the rotated one, if it rotated), and nothing stores a new pair
     * after the sign-out.
     *
     * The logout request carries `X-Installation-ID` (APP scope), which is how
     * the server detaches this installation from the account at once.
     */
    suspend fun signOut() {
        val baseUrl = baseUrlProvider()
        val refreshToken = refreshMutex.withLock {
            credentials.refreshToken.also { credentials.clear() }
        }

        if (baseUrl == null || refreshToken == null) return
        val client = clientFactory(baseUrl, HeaderScope.APP)
        runCatching {
            client.request(
                "POST", "api/v1/auth/logout",
                client.json.encodeToString(LogoutRequest.serializer(), LogoutRequest(refreshToken))
            )
        }
    }

    /**
     * Runs an authenticated call, refreshing once if the server rejects the
     * token. One retry, never a loop.
     */
    override suspend fun <T> authenticated(work: suspend (String) -> T): T {
        val token = accessToken()
        return try {
            work(token)
        } catch (exception: ApiException) {
            if (exception.error !is ApiError.Unauthorized) throw exception
            work(refreshAccessToken(rejected = token))
        }
    }
}
