# Free: 20 replies once at registration

The default Free allowance is **20 replies once per eligible account**. It is a welcome grant, not a daily or monthly plan quota. The existing registration/provisioning flow creates it; database uniqueness permits only one welcome grant for that user. Existing authentication, idempotency, reservation/refund and credit consumption rules are reused.

`usage.daily_limit` and `usage.remaining_today` remain zero for the Free plan because there is no recurring allowance. The usable welcome amount appears in `usage.welcome.total`, `usage.welcome.remaining` and `usage.remaining_total`. Purchased packages contribute separately to the latter. A client showing only the daily field can display zero while a welcome balance remains.

## Migration 0035

`0035_free_welcome_twenty.sql` changes the old stored default of `10` to `20`. It preserves an administrator's `0` (disabled) or other custom quantity. The fallback constant and fresh-install catalog also become 20.

The migration preserves `welcome_bonus_since`. Accounts created before that existing cutoff, legacy install accounts and simulator accounts remain ineligible. Existing eligible accounts whose registration grant failed can still use the established `/me` or sign-in repair path and receive the current default once. The migration itself creates no user grants and performs no blanket credit allocation.

Previously delivered grants remain exactly as issued. In particular, a ten-reply grant with zero remaining stays at zero; it is not topped up to twenty. Login, logout, provider linking, token refresh and a new calendar day do not replenish an existing welcome grant. Purchased units and expiration rules are unchanged. Base remains 40/day; valid Pro remains unlimited. Prices are unchanged.

The published catalog is versioned safely: when its Free quantity is still the old default, a new publication changes only that Free quantity. The prior JSON is retained for history and sold checkout snapshots. Other product prices, mapped store IDs, names, visibility and purchased entitlements are copied unchanged. A draft based on the replaced publication is rebased and its revision increases; unrelated owner edits and custom Free quantities remain. A previously stale draft remains stale.

Default plan descriptions are changed only when their text exactly matches the old ten-reply description. An administrator's custom text is retained. A disabled or custom runtime welcome setting is not automatically rewritten merely to match catalog marketing; review such an existing override explicitly before changing it.

## Investigating a displayed zero

Zero by itself is insufficient to diagnose the production account. Inspect the authenticated `/api/v1/me` or `/api/v1/me/usage` response and distinguish these cases:

| Observation | Meaning |
| --- | --- |
| Daily fields zero, welcome remaining positive | Expected lifetime Free model; show the welcome/total balance. |
| Existing welcome grant has zero remaining | Already spent; normal login does not issue another grant. |
| No welcome grant, account before cutoff | Ineligible under the existing cutoff. |
| No welcome grant, setting is zero | Administrator disabled the welcome grant. |
| No welcome grant, eligible new account | Check grant failure logs/storage and the existing repair path. |
| Total includes purchased credits | Preserve those units; they are a separate permanent balance. |

Read-only operator checks, after obtaining authorized database access:

```sql
SELECT key,value FROM system_settings
WHERE key IN ('welcome_bonus_units','welcome_bonus_since');

-- Bind the account ID parameter; avoid dumping all customer identities.
SELECT id,kind,created_at FROM users WHERE id=?;
SELECT kind,units_total,units_remaining,status,starts_at,expires_at
FROM credit_grants WHERE user_id=? ORDER BY created_at;
```

Account creation/cutoff timestamps are Unix milliseconds. Do not repair a suspected zero by deleting its spent grant, changing a paid balance or repeatedly creating accounts. An additional grant to an existing spent/ineligible account requires an explicit authorized administrative credit adjustment and its normal audit trail.

## Deployment and rollback

This is a repository change until the approved backend deployment runs migration 0035. Back up SQLite using the existing WAL-safe process and run migrations against a copy first. After deployment, verify the stored welcome setting, the new published Free quantity, preserved cutoff and a sandbox new-registration response. Do not use a real paid transaction for verification.

Rollback the future welcome setting using the existing administrator control; existing delivered grants remain as issued. Do not delete migration history or alter existing credit rows to reverse this default. Retain the catalog history and use a new reviewed publication for any catalog rollback. Returning to an earlier binary does not undo the stored setting, and an old binary's fallback remains its old value only if a setting is missing or invalid.

## Verification

Coverage is in `internal/catalog/welcome_twenty_migration_test.go` and `internal/apptest/welcome_twenty_test.go`. Tests exercise fresh defaults, guarded migration, immutable history/balances/prices, preserved custom/disabled settings, draft revisions, migration failure/rollback/retry, once-only registration and missing-grant repair, no daily replenishment, idempotent generation, packages, Base and Pro. Tests use local fake providers; no real payment or notification delivery is performed.
