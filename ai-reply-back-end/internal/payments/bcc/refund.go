package bcc

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// Refund requests one full sandbox refund. An uncertain transport result is
// recorded for reconciliation and is never retried as another money operation.
func (s *CheckoutService) Refund(ctx context.Context, adminID, id string) (BankOrder, error) {
	if !s.Enabled() {
		return BankOrder{}, domain.ErrPurchasesDisabled
	}
	if adminID == "" {
		return BankOrder{}, domain.ErrUnauthorized
	}
	var order BankOrder
	var fields url.Values
	var send bool
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		order, err = readOrder(tx.QueryRowContext(ctx, `SELECT `+orderColumns+` FROM billing_orders WHERE id=? AND environment='sandbox'`, id))
		if err != nil {
			return err
		}
		if order.Status == "refunded" {
			return nil
		}
		if order.Status != "succeeded" {
			return domain.ConflictField("payment", "not_paid")
		}
		var existing string
		err = tx.QueryRowContext(ctx, `SELECT status FROM billing_bank_operations WHERE order_id=? AND kind='refund'`, id).Scan(&existing)
		if err == nil {
			order.RefundStatus = existing
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var raw string
		if err = tx.QueryRowContext(ctx, `SELECT fields_json FROM billing_order_events WHERE id=?`, "fulfilled-"+id).Scan(&raw); err != nil {
			return err
		}
		var original map[string]string
		if err = json.Unmarshal([]byte(raw), &original); err != nil {
			return err
		}
		if !bankReference(original["RRN"]) || !bankReference(original["INT_REF"]) {
			return ErrInvalidRequest
		}
		var nonce [16]byte
		if _, err = rand.Read(nonce[:]); err != nil {
			return err
		}
		amount, _ := FormatKZT(order.AmountMinor)
		now := s.now().UTC()
		var originalRequest url.Values
		if err = json.Unmarshal([]byte(order.request), &originalRequest); err != nil {
			return err
		}
		rnID := originalRequest.Get("MERCH_RN_ID")
		if len(rnID) != 16 {
			return ErrInvalidRequest
		}
		fields = url.Values{"ORDER": {order.bankOrder}, "MERCH_RN_ID": {rnID}, "MERCH_GMT": {"0"}, "LANG": {"ru"}, "ORG_AMOUNT": {amount}, "AMOUNT": {amount}, "CURRENCY": {CurrencyKZT}, "RRN": {original["RRN"]}, "INT_REF": {original["INT_REF"]}, "TERMINAL": {s.cfg.Terminal}, "TIMESTAMP": {now.Format("20060102150405")}, "TRTYPE": {"14"}, "NONCE": {strings.ToUpper(hex.EncodeToString(nonce[:]))}, "NOTIFY_URL": {s.baseURL() + "/api/v1/payments/bcc/notify"}}
		signature, err := SignRequest(Refund, fields, s.cfg.MACKeyHex)
		if err != nil {
			return err
		}
		fields.Set("P_SIGN", signature)
		request, _ := json.Marshal(fields)
		if _, err = tx.ExecContext(ctx, `INSERT INTO billing_bank_operations(id,order_id,kind,status,request_json,created_by,created_at,updated_at) VALUES(?,?,'refund','pending',?,?,?,?)`, traits.NewID(), id, string(request), adminID, now.UnixMilli(), now.UnixMilli()); err != nil {
			return err
		}
		send = true
		return nil
	})
	if err != nil {
		return BankOrder{}, err
	}
	if !send {
		return order, nil
	}
	order.RefundStatus = "pending"
	response, requestErr := s.client.post(ctx, Refund, fields)
	if requestErr != nil {
		return order, nil
	} // persisted pending; inspect instead of retrying
	if response.Action == "0" && response.RC == "00" && (response.TRType == "" || response.TRType == "14") && (response.ReportedOriginalType == "" || response.ReportedOriginalType == "14") && response.Order == order.bankOrder && response.Terminal == s.cfg.Terminal && response.Amount == fields.Get("AMOUNT") && response.Currency == CurrencyKZT && bankReference(response.RRN) && bankReference(response.IntRef) {
		safe := map[string]string{"ORDER": response.Order, "TERMINAL": response.Terminal, "AMOUNT": response.Amount, "CURRENCY": response.Currency, "RRN": response.RRN, "INT_REF": response.IntRef, "ACTION": response.Action, "RC": response.RC, "TRTYPE": "14"}
		if err = s.refundAuthenticatedPack(ctx, order, safe); err != nil {
			return BankOrder{}, err
		}
		order.Status = "refunded"
		order.RefundStatus = "succeeded"
	}
	return order, nil
}
func (s *CheckoutService) refundRequest(ctx context.Context, id string) (url.Values, error) {
	var raw string
	err := s.db.Reader().QueryRowContext(ctx, `SELECT request_json FROM billing_bank_operations WHERE order_id=? AND kind='refund'`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var fields url.Values
	err = json.Unmarshal([]byte(raw), &fields)
	return fields, err
}
func (s *CheckoutService) refundAuthenticatedPack(ctx context.Context, order BankOrder, safe map[string]string) error {
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		var status string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM billing_orders WHERE id=? AND environment='sandbox'`, order.ID).Scan(&status); err != nil {
			return err
		}
		if status == "refunded" {
			return nil
		}
		if status != "succeeded" {
			return domain.ConflictField("payment", "not_paid")
		}
		var operation string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM billing_bank_operations WHERE order_id=? AND kind='refund'`, order.ID).Scan(&operation); err != nil {
			return err
		}
		now := s.now().UnixMilli()
		var grant, user string
		var remaining int
		err := tx.QueryRowContext(ctx, `SELECT id,user_id,units_remaining FROM credit_grants WHERE billing_order_id=?`, order.ID).Scan(&grant, &user, &remaining)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			if _, err = tx.ExecContext(ctx, `UPDATE credit_grants SET status='revoked',units_remaining=0,revoked_at=?,revoked_by='system',revoke_reason='BCC full refund',updated_at=? WHERE id=?`, now, now, grant); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO credit_ledger(id,user_id,grant_id,entry,kind,delta,actor,reason,created_at) VALUES(?,?,?,'revoke','pack',?,'system','BCC full refund',?)`, traits.NewID(), user, grant, -remaining, now); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, `UPDATE billing_orders SET status='refunded',updated_at=? WHERE id=?`, now, order.ID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE billing_bank_operations SET status='succeeded',updated_at=? WHERE id=?`, now, operation); err != nil {
			return err
		}
		raw, _ := json.Marshal(safe)
		_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO billing_order_events(id,order_id,source,state,fields_json,created_at) VALUES(?,?,'authenticated_refund','refunded',?,?)`, "refunded-"+order.ID, order.ID, string(raw), now)
		return err
	})
}

// PendingRefunds are exposed only to administrators for an uncertain refund.
func (s *CheckoutService) Orders(ctx context.Context) ([]BankOrder, error) {
	if !s.Enabled() {
		return []BankOrder{}, nil
	}
	rows, err := s.db.Reader().QueryContext(ctx, `SELECT `+orderColumns+` FROM billing_orders WHERE environment='sandbox' ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BankOrder{}
	for rows.Next() {
		o, err := readOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
