package catalog

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/migrations"
)

func googleMigrationFixture(t *testing.T) (*database.DB, func(string, ...any)) {
	t.Helper()
	db, err := database.Open(database.Options{Path: filepath.Join(t.TempDir(), "google-ids.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := database.Migrate(context.Background(), db, catalogMigrationsBefore(t, "0038")); err != nil {
		t.Fatal(err)
	}
	return db, func(query string, args ...any) {
		t.Helper()
		if _, err := db.Writer().Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGoogleMigrationCarriesAnExistingMappingIntoTheCatalog(t *testing.T) {
	ctx := context.Background()
	db, exec := googleMigrationFixture(t)
	products := InitialProducts()
	raw, _ := json.Marshal(products)
	exec(`INSERT INTO billing_catalog_versions(id,state,products_json,created_by,updated_by,published_by,created_at,updated_at,published_at) VALUES(1,'published',?,'owner','owner','owner',1000,1000,1000)`, string(raw))
	draftRaw, _ := json.Marshal(products)
	exec(`INSERT INTO billing_catalog_versions(id,state,base_version,revision,products_json,created_by,updated_by,created_at,updated_at) VALUES(2,'draft',1,3,?,'owner','owner',1000,1000)`, string(draftRaw))
	exec(`UPDATE iap_products SET google_product_id='pack_30',is_active=1 WHERE code='pack30'`)
	if _, err := database.Migrate(ctx, db, migrations.FS); err != nil {
		t.Fatal(err)
	}
	s := New(db)
	current, err := s.Published(ctx)
	if err != nil || current.ID == 1 || current.BaseVersion != 1 {
		t.Fatalf("publication: %+v %v", current, err)
	}
	want := append([]Product(nil), products...)
	want[5].GoogleProductID, want[5].AvailableAndroid = "pack_30", true
	if !reflect.DeepEqual(current.Products, want) {
		t.Fatalf("catalog: %+v", current.Products)
	}
	draft, err := s.Draft(ctx)
	if err != nil || draft.BaseVersion != current.ID || draft.Revision != 4 || !reflect.DeepEqual(draft.Products, want) {
		t.Fatalf("draft: %+v %v", draft, err)
	}
	// The mapping survives publishing, and Android keeps selling the pack.
	if _, err := s.Publish(ctx, "owner", draft.ID, draft.Revision); err != nil {
		t.Fatalf("publish: %v", err)
	}
	var google string
	var active, offAndroid int
	if err := db.Reader().QueryRow(`SELECT google_product_id,is_active,off_android FROM iap_products WHERE code='pack30'`).Scan(&google, &active, &offAndroid); err != nil ||
		google != "pack_30" || active != 1 || offAndroid != 0 {
		t.Fatalf("store mapping: %q %d %d %v", google, active, offAndroid, err)
	}
}

// Production has applied 0036 and 0037 and runs the catalogue version 0037 published.
func TestGoogleMigrationAppliesOnTopOfTheMonthlyBundle(t *testing.T) {
	for _, mapped := range []bool{false, true} {
		ctx := context.Background()
		db, err := database.Open(database.Options{Path: filepath.Join(t.TempDir(), "production.sqlite")})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		if _, err := database.Migrate(ctx, db, catalogMigrationsBefore(t, "0037")); err != nil {
			t.Fatal(err)
		}
		exec := func(query string, args ...any) {
			t.Helper()
			if _, err := db.Writer().Exec(query, args...); err != nil {
				t.Fatal(err)
			}
		}
		old := legacyDailyProducts()
		for i := 1; i < len(old); i++ {
			old[i].AppleProductID = "test." + old[i].Code
		}
		raw, _ := json.Marshal(old)
		for id := 1; id < 5; id++ {
			exec(`INSERT INTO billing_catalog_versions(id,state,products_json,created_by,updated_by,created_at,updated_at) VALUES(?,'superseded',?,'owner','owner',1000,1000)`, id, string(raw))
		}
		exec(`INSERT INTO billing_catalog_versions(id,state,products_json,created_by,updated_by,published_by,created_at,updated_at,published_at) VALUES(5,'published',?,'owner','owner','owner',1000,1000,1000)`, string(raw))
		exec(`UPDATE iap_products SET apple_product_id='test.'||code,is_active=1 WHERE code<>'free'`)
		if _, err := database.Migrate(ctx, db, catalogMigrationsBefore(t, "0038")); err != nil {
			t.Fatal(err)
		}
		s := New(db)
		version6, err := s.Published(ctx)
		if err != nil || version6.ID != 6 || version6.CreatedBy != "migration:0037" {
			t.Fatalf("after 0037: %+v %v", version6, err)
		}
		if mapped {
			exec(`UPDATE iap_products SET google_product_id='pack_30' WHERE code='pack30'`)
		}
		if _, err := database.Migrate(ctx, db, migrations.FS); err != nil {
			t.Fatal(err)
		}
		current, err := s.Published(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !mapped {
			if current.ID != 6 || !reflect.DeepEqual(current.Products, version6.Products) {
				t.Fatalf("0038 changed the catalogue: %+v", current)
			}
			var versions, offIOS, offAndroid int
			if err := db.Reader().QueryRow(`SELECT (SELECT count(*) FROM billing_catalog_versions),coalesce(SUM(off_ios),0),coalesce(SUM(off_android),0) FROM iap_products`).Scan(&versions, &offIOS, &offAndroid); err != nil ||
				versions != 6 || offIOS != 0 || offAndroid != 0 {
				t.Fatalf("0038 wrote more than its columns: %d %d %d %v", versions, offIOS, offAndroid, err)
			}
			continue
		}
		want := append([]Product(nil), version6.Products...)
		want[5].GoogleProductID, want[5].AvailableAndroid = "pack_30", true
		if current.ID != 7 || current.BaseVersion != 6 || !reflect.DeepEqual(current.Products, want) {
			t.Fatalf("mapped: %+v", current)
		}
	}
}

func TestGoogleMigrationDoesNothingWithoutAMapping(t *testing.T) {
	ctx := context.Background()
	db, exec := googleMigrationFixture(t)
	raw, _ := json.Marshal(InitialProducts())
	exec(`INSERT INTO billing_catalog_versions(id,state,products_json,created_by,updated_by,published_by,created_at,updated_at,published_at) VALUES(1,'published',?,'owner','owner','owner',1000,1000,1000)`, string(raw))
	if _, err := database.Migrate(ctx, db, migrations.FS); err != nil {
		t.Fatal(err)
	}
	history, err := New(db).History(ctx)
	if err != nil || len(history) != 1 || history[0].ID != 1 || history[0].UpdatedAt != 1000 {
		t.Fatalf("history: %+v %v", history, err)
	}
	var stored string
	if err := db.Reader().QueryRow(`SELECT products_json FROM billing_catalog_versions WHERE id=1`).Scan(&stored); err != nil || stored != string(raw) {
		t.Fatalf("publication rewritten: %v", err)
	}
}
