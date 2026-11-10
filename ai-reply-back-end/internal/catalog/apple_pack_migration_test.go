package catalog

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/migrations"
)

func TestApplePackMigrationPreservesCustomersOtherStoresAndDraftEdits(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(database.Options{Path: filepath.Join(t.TempDir(), "apple-pack.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err = database.Migrate(ctx, db, catalogMigrationsBefore(t, "0039")); err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Writer().Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	old := preApplePackProducts()
	for i := 1; i < len(old); i++ {
		old[i].AppleProductID = "test." + old[i].Code
	}
	old[1].GoogleProductID, old[1].AvailableAndroid = "base_month", true
	old[5].GoogleProductID, old[5].AvailableAndroid = "pack_30", true
	raw, _ := json.Marshal(old)
	exec(`INSERT INTO billing_catalog_versions(id,state,products_json,created_by,updated_by,created_at,updated_at) VALUES(1,'published',?,'owner','owner',1000,1000)`, string(raw))
	draft := append([]Product(nil), old...)
	draft[5].PriceMinor = 17777
	draft[1].Name = map[string]string{"ru": "Название владельца", "en": "Owner title", "kk": "Иесінің атауы", "uz": "Owner title"}
	draftRaw, _ := json.Marshal(draft)
	exec(`INSERT INTO billing_catalog_versions(id,state,base_version,revision,products_json,created_by,updated_by,created_at,updated_at) VALUES(2,'draft',1,4,?,'owner','owner',1000,1000)`, string(draftRaw))
	exec(`UPDATE iap_products SET apple_product_id='test.'||code,is_active=1`)
	exec(`UPDATE iap_products SET google_product_id='base_month' WHERE code='base_monthly'`)
	exec(`UPDATE iap_products SET google_product_id='pack_30' WHERE code='pack30'`)
	exec(`INSERT INTO users(id,email,status,kind,created_at,updated_at) VALUES('buyer','buyer@example.test','active','account',1000,1000)`)
	exec(`INSERT INTO credit_grants(id,user_id,kind,units_total,units_remaining,starts_at,status,created_at,updated_at) VALUES('paid','buyer','pack',30,17,1000,'active',1000,1000)`)
	order, _ := json.Marshal(old[1])
	exec(`INSERT INTO billing_orders(id,user_id,idempotency_key,catalog_version,product_code,snapshot_json,amount_minor,currency,provider,environment,bank_order,status,checkout_token_hash,request_json,created_at,updated_at) VALUES('sold','buyer','old',1,'base_monthly',?,99000,'KZT','bcc','sandbox','old-bank','pending','hash','{}',1000,1000)`, string(order))
	if applied, err := database.Migrate(ctx, db, migrations.FS); err != nil || len(applied) != 1 || applied[0] != "0039_apple_reply_pack_500.sql" {
		t.Fatalf("migration: %v %v", applied, err)
	}
	s := New(db)
	current, err := s.Published(ctx)
	want := append([]Product(nil), old...)
	want[1].AvailableIOS, want[2].AvailableIOS = false, false
	want = append(want, InitialProducts()[6])
	if err != nil || current.BaseVersion != 1 || !reflect.DeepEqual(current.Products, want) {
		t.Fatalf("publication: %+v %v", current, err)
	}
	want[1].Name, want[5].PriceMinor = draft[1].Name, draft[5].PriceMinor
	pending, err := s.Draft(ctx)
	if err != nil || pending.Revision != 5 || pending.BaseVersion != current.ID || !reflect.DeepEqual(pending.Products, want) {
		t.Fatalf("draft lost: %+v %v", pending, err)
	}
	var historical, sold string
	var amount, remaining int
	if err = db.Reader().QueryRow(`SELECT products_json FROM billing_catalog_versions WHERE id=1 AND state='superseded'`).Scan(&historical); err != nil || historical != string(raw) {
		t.Fatalf("history rewritten: %v", err)
	}
	if err = db.Reader().QueryRow(`SELECT snapshot_json,amount_minor FROM billing_orders WHERE id='sold'`).Scan(&sold, &amount); err != nil || sold != string(order) || amount != 99000 {
		t.Fatalf("sold order rewritten: %v", err)
	}
	if err = db.Reader().QueryRow(`SELECT units_remaining FROM credit_grants WHERE id='paid' AND expires_at IS NULL`).Scan(&remaining); err != nil || remaining != 17 {
		t.Fatalf("balance changed: %v", err)
	}
	repo := repository.New(db)
	base, err := repo.IAPProductByStoreID(ctx, domain.ProviderAppStore, "test.base_monthly")
	if err != nil || base.Offered(domain.PlatformIOS) || !base.Offered(domain.PlatformAndroid) {
		t.Fatalf("retirement affected another store or erased the identity: %+v %v", base, err)
	}
	pack, err := repo.IAPProductByStoreID(ctx, domain.ProviderAppStore, "kz.aireply.pack500")
	if err != nil || pack.Units != 500 || pack.PriceMinor != 99000 || !pack.Offered(domain.PlatformIOS) || pack.Offered(domain.PlatformAndroid) || pack.DurationHours != 0 {
		t.Fatalf("new pack: %+v %v", pack, err)
	}
	if _, err = s.Publish(ctx, "owner", pending.ID, pending.Revision); err != nil {
		t.Fatalf("rebased draft cannot publish: %v", err)
	}
	base, _ = repo.IAPProductByStoreID(ctx, domain.ProviderAppStore, "test.base_monthly")
	if base.Offered(domain.PlatformIOS) || !base.Offered(domain.PlatformAndroid) {
		t.Fatal("publishing a rebased draft reopened Apple Base or closed Android")
	}
	if applied, err := database.Migrate(ctx, db, migrations.FS); err != nil || len(applied) != 0 {
		t.Fatalf("reapplied: %v %v", applied, err)
	}
}

func TestApplePackMigrationRollsBackOnConflictingProductIdentity(t *testing.T) {
	db, err := database.Open(database.Options{Path: filepath.Join(t.TempDir(), "conflict.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err = database.Migrate(ctx, db, catalogMigrationsBefore(t, "0039")); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Writer().Exec(`UPDATE iap_products SET apple_product_id='kz.aireply.pack500' WHERE code='pack30'`); err != nil {
		t.Fatal(err)
	}
	if _, err = database.Migrate(ctx, db, migrations.FS); err == nil {
		t.Fatal("conflicting durable store identity was silently reused")
	}
	var count, off int
	if err = db.Reader().QueryRow(`SELECT (SELECT count(*) FROM iap_products WHERE code='pack500'),off_ios FROM iap_products WHERE code='base_monthly'`).Scan(&count, &off); err != nil || count != 0 || off != 0 {
		t.Fatalf("partial migration survived rollback: %d %d %v", count, off, err)
	}
}

func adoptionFixture(t *testing.T) (*database.DB, *repository.Store, domain.IAPProduct) {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(database.Options{Path: filepath.Join(t.TempDir(), "adoption.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err = database.Migrate(ctx, db, catalogMigrationsBefore(t, "0039")); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(preApplePackProducts())
	if _, err = db.Writer().Exec(`INSERT INTO billing_catalog_versions(id,state,products_json,created_by,updated_by,created_at,updated_at) VALUES(1,'published',?,'owner','owner',1000,1000)`, string(raw)); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	p := InitialProducts()[6]
	// This is exactly what the existing generic admin API persists. That API
	// generates a new UUID and has no off_android field (the DB default is 0).
	created, err := repo.CreateIAPProduct(ctx, domain.IAPProduct{
		Code: p.Code, Kind: p.Kind, Units: p.Units, AppleProductID: p.AppleProductID,
		PriceMinor: p.PriceMinor, Currency: p.Currency, Name: p.Name, IsActive: true, SortOrder: 50,
	}, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	return db, repo, created
}

func TestApplePackMigrationAdoptsAdminUUIDAndPreservesSoldReplay(t *testing.T) {
	for _, explicitAndroidOff := range []bool{false, true} {
		t.Run(map[bool]string{false: "old admin default", true: "explicit Android off"}[explicitAndroidOff], func(t *testing.T) {
			db, repo, before := adoptionFixture(t)
			ctx := context.Background()
			if explicitAndroidOff {
				if _, err := db.Writer().Exec(`UPDATE iap_products SET off_android=1 WHERE id=?`, before.ID); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.Writer().Exec(`INSERT INTO users(id,email,status,kind,created_at,updated_at) VALUES('buyer','buyer@example.test','active','account',1000,1000)`); err != nil {
				t.Fatal(err)
			}
			paid := repository.PurchaseGrant{Purchase: domain.IAPPurchase{
				UserID: "buyer", Provider: domain.ProviderAppStore, Environment: "Production",
				StoreProductID: before.AppleProductID, ProductCode: before.Code, TransactionID: "adopted-paid",
				Quantity: 1, Units: 500, PriceMilliunits: 990000, Currency: "KZT",
			}, Kind: domain.ProductPack, Now: time.Unix(100, 0)}
			first, err := repo.GrantPurchase(ctx, paid)
			if err != nil || !first.Granted {
				t.Fatalf("pre-migration purchase: %+v %v", first, err)
			}
			if applied, err := database.Migrate(ctx, db, migrations.FS); err != nil || !reflect.DeepEqual(applied, []string{"0039_apple_reply_pack_500.sql"}) {
				t.Fatalf("adoption migration: %v %v", applied, err)
			}
			after, err := repo.IAPProductByStoreID(ctx, domain.ProviderAppStore, before.AppleProductID)
			if err != nil || after.ID != before.ID || !after.CreatedAt.Equal(before.CreatedAt) || after.Units != 500 || after.PriceMinor != 99000 || after.Currency != "KZT" || !after.Offered(domain.PlatformIOS) || after.Offered(domain.PlatformAndroid) {
				t.Fatalf("canonical product was replaced or changed: %+v %v", after, err)
			}
			paid.Now = time.Unix(200, 0)
			replay, err := repo.GrantPurchase(ctx, paid)
			if err != nil || replay.Granted || !reflect.DeepEqual(replay.Purchase, first.Purchase) {
				t.Fatalf("sold replay changed: %+v %v", replay, err)
			}
			var products, grants, remaining, appliedCount int
			if err := db.Reader().QueryRow(`SELECT
 (SELECT count(*) FROM iap_products WHERE code='pack500'),
 (SELECT count(*) FROM credit_grants WHERE product_code='pack500' AND expires_at IS NULL),
 (SELECT sum(units_remaining) FROM credit_grants WHERE product_code='pack500'),
 (SELECT count(*) FROM schema_migrations WHERE name='0039_apple_reply_pack_500.sql')`).Scan(&products, &grants, &remaining, &appliedCount); err != nil || products != 1 || grants != 1 || remaining != 500 || appliedCount != 1 {
				t.Fatalf("adoption duplicated product/grant or lost migration version: %d %d %d %d %v", products, grants, remaining, appliedCount, err)
			}
			current, err := New(db).Published(ctx)
			if err != nil || current.BaseVersion != 1 || current.CreatedBy != "migration:0039" || len(current.Products) != 7 || current.Products[6].AppleProductID != before.AppleProductID {
				t.Fatalf("adopted catalog publication: %+v %v", current, err)
			}
			if applied, err := database.Migrate(ctx, db, migrations.FS); err != nil || len(applied) != 0 {
				t.Fatalf("adoption reapplied: %v %v", applied, err)
			}
		})
	}
}

func TestApplePackMigrationRejectsConflictingAdoptionTerms(t *testing.T) {
	for name, change := range map[string]string{
		"units": "units=499", "price": "price_minor=99001", "currency": "currency='USD'",
		"kind": "kind='pass'", "duration": "duration_hours=24", "plan": "plan_id='other-plan'",
		"period": "billing_period='month'", "store ID": "apple_product_id='other.sku'",
		"missing store ID": "apple_product_id=NULL", "code": "code='other_pack'",
		"inactive": "is_active=0", "Apple off": "off_ios=1", "Google mapping": "google_product_id='google_pack'",
		"archived": "archived_at=1234",
	} {
		t.Run(name, func(t *testing.T) {
			db, repo, product := adoptionFixture(t)
			if _, err := db.Writer().Exec(`UPDATE iap_products SET `+change+` WHERE id=?`, product.ID); err != nil {
				t.Fatal(err)
			}
			before, _ := repo.IAPProduct(context.Background(), product.ID)
			if _, err := database.Migrate(context.Background(), db, migrations.FS); err == nil {
				t.Fatal("conflicting product was adopted")
			}
			after, err := repo.IAPProduct(context.Background(), product.ID)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("failed adoption changed product: %+v %v", after, err)
			}
			var appliedCount, off, versions int
			if err := db.Reader().QueryRow(`SELECT (SELECT count(*) FROM schema_migrations WHERE name='0039_apple_reply_pack_500.sql'),off_ios,(SELECT count(*) FROM billing_catalog_versions) FROM iap_products WHERE code='base_monthly'`).Scan(&appliedCount, &off, &versions); err != nil || appliedCount != 0 || off != 0 || versions != 1 {
				t.Fatalf("partial adoption committed: %d %d %d %v", appliedCount, off, versions, err)
			}
		})
	}
}

func TestApplePackMigrationRejectsConflictingCatalogTerms(t *testing.T) {
	for _, field := range []string{"units", "available_web"} {
		t.Run(field, func(t *testing.T) {
			db, _, _ := adoptionFixture(t)
			products := InitialProducts()
			if field == "units" {
				products[6].Units = 499
			} else {
				products[6].AvailableWeb = true
			}
			raw, _ := json.Marshal(products)
			if _, err := db.Writer().Exec(`UPDATE billing_catalog_versions SET products_json=? WHERE id=1`, string(raw)); err != nil {
				t.Fatal(err)
			}
			if _, err := database.Migrate(context.Background(), db, migrations.FS); err == nil {
				t.Fatal("conflicting catalog terms were accepted")
			}
			var stored string
			if err := db.Reader().QueryRow(`SELECT products_json FROM billing_catalog_versions WHERE id=1 AND state='published'`).Scan(&stored); err != nil || stored != string(raw) {
				t.Fatalf("failed adoption changed catalog: %v", err)
			}
		})
	}
}
