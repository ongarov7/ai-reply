package adminapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/aireply/ai-reply-back-end/internal/admin"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/traits"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

func purchaseBreakdownDTO(rows []repository.PurchaseBreakdown, locale string) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, p := range rows {
		out = append(out, map[string]any{
			"product_code": p.ProductCode, "name": p.LocalizedName(locale), "kind": p.Kind,
			"catalog_price_minor": p.CatalogPriceMinor, "catalog_currency": p.CatalogCurrency,
			"purchases": p.Purchases, "buyers": p.Buyers, "units": p.Units,
			"amounts": p.Amounts, "unknown_price_purchases": p.UnknownPricePurchases,
		})
	}
	return out
}

func (s *Server) handleDashboardPurchases(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	query := r.URL.Query()
	page, limit := 1, 20
	for _, field := range []string{"page", "limit"} {
		raw := query.Get(field)
		if raw == "" {
			continue
		}
		value, err := strconv.Atoi(raw)
		max := 1000000
		if field == "limit" {
			max = 100
		}
		if err != nil || value < 1 || value > max {
			httpx.Fail(w, domain.InvalidField(field, "range"))
			return
		}
		if field == "page" {
			page = value
		} else {
			limit = value
		}
	}
	product := strings.TrimSpace(query.Get("product"))
	if len(product) > 256 {
		httpx.Fail(w, domain.InvalidField("product", "too_long"))
		return
	}
	rng := admin.ParseRange(query.Get("range"), query.Get("from"), query.Get("to"), s.cfg.App.Location(), s.admin.Now())
	rows, total, err := s.admin.PurchaseHistory(r.Context(), rng, product, traits.NewPage(limit, (page-1)*limit))
	if err != nil {
		s.fail(w, err)
		return
	}
	locale := adminFrom(r.Context()).Locale
	out := make([]map[string]any, 0, len(rows))
	for _, p := range rows {
		identifier := ""
		if p.User.ID != "" {
			identifier = maskIdentifier(p.User)
		}
		out = append(out, map[string]any{
			"id": p.ID, "user_id": p.User.ID, "identifier": identifier,
			"product_code": p.ProductCode, "name": p.LocalizedName(locale), "kind": p.Kind,
			"provider": p.Provider, "purchased_at": p.PurchasedAt.In(s.cfg.App.Location()).Format("2006-01-02 15:04:05"),
			"quantity": p.Quantity, "units": p.Units, "amount_milliunits": p.AmountMilliunits,
			"currency": p.Currency, "price_known": p.PriceKnown,
		})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"range":   map[string]string{"key": rng.Key, "from": rng.From.In(s.cfg.App.Location()).Format("2006-01-02"), "to": rng.To.In(s.cfg.App.Location()).Format("2006-01-02")},
		"product": product, "total": total, "page": page, "limit": limit, "purchases": out,
	})
}
