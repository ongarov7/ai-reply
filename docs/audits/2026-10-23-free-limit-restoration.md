# Free daily allowance restoration — 2026-10-23

The owner requested restoring the existing free plan from 20 to 7 daily replies automatically when the backend is updated. This is separate from the one-time welcome bonus of 20 replies.

## Confirmed source and current public state

Safe public GET `https://ai-reply.kz/api/v1/plans` returned HTTP 200 and the canonical free plan (`code=free`, existing seeded ID ending `0001`, `is_free=true`) with `daily_message_limit=20`. All four descriptions were the original seed boilerplate with its number changed to 20:

- KK: `Күніне 20 жауап. Картасыз бастаңыз.`
- RU: `20 ответов в день. Начните без карты.`
- EN: `20 replies per day. Start without a card.`
- UZ: `Kuniga 20 ta javob. Kartasiz boshlang.`

Repository schema (`0001_init.sql`) and initial seed (`0002_seed.sql`) already specify daily allowance 7. No existing repository migration sets the free daily limit to 20. The live database therefore differs from the initial seed. The specific administrator or manual SQL action responsible is not established; no admin audit log or private production DB was accessed.

Repository history provides the intended configuration context: commit `19fba43` (`docs: free plan 20 a day, welcome bonus off`, 21 October) documented the owner's previous decision to use daily allowance 20 and welcome bonus 0. The new 23 October request supersedes that daily allowance decision; the bonus remains off. This explains the documented intent, while the exact production database write remains unverified.

`internal/plans/service.go` and `internal/repository/plans.go` read editable plan values from SQLite on every request. Admin `/plans` saves `daily_message_limit` and the four descriptions independently. The descriptions are literal editable text, not dynamic number placeholders. `internal/transport/api/dto.go` returns both fields; the landing page also renders literal description and structured daily limit independently. Changing the original seed or a mobile label would not repair existing deployments.

Migration `0019_welcome_bonus_20.sql` only changes `system_settings.welcome_bonus_units` (one-time welcome credits), not `plans.daily_message_limit` (daily recurring allowance). It must remain unchanged.

## Implemented change

`ai-reply-back-end/migrations/0021_free_daily_limit_7.sql`:

- Updates only `code='free' AND is_free=1` to `daily_message_limit=7`.
- Synchronizes a localized description only when it exactly matches the known 20-reply boilerplate above. Custom admin descriptions, including other text containing 20, stay unchanged.
- Preserves plan names, monthly limit, visibility, active state, paid plans, subscriptions, usage counters, credits and welcome-bonus settings.
- Runs once through the existing transactional `schema_migrations` startup mechanism (`cmd/server/main.go`). Subsequent administrator edits remain authoritative on later restarts.
- Reapplying its SQL to an already-restored plan is a no-op.

No runtime hardcoding, mobile change, new dependency or production deployment was introduced. On deployment, existing users with more than 7 replies already used today retain that usage; lowering the plan does not reset their counters.

## Verification

`internal/apptest/free_limit_migration_test.go` creates an actual existing database through migration 0020, changes its free plan to 20, then applies the new migration. It verifies:

- Existing 20 → 7 on upgrade.
- Known boilerplate updated in all four languages.
- Custom multilingual admin text and names preserved exactly.
- A paid plan independently using limit 20 and the same text remains byte-for-byte equivalent at the domain level, including its timestamp.
- Monthly allowance, visibility and active status preserved.
- Welcome bonus remains 20 and existing usage remains 9.
- SQL idempotence after restoration.
- A later admin change to 12 and new description remains untouched on the next startup.

Focused command:

```sh
cd ai-reply-back-end
go test ./internal/apptest -run 'TestFreeDailyLimitUpgrade|TestAdminChangesTheFreeLimit' -count=1
```

Passed (1.757s), including the existing API enforcement test which changes limits through the admin and validates quota behavior. Full-suite result is recorded after completion below.

No production write, credential access, deployment or notification send was performed. Public GET is the only live check; the actual server will remain at 20 until the owner updates/restarts it with the new backend migration.

Final full regression:

```sh
cd ai-reply-back-end
go test ./... -count=1
```

Passed for every package (apptest 50.325s). `git diff --check` passed. The only implementation change for this allowance correction is migration 0021; no existing runtime/admin behavior was rewritten.

Documentation consistency pass:

- `docs/product-overhaul-report.md`: current decision and new-account daily verification now say 7, superseding the explicitly identified 21 October daily-20 decision. Welcome bonus remains off. Deployment notes include migrations 0020/0021 and their non-destructive data updates.
- `ai-reply-back-end/README.md`: existing seed-7 documentation retained; added the one-time existing-database restoration and preservation of later admin changes.
- `docs/STORE_RELEASE_AUDIT.md` already says Free 7/day; it and other historical evidence remain unchanged.
- The example `Бесплатные ответы: 15 из 20` is the separate welcome-bonus balance label (`balance_welcome_title` in Android), not the daily allowance. It and all separate 20-credit welcome-bonus descriptions remain unchanged.
