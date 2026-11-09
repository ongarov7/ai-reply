package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// recordAppleBasePeriod retains each verified purchase separately from the
// latest subscription state. The sold daily quantity is immutable on replay.
func recordAppleBasePeriod(ctx context.Context, tx *sql.Tx, in AppleSubscription, purchaseID string) error {
	var code string
	var units int
	if err := tx.QueryRowContext(ctx, `SELECT code,daily_message_limit FROM plans WHERE id=?`, in.PlanID).Scan(&code, &units); err != nil {
		return err
	}
	if code != "base_monthly" && code != "base_yearly" {
		// A signed replacement (including Pro) stops future Base accrual. Already
		// earned grants remain permanent, including on an immediate upgrade.
		_, err := tx.ExecContext(ctx, `UPDATE apple_base_credit_periods SET accrual_cap_at=MIN(COALESCE(accrual_cap_at,expires_at),?)
	   WHERE original_transaction_id=? AND starts_at<?`, ms(in.PurchasedAt), in.OriginalTransactionID, ms(in.PurchasedAt))
		return err
	}
	if units <= 0 {
		// Historical signed purchases keep the quantity sold before the monthly
		// rollout; replaying one must not mint a new 1,000-reply bundle.
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(
 (SELECT daily_units FROM apple_base_credit_periods WHERE purchase_id=?),
 (SELECT CAST(value AS INTEGER) FROM system_settings WHERE key='apple_base_legacy_daily_units'),0)`, purchaseID).Scan(&units); err != nil {
			return err
		}
		if units <= 0 {
			return nil
		}
	}
	end := in.ExpiresAt
	// Keep verified billing-grace access while retaining the raw signed end
	// when the normal entitlement path synthetically expires an upgraded item.
	if in.PaidExpiresAt.After(end) {
		end = in.PaidExpiresAt
	}
	if !end.After(in.PurchasedAt) {
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO apple_base_credit_periods
  (purchase_id,user_id,original_transaction_id,starts_at,expires_at,daily_units,signed_at,revoked_at,upgraded)
  VALUES (?,?,?,?,?,?,?,?,?) ON CONFLICT(purchase_id) DO UPDATE SET
  expires_at=CASE WHEN ? THEN excluded.expires_at ELSE MAX(apple_base_credit_periods.expires_at,excluded.expires_at) END,
  signed_at=excluded.signed_at,revoked_at=excluded.revoked_at,upgraded=MAX(apple_base_credit_periods.upgraded,excluded.upgraded)
  WHERE excluded.signed_at>=apple_base_credit_periods.signed_at
   AND (apple_base_credit_periods.revoked_at IS NULL OR excluded.revoked_at IS NOT NULL OR ?)`,
		purchaseID, in.UserID, in.OriginalTransactionID, ms(in.PurchasedAt), ms(end), units, ms(in.SignedAt), msPtr(in.RevokedAt), in.Upgraded, in.MayShorten || in.RevokedAt != nil, in.Restore)
	if err != nil {
		return err
	}
	// An older transaction may be restored after a later Pro upgrade was
	// already recorded; recover the signed replacement boundary from history.
	var replacement sql.NullInt64
	if err = tx.QueryRowContext(ctx, `SELECT MIN(purchased_at) FROM iap_purchases
  WHERE provider='app_store' AND original_transaction_id=? AND product_code IN ('pro_monthly','pro_yearly')
   AND purchased_at>? AND status IN ('granted','refunded','revoked')`, in.OriginalTransactionID, ms(in.PurchasedAt)).Scan(&replacement); err != nil {
		return err
	}
	if replacement.Valid {
		_, err = tx.ExecContext(ctx, `UPDATE apple_base_credit_periods SET accrual_cap_at=MIN(COALESCE(accrual_cap_at,expires_at),?) WHERE purchase_id=?`, replacement.Int64, purchaseID)
	}

	return err
}

type appleBasePeriod struct {
	id         string
	start, end time.Time
	units      int
	revoked    bool
}
type appleBaseDay struct {
	grant, purchase, status, revokedBy string
	remaining, total                   int
}

