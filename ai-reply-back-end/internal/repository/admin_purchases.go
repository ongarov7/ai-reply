package repository

import (
	"context"
	"time"
)

// PurchaseTotals counts completed production purchases, including packs and
// subscription renewals. Test checkouts and refunded purchases are excluded.
// These are transactions in the selected period, independently of current access.
type PurchaseTotals struct {
	Purchases int
	Buyers    int
}

func (s *Store) PurchaseTotals(ctx context.Context, from, to time.Time) (PurchaseTotals, error) {
	var out PurchaseTotals
	err := s.db.Reader().QueryRowContext(ctx, `
		WITH purchases AS (
			SELECT user_id FROM iap_purchases
			WHERE environment='Production' AND status='granted' AND purchased_at BETWEEN ? AND ?
			UNION ALL
			SELECT user_id FROM billing_orders
			WHERE environment='production' AND status='succeeded' AND created_at BETWEEN ? AND ?
		)
		SELECT COUNT(*), COUNT(DISTINCT user_id) FROM purchases`, ms(from), ms(to), ms(from), ms(to)).
		Scan(&out.Purchases, &out.Buyers)
	return out, err
}
