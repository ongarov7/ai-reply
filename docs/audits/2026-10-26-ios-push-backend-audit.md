# iOS push: backend audit — 26 October 2026

## Finding and scope

The reported symptom is a campaign preview showing three Android devices and zero iOS devices even though tokens exist. Source inspection did not find an iOS-specific exclusion in the backend: `notifications.Service.platforms()` includes both platforms whenever FCM is configured, and preview, campaign snapshots and automatic notifications use the same eligibility checks.

A stored token alone does not qualify a device. The installation must be attached to an active ordinary account, have an active encrypted token, allow notifications in the app and the OS, satisfy the category preference and campaign filters, and have been seen recently. The legacy `devices` table is distinct from the encrypted `app_installations` registry and is intentionally not a campaign audience. Anonymous installations are also intentionally excluded. Changing these rules to increase the iOS count would send to unintended recipients or bypass preferences.

The confirmed backend diagnostics gap was that exclusions were aggregated across platforms. A partial audience (Android eligible, iOS excluded) had no per-platform explanation. Existing `empty_reason` describes only an entirely empty audience, so it does not explain a zero iOS count when Android remains eligible.

No production database, admin session, APNs/Firebase console, credentials, deployment or real provider sends were accessed. The actual production exclusion reason and physical-device delivery remain unverified. Historical configuration evidence is not treated as the current console state.

## Changes

- Audience preview adds `by_platform.android` and `by_platform.ios`, each containing matched and eligible user/device counts and the existing exclusion reasons. Counts come from the same read transaction as the overall preview.
- Overall excluded users remain distinct: a person with iOS and Android is one overall user, while the platform sections each count that person once. Anonymous devices and accounts without installations do not receive a platform attribution. Platform filters apply to all counts.
- The admin preview displays matching-device counts under each platform and identifies platform counts beside each exclusion. Existing four-language translation keys are reused; older API responses remain supported.
- API/OpenAPI documentation describes the additive fields. Eligibility, marketing opt-in, dispatch, token encryption, recipient snapshots and database schema are unchanged.

## Provider review

FCM is used on both platforms. The iOS APNs block carries the alert push type, default sound, category thread, collapse ID and expiration. Notification title/body are supplied through FCM's `notification` field. Normal and important categories retain their existing priority behavior.

The classifier distinguishes `THIRD_PARTY_AUTH_ERROR` from an invalid device token: an APNs credential failure produces a provider failure without deleting or deactivating the FCM token. It therefore cannot by itself explain an iOS zero in preview. Token-specific `UNREGISTERED` failures deactivate the token, while a fresh token restores eligibility. Payload `INVALID_ARGUMENT` errors are not broadly treated as dead tokens.

Primary references checked:

- [Firebase HTTP v1 error codes](https://firebase.google.com/docs/cloud-messaging/error-codes)
- [Firebase registration management](https://firebase.google.com/docs/cloud-messaging/manage-tokens)
- [Firebase message priority](https://firebase.google.com/docs/cloud-messaging/customize-messages/setting-message-priority)
- [Firebase Apple client setup](https://firebase.google.com/docs/cloud-messaging/ios/get-started)

## Regression coverage

`internal/apptest/notification_ios_push_test.go` uses isolated SQLite databases and an in-process fake provider:

1. Exactly three eligible Android devices and one token-bearing iOS device with pending OS permission produce Android=3/iOS=0, with the exclusion attributed to iOS. A metadata-only authenticated registration after permission approval preserves the encrypted token, produces four eligible devices, snapshots four deliveries and sends each exactly once.
2. One account with two iOS and one Android denied devices remains one overall excluded user. Per-platform counts respect platform filters and omit a fourth anonymous token-bearing iOS installation.
3. An APNs credential rejection leaves the iOS token active and the iOS preview count at one; a subsequent notification succeeds with the same token after the fake provider is repaired.

`tools/tests/admin-push-ui.test.cjs` verifies the per-platform rendering helpers, backwards compatibility and the unchanged send guards, then compiles both actual Vue templates.

Verification passed:

```sh
cd ai-reply-back-end
go test ./... -count=1
go test -race ./internal/apptest -run 'TestIOSPushAudience|TestAudiencePlatformExclusions|TestAPNsCredentialFailure|TestAudienceExplains|TestCampaignSnapshot' -count=1
```

The full suite passed (integration package 52.152 s); the focused race suite passed
(27.917 s). The bundled Node runtime also passed `--check` for `admin-app.js` and
`tools/tests/admin-push-ui.test.cjs`, including both Vue template compilations.
`git diff --check` passed. No provider delivery or production state is inferred from these local results.

## Production verification — 26 October 2026

Production was inspected with owner-authorized access. Neither `.env` nor private-key contents were read. The server checkout was clean on `main` at `2ee6f98`. The owner had rebuilt the container before inspection: its creation time was 16:42:51 UTC (21:42:51 Asia/Almaty), health was good, and `/app/server -check` passed with `APP_ENV=production`.

Read-only SQLite aggregates showed seven iOS installations with an active token, authorized OS permission and notifications enabled in the app. Other iOS groups included `none / authorized` (4) and `none / not_determined` (24); one record had reason `account_deleted`. Android groups included `active / authorized` (16) and `active / not_determined` (2). These registry counts do not imply eligibility for every notification category.

Only one Android account had explicitly enabled marketing notifications. Fresh audience previews confirmed:

- Marketing, all platforms: Android 3, iOS 0; nine accounts were excluded for missing marketing opt-in.
- System, iOS only: seven eligible devices.
- System, the designated developer test account only: exactly one eligible iOS device.

Campaign `3a3f3484-33d8-4fd7-87f1-40b64b78a66c` was sent to that test account at 21:52 Asia/Almaty. It completed for one user and one iOS device. Its delivery recorded `provider_accepted` on attempt 1, an empty error code and a provider message ID. The opened flag remained false at verification. On 27 October 2026 (Asia/Almaty), the owner explicitly confirmed that the test notification appeared on the physical iPhone. Visible receipt is therefore confirmed; a notification tap and an opened event remain unverified.

No preferences, tokens, encryption secrets or provider keys were changed. No backend code correction was warranted: the zero iOS marketing audience was explained by missing category opt-in. Preserve that choice; do not enable consent on users' behalf or relabel marketing as system notifications to bypass it.

TestFlight 1.1.1 (10) was available to the three testers in the existing `AI-Reply Internal` group. Release verification passed: 631 iOS tests with no failures or skips; all 23 backend test-bearing packages; and eight focused push/audience race tests plus three subtests, with no failures, skips or race warnings. The App Store submission draft contained build 10 and passed the readiness check. After the requested physical-notification confirmation, version 1.1.1 (10) was submitted to Apple Review on 27 October 2026 at 00:09 Asia/Almaty. App Store Connect confirmed “Ожидание проверки” (Waiting for Review). [Submission `6ccfc053-ff63-445a-976a-89f00c98b6e5`](https://appstoreconnect.apple.com/apps/6818570150/distribution/reviewsubmissions/details/6ccfc053-ff63-445a-976a-89f00c98b6e5) is awaiting review; approval and public release have not been confirmed.

At the owner's request, repeat test campaign `7a84b83d-a124-4459-991f-866972d4bb01` was sent to the same single test iPhone on 27 October at 00:11 Asia/Almaty. It completed for one user and one iOS device running 1.1.1, with `provider_accepted` on attempt 1 and no errors. The delivery also recorded one opened event at 00:11, confirming that the app reported opening this repeat notification.
