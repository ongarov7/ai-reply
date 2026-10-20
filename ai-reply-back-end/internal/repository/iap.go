package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// ---------------------------------------------------------------- products

const iapProductColumns = `id, code, kind, units, duration_hours, COALESCE(apple_product_id, ''),
	COALESCE(google_product_id, ''), name_kk, name_ru, name_en, name_uz, is_active, sort_order,
	created_at, updated_at, archived_at`

// IAPProducts — әкімші баптаған өнімдер (sort_order, code бойынша). archived — мұрағаттағылар да.
func (s *Store) IAPProducts(ctx context.Context, archived bool) ([]domain.IAPProduct, error) {
	where := ""
	if !archived {
		where = " WHERE archived_at IS NULL"
	}
	rows, err := s.db.Reader().QueryContext(ctx, `SELECT `+iapProductColumns+` FROM iap_products`+where+`
		ORDER BY archived_at IS NOT NULL, sort_order, code`)
	if err != nil {
		return nil, err
	}
	return scanIAPProducts(rows)
}

// IAPProduct — бір өнім.
func (s *Store) IAPProduct(ctx context.Context, id string) (domain.IAPProduct, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `SELECT `+iapProductColumns+` FROM iap_products WHERE id = ?`, id)
	if err != nil {
		return domain.IAPProduct{}, err
	}
	return oneIAPProduct(rows)
}

// IAPProductByStoreID — дүкендегі идентификатор бойынша өнім (өшірілгені мен мұрағаттағысы да).
//
// A purchase that was paid for maps to what it bought even after the product
// was switched off or archived.
func (s *Store) IAPProductByStoreID(ctx context.Context, provider, storeID string) (domain.IAPProduct, error) {
	column := "apple_product_id"
	if provider == domain.ProviderGooglePlay {
		column = "google_product_id"
	}
	rows, err := s.db.Reader().QueryContext(ctx, `SELECT `+iapProductColumns+` FROM iap_products WHERE `+column+` = ?`, storeID)
	if err != nil {
		return domain.IAPProduct{}, err
	}
	return oneIAPProduct(rows)
}

