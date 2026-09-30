package apptest

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/migrations"
)

// 0005 бар дерекқорға қолданылады: қолданушылар, олардың ID-і мен кіру тәсілдері сақталады.
func TestAuthProvidersMigrationKeepsExistingAccounts(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(database.Options{Path: filepath.Join(t.TempDir(), "old.db"), MaxReadConns: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// The schema as production has it today: every migration before 0005.
	before := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") && e.Name() < "0005" {
			body, _ := fs.ReadFile(migrations.FS, e.Name())
			before[e.Name()] = &fstest.MapFile{Data: body}
		}
	}
	if _, err := database.Migrate(ctx, db, before); err != nil {
		t.Fatalf("old migrations: %v", err)
	}

	seed := []string{
		`INSERT INTO users (id, phone, status, created_at, updated_at) VALUES ('u-phone', '+77011112233', 'active', 1000, 1000)`,
		`INSERT INTO auth_identities (id, user_id, kind, value, country, verified_at, created_at)
		 VALUES ('i-phone', 'u-phone', 'phone', '+77011112233', 'KZ', 1000, 1000)`,
		`INSERT INTO users (id, email, status, created_at, updated_at) VALUES ('u-email', 'old@example.com', 'active', 2000, 2000)`,
		`INSERT INTO auth_identities (id, user_id, kind, value, verified_at, created_at)
		 VALUES ('i-email', 'u-email', 'email', 'old@example.com', 2500, 2000)`,
		`INSERT INTO users (id, status, kind, legacy_client, created_at, updated_at)
		 VALUES ('u-legacy', 'active', 'legacy_install', 'abc123', 3000, 3000)`,
		`INSERT INTO otp_codes (id, identity_kind, identity_value, code_hash, expires_at, created_at)
		 VALUES ('o-1', 'phone', '+77011112233', 'hash', 9000, 4000)`,
	}
	for _, stmt := range seed {
		if _, err := db.Writer().ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	applied, err := database.Migrate(ctx, db, migrations.FS)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if len(applied) == 0 || applied[0] != "0005_auth_providers.sql" {
		t.Fatalf("applied = %v", applied)
	}
	for _, name := range applied[1:] {
		if name <= "0005_auth_providers.sql" {
			t.Fatalf("applied out of order: %v", applied)
		}
	}

	var users int
	if err := db.Reader().QueryRow(`SELECT COUNT(*) FROM users`).Scan(&users); err != nil || users != 3 {
		t.Fatalf("users = %d (%v)", users, err)
	}
	var verifiedAt int64
	if err := db.Reader().QueryRow(`SELECT email_verified_at FROM users WHERE id = 'u-email'`).Scan(&verifiedAt); err != nil || verifiedAt != 2500 {
		t.Fatalf("email_verified_at = %d (%v), want the identity's verification time", verifiedAt, err)
	}
	var providerEmail string
	var updated int64
	if err := db.Reader().QueryRow(`SELECT provider_email, updated_at FROM auth_identities WHERE id = 'i-email'`).
		Scan(&providerEmail, &updated); err != nil || providerEmail != "old@example.com" || updated != 2000 {
		t.Fatalf("identity = %q %d (%v)", providerEmail, updated, err)
	}
	var purpose string
	if err := db.Reader().QueryRow(`SELECT purpose FROM otp_codes WHERE id = 'o-1'`).Scan(&purpose); err != nil || purpose != "login" {
		t.Fatalf("purpose = %q (%v)", purpose, err)
	}

	store := repository.New(db)
	byEmail, err := store.UserByAuthIdentity(ctx, domain.IdentityEmail, "old@example.com")
	if err != nil || byEmail.ID != "u-email" {
		t.Fatalf("existing e-mail account must stay reachable: %v %v", byEmail.ID, err)
	}
	byPhone, err := store.UserByAuthIdentity(ctx, domain.IdentityPhone, "+77011112233")
	if err != nil || byPhone.ID != "u-phone" {
		t.Fatalf("existing phone account must stay reachable: %v %v", byPhone.ID, err)
	}

	// Running again is a no-op.
	if again, err := database.Migrate(ctx, db, migrations.FS); err != nil || len(again) != 0 {
		t.Fatalf("second run applied %v (%v)", again, err)
	}
}
