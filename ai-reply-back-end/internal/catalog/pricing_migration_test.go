package catalog

import (
	"context"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/migrations"
)

func assertUnifiedProducts(t *testing.T, products []Product) {
	t.Helper()
	prices := map[string]int64{"free": 0, "base_monthly": 79000, "base_yearly": 790000, "pro_monthly": 345000, "pro_yearly": 3450000, "pack30": 13000}
	if len(products) != len(prices) {
		t.Fatalf("products: %+v", products)
	}
	for _, product := range products {
		price, ok := prices[product.Code]
		if !ok || product.PriceMinor != price || product.Currency != "KZT" {
			t.Fatalf("price: %+v", product)
		}
		switch product.Kind {
		case "welcome":
			if product.Units != 10 || product.DailyLimit != 0 || product.Unlimited {
				t.Fatalf("welcome: %+v", product)
			}
		case "pack":
			if product.Units != 30 || product.DailyLimit != 0 || product.Unlimited {
				t.Fatalf("pack: %+v", product)
			}
		case "subscription":
			if strings.HasPrefix(product.Code, "base_") && (product.DailyLimit != 40 || product.Unlimited) ||
				strings.HasPrefix(product.Code, "pro_") && (!product.Unlimited || product.DailyLimit != 0) {
				t.Fatalf("subscription: %+v", product)
			}
		}
	}
}

