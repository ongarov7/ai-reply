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

// 0016 бар дерекқорға қолданылады: орнатулар мен науқандар сақталады, жаңа кестелер бос.
func TestDiagnosticsMigrationKeepsExistingInstallations(t *testing.T) {
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
		if strings.HasSuffix(e.Name(), ".sql") && e.Name() < "0016" {
			body, _ := fs.ReadFile(migrations.FS, e.Name())
			before[e.Name()] = &fstest.MapFile{Data: body}
		}
	}
	if _, err := database.Migrate(ctx, db, before); err != nil {
		t.Fatalf("old migrations: %v", err)
	}
	for _, stmt := range []string{
		`INSERT INTO users (id, email, status, created_at, updated_at) VALUES ('u-1', 'old@example.com', 'active', 1000, 1000)`,
		`INSERT INTO app_installations (id, installation_id, user_id, platform, first_seen_at, last_seen_at, created_at, updated_at)
		 VALUES ('i-1', '00000000-0000-4000-8000-000000000001', 'u-1', 'ios', 1000, 1000, 1000, 1000)`,
		`INSERT INTO notification_campaigns (id, title, body, audience, status, created_by, created_at, updated_at)
		 VALUES ('c-1', 'T', 'B', 'all', 'completed', 'a-1', 1000, 1000)`,
		`INSERT INTO notifications (id, dedupe_key, idempotency_key, user_id, category, type, locale, title, body, link, data, params, created_at)
		 VALUES ('n-1', 'user:u-1:k', 'k', 'u-1', 'account', 'quota_low', 'kk', 'T', 'B', '', '{}', '{}', 1000)`,
	} {
		if _, err := db.Writer().ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	applied, err := database.Migrate(ctx, db, migrations.FS)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if len(applied) == 0 || applied[0] != "0016_permission_diagnostics.sql" {
		t.Fatalf("applied = %v", applied)
	}
	var kind string
	if err := db.Reader().QueryRow(`SELECT notification_type FROM notification_campaigns WHERE id = 'c-1'`).Scan(&kind); err != nil || kind != "campaign" {
		t.Fatalf("campaign type = %q (%v)", kind, err)
	}
	var notBefore any
	if err := db.Reader().QueryRow(`SELECT not_before FROM notifications WHERE id = 'n-1'`).Scan(&notBefore); err != nil || notBefore != nil {
		t.Fatalf("not_before = %v (%v)", notBefore, err)
	}
	for _, table := range []string{"installation_diagnostics", "notification_reminders"} {
		var n int
		if err := db.Reader().QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s: %d (%v)", table, n, err)
		}
	}
	// The diagnostics row goes with its installation.
	if _, err := db.Writer().ExecContext(ctx, `INSERT INTO installation_diagnostics (installation_id, reported_at, created_at, updated_at)
		VALUES ('i-1', 2000, 2000, 2000)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Writer().ExecContext(ctx, `DELETE FROM app_installations WHERE id = 'i-1'`); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := db.Reader().QueryRow(`SELECT COUNT(*) FROM installation_diagnostics`).Scan(&left); err != nil || left != 0 {
		t.Fatalf("diagnostics after the installation went: %d (%v)", left, err)
	}
}
