#!/usr/bin/env python3
"""Builds the on-device autocorrect dictionaries for the AI Reply keyboards.

Input: word-frequency lists from the Leipzig Corpora Collection (CC BY),
downloaded from https://wortschatz.uni-leipzig.de/en/download :

    rus-ru_web-public_2019_1M      (Russian, web)
    kaz_newscrawl_2016_1M          (Kazakh, news)
    kaz_wikipedia_2021_300K        (Kazakh, Wikipedia)
    eng-com_web-public_2018_1M     (English, web)

plus the hand-written lists in supplement/.

Output, per language (en, ru, kk), written to both apps:

    <lang>.words   UTF-8 text. Lines starting with '#' are comments. Every
                   other line is one word, most frequent first, so the line
                   index is the word's rank. Proper nouns keep their capital.
    <lang>.known   Bloom filter of every word form seen at least twice
                   (lowercased), used only to answer "is this a real word?".

Bloom filter format (little-endian):
    bytes 0-3   magic "AIRB"
    byte  4     format version (1)
    byte  5     k, the number of hash functions
    bytes 6-7   reserved (0)
    bytes 8-11  n, number of inserted keys (uint32)
    bytes 12-19 m, number of bits (uint64, a multiple of 64)
    bytes 20-   the bit array; bit b lives in byte b >> 3 at position b & 7

Hashing (must match the Swift and Kotlin readers exactly):
    key   = UTF-8 bytes of the lowercased word
    h1    = FNV-1a 64 (offset 0xcbf29ce484222325, prime 0x100000001b3)
    h2    = splitmix64(h1) | 1
    bit_i = (h1 + i * h2) mod 2^64 mod m, for i in 0 ..< k

Usage:
    python3 build_dictionaries.py <leipzig-dir>
"""

import collections
import math
import os
import re
import struct
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.abspath(os.path.join(HERE, "..", ".."))
# Compact frequent-word lists for the backend's language detection
# (internal/ai/langdata). Kazakh also gets the plain spellings people type
# on a Russian layout ("калайсын" for "қалайсың").
LANGDATA = os.path.join(REPO, "ai-reply-back-end", "internal", "ai", "langdata")
LANGDATA_WORDS = 6000

OUTPUTS = [
    os.path.join(REPO, "AI-Reply", "ReplyKeyboard", "Dictionaries"),
    os.path.join(REPO, "AI-Reply-Android", "app", "src", "main", "assets", "dictionaries"),
]

MASK = (1 << 64) - 1

LANGS = {
    "ru": {
        "files": ["rus-ru_web-public_2019_1M/rus-ru_web-public_2019_1M-words.txt"],
        "pattern": re.compile(r"^[а-яё]+(-[а-яё]+)*$"),
        "candidates": 80000,
        "singles": set("авиоксуябж"),
    },
    "kk": {
        "files": [
            "kaz_newscrawl_2016_1M/kaz_newscrawl_2016_1M-words.txt",
            "kaz_wikipedia_2021_300K/kaz_wikipedia_2021_300K-words.txt",
        ],
        "pattern": re.compile(r"^[а-яёәғқңөұүһі]+(-[а-яёәғқңөұүһі]+)*$"),
        "candidates": 80000,
        "singles": set("аеоу"),
    },
    "en": {
        "files": ["eng-com_web-public_2018_1M/eng-com_web-public_2018_1M-words.txt"],
        "pattern": re.compile(r"^[a-z]+('[a-z]+)?(-[a-z]+)*$"),
        "candidates": 50000,
        "singles": set("ai"),
    },
}

