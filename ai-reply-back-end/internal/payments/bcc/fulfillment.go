package bcc

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/aireply/ai-reply-back-end/internal/catalog"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// fulfillAuthenticatedPack is reachable only after Basic Auth and the complete
// original-operation binding have verified. Query wrappers and browser returns
// have no access to this path. Every lookup stays in the configured bank environment.
func (s *CheckoutService) fulfillAuthenticatedPack(ctx context.Context, order BankOrder, safe map[string]string) error {
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		var user, status, raw string
		if err := tx.QueryRowContext(ctx, `SELECT coalesce(user_id,''),status,snapshot_json FROM billing_orders WHERE id=? AND environment=?`, order.ID, s.Environment()).Scan(&user, &status, &raw); err != nil {
			return err
		}
		if status == "succeeded" || status == "refunded" {
			return nil
		}
		if status != "created" && status != "pending" && status != "verification_pending" && status != "needs_review" {
			return fmt.Errorf("bcc: order cannot be fulfilled in its current state")
		}
		var product catalog.Product
		if err := json.Unmarshal([]byte(raw), &product); err != nil {
			return err
		}
		if product.Kind != "pack" || product.Units <= 0 || product.Code != order.ProductCode || product.PriceMinor != order.AmountMinor || product.Currency != order.Currency {
			return ErrInvalidRequest
		}
		now := s.now().UnixMilli()
		safeRaw, _ := json.Marshal(safe)
		if user == "" {
			_, err := tx.ExecContext(ctx, `UPDATE billing_orders SET status='needs_review',updated_at=? WHERE id=?`, now, order.ID)
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO billing_order_events(id,order_id,source,state,fields_json,created_at) VALUES(?,?,'authenticated_notify','account_deleted',?,?)`, "deleted-"+order.ID, order.ID, string(safeRaw), now)
			return err
		}
		grantID := traits.NewID()
		reason := "Verified BCC " + string(s.Environment()) + " purchase"
		if _, err := tx.ExecContext(ctx, `INSERT INTO credit_grants(id,user_id,kind,units_total,units_remaining,starts_at,status,product_code,granted_by,reason,created_at,updated_at,billing_order_id) VALUES(?,?,'pack',?,?,?,'active',?,'system',?,?,?,?)`, grantID, user, product.Units, product.Units, now, product.Code, reason, now, now, order.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO credit_ledger(id,user_id,grant_id,entry,kind,delta,actor,reason,created_at) VALUES(?,?,?,'grant','pack',?,'system',?,?)`, traits.NewID(), user, grantID, product.Units, reason, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE billing_orders SET status='succeeded',updated_at=? WHERE id=?`, now, order.ID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO billing_order_events(id,order_id,source,state,fields_json,created_at) VALUES(?,?,'authenticated_notify','succeeded',?,?)`, "fulfilled-"+order.ID, order.ID, string(safeRaw), now)
		return err
	})
}
