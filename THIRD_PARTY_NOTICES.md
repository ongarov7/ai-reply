# Third-party notices

## Leipzig Corpora Collection — autocorrect word lists

The keyboard autocorrect dictionaries (`*.words`, `*.known`) in the iOS and Android apps
are derived from word-frequency data of the Leipzig Corpora Collection, Universität Leipzig,
licensed under Creative Commons Attribution (CC BY).
https://wortschatz.uni-leipzig.de/en/download

D. Goldhahn, T. Eckart, U. Quasthoff: *Building Large Monolingual Dictionaries at the
Leipzig Corpora Collection: From 100 to 200 Languages.* Proceedings of LREC 2012.

Changes: filtered to the letters of each language, offensive words removed from
suggestions, ranked by frequency, and converted to a word list and a Bloom filter
(`tools/dictionaries/build_dictionaries.py`).
