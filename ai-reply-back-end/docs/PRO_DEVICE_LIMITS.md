# Pro device limits: audit, compatibility and operations

Strict enforcement is **disabled by default**. This change is a compatible backend foundation, not authorization to deploy or enable it in production. Pro remains unlimited while the existing entitlement service grants unlimited Pro benefits. Free, Base, purchased balances, billing, renewal and refund rules remain owned by the existing services.

## Repository audit

The Go backend uses SQLite with WAL, a single writer connection, a reader pool and versioned embedded SQL migrations (`internal/database`, `migrations`). Native authentication creates `devices` rows and binds signed access-token `did` claims and refresh-token records to them. OTP, Apple and Google sign-in converge in `auth.Service.issueSession`; refresh uses the stored device rather than trusting a replacement ID in the refresh body. API middleware already makes the signed device ID available to AI handlers. The subscription service resolves usable subscriptions and unlimited entitlement; the device policy reuses that result.

There are two existing identities with different purposes:

| Identity | Existing use | Important limitation |
| --- | --- | --- |
| `device_id` | Per-user session/device row, signed access-token claim, refresh binding | Clients replace their stored value with the canonical ID returned by sign-in. Account switching can change it. It is not a hardware identity. |
| `installation_id` | Anonymous/authenticated installation and encrypted FCM-token registry | Historically separate from session devices. Old clients send it on installation registration and logout, not ordinary AI requests. |

The implementation extends these relationships; it does not identify installations by IP address, model, push token or User-Agent. New installation binding stores a server-side keyed fingerprint and references existing device rows. Device category comes from persisted, session-bound platform data, never an AI request's platform field. Client-supplied IDs cannot prove hardware identity against deliberate cloning or spoofing.

Existing notification infrastructure is reusable: durable notifications/deliveries, asynchronous dispatcher, Resend email, encrypted FCM tokens, and FCM forwarding to APNs for iOS. Localized templates use the existing localization bundle. Existing admin authentication, role checks, CSRF protection and audit logs are retained.

### Client compatibility

* **iOS:** `Shared/Account/AccountCredentials.swift` stores tokens in a keyboard-shared Keychain group and the canonical `device_id` in App Group defaults. `AccountSession.swift` shares tokens and serializes refresh across app/keyboard processes. The separate `AIReply/Features/Notifications/ClientContext.swift` installation identity uses a `ThisDeviceOnly` Keychain entry in the host application's private group, deliberately inaccessible to the keyboard. The keyboard can use the host's already-bound session without registering another identity. The App Group device-ID value has no demonstrated protection against backup restore to a second phone.
* **Android:** `AccountCredentials.kt` preserves canonical `device_id` through logout; app and input method use one `AccountSession`. Account preferences are excluded from backup. `push/ClientContext.kt` already supplies a random installation ID in `noBackupFilesDir`, stable across restarts and ordinary updates, with a new ID after reinstall/data reset.
* **macOS:** `SessionStore.swift` keeps a random device ID and session in a nonsynchronizing `ThisDeviceOnly` Keychain. The returned canonical device ID replaces the one stored previously. Account switching therefore needs the same independent installation binding as mobile.
* **Windows:** This repository contains `AirReply-Windows/Docs/ARCHITECTURE.md` only. Runtime identity, session and error handling cannot be verified from that document.

**Backend-only strict enforcement for every released client is not established.** In particular, iOS and Android map an unknown HTTP 403 to their unauthorized-session error and ignore the backend's message text (`Shared/Account/APIClient.swift`, Android `data/account/ApiClient.kt`). They will not show a dedicated device-limit warning. Account switching A → B → A also requires an independent installation identity; the old canonical device ID alone cannot prove that the returning installation is the original one.

Optional `device.installation_id` is available for compatible native sign-in, without making it mandatory for old clients. Its keyed fingerprint is bound to the signed access session and persisted through refresh rotation; subsequent request bodies cannot replace that claim. Authenticated installation registration can bind the caller's existing signed device to its installation. This permits shadow observation of current clients, but old sessions lacking a signed installation binding still cannot satisfy strict mode merely by registering. Current installation ownership must match the account. A registration associated with another account or a detached installation cannot be used as proof.

Minimum release work before activation:

