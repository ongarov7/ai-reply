// Package catalog manages reviewed business prices independently from store checkout prices.
package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

var ErrConflict = fmt.Errorf("%w: catalog changed; reload before saving", domain.ErrConflict)
var productID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,199}$`)

type Product struct {
	Code           string            `json:"code"`
	Kind           string            `json:"kind"`
	Name           map[string]string `json:"name"`
	PriceMinor     int64             `json:"price_minor"`
	Currency       string            `json:"currency"`
	BillingPeriod  string            `json:"billing_period"`
	Units          int               `json:"units"`
	DailyLimit     int               `json:"daily_limit"`
	Unlimited      bool              `json:"unlimited"`
	AppleProductID string            `json:"apple_product_id"`
	AvailableIOS   bool              `json:"available_ios"`
	AvailableWeb   bool              `json:"available_web"`
	Active         bool              `json:"active"`
}

type Version struct {
	ID          int64     `json:"id"`
	State       string    `json:"state"`
	BaseVersion int64     `json:"base_version"`
	Revision    int       `json:"revision"`
	Products    []Product `json:"products"`
	CreatedBy   string    `json:"created_by,omitempty"`
	UpdatedBy   string    `json:"updated_by,omitempty"`
	PublishedBy string    `json:"published_by,omitempty"`
	UpdatedAt   int64     `json:"updated_at"`
	PublishedAt *int64    `json:"published_at,omitempty"`
}

type Service struct{ db *database.DB }

func New(db *database.DB) *Service { return &Service{db: db} }

func InitialProducts() []Product {
	names := func(ru, en, kk string) map[string]string {
		return map[string]string{"ru": ru, "en": en, "kk": kk, "uz": en}
	}
	return []Product{
		{Code: "free", Kind: "welcome", Name: names("Бесплатно", "Free", "Тегін"), Currency: "KZT", Units: 10, Active: true, AvailableIOS: true, AvailableWeb: true},
		{Code: "base_monthly", Kind: "subscription", Name: names("Base · месяц", "Base · monthly", "Base · ай сайын"), Currency: "KZT", PriceMinor: 59900, BillingPeriod: "month", DailyLimit: 30, Active: true, AvailableIOS: true, AvailableWeb: true},
		{Code: "base_yearly", Kind: "subscription", Name: names("Base · год", "Base · yearly", "Base · жыл сайын"), Currency: "KZT", PriceMinor: 599900, BillingPeriod: "year", DailyLimit: 30, Active: true, AvailableIOS: true, AvailableWeb: true},
		{Code: "pro_monthly", Kind: "subscription", Name: names("Pro · месяц", "Pro · monthly", "Pro · ай сайын"), Currency: "KZT", PriceMinor: 299900, BillingPeriod: "month", Unlimited: true, Active: true, AvailableIOS: true, AvailableWeb: true},
		{Code: "pro_yearly", Kind: "subscription", Name: names("Pro · год", "Pro · yearly", "Pro · жыл сайын"), Currency: "KZT", PriceMinor: 2999000, BillingPeriod: "year", Unlimited: true, Active: true, AvailableIOS: true, AvailableWeb: true},
		{Code: "pack30", Kind: "pack", Name: names("30 ответов", "30 replies", "30 жауап"), Currency: "KZT", PriceMinor: 10000, Units: 30, Active: true, AvailableIOS: true, AvailableWeb: true},
	}
}

// EnsureInitial runs once after migrations. It does not enable store or bank payments.
func (s *Service) EnsureInitial(ctx context.Context) error {
	raw, _ := json.Marshal(InitialProducts())
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM billing_catalog_versions`).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
		now := time.Now().UnixMilli()
		_, err := tx.ExecContext(ctx, `INSERT INTO billing_catalog_versions(state,products_json,created_by,updated_by,published_by,created_at,updated_at,published_at) VALUES('published',?,'migration','migration','migration',?,?,?)`, string(raw), now, now, now)
		return err
	})
}

