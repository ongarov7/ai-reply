package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// The same completed production transactions feed totals, product breakdown and
// history. Historical amounts come only from signed receipts or sold bank orders;
// a catalogue edit must never change recorded sales amounts.
const adminPurchasesCTE = `WITH admin_purchases AS (
 SELECT 'iap:' || p.id AS id, p.user_id, p.provider,
  COALESCE(NULLIF(p.product_code,''),NULLIF(p.store_product_id,''),'unknown') AS product_code,
  COALESCE(c.kind,'') AS kind,
  COALESCE(c.name_kk,'') AS name_kk, COALESCE(c.name_ru,'') AS name_ru,
  COALESCE(c.name_en,'') AS name_en, COALESCE(c.name_uz,'') AS name_uz,
  COALESCE(c.price_minor,0) AS catalog_price_minor, COALESCE(c.currency,'') AS catalog_currency,
  p.purchased_at, p.quantity, p.units, p.price_milliunits AS amount_milliunits, p.currency,
  p.price_milliunits>0 AND p.currency<>'' AS price_known
 FROM iap_purchases p LEFT JOIN iap_products c ON c.code=p.product_code
 WHERE p.environment='Production' AND p.status='granted' AND p.purchased_at BETWEEN ? AND ?
 UNION ALL
 SELECT 'bank:' || o.id, o.user_id, o.provider, o.product_code,
  COALESCE(NULLIF(json_extract(o.snapshot_json,'$.kind'),''),c.kind,''),
  COALESCE(NULLIF(c.name_kk,''),json_extract(o.snapshot_json,'$.name.kk'),''),
  COALESCE(NULLIF(c.name_ru,''),json_extract(o.snapshot_json,'$.name.ru'),''),
  COALESCE(NULLIF(c.name_en,''),json_extract(o.snapshot_json,'$.name.en'),''),
  COALESCE(NULLIF(c.name_uz,''),json_extract(o.snapshot_json,'$.name.uz'),''),
  COALESCE(c.price_minor,0), COALESCE(c.currency,''),
  o.created_at, 1, COALESCE(CAST(json_extract(o.snapshot_json,'$.units') AS INTEGER),0),
  o.amount_minor*10, o.currency, o.amount_minor>0 AND o.currency<>''
 FROM billing_orders o LEFT JOIN iap_products c ON c.code=o.product_code
 WHERE o.environment='production' AND o.status='succeeded' AND o.created_at BETWEEN ? AND ?
)
`

func adminPurchaseArgs(from, to time.Time) []any {
	return []any{ms(from), ms(to), ms(from), ms(to)}
}

// PurchaseTotals counts completed production purchases, including packs and
// subscription renewals. Test checkouts and refunded purchases are excluded.
// These are transactions in the selected period, independently of current access.
type PurchaseTotals struct {
	Purchases int
	Buyers    int
}

func (s *Store) PurchaseTotals(ctx context.Context, from, to time.Time) (PurchaseTotals, error) {
	var out PurchaseTotals
	err := s.db.Reader().QueryRowContext(ctx, adminPurchasesCTE+`
		SELECT COUNT(*), COUNT(DISTINCT user_id) FROM admin_purchases`, adminPurchaseArgs(from, to)...).
		Scan(&out.Purchases, &out.Buyers)
	return out, err
}

type PurchaseAmount struct {
	Currency         string `json:"currency"`
	AmountMilliunits int64  `json:"amount_milliunits"`
	PricedPurchases  int    `json:"priced_purchases"`
}

// PurchaseProduct keeps current catalogue labels separate from receipt money.
type PurchaseProduct struct {
	ProductCode       string
	Name              map[string]string
	Kind              string
	CatalogPriceMinor int64
	CatalogCurrency   string
}

func (p PurchaseProduct) LocalizedName(locale string) string {
	return (domain.IAPProduct{Code: p.ProductCode, Name: p.Name}).LocalizedName(locale)
}

type PurchaseBreakdown struct {
	PurchaseProduct
	Purchases             int
	Buyers                int
	Units                 int
	Amounts               []PurchaseAmount
	UnknownPricePurchases int
}

