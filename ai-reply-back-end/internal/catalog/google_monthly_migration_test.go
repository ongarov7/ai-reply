package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/migrations"
)

func googleMonthlyFixture(t *testing.T) (*database.DB, func(string, ...any)) {
	t.Helper()
	db, err := database.Open(database.Options{Path: filepath.Join(t.TempDir(), "monthly-990.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err = database.Migrate(context.Background(), db, catalogMigrationsBefore(t, "0044")); err != nil {
		t.Fatal(err)
	}
	return db, func(q string, args ...any) {
		t.Helper()
		if _, err := db.Writer().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGoogleMonthlyMigrationPreservesPaidHistoryAndRebasesDraft(t *testing.T) {
	for _, stale := range []bool{false, true} {
		t.Run(map[bool]string{false: "current draft", true: "stale draft"}[stale], func(t *testing.T) {
			ctx := context.Background()
			db, exec := googleMonthlyFixture(t)
			old := InitialProducts()
			old[0].Units = 20
			old[3].GoogleProductID, old[3].AvailableAndroid = "kz.aireply.pro.monthly", true
			old[6].GoogleProductID, old[6].AvailableAndroid = "kz.aireply.pack500", true
			raw, _ := json.Marshal(old)
			exec(`INSERT INTO billing_catalog_versions(id,state,products_json,created_by,updated_by,created_at,updated_at) VALUES(1,'superseded',?,'owner','owner',1000,1000),(2,'published',?,'owner','owner',1000,1000)`, string(raw), string(raw))
			draft := append([]Product(nil), old...)
			draft[5].PriceMinor = 17777
			draw, _ := json.Marshal(draft)
			base := 2
			if stale {
				base = 1
			}
			exec(`INSERT INTO billing_catalog_versions(id,state,base_version,revision,products_json,created_by,updated_by,created_at,updated_at) VALUES(3,'draft',?,4,?,'owner','owner',1000,1000)`, base, string(draw))
			exec(`UPDATE system_settings SET value='1234' WHERE key='welcome_bonus_since'`)
			exec(`UPDATE iap_products SET google_product_id='kz.aireply.pack500',off_android=0 WHERE code='pack500'`)
			exec(`UPDATE iap_products SET google_product_id='kz.aireply.pro.monthly',off_android=0,is_active=1 WHERE code='pro_monthly'`)
			exec(`INSERT INTO users(id,email,status,kind,created_at,updated_at) VALUES('buyer','buyer@example.test','active','account',2000,2000)`)
			exec(`INSERT INTO iap_purchases(id,user_id,provider,environment,store_product_id,product_code,transaction_id,units,status,created_at,updated_at) VALUES('paid','buyer','google_play','Production','kz.aireply.pack500','pack500','original',500,'granted',1000,1000)`)
			exec(`INSERT INTO credit_grants(id,user_id,kind,units_total,units_remaining,starts_at,status,iap_purchase_id,created_at,updated_at) VALUES('pack','buyer','pack',500,417,1000,'active','paid',1000,1000),('welcome','buyer','welcome',20,7,1000,'active',NULL,1000,1000)`)
			sold, _ := json.Marshal(old[3])
			exec(`INSERT INTO billing_orders(id,user_id,idempotency_key,catalog_version,product_code,snapshot_json,amount_minor,currency,provider,environment,bank_order,status,checkout_token_hash,request_json,created_at,updated_at) VALUES('bank','buyer','before',2,'pro_monthly',?,399000,'KZT','bcc','sandbox','old-bank','succeeded','hash','{}',1000,1000)`, string(sold))
			if applied, err := database.Migrate(ctx, db, migrations.FS); err != nil || !reflect.DeepEqual(applied, []string{"0044_google_monthly_free_ten.sql"}) {
				t.Fatalf("migration: %v %v", applied, err)
			}
			s := New(db)
			published, err := s.Published(ctx)
			want := append([]Product(nil), old...)
			want[0].Units = 10
			want[6].AvailableAndroid = false
			if err != nil || published.BaseVersion != 2 || !reflect.DeepEqual(published.Products, want) {
				t.Fatalf("publication: %+v %v", published, err)
			}
			pending, err := s.Draft(ctx)
			want[5].PriceMinor = 17777
			wantBase := int64(base)
			if !stale {
				wantBase = published.ID
			}
			if err != nil || pending.Revision != 5 || pending.BaseVersion != wantBase || !reflect.DeepEqual(pending.Products, want) {
				t.Fatalf("draft: %+v %v", pending, err)
			}
			for key, want := range map[string]string{"welcome_bonus_units": "10", "welcome_bonus_since": "1234"} {
				var got string
				if err := db.Reader().QueryRow(`SELECT value FROM system_settings WHERE key=?`, key).Scan(&got); err != nil || got != want {
					t.Fatalf("setting %s=%s: %v", key, got, err)
				}
			}
			var history, order string
			var units, left, count int
			if err := db.Reader().QueryRow(`SELECT products_json FROM billing_catalog_versions WHERE id=2`).Scan(&history); err != nil || history != string(raw) {
				t.Fatalf("history: %v", err)
			}
			if err := db.Reader().QueryRow(`SELECT snapshot_json FROM billing_orders WHERE id='bank' AND amount_minor=399000`).Scan(&order); err != nil || order != string(sold) {
				t.Fatalf("sold order: %v", err)
			}
			if err := db.Reader().QueryRow(`SELECT SUM(units_total),SUM(units_remaining),COUNT(*) FROM credit_grants WHERE user_id='buyer'`).Scan(&units, &left, &count); err != nil || units != 520 || left != 424 || count != 2 {
				t.Fatalf("credits changed: %d/%d/%d %v", units, left, count, err)
			}
			repo := repository.New(db)
			pack, err := repo.IAPProductByStoreID(ctx, domain.ProviderGooglePlay, "kz.aireply.pack500")
			if err != nil || pack.Units != 500 || pack.Offered(domain.PlatformAndroid) || !pack.Offered(domain.PlatformIOS) {
				t.Fatalf("retired pack: %+v %v", pack, err)
			}
			_, err = s.Publish(ctx, "owner", pending.ID, pending.Revision)
			if stale {
				if !errors.Is(err, ErrConflict) {
					t.Fatalf("stale draft published: %v", err)
				}
			} else if err != nil {
				t.Fatalf("rebased draft: %v", err)
			}
			if applied, err := database.Migrate(ctx, db, migrations.FS); err != nil || len(applied) != 0 {
				t.Fatalf("migration replay: %v %v", applied, err)
			}
		})
	}
}

func TestGoogleMonthlyMigrationPreservesCustomWelcomeAndRollsBackFailure(t *testing.T) {
	for _, units := range []string{"0", "77"} {
		t.Run(units, func(t *testing.T) {
			db, exec := googleMonthlyFixture(t)
			exec(`UPDATE system_settings SET value=? WHERE key='welcome_bonus_units'`, units)
			if _, err := database.Migrate(context.Background(), db, migrations.FS); err != nil {
				t.Fatal(err)
			}
			var got string
			if err := db.Reader().QueryRow(`SELECT value FROM system_settings WHERE key='welcome_bonus_units'`).Scan(&got); err != nil || got != units {
				t.Fatalf("custom welcome: %s %v", got, err)
			}
		})
	}
	db, exec := googleMonthlyFixture(t)
	p := InitialProducts()
	p[0].Units = 20
	raw, _ := json.Marshal(p)
	exec(`INSERT INTO billing_catalog_versions(state,products_json,created_by,updated_by,created_at,updated_at) VALUES('published',?,'owner','owner',1000,1000)`, string(raw))
	exec(`CREATE TRIGGER fail_0044 BEFORE INSERT ON billing_catalog_audit BEGIN SELECT RAISE(ABORT,'test failure'); END`)
	if _, err := database.Migrate(context.Background(), db, migrations.FS); err == nil {
		t.Fatal("migration unexpectedly succeeded")
	}
	var got string
	if err := db.Reader().QueryRow(`SELECT value FROM system_settings WHERE key='welcome_bonus_units'`).Scan(&got); err != nil || got != "20" {
		t.Fatalf("partial migration survived: %s %v", got, err)
	}
	exec(`DROP TRIGGER fail_0044`)
	if _, err := database.Migrate(context.Background(), db, migrations.FS); err != nil {
		t.Fatalf("retry: %v", err)
	}
}
