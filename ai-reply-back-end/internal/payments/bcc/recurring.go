package bcc

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/catalog"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

const RecurringTermsVersion = "2026-11-01"

func periodDays(p catalog.Product) int {
	switch p.Code {
	case "base_monthly", "pro_monthly":
		if p.BillingPeriod == "month" {
			return 30
		}
	case "base_yearly", "pro_yearly":
		if p.BillingPeriod == "year" {
			return 365
		}
	}
	return 0
}

func (s *CheckoutService) RecurringEnabled() bool { return s.Enabled() && s.cfg.RecurringEnabled }

func (s *CheckoutService) validateMandate(ctx context.Context, tx *sql.Tx, user string, p catalog.Product, expected *ExpectedQuote) error {
	if !s.RecurringEnabled() || periodDays(p) == 0 {
		return domain.ErrPlanUnavailable
	}
	if expected == nil || !expected.RecurringConsent || expected.RecurringTermsVersion != RecurringTermsVersion {
		return domain.ConflictField("recurring_terms", "required")
	}
	until, err := time.Parse("20060102", expected.RecurringUntil)
	now := s.now().UTC()
	if err != nil || until.Format("20060102") != expected.RecurringUntil ||
		!until.After(now.Add(time.Duration(periodDays(p))*24*time.Hour)) || until.After(now.AddDate(5, 0, 0)) {
		return domain.InvalidField("recurring_until", "must_cover_the_next_period_and_not_exceed_the_card_expiry")
	}
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM bcc_subscriptions WHERE user_id=? AND environment=? AND status IN ('pending','active','needs_review')`, user, s.Environment()).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return domain.ConflictField("subscription", "already_active")
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM apple_subscriptions WHERE user_id=? AND auto_renew=1 AND revoked_at IS NULL AND expires_at>?`, user, now.UnixMilli()).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return domain.ConflictField("subscription", "manage_existing_store_subscription")
	}
	// Canceling a mandate does not resolve an already submitted money operation.
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM billing_orders WHERE user_id=? AND environment=? AND bcc_subscription_id IS NOT NULL AND status IN ('created','pending','verification_pending','needs_review')`, user, s.Environment()).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return domain.ConflictField("subscription", "payment_still_pending")
	}
	return nil
}

func (s *CheckoutService) createMandate(ctx context.Context, tx *sql.Tx, order BankOrder, p catalog.Product, expected *ExpectedQuote, rn string) error {
	var plan string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM plans WHERE code=? AND is_active=1 AND is_free=0`, p.Code).Scan(&plan); err != nil {
		return err
	}
	until, _ := time.Parse("20060102", expected.RecurringUntil)
	until = until.Add(24*time.Hour - time.Millisecond)
	id := traits.NewID()
	_, err := tx.ExecContext(ctx, `INSERT INTO bcc_subscriptions(id,user_id,environment,root_order_id,plan_id,snapshot_json,amount_minor,period_days,status,mandate_expires_at,merch_rn_id,created_at,updated_at)
 VALUES(?,?,?,?,?,?,?,?,'pending',?,?,?,?)`, id, order.userID, s.Environment(), order.ID, plan, order.snapshot, order.AmountMinor, periodDays(p), until.UnixMilli(), rn, order.createdAt, order.createdAt)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE billing_orders SET bcc_subscription_id=? WHERE id=?`, id, order.ID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE billing_order_context SET recurring_terms_version=?,recurring_until=? WHERE order_id=?`, RecurringTermsVersion, expected.RecurringUntil, order.ID)
	return err
}

// SubscriptionView contains account-facing information only, never bank references.
type SubscriptionView struct {
	ID            string            `json:"id"`
	ProductCode   string            `json:"product_code"`
	Name          map[string]string `json:"name"`
	Status        string            `json:"status"`
	AmountMinor   int64             `json:"amount_minor"`
	Currency      string            `json:"currency"`
	PeriodDays    int               `json:"period_days"`
	AutoRenew     bool              `json:"auto_renew"`
	PaidUntil     *time.Time        `json:"paid_until,omitempty"`
	NextChargeAt  *time.Time        `json:"next_charge_at,omitempty"`
	RenewalEndsAt time.Time         `json:"renewal_ends_at"`
}

