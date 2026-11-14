package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// GoogleSubscription — one verified read of a Play subscription.
type GoogleSubscription struct {
	// TokenHash — SHA-256 hex of the purchase token: also iap_purchases.transaction_id of its first order.
	TokenHash   string
	SealedToken string
	UserID      string
	ProductID   string
	ProductCode string
	PlanID      string
	Environment string
	OrderID     string
	State       string
	// LinkedTokenHash — the token this one replaces ("" — none).
	LinkedTokenHash string
	StartedAt       time.Time
	ExpiresAt       time.Time
	AutoRenew       bool
	Acknowledged    bool
	// NextCheckAt — when the worker reads it again (nil — never: the state is final).
	NextCheckAt *time.Time
}

// GoogleSubscriptionResult — what a read changed.
type GoogleSubscriptionResult struct {
	Purchase domain.IAPPurchase
	// Granted — this call recorded a paid order the server had not seen.
	Granted bool
	// Revoked — the latest paid order was refunded: the subscription gives nothing.
	Revoked bool
	// Superseded — a newer purchase replaced this token: it gives nothing any more.
	Superseded bool
	// Acknowledged — nothing is left to acknowledge.
	Acknowledged bool
}

type googleSubscriptionRow struct {
	userID, orderID, revokedOrderID, supersededBy string
	expiresAt                                     int64
	acknowledged, revoked                         bool
}

func googleSubscriptionInTx(ctx context.Context, tx *sql.Tx, tokenHash string) (googleSubscriptionRow, bool, error) {
	var row googleSubscriptionRow
	err := tx.QueryRowContext(ctx, `SELECT user_id,order_id,revoked_order_id,superseded_by,expires_at,acknowledged_at IS NOT NULL,revoked_at IS NOT NULL
		FROM google_subscriptions WHERE token_hash=?`, tokenHash).Scan(&row.userID, &row.orderID, &row.revokedOrderID, &row.supersededBy,
		&row.expiresAt, &row.acknowledged, &row.revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return row, false, nil
	}
	return row, err == nil, err
}

