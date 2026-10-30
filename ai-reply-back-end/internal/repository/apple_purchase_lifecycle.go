package repository

import (
	"context"
	"database/sql"
	"time"
)

// advanceApplePurchaseLifecycle uses Apple's verified event time, never delivery
// time. It shares the transaction that applies the event to the purchase/grant.
// A same-time reversal cannot override a revocation; a newer signed reversal can.
func advanceApplePurchaseLifecycle(ctx context.Context, tx *sql.Tx, transactionID, state string, signedAt, now time.Time) (bool, error) {
	res, err := tx.ExecContext(ctx, `INSERT INTO apple_purchase_lifecycle(transaction_id,state,signed_at,updated_at)
		VALUES (?,?,?,?) ON CONFLICT(transaction_id) DO UPDATE SET
		state=excluded.state,signed_at=excluded.signed_at,updated_at=excluded.updated_at
		WHERE excluded.signed_at>apple_purchase_lifecycle.signed_at
		   OR (excluded.signed_at=apple_purchase_lifecycle.signed_at AND apple_purchase_lifecycle.state='restored' AND excluded.state<>'restored')`,
		transactionID, state, ms(signedAt), ms(now))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
