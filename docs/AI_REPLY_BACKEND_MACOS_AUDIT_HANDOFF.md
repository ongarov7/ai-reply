# AI Reply backend and macOS implementation handoff for independent audit

Status snapshot: 3 November 2026. Repository paths below are relative to the repository root so this document can be used on another computer.

This report documents the changes made to the existing AI Reply backend and macOS desktop application. It distinguishes implemented functionality from verified behavior, deployment status, and remaining work. It is an implementation handoff, not an independent certification of code quality, security, usability, or factual answer accuracy.

The scope covered the Go backend and the existing macOS application. iOS, Android, and Windows application code was not modified. The follow-up audit and improvements should continue within the backend and macOS application. Compatibility issues in other clients are relevant to rollout readiness, but do not expand the requested implementation scope.

The main product objective was to make the desktop application more useful for context-dependent communication. A user should be able to describe their business, select a reusable profile, import an Excel catalogue or another document, and generate a reply using relevant information from those sources.

For example, a business user could import a product catalogue containing product names, prices, and availability, then answer a customer's question using the relevant catalogue entry. The implementation provides the initial workflow for this scenario. Retrieval accuracy, usability, and handling of incomplete evidence still require independent review.

## Implementation and release status

| Area | Status at the recorded check |
| --- | --- |
| Pro device-limit backend | Implemented and included in the previously deployed baseline; strict enforcement remains disabled |
| macOS context workflow | Implemented in the existing application |
| Backend support for macOS context | Implemented; capability was enabled at the last production check |
| Twenty free replies at registration | Implemented locally and staged; not applied to production |
| macOS release 1.0.1, build 2 | Built and signed; the application passed Apple notarization |
| Final DMG notarization and website publication | Pending; publication was paused |

Most of the main implementation is already committed in `11e5850` (`main: fixed and updated mac os desktop app`). Reviewing only the current uncommitted diff would miss most of the Pro and macOS work. Review the committed feature implementation as well as the working tree.

Some existing documentation records the earlier local implementation stage and contains historical "not deployed" statements. Those statements need to be reconciled with the later deployment status above. This is a snapshot of the last verified state, not a new inspection of the live environment. No deployment or production activation is authorized by this report.

The additional source changes transferred with this handoff include the Free20 change, migration `0035`, related tests and documentation, and a release-script correction to the argument order of `lipo -verify_arch`. Committing or pushing these files does not apply the migration to production or publish the DMG.

## Pro device policy

The Pro policy extends the existing authentication, device, subscription, and entitlement services.

The intended policy allows one authorized mobile installation and one authorized desktop installation per Pro account. Mobile includes supported iOS, iPadOS, and Android platforms; desktop includes macOS and Windows.

The implementation includes:

- Atomic authorization claims backed by SQLite transactions and uniqueness constraints.
- Separate mobile and desktop authorization categories.
- First-authorization ownership: another login does not automatically replace the authorized installation.
- Authorization history, separate shadow observations, and security audit events.
- Central checks before relevant AI work and credit reservation.
- Explicit device revocation and associated refresh-session revocation.
- User and administrator device-management endpoints.
- Localized errors for a reached limit, missing installation identity, and revoked authorization.

The checks cover reply, compose, polish, and transcription paths. A device denial does not cancel the subscription or consume purchased replies. Account information, billing, support, and permitted recovery operations remain accessible.

Authorizations are retained across subscription renewal, expiration, and reactivation. The existing entitlement service determines when Pro benefits are usable; the new device policy does not replace billing or subscription lifecycle logic. When an account no longer has usable unlimited Pro entitlement, existing Free, Base, and purchased-credit rules continue to decide access.

### Installation and session identity

The backend adds optional installation identity binding to authentication. An installation identifier can be converted into a keyed fingerprint, bound into the server-signed session, and preserved through refresh rotation. Subsequent AI request bodies cannot replace that signed binding. Existing device rows and installation registration are reused.

This is installation/session binding, not hardware attestation. Platform information originates from client registration and is subsequently persisted and signed. The implementation does not independently prove the physical operating system or prevent deliberate credential and identifier cloning.

The additive migrations are:

- `0031_pro_device_limits.sql`: authorizations, installation links, audit events, and separate shadow observations.
- `0032_pro_device_notifications.sql`: durable notice jobs.
- `0033_pro_session_identity.sql`: installation identity on refresh records.
- `0034_pro_notification_intent.sql`: durable notification intent associated with events.

