# Autocorrect dictionaries

The AI Reply keyboards (iOS `ReplyKeyboard/Dictionaries`, Android
`app/src/main/assets/dictionaries`) ship two files per language:

| File | What it is | Used for |
|---|---|---|
| `<lang>.words` | ~50–80k words, most frequent first | correction and completion candidates |
| `<lang>.known` | Bloom filter of every form seen at least twice | "is this a real word?" — never correct it |

Both files are generated. Do not edit them by hand; change the script or the
lists in `supplement/` and regenerate.

## Sources and licence

Word frequencies come from the [Leipzig Corpora Collection](https://wortschatz.uni-leipzig.de/en/download),
whose downloadable corpora are licensed under Creative Commons **CC BY**:

- `rus-ru_web-public_2019_1M` — Russian
- `kaz_newscrawl_2016_1M`, `kaz_wikipedia_2021_300K` — Kazakh
- `eng-com_web-public_2018_1M` — English

D. Goldhahn, T. Eckart, U. Quasthoff: *Building Large Monolingual Dictionaries at the
Leipzig Corpora Collection: From 100 to 200 Languages.* LREC 2012.

The attribution is shown in the app (Settings ▸ Keyboard footer) and in
`THIRD_PARTY_NOTICES.md`. `supplement/*.txt` are written by the AI Reply team.

## Regenerating

1. Download and unpack the four `*.tar.gz` archives above into one folder
   (only the `*-words.txt` files are needed).
2. Run `python3 tools/dictionaries/build_dictionaries.py <that folder>`.

The script writes both apps' copies. The output is deterministic for the same input.

## Format

See the docstring of `build_dictionaries.py`. The Bloom filter hashing (FNV-1a 64 plus
splitmix64, double hashing) is implemented identically in Swift
(`Shared/Keyboard/Autocorrect`) and Kotlin (`keyboard/autocorrect`); both have tests that
read the real files.

## What is filtered

- Only letters of the language (Kazakh keeps `ә ғ қ ң ө ұ ү һ і`; a Latin `i` in older
  Kazakh texts is mapped to `і`).
- Common misspellings that the corpora count as words (`supplement/<lang>_typos.txt`:
  teh, thier, извени, сдесь…) are removed from both files, so they get corrected.
- Offensive words are never suggested (they stay "known", so typing them is not corrected).
- Words capitalised in ≥ 90% of occurrences keep their capital (proper nouns).
- Kazakh "plain" spellings (`калайсын` for `қалайсың`) are added to the Kazakh known set,
  so Kazakh typed on a Russian layout is never "corrected" into Russian.
