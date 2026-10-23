# AI Reply push pipeline verification — 2026-10-23

## Scope and safety

Read repository architecture/API/notifications documentation and source before edits; initial six-issue audit was created by the coordinating agent before implementation was authorized. Initial branch `main`; initially clean worktree; no applicable AGENTS.md found. Existing mobile edits from other agents were preserved.

No backend `.env`, private key or credential contents were read. No production admin login, database access, provider send, deployment, push, merge or publish was performed. Test accounts, installation IDs and tokens below are synthetic and live only in isolated temporary test databases. The provider in integration tests is an in-process fake.

## Audit evidence and confirmed defects

The pipeline is mobile installation registration → encrypted `app_installations` token → authenticated account association → server SQL audience → notification outbox → leased dispatch → FCM HTTP v1 → Android or APNs → opened acknowledgement. Direct APNs delivery is not used. Both clients register FCM tokens; raw APNs tokens are rejected by the backend contract.

Confirmed source findings:

1. `internal/repository/audience.go` already shared `eligible()` between preview and campaign fan-out. It correctly excludes anonymous installations, disabled/deleted/non-account users, inactive/missing tokens, denied permissions, disabled in-app notifications, stale devices and disabled categories. Marketing is explicit opt-in. The device registry deliberately includes anonymous installations, so seeing devices there never implied campaign eligibility.
2. Preview exposed eligible counts and matched device count but did not explain exclusions, count matching accounts without installations, or show eligible recipient records. This made opt-in, account association and provider configuration problems appear as an unexplained zero.
3. `SendConfirm` in `internal/transport/web/static/admin-app.js` allowed confirmation after a failed/empty preview and did not enforce its `blocked` prop in its handler or button.
4. `QueueCampaign` changed status only. Counts stayed zero and recipients were selected later by the worker. A delayed worker could select a changed population after the administrator sent the campaign.
5. `Status.FCM` meant a configured provider, while its comment and badge implied verified working credentials. FCM acceptance was already correctly named `provider_accepted`; it is not display/delivery telemetry.

The exact reason for the reported production zero-recipient campaign or missing device display is **not established** without authorized campaign/installation diagnostics. The defects above are reproduced locally; removing opt-in or anonymous-account exclusions would be incorrect.

Safe read-only checks before implementation:

- `python3 tools/validate_firebase_config.py`: passed; iOS and Android configs match the same Firebase project and expected app identifiers. This validates client wiring, not server authentication/APNs delivery.
- Public GET `https://ai-reply.kz/healthz`: HTTP 200.
- Public GET `https://ai-reply.kz/api/v1/config`: HTTP 200; `installations=true`, `push_notifications=true`, `permission_diagnostics=true`, `push_reminders=false`. Feature flags do not prove provider credentials or displayed notifications.
- Local backend `.env` and expected FCM credential file were absent; their contents were not accessed. Historical release documents record production APNs setup and no Sandbox key at that checkpoint. Current Firebase/APNs console state was not verified.

## Implemented changes

- `internal/repository/audience.go`: exact eligibility checks now also generate prioritized exclusion reasons. Preview returns `matched_users`, `matched_devices`, eligible user/device/platform/language totals, `exclusions`, `empty_reason`, and up to 20 eligible device records with masked account identifiers and short token fingerprints. Raw tokens, full hashes and client installation IDs never leave this endpoint. One read transaction keeps preview totals and records internally consistent. Matching accounts with no installation are visible when filters allow them.
- `migrations/0020_campaign_recipient_snapshot.sql`: additive nullable `recipient_snapshot_at`; preserves existing data.
- `internal/repository/notifications.go`, `internal/notifications/notifications.go`: send atomically snapshots notification/delivery rows, counts, timestamp and `draft → queued`. No provider is called during this transaction. Workers use the stable snapshot. Existing queued campaigns with a NULL timestamp are expanded once by the worker. SQL claim/idempotency and duplicate-delivery constraints remain intact.
- `internal/notifications/dispatcher.go`: rechecks category preferences immediately before all optional-category sends, so a consent withdrawal after snapshot is honored. Existing logout/account-change, token rotation and invalid-token safeguards remain.
- `internal/domain/notifications.go`, `internal/transport/adminapi/notifications.go`: expose snapshot time; queued campaign history immediately shows recipient/device counts.
- `internal/transport/web/static/admin-app.js`: useful localized zero/exclusion explanations, masked sample, unified send guard for button and handler, snapshot history, explicit provider-configuration wording. Marketing opt-in and anonymous exclusion stay enforced. RU/KK/EN/UZ strings preserve existing language coverage.
- API/OpenAPI and `docs/notifications.md` updated for preview fields, snapshot timing and provider semantics.

A preview is a read-only point-in-time view; send takes a fresh transactional snapshot of the same server criteria. Devices added after send are not added to that campaign. Later logout/permission denial/category opt-out can still skip a selected delivery, and token rotation on the same selected device is honored. The legacy direct API may queue an empty stable snapshot for compatibility; the admin UI prevents confirmation and explains it.