// CreateIAPProduct — жаңа өнім. Бос емес code / дүкен id-і бос емес болса бірегей (ConflictField).
func (s *Store) CreateIAPProduct(ctx context.Context, p domain.IAPProduct, now time.Time) (domain.IAPProduct, error) {
	p.ID = traits.NewID()
	p.CreatedAt, p.UpdatedAt = now, now
	_, err := s.db.Writer().ExecContext(ctx, `
		INSERT INTO iap_products (id, code, kind, units, duration_hours, apple_product_id, google_product_id,
		                          name_kk, name_ru, name_en, name_uz, is_active, sort_order, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.Code, p.Kind, p.Units, p.DurationHours, nullText(p.AppleProductID), nullText(p.GoogleProductID),
		p.Name["kk"], p.Name["ru"], p.Name["en"], p.Name["uz"], p.IsActive, p.SortOrder, ms(now), ms(now))
	if err != nil {
		return domain.IAPProduct{}, productConflict(err)
	}
	return p, nil
}

// UpdateIAPProduct — өнімді өзгерту (мұрағаттағысы өзгермейді: ErrNotFound).
func (s *Store) UpdateIAPProduct(ctx context.Context, p domain.IAPProduct, now time.Time) error {
	res, err := s.db.Writer().ExecContext(ctx, `
		UPDATE iap_products SET units = ?, duration_hours = ?, apple_product_id = ?, google_product_id = ?,
		       name_kk = ?, name_ru = ?, name_en = ?, name_uz = ?, is_active = ?, sort_order = ?, updated_at = ?
		WHERE id = ? AND archived_at IS NULL`,
		p.Units, p.DurationHours, nullText(p.AppleProductID), nullText(p.GoogleProductID),
		p.Name["kk"], p.Name["ru"], p.Name["en"], p.Name["uz"], p.IsActive, p.SortOrder, ms(now), p.ID)
	if err != nil {
		return productConflict(err)
	}
	return affected(res, nil)
}

// ArchiveIAPProduct — өнімді мұрағаттау: сатылмайды, бұрынғы сатып алулар өз өнімін табады.
func (s *Store) ArchiveIAPProduct(ctx context.Context, id string, now time.Time) error {
	res, err := s.db.Writer().ExecContext(ctx, `
		UPDATE iap_products SET is_active = 0, archived_at = ?, updated_at = ? WHERE id = ? AND archived_at IS NULL`,
		ms(now), ms(now), id)
	return affected(res, err)
}

// productConflict — бірегейлік қатесі қай өріске тиесілі.
func productConflict(err error) error {
	if !isUnique(err) {
		return err
	}
	msg := err.Error()
	for _, field := range []string{"apple_product_id", "google_product_id", "code"} {
		if strings.Contains(msg, "iap_products."+field) {
			return domain.ConflictField(field, "taken")
		}
	}
	return domain.ErrConflict
}

func oneIAPProduct(rows *sql.Rows) (domain.IAPProduct, error) {
	list, err := scanIAPProducts(rows)
	if err != nil {
		return domain.IAPProduct{}, err
	}
	if len(list) == 0 {
		return domain.IAPProduct{}, domain.ErrNotFound
	}
	return list[0], nil
}

func scanIAPProducts(rows *sql.Rows) ([]domain.IAPProduct, error) {
	defer rows.Close()
	var out []domain.IAPProduct
	for rows.Next() {
		var (
			p                domain.IAPProduct
			kk, ru, en, uz   string
			active           bool
			created, updated int64
			archived         sql.NullInt64
		)
		if err := rows.Scan(&p.ID, &p.Code, &p.Kind, &p.Units, &p.DurationHours, &p.AppleProductID,
			&p.GoogleProductID, &kk, &ru, &en, &uz, &active, &p.SortOrder, &created, &updated, &archived); err != nil {
			return nil, err
		}
		p.Name = map[string]string{"kk": kk, "ru": ru, "en": en, "uz": uz}
		p.IsActive = active
		p.CreatedAt, p.UpdatedAt, p.ArchivedAt = timeFrom(created), timeFrom(updated), timePtr(archived)
		out = append(out, p)
	}
	return out, rows.Err()
}

// IAPProductSales — өнім коды бойынша берілген (granted) сатып алулар саны.
func (s *Store) IAPProductSales(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT product_code, COUNT(*) FROM iap_purchases WHERE status = 'granted' GROUP BY product_code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var (
			code string
			n    int
		)
		if err := rows.Scan(&code, &n); err != nil {
			return nil, err
		}
		out[code] = n
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- purchases

const iapPurchaseColumns = `p.id, COALESCE(p.user_id, ''), p.provider, p.environment, p.store_product_id, p.product_code,
	p.transaction_id, p.original_transaction_id, p.order_id, p.quantity, p.units, p.status, p.consumed_at,
	p.purchased_at, p.revoked_at, p.revoke_reason, p.created_at, p.updated_at`

func scanIAPPurchase(scan func(...any) error, extra ...any) (domain.IAPPurchase, error) {
	var (
		p                           domain.IAPPurchase
		consumed, purchased, revoke sql.NullInt64
		created, updated            int64
	)
	dest := []any{&p.ID, &p.UserID, &p.Provider, &p.Environment, &p.StoreProductID, &p.ProductCode,
		&p.TransactionID, &p.OriginalTransactionID, &p.OrderID, &p.Quantity, &p.Units, &p.Status, &consumed,
		&purchased, &revoke, &p.RevokeReason, &created, &updated}
	if err := scan(append(dest, extra...)...); err != nil {
		return domain.IAPPurchase{}, err
	}
	p.ConsumedAt, p.PurchasedAt, p.RevokedAt = timePtr(consumed), timePtr(purchased), timePtr(revoke)
	p.CreatedAt, p.UpdatedAt = timeFrom(created), timeFrom(updated)
	return p, nil
}

// IAPPurchaseByTransaction — дүкен транзакциясы бойынша жол.
func (s *Store) IAPPurchaseByTransaction(ctx context.Context, provider, transactionID string) (domain.IAPPurchase, error) {
	row := s.db.Reader().QueryRowContext(ctx, `SELECT `+iapPurchaseColumns+` FROM iap_purchases p
		WHERE p.provider = ? AND p.transaction_id = ?`, provider, transactionID)
	p, err := scanIAPPurchase(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.IAPPurchase{}, domain.ErrNotFound
	}
	return p, err
}

// PurchaseGrant — расталған транзакция және ол беретін кредит.
type PurchaseGrant struct {
	// Purchase — UserID, Provider, Environment, StoreProductID, ProductCode, TransactionID,
	// OriginalTransactionID, OrderID, Quantity, Units (units × quantity), PurchasedAt.
	Purchase domain.IAPPurchase
	// Kind — pack | pass; Duration — pass-тың ұзақтығы (pack — 0).
	Kind     string
	Duration time.Duration
	// SealedToken — Google: тұтынылғанша сақталатын шифрланған токен ("" — жоқ).
	SealedToken string
	Now         time.Time
}

// GrantResult — нәтиже: Granted — осы шақыру берді (false — бұрын берілген).
type GrantResult struct {
	Purchase domain.IAPPurchase
	Granted  bool
}

// GrantPurchase — транзакцияны тіркеп, кредитті бір атомарлы транзакцияда береді.
//
// ON CONFLICT (provider, transaction_id) makes a re-posted transaction return
// the existing row: granted again → GrantResult.Granted false; bound to
// another account (or to a deleted one) → ErrPurchaseOwnedElsewhere; already
// refunded or revoked → PurchaseInvalid(revoked); a pending row of this
// account is completed here, and its created_at becomes the grant time, so
// the consume retry counts from the grant (UnconsumedPurchases). A pack never
// expires. A pass starts now or when the account's latest pass that still has
// units ends, whichever is later, and lasts Duration: a pass that is used up
// never holds a new one back (startWaitingPasses). The grant and its ledger
// entry are written in the same transaction, so a purchase can never be
// recorded without its units or granted twice.
func (s *Store) GrantPurchase(ctx context.Context, in PurchaseGrant) (GrantResult, error) {
	var out GrantResult
	p := in.Purchase
	now := ms(in.Now)
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		id := traits.NewID()
		res, err := tx.ExecContext(ctx, `
			INSERT INTO iap_purchases (id, user_id, provider, environment, store_product_id, product_code, transaction_id,
			                           original_transaction_id, order_id, quantity, units, status, purchase_token_sealed,
			                           purchased_at, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT (provider, transaction_id) DO NOTHING`,
			id, p.UserID, p.Provider, p.Environment, p.StoreProductID, p.ProductCode, p.TransactionID,
			p.OriginalTransactionID, p.OrderID, p.Quantity, p.Units, domain.PurchaseGranted, nullText(in.SealedToken),
			msPtr(p.PurchasedAt), now, now)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			existing, err := purchaseInTx(ctx, tx, p.Provider, p.TransactionID)
			if err != nil {
				return err
			}
			switch {
			case existing.Status == domain.PurchaseRefunded || existing.Status == domain.PurchaseRevoked:
				return domain.PurchaseInvalid(domain.PurchaseReasonRevoked)
			case existing.Status == domain.PurchaseRejected:
				return domain.PurchaseInvalid(domain.PurchaseReasonCanceled)
			case existing.UserID != p.UserID:
				return domain.ErrPurchaseOwnedElsewhere
			case existing.Status == domain.PurchaseGranted:
				out.Purchase = existing
				return nil
			}
			// A pending purchase of this account was paid: it is granted now, and
			// from now on (created_at) the consume retry measures its grace and give-up.
			if _, err := tx.ExecContext(ctx, `
				UPDATE iap_purchases SET status = ?, environment = ?, store_product_id = ?, product_code = ?,
				       order_id = ?, quantity = ?, units = ?, purchase_token_sealed = ?, purchased_at = ?,
				       created_at = ?, updated_at = ?
				WHERE id = ? AND status = ?`,
				domain.PurchaseGranted, p.Environment, p.StoreProductID, p.ProductCode, p.OrderID, p.Quantity, p.Units,
				nullText(in.SealedToken), msPtr(p.PurchasedAt), now, now, existing.ID, domain.PurchasePending); err != nil {
				return err
			}
			id = existing.ID
		}

		starts := in.Now
		var expires *time.Time
		if in.Kind == domain.GrantPass {
			if starts, err = startWaitingPasses(ctx, tx, p.UserID, in.Now); err != nil {
				return err
			}
			end := starts.Add(in.Duration)
			expires = &end
		}
		grantID := traits.NewID()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO credit_grants (id, user_id, kind, units_total, units_remaining, starts_at, expires_at, status,
			                           iap_purchase_id, product_code, granted_by, reason, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,'active',?,?,'system','',?,?)`,
			grantID, p.UserID, in.Kind, p.Units, p.Units, ms(starts), msPtr(expires), id, p.ProductCode,
			now, now); err != nil {
			return err
		}
		if err := insertLedger(ctx, tx, domain.LedgerEntry{UserID: p.UserID, GrantID: grantID, Entry: domain.LedgerGrant,
			Kind: in.Kind, Delta: p.Units, Actor: "system", Reason: p.Provider, CreatedAt: in.Now}, ""); err != nil {
			return err
		}
		out.Purchase, err = purchaseInTx(ctx, tx, p.Provider, p.TransactionID)
		out.Granted = true
		return err
	})
	if err != nil {
		return GrantResult{}, err
	}
	return out, nil
}