1. Keep a stable, non-backup installation identity separate from the per-user canonical device ID. Reuse Android's existing installation ID and iOS's existing host identity. Give macOS an independent installation identity; verify Windows implementation.
2. Send that identity during native sign-in and complete authenticated installation registration before Pro use. Refresh preserves the original binding rather than accepting another installation from its request body. Keyboard requests reuse the host's bound session; no keyboard-only installation is needed.
3. Handle `PRO_DEVICE_LIMIT_REACHED`, `PRO_DEVICE_IDENTITY_REQUIRED` and `PRO_DEVICE_REVOKED` without treating the account as signed out. Provide device-management/support guidance and preserve access to subscription management.
4. Verify backup restore, reinstall and A → B → A account switching with the actual release builds. Do not infer compliance from an API-shaped test or architecture document.

## Authorization and lifecycle

Each account has at most one active mobile authorization (`ios`, `android`, supported `ipados`) and one active desktop authorization (`macos`, `windows`). First successful authorization owns the category; another installation never replaces it automatically. Atomic transactions and database uniqueness constraints settle simultaneous claims. Repeated requests from an authorized installation reuse its slot. The policy executes before AI-provider calls and before credit reservation, including reply, compose, polish and transcription paths.

Denied Pro access does not cancel a subscription, consume a purchased reply or revoke the original device. Account/status, subscription, support and device-list operations remain reachable. Releasing a slot is explicit and audited. A blocked installation cannot silently revoke the original authorization; an existing authorized installation or an administrator must approve recovery through the available backend controls. Revocation closes associated refresh sessions and makes replayed access tokens unusable for Pro. A replacement may claim the released slot; revoked installations are not silently restored by logging in again.

Slots survive renewal, cancellation while benefits remain usable, expiration and reactivation. When the existing entitlement is no longer unlimited, the policy does not apply; existing Free/Base/package rules decide access. Reactivation reuses retained authorization, which prevents an expired subscription from becoming a replacement shortcut. Linked identity providers resolve the same user and therefore the same slots. No general account-merge operation is implemented in the repository: any future merge must resolve conflicting slots under an audited procedure before enforcement for the merged account.

Shadow mode evaluates and records decisions but never blocks a request or sends a notification claiming a device was blocked. Its observations are not proof of ownership and must not be promoted into live authorization automatically.

## Configuration

The migrations are additive: `0031_pro_device_limits.sql` introduces authorization, explicit installation links, audit events and separate shadow observations; `0032_pro_device_notifications.sql` adds the durable notice queue; `0033_pro_session_identity.sql` adds the optional keyed installation identity to existing refresh records; `0034_pro_notification_intent.sql` records durable notification intent on audit events. Existing rows are not assigned a winning device and no user, payment, subscription or device row is deleted. Partial uniqueness on active `(user_id, category)` authorizations enforces one slot; per-user device uniqueness prevents a duplicate authorization for one installation.

Safe initial configuration:

```dotenv
PRO_DEVICE_LIMIT_ENABLED=false
PRO_DEVICE_LIMIT_SHADOW_MODE=true
PRO_DEVICE_LIMIT_CLIENTS_READY=false
PRO_MAX_MOBILE_DEVICES=1
PRO_MAX_DESKTOP_DEVICES=1
PRO_DEVICE_LIMIT_NOTIFICATIONS_ENABLED=false
PRO_DEVICE_LIMIT_NOTIFICATION_COOLDOWN=1h
```

The maximum values enforce the requested one-slot-per-category policy; they are not an invitation to increase the plan allowance. `CLIENTS_READY` is an explicit operator compatibility gate, not automatic verification. Strict mode requires enabled, shadow mode off and readiness confirmed. Missing or unreliable identity has explicit compatibility behavior; the server does not invent one from metadata.

## API and notification behavior

`GET /api/v1/me/pro-devices` lists authorization records, category/platform and activity timestamps. `DELETE /api/v1/me/pro-devices/{authorization_id}` explicitly revokes a permitted authorization. Responses omit raw installation identities and credentials. Management authorization is independent from merely possessing a blocked account session.

Device denial uses the existing error envelope:

```json
{"error":{"code":"PRO_DEVICE_LIMIT_REACHED","message":"Your Pro subscription already has an authorized device in this category.","details":{"device_category":"mobile","allowed_devices":1}}}
```

