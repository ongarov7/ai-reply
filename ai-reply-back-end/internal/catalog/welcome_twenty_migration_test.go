package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/aireply/ai-reply-back-end/internal/database"
)

func welcomeTwentyFixture(t *testing.T) (*database.DB, func(string, ...any)) {
	t.Helper()
	db, err := database.Open(database.Options{Path: filepath.Join(t.TempDir(), "welcome-twenty.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := database.Migrate(context.Background(), db, catalogMigrationsBefore(t, "0035")); err != nil {
		t.Fatal(err)
	}
	return db, func(query string, args ...any) {
		t.Helper()
		if _, err := db.Writer().Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWelcomeTwentyMigrationVersionsOnlyDefaultFreeAndPreservesCustomers(t *testing.T) {
	ctx := context.Background()
	db, exec := welcomeTwentyFixture(t)
	old := legacyDailyProducts()
	old[0].Units = 10
	old[1].PriceMinor = 81111 // A later owner price must never be rewound.
	old[2].AvailableWeb = false
	old[5].AppleProductID = "test.purchased-pack"
	raw, _ := json.Marshal(old)
	exec(`INSERT INTO billing_catalog_versions(id,state,products_json,created_by,updated_by,published_by,created_at,updated_at,published_at)
 VALUES(1,'published',?,'owner','owner','owner',1000,1000,1000)`, string(raw))
	draftProducts := append([]Product(nil), old...)
	draftProducts[1].Name = map[string]string{"ru": "Изменение владельца", "en": "Owner edit", "kk": "Иесінің өзгерісі", "uz": "Owner edit"}
	draftProducts[5].PriceMinor = 17777
	draftRaw, _ := json.Marshal(draftProducts)
	exec(`INSERT INTO billing_catalog_versions(id,state,base_version,revision,products_json,created_by,updated_by,created_at,updated_at)
 VALUES(2,'draft',1,4,?,'owner','owner',1000,1000)`, string(draftRaw))
	exec(`UPDATE system_settings SET value='10' WHERE key='welcome_bonus_units'`)
	exec(`UPDATE system_settings SET value='1234' WHERE key='welcome_bonus_since'`)
	exec(`UPDATE plans SET price=81111 WHERE code='base_monthly'`)
	exec(`INSERT INTO users(id,email,status,kind,created_at,updated_at) VALUES('existing','existing@example.test','active','account',2000,2000),('missed','missed@example.test','active','account',2000,2000)`)
	exec(`INSERT INTO credit_grants(id,user_id,kind,units_total,units_remaining,starts_at,status,created_at,updated_at)
 VALUES('spent-welcome','existing','welcome',10,0,1000,'active',1000,1000),('purchased','existing','pack',30,19,1000,'active',1000,1000)`)
	exec(`INSERT INTO usage_daily(user_id,usage_date,used,updated_at) VALUES('existing','2026-11-03',5,1000)`)
	orderRaw, _ := json.Marshal(old[1])
	exec(`INSERT INTO billing_orders(id,user_id,idempotency_key,catalog_version,product_code,snapshot_json,amount_minor,currency,provider,environment,bank_order,status,checkout_token_hash,request_json,created_at,updated_at)
 VALUES('sold','existing','original',1,'base_monthly',?,81111,'KZT','bcc','sandbox','original-bank','pending','test-hash','{}',1000,1000)`, string(orderRaw))
	if applied, err := database.Migrate(ctx, db, catalogMigrationsBefore(t, "0036")); err != nil || !reflect.DeepEqual(applied, []string{"0035_free_welcome_twenty.sql"}) {
		t.Fatalf("migration: %v %v", applied, err)
	}
	s := New(db)
	current, err := s.Published(ctx)
	want := append([]Product(nil), old...)
	want[0].Units = 20
	if err != nil || current.ID == 1 || current.BaseVersion != 1 || !reflect.DeepEqual(current.Products, want) {
		t.Fatalf("published fields changed unexpectedly: %+v %v", current, err)
	}
	draft, err := s.Draft(ctx)
	draftProducts[0].Units = 20
	if err != nil || draft.ID != 2 || draft.Revision != 5 || draft.BaseVersion != current.ID || !reflect.DeepEqual(draft.Products, draftProducts) {
		t.Fatalf("draft edits lost or stale revision retained: %+v %v", draft, err)
	}
	if _, err := s.SaveDraft(ctx, "owner", 4, draftProducts); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale draft save accepted: %v", err)
	}
	var historical, sold string
	var amount, total, remaining, count, used int
	if err := db.Reader().QueryRow(`SELECT products_json FROM billing_catalog_versions WHERE id=1 AND state='superseded'`).Scan(&historical); err != nil || historical != string(raw) {
		t.Fatalf("historical publication changed: %v", err)
	}
	if err := db.Reader().QueryRow(`SELECT snapshot_json,amount_minor FROM billing_orders WHERE id='sold'`).Scan(&sold, &amount); err != nil || sold != string(orderRaw) || amount != 81111 {
		t.Fatalf("sold snapshot changed: %v", err)
	}
	if err := db.Reader().QueryRow(`SELECT sum(units_total),sum(units_remaining),count(*) FROM credit_grants WHERE user_id='existing'`).Scan(&total, &remaining, &count); err != nil || total != 40 || remaining != 19 || count != 2 {
		t.Fatalf("existing balances changed: %d/%d/%d %v", total, remaining, count, err)
	}
	if err := db.Reader().QueryRow(`SELECT COUNT(*) FROM credit_grants WHERE user_id='missed'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("migration created a retroactive grant: %v", err)
	}
	if err := db.Reader().QueryRow(`SELECT used FROM usage_daily WHERE user_id='existing'`).Scan(&used); err != nil || used != 5 {
		t.Fatalf("usage changed: %v", err)
	}
	for key, expected := range map[string]string{"welcome_bonus_units": "20", "welcome_bonus_since": "1234"} {
		var value string
		if err := db.Reader().QueryRow(`SELECT value FROM system_settings WHERE key=?`, key).Scan(&value); err != nil || value != expected {
			t.Fatalf("setting %s=%s, want %s: %v", key, value, expected, err)
		}
	}
	var baseDaily, proDaily int
	var basePrice, proPrice int64
	var proKind string
	if err := db.Reader().QueryRow(`SELECT price,daily_message_limit FROM plans WHERE code='base_monthly'`).Scan(&basePrice, &baseDaily); err != nil || basePrice != 81111 || baseDaily != 40 {
		t.Fatalf("Base changed: %v", err)
	}
	if err := db.Reader().QueryRow(`SELECT price,daily_message_limit,entitlement_kind FROM plans WHERE code='pro_yearly'`).Scan(&proPrice, &proDaily, &proKind); err != nil || proPrice != 3490000 || proDaily != 0 || proKind != "unlimited" {
		t.Fatalf("Pro changed: %v", err)
	}
	audit, err := s.AuditHistory(ctx)
	if err != nil || len(audit) != 2 || audit[0].Action != "publish" || audit[1].Action != "rebase_welcome" {
		t.Fatalf("missing migration audit: %+v %v", audit, err)
	}
	if applied, err := database.Migrate(ctx, db, catalogMigrationsBefore(t, "0036")); err != nil || len(applied) != 0 {
		t.Fatalf("repeat migration: %v %v", applied, err)
	}
}

func TestWelcomeTwentyMigrationPreservesDisabledAndCustomSettings(t *testing.T) {
	for _, units := range []string{"0", "7", "50"} {
		t.Run(units, func(t *testing.T) {
			db, exec := welcomeTwentyFixture(t)
			old := legacyDailyProducts()
			old[0].Units = 10
			raw, _ := json.Marshal(old)
			exec(`INSERT INTO billing_catalog_versions(id,state,products_json,created_by,updated_by,created_at,updated_at)
 VALUES(1,'published',?,'owner','owner',1000,1000)`, string(raw))
			exec(`UPDATE system_settings SET value=? WHERE key='welcome_bonus_units'`, units)
			exec(`UPDATE system_settings SET value='1234' WHERE key='welcome_bonus_since'`)
			if _, err := database.Migrate(context.Background(), db, catalogMigrationsBefore(t, "0036")); err != nil {
				t.Fatal(err)
			}
			var actual, cutoff, after string
			if err := db.Reader().QueryRow(`SELECT value FROM system_settings WHERE key='welcome_bonus_units'`).Scan(&actual); err != nil || actual != units {
				t.Fatalf("custom override replaced: %q %v", actual, err)
			}
			if err := db.Reader().QueryRow(`SELECT value FROM system_settings WHERE key='welcome_bonus_since'`).Scan(&cutoff); err != nil || cutoff != "1234" {
				t.Fatalf("cutoff changed: %q %v", cutoff, err)
			}
			if err := db.Reader().QueryRow(`SELECT products_json FROM billing_catalog_versions WHERE id=1 AND state='published'`).Scan(&after); err != nil || after != string(raw) {
				t.Fatalf("custom policy publication changed: %v", err)
			}
		})
	}
}

func TestWelcomeTwentyMigrationPreservesCustomDraftAndAlreadyAlignedPublication(t *testing.T) {
	ctx := context.Background()
	db, exec := welcomeTwentyFixture(t)
	products := legacyDailyProducts()
	raw, _ := json.Marshal(products)
	exec(`INSERT INTO billing_catalog_versions(id,state,products_json,created_by,updated_by,created_at,updated_at)
 VALUES(1,'published',?,'owner','owner',1000,1000)`, string(raw))
	products[0].Units = 7
	draftRaw, _ := json.Marshal(products)
	exec(`INSERT INTO billing_catalog_versions(id,state,base_version,revision,products_json,created_by,updated_by,created_at,updated_at)
 VALUES(2,'draft',1,4,?,'owner','owner',1000,1000)`, string(draftRaw))
	exec(`UPDATE system_settings SET value='20' WHERE key='welcome_bonus_units'`)
	if _, err := database.Migrate(ctx, db, catalogMigrationsBefore(t, "0036")); err != nil {
		t.Fatal(err)
	}
	s := New(db)
	published, err := s.Published(ctx)
	if err != nil || published.ID != 1 || published.UpdatedAt != 1000 || !reflect.DeepEqual(published.Products, legacyDailyProducts()) {
		t.Fatalf("already aligned publication rewritten: %+v %v", published, err)
	}
	draft, err := s.Draft(ctx)
	if err != nil || draft.BaseVersion != 1 || draft.Revision != 4 || !reflect.DeepEqual(draft.Products, products) {
		t.Fatalf("custom draft changed: %+v %v", draft, err)
	}
}

func TestWelcomeTwentyMigrationRebasesOnlyCurrentDraftAndKeepsCustomQuantity(t *testing.T) {
	for _, tc := range []struct {
		name  string
		base  int64
		units int
	}{
		{"current custom quantity", 1, 7},
		{"stale default quantity", 999, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, exec := welcomeTwentyFixture(t)
			products := legacyDailyProducts()
			products[0].Units = 10
			raw, _ := json.Marshal(products)
			exec(`INSERT INTO billing_catalog_versions(id,state,products_json,created_by,updated_by,created_at,updated_at)
 VALUES(1,'published',?,'owner','owner',1000,1000)`, string(raw))
			products[0].Units = tc.units
			draftRaw, _ := json.Marshal(products)
			exec(`INSERT INTO billing_catalog_versions(id,state,base_version,revision,products_json,created_by,updated_by,created_at,updated_at)
 VALUES(2,'draft',?,4,?,'owner','owner',1000,1000)`, tc.base, string(draftRaw))
			if _, err := database.Migrate(ctx, db, catalogMigrationsBefore(t, "0036")); err != nil {
				t.Fatal(err)
			}
			s := New(db)
			current, err := s.Published(ctx)
			if err != nil || current.Products[0].Units != 20 {
				t.Fatalf("new default missing: %+v %v", current, err)
			}
			draft, err := s.Draft(ctx)
			wantBase, wantUnits := tc.base, tc.units
			if tc.base == 1 {
				wantBase = current.ID
			}
			if tc.units == 10 {
				wantUnits = 20
			}
			if err != nil || draft.BaseVersion != wantBase || draft.Revision != 5 || draft.Products[0].Units != wantUnits {
				t.Fatalf("incorrect draft rebase: %+v %v", draft, err)
			}
		})
	}
}

func TestWelcomeTwentyMigrationFailureRollsBackAndCanRetry(t *testing.T) {
	ctx := context.Background()
	db, exec := welcomeTwentyFixture(t)
	products := legacyDailyProducts()
	products[0].Units = 10
	raw, _ := json.Marshal(products)
	exec(`INSERT INTO billing_catalog_versions(id,state,products_json,created_by,updated_by,created_at,updated_at)
 VALUES(1,'published',?,'owner','owner',1000,1000)`, string(raw))
	exec(`CREATE TRIGGER fail_welcome_migration BEFORE INSERT ON billing_catalog_audit
 WHEN NEW.actor='migration:0035' BEGIN SELECT RAISE(ABORT,'test audit failure'); END`)
	if _, err := database.Migrate(ctx, db, catalogMigrationsBefore(t, "0036")); err == nil {
		t.Fatal("expected simulated migration failure")
	}
	var units string
	if err := db.Reader().QueryRow(`SELECT value FROM system_settings WHERE key='welcome_bonus_units'`).Scan(&units); err != nil || units != "10" {
		t.Fatalf("partial allowance update survived rollback: %s %v", units, err)
	}
	current, err := New(db).Published(ctx)
	if err != nil || current.ID != 1 || current.Products[0].Units != 10 {
		t.Fatalf("partial catalog publication survived rollback: %+v %v", current, err)
	}
	exec(`DROP TRIGGER fail_welcome_migration`)
	if applied, err := database.Migrate(ctx, db, catalogMigrationsBefore(t, "0036")); err != nil || !reflect.DeepEqual(applied, []string{"0035_free_welcome_twenty.sql"}) {
		t.Fatalf("migration retry: %v %v", applied, err)
	}
}
