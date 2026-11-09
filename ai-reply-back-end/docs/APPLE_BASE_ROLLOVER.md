# Apple Base permanent replies

## Monthly bundle (migration 0037)

New Base Monthly purchases after `apple_base_monthly_since` grant the full
1,000-reply bundle for each verified paid transaction. The unique
`credit_grants.iap_purchase_id` index prevents duplicate delivery. Grants use
the existing permanent pack balance with `expires_at=NULL` and product code
`apple_base_monthly`. They remain spendable across devices after cancellation,
expiry or an upgrade to Pro. Pro access does not consume these credits.

Refunds remove only the remaining units in the same database transaction as
the subscription lifecycle update. A newer signed refund reversal restores
exactly those removed units. Replaying a purchase or changing the current
catalog cannot increase its already sold grant quantity. Historical daily
periods retain their own immutable accounting, including delayed replays of
transactions purchased before the rollout boundary.

The IAP product publishes `entitlement_kind:monthly`, `monthly_limit:1000`,
`daily_limit:0` and `credits_never_expire:true`. Usage has both quota limits zero:
generation spends the permanent balance, rather than a resetting calendar cap.

The new business prices are Base Monthly 990 KZT, Pro Monthly 3,990 KZT,
Pro Yearly 39,990 KZT; the permanent 30-reply pack remains 130 KZT.
Base Yearly is hidden from new sales while its existing periods remain usable.
App Store price points must be saved and verified separately before rollout.

BCC monthly purchases use the immutable order's `monthly_limit` to grant one
permanent bundle per authenticated paid order. Duplicate notifications cannot
grant it twice; cancellation and expiry retain it, while a confirmed refund
revokes its remainder. Legacy bank mandates retain their saved daily terms
and prices. Manually assigned legacy Base access retains its prior daily quota.

These changes require a backend deployment; rebuilding iOS does not deploy them.
Do not run an older binary over the migrated database. A rollback stops the
backend and restores the full saved data directory with its matching image, as
`docs/PRODUCTION_RELEASE.md` describes.

## Historical daily rollover (migration 0036)

Base Monthly and Base Yearly bought through Apple continue to add the sold daily
quantity (currently 40). Unused replies accumulate permanently. They remain
spendable after cancellation of renewal, subscription expiry, or upgrade to Pro.
The balance belongs to the account and is usable from every supported device.
Bank and manually assigned Base subscriptions retain their existing daily quota;
this migration changes the purchased Apple Base entitlement only.

## Accounting

- `apple_base_credit_periods` retains each exact verified Apple paid interval and
  its daily quantity. Transaction replays cannot change the sold daily quantity.
  Verified billing grace extends access; an immediate upgrade uses the raw signed
  interval together with the verified replacement purchase time to stop accrual.
- `apple_base_credit_days` has a primary key on account/calendar date. The
  configured server timezone determines the day. The first partial paid day adds
  one allowance. No allowance is added before purchase, after the paid interval,
  or at an exact midnight expiry. Renewals and overlapping Base transactions on
  the same date do not multiply the daily quantity.
- Each accrued day creates an ordinary `pack` credit grant with `expires_at=NULL`
  and product code `apple_base_daily`. Existing reservation/refund/ledger code
  spends it atomically. Missed days are materialized on entitlement reads,
  including after an offline paid period has ended. `/me/credits` also accrues.
- Refund/revoke notifications remove only unspent credits for days with no other
  valid paid Apple Base coverage. The update commits in the notification's own
  database transaction. A newer refund reversal restores exactly the removed
  remainder. Earned credits survive a normal upgrade or cancellation.
- A Pro replacement records a separate accrual cap. Reposting an old Base
  transaction cannot reopen the interval. An upgraded historical Base transaction
  without a verified replacement boundary waits for that boundary rather than
  inventing paid days; already earned grants remain intact.

## Existing accounts and limits of historical recovery

Migration 0036 copies the latest exact Apple Base interval retained in
`apple_subscriptions`, including an already expired interval. It leaves existing
credit grants and ledger rows intact. Earlier renewals did not retain signed
expiry dates in the old schema; their dates are not guessed. A later valid signed
transaction replay can add its exact historical interval without replacing a
newer current subscription. Normal iOS Restore currently posts current
entitlements and unfinished transactions, so it does not promise full historical
recovery of expired renewals.

Backfill subtracts already charged `daily` reservations inside recorded Base
intervals. Pro/unlimited and pack reservations are excluded. When reservation
retention has deleted attribution, retained `usage_daily.used` counters provide a
conservative fallback: unattributed used units are deducted, capped at that day's
allowance. This prevents old spent replies from being minted again; ambiguous
overlap with another daily plan can reduce historical recovery. Normal retention
keeps the daily/monthly counters while deleting old detailed reservations.
An in-flight legacy daily reservation is linked to its backfilled grant so a later
provider failure refunds its permanent unit exactly once.

## API and rollout

Apple Base products retain `daily_limit:40` and publish
`credits_never_expire:true`. Account usage publishes `daily_limit:0`, an active
`daily_credit_grant:40`, and `credits_never_expire:true` when Apple Base history
exists. Accrued units appear in `purchased_credits`, `credits`, and
`remaining_total`. Generation responses carry the same metadata. Existing
clients can use their existing total/purchased balance fields.

Deploy the backend with migration 0036 before distributing clients that promise
permanent Base reply balances. Keep the server quota timezone stable. Migration
0037 sets Base's daily limit to 0, so reverting only the binary no longer restores
the old daily quota; see the rollback note in the monthly bundle section above.
No production deployment is performed by this code change.

## Validation

- `go test ./...` and `go vet ./...` passed.
- Seven independent rollover regressions passed with `-race`, including spending
  41 accumulated replies in one day, delayed upgrades and overlapping refunds.
- The populated-database migration test covers preserved balances, prior usage,
  exact expiry, repeat migration/accrual and account deletion.
- 102 focused iOS tests passed; after the final display correction, the 21 balance
  and subscription-display tests passed again. The app and keyboard build passed.
