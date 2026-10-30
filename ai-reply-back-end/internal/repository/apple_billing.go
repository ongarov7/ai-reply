package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// EnsureIAPAccountToken persists the legacy token before any signing-key rotation.
// Existing paid transactions retain their account binding; the token never changes afterwards.
func (s *Store) EnsureIAPAccountToken(ctx context.Context, userID, initial string) (string, error) {
	_, err := s.db.Writer().ExecContext(ctx, `INSERT INTO iap_account_tokens(user_id, token) VALUES (?,?) ON CONFLICT(user_id) DO NOTHING`, userID, initial)
	if err != nil {
		return "", err
	}
	var token string
	err = s.db.Reader().QueryRowContext(ctx, `SELECT token FROM iap_account_tokens WHERE user_id=?`, userID).Scan(&token)
	return token, err
}

func (s *Store) UserByIAPAccountToken(ctx context.Context, token string) (domain.User, error) {
	var id string
	err := s.db.Reader().QueryRowContext(ctx, `SELECT user_id FROM iap_account_tokens WHERE token=? COLLATE NOCASE`, token).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.User{}, err
	}
	return s.UserByID(ctx, id)
}

type AppleSubscription struct {
	UserID, OriginalTransactionID, TransactionID, ProductID, PlanID, Environment string
	PurchasedAt, ExpiresAt, SignedAt                                             time.Time
	RevokedAt                                                                    *time.Time
	AutoRenew                                                                    *bool
	PriceMilliunits                                                              int64
	Currency                                                                     string
	Restore                                                                      bool
	MayShorten                                                                   bool
}

// ApplyAppleSubscription records a verified period and its entitlement atomically.
// Old periods cannot replace newer renewals; replaying a signed transaction never creates credits.
func (s *Store) ApplyAppleSubscription(ctx context.Context, in AppleSubscription, now time.Time) (GrantResult, error) {
	var out GrantResult
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		var owner string
		err := tx.QueryRowContext(ctx, `SELECT user_id FROM apple_subscriptions WHERE original_transaction_id=?`, in.OriginalTransactionID).Scan(&owner)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && owner != in.UserID {
			return domain.ErrPurchaseOwnedElsewhere
		}
		status := domain.PurchaseGranted
		if in.RevokedAt != nil {
			status = domain.PurchaseRevoked
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO iap_purchases(id,user_id,provider,environment,store_product_id,product_code,transaction_id,original_transaction_id,quantity,units,status,purchased_at,revoked_at,created_at,updated_at)
   VALUES (?,?,?,?,?,COALESCE((SELECT code FROM iap_products WHERE apple_product_id=?),''),?,?,1,0,?,?,?,?,?) ON CONFLICT(provider,transaction_id) DO NOTHING`,
			traits.NewID(), in.UserID, domain.ProviderAppStore, in.Environment, in.ProductID, in.ProductID, in.TransactionID, in.OriginalTransactionID, status, ms(in.PurchasedAt), msPtr(in.RevokedAt), ms(now), ms(now))
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		out.Granted = n > 0
		existing, err := purchaseInTx(ctx, tx, domain.ProviderAppStore, in.TransactionID)
		if err != nil {
			return err
		}
		if existing.UserID != in.UserID {
			return domain.ErrPurchaseOwnedElsewhere
		}
		// Refund tombstones must not be undone by an older transaction posted by the app.
		if (existing.Status == domain.PurchaseRefunded || existing.Status == domain.PurchaseRevoked) && in.RevokedAt == nil && !in.Restore {
			return domain.PurchaseInvalid(domain.PurchaseReasonRevoked)
		}
		if in.RevokedAt != nil {
			if _, err = tx.ExecContext(ctx, `UPDATE iap_purchases SET status=?,revoked_at=?,updated_at=? WHERE provider=? AND transaction_id=?`, status, msPtr(in.RevokedAt), ms(now), domain.ProviderAppStore, in.TransactionID); err != nil {
				return err
			}
		}
		if in.Restore {
			if _, err = tx.ExecContext(ctx, `UPDATE iap_purchases SET status='granted',revoked_at=NULL,updated_at=? WHERE provider=? AND transaction_id=?`, ms(now), domain.ProviderAppStore, in.TransactionID); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, `UPDATE iap_purchases SET price_milliunits=?,currency=? WHERE provider=? AND transaction_id=? AND price_milliunits=0`, in.PriceMilliunits, in.Currency, domain.ProviderAppStore, in.TransactionID); err != nil {
			return err
		}
		autoRenew := true
		if in.AutoRenew != nil {
			autoRenew = *in.AutoRenew
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO apple_subscriptions(original_transaction_id,user_id,transaction_id,product_id,plan_id,environment,purchased_at,expires_at,signed_at,revoked_at,auto_renew,updated_at)
    VALUES (?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(original_transaction_id) DO UPDATE SET
    transaction_id=excluded.transaction_id,product_id=excluded.product_id,plan_id=excluded.plan_id,environment=excluded.environment,
    purchased_at=excluded.purchased_at,expires_at=CASE WHEN excluded.transaction_id=apple_subscriptions.transaction_id AND NOT ? THEN MAX(apple_subscriptions.expires_at,excluded.expires_at) ELSE excluded.expires_at END,signed_at=excluded.signed_at,revoked_at=excluded.revoked_at,
    auto_renew=CASE WHEN ? THEN excluded.auto_renew ELSE apple_subscriptions.auto_renew END,updated_at=excluded.updated_at
    WHERE excluded.purchased_at>=apple_subscriptions.purchased_at AND excluded.signed_at>=apple_subscriptions.signed_at
      AND (apple_subscriptions.revoked_at IS NULL OR excluded.revoked_at IS NOT NULL OR excluded.transaction_id<>apple_subscriptions.transaction_id OR ?)`,
			in.OriginalTransactionID, in.UserID, in.TransactionID, in.ProductID, in.PlanID, in.Environment, ms(in.PurchasedAt), ms(in.ExpiresAt), ms(in.SignedAt), msPtr(in.RevokedAt), autoRenew, ms(now), in.MayShorten || in.RevokedAt != nil, in.AutoRenew != nil, in.Restore)
		if err != nil {
			return err
		}
		out.Purchase, err = purchaseInTx(ctx, tx, domain.ProviderAppStore, in.TransactionID)
		return err
	})
	return out, err
}

