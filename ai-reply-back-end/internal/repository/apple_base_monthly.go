package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// recordAppleBaseMonthly grants the complete sold bundle once per verified
// transaction. The existing purchase/grant unique index serializes concurrent
// deliveries. An old daily period always retains its original accounting.
func recordAppleBaseMonthly(ctx context.Context, tx *sql.Tx, in AppleSubscription, purchaseID string, now time.Time) (bool, error) {
	var grant, status, revokedBy string
	var total, remaining int
	err := tx.QueryRowContext(ctx, `SELECT id,status,revoked_by,units_total,units_remaining FROM credit_grants
 WHERE iap_purchase_id=? AND product_code='apple_base_monthly'`, purchaseID).Scan(&grant, &status, &revokedBy, &total, &remaining)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if errors.Is(err, sql.ErrNoRows) {
		var code, kind string
		var since int64
		var dailyPeriod bool
		err = tx.QueryRowContext(ctx, `SELECT code,entitlement_kind,monthly_message_limit,
 COALESCE((SELECT CAST(value AS INTEGER) FROM system_settings WHERE key='apple_base_monthly_since'),0),
 EXISTS(SELECT 1 FROM apple_base_credit_periods WHERE purchase_id=?) FROM plans WHERE id=?`, purchaseID, in.PlanID).Scan(&code, &kind, &total, &since, &dailyPeriod)
		if err != nil {
			return false, err
		}
		if code != "base_monthly" || kind != domain.EntitlementMonthly || total <= 0 || dailyPeriod || ms(in.PurchasedAt) < since {
			return false, nil
		}
		end := in.ExpiresAt
		if in.PaidExpiresAt.After(end) {
			end = in.PaidExpiresAt
		}
		if !end.After(in.PurchasedAt) {
			return true, nil
		}
		grant, status, remaining = traits.NewID(), domain.GrantActive, total
		_, err = tx.ExecContext(ctx, `INSERT INTO credit_grants
 (id,user_id,kind,units_total,units_remaining,starts_at,expires_at,status,iap_purchase_id,product_code,granted_by,reason,created_at,updated_at)
 VALUES (?,?,'pack',?,?,?,NULL,'active',?,'apple_base_monthly','system','Apple Base monthly bundle',?,?)`, grant, in.UserID, total, total, ms(in.PurchasedAt), purchaseID, ms(now), ms(now))
		if err != nil {
			return false, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE iap_purchases SET units=? WHERE id=?`, total, purchaseID); err != nil {
			return false, err
		}
		if err = insertLedger(ctx, tx, domain.LedgerEntry{UserID: in.UserID, GrantID: grant, Entry: domain.LedgerGrant, Kind: domain.GrantPack, Delta: total, Actor: "system", Reason: "Apple Base monthly bundle", CreatedAt: now}, ""); err != nil {
			return false, err
		}
	}
	// Use the persisted lifecycle, not the delivered snapshot: older notifications
	// cannot undo a refund, and a reversal restores only the unspent remainder.
	var purchaseStatus string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM iap_purchases WHERE id=?`, purchaseID).Scan(&purchaseStatus); err != nil {
		return false, err
	}
	if (purchaseStatus == domain.PurchaseRevoked || purchaseStatus == domain.PurchaseRefunded) && status == domain.GrantActive {
		_, err = tx.ExecContext(ctx, `UPDATE credit_grants SET status='revoked',units_remaining=0,revoked_at=?,revoked_by='system',revoke_reason='apple_base_monthly_refund',updated_at=? WHERE id=?`, ms(now), ms(now), grant)
		if err == nil {
			err = insertLedger(ctx, tx, domain.LedgerEntry{UserID: in.UserID, GrantID: grant, Entry: domain.LedgerRevoke, Kind: domain.GrantPack, Delta: -remaining, Actor: "system", Reason: "apple_base_monthly_refund", CreatedAt: now}, "")
		}
	} else if purchaseStatus == domain.PurchaseGranted && status == domain.GrantRevoked && revokedBy == "system" && in.Restore {
		var removed int
		err = tx.QueryRowContext(ctx, `SELECT -delta FROM credit_ledger WHERE grant_id=? AND entry='revoke' AND reason='apple_base_monthly_refund' ORDER BY rowid DESC LIMIT 1`, grant).Scan(&removed)
		if errors.Is(err, sql.ErrNoRows) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		removed = min(total, removed)
		_, err = tx.ExecContext(ctx, `UPDATE credit_grants SET status='active',units_remaining=?,revoked_at=NULL,revoked_by='',revoke_reason='',updated_at=? WHERE id=?`, removed, ms(now), grant)
		if err == nil {
			err = insertLedger(ctx, tx, domain.LedgerEntry{UserID: in.UserID, GrantID: grant, Entry: domain.LedgerGrant, Kind: domain.GrantPack, Delta: removed, Actor: "system", Reason: "apple_base_monthly_refund_reversed", CreatedAt: now}, "")
		}
	}
	return true, err
}