# Never offered as a correction or completion. They stay "known", so typing
# them is never corrected either — the keyboard just will not suggest them.
OFFENSIVE_STEMS = {
    "ru": ["хуй", "хуё", "хуе", "хуя", "пизд", "ебат", "ебан", "ебал", "ебу", "ёбан", "ёб", "бляд", "блят",
           "сука", "суки", "мудак", "мудил", "пидор", "пидар", "залуп", "гандон", "шлюх", "дрочи", "манда"],
    "kk": ["қотақ", "амың", "сігі", "сікт", "сігу", "қатын", "шешеңді", "қотағ", "хуй", "пизд", "бляд", "сука"],
    "en": ["fuck", "shit", "bitch", "cunt", "dick", "cock", "pussy", "asshole", "nigg", "fag", "whore", "slut",
           "bastard", "wank", "twat", "retard"],
}

# Kazakh letters and the base letter people type when the special key is
# not at hand. Used to add "plain" spellings to the Kazakh known set, so a
# Kazakh word typed on a Russian layout is not "corrected" into Russian.
KK_PLAIN = str.maketrans({"ә": "а", "ғ": "г", "қ": "к", "ң": "н", "ө": "о", "ұ": "у", "ү": "у", "һ": "х", "і": "и"})

TOTAL_RE = re.compile(r"(.)\1\1")


def fnv1a64(data):
    h = 0xCBF29CE484222325
    for b in data:
        h ^= b
        h = (h * 0x100000001B3) & MASK
    return h


def splitmix64(x):
    z = (x + 0x9E3779B97F4A7C15) & MASK
    z = ((z ^ (z >> 30)) * 0xBF58476D1CE4E5B9) & MASK
    z = ((z ^ (z >> 27)) * 0x94D049BB133111EB) & MASK
    return z ^ (z >> 31)


def bloom_bits(word, k, m):
    h1 = fnv1a64(word.encode("utf-8"))
    h2 = splitmix64(h1) | 1
    return [((h1 + i * h2) & MASK) % m for i in range(k)]