## Verification

Baseline before changes:

```sh
cd ai-reply-back-end
go test ./internal/push ./internal/installations ./internal/notifications ./internal/apptest
```

Passed (apptest 57.357s). Existing tests already cover registration/authenticated association, multiple devices, rotation/deduplication, logout/account changes, invalid/permanent tokens, transient errors/retries, FCM payload/error classification, filters, history, cancellation, leases and concurrent workers.

After implementation:

```sh
cd ai-reply-back-end
go test ./... -count=1
```

Passed for all packages (apptest 51.675s). Focused new tests in `internal/apptest/notification_audience_diagnostics_test.go` additionally verify:

- Explained marketing zero, explicit opt-in, anonymous exclusion and masked sample without private registration fields.
- Matching accounts without associated devices, unknown accounts, inactive tokens, denied permission, in-app disablement, stale devices and disabled provider.
- Frozen send-time recipients/counts before worker execution; later device excluded; token rotation on selected device accepted.
- Transaction rollback on a synthetic delivery insertion failure and successful retry.
- Consent withdrawal after snapshot prevents send.
- Legacy queued campaign receives exactly one snapshot.
- Empty snapshot never acquires subsequently registered recipients.
- Concurrent send requests create one snapshot and one provider attempt.

Focused race verification after adding empty/concurrent snapshot cases:

```sh
go test -race ./internal/apptest -run 'TestAudienceExplains|TestCampaignRecipient|TestCampaignSnapshot|TestLegacyQueued|TestEmptyCampaignSnapshot|TestConcurrentCampaignSend|TestConcurrentWorkersNeverSendTheSameDeliveryTwice|TestAdminTranslations' -count=1
```

Passed (29.272s); includes concurrent request/worker safety and all new snapshot/diagnostic scenarios. A final focused non-race empty-snapshot/localization run also passed (1.937s).

Admin JavaScript:

```sh
node --check ai-reply-back-end/internal/transport/web/static/admin-app.js
node tools/tests/admin-push-ui.test.cjs
```

The environment has no Node on the shell PATH; commands use the bundled Node executable reported by `load_workspace_dependencies`. Syntax passes. The dependency-free test verifies eight confirmation states (loaded/loading/error/blocked/busy/missing/zero/missing count), recipient diagnostics, and compiles both changed templates with the repository's shipped Vue compiler. Dynamic exclusion key families are included in existing localization completeness tests.

## Remaining external verification and release scope

No simulator injection or fake provider acceptance is presented as real push delivery. Designated physical iOS and Android test devices must verify granted/denied notification permission, foreground/background/terminated behavior, channels, notification tap/open telemetry, token rotation, logout and account switching. Need authorized production/test deployment and current Firebase/APNs credential verification before concluding why the customer's real devices did not display notifications. Do not broadcast to production users.

Backend/admin changes are ready for internal development QA based on local tests. They are not proof of production end-to-end delivery or production submission readiness. The migration has been exercised by isolated test harness databases only and has not been deployed.

## Review correction — campaign start and provider-specific claims

Cross-platform review identified an interaction introduced by earlier recipient snapshot creation: outbox rows now exist while a campaign is still `queued`. The old `ClaimDeliveries` query had no parent campaign-state condition, so a concurrently running batch (or a tick which starts at most five campaigns) could claim a later campaign before its `processing` transition. An email-only worker could then finalize push work as `provider_unavailable`.

Corrected before final handoff:

- Campaign delivery claims require the parent campaign to be `processing`; automatic deliveries with NULL campaign IDs remain eligible.
- `ProcessBatch` supplies only channels this process can currently send: push when ready, email when its mailer is configured. Email-only instances preserve both queued and already-processing push outboxes for a push-capable worker. No attempt is consumed and no permanent provider-unavailable skip is written.
- Cancelled expired leases continue to be closed by the existing cancellation cleanup. Automatic priority and lease/idempotency rules are preserved.

New integration regressions:

- `TestCampaignSnapshotDeliveriesWaitForProcessingWhileAutomaticSendsContinue`: six queued campaign snapshots plus one automatic notice; a direct concurrent batch processes only the automatic row, first tick processes five campaigns, the sixth remains untouched until the next tick.
- `TestEmailOnlyWorkerPreservesQueuedAndProcessingPushSnapshots`: mixed queued/processing campaigns plus automatic push/email; email-only instance sends only the email, all three push deliveries remain queued with zero attempts, then a push-capable worker resumes all exactly once.

Focused tests passed (1.476s). Full regression and focused race results after this correction are recorded below.

Final verification after the worker-claim correction:

```sh
cd ai-reply-back-end
go test ./... -count=1
go test -race ./internal/apptest -run 'TestCampaignSnapshotDeliveriesWait|TestEmailOnlyWorker|TestConcurrentCampaignSend|TestConcurrentWorkersNeverSendTheSameDeliveryTwice|TestACancelledCampaign' -count=1
```

Both passed: full suite apptest 54.342s; focused race 20.223s. `git diff --check` also passed. No production actions were performed.