const columns = `id,state,base_version,revision,products_json,created_by,updated_by,published_by,updated_at,published_at`

func scan(row interface{ Scan(...any) error }) (Version, error) {
	var v Version
	var raw string
	var published sql.NullInt64
	err := row.Scan(&v.ID, &v.State, &v.BaseVersion, &v.Revision, &raw, &v.CreatedBy, &v.UpdatedBy, &v.PublishedBy, &v.UpdatedAt, &published)
	if errors.Is(err, sql.ErrNoRows) {
		return v, domain.ErrNotFound
	}
	if err != nil {
		return v, err
	}
	if published.Valid {
		v.PublishedAt = &published.Int64
	}
	err = json.Unmarshal([]byte(raw), &v.Products)
	return v, err
}
func (s *Service) Published(ctx context.Context) (Version, error) {
	return scan(s.db.Reader().QueryRowContext(ctx, `SELECT `+columns+` FROM billing_catalog_versions WHERE state='published'`))
}
func (s *Service) Draft(ctx context.Context) (Version, error) {
	return scan(s.db.Reader().QueryRowContext(ctx, `SELECT `+columns+` FROM billing_catalog_versions WHERE state='draft'`))
}
func (s *Service) History(ctx context.Context) ([]Version, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `SELECT `+columns+` FROM billing_catalog_versions ORDER BY id DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Version{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func Validate(products []Product) error {
	if len(products) != 6 {
		return domain.InvalidField("products", "six_products_required")
	}
	expected := map[string]Product{}
	for _, p := range InitialProducts() {
		expected[p.Code] = p
	}
	ids := map[string]bool{}
	seen := map[string]bool{}
	for _, p := range products {
		base, ok := expected[p.Code]
		if !ok || seen[p.Code] {
			return domain.InvalidField("code", "value")
		}
		seen[p.Code] = true
		if p.Kind != base.Kind || p.BillingPeriod != base.BillingPeriod || p.Currency != "KZT" || p.PriceMinor < 0 || p.PriceMinor > 100000000 || p.Units < 0 || p.Units > 1000000 || p.DailyLimit < 0 || p.DailyLimit > 1000000 {
			return domain.InvalidField(p.Code, "value")
		}
		if p.Kind == "welcome" && (p.PriceMinor != 0 || p.Unlimited || p.DailyLimit != 0 || !p.Active || p.Units < 1) {
			return domain.InvalidField(p.Code, "welcome")
		}
		if p.Kind != "welcome" && p.PriceMinor <= 0 {
			return domain.InvalidField("price_minor", "positive")
		}
		if p.Kind == "pack" && (p.Units < 1 || p.DailyLimit != 0 || p.Unlimited) {
			return domain.InvalidField(p.Code, "pack")
		}
		if p.Kind == "subscription" && (p.Units != 0 || p.Unlimited == (p.DailyLimit > 0)) {
			return domain.InvalidField(p.Code, "allowance")
		}
		for _, l := range []string{"ru", "kk", "en"} {
			if n := strings.TrimSpace(p.Name[l]); n == "" || len([]rune(n)) > 80 {
				return domain.InvalidField("name", "required")
			}
		}
		if p.AppleProductID != "" {
			if !productID.MatchString(p.AppleProductID) || ids[p.AppleProductID] {
				return domain.InvalidField("apple_product_id", "unique")
			}
			ids[p.AppleProductID] = true
		}
	}
	return nil
}
func (s *Service) SaveDraft(ctx context.Context, actor string, expectedRevision int, products []Product) (Version, error) {
	if actor == "" {
		return Version{}, domain.ErrUnauthorized
	}
	if err := Validate(products); err != nil {
		return Version{}, err
	}
	raw, _ := json.Marshal(products)
	var result Version
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		current, err := scan(tx.QueryRowContext(ctx, `SELECT `+columns+` FROM billing_catalog_versions WHERE state='draft'`))
		exists := err == nil
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		if exists && current.Revision != expectedRevision || !exists && expectedRevision != 0 {
			return ErrConflict
		}
		now := time.Now().UnixMilli()
		before := "[]"
		if exists {
			b, _ := json.Marshal(current.Products)
			before = string(b)
			_, err = tx.ExecContext(ctx, `UPDATE billing_catalog_versions SET products_json=?,revision=revision+1,updated_by=?,updated_at=? WHERE id=?`, string(raw), actor, now, current.ID)
		} else {
			var base int64
			if err = tx.QueryRowContext(ctx, `SELECT id FROM billing_catalog_versions WHERE state='published'`).Scan(&base); err != nil {
				return err
			}
			var r sql.Result
			r, err = tx.ExecContext(ctx, `INSERT INTO billing_catalog_versions(state,base_version,products_json,created_by,updated_by,created_at,updated_at) VALUES('draft',?,?,?,?,?,?)`, base, string(raw), actor, actor, now, now)
			if err == nil {
				current.ID, _ = r.LastInsertId()
			}
		}
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO billing_catalog_audit(version_id,action,actor,before_json,after_json,created_at) VALUES(?,'save_draft',?,?,?,?)`, current.ID, actor, before, string(raw), now); err != nil {
			return err
		}
		result, err = scan(tx.QueryRowContext(ctx, `SELECT `+columns+` FROM billing_catalog_versions WHERE id=?`, current.ID))
		return err
	})
	return result, err
}
func (s *Service) Publish(ctx context.Context, actor string, id int64, revision int) (Version, error) {
	if actor == "" {
		return Version{}, domain.ErrUnauthorized
	}
	var out Version
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		v, err := scan(tx.QueryRowContext(ctx, `SELECT `+columns+` FROM billing_catalog_versions WHERE id=? AND state='draft'`, id))
		if err != nil {
			return err
		}
		if v.Revision != revision {
			return ErrConflict
		}
		if err = Validate(v.Products); err != nil {
			return err
		}
		current, err := scan(tx.QueryRowContext(ctx, `SELECT `+columns+` FROM billing_catalog_versions WHERE state='published'`))
		if err != nil {
			return err
		}
		if current.ID != v.BaseVersion {
			return ErrConflict
		}
		now := time.Now().UnixMilli()
		for _, p := range v.Products {
			if err = synchronize(ctx, tx, p, now); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, `UPDATE billing_catalog_versions SET state='superseded' WHERE state='published'`); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE billing_catalog_versions SET state='published',published_by=?,published_at=?,updated_at=? WHERE id=?`, actor, now, now, id); err != nil {
			return err
		}
		before, _ := json.Marshal(current.Products)
		after, _ := json.Marshal(v.Products)
		if _, err = tx.ExecContext(ctx, `INSERT INTO billing_catalog_audit(version_id,action,actor,before_json,after_json,created_at) VALUES(?,'publish',?,?,?,?)`, id, actor, string(before), string(after), now); err != nil {
			return err
		}
		out, err = scan(tx.QueryRowContext(ctx, `SELECT `+columns+` FROM billing_catalog_versions WHERE id=?`, id))
		return err
	})
	return out, err
}
func synchronize(ctx context.Context, tx *sql.Tx, p Product, now int64) error {
	// A published price may change; an already sold purchase's grant cannot be rewritten.
	if p.Kind != "welcome" {
		var units int
		var apple string
		var sold int
		err := tx.QueryRowContext(ctx, `SELECT units,coalesce(apple_product_id,''),(SELECT count(*) FROM iap_purchases WHERE product_code=iap_products.code) FROM iap_products WHERE code=?`, p.Code).Scan(&units, &apple, &sold)
		if err != nil {
			return err
		}
		if (sold > 0 || apple != "") && (units != p.Units || apple != p.AppleProductID) {
			return domain.ConflictField("product", "sold_entitlement_immutable")
		}
		if p.Kind == "subscription" {
			var daily int
			var kind string
			if err = tx.QueryRowContext(ctx, `SELECT daily_message_limit,entitlement_kind FROM plans WHERE code=?`, p.Code).Scan(&daily, &kind); err != nil {
				return err
			}
			target := "daily"
			if p.Unlimited {
				target = "unlimited"
			}
			var subscribers int
			if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM subscriptions WHERE plan_id=(SELECT id FROM plans WHERE code=?)`, p.Code).Scan(&subscribers); err != nil {
				return err
			}
			if (sold > 0 || subscribers > 0 || apple != "") && (daily != p.DailyLimit || kind != target) {
				return domain.ConflictField("plan", "sold_entitlement_immutable")
			}
		}
	}

	if p.Kind == "welcome" {
		_, err := tx.ExecContext(ctx, `UPDATE system_settings SET value=?,updated_at=? WHERE key='welcome_bonus_units'`, fmt.Sprint(p.Units), now)
		return err
	}
	var planID any = ""
	if p.Kind == "subscription" {
		entitlement := "daily"
		if p.Unlimited {
			entitlement = "unlimited"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE plans SET price=?,currency=?,daily_message_limit=?,monthly_message_limit=0,entitlement_kind=?,is_active=?,is_visible=?,name_ru=?,name_en=?,name_kk=?,name_uz=?,updated_at=? WHERE code=?`, p.PriceMinor, p.Currency, p.DailyLimit, entitlement, p.Active, p.Active, p.Name["ru"], p.Name["en"], p.Name["kk"], p.Name["uz"], now, p.Code); err != nil {
			return err
		}
		var id string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM plans WHERE code=?`, p.Code).Scan(&id); err != nil {
			return err
		}
		planID = id
	}
	var apple any
	if p.AppleProductID != "" {
		apple = p.AppleProductID
	}
	_, err := tx.ExecContext(ctx, `UPDATE iap_products SET apple_product_id=?,units=?,plan_id=?,billing_period=?,price_minor=?,currency=?,is_active=?,name_ru=?,name_en=?,name_kk=?,updated_at=? WHERE code=? AND archived_at IS NULL`, apple, p.Units, planID, p.BillingPeriod, p.PriceMinor, p.Currency, p.Active && p.AvailableIOS && p.AppleProductID != "", p.Name["ru"], p.Name["en"], p.Name["kk"], now, p.Code)
	return err
}