// RecordPendingPurchase — Google «pending» сатып алуы: ештеңе берілмейді, транзакция тіркелгіге байланады.
//
// The same rules as GrantPurchase for an existing row: another account's →
// ErrPurchaseOwnedElsewhere, refunded / revoked / cancelled → PurchaseInvalid.
// An already granted row stays granted (status returned as is).
func (s *Store) RecordPendingPurchase(ctx context.Context, p domain.IAPPurchase, now time.Time) (domain.IAPPurchase, error) {
	var out domain.IAPPurchase
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO iap_purchases (id, user_id, provider, environment, store_product_id, product_code, transaction_id,
			                           original_transaction_id, order_id, quantity, units, status, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,0,?,?,?)
			ON CONFLICT (provider, transaction_id) DO NOTHING`,
			traits.NewID(), p.UserID, p.Provider, p.Environment, p.StoreProductID, p.ProductCode, p.TransactionID,
			p.OriginalTransactionID, p.OrderID, p.Quantity, domain.PurchasePending, ms(now), ms(now)); err != nil {
			return err
		}
		existing, err := purchaseInTx(ctx, tx, p.Provider, p.TransactionID)
		if err != nil {
			return err
		}
		switch {
		case existing.Status == domain.PurchaseRefunded || existing.Status == domain.PurchaseRevoked:
			return domain.PurchaseInvalid(domain.PurchaseReasonRevoked)
		case existing.Status == domain.PurchaseRejected:
			return domain.PurchaseInvalid(domain.PurchaseReasonCanceled)
		case existing.UserID != p.UserID:
			return domain.ErrPurchaseOwnedElsewhere
		}
		out = existing
		return nil
	})
	return out, err
}

// RejectPendingPurchase — дүкен «pending» сатып алуды болдырмады (rejected, ештеңе берілмеген).
func (s *Store) RejectPendingPurchase(ctx context.Context, provider, transactionID string, now time.Time) error {
	_, err := s.db.Writer().ExecContext(ctx, `
		UPDATE iap_purchases SET status = ?, revoked_at = ?, revoke_reason = 'canceled', updated_at = ?
		WHERE provider = ? AND transaction_id = ? AND status = ?`,
		domain.PurchaseRejected, ms(now), ms(now), provider, transactionID, domain.PurchasePending)
	return err
}

func purchaseInTx(ctx context.Context, tx *sql.Tx, provider, transactionID string) (domain.IAPPurchase, error) {
	row := tx.QueryRowContext(ctx, `SELECT `+iapPurchaseColumns+` FROM iap_purchases p
		WHERE p.provider = ? AND p.transaction_id = ?`, provider, transactionID)
	return scanIAPPurchase(row.Scan)
}

// PurchaseRevocation — дүкен қайтарған транзакция.
type PurchaseRevocation struct {
	Provider      string
	TransactionID string
	// Status — refunded | revoked.
	Status string
	// Reason — refund | revoke | voided (+ дүкеннің себеп коды); журнал мен әкімші үшін.
	Reason string
	// At — дүкен қашан қайтарды (revocationDate, voidedTimeMillis).
	At  time.Time
	Now time.Time
	// Tombstone — бізде жоқ транзакция: осы жол Status-пен, иесіз жазылады, кейін берілмейді.
	Tombstone *domain.IAPPurchase
}

// RevocationResult — не болды.
type RevocationResult struct {
	// Found — транзакция бізде бар еді.
	Found bool
	// Revoked — осы шақыру күйін ауыстырды (қайталанған хабарлама — false).
	Revoked bool
	// UnitsRemoved — кредиттің қалдығынан алынғаны (теріс емес).
	UnitsRemoved int
	UserID       string
}

// RevokePurchase — қайтарылған сатып алу: транзакция refunded | revoked, кредиттің қалдығы нөлге.
//
// Units already spent stay spent and the balance never goes negative: the
// ledger records exactly what was left (delta = -units_remaining). Repeating
// the same notification changes nothing. An unknown transaction is written
// as a tombstone when the caller has its details, so a later post of it is
// refused instead of granted.
func (s *Store) RevokePurchase(ctx context.Context, in PurchaseRevocation) (RevocationResult, error) {
	var out RevocationResult
	at := ms(in.At)
	if in.At.IsZero() {
		at = ms(in.Now)
	}
	now := ms(in.Now)
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		existing, err := purchaseInTx(ctx, tx, in.Provider, in.TransactionID)
		if errors.Is(err, sql.ErrNoRows) {
			if in.Tombstone == nil {
				return nil
			}
			t := in.Tombstone
			_, err := tx.ExecContext(ctx, `
				INSERT INTO iap_purchases (id, user_id, provider, environment, store_product_id, product_code, transaction_id,
				                           original_transaction_id, order_id, quantity, units, status, purchased_at,
				                           revoked_at, revoke_reason, created_at, updated_at)
				VALUES (?,NULL,?,?,?,?,?,?,?,?,0,?,?,?,?,?,?)
				ON CONFLICT (provider, transaction_id) DO NOTHING`,
				traits.NewID(), in.Provider, t.Environment, t.StoreProductID, t.ProductCode, in.TransactionID,
				t.OriginalTransactionID, t.OrderID, max(t.Quantity, 1), in.Status, msPtr(t.PurchasedAt), at, in.Reason,
				now, now)
			return err
		}
		if err != nil {
			return err
		}
		out.Found, out.UserID = true, existing.UserID
		if existing.Status == domain.PurchaseRefunded || existing.Status == domain.PurchaseRevoked {
			return nil
		}
		out.Revoked = true
		if _, err := tx.ExecContext(ctx, `
			UPDATE iap_purchases SET status = ?, revoked_at = ?, revoke_reason = ?, purchase_token_sealed = NULL,
			       updated_at = ?
			WHERE id = ?`, in.Status, at, in.Reason, now, existing.ID); err != nil {
			return err
		}
		if existing.Status != domain.PurchaseGranted {
			return nil // pending or rejected: nothing was granted
		}
		var (
			grantID, kind, userID string
			remaining             int
		)
		err = tx.QueryRowContext(ctx, `
			SELECT id, kind, user_id, units_remaining FROM credit_grants WHERE iap_purchase_id = ? AND status = 'active'`,
			existing.ID).Scan(&grantID, &kind, &userID, &remaining)
		if errors.Is(err, sql.ErrNoRows) {
			return nil // the account was deleted, or an admin revoked the grant already
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE credit_grants SET status = 'revoked', units_remaining = 0, revoked_at = ?, revoked_by = 'system',
			       revoke_reason = ?, updated_at = ?
			WHERE id = ? AND status = 'active'`, now, in.Reason, now, grantID); err != nil {
			return err
		}
		out.UnitsRemoved = max(remaining, 0)
		if kind == domain.GrantPass {
			if _, err := startWaitingPasses(ctx, tx, userID, in.Now); err != nil {
				return err
			}
		}
		return insertLedger(ctx, tx, domain.LedgerEntry{UserID: userID, GrantID: grantID, Entry: domain.LedgerRevoke,
			Kind: kind, Delta: -out.UnitsRemoved, Actor: "system", Reason: in.Reason, CreatedAt: in.Now}, "")
	})
	if err != nil {
		return RevocationResult{}, err
	}
	return out, nil
}

