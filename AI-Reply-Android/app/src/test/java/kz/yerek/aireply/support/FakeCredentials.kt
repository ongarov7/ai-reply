package kz.yerek.aireply.support

import kz.yerek.aireply.data.account.AccountSession
import kz.yerek.aireply.data.account.ApiClient
import kz.yerek.aireply.data.account.DeviceDescriptor
import kz.yerek.aireply.data.account.SessionCredentials

/** The stored session as a few fields. [fresh] is what the expiry claims. */
class FakeCredentials(
    var access: String? = null,
    var refresh: String? = null,
    var fresh: Boolean = true
) : SessionCredentials {
    var stores = 0
    var clears = 0

    override val accessToken: String? get() = access
    override val refreshToken: String? get() = refresh
    override val isSignedIn: Boolean get() = refresh != null
    override val isAccessTokenFresh: Boolean get() = access != null && fresh
    override var deviceId: String = "device-1"
    override var displayIdentifier: String? = null

    override fun store(accessToken: String, refreshToken: String, expiresInSeconds: Int) {
        access = accessToken
        refresh = refreshToken
        fresh = true
        stores++
    }

    override fun clear() {
        access = null
        refresh = null
        clears++
    }
}

/** A real AccountSession whose requests go to [server]. */
fun sessionOn(
    server: FakeServer,
    credentials: FakeCredentials,
    installationId: () -> String? = { null }
): AccountSession = AccountSession(
    credentials = credentials,
    baseUrlProvider = { "https://example.test" },
    deviceDescriptor = {
        DeviceDescriptor("device-1", "android", "1.0", "15", "en", "Asia/Almaty")
    },
    installationId = installationId,
    clientFactory = { baseUrl -> ApiClient(baseUrl, openConnection = server::open) }
)

/** `/api/v1/auth/refresh` answering with [access] / [refresh]. */
fun tokenPair(access: String, refresh: String): FakeResponse =
    FakeResponse(200, """{"access_token":"$access","refresh_token":"$refresh","expires_in":900}""")