HTTP status is 403. Messages are localized in English, Russian and Kazakh. Identity-required and revoked-device errors are also explicit; old clients remain operational under default disabled/shadow settings, but cannot be assumed to display these messages correctly in strict mode.

Only real enforced denials can schedule a security notice. Durable intent is recorded without contacting SMTP/Resend/FCM in the request. Jobs enter the existing notification outbox; temporary failures retry, idempotency prevents duplicate outbox creation and a configurable rolling cooldown suppresses repetitive alerts while audit events preserve attempts. The queue allows at most three notices per account per cooldown even when an attacker changes device IDs. Queue transfer retries at one-minute intervals, up to ten attempts; provider delivery uses the existing dispatcher retry policy. Email goes only to a verified account address. Missing verified email/push tokens are recorded as unavailable, and never turn a device denial into a provider dependency. Push targets use known authorized installation bindings, not all installations on the account. No raw device identifier, token, prompt, response or payment secret belongs in notification content or diagnostic logs.

The existing administrator panel exposes authorized devices and recent security events. Its backend provides `GET /api/v1/admin/users/{user_id}/pro-devices` and `POST /api/v1/admin/users/{user_id}/pro-devices/{authorization_id}/revoke`. Revocation requires an active administrator role, the existing CSRF token and a nonempty reason of at most 200 characters. Slot release, session revocation and administrator audit commit together.

## Existing-customer migration and activation

Do not run a blind "oldest device wins" backfill. Historical devices can be duplicated by account switching, restored IDs or incomplete registration, and `created_at` does not prove legitimate ownership. Shadow observations and device history should produce a review inventory of multiple installations per category, identity gaps, duplicate bindings and ambiguous ordering. Resolve ambiguous accounts through a controlled owner/admin recovery process. Seed live authorizations only from reliable evidence or an explicit audited resolution; leave shadow-only candidates unpromoted.

These read-only queries help produce the inventory. They show evidence to investigate, not ownership decisions:

```sql
-- Accounts with more than one observed device row in a category.
SELECT user_id, category, COUNT(*) AS observed_devices,
       MIN(first_seen_at) AS first_observed, MAX(last_seen_at) AS last_observed
FROM pro_device_observations
GROUP BY user_id, category HAVING COUNT(*) > 1;

-- Still-open refresh sessions issued without the optional signed binding.
SELECT user_id, COUNT(*) AS sessions_without_installation_identity
FROM refresh_tokens
WHERE revoked_at IS NULL AND installation_identity = ''
GROUP BY user_id;

-- Shadow compatibility and limit decisions; timestamps are Unix milliseconds.
SELECT kind, reason, COUNT(*) AS attempts
FROM pro_device_events WHERE mode = 'shadow'
GROUP BY kind, reason;
```

Before any production activation:

1. Make and verify an existing SQLite online backup, including the usual WAL-safe procedure. Run the additive migrations against a copy and retain the pre-change binary/configuration.
2. Build and run configuration checks with the safe flags above. Deploy only after separate approval. Do not alter existing billing configuration or payment routes.
3. Observe shadow mode for all supported release builds, including keyboards, desktops, account switching, expired/restored subscriptions, missing registrations and notifications-disabled installations. Investigate operational errors and false positives.
4. Complete actual client compatibility verification, migration inventory and ambiguous-owner resolution. Confirm authorization race tests, lock-contention behavior, migration checks, notification sandbox delivery/retries, admin recovery and billing regression tests.
5. Obtain explicit approval for controlled activation. Set readiness only after the evidence above exists. Enable strict behavior in the approved environment/cohort, monitor denial/identity/error rates and authorized-device continuity. Keep notification delivery off until localized outbox delivery is verified, then enable it separately.

The flags alone cannot establish client readiness or ownership history. This repository change does not perform a live deployment, revoke existing production sessions or enable enforcement.

## Rollback

Disable `PRO_DEVICE_LIMIT_ENABLED`, keep `PRO_DEVICE_LIMIT_NOTIFICATIONS_ENABLED=false`, and restart the service using the existing operational process. This restores normal entitlement decisions immediately after restart. Retain the additive tables and authorization/audit history; dropping them loses recovery evidence and can change future slot ownership. If returning to the pre-feature binary, leave the database migration records and additive schema intact. Never reset the production database or reverse billing migrations.