func (s *CheckoutService) Subscriptions(ctx context.Context, user string) ([]SubscriptionView, error) {
	out := []SubscriptionView{}
	if s == nil || s.db == nil {
		return out, nil
	}
	rows, err := s.db.Reader().QueryContext(ctx, `SELECT id,snapshot_json,status,amount_minor,period_days,paid_until,next_due_at,mandate_expires_at FROM bcc_subscriptions WHERE user_id=? AND environment=? ORDER BY created_at DESC LIMIT 20`, user, s.Environment())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var v SubscriptionView
		var raw string
		var paid, next sql.NullInt64
		var until int64
		if err := rows.Scan(&v.ID, &raw, &v.Status, &v.AmountMinor, &v.PeriodDays, &paid, &next, &until); err != nil {
			return nil, err
		}
		var p catalog.Product
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			return nil, err
		}
		v.ProductCode, v.Name, v.Currency = p.Code, p.Name, p.Currency
		v.AutoRenew = v.Status == "active"
		if paid.Valid {
			t := time.UnixMilli(paid.Int64).UTC()
			v.PaidUntil = &t
		}
		if next.Valid && v.AutoRenew {
			t := time.UnixMilli(next.Int64).UTC()
			v.NextChargeAt = &t
		}
		v.RenewalEndsAt = time.UnixMilli(until).UTC()
		out = append(out, v)
	}
	return out, rows.Err()
}

// CancelSubscription stops future scheduling. Already paid periods remain usable.
func (s *CheckoutService) CancelSubscription(ctx context.Context, user, id string) error {
	if s == nil || s.db == nil {
		return domain.ErrNotFound
	}
	result, err := s.db.Writer().ExecContext(ctx, `UPDATE bcc_subscriptions SET status='canceled',next_due_at=NULL,updated_at=? WHERE id=? AND user_id=? AND environment=?`, s.now().UnixMilli(), id, user, s.Environment())
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return domain.ErrNotFound
	}
	return err
}

