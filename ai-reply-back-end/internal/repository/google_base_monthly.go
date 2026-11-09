package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

const (
	googleBaseMonthlyCode   = "google_base_monthly"
	googleBaseMonthlyReason = "Google Base monthly bundle"
)

// grantGoogleBaseMonthly adds the sold bundle of one order the server has just
// recorded, in that same transaction. A replay records no order and never gets
// here, and the grant is bound to the order's iap_purchases row, so the unique
// credit_grants.iap_purchase_id index keeps delivery exactly-once.
//
// The quantity comes from the plan now and is never read again: a later change
// of the catalogue cannot alter what was sold. The grant has no expiry and
// survives cancellation, expiry and an upgrade to Pro.
func grantGoogleBaseMonthly(ctx context.Context, tx *sql.Tx, in GoogleSubscription, purchase *domain.IAPPurchase, now time.Time) error {
	var code, kind string
	var total int
	if err := tx.QueryRowContext(ctx, `SELECT code,entitlement_kind,monthly_message_limit FROM plans WHERE id=?`, in.PlanID).Scan(&code, &kind, &total); err != nil {
		return err
	}
	if code != "base_monthly" || kind != domain.EntitlementMonthly || total <= 0 {
		return nil
	}
	if in.LinkedTokenHash != "" && purchase.TransactionID == in.TokenHash {
		// A replacement moves the time already paid for onto the new plan and
		// charges nothing, so its first order is not a new payment and cycling
		// between Base and Pro must not mint bundles. It still brings one when the
		// account has had none for a month: a downgrade from Pro would otherwise
		// be a Base period with nothing to spend.
		var recent bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM credit_grants
			WHERE user_id=? AND product_code=? AND status='active' AND created_at>?)`,
			in.UserID, googleBaseMonthlyCode, ms(now.AddDate(0, -1, 0))).Scan(&recent); err != nil {
			return err
		}
		if recent {
			return nil
		}
	}
	// A renewal read shortly before the old period ends is already paid for.
	starts := now
	if purchase.PurchasedAt != nil && purchase.PurchasedAt.Before(now) {
		starts = *purchase.PurchasedAt
	}
	grant := traits.NewID()
	res, err := tx.ExecContext(ctx, `INSERT INTO credit_grants
		(id,user_id,kind,units_total,units_remaining,starts_at,expires_at,status,iap_purchase_id,product_code,granted_by,reason,created_at,updated_at)
		VALUES (?,?,'pack',?,?,?,NULL,'active',?,?,'system',?,?,?)
		ON CONFLICT(iap_purchase_id) WHERE iap_purchase_id IS NOT NULL DO NOTHING`,
		grant, in.UserID, total, total, ms(starts), purchase.ID, googleBaseMonthlyCode, googleBaseMonthlyReason, ms(now), ms(now))
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE iap_purchases SET units=? WHERE id=?`, total, purchase.ID); err != nil {
		return err
	}
	purchase.Units = total
	return insertLedger(ctx, tx, domain.LedgerEntry{UserID: in.UserID, GrantID: grant, Entry: domain.LedgerGrant, Kind: domain.GrantPack,
		Delta: total, Actor: "system", Reason: googleBaseMonthlyReason, CreatedAt: now}, "")
}

// revokeGoogleBaseMonthly takes back what is left of one order's bundle, in the
// refund's own transaction. What was spent stays spent; the bundles of other
// orders and the account's packs are not touched.
func revokeGoogleBaseMonthly(ctx context.Context, tx *sql.Tx, purchaseID, reason string, now time.Time) error {
	var grant, user string
	var remaining int
	err := tx.QueryRowContext(ctx, `SELECT id,user_id,units_remaining FROM credit_grants
		WHERE iap_purchase_id=? AND product_code=? AND status='active'`, purchaseID, googleBaseMonthlyCode).Scan(&grant, &user, &remaining)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE credit_grants SET status='revoked',units_remaining=0,revoked_at=?,revoked_by='system',revoke_reason=?,updated_at=? WHERE id=?`,
		ms(now), reason, ms(now), grant); err != nil {
		return err
	}
	return insertLedger(ctx, tx, domain.LedgerEntry{UserID: user, GrantID: grant, Entry: domain.LedgerRevoke, Kind: domain.GrantPack,
		Delta: -remaining, Actor: "system", Reason: reason, CreatedAt: now}, "")
}