Historical users are not automatically assigned a winning device. Shadow observations are not automatically promoted into live authorizations.

Strict enforcement remains disabled under the last observed configuration:

```dotenv
PRO_DEVICE_LIMIT_ENABLED=false
PRO_DEVICE_LIMIT_SHADOW_MODE=true
PRO_DEVICE_LIMIT_CLIENTS_READY=false
PRO_MAX_MOBILE_DEVICES=1
PRO_MAX_DESKTOP_DEVICES=1
PRO_DEVICE_LIMIT_NOTIFICATIONS_ENABLED=false
```

### Client readiness and recovery

Client readiness remains unfinished. macOS now handles the three dedicated Pro device errors with localized messages and a billing/navigation action:

```text
PRO_DEVICE_LIMIT_REACHED
PRO_DEVICE_IDENTITY_REQUIRED
PRO_DEVICE_REVOKED
```

Its independent installation-binding lifecycle still needs completion or verification. Existing iOS and Android clients retain compatibility gaps around installation identity and unknown HTTP 403 responses. The Windows directory contains an architecture document rather than a working application.

Existing-customer migration also requires review. Historical device rows do not establish legitimate ownership, so there is no automatic "oldest device wins" migration. Account switching, restored backups, reinstallations, and ambiguous histories need an explicit recovery and migration strategy. No general account-merge conflict-resolution flow or tombstone-reactivation UI/API has been implemented.

Revocation has a specific boundary: subsequent guarded Pro requests are denied and refresh sessions are revoked. General access-token authentication remains stateless, so basic account access may continue until token expiry. Provider work already admitted before revocation is not cancelled automatically.

The management API includes:

```text
GET    /api/v1/me/pro-devices
DELETE /api/v1/me/pro-devices/{authorization_id}

GET    /api/v1/admin/users/{user_id}/pro-devices
POST   /api/v1/admin/users/{user_id}/pro-devices/{authorization_id}/revoke
```

A blocked installation cannot silently release the original authorization. User recovery requires the permitted trusted authorization relationship; administrator recovery uses the existing administrator controls.

### Notifications and administration

Real enforced denials can create durable notification intent and queued jobs. Shadow observations do not send messages claiming that a device was blocked. Notification processing includes deduplication, rolling cooldowns, bounded retries, verified-email targeting, and filtering of authorized push installations.

Administrator revocation uses the existing role checks, CSRF protection, a required reason, and transactional audit logging. Disabling Pro notifications also prevents pending notices of this type from being delivered.

Automated tests exercised these paths with fake providers. Physical email and push delivery were not established by those tests. Event/job retention and recovery operations remain relevant audit topics.

## Existing macOS application and context interface

The macOS work extends the existing floating quick bar. The application continues to use its existing floating panel, menu-bar presence, global shortcut, clipboard handling, microphone implementation, positioning behavior, and copy/paste flow.

The compact interface now includes Message/Reply selection, a Friend/Business/Customer/Custom template menu, an expandable AI Context control, and an Account button that opens the existing Account settings tab. Existing copied-message recipient chips and their keyboard shortcuts remain available.

The current context workflow is:

1. The user opens the floating panel and selects Message or Reply. Reply can use a copied message or an explicitly entered original message.
2. The user expands AI Context inside the same panel.
3. The user optionally selects or creates a saved profile.
4. The user adds temporary instructions and imports reference documents.
5. The user chooses Standard or Extended output length.
6. The user explicitly submits generation, reviews or edits the result, and copies it.

Profiles contain a display name, personal/business classification, person name, occupation, company, industry, description, communication style, products, services, policies, and additional instructions.

Only the selected profile contributes to the current request. There is also a separate field for instructions specific to the current interaction.

Document persistence depends on the selection state. A document imported while a saved profile is selected is attached to that profile and stored in its encrypted local cache. A document imported without a selected profile is an ephemeral reference for the current working context.

Selecting a template, editing a profile, or importing a document does not generate an answer or consume a generation credit. Dictation inserts editable text; generation still requires submission.

The expanded controls use the existing macOS visual language and remain inside a bounded scrolling area. The file picker temporarily keeps the floating panel open. Reduced Motion is respected for the added expansion animation.

These changes establish the interface, but they do not represent a completed usability or visual-design audit.

## Local document processing and retrieval

