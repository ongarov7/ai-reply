package kz.yerek.aireply.data.account

import java.io.File
import java.io.OutputStream
import java.util.UUID

/**
 * A `multipart/form-data` body (RFC 7578), written straight to a stream.
 *
 * Көп бөлікті дене: файл жадқа жиналмай, дискіден тікелей жіберіледі.
 *
 * Built for one job - a voice recording and a few short fields beside it - and
 * kept free of Android, so the exact bytes are pinned by a JVM test rather
 * than discovered by a server refusing them.
 *
 *  * Every line ends in CRLF, as the format requires; the body ends with the
 *    closing boundary and its own CRLF.
 *  * The boundary is random per body, so it cannot occur inside the audio by
 *    accident.
 *  * [contentLength] is exact before anything is written, which lets
 *    [ApiClient.postMultipart] stream the file in fixed-length mode.
 *  * A file is opened when the body is written, every time it is written: a
 *    retry after a token refresh sends the whole recording again.
 */
class MultipartBody(
    val parts: List<Part>,
    val boundary: String = randomBoundary()
) {

    sealed interface Part {
        val name: String

        /** A short text field, sent as UTF-8. */
        data class Field(override val name: String, val value: String) : Part

        /** A file read from disk as it is sent. [filename] is what the server is told it is called. */
        data class FilePart(
            override val name: String,
            val file: File,
            val contentType: String,
            val filename: String = file.name
        ) : Part
    }

    /** The request's `Content-Type`, boundary included. */
    val contentType: String get() = "multipart/form-data; boundary=$boundary"

    /** Exactly what [writeTo] writes, as long as the files keep their size meanwhile. */
    val contentLength: Long
        get() = parts.sumOf { part ->
            val body = when (part) {
                is Part.Field -> part.value.toByteArray(Charsets.UTF_8).size.toLong()
                is Part.FilePart -> part.file.length()
            }
            head(part).size + body + CRLF.size
        } + closing().size

    fun writeTo(out: OutputStream) {
        for (part in parts) {
            out.write(head(part))
            when (part) {
                is Part.Field -> out.write(part.value.toByteArray(Charsets.UTF_8))
                is Part.FilePart -> part.file.inputStream().use { it.copyTo(out) }
            }
            out.write(CRLF)
        }
        out.write(closing())
        out.flush()
    }

    /** The boundary line and the part's headers, up to and including the blank line. */
    private fun head(part: Part): ByteArray = buildString {
        append("--").append(boundary).append(NEWLINE)
        append("Content-Disposition: form-data; name=\"").append(quoted(part.name)).append('"')
        if (part is Part.FilePart) append("; filename=\"").append(quoted(part.filename)).append('"')
        append(NEWLINE)
        val type = when (part) {
            is Part.Field -> FIELD_CONTENT_TYPE
            is Part.FilePart -> part.contentType
        }
        append("Content-Type: ").append(type).append(NEWLINE)
        append(NEWLINE)
    }.toByteArray(Charsets.UTF_8)

    private fun closing(): ByteArray = "--$boundary--$NEWLINE".toByteArray(Charsets.UTF_8)

    companion object {
        private const val NEWLINE = "\r\n"
        private val CRLF = NEWLINE.toByteArray(Charsets.US_ASCII)
        private const val FIELD_CONTENT_TYPE = "text/plain; charset=utf-8"

        /** 128 random bits: never part of a recording, well under the 70-character limit. */
        fun randomBoundary(): String = "AIReply-" + UUID.randomUUID().toString().replace("-", "")

        /**
         * A name or file name inside double quotes. A quote or a line break
         * would end the header early, so they are percent-encoded, as browsers
         * do (HTML's form encoding).
         */
        internal fun quoted(value: String): String =
            value.replace("\"", "%22").replace("\r", "%0D").replace("\n", "%0A")
    }
}