func TestUnifiedPricesOnFreshInstall(t *testing.T) {
	s, db := testCatalog(t)
	ctx := context.Background()
	version, err := s.Published(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertUnifiedProducts(t, version.Products)
	for _, product := range version.Products {
		if product.Kind == "welcome" {
			continue
		}
		var price int64
		if err := db.Reader().QueryRow(`SELECT price_minor FROM iap_products WHERE code=?`, product.Code).Scan(&price); err != nil || price != product.PriceMinor {
			t.Fatalf("store business price %s: %d %v", product.Code, price, err)
		}
		if product.Kind == "subscription" {
			var daily int
			if err := db.Reader().QueryRow(`SELECT price,daily_message_limit FROM plans WHERE code=?`, product.Code).Scan(&price, &daily); err != nil || price != product.PriceMinor || daily != product.DailyLimit {
				t.Fatalf("plan %s: %d %d %v", product.Code, price, daily, err)
			}
		}
	}
}

func TestUnifiedPricesUpgradePreservesHistoryAndSoldPurchases(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(database.Options{Path: filepath.Join(t.TempDir(), "upgrade.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	before := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".sql") && entry.Name() < "0029" {
			body, err := fs.ReadFile(migrations.FS, entry.Name())
			if err != nil {
				t.Fatal(err)
			}
			before[entry.Name()] = &fstest.MapFile{Data: body}
		}
	}
	if _, err := database.Migrate(ctx, db, before); err != nil {
		t.Fatal(err)
	}
	oldProducts := InitialProducts()
	oldPrices := []int64{0, 59900, 599900, 299900, 2999000, 10000}
	for i := range oldProducts {
		oldProducts[i].PriceMinor = oldPrices[i]
		if oldProducts[i].DailyLimit > 0 {
			oldProducts[i].DailyLimit = 30
		}
		if i > 0 {
			oldProducts[i].AppleProductID = "test." + oldProducts[i].Code
		}
	}
	oldProducts[2].AvailableWeb = false
	raw, _ := json.Marshal(oldProducts)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Writer().ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO billing_catalog_versions(id,state,products_json,created_by,updated_by,published_by,created_at,updated_at,published_at) VALUES(1,'published',?,'owner','owner','owner',1000,1000,1000)`, string(raw))
	exec(`INSERT INTO billing_catalog_versions(id,state,base_version,products_json,created_by,updated_by,created_at,updated_at) VALUES(2,'draft',1,?,'owner','owner',1000,1000)`, string(raw))
	exec(`UPDATE iap_products SET apple_product_id='test.'||code,is_active=1 WHERE code IN ('base_monthly','base_yearly','pro_monthly','pro_yearly','pack30')`)
	exec(`UPDATE system_settings SET value='1234' WHERE key='welcome_bonus_since'`)
	exec(`UPDATE plans SET daily_message_limit=7,entitlement_kind='daily' WHERE code='free'`)
	exec(`INSERT INTO users(id,email,status,kind,created_at,updated_at) VALUES('existing','existing@example.test','active','account',1000,1000)`)
	exec(`INSERT INTO credit_grants(id,user_id,kind,units_total,units_remaining,starts_at,status,created_at,updated_at) VALUES('old-welcome','existing','welcome',20,7,1000,'active',1000,1000)`)
	exec(`INSERT INTO usage_daily(user_id,usage_date,used,updated_at) VALUES('existing','2026-11-02',12,1000)`)
	orderSnapshot, _ := json.Marshal(oldProducts[1])
	exec(`INSERT INTO billing_orders(id,user_id,idempotency_key,catalog_version,product_code,snapshot_json,amount_minor,currency,provider,environment,bank_order,status,checkout_token_hash,request_json,created_at,updated_at) VALUES('pending','existing','original',1,'base_monthly',?,59900,'KZT','bcc','sandbox','original-bank-order','pending','token-hash','{}',1000,1000)`, string(orderSnapshot))
	exec(`INSERT INTO bcc_subscriptions(id,user_id,environment,root_order_id,plan_id,snapshot_json,amount_minor,period_days,status,mandate_expires_at,merch_rn_id,created_at,updated_at) SELECT 'old-contract','existing','sandbox','pending',id,?,59900,30,'active',9999999999999,'original-mandate',1000,1000 FROM plans WHERE code='base_monthly'`, string(orderSnapshot))
	if applied, err := database.Migrate(ctx, db, migrations.FS); err != nil || len(applied) != 1 || applied[0] != "0029_unified_pricing.sql" {
		t.Fatalf("upgrade: %v %v", applied, err)
	}
	s := New(db)
	current, err := s.Published(ctx)
	if err != nil || current.BaseVersion != 1 || current.ID == 1 {
		t.Fatalf("new version: %+v %v", current, err)
	}
	assertUnifiedProducts(t, current.Products)
	if current.Products[2].AvailableWeb || current.Products[1].AppleProductID != "test.base_monthly" {
		t.Fatal("store mapping or availability changed")
	}
	var original, cutoff, pending, contract string
	if err := db.Reader().QueryRow(`SELECT products_json FROM billing_catalog_versions WHERE id=1 AND state='superseded'`).Scan(&original); err != nil || original != string(raw) {
		t.Fatalf("catalog history changed: %v", err)
	}
	if err := db.Reader().QueryRow(`SELECT value FROM system_settings WHERE key='welcome_bonus_since'`).Scan(&cutoff); err != nil || cutoff != "1234" {
		t.Fatalf("welcome cutoff changed: %s %v", cutoff, err)
	}
	var amount, units, remaining, used, active int
	if err := db.Reader().QueryRow(`SELECT snapshot_json,amount_minor FROM billing_orders WHERE id='pending'`).Scan(&pending, &amount); err != nil || pending != string(orderSnapshot) || amount != 59900 {
		t.Fatalf("pending order changed: %v", err)
	}
	if err := db.Reader().QueryRow(`SELECT snapshot_json,amount_minor FROM bcc_subscriptions WHERE id='old-contract'`).Scan(&contract, &amount); err != nil || contract != string(orderSnapshot) || amount != 59900 {
		t.Fatalf("bank mandate changed: %v", err)
	}
	if err := db.Reader().QueryRow(`SELECT units_total,units_remaining FROM credit_grants WHERE id='old-welcome'`).Scan(&units, &remaining); err != nil || units != 20 || remaining != 7 {
		t.Fatalf("welcome grant changed: %v", err)
	}
	var grants, daily int
	var entitlement string
	if err := db.Reader().QueryRow(`SELECT count(*) FROM credit_grants WHERE user_id='existing'`).Scan(&grants); err != nil || grants != 1 {
		t.Fatalf("welcome bonus regranted: %d %v", grants, err)
	}
	if err := db.Reader().QueryRow(`SELECT daily_message_limit,entitlement_kind FROM plans WHERE code='free'`).Scan(&daily, &entitlement); err != nil || daily != 0 || entitlement != "welcome" {
		t.Fatalf("free still renews: %d %s %v", daily, entitlement, err)
	}
	if err := db.Reader().QueryRow(`SELECT used FROM usage_daily WHERE user_id='existing'`).Scan(&used); err != nil || used != 12 {
		t.Fatalf("usage changed: %v", err)
	}
	if err := db.Reader().QueryRow(`SELECT is_active FROM iap_products WHERE code='base_monthly'`).Scan(&active); err != nil || active != 1 {
		t.Fatalf("store availability changed: %v", err)
	}
	if _, err := s.Draft(ctx); err == nil {
		t.Fatal("obsolete draft can restore old prices")
	}
	if _, err := s.SaveDraft(ctx, "owner", 0, current.Products); err != nil {
		t.Fatalf("new draft blocked after migration: %v", err)
	}
	if applied, err := database.Migrate(ctx, db, migrations.FS); err != nil || len(applied) != 0 {
		t.Fatalf("rerun: %v %v", applied, err)
	}
}

func TestUnifiedPricesCustomizedPackKeepsDelayedStoreFulfillment(t *testing.T) {
	for _, provider := range []string{"unmapped", domain.ProviderAppStore, domain.ProviderGooglePlay} {
		t.Run(provider, func(t *testing.T) {
			ctx := context.Background()
			db, err := database.Open(database.Options{Path: filepath.Join(t.TempDir(), "customized.sqlite")})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			before := fstest.MapFS{}
			entries, _ := fs.ReadDir(migrations.FS, ".")
			for _, entry := range entries {
				if strings.HasSuffix(entry.Name(), ".sql") && entry.Name() < "0029" {
					body, err := fs.ReadFile(migrations.FS, entry.Name())
					if err != nil {
						t.Fatal(err)
					}
					before[entry.Name()] = &fstest.MapFile{Data: body}
				}
			}
			if _, err := database.Migrate(ctx, db, before); err != nil {
				t.Fatal(err)
			}
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := db.Writer().ExecContext(ctx, query, args...); err != nil {
					t.Fatal(err)
				}
			}
			products := InitialProducts()
			products[5].Units = 60
			if provider == domain.ProviderAppStore {
				products[5].AppleProductID = "test.legacy60"
				exec(`UPDATE iap_products SET apple_product_id='test.legacy60',is_active=1 WHERE code='pack30'`)
			} else if provider == domain.ProviderGooglePlay {
				exec(`UPDATE iap_products SET google_product_id='test.legacy60',is_active=1 WHERE code='pack30'`)
			}
			exec(`UPDATE iap_products SET units=60 WHERE code='pack30'`)
			raw, _ := json.Marshal(products)
			exec(`INSERT INTO billing_catalog_versions(state,products_json,created_by,updated_by,published_by,created_at,updated_at,published_at) VALUES('published',?,'owner','owner','owner',1000,1000,1000)`, string(raw))
			if provider != "unmapped" {
				exec(`INSERT INTO users(id,email,status,kind,created_at,updated_at) VALUES('buyer','buyer@example.test','active','account',1000,1000)`)
				exec(`INSERT INTO iap_purchases(id,user_id,provider,environment,store_product_id,product_code,transaction_id,units,status,created_at,updated_at) VALUES('delayed','buyer',?,'Production','test.legacy60','pack30','legacy-tx',60,'pending',1000,1000)`, provider)
			}
			if _, err := database.Migrate(ctx, db, migrations.FS); err != nil {
				t.Fatal(err)
			}
			var units, active int
			var apple string
			if err := db.Reader().QueryRow(`SELECT units,is_active,coalesce(apple_product_id,'') FROM iap_products WHERE code='pack30'`).Scan(&units, &active, &apple); err != nil || units != 30 || apple != "" {
				t.Fatalf("new store offer: %d %d %q %v", units, active, apple, err)
			}
			s := New(db)
			current, err := s.Published(ctx)
			if err != nil {
				t.Fatal(err)
			}
			assertUnifiedProducts(t, current.Products)
			if provider == "unmapped" {
				return
			}
			if active != 0 || current.Products[5].AppleProductID != "" || current.Products[5].AvailableIOS {
				t.Fatal("replacement offer reused a legacy store identity")
			}
			repo := repository.New(db)
			legacy, err := repo.IAPProductByStoreID(ctx, provider, "test.legacy60")
			if err != nil || legacy.Units != 60 || legacy.IsActive || legacy.ArchivedAt == nil {
				t.Fatalf("legacy store entitlement: %+v %v", legacy, err)
			}
			// A new store ID remains mappable even though a pending purchase has
			// the old business code; store identity defines that purchase's grant.
			current.Products[5].AppleProductID = "test.new30"
			current.Products[5].AvailableIOS = true
			draft, err := s.SaveDraft(ctx, "owner", 0, current.Products)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.Publish(ctx, "owner", draft.ID, draft.Revision); err != nil {
				t.Fatalf("replacement mapping blocked by old purchase: %v", err)
			}
			result, err := repo.GrantPurchase(ctx, repository.PurchaseGrant{
				Purchase: domain.IAPPurchase{UserID: "buyer", Provider: provider, Environment: "Production", StoreProductID: "test.legacy60", ProductCode: legacy.Code, TransactionID: "legacy-tx", Quantity: 1, Units: legacy.Units},
				Kind:     legacy.Kind, Now: time.Now(),
			})
			if err != nil || !result.Granted || result.Purchase.Units != 60 {
				t.Fatalf("delayed fulfillment changed: %+v %v", result, err)
			}
		})
	}
}
