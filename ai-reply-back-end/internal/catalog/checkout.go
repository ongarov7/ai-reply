package catalog

import (
	"context"
	"database/sql"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// ProductForWebInTx fixes a published offer within the purchase transaction.
func (s *Service) ProductForWebInTx(ctx context.Context, tx *sql.Tx, code string) (Product, int64, error) {
	v, err := scan(tx.QueryRowContext(ctx, `SELECT `+columns+` FROM billing_catalog_versions WHERE state='published'`))
	if err != nil {
		return Product{}, 0, err
	}
	for _, p := range v.Products {
		if p.Code == code && p.Active && p.AvailableWeb && p.PriceMinor > 0 {
			return p, v.ID, nil
		}
	}
	return Product{}, 0, domain.ErrPlanUnavailable
}
