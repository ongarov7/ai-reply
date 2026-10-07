package kz.yerek.aireply.keyboard.autocorrect

import android.content.res.AssetManager
import java.io.InputStream

/**
 * The dictionaries shipped in the APK under `assets/dictionaries/`, generated
 * by `tools/dictionaries/build_dictionaries.py`.
 */
class AssetDictionarySource(private val assets: AssetManager) : DictionarySource {

    override fun open(fileName: String): InputStream =
        assets.open("$FOLDER/$fileName", AssetManager.ACCESS_STREAMING)

    private companion object {
        const val FOLDER = "dictionaries"
    }
}