// AccrueAppleBaseCredits materializes every elapsed paid calendar day, even if
// the app was closed throughout the paid period or the subscription has ended.
// One transaction and the account/date primary key serialize concurrent readers.
// Returned allowance is only present while a paid Base period is running.
func (s *Store) AccrueAppleBaseCredits(ctx context.Context, user string, now time.Time, loc *time.Location) (int, bool, error) {
	var allowance int
	var hasHistory bool
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM credit_grants WHERE user_id=? AND product_code IN ('apple_base_monthly','bcc_base_monthly','google_base_monthly'))`, user).Scan(&hasHistory); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT period.purchase_id,period.starts_at,
    CASE WHEN period.upgraded AND period.accrual_cap_at IS NULL THEN period.starts_at ELSE MIN(period.expires_at,COALESCE(period.accrual_cap_at,period.expires_at)) END,period.daily_units,
    period.revoked_at IS NOT NULL OR purchase.status IN ('refunded','revoked')
    FROM apple_base_credit_periods period JOIN iap_purchases purchase ON purchase.id=period.purchase_id
    WHERE period.user_id=? ORDER BY period.starts_at,period.purchase_id`, user)
		if err != nil {
			return err
		}
		var periods []appleBasePeriod
		for rows.Next() {
			var p appleBasePeriod
			var start, end int64
			if err = rows.Scan(&p.id, &start, &end, &p.units, &p.revoked); err != nil {
				rows.Close()
				return err
			}
			p.start, p.end = timeFrom(start), timeFrom(end)
			periods = append(periods, p)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(periods) == 0 {
			return nil
		}
		hasHistory = true
		days := map[string]appleBaseDay{}
		rows, err = tx.QueryContext(ctx, `SELECT day.usage_date,day.grant_id,day.purchase_id,g.status,g.revoked_by,g.units_remaining,g.units_total
   FROM apple_base_credit_days day JOIN credit_grants g ON g.id=day.grant_id WHERE day.user_id=?`, user)
		if err != nil {
			return err
		}
		for rows.Next() {
			var date string
			var d appleBaseDay
			if err = rows.Scan(&date, &d.grant, &d.purchase, &d.status, &d.revokedBy, &d.remaining, &d.total); err != nil {
				rows.Close()
				return err
			}
			days[date] = d
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		eligible := map[string]appleBasePeriod{}
		for _, p := range periods {
			if p.revoked || p.start.After(now) || !p.end.After(p.start) {
				continue
			}
			if !now.Before(p.start) && now.Before(p.end) {
				allowance = max(allowance, p.units)
			}
			start := p.start.In(loc)
			date := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc)
			for !date.After(now) && date.Before(p.end) {
				key := date.Format("2006-01-02")
				if existing, ok := eligible[key]; !ok || p.units > existing.units {
					eligible[key] = p
				}
				date = date.AddDate(0, 0, 1)
			}
		}
		for date, p := range eligible {
			if _, ok := days[date]; ok {
				continue
			}
			start, err := time.ParseInLocation("2006-01-02", date, loc)
			if err != nil {
				return err
			}
			// Attribute old charges by reservation time and daily bucket. Pro and
			// purchased packs never count as legacy Base expenditure.
			used := 0
			legacy := []string{}
			usage, err := tx.QueryContext(ctx, `SELECT id,created_at FROM usage_reservations WHERE user_id=? AND usage_date=?
    AND bucket='daily' AND status IN ('reserved','committed')`, user, date)
			if err != nil {
				return err
			}
			for usage.Next() {
				var reservation string
				var at int64
				if err = usage.Scan(&reservation, &at); err != nil {
					usage.Close()
					return err
				}
				when := timeFrom(at)
				for _, candidate := range periods {
					if !candidate.revoked && !when.Before(candidate.start) && when.Before(candidate.end) {
						if used < p.units {
							used++
							legacy = append(legacy, reservation)
						}
						break
					}
				}
			}
			err = usage.Err()
			usage.Close()
			if err != nil {
				return err
			}
			used = min(used, p.units)
			// Retention can delete old reservations before signed history is
			// restored. Conservatively account for unattributed counter units;
			// never mint previously consumed replies because their rows aged out.
			var counter, retained int
			if err = tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT used FROM usage_daily WHERE user_id=? AND usage_date=?),0),
    (SELECT COUNT(*) FROM usage_reservations WHERE user_id=? AND usage_date=? AND bucket IN ('daily','unlimited') AND status IN ('reserved','committed'))`, user, date, user, date).Scan(&counter, &retained); err != nil {
				return err
			}
			used = min(p.units, used+max(counter-retained, 0))
			grant := traits.NewID()
			left := p.units - used
			_, err = tx.ExecContext(ctx, `INSERT INTO credit_grants
    (id,user_id,kind,units_total,units_remaining,starts_at,expires_at,status,product_code,granted_by,reason,created_at,updated_at)
    VALUES (?,?,'pack',?,?,?,NULL,'active','apple_base_daily','system','Apple Base daily rollover',?,?)`, grant, user, p.units, left, ms(start), ms(now), ms(now))
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO apple_base_credit_days(user_id,usage_date,starts_at,ends_at,purchase_id,grant_id)VALUES(?,?,?,?,?,?)`, user, date, ms(start), ms(start.AddDate(0, 0, 1)), p.id, grant)
			if err != nil {
				return err
			}
			if err = insertLedger(ctx, tx, domain.LedgerEntry{UserID: user, GrantID: grant, Entry: domain.LedgerGrant, Kind: domain.GrantPack, Delta: p.units, Reason: "Apple Base daily rollover", CreatedAt: now}, ""); err != nil {
				return err
			}
			if used > 0 {
				if err = insertLedger(ctx, tx, domain.LedgerEntry{UserID: user, GrantID: grant, Entry: domain.LedgerConsume, Kind: domain.GrantPack, Delta: -used, Reason: "Previously used Base replies", CreatedAt: now}, ""); err != nil {
					return err
				}
				for _, reservation := range legacy {
					if _, err = tx.ExecContext(ctx, `INSERT INTO apple_base_legacy_charges(reservation_id,grant_id)VALUES(?,?)`, reservation, grant); err != nil {
						return err
					}
				}
			}
			days[date] = appleBaseDay{grant: grant, purchase: p.id, status: domain.GrantActive, remaining: left, total: p.units}
		}
		return reconcileAppleBaseCreditDays(ctx, tx, user, now)
	})
	return allowance, hasHistory, err
}

