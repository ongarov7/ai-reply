# AI Context in the existing Mac quick bar

The enhancement lives in the existing `AirReply-macOS` project. The floating `NSPanel`, menu item, clipboard handling, positioning, global shortcuts, copy/paste flow and microphone implementation remain the application's existing components. There is no new app or replacement main window. iOS, Android and Windows sources are unchanged.

## Interaction

The compact bar adds Message / Reply and a Friend / Business / Customer / Custom template menu. Reply can use a copied message or an explicitly entered original message. Existing copied-message recipient chips and Command-1 through Command-4 remain available. Templates use the existing backend recipient vocabulary rather than another prompt service.

A compact Account button (person icon) opens the existing settings window directly on its Account tab, where email, current plan and balance are visible. It does not change the subscription or sign-in flow.

AI Context expands controls within the same panel. It provides an optional profile selector/editor, custom instructions, document attachment/removal and Standard 400 / Extended 600 character output. The existing InputRow and microphone continue to accept instructions. Recognition inserts editable text; generation still requires the user's submit action. Selecting a template, saving a profile, opening or parsing a file performs no AI generation and consumes no message.

Profiles can contain a person's name, occupation, company, industry, communication style, products, services, policies, description and instructions. Only the explicitly selected profile contributes context. Switching accounts clears active context and ephemeral references. Clearing context deselects a profile without deleting it; deleting a profile or an attachment removes its stored parsed data.

Advanced controls use the existing material, typography, accent color and button conventions. The expanded section has a bounded scrolling area; it does not grow into a separate document window. Reduced Motion disables the added expansion animations. Opening the native file picker holds the quick panel open while that system interaction completes.

## Local document processing

The file picker accepts `.xlsx`, `.csv`, `.pdf`, `.docx` and `.txt`. Only explicitly selected regular files are read. The importer runs away from the main actor and rejects unsupported, malformed, unsafe or oversized input. Its defaults are configurable through `DocumentImportLimits`:

| Limit | Default |
| --- | --- |
| Selected file bytes | 8 MiB |
| Expanded archive bytes | 16 MiB |
| Archive entries | 2,048 |
| Extracted characters | 2,000,000 |
| Rows/paragraph segments | 50,000 |
| Worksheet count | 128 |
| Columns | 256 |

The floating UI allows at most four active references. Import parameters are code configuration; no new global file-monitoring preference or background directory access is introduced.

XLSX parsing detects worksheets, resolves shared/inline strings and associates row values with the first row's column headers. CSV supports comma/semicolon delimiters and quoted fields. PDFKit extracts a PDF's existing readable text; Word XML provides paragraphs. Plain text supports UTF-8 and UTF-16. Word/Excel archives are parsed as data, with bounded expansion and XML entity/external relationship restrictions. Macros and embedded scripts are never executed. Formula cells, including potentially stale cached values, are omitted.

Retrieval is local lexical matching over structured rows or paragraphs, with product-identifier preference. It selects at most eight complete evidence segments and preserves row values instead of cutting a price record in half. An unrelated file section is not included merely because a file is attached. There is no vector database, embedding call or remote document indexing.

The selected profile metadata is bounded to 1,000 UTF-8 bytes. Profile plus matching references fit a conservative budget of at most `3072 - query.utf8.count` bytes, reserving room for server prompt wrappers/escaping. Core also validates the outgoing context. The backend independently checks limits and can reject text whose escaped form is too large. This is a conservative bound on dynamic text, not a claim of exact model-token counting. A multi-megabyte file does not become a multi-megabyte prompt.

## Storage and privacy

Original files and their paths are not sent to the backend. The request contains only selected profile text and retrieved excerpts. The UI explains that those excerpts are sent when the user generates. Source messages, document contents, profiles and generated text are not added to diagnostics or analytics.

Saved profiles and parsed caches are stored in an AES-GCM encrypted file under `Application Support/AIReply/Context`. The encryption key uses the existing nonsynchronizing, `ThisDeviceOnly` Keychain abstraction. File replacement is atomic, size is bounded, and persistence/serialization/encryption run on a background actor. Ephemeral attachments stay in memory until removed, cleared or the existing draft lifetime expires. No full document is stored on the server by this feature.

Request state includes a value snapshot of the chosen profile, attachments, custom instructions and length. Local retrieval runs off the main actor against that snapshot. Retry reuses the same idempotency key and context; another variant uses a new key while preserving that context. Clearing context during an import discards that import's eventual result; changing profile selection cannot attach the result to an unrelated profile.

## Backend contract and compatibility

The Mac reuses `/api/v1/config`, authentication, `/me`, `/ai/reply`, `/ai/compose`, transcription and the existing quota accounting. Capability `features.mac_context_generation` gates new request fields. Older backends continue to receive the old body for ordinary generation; selected advanced context cannot be silently sent to a backend without the capability. The user can clear context to keep using ordinary generation.

The backward-compatible additions are optional `context: {profile, references}`, `output_char_limit: 400 | 600` and compose `template_id`. Only a signed macOS session may use them. A forged `platform` request field cannot enable them. The capability response publishes profile/reference/context/input limits; the backend is authoritative.

The backend treats document/profile data as untrusted facts and preferences below its system rules. It instructs the model to preserve supplied product details, avoid invented prices/stock/policies and ask for clarification when evidence is missing or conflicting. Output character length and model output-token allowance are separate. Overlong or truncated generation has at most the existing single repair; a second failure is returned and its original quota reservation is refunded. Text is not simply cut mid-word.

