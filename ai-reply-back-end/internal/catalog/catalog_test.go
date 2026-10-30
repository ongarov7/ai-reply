package catalog

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/migrations"
)

func testCatalog(t *testing.T) (*Service, *database.DB) {
	t.Helper()
	db, err := database.Open(database.Options{Path: filepath.Join(t.TempDir(), "catalog.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err = database.Migrate(context.Background(), db, migrations.FS); err != nil {
		t.Fatal(err)
	}
	s := New(db)
	if err = s.EnsureInitial(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s, db
}
func TestDraftReviewPublishIsAtomicAndVersioned(t *testing.T) {
	s, db := testCatalog(t)
	ctx := context.Background()
	initial, _ := s.Published(ctx)
	p := InitialProducts()
	p[5].PriceMinor = 20000
	p[5].AppleProductID = "test.pack30"
	draft, err := s.SaveDraft(ctx, "owner@example.test", 0, p)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := s.Published(ctx)
	if before.ID != initial.ID || before.Products[5].PriceMinor != 10000 {
		t.Fatal("draft leaked into published prices")
	}
	if _, err = s.SaveDraft(ctx, "stale@example.test", 0, p); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale save: %v", err)
	}
	if _, err = s.Publish(ctx, "owner@example.test", draft.ID, draft.Revision+1); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale publish: %v", err)
	}
	published, err := s.Publish(ctx, "owner@example.test", draft.ID, draft.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if published.ID == initial.ID || published.Products[5].PriceMinor != 20000 {
		t.Fatal("new snapshot not published")
	}
	var price, units int
	var id string
	if err = db.Reader().QueryRow(`SELECT price_minor,units,apple_product_id FROM iap_products WHERE code='pack30'`).Scan(&price, &units, &id); err != nil || price != 20000 || units != 30 || id != "test.pack30" {
		t.Fatalf("store mapping %d %d %q %v", price, units, id, err)
	}
	history, _ := s.History(ctx)
	if len(history) != 2 || history[1].State != "superseded" || history[1].Products[5].PriceMinor != 10000 {
		t.Fatal("published history rewritten")
	}
	audit, _ := s.AuditHistory(ctx)
	if len(audit) != 2 || audit[0].Actor != "owner@example.test" || audit[0].Before[5].PriceMinor != 10000 {
		t.Fatal("publication audit missing")
	}
	// A mapped store product may be pending outside our server. Even with no
	// received transaction, its entitlement cannot silently change underneath it.
	p[5].Units = 31
	draft, err = s.SaveDraft(ctx, "owner@example.test", 0, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Publish(ctx, "owner@example.test", draft.ID, draft.Revision); err == nil {
		t.Fatal("mapped entitlement was rewritten")
	}
	after, _ := s.Published(ctx)
	if after.ID != published.ID {
		t.Fatal("failed publish changed active version")
	}
}
func TestCatalogRejectsInvalidOrAmbiguousProducts(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func([]Product)
	}{
		{"negative price", func(p []Product) { p[1].PriceMinor = -1 }},
		{"free daily allowance", func(p []Product) { p[0].DailyLimit = 7 }},
		{"disabled welcome", func(p []Product) { p[0].Active = false }},
		{"empty names", func(p []Product) { p[3].Name = map[string]string{} }},
		{"duplicate store ids", func(p []Product) { p[1].AppleProductID = "test.same"; p[2].AppleProductID = "test.same" }},
		{"hidden numeric pro cap", func(p []Product) { p[3].DailyLimit = 30 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := InitialProducts()
			test.change(p)
			if Validate(p) == nil {
				t.Fatal("invalid catalog accepted")
			}
		})
	}
}
