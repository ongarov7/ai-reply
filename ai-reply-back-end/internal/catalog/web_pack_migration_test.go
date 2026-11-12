package catalog

import (
	"context"
	"database/sql"
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

// Keep historical migration fixtures independent of today's web lineup.
func productsBeforeWebPack() []Product {
	p := InitialProducts()
	p[1].AvailableWeb, p[6].AvailableWeb = true, false
	return p
}

func TestWebPackMigrationAlreadyAlignedDoesNotRepublish(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(database.Options{Path: filepath.Join(t.TempDir(), "aligned-web-pack.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err = database.Migrate(ctx, db, catalogMigrationsBefore(t, "0040")); err != nil {
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
	if err != nil || v.ID != 1 || v.UpdatedAt != 1000 || !reflect.DeepEqual(v.Products, InitialProducts()) {
		t.Fatalf("redundant publication: %+v %v", v, err)
	}
}

func TestWebPackMigrationPreservesPaidContractsStoresAndDrafts(t *testing.T) {
	for _, stale := range []bool{false, true} {
		t.Run(map[bool]string{false: "current draft", true: "stale draft"}[stale], func(t *testing.T) {
			ctx := context.Background()
			db, err := database.Open(database.Options{Path: filepath.Join(t.TempDir(), "web-pack.sqlite")})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			if _, err = database.Migrate(ctx, db, catalogMigrationsBefore(t, "0040")); err != nil {
				t.Fatal(err)
			}
			exec := func(q string, args ...any) {
				t.Helper()
				if _, err := db.Writer().Exec(q, args...); err != nil {
					t.Fatal(err)
				}
			}
			old := productsBeforeWebPack()
			raw, _ := json.Marshal(old)
			exec(`INSERT INTO billing_catalog_versions(id,state,products_json,created_by,updated_by,created_at,updated_at) VALUES(1,'superseded',?,'owner','owner',1000,1000),(2,'published',?,'owner','owner',1000,1000)`, string(raw), string(raw))
			draft := productsBeforeWebPack()
			draft[5].PriceMinor = 17777
			draft[6].Name["en"] = "Owner pack title"
			draftRaw, _ := json.Marshal(draft)
			parent := 2
			if stale {
				parent = 1
			}
			exec(`INSERT INTO billing_catalog_versions(id,state,base_version,revision,products_json,created_by,updated_by,created_at,updated_at) VALUES(3,'draft',?,3,?,'owner','owner',1000,1000)`, parent, string(draftRaw))
			exec(`INSERT INTO users(id,email,status,kind,created_at,updated_at) VALUES('buyer','buyer@example.test','active','account',1000,1000)`)
			sold, _ := json.Marshal(old[1])
			exec(`INSERT INTO billing_orders(id,user_id,idempotency_key,catalog_version,product_code,snapshot_json,amount_minor,currency,provider,environment,bank_order,status,checkout_token_hash,request_json,created_at,updated_at) VALUES('sold','buyer','old',2,'base_monthly',?,99000,'KZT','bcc','sandbox','old-bank','succeeded','hash','{}',1000,1000)`, string(sold))
			exec(`INSERT INTO bcc_subscriptions(id,user_id,environment,root_order_id,plan_id,snapshot_json,amount_minor,period_days,status,mandate_expires_at,merch_rn_id,created_at,updated_at)
 SELECT 'mandate','buyer','sandbox','sold',id,?,99000,30,'active',9999999999999,'original-mandate',1000,1000 FROM plans WHERE code='base_monthly'`, string(sold))
			exec(`INSERT INTO credit_grants(id,user_id,kind,units_total,units_remaining,starts_at,status,product_code,billing_order_id,created_at,updated_at) VALUES('paid','buyer','pack',1000,917,1000,'active','bcc_base_monthly','sold',1000,1000)`)
			repo := repository.New(db)
			storeBefore, err := repo.IAPProductByStoreID(ctx, domain.ProviderAppStore, "kz.aireply.pack500")
			if err != nil {
				t.Fatal(err)
			}
			planBefore, err := repo.PlanByCode(ctx, "base_monthly")
			if err != nil {
				t.Fatal(err)
			}
			if applied, err := database.Migrate(ctx, db, migrations.FS); err != nil || !reflect.DeepEqual(applied, []string{"0040_web_reply_pack_500.sql"}) {
				t.Fatalf("migration: %v %v", applied, err)
			}
			s := New(db)
			current, err := s.Published(ctx)
			if err != nil || current.BaseVersion != 2 || current.CreatedBy != "migration:0040" || !reflect.DeepEqual(current.Products, InitialProducts()) {
				t.Fatalf("publication: %+v %v", current, err)
			}
			wantDraft := draft
			wantDraft[1].AvailableWeb, wantDraft[6].AvailableWeb = false, true
			pending, err := s.Draft(ctx)
			wantParent := current.ID
			if stale {
				wantParent = 1
			}
			if err != nil || pending.Revision != 4 || pending.BaseVersion != wantParent || !reflect.DeepEqual(pending.Products, wantDraft) {
				t.Fatalf("draft edits/conflict guard lost: %+v %v", pending, err)
			}
			var history, order, mandate string
			var total, left int
			if err = db.Reader().QueryRow(`SELECT products_json FROM billing_catalog_versions WHERE id=2 AND state='superseded'`).Scan(&history); err != nil || history != string(raw) {
				t.Fatal("history changed", err)
			}
			if err = db.Reader().QueryRow(`SELECT snapshot_json FROM billing_orders WHERE id='sold' AND amount_minor=99000`).Scan(&order); err != nil || order != string(sold) {
				t.Fatal("sold order changed", err)
			}
			if err = db.Reader().QueryRow(`SELECT snapshot_json FROM bcc_subscriptions WHERE id='mandate' AND status='active' AND amount_minor=99000`).Scan(&mandate); err != nil || mandate != string(sold) {
				t.Fatal("recurring contract changed", err)
			}
			if err = db.Reader().QueryRow(`SELECT units_total,units_remaining FROM credit_grants WHERE id='paid' AND expires_at IS NULL`).Scan(&total, &left); err != nil || total != 1000 || left != 917 {
				t.Fatalf("paid balance changed: %d %d %v", total, left, err)
			}
			storeAfter, err := repo.IAPProductByStoreID(ctx, domain.ProviderAppStore, "kz.aireply.pack500")
			if err != nil || !reflect.DeepEqual(storeBefore, storeAfter) {
				t.Fatal("Apple product changed", err)
			}
			planAfter, err := repo.PlanByCode(ctx, "base_monthly")
			if err != nil || !reflect.DeepEqual(planBefore, planAfter) {
				t.Fatal("legacy native plan changed", err)
			}
			if err = db.Tx(ctx, func(tx *sql.Tx) error {
				product, _, err := s.ProductForWebInTx(ctx, tx, "pack500")
				if err != nil || product.Kind != "pack" || product.Units != 500 || product.PriceMinor != 99000 || product.BillingPeriod != "" {
					t.Fatalf("new one-time checkout: %+v %v", product, err)
				}
				if _, _, err = s.ProductForWebInTx(ctx, tx, "base_monthly"); !errors.Is(err, domain.ErrPlanUnavailable) {
					t.Fatalf("retired Base still offered: %v", err)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if stale {
				if _, err := s.Publish(ctx, "owner", pending.ID, pending.Revision); !errors.Is(err, ErrConflict) {
					t.Fatalf("stale draft published: %v", err)
				}
			} else if _, err := s.Publish(ctx, "owner", pending.ID, pending.Revision); err != nil {
				t.Fatalf("aligned draft cannot publish: %v", err)
			}
			if applied, err := database.Migrate(ctx, db, migrations.FS); err != nil || len(applied) != 0 {
				t.Fatalf("reapplied: %v %v", applied, err)
			}
		})
	}
}