// ApplyGoogleSubscription — a read that grants access, atomically: the paid
// order, the subscription row and the token it replaces.
//
// A token is bound to one account. A token that a newer purchase replaced gives
// nothing, in either order of arrival. An order the store refunded stays
// refunded until a later order is paid.
func (s *Store) ApplyGoogleSubscription(ctx context.Context, in GoogleSubscription, now time.Time) (GoogleSubscriptionResult, error) {
	var out GoogleSubscriptionResult
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		row, exists, err := googleSubscriptionInTx(ctx, tx, in.TokenHash)
		if err != nil {
			return err
		}
		if exists && row.userID != in.UserID {
			return domain.ErrPurchaseOwnedElsewhere
		}
		replaced := exists && row.supersededBy != ""
		if !replaced {
			var newer string
			err := tx.QueryRowContext(ctx, `SELECT token_hash FROM google_subscriptions WHERE linked_token_hash=? AND token_hash<>? LIMIT 1`, in.TokenHash, in.TokenHash).Scan(&newer)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			replaced = err == nil
		}
		if replaced {
			out.Superseded = true
			return nil
		}

		var previous *time.Time
		if exists {
			t := timeFrom(row.expiresAt)
			previous = &t
		}
		if out.Purchase, out.Granted, out.Revoked, err = recordGoogleOrder(ctx, tx, in, previous, now); err != nil {
			return err
		}
		if out.Granted {
			if err := grantGoogleBaseMonthly(ctx, tx, in, &out.Purchase, now); err != nil {
				return err
			}
		}

		var acknowledged, nextCheck any
		if in.Acknowledged {
			acknowledged = ms(now)
		}
		if in.NextCheckAt != nil {
			nextCheck = ms(*in.NextCheckAt)
		}
		// A store refund of an earlier order ends once a later order has been paid.
		restored := exists && row.revoked && row.revokedOrderID != "" && in.OrderID != "" && in.OrderID != row.revokedOrderID && !out.Revoked
		if _, err := tx.ExecContext(ctx, `INSERT INTO google_subscriptions
			(token_hash,user_id,purchase_token_sealed,product_id,product_code,plan_id,environment,state,order_id,started_at,expires_at,auto_renew,linked_token_hash,granted_at,acknowledged_at,read_at,next_check_at,check_failures,updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,0,?)
			ON CONFLICT(token_hash) DO UPDATE SET
			 purchase_token_sealed=COALESCE(excluded.purchase_token_sealed,google_subscriptions.purchase_token_sealed),
			 product_id=excluded.product_id,product_code=excluded.product_code,plan_id=excluded.plan_id,environment=excluded.environment,
			 state=excluded.state,order_id=CASE WHEN excluded.order_id<>'' THEN excluded.order_id ELSE google_subscriptions.order_id END,
			 started_at=excluded.started_at,expires_at=excluded.expires_at,auto_renew=excluded.auto_renew,
			 linked_token_hash=CASE WHEN excluded.linked_token_hash<>'' THEN excluded.linked_token_hash ELSE google_subscriptions.linked_token_hash END,
			 acknowledged_at=COALESCE(google_subscriptions.acknowledged_at,excluded.acknowledged_at),
			 revoked_at=CASE WHEN ? THEN NULL ELSE google_subscriptions.revoked_at END,
			 revoke_reason=CASE WHEN ? THEN '' ELSE google_subscriptions.revoke_reason END,
			 revoked_order_id=CASE WHEN ? THEN '' ELSE google_subscriptions.revoked_order_id END,
			 read_at=excluded.read_at,next_check_at=excluded.next_check_at,check_failures=0,updated_at=excluded.updated_at
			WHERE excluded.read_at>=google_subscriptions.read_at`,
			in.TokenHash, in.UserID, nullText(in.SealedToken), in.ProductID, in.ProductCode, in.PlanID, in.Environment, in.State, in.OrderID,
			ms(in.StartedAt), ms(in.ExpiresAt), in.AutoRenew, in.LinkedTokenHash, ms(now), acknowledged, ms(now), nextCheck, ms(now),
			restored, restored, restored); err != nil {
			return err
		}
		if out.Revoked {
			if _, err := tx.ExecContext(ctx, `UPDATE google_subscriptions SET revoked_at=COALESCE(revoked_at,?),revoke_reason=CASE WHEN revoke_reason='' THEN 'refunded' ELSE revoke_reason END,
				revoked_order_id=? WHERE token_hash=?`, ms(now), in.OrderID, in.TokenHash); err != nil {
				return err
			}
		}
		if in.LinkedTokenHash != "" && in.LinkedTokenHash != in.TokenHash {
			if _, err := tx.ExecContext(ctx, `UPDATE google_subscriptions SET superseded_by=?,purchase_token_sealed=NULL,next_check_at=NULL,updated_at=?
				WHERE token_hash=? AND superseded_by=''`, in.TokenHash, ms(now), in.LinkedTokenHash); err != nil {
				return err
			}
		}
		return tx.QueryRowContext(ctx, `SELECT acknowledged_at IS NOT NULL OR ack_abandoned_at IS NOT NULL FROM google_subscriptions WHERE token_hash=?`, in.TokenHash).Scan(&out.Acknowledged)
	})
	return out, err
}

