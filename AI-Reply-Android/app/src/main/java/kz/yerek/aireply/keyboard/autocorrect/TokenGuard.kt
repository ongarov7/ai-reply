package kz.yerek.aireply.keyboard.autocorrect

import kz.yerek.aireply.core.lang.KeyboardLanguage

/**
 * Tokens autocorrect never touches (DESIGN §7.3): handles, addresses, numbers,
 * acronyms, brand spellings, and text in another script than the layout.
 *
 * Түзетілмейтін сөздер: сілтеме, сан, бас әріптер, басқа әліпби.
 *
 * Learned words and fields that switch suggestions off are guarded too, by the
 * engine and by the keyboard; this type only judges the characters.
 */
object TokenGuard {

    /**
     * Characters that make a chunk look like an address, a handle, a path or
     * code. They cover `://` and `www.` as well.
     */
    private const val MARKERS = "@#/\\:._=&"

    /**
     * True when [word] must stay as typed. [before] is the text before it;
     * only its last whitespace-delimited chunk counts, so `@` in `@превт` or
     * `site.` in `site.ru` protects the word that follows.
     */
    fun isProtected(word: String, before: String, language: KeyboardLanguage): Boolean {
        var letters = 0
        word.forEachIndexed { index, character ->
            when {
                character.isDigit() || character in MARKERS -> return true
                character.isLetter() -> {
                    // One script per layout: Latin on the English one, Cyrillic
                    // on the others. Mixed words fail this as well.
                    if (!language.writes(character)) return true
                    // A capital after the first character: iPhone, McDonald,
                    // and with it every ALL-CAPS word of two letters or more.
                    if (index > 0 && character.isUpperCase()) return true
                    letters++
                }
            }
        }
        if (letters < 2 && !(language == KeyboardLanguage.ENGLISH && word.lowercase() == "i")) return true
        return before.takeLastWhile { !it.isWhitespace() }.any { it in MARKERS }
    }

    private fun KeyboardLanguage.writes(letter: Char): Boolean {
        val script = Character.UnicodeScript.of(letter.code)
        return when (this) {
            KeyboardLanguage.ENGLISH -> script == Character.UnicodeScript.LATIN
            KeyboardLanguage.RUSSIAN, KeyboardLanguage.KAZAKH -> script == Character.UnicodeScript.CYRILLIC
        }
    }
}