// RestoreResult — REFUND_REVERSED нәтижесі.
type RestoreResult struct {
	// Restored — транзакция қайта granted болды.
	Restored bool
	// Units — кредитке қайтарылғаны.
	Units int
	// Cleared — ерте келген қайтарудың иесіз белгісі өшірілді: қосымша жібергенде транзакция беріледі.
	Cleared bool
}

// RestorePurchase — дүкен қайтаруды болдырмады (REFUND_REVERSED): транзакция қайта granted.
//
// The grant gets back what the store revocation took from it, when the
// store (not an administrator) revoked it. A pass whose time is over stays
// over. A refund that came before the app posted the transaction left a
// tombstone (no account, nothing granted, RevokePurchase): it is removed, so
// the app's post grants the transaction normally, still only to the account
// its appAccountToken names. A deleted account's purchase stays as it is.
func (s *Store) RestorePurchase(ctx context.Context, provider, transactionID string, now time.Time) (RestoreResult, error) {
	var out RestoreResult
	at := ms(now)
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		existing, err := purchaseInTx(ctx, tx, provider, transactionID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if existing.Status != domain.PurchaseRefunded && existing.Status != domain.PurchaseRevoked {
			return nil
		}
		if existing.UserID == "" && existing.Units == 0 {
			res, err := tx.ExecContext(ctx, `
				DELETE FROM iap_purchases WHERE id = ? AND user_id IS NULL AND units = 0
				  AND NOT EXISTS (SELECT 1 FROM credit_grants WHERE iap_purchase_id = iap_purchases.id)`, existing.ID)
			if err != nil {
				return err
			}
			n, err := res.RowsAffected()
			out.Cleared = n == 1
			return err
		}
		if existing.UserID == "" || existing.Units == 0 {
			return nil // a deleted account: nothing to give back to
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE iap_purchases SET status = 'granted', revoked_at = NULL, revoke_reason = '', updated_at = ? WHERE id = ?`,
			at, existing.ID); err != nil {
			return err
		}
		out.Restored = true
		var (
			grantID, kind, revokedBy, userID string
			total                            int
		)
		err = tx.QueryRowContext(ctx, `
			SELECT id, kind, revoked_by, user_id, units_total FROM credit_grants WHERE iap_purchase_id = ? AND status = 'revoked'`,
			existing.ID).Scan(&grantID, &kind, &revokedBy, &userID, &total)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if revokedBy != "system" {
			return nil // an administrator's revocation stays
		}
		var taken sql.NullInt64
		if err := tx.QueryRowContext(ctx, `
			SELECT delta FROM credit_ledger WHERE grant_id = ? AND entry = 'revoke'
			ORDER BY created_at DESC, rowid DESC LIMIT 1`, grantID).Scan(&taken); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		out.Units = min(max(int(-taken.Int64), 0), total)
		if _, err := tx.ExecContext(ctx, `
			UPDATE credit_grants SET status = 'active', units_remaining = ?, revoked_at = NULL, revoked_by = '',
			       revoke_reason = '', updated_at = ?
			WHERE id = ?`, out.Units, at, grantID); err != nil {
			return err
		}
		return insertLedger(ctx, tx, domain.LedgerEntry{UserID: userID, GrantID: grantID, Entry: domain.LedgerGrant,
			Kind: kind, Delta: out.Units, Actor: "system", Reason: "refund_reversed", CreatedAt: now}, "")
	})
	if err != nil {
		return RestoreResult{}, err
	}
	return out, nil
}

// UnconsumedPurchase — сервер әлі тұтынбаған Google сатып алуы (қайталау үшін).
type UnconsumedPurchase struct {
	ID             string
	StoreProductID string
	TransactionID  string
	SealedToken    string
	// GrantedAt — қашан берілді (created_at: pending жол төленгенде жаңарады).
	GrantedAt time.Time
}

// UnconsumedPurchases — before-тан бұрын берілген, бірақ тұтынылмаған сатып алулар (ескісі бірінші).
//
// created_at of a granted row is its grant time: GrantPurchase writes it
// when a pending row is paid, so a purchase that stayed pending for days is
// not mistaken for one the store has already refunded.
func (s *Store) UnconsumedPurchases(ctx context.Context, provider string, before time.Time, limit int) ([]UnconsumedPurchase, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT id, store_product_id, transaction_id, COALESCE(purchase_token_sealed, ''), created_at FROM iap_purchases
		WHERE provider = ? AND status = 'granted' AND consumed_at IS NULL AND created_at < ?
		  AND purchase_token_sealed IS NOT NULL
		ORDER BY created_at LIMIT ?`, provider, ms(before), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UnconsumedPurchase
	for rows.Next() {
		var (
			u       UnconsumedPurchase
			created int64
		)
		if err := rows.Scan(&u.ID, &u.StoreProductID, &u.TransactionID, &u.SealedToken, &created); err != nil {
			return nil, err
		}
		u.GrantedAt = timeFrom(created)
		out = append(out, u)
	}
	return out, rows.Err()
}

// MarkPurchaseConsumed — сервер тұтынды: шифрланған токен өшіріледі.
func (s *Store) MarkPurchaseConsumed(ctx context.Context, id string, now time.Time) error {
	_, err := s.db.Writer().ExecContext(ctx, `
		UPDATE iap_purchases SET consumed_at = ?, purchase_token_sealed = NULL, updated_at = ?
		WHERE id = ? AND consumed_at IS NULL`, ms(now), ms(now), id)
	return err
}

// ForgetPurchaseToken — тұтыну мүмкін емес (дүкен бас тартты): токен өшіріледі, қайталау тоқтайды.
func (s *Store) ForgetPurchaseToken(ctx context.Context, id string, now time.Time) error {
	_, err := s.db.Writer().ExecContext(ctx, `
		UPDATE iap_purchases SET purchase_token_sealed = NULL, updated_at = ? WHERE id = ?`, ms(now), id)
	return err
}

// IAPPurchasesByUser — қолданушының сатып алулары, жаңасы бірінші.
func (s *Store) IAPPurchasesByUser(ctx context.Context, userID string, limit int) ([]domain.IAPPurchase, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `SELECT `+iapPurchaseColumns+` FROM iap_purchases p
		WHERE p.user_id = ? ORDER BY p.created_at DESC, p.id LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.IAPPurchase
	for rows.Next() {
		p, err := scanIAPPurchase(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PurchaseFilter — әкімшінің «Покупки» тізімі.
type PurchaseFilter struct {
	Provider    string
	Status      string
	Environment string
	UserID      string
	Page        traits.Page
}

// PurchaseRow — тізім жолы және иесі (жойылған тіркелгіде User.ID бос).
type PurchaseRow struct {
	Purchase domain.IAPPurchase
	User     domain.User
}

// ListIAPPurchases — сүзгіленген сатып алулар, жаңасы бірінші.
func (s *Store) ListIAPPurchases(ctx context.Context, f PurchaseFilter) ([]PurchaseRow, int, error) {
	where, args := []string{"1 = 1"}, []any{}
	for _, c := range []struct{ column, value string }{
		{"p.provider", f.Provider}, {"p.status", f.Status}, {"p.environment", f.Environment}, {"p.user_id", f.UserID},
	} {
		if c.value != "" {
			where = append(where, c.column+" = ?")
			args = append(args, c.value)
		}
	}
	clause := strings.Join(where, " AND ")
	var total int
	if err := s.db.Reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM iap_purchases p WHERE `+clause, args...).
		Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT `+iapPurchaseColumns+`, COALESCE(u.email, ''), COALESCE(u.phone, ''), COALESCE(u.legacy_client, '')
		FROM iap_purchases p LEFT JOIN users u ON u.id = p.user_id
		WHERE `+clause+` ORDER BY p.created_at DESC, p.id LIMIT ? OFFSET ?`,
		append(args, f.Page.Limit, f.Page.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []PurchaseRow
	for rows.Next() {
		var row PurchaseRow
		p, err := scanIAPPurchase(rows.Scan, &row.User.Email, &row.User.Phone, &row.User.LegacyClient)
		if err != nil {
			return nil, 0, err
		}
		row.Purchase, row.User.ID = p, p.UserID
		out = append(out, row)
	}
	return out, total, rows.Err()
}

// PurchaseStat — бір топтың саны мен бірліктері.
type PurchaseStat struct {
	Provider    string
	Environment string
	Status      string
	Count       int
	Units       int
}

// IAPPurchaseStats — дүкен, орта және күй бойынша топтар.
func (s *Store) IAPPurchaseStats(ctx context.Context) ([]PurchaseStat, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT provider, environment, status, COUNT(*), COALESCE(SUM(units), 0) FROM iap_purchases
		GROUP BY provider, environment, status ORDER BY provider, environment, status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PurchaseStat
	for rows.Next() {
		var st PurchaseStat
		if err := rows.Scan(&st.Provider, &st.Environment, &st.Status, &st.Count, &st.Units); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- notifications

// IAPNotificationSeen — бұл хабарлама бұрын өңделген бе.
func (s *Store) IAPNotificationSeen(ctx context.Context, provider, notificationID string) (bool, error) {
	var n int
	err := s.db.Reader().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM iap_notifications WHERE provider = ? AND notification_id = ?`, provider, notificationID).Scan(&n)
	return n > 0, err
}

// InsertIAPNotification — өңделген хабарлама. false — бұрын жазылған.
func (s *Store) InsertIAPNotification(ctx context.Context, n domain.IAPNotification) (bool, error) {
	res, err := s.db.Writer().ExecContext(ctx, `
		INSERT INTO iap_notifications (id, provider, notification_id, type, subtype, transaction_id, outcome, received_at)
		VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT (provider, notification_id) DO NOTHING`,
		traits.NewID(), n.Provider, n.NotificationID, n.Type, n.Subtype, n.TransactionID, n.Outcome, ms(n.ReceivedAt))
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	return affected == 1, err
}
