package kz.yerek.aireply.keyboard.autocorrect

/**
 * Exact English fixes that apply even though the typed form is in the
 * dictionary (DESIGN §7.5): the apostrophe-less contractions and `i`.
 *
 * Ағылшынша дайын түзетулер: dont → don't, i → I.
 *
 * Deliberately absent: `lets`, `its`, `ill`, `id`, `well`, `were` - each is a
 * word in its own right.
 *
 * Classic misspellings (`teh`, `thier`) are not listed: the dictionaries are
 * generated without them (`tools/dictionaries/supplement/en_typos.txt`), so the
 * distance rules fix them like any other typo, exactly as on iOS.
 */
internal object EnglishReplacements {

    private val TABLE: Map<String, String> = mapOf(
        "i" to "I",
        "dont" to "don't", "cant" to "can't", "wont" to "won't", "im" to "I'm", "ive" to "I've",
        "isnt" to "isn't", "doesnt" to "doesn't", "didnt" to "didn't", "wasnt" to "wasn't",
        "werent" to "weren't", "arent" to "aren't", "havent" to "haven't", "hasnt" to "hasn't",
        "hadnt" to "hadn't", "couldnt" to "couldn't", "wouldnt" to "wouldn't", "shouldnt" to "shouldn't",
        "thats" to "that's", "theyre" to "they're", "youre" to "you're", "whats" to "what's",
        "theres" to "there's", "heres" to "here's", "youll" to "you'll", "youve" to "you've",
        "weve" to "we've", "theyll" to "they'll", "theyve" to "they've", "hes" to "he's", "shes" to "she's"
    )

    /** The fix for [key], a lowercased typed word, as it should be shown. */
    operator fun get(key: String): String? = TABLE[key]

    /**
     * True for a typed form with a better spelling in the table (`dont`),
     * which is therefore never offered as a candidate or a completion. (`i` is
     * just its own display form.)
     */
    fun isReplaced(key: String): Boolean = TABLE[key]?.lowercase()?.let { it != key } == true
}
