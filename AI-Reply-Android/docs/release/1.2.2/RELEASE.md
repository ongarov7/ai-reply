# Android production 1.2.2 (6)

Public production build: `kz.yerek.aireply`, version `1.2.2`, code `6`, normal
AI Reply label, `https://ai-reply.kz`, unchanged production storage names.
The default Gradle version is now 1.2.2 (6); application behavior and prices are
unchanged. Sandbox builds remain explicit and the sandbox helper keeps code 5.

## Build and sign on the existing upload-key Mac

```sh
cd AI-Reply-Android
./scripts/build-play-production.sh /absolute/path/existing-upload.jks EXISTING_ALIAS
```

Keep the existing Web OAuth setting on that Mac. Use the existing Play upload
keystore and alias; type passwords only at the terminal prompts. The helper
builds offline with explicit production/code 6/version 1.2.2, preserves older
AABs and retains a signed output only when its upload certificate SHA-256 is:

`55:D8:6D:5B:8C:5F:DE:F2:53:F5:B7:B2:A1:16:49:95:3D:25:8C:CD:93:4E:F1:DA:B9:97:88:72:70:F9:87:DC`.

The signed file is
`app/build/play-production/AIReply-1.2.2-code6-production-signed.aab`, with an
adjacent SHA-256 file. No new key, debug-key signing, upload or publication is
performed by the helper. For review without the key, use:

```sh
./scripts/build-play-production.sh --unsigned-only
```

## Public release target

Upload only the **production-signed 1.2.2/code 6** bundle to this app's public
**Production** track. Inspect the manifest/version/signature and Console's
release summary before rollout. The earlier Sandbox 1.2.1/code 5 bundle points
at `/sandbox` and must remain a testing artifact. Public code 6 supersedes code 5
for eligible testers even if a sandbox bundle was already uploaded.

Expected offers remain: 30 replies / 130 ₸, 500 replies / 990 ₸, Pro monthly /
3,990 ₸, Pro yearly / 39,990 ₸. Base 1000 remains off. Prices and product
availability are controlled by the existing backend and Google Play catalog.

## Production receipts and license testers

Keep primary `GOOGLE_PLAY_ACCEPT_TEST=false`. The publisher account and the
configured Google Group license testers receive **Test** receipts with normal
license-test payment instruments. Primary intentionally rejects these receipts;
that refusal does not show that purchases by ordinary production customers are
broken. Keep the owner/group license configuration unchanged. Free license-test
purchases belong to the isolated Sandbox build and backend.

A real production purchase must be made by the user with an ordinary Google
account that is **not** a license tester. Google also documents a Play Billing
Lab real-payment mode, which disables the other license-test features; access
requires Google's permission, so it may not be available to this account. See
[Google's real-payment testing instructions](https://developer.android.com/google/play/billing/test).
Real payments require the account owner's approval.

## Verification boundary

Local checks on 14 November 2026 passed: 75 release JVM tests (billing,
subscription display and environment/storage isolation), offline production
bundle build, manifest/resources inspection and actual bundled DEX inspection.
The manifest is 1.2.2/code 6 with the normal AI Reply label; DEX selects
PRODUCTION with the exact root URL and empty storage prefix. Earlier production
and sandbox bundles were preserved. The helper's missing-argument,
missing-keystore and debug-key guards were exercised.

The signing Mac must verify the existing upload key and final signed bundle.
Actual Google purchases, credit grants and subscription renewals have **not**
yet been exercised on an authenticated Android device. Automated billing tests
and manifest/DEX inspection do not replace that check.

## Backend deployment

On 14 November 2026, production Google verification was enabled on ai-reply.kz
with `GOOGLE_PLAY_ACCEPT_TEST=false`. `/sandbox` retains its independent test
database, credentials configuration and token-encryption secret. The existing
service-account JSON is mounted read-only; secrets are outside Git. Keep each
token-encryption secret stable through future deployments.

The backend image was built from `e402987`; the activation recreated only the
backend service after consistent backups of all three existing SQLite databases
passed integrity checks. Published catalogs remained primary 9 / sandbox 8;
all unrelated runtime configuration, volumes and source files were preserved.
iOS and BCC settings were unchanged. Android products were checked against the
active Google Play products and prices; Base remains disabled for Android.

Before public rollout, use an authenticated Android device to verify a purchase,
repeat/restore without duplicate credits, cancellation and a refund. A successful
build and reachable API do not establish that end-to-end payment verification
has already passed. This release has not been uploaded or published here.

## Release notes for Play Console

```text
Покупки через Google Play: разовые пакеты на 30 и 500 ответов и подписка Pro
с безлимитными ответами. Поддерживается восстановление покупок и проверка статуса
подписки.
```
