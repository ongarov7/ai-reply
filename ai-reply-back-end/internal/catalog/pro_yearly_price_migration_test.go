package catalog

import (
	"context"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/migrations"
)

func catalogMigrationsBefore(t *testing.T, before string) fstest.MapFS {
	t.Helper()
	out := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".sql") && entry.Name() < before {
			body, err := fs.ReadFile(migrations.FS, entry.Name())
			if err != nil {
				t.Fatal(err)
			}
			out[entry.Name()] = &fstest.MapFile{Data: body}
		}
	}
	return out
}

func annualPriceFixture(t *testing.T) (*database.DB, func(string, ...any)) {
	t.Helper()
	db, err := database.Open(database.Options{Path: filepath.Join(t.TempDir(), "annual-price.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := database.Migrate(context.Background(), db, catalogMigrationsBefore(t, "0030")); err != nil {
		t.Fatal(err)
	}
	return db, func(query string, args ...any) {
		t.Helper()
		if _, err := db.Writer().Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAnnualProPriceMigrationPreservesSalesHistoryAndDraft(t *testing.T) {
	ctx := context.Background()
	db, exec := annualPriceFixture(t)
	old := legacyDailyProducts()
	old[4].PriceMinor = 3450000
	for i := 1; i < len(old); i++ {
		old[i].AppleProductID = "test." + old[i].Code
	}
	old[2].AvailableWeb = false
	oldRaw, _ := json.Marshal(old)
	exec(`INSERT INTO billing_catalog_versions(id,state,products_json,created_by,updated_by,published_by,created_at,updated_at,published_at) VALUES(1,'published',?,'owner','owner','owner',1000,1000,1000)`, string(oldRaw))
	draftProducts := legacyDailyProducts()
	for i := range old {
		draftProducts[i] = old[i]
	}
	draftProducts[1].Name = map[string]string{"ru": "Название владельца", "en": "Owner draft", "kk": "Иесінің атауы", "uz": "Owner draft"}
	draftProducts[3].Active = false
	draftRaw, _ := json.Marshal(draftProducts)
	exec(`INSERT INTO billing_catalog_versions(id,state,base_version,revision,products_json,created_by,updated_by,created_at,updated_at) VALUES(2,'draft',1,4,?,'owner','owner',1000,1000)`, string(draftRaw))
	exec(`UPDATE iap_products SET apple_product_id='test.'||code,is_active=1 WHERE code IN ('base_monthly','base_yearly','pro_monthly','pro_yearly','pack30')`)
	exec(`UPDATE system_settings SET value='true' WHERE key='iap_enabled'`)
	exec(`UPDATE system_settings SET value='1234' WHERE key='welcome_bonus_since'`)
	exec(`INSERT INTO users(id,email,status,kind,created_at,updated_at) VALUES('buyer','buyer@example.test','active','account',1000,1000)`)
	exec(`INSERT INTO credit_grants(id,user_id,kind,units_total,units_remaining,starts_at,status,created_at,updated_at) VALUES('welcome','buyer','welcome',20,7,1000,'active',1000,1000),('pack','buyer','pack',30,18,1000,'active',1000,1000)`)
	exec(`INSERT INTO usage_daily(user_id,usage_date,used,updated_at) VALUES('buyer','2026-11-02',12,1000)`)
	orderRaw, _ := json.Marshal(old[4])
	exec(`INSERT INTO billing_orders(id,user_id,idempotency_key,catalog_version,product_code,snapshot_json,amount_minor,currency,provider,environment,bank_order,status,checkout_token_hash,request_json,created_at,updated_at) VALUES('old-order','buyer','original',1,'pro_yearly',?,3450000,'KZT','bcc','sandbox','original-bank-order','pending','token-hash','{}',1000,1000)`, string(orderRaw))
	exec(`INSERT INTO bcc_subscriptions(id,user_id,environment,root_order_id,plan_id,snapshot_json,amount_minor,period_days,status,mandate_expires_at,merch_rn_id,created_at,updated_at) SELECT 'old-mandate','buyer','sandbox','old-order',id,?,3450000,365,'active',9999999999999,'original-mandate',1000,1000 FROM plans WHERE code='pro_yearly'`, string(orderRaw))
	exec(`INSERT INTO iap_purchases(id,user_id,provider,environment,store_product_id,product_code,transaction_id,units,status,created_at,updated_at,price_milliunits,currency) VALUES('old-apple','buyer','app_store','Production','test.pro_yearly','pro_yearly','paid-original',0,'granted',1000,1000,34500000,'KZT')`)
	if applied, err := database.Migrate(ctx, db, catalogMigrationsBefore(t, "0031")); err != nil || len(applied) != 1 || applied[0] != "0030_pro_yearly_store_price.sql" {
		t.Fatalf("migration: %v %v", applied, err)
	}
	s := New(db)
	current, err := s.Published(ctx)
	if err != nil || current.ID == 1 || current.BaseVersion != 1 {
		t.Fatalf("publication: %+v %v", current, err)
	}
	want := append([]Product(nil), old...)
	want[4].PriceMinor = 3490000
	if !reflect.DeepEqual(current.Products, want) {
		t.Fatalf("publication changed other fields: %+v", current.Products)
	}
	draft, err := s.Draft(ctx)
	draftProducts[4].PriceMinor = 3490000
	if err != nil || draft.ID != 2 || draft.Revision != 5 || draft.BaseVersion != current.ID || !reflect.DeepEqual(draft.Products, draftProducts) {
		t.Fatalf("draft edits lost: %+v %v", draft, err)
	}
	var historical, order, mandate, currency, setting string
	if err := db.Reader().QueryRow(`SELECT products_json FROM billing_catalog_versions WHERE id=1 AND state='superseded'`).Scan(&historical); err != nil || historical != string(oldRaw) {
		t.Fatalf("history rewritten: %v", err)
	}
	var amount, units, remaining, count, used int64
	if err := db.Reader().QueryRow(`SELECT amount_minor,snapshot_json FROM billing_orders WHERE id='old-order'`).Scan(&amount, &order); err != nil || amount != 3450000 || order != string(orderRaw) {
		t.Fatalf("sold order rewritten: %v", err)
	}
	if err := db.Reader().QueryRow(`SELECT amount_minor,snapshot_json FROM bcc_subscriptions WHERE id='old-mandate'`).Scan(&amount, &mandate); err != nil || amount != 3450000 || mandate != string(orderRaw) {
		t.Fatalf("mandate rewritten: %v", err)
	}
	if err := db.Reader().QueryRow(`SELECT price_milliunits,currency FROM iap_purchases WHERE id='old-apple'`).Scan(&amount, &currency); err != nil || amount != 34500000 || currency != "KZT" {
		t.Fatalf("signed Apple amount rewritten: %v", err)
	}
	if err := db.Reader().QueryRow(`SELECT sum(units_total),sum(units_remaining),count(*) FROM credit_grants WHERE user_id='buyer'`).Scan(&units, &remaining, &count); err != nil || units != 50 || remaining != 25 || count != 2 {
		t.Fatalf("credits changed: %d %d %d %v", units, remaining, count, err)
	}
	if err := db.Reader().QueryRow(`SELECT used FROM usage_daily WHERE user_id='buyer'`).Scan(&used); err != nil || used != 12 {
		t.Fatalf("quota changed: %v", err)
	}
	if err := db.Reader().QueryRow(`SELECT value FROM system_settings WHERE key='welcome_bonus_since'`).Scan(&setting); err != nil || setting != "1234" {
		t.Fatalf("welcome cutoff changed: %v", err)
	}
	if err := db.Reader().QueryRow(`SELECT value FROM system_settings WHERE key='iap_enabled'`).Scan(&setting); err != nil || setting != "true" {
		t.Fatalf("sales switch changed: %v", err)
	}
	var sku, entitlement string
	var active int
	if err := db.Reader().QueryRow(`SELECT price_minor,apple_product_id,is_active FROM iap_products WHERE code='pro_yearly'`).Scan(&amount, &sku, &active); err != nil || amount != 3490000 || sku != "test.pro_yearly" || active != 1 {
		t.Fatalf("IAP offer: %d %s %d %v", amount, sku, active, err)
	}
	if err := db.Reader().QueryRow(`SELECT price,entitlement_kind,daily_message_limit FROM plans WHERE code='pro_yearly'`).Scan(&amount, &entitlement, &used); err != nil || amount != 3490000 || entitlement != "unlimited" || used != 0 {
		t.Fatalf("plan: %d %s %d %v", amount, entitlement, used, err)
	}
	audit, err := s.AuditHistory(ctx)
	if err != nil || len(audit) != 2 || audit[0].Action != "publish" || audit[1].Action != "rebase_price" || !reflect.DeepEqual(audit[1].After, draftProducts) {
		t.Fatalf("audit: %+v %v", audit, err)
	}
	if applied, err := database.Migrate(ctx, db, catalogMigrationsBefore(t, "0031")); err != nil || len(applied) != 0 {
		t.Fatalf("rerun: %v %v", applied, err)
	}
}

func TestAnnualProPriceMigrationAlreadyPublishedDoesNotRewriteHistory(t *testing.T) {
	ctx := context.Background()
	db, exec := annualPriceFixture(t)
	products := legacyDailyProducts()
	raw, _ := json.Marshal(products)
	exec(`INSERT INTO billing_catalog_versions(id,state,products_json,created_by,updated_by,published_by,created_at,updated_at,published_at) VALUES(1,'published',?,'owner','owner','owner',1000,1000,1000)`, string(raw))
	products[4].PriceMinor = 3450000
	draftRaw, _ := json.Marshal(products)
	exec(`INSERT INTO billing_catalog_versions(id,state,base_version,revision,products_json,created_by,updated_by,created_at,updated_at) VALUES(2,'draft',1,2,?,'owner','owner',1000,1000)`, string(draftRaw))
	if _, err := database.Migrate(ctx, db, catalogMigrationsBefore(t, "0031")); err != nil {
		t.Fatal(err)
	}
	s := New(db)
	current, err := s.Published(ctx)
	if err != nil || current.ID != 1 || current.UpdatedAt != 1000 || !reflect.DeepEqual(current.Products, legacyDailyProducts()) {
		t.Fatalf("already aligned publication changed: %+v %v", current, err)
	}
	draft, err := s.Draft(ctx)
	if err != nil || draft.BaseVersion != 1 || draft.Revision != 3 || draft.Products[4].PriceMinor != 3490000 {
		t.Fatalf("old draft price not aligned: %+v %v", draft, err)
	}
	history, _ := s.History(ctx)
	if len(history) != 2 {
		t.Fatalf("redundant publication: %+v", history)
	}
}