// Reconcile in the same transaction as a refund/reversal, so a concurrent
// generation cannot spend a grant after the revocation has committed.
func reconcileAppleBaseCreditDays(ctx context.Context, tx *sql.Tx, user string, now time.Time) error {
	rows, err := tx.QueryContext(ctx, `SELECT day.grant_id,g.status,g.revoked_by,g.units_remaining,g.units_total,
  source.revoked_at IS NOT NULL OR owner.status IN ('refunded','revoked'),
  EXISTS(SELECT 1 FROM apple_base_credit_periods p JOIN iap_purchases purchase ON purchase.id=p.purchase_id
   WHERE p.user_id=day.user_id AND p.revoked_at IS NULL AND purchase.status='granted'
    AND p.starts_at<=? AND p.starts_at<day.ends_at
    AND CASE WHEN p.upgraded AND p.accrual_cap_at IS NULL THEN p.starts_at ELSE MIN(p.expires_at,COALESCE(p.accrual_cap_at,p.expires_at)) END>day.starts_at)
  FROM apple_base_credit_days day JOIN credit_grants g ON g.id=day.grant_id
  JOIN apple_base_credit_periods source ON source.purchase_id=day.purchase_id
  JOIN iap_purchases owner ON owner.id=source.purchase_id WHERE day.user_id=?`, ms(now), user)
	if err != nil {
		return err
	}
	type state struct {
		day              appleBaseDay
		revoked, covered bool
	}
	var states []state
	for rows.Next() {
		var v state
		if err = rows.Scan(&v.day.grant, &v.day.status, &v.day.revokedBy, &v.day.remaining, &v.day.total, &v.revoked, &v.covered); err != nil {
			rows.Close()
			return err
		}
		states = append(states, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, v := range states {
		d := v.day
		if !v.covered && v.revoked && d.status == domain.GrantActive {
			_, err = tx.ExecContext(ctx, `UPDATE credit_grants SET status='revoked',units_remaining=0,revoked_at=?,revoked_by='system',revoke_reason='apple_base_refund',updated_at=? WHERE id=?`, ms(now), ms(now), d.grant)
			if err != nil {
				return err
			}
			if err = insertLedger(ctx, tx, domain.LedgerEntry{UserID: user, GrantID: d.grant, Entry: domain.LedgerRevoke, Kind: domain.GrantPack, Delta: -d.remaining, Reason: "apple_base_refund", CreatedAt: now}, ""); err != nil {
				return err
			}
		} else if v.covered && d.status == domain.GrantRevoked && d.revokedBy == "system" {
			var taken int
			err = tx.QueryRowContext(ctx, `SELECT -delta FROM credit_ledger WHERE grant_id=? AND entry='revoke' AND reason='apple_base_refund' ORDER BY rowid DESC LIMIT 1`, d.grant).Scan(&taken)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			restored := min(taken, d.total)
			_, err = tx.ExecContext(ctx, `UPDATE credit_grants SET status='active',units_remaining=?,revoked_at=NULL,revoked_by='',revoke_reason='',updated_at=? WHERE id=?`, restored, ms(now), d.grant)
			if err != nil {
				return err
			}
			if err = insertLedger(ctx, tx, domain.LedgerEntry{UserID: user, GrantID: d.grant, Entry: domain.LedgerGrant, Kind: domain.GrantPack, Delta: restored, Reason: "apple_base_refund_reversed", CreatedAt: now}, ""); err != nil {
				return err
			}
		}
	}
	return nil
}