// RegisterPublic exposes intended prices. Actual iOS checkout prices come from StoreKit.
func (s *Service) RegisterPublic(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/billing/catalog", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.Published(r.Context())
		if err != nil {
			httpx.Fail(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		products := []Product{}
		for _, p := range v.Products {
			if p.Active {
				products = append(products, p)
			}
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"version": v.ID, "currency": "KZT", "products": products, "store_prices_authoritative": true, "reset_timezone": "Asia/Almaty"})
	})
}

// ProductForWeb returns only server-published purchasable products and the version used.
func (s *Service) ProductForWeb(ctx context.Context, code string) (Product, int64, error) {
	v, err := s.Published(ctx)
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

// AuditEntry retains every saved draft and publication, including the before/after snapshots.
type AuditEntry struct {
	ID        int64     `json:"id"`
	VersionID int64     `json:"version_id"`
	Action    string    `json:"action"`
	Actor     string    `json:"actor"`
	Before    []Product `json:"before"`
	After     []Product `json:"after"`
	CreatedAt int64     `json:"created_at"`
}

func (s *Service) AuditHistory(ctx context.Context) ([]AuditEntry, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `SELECT id,version_id,action,actor,before_json,after_json,created_at FROM billing_catalog_audit ORDER BY id DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var v AuditEntry
		var before, after string
		if err = rows.Scan(&v.ID, &v.VersionID, &v.Action, &v.Actor, &before, &after, &v.CreatedAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(before), &v.Before); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(after), &v.After); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
