# iOS 1.3.0 (34): microphone and Control Center tutorial

The owner requested a genuine Simulator recording of microphone permission and
adding AI Reply Voice to Control Center, preferably on the newest available
iPhone with Dynamic Island, followed by TestFlight and App Store submission.

## Recording and content

The recording uses a dedicated iPhone 17 Pro Simulator, iOS 26.5 (23F77), the
newest Pro model available in the installed Xcode. Russian iOS was selected
through Settings. The owner explicitly confirmed the legal and OpenAI consent
for this test installation and performed the opening Control Center swipe when
the automation could not transmit that system gesture. Subsequent setup steps
were operated through the actual iOS UI. No permission outcome was fabricated.

The finished local video is 72.37 seconds, 720×1784, H264/yuv420p at 30 fps,
1,531,686 bytes, without audio or subtitle tracks. Captions occupy a separate
strip below the original recorded screen. It includes the app's separate
OpenAI voice disclosure, the real iOS microphone alert and Allow action,
Settings → Apps → AI Reply with the enabled «Эфир активности» switch, and
Control Center → + → Add a Control → AI Reply Voice. The final frame shows the
wide named control outside editing mode. Login, general legal acceptance,
personal message text and the system keyboard's first-run popup are excluded.

Video SHA-256:
`3f2f316d24589fd627ec396eb3a2c04f747974805bc15fec48b44b3472253c07`.
Raw recordings, measured capture wall times, source hashes, timeline, full decode
verification and visual contact sheet are in
`outputs/voice-setup-tutorial-20261118/`.

The clean Simulator did not render several Control Center glyphs, including
stock system controls. The installed custom symbol entries match archive 33
byte-for-byte. The recording uses the actual readable control name; missing
glyphs were not painted over. A physical iPhone, live speech transcription and
expanded Dynamic Island controls are not claimed as verified by this video.

The permission recording uses a separately signed build of the unchanged build
33 sources with proper simulated Keychain/App Group entitlements. The unsigned
unit-test host is unsuitable for genuine sign-in capture. Its first attempt is
not used for permission footage. Signing verification is recorded separately.

## Application integration

The local guide is linked from the voice screen, Settings and Permissions.
Written instructions support EN/RU/KK/UZ; non-Russian locales explicitly identify
the Russian video fallback. The Russian instructions use the observed system
label «Эфир активности» (Live Activities). Existing localization entries retain
their original content and formatting; 18 guide keys were added.

The player does not autoplay. The Watch/Replay button opens a full-screen
player, with pause and an explicit localized close control. Closing, leaving
the guide or backgrounding pauses playback and saves progress; observers are
released on exit. Watching does not grant consent or microphone permission and
does not start recording. iOS 17 instructions use the in-app microphone; the
Control Center instructions apply to iOS 18 and later.

All four bundle versions are 1.3.0 (34). Voice capture, session handling,
entitlements, backend and purchase logic are unchanged from build 33.

## Verification and delivery

Delivery receipts and final test/archive/export results are recorded in
`outputs/release-1.3.0-34/`. Both complete test runs report 985 tests (984 passed, one skipped, no failures),
including the final run after the full-screen playback improvement. The keyboard
presentation guard passed. In-app Simulator QA verified the voice and Settings
entry points, no autoplay, explicit full-screen Watch/Replay, playback and pause,
Close, and return to a ready voice screen without starting recording. The final
source manifest/tar/patch were refreshed before archiving. The Release archive
succeeded and all four bundle signatures, versions, App Intents, production URLs
and exact bundled tutorial hash were verified.

The signed App Store IPA export succeeded and its four bundle profiles,
production APNs, disabled `get-task-allow`, App Intents and exact video hash
passed verification. Its SHA-256 is
`be38041fe66141c4246dd0d090cae5fdcdd1b361a2c7e798fafd901136eb5ecb`.
An independent audit confirmed all 320 source file hashes match the final
manifest and tar, and that the source/archive/IPA video hashes agree.

