package bcc

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/catalog"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/email"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

type ReceiptSender interface {
	Send(context.Context, email.Outgoing) error
}

func (s *CheckoutService) WithReceiptEmail(sender ReceiptSender, templates email.NotificationTemplates) *CheckoutService {
	s.receiptSender, s.receiptTemplates = sender, templates
	return s
}

func (s *CheckoutService) enqueueReceiptTx(ctx context.Context, tx *sql.Tx, order BankOrder, product catalog.Product, user string, now int64) error {
	locale := "en"
	err := tx.QueryRowContext(ctx, `SELECT locale FROM billing_order_context WHERE order_id=?`, order.ID).Scan(&locale)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	locale = domain.NormalizeLocale(locale)
	name := product.Name[locale]
	if name == "" {
		name = product.Name["en"]
	}
	params := map[string]string{"product": name, "amount": traits.FormatMoney(order.AmountMinor, order.Currency), "units": fmt.Sprint(product.Units), "order": order.ID, "manage_url": s.baseURL() + "/account?lang=" + locale}
	kind := "bcc_pack_receipt"
	if product.Kind == "subscription" {
		kind = "bcc_subscription_receipt"
		var end int64
		if err = tx.QueryRowContext(ctx, `SELECT expires_at FROM bcc_subscription_periods WHERE order_id=?`, order.ID).Scan(&end); err != nil {
			return err
		}
		params["date"] = time.UnixMilli(end).UTC().Format("2006-01-02 15:04 UTC")
		params["days"] = fmt.Sprint(periodDays(product))
		var status string
		if err = tx.QueryRowContext(ctx, `SELECT status FROM bcc_subscriptions WHERE id=(SELECT bcc_subscription_id FROM billing_orders WHERE id=?)`, order.ID).Scan(&status); err != nil {
			return err
		}
		if status == "active" {
			kind = "bcc_renewing_receipt"
		}
	}
	raw, _ := json.Marshal(params)
	_, err = tx.ExecContext(ctx, `INSERT INTO billing_receipt_outbox(order_id,user_id,locale,params_json,kind,created_at,next_attempt_at) VALUES(?,?,?,?,?,?,?)`, order.ID, user, locale, string(raw), kind, now, now)
	return err
}

// Run owns the persistent scheduler and receipt queue. The database, rather
// than an in-memory delay, determines which cycle can be submitted after restart.
func (s *CheckoutService) Run(ctx context.Context, log *slog.Logger) {
	if !s.Enabled() {
		<-ctx.Done()
		return
	}
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		if err := s.RunDue(ctx); err != nil && ctx.Err() == nil {
			log.Error("BCC renewal scheduler failed")
		}
		if err := s.SendReceipts(ctx); err != nil && ctx.Err() == nil {
			log.Error("BCC receipt queue failed")
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// SendReceipts freezes each outgoing message on its first attempt. Retries use
// the identical payload and provider idempotency key, within Resend's 24h window.
func (s *CheckoutService) SendReceipts(ctx context.Context) error {
	if !s.Enabled() || s.Environment() != Production || s.receiptSender == nil {
		return nil
	}
	for i := 0; i < 20; i++ {
		var id, claim string
		var msg email.Outgoing
		err := s.db.Tx(ctx, func(tx *sql.Tx) error {
			now := s.now().UnixMilli()
			// Past the provider's deduplication window, require operator review.
			if _, err := tx.ExecContext(ctx, `UPDATE billing_receipt_outbox SET status='needs_review' WHERE status IN ('pending','sending') AND (attempts>=8 OR first_attempt_at<=?) AND lease_until<=?`, now-int64(23*time.Hour/time.Millisecond), now); err != nil {
				return err
			}
			var raw, locale, kind, to, delivery string
			err := tx.QueryRowContext(ctx, `SELECT o.order_id,o.params_json,o.locale,o.kind,u.email,o.delivery_json
 FROM billing_receipt_outbox o JOIN billing_orders b ON b.id=o.order_id JOIN users u ON u.id=o.user_id
 WHERE b.environment=? AND u.status='active' AND u.deleted_at IS NULL AND u.email_verified_at IS NOT NULL
 AND o.status IN ('pending','sending') AND o.next_attempt_at<=? AND o.lease_until<=? ORDER BY o.created_at LIMIT 1`, s.Environment(), now, now).Scan(&id, &raw, &locale, &kind, &to, &delivery)
			if errors.Is(err, sql.ErrNoRows) {
				id = ""
				return nil
			}
			if err != nil {
				return err
			}
			if delivery == "" {
				var params map[string]string
				if err = json.Unmarshal([]byte(raw), &params); err != nil {
					return err
				}
				content, err := s.receiptTemplates.Render(kind, locale, params)
				if err != nil {
					return err
				}
				msg = email.Outgoing{To: to, Content: content, IdempotencyKey: "bcc-receipt-" + id, Reference: "bcc-receipt-" + id}
				encoded, err := json.Marshal(msg)
				if err != nil {
					return err
				}
				delivery = string(encoded)
			} else if err = json.Unmarshal([]byte(delivery), &msg); err != nil {
				return err
			}
			claim = traits.NewID()
			_, err = tx.ExecContext(ctx, `UPDATE billing_receipt_outbox SET status='sending',attempts=attempts+1,first_attempt_at=coalesce(first_attempt_at,?),lease_until=?,claim_token=?,delivery_json=? WHERE order_id=?`, now, now+int64(2*time.Minute/time.Millisecond), claim, delivery, id)
			return err
		})
		if err != nil || id == "" {
			return err
		}
		sendErr := s.receiptSender.Send(ctx, msg)
		now := s.now().UnixMilli()
		if sendErr == nil {
			_, err = s.db.Writer().ExecContext(ctx, `UPDATE billing_receipt_outbox SET status='sent',sent_at=?,lease_until=0 WHERE order_id=? AND claim_token=?`, now, id, claim)
		} else {
			status := "pending"
			var deliveryError *email.DeliveryError
			if errors.As(sendErr, &deliveryError) && deliveryError.Status >= 400 && deliveryError.Status < 500 && deliveryError.Status != 429 {
				status = "needs_review"
			}
			_, err = s.db.Writer().ExecContext(ctx, `UPDATE billing_receipt_outbox SET status=?,lease_until=0,next_attempt_at=?+min(3600000,60000*(1 << min(attempts,6))) WHERE order_id=? AND claim_token=?`, status, now, id, claim)
		}
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return nil
}
