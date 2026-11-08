package catalog

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/migrations"
)

// Historical migrations must be tested against the catalog they actually sold.
func legacyDailyProducts() []Product {
	p := InitialProducts()
	p[1].PriceMinor, p[1].DailyLimit, p[1].MonthlyLimit = 79000, 40, 0
	p[2].Active, p[2].AvailableIOS, p[2].AvailableWeb = true, true, true
	p[3].PriceMinor, p[4].PriceMinor = 345000, 3490000
	return p
}

func assertMonthlyBundleProducts(t *testing.T, products []Product) {
	t.Helper()
	want := InitialProducts()
	if !reflect.DeepEqual(products, want) {
		t.Fatalf("monthly catalog: %+v; want %+v", products, want)
	}
	if err := Validate(products); err != nil {
		t.Fatal(err)
	}
}

func TestMonthlyBundleMigrationPreservesBalancesHistoryAndDraft(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(database.Options{Path: filepath.Join(t.TempDir(), "monthly.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err = database.Migrate(ctx, db, catalogMigrationsBefore(t, "0037")); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Writer().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	old := legacyDailyProducts()
	for i := 1; i < len(old); i++ {
		old[i].AppleProductID = "test." + old[i].Code
	}
	raw, _ := json.Marshal(old)
	exec(`INSERT INTO billing_catalog_versions(id,state,products_json,created_by,updated_by,created_at,updated_at) VALUES(1,'published',?,'owner','owner',1000,1000)`, string(raw))
	draft := append([]Product(nil), old...)
	draft[1].Name = map[string]string{"ru": "Своё название", "en": "Owner title", "kk": "Өз атауы", "uz": "Owner title"}
	draft[5].PriceMinor = 17777
	draftRaw, _ := json.Marshal(draft)
	exec(`INSERT INTO billing_catalog_versions(id,state,base_version,revision,products_json,created_by,updated_by,created_at,updated_at) VALUES(2,'draft',1,4,?,'owner','owner',1000,1000)`, string(draftRaw))
	exec(`UPDATE iap_products SET apple_product_id='test.'||code,is_active=1 WHERE code<>'free'`)
	exec(`INSERT INTO users(id,email,status,kind,created_at,updated_at) VALUES('buyer','buyer@example.test','active','account',1000,1000)`)
	exec(`INSERT INTO credit_grants(id,user_id,kind,units_total,units_remaining,starts_at,status,created_at,updated_at) VALUES('paid','buyer','pack',30,17,1000,'active',1000,1000)`)
	order, _ := json.Marshal(old[1])
	exec(`INSERT INTO billing_orders(id,user_id,idempotency_key,catalog_version,product_code,snapshot_json,amount_minor,currency,provider,environment,bank_order,status,checkout_token_hash,request_json,created_at,updated_at) VALUES('old-order','buyer','old',1,'base_monthly',?,79000,'KZT','bcc','sandbox','old-bank','pending','hash','{}',1000,1000)`, string(order))
	exec(`INSERT INTO bcc_subscriptions(id,user_id,environment,root_order_id,plan_id,snapshot_json,amount_minor,period_days,status,mandate_expires_at,merch_rn_id,created_at,updated_at)
 SELECT 'legacy-bank','buyer','sandbox','old-order',id,?,79000,30,'active',9999999999999,'legacy-mandate',1000,1000 FROM plans WHERE code='base_monthly'`, string(order))
	if _, err = database.Migrate(ctx, db, migrations.FS); err != nil {
		t.Fatal(err)
	}
	s := New(db)
	current, err := s.Published(ctx)
	if err != nil || current.ID == 1 || current.BaseVersion != 1 {
		t.Fatalf("new publication: %+v %v", current, err)
	}
	want := InitialProducts()
	for i := 1; i < len(want); i++ {
		want[i].AppleProductID = old[i].AppleProductID
	}
	if !reflect.DeepEqual(current.Products, want) {
		t.Fatalf("unexpected fields: %+v", current.Products)
	}
	updated, err := s.Draft(ctx)
	want[1].Name = draft[1].Name
	want[5].PriceMinor = 17777
	if err != nil || updated.Revision != 5 || updated.BaseVersion != current.ID || !reflect.DeepEqual(updated.Products, want) {
		t.Fatalf("draft lost: %+v %v", updated, err)
	}
	var historical, sold string
	var amount, left int
	if err = db.Reader().QueryRow(`SELECT products_json FROM billing_catalog_versions WHERE id=1 AND state='superseded'`).Scan(&historical); err != nil || historical != string(raw) {
		t.Fatal("history rewritten", err)
	}
	if err = db.Reader().QueryRow(`SELECT snapshot_json,amount_minor FROM billing_orders WHERE id='old-order'`).Scan(&sold, &amount); err != nil || sold != string(order) || amount != 79000 {
		t.Fatal("sold order rewritten", err)
	}
	if err = db.Reader().QueryRow(`SELECT units_remaining FROM credit_grants WHERE id='paid'`).Scan(&left); err != nil || left != 17 {
		t.Fatal("balance changed", err)
	}
	var planID string
	if err = db.Reader().QueryRow(`SELECT plan_id FROM bcc_subscriptions WHERE id='legacy-bank'`).Scan(&planID); err != nil {
		t.Fatal(err)
	}
	legacy, err := repository.New(db).BankSubscriptionPlan(ctx, "legacy-bank", planID)
	if err != nil || legacy.Price != 79000 || legacy.DailyLimit != 40 || legacy.MonthlyLimit != 0 || legacy.EntitlementKind != "daily" {
		t.Fatalf("legacy mandate was reinterpreted as the new bundle: %+v %v", legacy, err)
	}
	if _, err = s.Publish(ctx, "owner", updated.ID, updated.Revision); err != nil {
		t.Fatalf("aligned draft cannot publish: %v", err)
	}
	if applied, err := database.Migrate(ctx, db, migrations.FS); err != nil || len(applied) != 0 {
		t.Fatalf("reapplied: %v %v", applied, err)
	}
}

func TestMonthlyBundleMigrationAlreadyAlignedDoesNotRepublish(t *testing.T) {
	db, err := database.Open(database.Options{Path: filepath.Join(t.TempDir(), "aligned.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err = database.Migrate(ctx, db, catalogMigrationsBefore(t, "0037")); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(InitialProducts())
	if _, err = db.Writer().Exec(`INSERT INTO billing_catalog_versions(id,state,products_json,created_by,updated_by,created_at,updated_at) VALUES(1,'published',?,'owner','owner',1000,1000)`, string(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err = database.Migrate(ctx, db, migrations.FS); err != nil {
		t.Fatal(err)
	}
	v, err := New(db).Published(ctx)
	if err != nil || v.ID != 1 || v.UpdatedAt != 1000 {
		t.Fatalf("redundant publication: %+v %v", v, err)
	}
}