Apple accepted build 34 at 22:07:28 Asia/Almaty and began processing it. The owner
authorized replacing the previous submission: build 33 was withdrawn, and the
version temporarily shows Developer Rejected as expected for a developer
withdrawal. Updated Russian release text and English Review Notes were saved
and read back. The real 72-second MP4 was also attached to App Review
Information. Automatic release after approval remains selected.
No approval or public availability of build 34 is inferred from uploading.

The production review account was independently checked read-only: login,
consent and transcription access are available, but its prior ten admin replies
are exhausted. The owner has been asked to approve exactly 50 test replies.
The prepared helper defaults to read-only and will use the standard audited
admin grant endpoint only after explicit approval. Until then its balance is
unchanged. Evidence is under `outputs/release-1.3.0-34/qa/`.

Final backend recheck at 22:08 Asia/Almaty confirms production and sandbox
HTTP 200/healthy, the expected deployed image/source identity, advertised
voice/realtime capabilities and expected 401 on unauthenticated realtime.
No additional backend deployment or mutation was made for tutorial build 34.

## Delivery state on 18 November, pending owner decision

TestFlight group AI-Reply Internal (two internal testers) lists 1.3.0 (34) as
Testing. The saved Russian test plan was read back. Build ID:
`f335ae12-f4e6-4946-8949-6ac25e231afd`. Tester installation of build 34 was not
verified. Group and delivery evidence is saved in `qa/testflight-delivery.json`.

The version now shows Ready for Review. A draft started at 22:12 Asia/Almaty
contains exactly 1.3.0 (34), with the final Submit for Review button available.
The final submission has **not** been performed: the existing demo account has
zero usable replies, so its generation flow cannot complete. The explicit
request to the owner for exactly 50 test replies is still unanswered. No credit
grant has been executed, and Review Notes make no assertion that it has.
The prepared grant helper was independently reviewed read-only without blockers.
After owner approval, execute one grant with backup/audit verification, confirm
the actual balance, update the account-access sentence in Review Notes, save,
submit the existing draft and verify Apple's new submission receipt/status.

Apple's current review guidance requires full reviewer access:
https://developer.apple.com/app-store/review/guidelines/. The balance finding
is based on the separate production read-only receipt, not on that guidance.

## Resolved on 19 November

The owner explicitly authorized exactly 50 test replies for the existing
`demo@ai-reply.kz` account. Fresh read-only preflight confirmed the exact account,
Free plan, zero balance and unchanged production identity. At 01:46 Asia/Almaty,
the prepared helper issued one standard audited admin credit POST: balance 0 → 50.
Grant: `72be3ae6-0df5-4e25-bef4-e0c525cc4a17`. One matching +50 ledger entry and
one audit entry were verified. No raw SQL product writes, AI generation,
server restart or environment changes occurred. The temporary admin session
was logged out and independently confirmed revoked.

A consistent pre-mutation SQLite backup passed integrity checking and was
preserved with mode 0600 under:
`/home/ai-reply-backups/review-ready-20261118-ios34-plus50/production-before-20261118T204610Z.db`.
Independent read-only verification confirmed all 39 grants belonging to other
accounts and all 64 subscriptions were unchanged. Across the entire grant and
credit-ledger tables, exactly one row was added to each, without changed or
removed prior rows. The prior exhausted demo grant and Free plan remain intact.
Detailed redacted receipts are in `qa/review-grant-50-*-20261119.json`.

After restoring the existing Apple session through the correct saved customer
profile, App Store Connect already showed a submission dated 18 November at
23:24 Asia/Almaty. Its details confirm exactly one submitted item, **1.3.0 (34)**,
status **Waiting for Review**, submission
`abe687e9-9146-4be0-9638-4f71c337856f`. This submission already existed when work
resumed; this turn did not click Submit or cancel/recreate the queue entry.

Review Notes were updated only after the successful grant to state that the
review login has 50 test replies. Save and read-back succeeded. The tutorial MP4
attachment and automatic release-after-approval setting were verified. The
current submission is:
https://appstoreconnect.apple.com/apps/6818570150/distribution/reviewsubmissions/details/abe687e9-9146-4be0-9638-4f71c337856f.
`qa/app-store-delivery-20261119.json` records the observed Apple result. Apple
approval and public availability remain pending.