def write_bloom(path, keys, false_positive=0.004):
    n = len(keys)
    m = int(math.ceil(-n * math.log(false_positive) / (math.log(2) ** 2)))
    m = ((m + 63) // 64) * 64
    k = max(1, min(16, int(round(m / n * math.log(2)))))
    bits = bytearray(m // 8)
    for key in keys:
        for b in bloom_bits(key, k, m):
            bits[b >> 3] |= 1 << (b & 7)
    with open(path, "wb") as f:
        f.write(b"AIRB")
        f.write(struct.pack("<BBHIQ", 1, k, 0, n, m))
        f.write(bits)
    return n, m, k


def read_counts(paths, lang):
    counts = collections.Counter()
    lower_initial = collections.Counter()
    for path in paths:
        with open(path, encoding="utf-8", errors="replace") as f:
            for line in f:
                parts = line.rstrip("\n").split("\t")
                if len(parts) < 3:
                    continue
                word, n = parts[1], int(parts[2])
                if lang == "kk":
                    # Older Kazakh texts type a Latin "i" for "і".
                    if re.search("[а-яәғқңөұүһі]", word.lower()) and re.fullmatch(r"[A-Za-zА-Яа-яЁёӘәҒғҚқҢңӨөҰұҮүҺһІі\-]+", word):
                        word = word.replace("i", "і").replace("I", "І")
                low = word.lower()
                counts[low] += n
                if word[:1] == low[:1]:
                    lower_initial[low] += n
    return counts, lower_initial


def read_list(name):
    path = os.path.join(HERE, "supplement", name)
    out = []
    if not os.path.exists(path):
        return out
    with open(path, encoding="utf-8") as f:
        for line in f:
            w = line.strip()
            if not w or w.startswith("#") or " " in w:
                continue
            out.append(w.lower())
    return out


def is_clean(word, lang):
    spec = LANGS[lang]
    if not spec["pattern"].match(word):
        return False
    if len(word) > 24:
        return False
    if len(word) == 1 and word not in spec["singles"]:
        return False
    if TOTAL_RE.search(word):
        return False
    return True


def offensive(word, lang):
    return any(stem in word for stem in OFFENSIVE_STEMS[lang])


def build(leipzig_dir, lang):
    spec = LANGS[lang]
    counts, lower_initial = read_counts([os.path.join(leipzig_dir, p) for p in spec["files"]], lang)
    typos = set(read_list(lang + "_typos.txt"))
    if lang == "kk":
        # The Russian layout also trusts the Kazakh known set (and vice versa).
        typos.update(read_list("ru_typos.txt"))
    clean = {w: n for w, n in counts.items() if is_clean(w, lang) and w not in typos}

    ranked = sorted(clean.items(), key=lambda item: (-item[1], item[0]))
    boost = ranked[min(3000, len(ranked) - 1)][1]
    extra_words = [w for w in read_list(lang + "_words.txt") if is_clean(w, lang)]
    for w in extra_words:
        clean[w] = max(clean.get(w, 0), boost)
    ranked = sorted(clean.items(), key=lambda item: (-item[1], item[0]))

    candidates = []
    for word, n in ranked:
        if offensive(word, lang):
            continue
        display = word
        # Capitalised in at least 90% of real occurrences: a proper noun.
        # The supplement boost is ignored here, it is not a real count.
        seen = counts.get(word, 0)
        if seen >= 20 and lower_initial.get(word, 0) < 0.1 * seen:
            display = word[:1].upper() + word[1:]
        if lang == "en" and word == "i":
            display = "I"
        candidates.append(display)
        if len(candidates) >= spec["candidates"]:
            break

    known = {w for w, n in clean.items() if n >= 2}
    known.update(w.lower() for w in candidates)
    known.update(read_list(lang + "_known.txt"))
    known.update(extra_words)
    known.difference_update(typos)
    if lang in ("ru", "kk"):
        known.update({w.replace("ё", "е") for w in list(known) if "ё" in w})
    if lang == "kk":
        plain = {w.translate(KK_PLAIN) for w, n in clean.items() if n >= 3}
        known.update(plain)
    return candidates, sorted(known)


def write_langdata(lang, candidates):
    # Most frequent first: the line index is the rank, so a word found in two
    # lists can be given to the language where it is far more frequent.
    words, seen = [], set()
    for w in (c.lower() for c in candidates[:LANGDATA_WORDS]):
        forms = [w, w.translate(KK_PLAIN)] if lang == "kk" else [w]
        for form in forms:
            if len(form) > 1 and form not in seen:
                seen.add(form)
                words.append(form)
    os.makedirs(LANGDATA, exist_ok=True)
    with open(os.path.join(LANGDATA, lang + ".txt"), "w", encoding="utf-8") as f:
        f.write(f"# {len(words)} frequent {lang} words for language detection; generated by tools/dictionaries\n")
        f.write("# Derived from the Leipzig Corpora Collection (CC BY)\n")
        f.write("\n".join(words))
        f.write("\n")


def main():
    if len(sys.argv) != 2:
        print(__doc__)
        sys.exit(2)
    leipzig = sys.argv[1]
    for out in OUTPUTS:
        os.makedirs(out, exist_ok=True)
    for lang in ("en", "ru", "kk"):
        candidates, known = build(leipzig, lang)
        header = (
            f"# AI Reply autocorrect word list, lang={lang}, words={len(candidates)}, format=1\n"
            "# Most frequent first; the line index (comments excluded) is the rank.\n"
            "# Derived from the Leipzig Corpora Collection (CC BY), see tools/dictionaries/README.md\n"
        )
        for out in OUTPUTS:
            with open(os.path.join(out, lang + ".words"), "w", encoding="utf-8") as f:
                f.write(header)
                f.write("\n".join(candidates))
                f.write("\n")
            n, m, k = write_bloom(os.path.join(out, lang + ".known"), known)
        write_langdata(lang, candidates)
        print(f"{lang}: {len(candidates)} candidates, {n} known forms, bloom {m // 8 // 1024} KiB, k={k}")


if __name__ == "__main__":
    main()