// recordGoogleOrder keeps one iap_purchases row per paid order. The first order
// of a token uses the token's hash as its transaction id; renewals append the
// order id. Refunded before the app posted it, the row has no owner: the token
// is refused unless a later order has been paid since.
func recordGoogleOrder(ctx context.Context, tx *sql.Tx, in GoogleSubscription, previous *time.Time, now time.Time) (p domain.IAPPurchase, granted, revoked bool, err error) {
	first, err := purchaseInTx(ctx, tx, domain.ProviderGooglePlay, in.TokenHash)
	if errors.Is(err, sql.ErrNoRows) {
		started := in.StartedAt
		if _, err = tx.ExecContext(ctx, `INSERT INTO iap_purchases (id,user_id,provider,environment,store_product_id,product_code,transaction_id,original_transaction_id,order_id,quantity,units,status,purchased_at,created_at,updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,1,0,?,?,?,?)`, traits.NewID(), in.UserID, domain.ProviderGooglePlay, in.Environment, in.ProductID, in.ProductCode,
			in.TokenHash, in.TokenHash, in.OrderID, domain.PurchaseGranted, ms(started), ms(now), ms(now)); err != nil {
			return p, false, false, err
		}
		p, err = purchaseInTx(ctx, tx, domain.ProviderGooglePlay, in.TokenHash)
		return p, true, false, err
	}
	if err != nil {
		return p, false, false, err
	}
	refunded := func(status string) bool { return status == domain.PurchaseRefunded || status == domain.PurchaseRevoked }
	sameOrder := in.OrderID == "" || first.OrderID == "" || first.OrderID == in.OrderID
	switch {
	case first.Status == domain.PurchaseRejected:
		return p, false, false, domain.PurchaseInvalid(domain.PurchaseReasonCanceled)
	case first.UserID == "":
		if !refunded(first.Status) {
			return p, false, false, domain.ErrPurchaseOwnedElsewhere
		}
		if sameOrder {
			return p, false, false, domain.PurchaseInvalid(domain.PurchaseReasonRevoked)
		}
		fallthrough
	case first.Status == domain.PurchasePending && first.UserID == in.UserID:
		if _, err = tx.ExecContext(ctx, `UPDATE iap_purchases SET user_id=?,status=?,environment=?,store_product_id=?,product_code=?,original_transaction_id=?,order_id=?,
			purchased_at=?,revoked_at=NULL,revoke_reason='',created_at=?,updated_at=? WHERE id=?`, in.UserID, domain.PurchaseGranted, in.Environment, in.ProductID, in.ProductCode,
			in.TokenHash, in.OrderID, ms(in.StartedAt), ms(now), ms(now), first.ID); err != nil {
			return p, false, false, err
		}
		p, err = purchaseInTx(ctx, tx, domain.ProviderGooglePlay, in.TokenHash)
		return p, true, false, err
	case first.UserID != in.UserID:
		return p, false, false, domain.ErrPurchaseOwnedElsewhere
	}
	if sameOrder {
		if first.OrderID == "" && in.OrderID != "" {
			if _, err = tx.ExecContext(ctx, `UPDATE iap_purchases SET order_id=?,updated_at=? WHERE id=?`, in.OrderID, ms(now), first.ID); err != nil {
				return p, false, false, err
			}
		}
		p, err = purchaseInTx(ctx, tx, domain.ProviderGooglePlay, in.TokenHash)
		return p, false, err == nil && refunded(p.Status), err
	}
	renewal := googleOrderTransaction(in.TokenHash, in.OrderID)
	paid := in.StartedAt
	if previous != nil {
		paid = *previous
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO iap_purchases (id,user_id,provider,environment,store_product_id,product_code,transaction_id,original_transaction_id,order_id,quantity,units,status,purchased_at,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,1,0,?,?,?,?) ON CONFLICT(provider,transaction_id) DO NOTHING`, traits.NewID(), in.UserID, domain.ProviderGooglePlay, in.Environment,
		in.ProductID, in.ProductCode, renewal, in.TokenHash, in.OrderID, domain.PurchaseGranted, ms(paid), ms(now), ms(now))
	if err != nil {
		return p, false, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return p, false, false, err
	}
	p, err = purchaseInTx(ctx, tx, domain.ProviderGooglePlay, renewal)
	if err != nil {
		return p, false, false, err
	}
	return p, n > 0, refunded(p.Status), nil
}

func googleOrderTransaction(tokenHash, orderID string) string { return tokenHash + ":" + orderID }

// GoogleSubscriptionState — a read that grants nothing: the state of a known token moves on.
type GoogleSubscriptionState struct {
	TokenHash   string
	State       string
	ExpiresAt   time.Time
	AutoRenew   bool
	NextCheckAt *time.Time
	// DropToken — the state is final: the sealed token is no longer needed.
	DropToken bool
}

// UpdateGoogleSubscriptionState — on hold, paused, expired. Unknown tokens change nothing.
func (s *Store) UpdateGoogleSubscriptionState(ctx context.Context, in GoogleSubscriptionState, now time.Time) error {
	var nextCheck any
	if in.NextCheckAt != nil {
		nextCheck = ms(*in.NextCheckAt)
	}
	_, err := s.db.Writer().ExecContext(ctx, `UPDATE google_subscriptions SET state=?,expires_at=?,auto_renew=?,next_check_at=?,check_failures=0,read_at=?,updated_at=?,
		purchase_token_sealed=CASE WHEN ? THEN NULL ELSE purchase_token_sealed END
		WHERE token_hash=? AND read_at<=?`, in.State, ms(in.ExpiresAt), in.AutoRenew, nextCheck, ms(now), ms(now), in.DropToken, in.TokenHash, ms(now))
	return err
}

// GoogleDue — a subscription the worker should read now.
type GoogleDue struct {
	TokenHash   string
	UserID      string
	ProductID   string
	ProductCode string
	PlanID      string
	Environment string
	SealedToken string
	State       string
	Failures    int
	StartedAt   time.Time
	ExpiresAt   time.Time
}

// DueGoogleSubscriptions — the longest waiting reads first, at most limit.
func (s *Store) DueGoogleSubscriptions(ctx context.Context, now time.Time, limit int) ([]GoogleDue, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `SELECT token_hash,user_id,product_id,product_code,plan_id,environment,purchase_token_sealed,state,check_failures,started_at,expires_at
		FROM google_subscriptions WHERE next_check_at IS NOT NULL AND next_check_at<=? AND purchase_token_sealed IS NOT NULL AND superseded_by=''
		ORDER BY next_check_at LIMIT ?`, ms(now), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GoogleDue
	for rows.Next() {
		var d GoogleDue
		var started, expires int64
		if err := rows.Scan(&d.TokenHash, &d.UserID, &d.ProductID, &d.ProductCode, &d.PlanID, &d.Environment, &d.SealedToken, &d.State, &d.Failures, &started, &expires); err != nil {
			return nil, err
		}
		d.StartedAt, d.ExpiresAt = timeFrom(started), timeFrom(expires)
		out = append(out, d)
	}
	return out, rows.Err()
}

// DeferGoogleSubscription — the read failed: try again at next, nothing else changes.
func (s *Store) DeferGoogleSubscription(ctx context.Context, tokenHash string, failures int, next, now time.Time) error {
	_, err := s.db.Writer().ExecContext(ctx, `UPDATE google_subscriptions SET check_failures=?,next_check_at=?,updated_at=? WHERE token_hash=?`,
		failures, ms(next), ms(now), tokenHash)
	return err
}

// GoogleUnacknowledged — a granted purchase Google has not been told about.
type GoogleUnacknowledged struct {
	TokenHash   string
	ProductID   string
	SealedToken string
	GrantedAt   time.Time
	Environment string
}

// UnacknowledgedGoogleSubscriptions — granted before the cutoff and still waiting, oldest first.
func (s *Store) UnacknowledgedGoogleSubscriptions(ctx context.Context, before time.Time, limit int) ([]GoogleUnacknowledged, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `SELECT token_hash,product_id,purchase_token_sealed,granted_at,environment FROM google_subscriptions
		WHERE acknowledged_at IS NULL AND ack_abandoned_at IS NULL AND granted_at<? AND purchase_token_sealed IS NOT NULL AND superseded_by=''
		ORDER BY granted_at LIMIT ?`, ms(before), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GoogleUnacknowledged
	for rows.Next() {
		var u GoogleUnacknowledged
		var granted int64
		if err := rows.Scan(&u.TokenHash, &u.ProductID, &u.SealedToken, &granted, &u.Environment); err != nil {
			return nil, err
		}
		u.GrantedAt = timeFrom(granted)
		out = append(out, u)
	}
	return out, rows.Err()
}

// MarkGoogleSubscriptionAcknowledged — Google knows the purchase was granted.
func (s *Store) MarkGoogleSubscriptionAcknowledged(ctx context.Context, tokenHash string, now time.Time) error {
	_, err := s.db.Writer().ExecContext(ctx, `UPDATE google_subscriptions SET acknowledged_at=COALESCE(acknowledged_at,?),updated_at=? WHERE token_hash=?`,
		ms(now), ms(now), tokenHash)
	return err
}

// AbandonGoogleAcknowledge — past Google's window: stop retrying, the subscription keeps being read.
func (s *Store) AbandonGoogleAcknowledge(ctx context.Context, tokenHash string, now time.Time) error {
	_, err := s.db.Writer().ExecContext(ctx, `UPDATE google_subscriptions SET ack_abandoned_at=COALESCE(ack_abandoned_at,?),updated_at=? WHERE token_hash=?`,
		ms(now), ms(now), tokenHash)
	return err
}

// GoogleVoid — one voided order from the Play voided-purchases list.
type GoogleVoid struct {
	TokenHash string
	OrderID   string
	Reason    string
	At, Now   time.Time
}

// GoogleVoidResult — what the voided order changed.
type GoogleVoidResult struct {
	// Found — the token is one of this server's subscriptions.
	Found bool
	// Revoked — the latest paid order was voided: access is gone now.
	Revoked bool
	UserID  string
}

// VoidGoogleSubscription — a refund of one order. Renewal orders share a token,
// so only the latest paid order takes the running period away; an earlier order
// is recorded and the period someone paid for since stays. The result is the
// same however many times the voided list repeats the order.
func (s *Store) VoidGoogleSubscription(ctx context.Context, in GoogleVoid) (GoogleVoidResult, error) {
	var out GoogleVoidResult
	at := in.At
	if at.IsZero() {
		at = in.Now
	}
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		row, exists, err := googleSubscriptionInTx(ctx, tx, in.TokenHash)
		if err != nil || !exists {
			return err
		}
		out.Found, out.UserID = true, row.userID
		fresh, err := refundGoogleOrder(ctx, tx, row, in, at)
		if err != nil {
			return err
		}
		latest := in.OrderID == "" || row.orderID == "" || in.OrderID == row.orderID
		if !latest || row.revoked && row.revokedOrderID == in.OrderID {
			if !fresh {
				return nil
			}
			// A charge-back of an earlier order can end the whole subscription, and
			// this list is the only sign of it between reads: Google's state decides.
			_, err = tx.ExecContext(ctx, `UPDATE google_subscriptions SET next_check_at=CASE WHEN next_check_at IS NULL THEN NULL ELSE ? END,updated_at=? WHERE token_hash=?`,
				ms(in.Now), ms(in.Now), in.TokenHash)
			return err
		}
		out.Revoked = true
		soon := ms(in.Now)
		_, err = tx.ExecContext(ctx, `UPDATE google_subscriptions SET revoked_at=?,revoke_reason=?,revoked_order_id=?,
			next_check_at=CASE WHEN next_check_at IS NULL THEN NULL ELSE ? END,updated_at=? WHERE token_hash=?`,
			ms(at), in.Reason, in.OrderID, soon, ms(in.Now), in.TokenHash)
		return err
	})
	return out, err
}

// refundGoogleOrder marks the order's purchase row and takes back what is left
// of that order's bundle. A renewal this server has not read yet gets its
// refunded row now, so a later read cannot grant it. fresh is false when the
// voided list only repeats an order already refunded.
func refundGoogleOrder(ctx context.Context, tx *sql.Tx, row googleSubscriptionRow, in GoogleVoid, at time.Time) (fresh bool, err error) {
	first, err := purchaseInTx(ctx, tx, domain.ProviderGooglePlay, in.TokenHash)
	if err != nil {
		return false, err
	}
	purchaseID := first.ID
	var res sql.Result
	if in.OrderID == "" || first.OrderID == "" || first.OrderID == in.OrderID {
		res, err = tx.ExecContext(ctx, `UPDATE iap_purchases SET status=?,revoked_at=?,revoke_reason=?,updated_at=? WHERE id=? AND status<>?`,
			domain.PurchaseRefunded, ms(at), in.Reason, ms(in.Now), first.ID, domain.PurchaseRefunded)
		if err != nil {
			return false, err
		}
	} else {
		transaction := googleOrderTransaction(in.TokenHash, in.OrderID)
		res, err = tx.ExecContext(ctx, `INSERT INTO iap_purchases (id,user_id,provider,environment,store_product_id,product_code,transaction_id,original_transaction_id,order_id,quantity,units,status,revoked_at,revoke_reason,created_at,updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,1,0,?,?,?,?,?)
			ON CONFLICT(provider,transaction_id) DO UPDATE SET status=excluded.status,revoked_at=excluded.revoked_at,revoke_reason=excluded.revoke_reason,updated_at=excluded.updated_at
			WHERE iap_purchases.status<>excluded.status`,
			traits.NewID(), row.userID, domain.ProviderGooglePlay, first.Environment, first.StoreProductID, first.ProductCode, transaction,
			in.TokenHash, in.OrderID, domain.PurchaseRefunded, ms(at), in.Reason, ms(in.Now), ms(in.Now))
		if err != nil {
			return false, err
		}
		renewal, err := purchaseInTx(ctx, tx, domain.ProviderGooglePlay, transaction)
		if err != nil {
			return false, err
		}
		purchaseID = renewal.ID
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, revokeGoogleBaseMonthly(ctx, tx, purchaseID, in.Reason, in.Now)
}

// googleRenewalBridge — how long a renewing subscription keeps its access past
// an end no read has looked at yet. Google moves the end when it charges, and
// the first read after the old end decides; this only bounds an outage of
// Google or of the worker.
const googleRenewalBridge = 48 * time.Hour

// GoogleSubscriptionHeldBy — this token already belongs to this account.
func (s *Store) GoogleSubscriptionHeldBy(ctx context.Context, tokenHash, userID string) (bool, error) {
	var held bool
	err := s.db.Reader().QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM google_subscriptions WHERE token_hash=? AND user_id=?)`, tokenHash, userID).Scan(&held)
	return held, err
}

