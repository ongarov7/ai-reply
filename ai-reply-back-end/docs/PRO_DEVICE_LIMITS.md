# Pro device limits: audit, compatibility and operations

Strict enforcement is **disabled by default**. The original signed-installation policy and its historical rollout findings are documented below. The released-client compatibility policy added on 16 November 2026 is described in the next section. Pro remains unlimited on its authorized device while the existing entitlement service grants unlimited Pro benefits. Free, Base, purchased balances, billing, renewal and refund rules remain owned by the existing services.

## Released-client compatibility policy — 16 November 2026

The owner requested backend-only enforcement for published iOS and Android apps. These apps already send a stable session device ID, but do not send the independent installation ID during sign-in. `PRO_DEVICE_LIMIT_LEGACY_SESSIONS=true` explicitly permits verified legacy sessions with an empty signed installation claim to use their existing device ID. It requires enabled, non-shadow enforcement and the readiness gate. Defaults remain unchanged. Sessions with a nonempty installation claim retain all original strict checks; an inconsistent signed claim never downgrades to compatibility access.

The policy uses the existing atomic authorizations: one mobile slot shared by iOS/Android/iPadOS and one desktop slot shared by macOS/Windows. Monthly and yearly unlimited entitlements use the same guard. An additional device cannot use unlimited benefits, including through an already-open or refreshed session. Separately purchased replies remain usable under the purchased-pack policy below. Account, subscription and purchase verification remain available, and a denied request does not spend credits. Successful native purchase verification attempts to claim an empty slot for the buyer before their first AI request; replaying/restoring a purchase cannot replace an occupied slot and still finishes normally.

Authenticated installation registration can recover a historical per-account device after A → B → A. Additive migration `0041_pro_device_session_aliases.sql` records an immutable session alias to the existing same-user, same-platform device. Registration cannot merge authorizations, change a binding, bypass a revocation or alias a different registered installation. Revoking the canonical authorization also revokes its alias devices and refresh families atomically. Existing signed-installation behavior remains independent of aliases.

Published apps map an unfamiliar HTTP 403 to an authentication failure. Compatibility AI denials therefore use **HTTP 409** with the existing `PRO_DEVICE_LIMIT_REACHED`, `PRO_DEVICE_REVOKED` or `PRO_DEVICE_IDENTITY_REQUIRED` code and safe details. This avoids an unnecessary authentication retry. Current apps display their generic conflict message; they cannot show the server's dedicated explanation without a client update. Subscription data continues to report the actual paid Pro plan. On a valid additional device, usage is projected to its finite purchased-pack allowance, so a profile or token refresh does not incorrectly restore the cached unlimited indicator.

This prevents ordinary account sharing across separately signed-in phones. The existing API cannot prove physical hardware identity against deliberate copying of a device ID or bearer credentials; compatibility mode does not claim hardware attestation. Reinstallation or a genuinely replaced phone requires explicit owner/support slot recovery, while account switching on the same registered installation can recover automatically. No IP, model, push token or User-Agent is used as an identity.

Activation configuration for this policy:

```dotenv
PRO_DEVICE_LIMIT_ENABLED=true
PRO_DEVICE_LIMIT_SHADOW_MODE=false
PRO_DEVICE_LIMIT_CLIENTS_READY=true
PRO_DEVICE_LIMIT_LEGACY_SESSIONS=true
PRO_MAX_MOBILE_DEVICES=1
PRO_MAX_DESKTOP_DEVICES=1
PRO_DEVICE_LIMIT_NOTIFICATIONS_ENABLED=false
```

Before activation, preserve the old image, source, private environment and consistent SQLite backups. Inventory unlimited subscribers across `subscriptions`, `apple_subscriptions`, `google_subscriptions` and bank periods; manual subscriptions alone do not include App Store purchases. Preserve a reviewed existing owner with a seeded authorization and audit event. Do not select a winner from shadow observations or let a new request displace an existing customer. Abort automatic seeding if more than one plausible existing device/installation remains. Migration 0041 creates no authorizations or aliases automatically.

To disable this policy, set **all** of `PRO_DEVICE_LIMIT_LEGACY_SESSIONS=false`, `PRO_DEVICE_LIMIT_ENABLED=false`, `PRO_DEVICE_LIMIT_SHADOW_MODE=false` and `PRO_DEVICE_LIMIT_NOTIFICATIONS_ENABLED=false`, then recreate the backend. Disabling only enforcement while leaving legacy compatibility enabled fails configuration validation; leaving shadow mode enabled continues observation. Preserve additive schema, authorizations, aliases and billing data. A binary rollback may use the prior image with this additive schema retained; do not restore an older payment database over newer transactions.

Focused API coverage is in `pro_device_legacy_test.go` and `pro_device_legacy_identity_test.go`: published request shape, every mobile combination, independent desktop slot, existing/rotated sessions, account switching, purchase verification, monthly/yearly Apple entitlement, denied provider calls, unchanged balances, support revocation, signed-identity downgrade refusal and unaffected Free/Base usage. Repository tests exercise alias ownership, binding immutability, platform pinning and revocation.

### Purchased packs on additional devices

The owner explicitly selected this rule on 16 November: an account with active Pro and retained purchased replies may use those purchased replies on an additional phone or computer. Unlimited Pro remains restricted to the existing one-mobile/one-desktop pair. Only an occupied-slot denial permits this fallback; revoked devices, invalid installation identity and storage errors stay denied.