Only generation enters the existing credit reservation/idempotency flow. Existing Free/Base/Pro/package rules and prices remain unchanged. The actual repository catalog currently gives Base **40 replies/day**; the prompt's 30-reply example was not used to alter production rules. Pro remains unlimited while its existing entitlement is usable.

Backend configuration is `MAC_CONTEXT_GENERATION_ENABLED=true` and `MAC_CONTEXT_INPUT_TOKEN_BUDGET=4096` by default. Disabling the feature hides capability on the next config refresh. An already-open Mac bar may need reopening or restarting to refresh that capability. Existing requests without new fields keep their prior behavior. There is no production deployment in this change.

## Source map

| Component | Files |
| --- | --- |
| Existing window and system behavior | `Sources/AirReplyUI/QuickPanel.swift`, `AppCoordinator.swift`, `AirReplySystem`, `AirReplyVoice` |
| Expanded controls and compact toolbar | `Sources/AirReplyUI/AdvancedContextView.swift`, `QuickBarView.swift`, existing `AppCoordinator.swift`/`SettingsWindow.swift` for direct account navigation |
| Selection, import lifecycle, generation snapshots | `Sources/AirReplyUI/QuickBarModel.swift` |
| Profiles, encrypted cache and retrieval | `Sources/AirReplyCore/ContextProfiles.swift` |
| Document parsing and ZIP safety | `Sources/AirReplyCore/DocumentContextImporter.swift`, `DocumentArchive.swift` |
| Optional API fields and capability | `Sources/AirReplyCore/Generator.swift`, `Models.swift` |
| Russian/Kazakh/English strings | `AirReply/Localizable.xcstrings` |
| UI, privacy and idempotency regressions | `Tests/AirReplyUITests/QuickBarModelTests.swift`, `LocalizationTests.swift`, `SnapshotTests.swift` |

Package paths in the table are under `Packages/AirReplyKit` unless otherwise indicated. Backend device-limit rollout is documented separately in `ai-reply-back-end/docs/PRO_DEVICE_LIMITS.md`.

## Verification and remaining limits

The targeted UI/localization run completed successfully: `swift test --package-path AirReply-macOS/Packages/AirReplyKit --filter 'QuickBarModelTests|LocalizationTests'` passed 25 tests in three suites. It covers prior generation/clipboard/consent/session behavior plus manual reply, optional advanced capability, no generation from profile/file operations, account-switch context clearing, selected-profile-only retrieval and immutable retry context. Core parsing/storage/request tests and the final full build/test results are recorded in the implementation handoff.

Opt-in SnapshotTests rendered light/dark structural states including expanded context and profile editing. SwiftPM's host bundle displays localization keys in those images; they are **not** evidence of complete localized visual review of the shipping app. Catalog completeness and placeholder checks passed. A successful build does not verify physical microphone input or manual dragging.

Release checks still requiring a person on the intended macOS builds:

* Launch the built app and inspect compact/expanded layouts in RU/KK/EN, light/dark, Reduce Motion and a small display.
* Drag the original panel, reopen it at remembered/top/bottom positions, and exercise the existing global and editing shortcuts.
* Record real speech, cancel/finish recording, verify microphone permission denial/recovery, edit transcription and submit exactly once.
* Exercise explicit file selection, long profile names, malformed/large references, attachment removal and local Keychain access failures.
* Verify sign-in, consent, account switching, website purchase reconciliation and Pro recovery against an approved sandbox, without real payment transactions.

Scanned/image-only PDFs require a text layer; there is no OCR. Password-protected PDFs and unsupported Office variants are rejected. XLS/legacy DOC files, macro-enabled file types, formula calculation, Excel formatting/charts/pivot semantics and external workbook links are not supported. Lexical retrieval is lightweight rather than semantic search: differently worded or inflected queries can miss evidence. Missing rows lead to clarification guidance, not a guarantee that every model answer is factually verified. Review important business facts before sending.


## Final local verification (2026-11-03)

`swift test --package-path AirReply-macOS/Packages/AirReplyKit` passed 162 tests across 26 suites. The existing Xcode project built successfully in Debug and universal Release (arm64 and x86_64) with `CODE_SIGNING_ALLOWED=NO`; these are local builds, not signed/notarized releases. The built application launched and its sign-in panel displayed localized Russian text. The backend full suite, focused device/context race suite, `go vet`, backend build and admin JavaScript syntax checks also passed. No iOS, Android or Windows files were changed.

Physical microphone capture, manual dragging and complete RU/KK/EN advanced-layout verification remain release checks; automated voice/system tests passed. Production deployment and payment transactions were not performed. Enable the optional context capability on the approved backend before expecting advanced generation against that backend; an older server continues to support ordinary generation with the old request body.

Parser implementation references: [zlib manual](https://www.zlib.net/manual.html) and [Microsoft Open XML shared strings](https://learn.microsoft.com/en-us/office/open-xml/spreadsheet/working-with-the-shared-string-table).


Owner-assisted live verification completed after the local build: email sign-in succeeded against the existing production API; refreshing the Account view displayed the current server-backed plan and balance. The new person-icon button opened the existing Account tab directly. After closing duplicate local test instances and re-registering the same existing shortcut, Option-Space opened the floating panel. This check did not change the account's tariff, buy anything or deploy backend changes. Compact and expanded Russian dark-mode controls were visually inspected; one duplicate output-picker label was removed, then the complete 162-test Swift suite and universal Release build passed again.