Document processing happens locally on the Mac. Supported formats are `.xlsx`, `.csv`, `.pdf`, `.docx`, and `.txt`.

The implementation extracts structured spreadsheet rows, CSV records, readable PDF text, Word paragraphs, and plain text. XLSX rows associate values with column headers. CSV parsing supports comma and semicolon delimiters and quoted fields. Plain text supports UTF-8 and UTF-16.

Parsing runs away from the main UI actor. Safety checks were added for malformed files, archive expansion, entry counts, XML handling, and oversized extracted content. Macros and embedded scripts are not executed.

| Setting | Current limit |
| --- | ---: |
| Individual selected file | 8 MiB |
| Expanded archive content | 16 MiB |
| Archive entries | 2,048 |
| Extracted characters | 2,000,000 |
| Rows or paragraph segments | 50,000 |
| Worksheets | 128 |
| Columns | 256 |
| Active attachments exposed by the UI | 4 |
| Retrieved evidence segments per request | Up to 8 |

The importer does not provide OCR. Image-only PDFs require a text layer. Password-protected PDFs, legacy `.xls` and `.doc` files, macro-enabled formats, formula calculation, and complete spreadsheet formatting or chart semantics are unsupported. Formula cells are omitted, including potentially stale cached results.

Imported documents are parsed snapshots. Changes to the original file are not continuously synchronized into the application.

### Evidence selection

The application builds a search query from the message and generation instructions, ranks document segments using matching words and phrases, and selects relevant complete rows or paragraphs within the available budget. There is additional preference for matching product phrases and identifiers. Retrieved excerpts carry source labels such as the filename and available section information.

The complete original file and its filesystem path are not uploaded. The backend receives selected profile text and retrieved excerpts when the user generates.

This is a lightweight lexical retrieval implementation. It does not use embeddings or a vector database. Synonyms, grammatical variations, cross-language queries, ambiguous products, and complicated spreadsheets can lead to missing or unsuitable evidence. Attaching a file therefore does not guarantee that its relevant information will reach the model.

There are several distinct context limits:

- Profile metadata transmitted by the Mac is conservatively bounded to approximately 1,000 UTF-8 bytes.
- Available local context space decreases as the query becomes longer.
- Backend validation allows up to 1,500 Unicode characters in the profile.
- References are limited to eight segments and 4,000 characters in aggregate.
- Combined backend context is limited to 4,096 UTF-8 bytes.
- A further conservative check measures the escaped dynamic user prompt.

The configuration name `MAC_CONTEXT_INPUT_TOKEN_BUDGET` should be reviewed carefully: the current implementation uses a conservative UTF-8 byte estimate, rather than an exact model tokenizer, and does not include the fixed server policy in that estimate.

Profile fields can store more text than the request budget can transmit. Later fields, including instructions, may consequently be omitted during bounded serialization. This deserves particular attention in the next review.

## Backend generation contract and factuality controls

The backend accepts optional macOS generation fields through the existing generation endpoints:

```text
context.profile
context.references
output_char_limit
template_id
```

Advanced options require an authenticated session carrying the server-signed macOS platform value. Changing a platform field in an AI request does not enable them. This should not be confused with Apple code-signing attestation.

The capability is advertised through `/api/v1/config` as `features.mac_context_generation`. Ordinary requests can continue using the previous contract against an older backend. Selected advanced context is not silently discarded to make an incompatible request appear successful.

Output choices are Standard, up to 400 characters, and Extended, up to 600 characters. These are character limits, separate from model token allowances.

Backend prompt rules treat document excerpts as untrusted reference data. They instruct the model to preserve supplied names and numbers, avoid inventing prices, stock, deadlines, discounts, or policies, and request clarification when facts are missing or conflicting. Document instructions do not gain authority over the server's system/developer policy.

Those instructions are guardrails, not a deterministic guarantee of factual accuracy or prompt-injection resistance.

Overlong or truncated generation uses the existing bounded repair flow. A final failure refunds the original quota reservation. Retry preserves the original context snapshot and idempotency key; requesting another variant uses a new key.

Existing pricing, payment, subscription, and purchased-credit services remain authoritative. Base remains 40 replies per day, and usable Pro entitlement remains unlimited.

## Local storage and asynchronous state

Saved profiles and parsed document caches are stored in an AES-GCM encrypted file under the application's local Application Support directory. The encryption key uses the existing nonsynchronizing, device-local Keychain abstraction. Persistence uses atomic file replacement and background work.

