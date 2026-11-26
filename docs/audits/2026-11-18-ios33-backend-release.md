# iOS 1.3.0 (33) and backend release — 18 November 2026

The owner requested rebuilding the latest iPhone changes, distributing them in
TestFlight, submitting the App Store release, and rebuilding the backend over SSH.
The source baseline is `a46094b7c775ae0997acd633e1447701d67a5369`.

## iOS build

Build 33 includes the latest voice screen, keyboard voice strip, variable branded
Control Center symbol, field-bound insertion and longer voice instructions.
Build 32's archive predates these changes and was not reused. All four bundles
(iPhone, keyboard, voice widgets and Watch) were advanced to 1.3.0 (33).

The complete simulator suite reports 981 tests: 980 passed, one skipped, no
failures. The keyboard modal-presentation guard and `git diff --check` passed.
The Release archive and App Store export succeeded using the existing Xcode
account and team `2PK6339Q47`.

Archive and IPA verification covered all four versions, strict code signatures,
App Group entitlements, production API URLs, voice disclosures, the new symbol
and public recording App Intent metadata. The archive uses development signing;
the exported IPA was independently verified to use App Store profiles, disabled
`get-task-allow` and production APNs. Internal-only distribution is disabled; automatic build
number management is disabled. Exported IPA SHA-256:
`c92756f275718a8832bcc4496c9cba34f4eb6bb219f294e698c30e360c772395`.

Artifacts: `outputs/release-1.3.0-33/`, including source manifest and patch,
test results, archive/IPA verification, release metadata and delivery logs.
No physical iPhone was connected; microphone capture, physical Control Center,
expanded Dynamic Island and locked-device handoff are not hardware-verified.

## Backend deployment

The backend was rebuilt on the existing server over SSH and activated with
rollback preparation. Image: `ai-reply-backend:release-20261118T155320Z`;
image ID: `sha256:1ffa4d30a8f7dd86d14674ae652b090d4d88a205b9762c05949e401c7022b9aa`.
All 502 deployed backend files match the source baseline. The existing overlay
workflow preserves server checkout HEAD `c957d6d`; deployed source identity is
recorded separately by the file manifest, SHA-256
`fbdfe2c31f10207bb9dc946df111e54bea73384112f121306a58d3a1c445b066`.

Full Go tests (32 packages), `go vet`, focused race tests, production environment
validation and independent public checks passed. Production and sandbox health
return 200. Both advertise realtime transcription and voice instructions with a
1,000-character voice limit; the typed limit remains 400. Unauthenticated realtime
requests return 401. Privacy pages in RU/KK/EN/UZ return 200 and describe the voice
processing and OpenAI recipient.

Backup: `/home/ai-reply-backups/release-20261118T155320Z`. Both offline SQLite
backups pass integrity checks. All three private environment files and the
existing data volume were preserved. No new migrations were introduced.
Receipts and logs: `outputs/backend-rebuild-20261118/`.

## Apple delivery

The previous App Store submission containing build 31 was shown as Removed;
the version status was Developer Rejected, indicating the developer withdrew
that submission. Russian What's New and English App Review notes were updated
for build 33. The existing automatic release-after-approval setting is retained.
Apple accepted the upload at 20:59:59 Asia/Almaty, reporting “Upload succeeded”
and “Uploaded package is processing.” Delivery status is recorded in
`outputs/release-1.3.0-33/release-status.json`.

Apple confirmed “Submitted items: 1” at approximately 21:05 Asia/Almaty.
The version is now Waiting for Review, with build 33 selected. Submission:
https://appstoreconnect.apple.com/apps/6818570150/distribution/reviewsubmissions/details/9595ac62-791f-47e4-88f3-84b002a81dcc.
Approval and public availability are pending Apple review.

After a fresh TestFlight page reload, build 33 shows one assigned group:
`AI-Reply Internal`, internal, two testers. The saved Russian testing plan was
read back successfully. Tester installation of build 33 is not inferred.
Build ID: `0e643a08-1515-45ab-8cad-5daf0cc3e4a7`.
