package bcc

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/aireply/ai-reply-back-end/internal/catalog"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/repository"
)

func (s *CheckoutService) checkUnlimitedPurchaseTx(ctx context.Context, tx *sql.Tx, user string, product catalog.Product) error {
	if product.Kind != "subscription" || !product.Unlimited {
		return nil
	}
	hasAccess, err := repository.HasUsableUnlimitedSubscriptionInTx(ctx, tx, user, s.now().UTC())
	if err != nil {
		return err
	}
	if hasAccess {
		return domain.ConflictField("subscription", "unlimited_active")
	}
	return nil
}

// A retry keeps its original order and offer. Before sending an unpaid order
// back to the bank, also account for access granted since its creation.
func (s *CheckoutService) checkExistingUnlimitedPurchase(ctx context.Context, order BankOrder) error {
	if order.Status != "created" || order.Kind != "subscription" {
		return nil
	}
	var product catalog.Product
	if err := json.Unmarshal([]byte(order.snapshot), &product); err != nil {
		return err
	}
	if !product.Unlimited {
		return nil
	}
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		return s.checkUnlimitedPurchaseTx(ctx, tx, order.userID, product)
	})
}
