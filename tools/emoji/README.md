# Emoji catalog

One emoji list for both keyboards, byte-identical in:

- `AI-Reply/Shared/Keyboard/Emoji/emoji-catalog.txt` (iOS)
- `AI-Reply-Android/app/src/main/assets/emoji/emoji-catalog.txt` (Android)

Both files are generated. Do not edit them by hand. Change the data blocks in
`build_catalog.swift` and regenerate.

## Regenerating

Run this on a Mac with Xcode. The script uses CoreText and the system ICU, and it
downloads nothing.

```sh
swift tools/emoji/build_catalog.swift          # validate, then write both copies
swift tools/emoji/build_catalog.swift --check  # validate, check both copies are current
```

The script prints the count for each category. If any check fails, it exits with code 1
and lists every problem. Given the same data, the output is always the same bytes.

## Format

The file is UTF-8 with LF line endings, has no BOM and no comments, and ends with a newline.

```
@smileys_emotion
😀	1.0
😃	0.6
...
@flags
🏁	0.6
```

- `@<category_id>` starts a category. The categories come in this order: `smileys_emotion`,
  `people_body`, `animals_nature`, `food_drink`, `travel_places`, `activities`, `objects`,
  `symbols`, `flags`.
- Every other line is `<emoji>\t<emoji version>`. The emoji is the fully-qualified
  sequence (VS16 where required, e.g. `❤️` not `❤`).

## What is in it

- Every RGI emoji up to **Emoji 15.1**, in the default (yellow) skin tone: gender variants,
  ZWJ sequences, keycaps `#️⃣ *️⃣ 0️⃣…9️⃣ 🔟`, all 258 country flags, and the England,
  Scotland and Wales flags.
- Left out: skin-tone variants, components that do not stand alone (🏻…🏿, 🦰…🦳), and
  anything newer than 15.1. That covers all Unicode 16/17 code points, 🇨🇶 (E16.0) and
  🧑‍🩰 (E17.0).
- The order is the Unicode CLDR emoji order. The script checks it against the CLDR emoji
  collation (`und-u-co-emoji`) of the macOS ICU (ICU 78.1, CLDR 48). In a few places newer
  CLDR differs from emoji-test 15.1, and the catalog follows CLDR there. Examples: the
  hair-colour rows of the person subgroup, 🎺 next to 🎷, 🪙 before 💰.

## Version filter (apps)

Each keyboard hides the entries the system font cannot draw:

- **iOS**: iOS 17.0–17.3 shows versions **≤ 15.0**. iOS 17.4 and later shows **≤ 15.1**.
- **Android**: the emoji font depends on the OS version and the OEM. Keep an entry only
  when `Paint.hasGlyph(emoji)` is true. The version can serve as a cheap pre-filter or as
  a cache key.

## Validation done by the script

1. **Code points.** Every code point is assigned and is an emoji code point (Swift
   `Unicode.Scalar.Properties`: `age`, `isEmoji`, `isEmojiPresentation`). Some characters
   are allowed only inside their proper sequence:
   - ZWJ U+200D only between two emoji;
   - VS16 U+FE0F only after a text-default emoji;
   - U+20E3 only in `[#*0-9] FE0F 20E3`;
   - regional indicators only as a pair;
   - tag characters only as `🏴 tags… U+E007F`.

   Skin-tone modifiers are rejected.
2. **Full qualification.** The data may leave out VS16. The script removes VS16 and puts
   it back after every `Emoji_Presentation=No` code point. A separate check then
   re-verifies the result. Every entry must also be in ICU's `[:RGI_Emoji:]` set.
3. **No duplicates.** Every entry must also be exactly one grapheme cluster.
4. **Apple Color Emoji.** CoreText must draw every entry as exactly one glyph from
   `AppleColorEmoji`, with no font fallback and no `.notdef`, one emoji cell wide.
   Apple draws the six couple and kiss sequences (👩‍❤️‍👨, 👩‍❤️‍💋‍👨, …) as a zero-advance
   left half plus a right half at the same origin. That is still one cell, so the script
   accepts and lists them.
5. **Versions.**
   - Every version is a known emoji version and is ≤ 15.1.
   - No version is older than the newest code point in its sequence allows: Unicode
     ≤ 6.0 → 0.6, 6.1 → 1.0, 7.0 → 0.7, 8.0 → 1.0, 9.0 → 3.0, 10.0 → 5.0; from 11.0 on,
     the emoji version is the same as the Unicode version.
   - ZWJ sequences are ≥ 2.0, gendered ZWJ sequences ≥ 4.0, and tag flags ≥ 5.0.
   - 15.1 entries must be ZWJ sequences, because Emoji 15.1 added no new code points.
6. **Completeness.** The script lists ICU's RGI set and drops skin tones, components and
   anything built from code points newer than 15.1. Every emoji that remains must be in
   the catalog or in `newerThan15_1`.
7. **Order.** Under the CLDR emoji collation, the whole catalog must be strictly
   ascending.

## Licence

The emoji order follows the Unicode CLDR emoji ordering (© Unicode, Inc., Unicode
License v3). The AI Reply team wrote the list and the version data themselves. See
`THIRD_PARTY_NOTICES.md`.