Reply and compose use a request-scoped finite entitlement and `ReserveInput.PurchasedOnly`. The original subscription and slot ownership stay unchanged. The writer transaction can debit only `credit_grants.kind='pack'` for the same account; unlimited/daily/monthly allowance, passes, welcome bonuses and admin credits cannot fund an additional-device request. The successful response reports `unlimited=false`, `bucket=pack` and the remaining purchased balance. An authorized device still uses unlimited benefits without reducing packs. Zero packs preserve the Pro-device denial.

Sign-in/refresh, purchase responses, `/me` and `/me/usage` apply the same finite usage projection only when device authorization returns the occupied-slot error. The actual subscription/plan stays Pro and account management still returns success. Authorized devices continue to receive unlimited usage, and Free/limited-pack accounts retain their existing balances. Identity, revocation and storage errors are never treated as evidence of eligibility for paid fallback. Another app's cached screen still needs its normal next refresh to receive the new counters.

The shared pack debit, ledger and reservation commit atomically before provider work. Distinct simultaneous requests cannot spend the same last unit. Provider failure refunds that same pack reservation once. Existing bounded retry of a paid pack action remains available even after its final unit; a key previously funded by unlimited Pro or another excluded bucket cannot be replayed for free on an extra device. This retains retry behavior without making Pro transferable through request keys.

Polish and dictation remain auxiliary operations under their existing per-account caps. An additional device needs a currently positive purchased balance for these operations; they do not debit an answer unit. Authentication and revocation checks occur before cached voice results or polish-cap shortcuts. Derived finite access does not send an exhausted-Pro quota notification.

Accounts without active unlimited Pro keep the existing shared pack behavior on every device. Server accounting is immediate and authoritative, while another app's cached balance refreshes on its next account/usage/generation request; there is no continuous balance push. API regressions include pack30/pack500 across all four platforms and eight distinct concurrent actions competing for one last reply. Dedicated fallback coverage is in `pro_device_pack_fallback_test.go`; repository policy/replay/SQLite race checks are in `purchased_only_credits_test.go`. No new schema migration is required.

### Production activation evidence

Activated on 16 November 2026 at the owner's request. The existing production subscriber had one native device, one currently owned installation, a live refresh session and successful AI history; this reviewed device was seeded with an `authorized` audit event before enforcement. No historical multi-device account was assigned an arbitrary winner. Inspection also found an account with two Android registrations and an iPhone whose legacy Pro expired on 3 November; it currently uses purchased packs, so the unlimited-device policy does not block those balances.

Backup directory: `/home/ai-reply-backups/pro-devices-20261116T133139Z`. It contains the original source/private environment, verified online backups of both SQLite databases, the prior image reference and activation metadata. Prior image tag: `ai-reply-backend:before-pro-devices-20261116T133139Z`; deployed image also tagged `ai-reply-backend:pro-devices-20261116`. Source upload SHA-256: `d02936491203283f775c466c5c0414e7670e6440b39054e5f51cc394a0f65de9`. All original non-policy environment assignments were preserved verbatim.

Validation completed: full `go test ./...`, `go vet ./...`, repository race suite and targeted API race suite; image build and production configuration check; migration against both production-database copies with all 61 existing tables unchanged; live health checks; actual HTTP sandbox checks using old-style signed sessions with no installation claim. A pre-existing synthetic reservation stopped permitted AI requests before provider calls: iPhone and desktop reached the reservation conflict, second Android received `409 PRO_DEVICE_LIMIT_REACHED`, and the blocked phone retained account access. No AI usage event or notification was created, and the synthetic account and dependent rows were removed.

After deployment each database has migration 0041 exactly once, `quick_check=ok` and no foreign-key violations. Existing purchases, Apple/Google/bank subscriptions, bank orders, credit grants and manual subscriptions compare identically to the pre-activation backup in both databases. Production has one seeded active mobile authorization; sandbox has no retained test authorization. Physical-device UI verification was not performed; the published apps retain the generic-conflict-message limitation described above.

### Purchased-pack follow-up deployment

The owner-approved purchased-only fallback and device-scoped usage projection were deployed on 16 November 2026. Backup: `/home/ai-reply-backups/pro-pack-fallback-20261116`; rollback image tag: `ai-reply-backend:before-pro-pack-fallback-20261116`. Final uploaded source SHA-256: `5a454ae4fda274f7c58d0003ec7df98e40432fefc6db09a079a39ebf9e61296a`; running image: `sha256:154406a91dcb63fb6ccf111ef77f0585c244c6c1d30bbaceb247cdaa8c4f88b1`, also tagged `ai-reply-backend:pro-pack-fallback-20261116`.

Full backend tests, `go vet` and focused race tests passed. New coverage proves pack30/pack500 sharing across four platforms, exactly one success from eight distinct concurrent requests for the final unit, purchased-only selection, same-pack refunds, replay restrictions, and finite usage after extra-device sign-in/refresh/status reload. A live sandbox HTTP check retained the authorized phone/desktop, refused unlimited access on an extra phone with no pack, allowed a positive pack balance to reach a safety reservation, and confirmed finite extra-device usage while the owners retained unlimited usage. Safety reservations prevented provider calls; zero AI usage events were created and all synthetic rows were removed.

Final read-only checks report healthy production/sandbox, zero restarts/errors, no new migrations, no duplicate active category slots and preserved authorization owners. Every existing financial row in both databases matches the follow-up backup. Private environment, Caddy and the Windows executable are unchanged. Recovery disables the feature or uses the saved recent image without restoring a payment database. The already-published Windows preview page remains available.

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