Clearing active context deselects the profile and removes ephemeral references. It does not delete saved profiles. Account switching clears active context, but the saved profile store is local to the Mac rather than namespaced by application account. The intended account-isolation behavior needs an explicit privacy and product review.

Generation captures an immutable snapshot of the selected context. Import completion is guarded so that clearing context or changing selection does not attach an old result to an unrelated profile.

The implementation avoids adding document contents, source messages, profiles, or generated replies to application diagnostics.

## Audit priorities and acceptance cases

The next audit should concentrate on the following concrete issues:

- **Context discoverability:** whether a new user can find AI Context, understand profiles, and attach a catalogue without assistance.
- **Information hierarchy:** whether the profile editor is too dense for the small floating panel, and whether permanent profile data is clearly distinguished from temporary instructions and attachments.
- **Evidence visibility:** the current interface lists attachment names but does not show a retrieval inspector or clearly explain which rows were used, omitted, or unmatched.
- **Import feedback:** whether users receive useful explanations for unsupported formats, formula-heavy workbooks, scanned PDFs, empty extraction, oversized files, and failed persistence. Several failures currently become generic messages.
- **Business accuracy:** realistic tests with prices, currencies, stock counts, product variants, delivery terms, and conflicting documents.
- **Retrieval quality:** exact SKU matching, similar product names, multiple worksheets, wide rows, missing values, Russian/Kazakh inflections, English synonyms, and multilingual queries.
- **Budget behavior:** whether important policies or instructions are silently omitted, and whether UI character counters align with server byte and Unicode limits.
- **Security and privacy:** hostile document instructions, archive/XML edge cases, account switching, saved-profile isolation, Keychain failures, and diagnostic redaction.
- **State correctness:** cancellation, profile switching during imports, repeated submission, retry, another variant, stale capability data, and credit refunds.
- **Visual and accessibility quality:** keyboard navigation, VoiceOver labels, focus order, long filenames, small displays, light/dark appearance, Reduce Motion, and Russian/Kazakh/English layouts.
- **Pro readiness:** installation/session behavior on actual release clients, existing-customer migration, recovery from revocation, and real notification delivery.

A useful business acceptance case is a catalogue containing several similar products with different prices and availability. The generated reply should use the correct product row and preserve its facts. Missing, conflicting, or unmatched evidence should produce a clear clarification path rather than a confident invented answer.

Additional representative cases should include a price list with formula cells, a document whose relevant row exceeds the context budget, a long business profile whose policies appear near the end, a scanned PDF, a multilingual product query, and a stale catalogue that conflicts with another reference.

## Recorded verification and remaining release checks

Previously recorded verification includes:

- The full Go test suite passing; the recorded main implementation run contained 1,033 passing test/subtest events.
- Focused race tests for device authorization and macOS context paths.
- Successful `go vet`, backend build, and administrator JavaScript syntax checks.
- 162 passing Swift tests across 26 suites.
- Successful Debug and universal Release builds for Apple Silicon and Intel.
- A subsequently signed macOS 1.0.1 build 2, with the application accepted by Apple notarization.
- Owner-assisted checks of email sign-in, server-backed account information, direct Account navigation, the global shortcut, and Russian dark-mode compact/expanded controls.

These results do not establish complete visual quality, physical microphone behavior, real notification delivery, or factual answer quality against representative customer documents. Snapshot tests also do not substitute for inspecting the localized shipping application.

Physical microphone capture, permission denial/recovery, manual dragging, complete localized layout review, and representative document-based answer evaluation remain checks for the intended release environment.

## Pending Free allowance change

The separate Free allowance change sets the default to **20 replies once at registration**. It preserves existing grants, purchased balances, the eligibility cutoff, and custom administrator settings. It does not replenish spent grants or automatically credit older ineligible accounts.

Migration `0035_free_welcome_twenty.sql` changes the previous default and versions the relevant catalogue data without rewriting sold order snapshots. Base remains 40 replies per day, and usable Pro entitlement remains unlimited.

This change has passed local tests and migration checks against private database copies, and a candidate server image has been prepared. It remains undeployed. Existing accounts with no eligible welcome grant can legitimately continue to show zero; the change does not perform a blanket credit allocation.

## Pending macOS publication

