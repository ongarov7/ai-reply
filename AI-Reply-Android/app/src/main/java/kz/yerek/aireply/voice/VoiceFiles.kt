package kz.yerek.aireply.voice

import java.io.File
import java.util.UUID

/**
 * Where recordings live on the phone, and how they are cleared away.
 *
 * Дауыс файлдары телефонда ұзақ жатпайды.
 *
 * A recording is the user's voice. It is written to the app's cache only to be
 * uploaded once, and [VoiceRecorder.discard] deletes it. A keyboard that was
 * killed mid-upload never gets to discard, so [sweep] - run when the keyboard
 * starts - removes whatever such a run left behind.
 */
object VoiceFiles {

    /** Every file the recorder writes starts with this, and nothing else does. */
    const val PREFIX = "voice-"

    const val MIME_M4A = "audio/mp4"
    const val MIME_WAV = "audio/wav"

    /** A fresh file name: `voice-<uuid>.<extension>`. */
    fun newFile(directory: File, extension: String): File =
        File(directory, "$PREFIX${UUID.randomUUID()}.$extension")

    /**
     * Deletes `voice-*` files in [directory] last modified at least
     * [olderThanMs] before [nowMs], and returns how many went. Anything newer
     * may belong to a recording still being uploaded, so it stays. Never
     * throws: a file that will not go is left for the next sweep.
     */
    fun sweep(
        directory: File,
        nowMs: Long = System.currentTimeMillis(),
        olderThanMs: Long = 60 * 60 * 1000L
    ): Int = runCatching {
        directory.listFiles()?.count { file ->
            runCatching {
                file.isFile &&
                    file.name.startsWith(PREFIX) &&
                    nowMs - file.lastModified() >= olderThanMs &&
                    file.delete()
            }.getOrDefault(false)
        } ?: 0
    }.getOrDefault(0)
}