// ActiveGoogleSubscription — the strongest Play subscription that gives access now.
func (s *Store) ActiveGoogleSubscription(ctx context.Context, userID string, now time.Time) (domain.Subscription, error) {
	var sub domain.Subscription
	var started, expires int64
	var auto bool
	err := s.db.Reader().QueryRowContext(ctx, `SELECT g.token_hash,g.plan_id,g.started_at,g.expires_at,g.auto_renew
		FROM google_subscriptions g JOIN plans p ON p.id=g.plan_id
		WHERE g.user_id=? AND g.revoked_at IS NULL AND g.superseded_by='' AND g.state IN (?,?,?)
		 AND (g.expires_at>? OR (g.auto_renew=1 AND g.state<>? AND g.read_at<g.expires_at AND g.expires_at>?))
		ORDER BY p.entitlement_kind='unlimited' DESC,p.daily_message_limit DESC,g.expires_at DESC LIMIT 1`,
		userID, domain.GoogleSubActive, domain.GoogleSubGrace, domain.GoogleSubCanceled, ms(now), domain.GoogleSubCanceled, ms(now.Add(-googleRenewalBridge))).
		Scan(&sub.ID, &sub.PlanID, &started, &expires, &auto)
	if errors.Is(err, sql.ErrNoRows) {
		return sub, domain.ErrNotFound
	}
	if err != nil {
		return sub, err
	}
	end := timeFrom(expires)
	sub.AutoRenews = &auto
	sub.UserID, sub.Status, sub.Source, sub.StartedAt, sub.ExpiresAt = userID, domain.SubActive, domain.ProviderGooglePlay, timeFrom(started), &end
	return sub, nil
}