macOS version 1.0.1 build 2 was built as a universal application for Apple Silicon and Intel, signed with Developer ID, and accepted by Apple's application notarization service. The application ticket was stapled and validated, and the application passed the recorded Gatekeeper assessment.

A signed DMG was prepared and its integrity and contained application were checked. Final DMG-container notarization, its final stapled checksum/manifest, and website publication remain pending. The application's successful notarization should not be presented as completion of the entire installer release.

Publication and production changes were paused at the owner's request. This audit handoff does not resume deployment or enable strict device enforcement.

## Repository source map

All paths in this section are relative to the repository root. The repository itself, or an appropriate source archive, is required to verify code-level claims; this report alone supports a review of the implementation description and audit plan.

| Area | Entry points |
| --- | --- |
| Pro policy, rollout, APIs, and detailed source map | `ai-reply-back-end/docs/PRO_DEVICE_LIMITS.md` |
| Pro authorization service and repository | `ai-reply-back-end/internal/prodevices/service.go`, `ai-reply-back-end/internal/repository/pro_devices.go` |
| Session binding | `ai-reply-back-end/internal/auth/service.go`, `ai-reply-back-end/internal/auth/jwt.go`, `ai-reply-back-end/internal/repository/auth.go` |
| Pro notification processing | `ai-reply-back-end/internal/notifications/pro_devices.go`, `ai-reply-back-end/internal/repository/pro_device_notifications.go` |
| Backend context validation and prompt integration | `ai-reply-back-end/internal/ai/mac_context.go`, `service.go`, `compose.go`, `quality.go` |
| Backend context and device integration tests | `ai-reply-back-end/internal/apptest/mac_context_test.go`, `pro_devices_test.go`, `pro_device_lifecycle_test.go`, `pro_device_notifications_test.go` |
| macOS context behavior and limitations | `AirReply-macOS/Docs/CONTEXT.md` |
| Original macOS product/design specification | `AirReply-macOS/Docs/DESIGN.md` |
| Context controls and profile editor | `AirReply-macOS/Packages/AirReplyKit/Sources/AirReplyUI/AdvancedContextView.swift` |
| Context selection, imports, and generation lifecycle | `AirReply-macOS/Packages/AirReplyKit/Sources/AirReplyUI/QuickBarModel.swift` |
| Main floating interface and error presentation | `AirReply-macOS/Packages/AirReplyKit/Sources/AirReplyUI/QuickBarView.swift` |
| Encrypted profiles and local retrieval | `AirReply-macOS/Packages/AirReplyKit/Sources/AirReplyCore/ContextProfiles.swift` |
| Document parsing and archive handling | `AirReply-macOS/Packages/AirReplyKit/Sources/AirReplyCore/DocumentContextImporter.swift`, `DocumentArchive.swift` |
| Client generation contract | `AirReply-macOS/Packages/AirReplyKit/Sources/AirReplyCore/Generator.swift`, `Models.swift` |
| macOS UI/state regression tests | `AirReply-macOS/Packages/AirReplyKit/Tests/AirReplyUITests/QuickBarModelTests.swift`, `LocalizationTests.swift`, `SnapshotTests.swift` |
| Pending Free allowance change | `ai-reply-back-end/docs/FREE_WELCOME.md`, `ai-reply-back-end/migrations/0035_free_welcome_twenty.sql` |
| macOS release script | `AirReply-macOS/scripts/build-release.sh` |

Where a table cell lists additional filenames after one full path, those filenames are in the same directory as that path.

## Requested independent review outcome

The requested follow-up is an independent code, design, UX, privacy, and answer-quality audit, followed by a prioritized improvement proposal within the existing backend and macOS application.

The audit should distinguish confirmed defects, architectural risks, product/UX shortcomings, and unverified assumptions. Findings should include severity, source file and line references, reproduction or supporting evidence, user impact, and a concrete correction.

The design review should explain where users enter context, how they understand saved versus temporary data, how they discover imported evidence limitations, and how the interface communicates what information actually influenced an answer. Suggested screen changes or annotated screenshots would make the proposal reviewable.

Any later implementation should preserve existing authentication, billing, subscriptions, quotas, idempotency/refunds, clipboard handling, shortcuts, voice, and ordinary generation behavior. Strict Pro enforcement and production deployment remain separate rollout actions.

If only this document is available, the review must identify the source files and runtime evidence still needed rather than claim to have verified the code.