func (s *Store) ActiveAppleSubscription(ctx context.Context, userID string, now time.Time) (domain.Subscription, error) {
	var sub domain.Subscription
	var purchased, expires int64
	var auto bool
	err := s.db.Reader().QueryRowContext(ctx, `SELECT a.original_transaction_id,a.plan_id,a.purchased_at,a.expires_at,a.auto_renew FROM apple_subscriptions a JOIN plans p ON p.id=a.plan_id
  WHERE a.user_id=? AND a.revoked_at IS NULL AND a.expires_at>? ORDER BY CASE p.entitlement_kind WHEN 'unlimited' THEN 0 ELSE 1 END,p.daily_message_limit DESC,a.expires_at DESC LIMIT 1`, userID, ms(now)).Scan(&sub.ID, &sub.PlanID, &purchased, &expires, &auto)
	if errors.Is(err, sql.ErrNoRows) {
		return sub, domain.ErrNotFound
	}
	if err != nil {
		return sub, err
	}
	end := timeFrom(expires)
	sub.AutoRenews = &auto
	sub.UserID, sub.Status, sub.Source, sub.StartedAt, sub.ExpiresAt = userID, domain.SubActive, domain.ProviderAppStore, timeFrom(purchased), &end
	return sub, nil
}

type PackageSpending struct {
	Count          int
	SpentMinor     int64
	AllPricesKnown bool
}

// PackageSpending is for contextual recommendations, not accounting reconciliation.
// Apple's signed price already includes quantity and uses milliunits (1 KZT = 1000).
func (s *Store) PackageSpending(ctx context.Context, userID string) (PackageSpending, error) {
	var out PackageSpending
	var unknown int
	var milli int64
	err := s.db.Reader().QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(p.price_milliunits),0),COALESCE(SUM(CASE WHEN p.currency<>'KZT' OR p.price_milliunits<=0 OR p.price_milliunits%10<>0 THEN 1 ELSE 0 END),0)
	 FROM iap_purchases p JOIN iap_products product ON product.code=p.product_code
	 WHERE p.user_id=? AND p.provider='app_store' AND p.environment='Production' AND p.status='granted' AND product.kind='pack'`, userID).Scan(&out.Count, &milli, &unknown)
	out.SpentMinor, out.AllPricesKnown = milli/10, unknown == 0
	return out, err
}
