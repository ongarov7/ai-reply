package apptest

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/migrations"
)

// 0018 бар дерекқорға қолданылады: тіркелгілер мен кредиттер сақталады, сату өшірулі, өнім жоқ.
func TestIAPMigrationKeepsExistingAccounts(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(database.Options{Path: filepath.Join(t.TempDir(), "old.db"), MaxReadConns: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	before := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") && e.Name() < "0018" {
			body, _ := fs.ReadFile(migrations.FS, e.Name())
			before[e.Name()] = &fstest.MapFile{Data: body}
		}
	}
	if _, err := database.Migrate(ctx, db, before); err != nil {
		t.Fatalf("old migrations: %v", err)
	}
	exec := func(stmt string, args ...any) {
		t.Helper()
		if _, err := db.Writer().ExecContext(ctx, stmt, args...); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	scalar := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := db.Reader().QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		return n
	}
	exec(`INSERT INTO users (id, email, status, created_at, updated_at) VALUES ('u-1', 'old@example.com', 'active', 1000, 1000)`)
	exec(`INSERT INTO credit_grants (id, user_id, kind, units_total, units_remaining, starts_at, status, created_at, updated_at)
		VALUES ('g-1', 'u-1', 'admin', 5, 5, 1000, 'active', 1000, 1000)`)

	applied, err := database.Migrate(ctx, db, migrations.FS)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if len(applied) == 0 || applied[0] != "0018_iap.sql" {
		t.Fatalf("applied = %v", applied)
	}
	if scalar(`SELECT COUNT(*) FROM users`) != 1 || scalar(`SELECT units_remaining FROM credit_grants WHERE id = 'g-1'`) != 5 {
		t.Fatal("existing rows changed")
	}
	for _, table := range []string{"iap_products", "iap_purchases", "iap_notifications"} {
		if n := scalar(`SELECT COUNT(*) FROM ` + table); n != 0 {
			t.Fatalf("%s has %d seeded row(s): no products or prices are seeded", table, n)
		}
	}
	var enabled string
	if err := db.Reader().QueryRowContext(ctx, `SELECT value FROM system_settings WHERE key = 'iap_enabled'`).Scan(&enabled); err != nil ||
		enabled != "false" {
		t.Fatalf("iap_enabled = %q (%v)", enabled, err)
	}

	// One row per store transaction.
	purchase := `INSERT INTO iap_purchases (id, user_id, provider, transaction_id, status, created_at, updated_at)
		VALUES (?, 'u-1', 'app_store', '2000000000000001', 'granted', 2000, 2000)`
	exec(purchase, "p-1")
	if _, err := db.Writer().ExecContext(ctx, purchase, "p-2"); err == nil {
		t.Fatal("the same transaction was recorded twice")
	}
	exec(`INSERT INTO iap_purchases (id, user_id, provider, transaction_id, status, created_at, updated_at)
		VALUES ('p-3', 'u-1', 'google_play', '2000000000000001', 'granted', 2000, 2000)`)
	exec(`INSERT INTO iap_notifications (id, provider, notification_id, type, outcome, received_at)
		VALUES ('n-1', 'app_store', 'uuid-1', 'TEST', 'logged', 2000)`)
	if _, err := db.Writer().ExecContext(ctx, `INSERT INTO iap_notifications (id, provider, notification_id, type, outcome, received_at)
		VALUES ('n-2', 'app_store', 'uuid-1', 'TEST', 'logged', 2000)`); err == nil {
		t.Fatal("the same notification was recorded twice")
	}
	for _, code := range []string{"pack_a", "pack_b"} {
		if _, err := db.Writer().ExecContext(ctx, `INSERT INTO iap_products (id, code, kind, apple_product_id, created_at, updated_at)
			VALUES (?, ?, 'pack', 'kz.same.id', 1, 1)`, code, code); err != nil && code == "pack_a" {
			t.Fatal(err)
		} else if err == nil && code == "pack_b" {
			t.Fatal("two products share an App Store id")
		}
	}

	// A purchase outlives its account, without the person.
	exec(`DELETE FROM users WHERE id = 'u-1'`)
	if scalar(`SELECT COUNT(*) FROM iap_purchases WHERE user_id IS NULL`) != 2 || scalar(`SELECT COUNT(*) FROM credit_grants`) != 0 {
		t.Fatal("purchases must stay with user_id NULL, grants go with the account")
	}
	if again, err := database.Migrate(ctx, db, migrations.FS); err != nil || len(again) != 0 {
		t.Fatalf("rerun: %v %v", again, err)
	}
}