// RunDue persists each cycle before one outbound monetary request. A timeout or
// crash leaves it awaiting the bank, never eligible for a blind second charge.
func (s *CheckoutService) RunDue(ctx context.Context) error {
	if !s.RecurringEnabled() {
		return nil
	}
	for i := 0; i < 10; i++ {
		var fields url.Values
		var orderID string
		err := s.db.Tx(ctx, func(tx *sql.Tx) error {
			now := s.now().UTC()
			var subID, user, raw, root, rn, intRef, rrn string
			var amount, next, until int64
			if _, err := tx.ExecContext(ctx, `UPDATE bcc_subscriptions SET status='expired',next_due_at=NULL,updated_at=? WHERE status='active' AND environment=? AND mandate_expires_at<=?`, now.UnixMilli(), s.Environment(), now.UnixMilli()); err != nil {
				return err
			}
			err := tx.QueryRowContext(ctx, `SELECT b.id,b.user_id,b.snapshot_json,b.root_order_id,b.amount_minor,b.next_due_at,b.mandate_expires_at,b.merch_rn_id,b.first_rrn,b.first_int_ref
 FROM bcc_subscriptions b JOIN users u ON u.id=b.user_id WHERE b.environment=? AND b.status='active' AND b.next_due_at<=? AND b.next_due_at<b.mandate_expires_at AND u.status='active' AND u.deleted_at IS NULL
 AND NOT EXISTS(SELECT 1 FROM bcc_recurring_attempts a WHERE a.subscription_id=b.id AND a.period_start=b.next_due_at) ORDER BY b.next_due_at LIMIT 1`, s.Environment(), now.UnixMilli()).Scan(&subID, &user, &raw, &root, &amount, &next, &until, &rn, &rrn, &intRef)
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			if !bankReference(rrn) || !bankReference(intRef) || len(rn) != 16 {
				return ErrInvalidRequest
			}
			var p catalog.Product
			if err = json.Unmarshal([]byte(raw), &p); err != nil {
				return err
			}
			var version int64
			if err = tx.QueryRowContext(ctx, `SELECT catalog_version FROM billing_orders WHERE id=?`, root).Scan(&version); err != nil {
				return err
			}
			var nonce [16]byte
			if _, err = rand.Read(nonce[:]); err != nil {
				return err
			}
			orderID = traits.NewID()
			// ORDER must be decimal according to BCC, including suffix entropy.
			bankOrder := fmt.Sprintf("%d%06d", now.UnixMilli(), (int(nonce[0])<<16|int(nonce[1])<<8|int(nonce[2]))%1000000)
			money, _ := FormatKZT(amount)
			fields = url.Values{"TRTYPE": {"171"}, "MERCH_TRAN_STATE": {"M"}, "MERCH_RN_ID": {rn}, "RECUR_REF": {rrn}, "INT_REF": {intRef}, "ORDER": {bankOrder}, "AMOUNT": {money}, "CURRENCY": {CurrencyKZT}, "TERMINAL": {s.cfg.Terminal}, "TIMESTAMP": {now.Format("20060102150405")}, "MERCH_GMT": {"0"}, "LANG": {"ru"}, "DESC": {"AI Reply " + p.Code}, "NONCE": {strings.ToUpper(hex.EncodeToString(nonce[:]))}, "NOTIFY_URL": {s.baseURL() + "/api/v1/payments/bcc/notify"}}
			signature, err := SignRequest(RecurringCharge, fields, s.cfg.MACKeyHex)
			if err != nil {
				return err
			}
			fields.Set("P_SIGN", signature)
			request, _ := json.Marshal(fields)
			_, err = tx.ExecContext(ctx, `INSERT INTO billing_orders(id,user_id,idempotency_key,catalog_version,product_code,snapshot_json,amount_minor,currency,provider,environment,bank_order,status,checkout_token_hash,request_json,created_at,updated_at,bcc_subscription_id)
 VALUES(?,?,?,?,?,?,?,'KZT','bcc',?,?,'verification_pending','',?,?,?,?)`, orderID, user, "renewal:"+subID+":"+fmt.Sprint(next), version, p.Code, raw, amount, s.Environment(), bankOrder, string(request), now.UnixMilli(), now.UnixMilli(), subID)
			if err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO billing_order_context(order_id,locale,recurring_terms_version,recurring_until) SELECT ?,locale,recurring_terms_version,recurring_until FROM billing_order_context WHERE order_id=?`, orderID, root); err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO bcc_recurring_attempts(order_id,subscription_id,period_start,submitted_at,state) VALUES(?,?,?,?,'submitted')`, orderID, subID, next, now.UnixMilli())
			return err
		})
		if err != nil {
			return err
		}
		if orderID == "" {
			return nil
		}
		// The final authenticated notification, not this wrapper response, grants access.
		_, sendErr := s.client.post(ctx, RecurringCharge, fields)
		if sendErr != nil {
			// The charge may or may not have reached the bank: no blind retry, an
			// operator reconciles it (TRTYPE 90 within 24 h) — hence the alert.
			_, _ = s.db.Writer().ExecContext(context.WithoutCancel(ctx), `UPDATE bcc_recurring_attempts SET state='uncertain' WHERE order_id=? AND state='submitted'`, orderID)
			s.log.Error("BCC renewal charge unconfirmed", "order_id", orderID, "environment", string(s.Environment()), "error", sendErr.Error())
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return nil
}

func (s *CheckoutService) recordDecline(ctx context.Context, order BankOrder, fields map[string]string) error {
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		now := s.now().UnixMilli()
		result, err := tx.ExecContext(ctx, `UPDATE billing_orders SET status='failed',updated_at=? WHERE id=? AND status IN ('created','pending','verification_pending','needs_review')`, now, order.ID)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil || n == 0 {
			return err
		}
		raw, _ := json.Marshal(fields)
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO billing_order_events(id,order_id,source,state,fields_json,created_at) VALUES(?,?,'authenticated_notify','failed',?,?)`, "declined-"+order.ID, order.ID, string(raw), now); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE bcc_subscriptions SET status=CASE WHEN status='canceled' THEN status ELSE 'needs_review' END,next_due_at=NULL,updated_at=? WHERE id=(SELECT bcc_subscription_id FROM billing_orders WHERE id=?)`, now, order.ID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE bcc_recurring_attempts SET state='declined' WHERE order_id=?`, order.ID)
		return err
	})
}

func (s *CheckoutService) fulfillSubscriptionTx(ctx context.Context, tx *sql.Tx, order BankOrder, safe map[string]string, now time.Time) error {
	var id, status, root string
	var paid sql.NullInt64
	var days int
	if err := tx.QueryRowContext(ctx, `SELECT id,status,root_order_id,paid_until,period_days FROM bcc_subscriptions WHERE id=(SELECT bcc_subscription_id FROM billing_orders WHERE id=?) AND environment=?`, order.ID, s.Environment()).Scan(&id, &status, &root, &paid, &days); err != nil {
		return err
	}
	start := now
	if paid.Valid && paid.Int64 > now.UnixMilli() {
		start = time.UnixMilli(paid.Int64)
	}
	end := start.Add(time.Duration(days) * 24 * time.Hour)
	if _, err := tx.ExecContext(ctx, `INSERT INTO bcc_subscription_periods(order_id,subscription_id,starts_at,expires_at) VALUES(?,?,?,?)`, order.ID, id, start.UnixMilli(), end.UnixMilli()); err != nil {
		return err
	}
	if status == "pending" {
		status = "active"
	}
	var next any
	if status == "active" {
		next = end.UnixMilli()
	}
	if _, err := tx.ExecContext(ctx, `UPDATE bcc_subscriptions SET status=?,paid_until=?,next_due_at=?,first_rrn=CASE WHEN root_order_id=? THEN ? ELSE first_rrn END,first_int_ref=CASE WHEN root_order_id=? THEN ? ELSE first_int_ref END,updated_at=? WHERE id=?`, status, end.UnixMilli(), next, order.ID, safe["RRN"], order.ID, safe["INT_REF"], now.UnixMilli(), id); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE bcc_recurring_attempts SET state='confirmed' WHERE order_id=?`, order.ID)
	return err
}
