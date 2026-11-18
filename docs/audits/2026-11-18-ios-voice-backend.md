# iOS Voice backend release — 18 November 2026

Implementation adds an authenticated, bounded realtime transcription WebSocket
while preserving the existing file-transcription and reply/compose workflows.
The iPhone is responsible for foreground activation, recording consent,
client-side end-of-speech detection and explicit clipboard use. The keyboard
receives text through protected App Group storage and never captures audio.

## Verification

- `go test ./...` passed with the new runtime and privacy disclosures.
- `go vet ./...` passed.
- `go test -race ./internal/ai ./internal/apptest -run Realtime -count=1` passed.
- Focused privacy/legal tests passed for RU/KK/EN/UZ.
- A temporary isolated process using the deployed provider client and existing
  private Compose environment successfully received OpenAI session-ready with
  `gpt-live-transcribe` and `ru/kk/en` hints. No audio was sent, no credentials
  were printed, and the process did not access a database.
- Independent review found no authentication, privacy, lifecycle or duration-cap
  blocker. Existing per-minute/day limits, device authorization and request-ID
  replay are reused.

The automated tests use local real WebSocket peers; they do not claim a live
OpenAI speech-quality evaluation or physical iPhone verification. Realtime usage
records duration and outcome; precise provider token/cost aggregates are not yet
populated for this duration-billed model.

## Source and deployment evidence

- Base commit: `c957d6d64507d8d5bb8945f7503aeda0e09e0a71`.
- Backend source manifest SHA-256:
  `c24689654a0faa8e7e1cf7410d011d23329b714383fc5bcdc20d8d6149fda9d0`.
- Source archive SHA-256:
  `4c765faa27056bdcd66aeb48018bed998be274dea8669c8ce3cbaf7cdbcac296`.
- Image tag: `ai-reply-backend:ios-voice-20261118T124505Z`.
- Staging directory: `/home/ai-reply-builds/ios-voice-20261118T124505Z`.
- The source bundle has 501 files and excludes `.env`, private provider files
  and signing keys.

The new image was activated successfully and observed healthy. Its runtime
source label matches the manifest checksum above. The server checkout remains on
the same base commit with the exact feature source overlay; a source archive and
manifest retain the deployed state independently of any later Git commit.

- Backup: `/home/ai-reply-backups/ios-voice-20261118T124505Z`.
- Previous image retained as
  `ai-reply-backend:before-ios-voice-20261118T124505Z`.
- Backup contains prior backend source, the previous Compose override, the new
  source bundle/manifest, `.env`, both additional private Google environment
  files, and both production/sandbox database files with their sidecars.
- SHA-256 checks confirmed all three environment files unchanged after rollout.
- The existing Compose project and `/app/data` volume were preserved. The public
  override changed only its image tag. No migration was added.
- Production and sandbox `/healthz`: HTTP 200, `{"ok":true}`.
- Production `/api/v1/config`: `voice_transcription=true`,
  `voice_realtime_transcription=true`, `iap_ios=true`.
- An unauthenticated real WebSocket upgrade to the new route returns HTTP 401.
- Production billing catalog remains version 9 with the existing active iOS
  Pro monthly/yearly, 30-reply and 500-reply Apple product IDs; sandbox catalog
  also returns HTTP 200.
- Public `/privacy?lang=ru|kk|en|uz`: HTTP 200; new iPhone voice disclosures and
  displayed edition 2026-11-18 verified. Existing general consent versions stay
  unchanged; the new optional microphone mode has its own explicit consent.

No unrelated dirty local iOS files were copied to the server. Activation needed
a brief backend restart; the running service recovered healthy and the rollback
path was not required. The authenticated OpenAI connection and session configuration were verified
without sending audio. Physical-device microphone behavior and multilingual
recognition quality remain a separate verification step.

## Prepared Control Center flow disclosure — build 30

After the new iOS AudioRecordingIntent path compiled and the root's iOS suite
passed, the privacy wording was corrected and deployed in all four locales.
Foreground setup still obtains explicit microphone/provider consent. A prepared
iOS 18+ Control Center action can subsequently record without opening the app
while a Live Activity remains active; iOS may require device unlock.

Only `internal/transport/web/voice_privacy_texts.go` and its
`internal/apptest/voice_privacy_test.go` differ from the first deployed source
snapshot. Focused Go privacy tests passed. No API runtime or billing behavior was
changed. The new tagged image passed the existing production environment check
before activation; the previous source manifest was checked before replacement.

- Current image: `ai-reply-backend:ios-voice-policy-20261118T131433Z`.
- Current source manifest SHA-256:
  `bce66a72c01a49d98b8e1311277197e2929905bce51573f3cf76db3668218e41`.
- Source archive SHA-256:
  `837cb2fed8cd81706f419948a305a261083eaf5b7be58667fc5db1a89caa150e`.
- New backup: `/home/ai-reply-backups/ios-voice-20261118T131433Z`.
- The image is healthy; production and sandbox health checks pass. Realtime
  remains enabled and rejects unauthenticated upgrades with HTTP 401.
- Both databases, prior image/source and private environment files were backed
  up again. All three environment hashes and the existing data volume remain
  unchanged.
- Live `/privacy?lang=en|ru|kk|uz` pages were checked for the prepared-background
  wording after activation. General consent versions remain unchanged.

The separate build 29 keyboard crash diagnosis is documented in
[the iOS crash report](2026-11-18-ios29-keyboard-crash.md). It identifies UIKit
alert construction in the keyboard extension, with no backend crash or provider
failure implicated by those stack traces.

## Final manual-Done wording and backend freeze — build 31

The remaining short-pause auto-finish claim was found on all four published
privacy pages and corrected. The final text states explicit Done generates,
a short speech pause does not finish recording, the maximum is 40 seconds, and
an initial 20 seconds without speech may end capture. Cancel discards and Pause
releases the microphone. The protocol document now describes the same one-commit
manual-Done flow and existing pre-commit delta events.

Focused four-locale privacy tests passed (0.926 seconds). Relative to the prior
deployed source, only the privacy wording, its regression test and
`docs/IOS_VOICE_REALTIME.md` changed. No voice API, provider, quota, model,
capability flag or WebSocket protocol change was made.

- Final active image:
  `ai-reply-backend:ios-voice-policy-final-20261118T135053Z` (healthy).
- Final source manifest SHA-256:
  `869fff68728fdc8e79ae7c493eff8ab91107fcd4bb150d11f0aae75c55acf7b7`.
- Source archive SHA-256:
  `a4604f13e91f6427278d7d8dbbdea50d88fb4b5db15c69b0576c8fd5ad22abd2`.
- Final backup: `/home/ai-reply-backups/ios-voice-20261118T135053Z`.
- All four live locale pages were verified for Done/40s/20s/no-short-pause
  semantics after activation. Production/sandbox health pass; unauthenticated
  realtime remains HTTP 401. All three private environment files and the data
  volume remain unchanged, and both databases and the prior image/source were
  backed up again.
- Ignored machine-readable receipt:
  `outputs/release-1.3.0-31/backend-policy-receipt.json`.

The live-delta experiment is documented separately in
[the timing note](2026-11-18-live-voice-delta-experiment.md). It establishes that
no segmented/continuous protocol rewrite is needed: the existing backend emits
live transcription deltas before the one final commit. Backend work is now
frozen pending any new explicit task.