func (s *Store) PurchaseBreakdown(ctx context.Context, from, to time.Time) ([]PurchaseBreakdown, error) {
	rows, err := s.db.Reader().QueryContext(ctx, adminPurchasesCTE+`
 SELECT sales.product_code, MAX(sales.kind), MAX(sales.name_kk), MAX(sales.name_ru), MAX(sales.name_en), MAX(sales.name_uz),
  MAX(sales.catalog_price_minor), MAX(sales.catalog_currency), COUNT(*), COUNT(DISTINCT sales.user_id), SUM(sales.units),
  SUM(CASE WHEN sales.price_known THEN 0 ELSE 1 END),
  (SELECT json_group_array(json_object('currency',currency,'amount_milliunits',amount,'priced_purchases',priced))
   FROM (SELECT currency,SUM(amount_milliunits) AS amount,COUNT(*) AS priced FROM admin_purchases money
         WHERE money.product_code=sales.product_code AND money.price_known GROUP BY currency ORDER BY currency))
 FROM admin_purchases sales GROUP BY sales.product_code ORDER BY COUNT(*) DESC, sales.product_code`, adminPurchaseArgs(from, to)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]PurchaseBreakdown, 0)
	for rows.Next() {
		var p PurchaseBreakdown
		var kk, ru, en, uz, amounts string
		if err := rows.Scan(&p.ProductCode, &p.Kind, &kk, &ru, &en, &uz, &p.CatalogPriceMinor, &p.CatalogCurrency,
			&p.Purchases, &p.Buyers, &p.Units, &p.UnknownPricePurchases, &amounts); err != nil {
			return nil, err
		}
		p.Name = map[string]string{"kk": kk, "ru": ru, "en": en, "uz": uz}
		if err := json.Unmarshal([]byte(amounts), &p.Amounts); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

type PurchaseHistoryRow struct {
	PurchaseProduct
	ID               string
	User             domain.User
	Provider         string
	PurchasedAt      time.Time
	Quantity         int
	Units            int
	AmountMilliunits int64
	Currency         string
	PriceKnown       bool
}

func (s *Store) PurchaseHistory(ctx context.Context, from, to time.Time, product string, page traits.Page) ([]PurchaseHistoryRow, int, error) {
	where := ""
	args := adminPurchaseArgs(from, to)
	if product != "" {
		where = " WHERE sales.product_code=?"
		args = append(args, product)
	}
	var total int
	if err := s.db.Reader().QueryRowContext(ctx, adminPurchasesCTE+`SELECT COUNT(*) FROM admin_purchases sales`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	page = traits.NewPage(page.Limit, page.Offset)
	rows, err := s.db.Reader().QueryContext(ctx, adminPurchasesCTE+`
 SELECT sales.id,COALESCE(u.id,''),COALESCE(u.phone,''),COALESCE(u.email,''),COALESCE(u.legacy_client,''),
  sales.provider,sales.product_code,sales.kind,sales.name_kk,sales.name_ru,sales.name_en,sales.name_uz,
  sales.catalog_price_minor,sales.catalog_currency,sales.purchased_at,sales.quantity,sales.units,
  sales.amount_milliunits,sales.currency,sales.price_known
 FROM admin_purchases sales LEFT JOIN users u ON u.id=sales.user_id AND u.deleted_at IS NULL`+where+`
 ORDER BY sales.purchased_at DESC,sales.id DESC LIMIT ? OFFSET ?`, append(args, page.Limit, page.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]PurchaseHistoryRow, 0)
	for rows.Next() {
		var p PurchaseHistoryRow
		var purchased int64
		var kk, ru, en, uz string
		if err := rows.Scan(&p.ID, &p.User.ID, &p.User.Phone, &p.User.Email, &p.User.LegacyClient, &p.Provider,
			&p.ProductCode, &p.Kind, &kk, &ru, &en, &uz, &p.CatalogPriceMinor, &p.CatalogCurrency,
			&purchased, &p.Quantity, &p.Units, &p.AmountMilliunits, &p.Currency, &p.PriceKnown); err != nil {
			return nil, 0, err
		}
		p.Name = map[string]string{"kk": kk, "ru": ru, "en": en, "uz": uz}
		p.PurchasedAt = timeFrom(purchased)
		out = append(out, p)
	}
	return out, total, rows.Err()
}
