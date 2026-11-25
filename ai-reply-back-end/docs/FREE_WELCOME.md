# Free: 10 replies once at registration

The default Free allowance is **10 replies once per eligible account**. It is a welcome grant, not a daily or monthly plan quota. The existing registration/provisioning flow creates it; database uniqueness permits only one welcome grant for that user. Existing authentication, idempotency, reservation/refund and credit consumption rules are reused.

`usage.daily_limit` and `usage.remaining_today` remain zero for the Free plan because there is no recurring allowance. The usable welcome amount appears in `usage.welcome.total`, `usage.welcome.remaining` and `usage.remaining_total`. Purchased packages contribute separately to the latter. A client showing only the daily field can display zero while a welcome balance remains.

## Migration 0044

`0044_google_monthly_free_ten.sql` changes the prior default of `20` to `10`, matching the runtime fallback and fresh-install catalog. Explicit `0` (disabled) and custom quantities remain authoritative. This welcome grant is account-wide and shared across platforms.

The migration preserves `welcome_bonus_since`, existing grants and their remaining balances, usage, purchases, subscriptions and checkout snapshots. An existing 20-reply grant remains exactly as issued; spent grants are never replenished. Eligible accounts whose registration grant failed still receive the current default once through the existing repair flow. Login, token refresh and a new day do not add replies.

It versions the published Free catalog quantity, preserves its historical JSON and unrelated owner edits, and rebases only drafts of the replaced publication. A stale draft stays stale. Default plan descriptions change only when they exactly match the previous 20-reply text; custom text is retained. Separately, Android `pack500` is retired from new sales without deleting its durable store identity or already purchased credits.

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

This is a repository change until the approved backend deployment runs migration 0044. Back up SQLite using the existing WAL-safe process and run migrations against a copy first. After deployment, verify the stored welcome setting, the new published Free quantity, preserved cutoff and a sandbox new-registration response. Do not use a real paid transaction for verification.

Rollback the future welcome setting using the existing administrator control; existing delivered grants remain as issued. Do not delete migration history or alter existing credit rows to reverse this default. Retain the catalog history and use a new reviewed publication for any catalog rollback. Returning to an earlier binary does not undo the stored setting, and an old binary's fallback remains its old value only if a setting is missing or invalid.

## Verification

Coverage is in `internal/catalog/google_monthly_migration_test.go` and `internal/apptest/welcome_ten_test.go`. Tests exercise fresh defaults, guarded migration, immutable history/balances/prices, preserved custom/disabled settings, draft revisions, migration failure/rollback/retry, once-only registration and missing-grant repair, no daily replenishment, idempotent generation, packages, Base and Pro. Tests use local fake providers; no real payment or notification delivery is performed.
