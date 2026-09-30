package kz.yerek.aireply.support

import java.io.ByteArrayOutputStream
import java.io.IOException
import java.io.InputStream
import java.io.OutputStream
import java.net.HttpURLConnection
import java.net.URL
import java.util.concurrent.CopyOnWriteArrayList

/** One request as the fake server saw it. */
data class RecordedRequest(
    val method: String,
    val path: String,
    val headers: Map<String, String>,
    val body: String
) {
    fun header(name: String): String? = headers.entries.firstOrNull { it.key.equals(name, ignoreCase = true) }?.value
}

data class FakeResponse(
    val status: Int,
    val body: String = "",
    val headers: Map<String, String> = emptyMap()
)

/**
 * An HTTP server in memory, reached through ApiClient's connection factory.
 * [handler] answers each request, or throws an IOException to simulate a
 * request that got no answer at all.
 */
class FakeServer(private val handler: (RecordedRequest) -> FakeResponse) {

    val requests = CopyOnWriteArrayList<RecordedRequest>()

    fun open(url: URL): HttpURLConnection = Connection(url)

    fun count(path: String): Int = requests.count { it.path == path }

    private inner class Connection(url: URL) : HttpURLConnection(url) {
        private val sent = ByteArrayOutputStream()
        private var answer: FakeResponse? = null

        private fun respond(): FakeResponse {
            answer?.let { return it }
            val headers = requestProperties.mapValues { (_, values) -> values.joinToString(",") }
            val request = RecordedRequest(requestMethod, url.path, headers, sent.toString(Charsets.UTF_8.name()))
            requests += request
            return handler(request).also { answer = it }
        }

        override fun connect() = Unit
        override fun disconnect() = Unit
        override fun usingProxy(): Boolean = false
        override fun getOutputStream(): OutputStream = sent
        override fun getResponseCode(): Int = respond().status

        override fun getInputStream(): InputStream {
            val response = respond()
            if (response.status >= 400) throw IOException("HTTP ${response.status}")
            return response.body.byteInputStream(Charsets.UTF_8)
        }

        override fun getErrorStream(): InputStream? {
            val response = respond()
            return if (response.status >= 400) response.body.byteInputStream(Charsets.UTF_8) else null
        }

        override fun getHeaderField(name: String?): String? =
            respond().headers.entries.firstOrNull { it.key.equals(name, ignoreCase = true) }?.value
    }

    companion object {
        fun envelope(code: String, requestId: String? = null): String {
            val id = requestId?.let { ""","request_id":"$it"""" }.orEmpty()
            return """{"error":{"code":"$code","message":"test"$id}}"""
        }
    }
}