Revocations deliberately completed before rollback remain revocations; toggling enforcement does not reconstruct revoked refresh credentials. Users may need a new authenticated session through the normal sign-in flow. The dispatcher has a type-specific Pro notice delivery switch: disable it using the feature configuration above so both pending jobs and notices already copied to the regular outbox remain undelivered during rollback. Other notification types keep their existing behavior. Restore a database backup only as a separate incident-recovery decision, since it would also rewind payments and account data created after that backup.

## Verification

API integration coverage is in `internal/apptest/pro_devices_test.go`; service/repository tests cover atomic claims, category classification, revocation and database invariants. Existing IAP, credit, authentication, keyboard-related API, notification and admin suites remain regression coverage. All providers used by automated tests are fakes or local sandbox servers. Record the actual command results in the implementation handoff; this document does not assert that an unexecuted test passed.


## Implementation handoff (2026-11-03)

All changes are local workspace changes. The production environment file was not edited, no production deployment took place, and strict enforcement remains off by default. Apply migrations 0031–0034 before starting the new binary, including when enforcement is disabled: the existing device upsert and refresh persistence now reference the additive schema. Keep migration history and tables when rolling back to the prior binary.

| Area | Modified/new components |
| --- | --- |
| Rollout configuration | `config/config.go`, `config/pro_devices.go`, `.env.example` |
| Atomic authorization, audit and installation links | `migrations/0031_pro_device_limits.sql`, `internal/domain/pro_devices.go`, `internal/repository/pro_devices.go`, `internal/repository/devices.go`, `internal/prodevices/service.go` |
| Session binding | `migrations/0033_pro_session_identity.sql`, `internal/auth/jwt.go`, `internal/auth/service.go`, `internal/repository/auth.go`, `internal/transport/api/auth_handlers.go`, `context.go`, `installation_handlers.go`, `internal/installations/installations.go` |
| Central AI authorization | `internal/ai/service.go`, `polish.go`, `transcribe.go`, `internal/transport/api/server.go`, `pro_device_handlers.go`, `me_handlers.go`, `internal/transport/httpx/httpx.go`, `cmd/server/main.go` |
| Durable notification intent/jobs and delivery | `migrations/0032_pro_device_notifications.sql`, `0034_pro_notification_intent.sql`, `internal/repository/pro_device_notifications.go`, `audience.go`, `internal/notifications/pro_devices.go`, `notifications.go`, `dispatcher.go`, `internal/domain/notifications.go` |
| Admin management/localization | `internal/admin/pro_devices.go`, `internal/transport/adminapi/pro_devices.go`, `server.go`, `internal/transport/web/static/admin-app.js`, locale JSON files for ru/kk/en/uz |
| Tests | `config/pro_devices_test.go`, `internal/repository/pro_devices_test.go`, `internal/apptest/pro_devices_test.go`, `pro_device_lifecycle_test.go`, `pro_device_notifications_test.go`, test harness/notification metadata fixtures; the annual pricing migration fixture now isolates migration 0030 from later migrations |

Final verification completed successfully on the local checkout:

* `go test -json ./...`: no failed packages or tests; 1,033 passed test/subtest events and one deliberately skipped live test.
* `go test -race ./internal/repository ./internal/apptest -run 'Test(ProDevice|ProFirst|ProCloned|ProAppleGrace|AdminProDevices|MacContext)' -count=1`: passed.
* `go vet ./...`, backend build, admin JavaScript syntax check and `git diff --check`: passed.

Coverage includes platform combinations, first registration/refresh, cloned ID metadata mutation, concurrent claim/legacy revoke, account switching, stale access and refresh tokens, renewal/expiry/restore, signed Apple cancellation/grace, purchased-balance continuity, provider outages, notification retry/deduplication/intent recovery, notification rollback, and admin role/CSRF/audit. Tests use fake providers; real billing, email and push delivery were not triggered by the automated suite.

Strict readiness is still blocked by released-client identity/error-handling compatibility and unverified Windows runtime support. There is no automatic promotion of shadow observations into authorized devices. Physical hardware attestation is not provided. See the existing-customer migration review above before any activation.

The separate Mac enhancement and optional context API are described in `../../AirReply-macOS/Docs/CONTEXT.md`.
