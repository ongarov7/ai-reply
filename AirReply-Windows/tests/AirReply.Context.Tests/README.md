The fixtures and `cases.json` are copied from `docs/audits/evidence/2026-11-03-document-acceptance/`.
The 62 original queries are executed locally against the Windows importer/planner; no model or production request is made.
The tests check escaped prompt size, raw UTF-8 size, rune/reference caps and the expected imported facts.

The old audit described desired outcomes, including some beyond lexical retrieval. The Windows expectations explicitly differ in these cases:

- 1d: “килограммовая” does not infer a particular package weight; references must contain Арабика.
- 3e–3g: English/Russian furniture translation and the couch/sofa synonym are unsupported, so no reference is sent.
- 6b: conflicting price lists are ordered by import date, as specified; the last imported old price is first. This is not an assertion that the price is commercially current.
- 7b/7d: priority instructions and the matching sofa survive; omitted profile fields are reported when the whole profile cannot fit.
- 7c: 1000-rune per-message instructions are rejected whole by the Windows 500-rune limit.
- 9g/9h: image-only/password-protected PDFs produce dedicated import errors.

The focused tests additionally cover account separation, encryption, concurrent/atomic persistence, metadata merges after import, replacement/removal, corrupt/future stores, profile/document caps, SKU spelling, cached formulas, hidden internal columns, physical CSV row numbers, text encodings, wide-row projection, cross-sheet stock joins and ZIP/XML safety limits.

No 200,000-segment performance target or real-world scanned/OCR document support is claimed. The parser uses the existing selectable PDF text layer and never performs OCR.
