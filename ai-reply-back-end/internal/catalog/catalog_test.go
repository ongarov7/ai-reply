package catalog

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/internal/domain"
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
	if before.ID != initial.ID || before.Products[5].PriceMinor != 13000 {
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
	if len(history) != 2 || history[1].State != "superseded" || history[1].Products[5].PriceMinor != 13000 {
		t.Fatal("published history rewritten")
	}
	audit, _ := s.AuditHistory(ctx)
	if len(audit) != 2 || audit[0].Actor != "owner@example.test" || audit[0].Before[5].PriceMinor != 13000 {
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

func TestCatalogGoogleIDsAreValidatedAndUnique(t *testing.T) {
	for name, change := range map[string]func([]Product){
		"format":    func(p []Product) { p[1].GoogleProductID = "Bad ID" },
		"duplicate": func(p []Product) { p[1].GoogleProductID, p[2].GoogleProductID = "kz.same", "kz.same" },
	} {
		p := InitialProducts()
		change(p)
		if err := Validate(p); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	// The same id on two different stores is fine; Android is off until asked for.
	p := InitialProducts()
	for i := range p {
		if p[i].GoogleProductID != "" || p[i].AvailableAndroid {
			t.Fatalf("Android is on by default: %+v", p[i])
		}
	}
	p[1].AppleProductID, p[1].GoogleProductID = "kz.aireply.base.monthly", "kz.aireply.base.monthly"
	if err := Validate(p); err != nil {
		t.Fatal(err)
	}
}

// Older Android builds list only packs: with subscriptions alone on sale they
// would be told purchases are open and show an empty screen.
func TestAndroidSubscriptionsNeedThePackOnAndroid(t *testing.T) {
	p := InitialProducts()
	p[1].GoogleProductID, p[1].AvailableAndroid = "kz.aireply.base.monthly", true
	err := Validate(p)
	if !errors.Is(err, domain.ErrInvalidRequest) || !strings.Contains(fmt.Sprint(err), "available_android") {
		t.Fatalf("subscriptions alone: %v", err)
	}
	p[5].GoogleProductID = "kz.aireply.pack30"
	if err := Validate(p); err == nil {
		t.Fatal("the pack is mapped but its switch is off")
	}
	p[5].AvailableAndroid = true
	if err := Validate(p); err != nil {
		t.Fatalf("together: %v", err)
	}
	// An id alone does not put a subscription on sale.
	p = InitialProducts()
	p[1].GoogleProductID = "kz.aireply.base.monthly"
	if err := Validate(p); err != nil {
		t.Fatalf("mapped, switch off: %v", err)
	}
}

// The public catalogue stays as it was once Play ids are configured.
func TestPublicCatalogDoesNotCarryPlayMapping(t *testing.T) {
	s, _ := testCatalog(t)
	p := InitialProducts()
	p[1].AppleProductID, p[1].GoogleProductID, p[1].AvailableAndroid = "test.base", "kz.aireply.base.monthly", true
	p[5].GoogleProductID, p[5].AvailableAndroid = "kz.aireply.pack30", true
	if err := publishProducts(t, s, p); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.RegisterPublic(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/billing/catalog", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "test.base") ||
		strings.Contains(rec.Body.String(), "google_product_id") || strings.Contains(rec.Body.String(), "available_android") {
		t.Fatalf("public catalog: %d %s", rec.Code, rec.Body.String())
	}
}

func publishProducts(t *testing.T, s *Service, p []Product) error {
	t.Helper()
	ctx := context.Background()
	draft, err := s.Draft(ctx)
	revision := 0
	if err == nil {
		revision = draft.Revision
	}
	saved, err := s.SaveDraft(ctx, "owner@example.test", revision, p)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Publish(ctx, "owner@example.test", saved.ID, saved.Revision)
	return err
}

func TestPublishMapsEachStoreAndItsSwitch(t *testing.T) {
	s, db := testCatalog(t)
	read := func() (apple, google string, active, offIOS, offAndroid int) {
		t.Helper()
		if err := db.Reader().QueryRow(`SELECT coalesce(apple_product_id,''),coalesce(google_product_id,''),is_active,off_ios,off_android FROM iap_products WHERE code='base_monthly'`).
			Scan(&apple, &google, &active, &offIOS, &offAndroid); err != nil {
			t.Fatal(err)
		}
		return
	}
	// Nothing configured: nothing sold, and nothing but what was written before.
	if err := publishProducts(t, s, InitialProducts()); err != nil {
		t.Fatal(err)
	}
	if _, google, active, offIOS, offAndroid := read(); google != "" || active != 0 || offIOS != 0 || offAndroid != 0 {
		t.Fatalf("unconfigured: %q %d %d %d", google, active, offIOS, offAndroid)
	}

	p := InitialProducts()
	p[1].AppleProductID = "test.base"
	if err := publishProducts(t, s, p); err != nil {
		t.Fatal(err)
	}
	if apple, google, active, offIOS, offAndroid := read(); apple != "test.base" || google != "" || active != 1 || offIOS != 0 || offAndroid != 0 {
		t.Fatalf("Apple only: %q %q %d %d %d", apple, google, active, offIOS, offAndroid)
	}

	// A Google id joins an Apple-mapped product; Android opens only with its switch.
	p[1].GoogleProductID = "kz.aireply.base.monthly"
	if err := publishProducts(t, s, p); err != nil {
		t.Fatalf("adding a Google id to a mapped product: %v", err)
	}
	if _, google, active, _, offAndroid := read(); google != "kz.aireply.base.monthly" || active != 1 || offAndroid != 1 {
		t.Fatalf("mapped, switch off: %q %d %d", google, active, offAndroid)
	}
	p[1].AvailableAndroid = true
	p[5].GoogleProductID, p[5].AvailableAndroid = "kz.aireply.pack30", true
	if err := publishProducts(t, s, p); err != nil {
		t.Fatal(err)
	}
	if _, _, active, _, offAndroid := read(); active != 1 || offAndroid != 0 {
		t.Fatalf("switched on: %d %d", active, offAndroid)
	}

	// Android alone keeps the product on sale there without reopening iOS.
	p[1].AvailableIOS = false
	if err := publishProducts(t, s, p); err != nil {
		t.Fatal(err)
	}
	if _, _, active, offIOS, offAndroid := read(); active != 1 || offIOS != 1 || offAndroid != 0 {
		t.Fatalf("Android only: %d %d %d", active, offIOS, offAndroid)
	}

	// A mapped Google id is a contract, like an Apple one.
	p[1].GoogleProductID = "kz.aireply.base.other"
	if err := publishProducts(t, s, p); err == nil {
		t.Fatal("a mapped Google id was replaced")
	}

	// An id held by another product, even an archived one, is refused cleanly.
	if _, err := db.Writer().Exec(`UPDATE iap_products SET google_product_id='kz.taken',archived_at=1 WHERE code='pack30'`); err != nil {
		t.Fatal(err)
	}
	p = InitialProducts()
	p[1].AppleProductID, p[1].GoogleProductID = "test.base", "kz.aireply.base.monthly"
	p[2].GoogleProductID = "kz.taken"
	if err := publishProducts(t, s, p); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("taken id: %v", err)
	}
}
