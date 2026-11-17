package repository

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/migrations"
)

func legacyAliasStore(t *testing.T) *Store {
	t.Helper()
	s, _ := proTestStore(t)
	proTestUser(t, s, "user")
	proTestDevice(t, s, "user", "original", "ios")
	proTestDevice(t, s, "user", "returning", "ios")
	proTestInstallation(t, s, "user", "installation", "raw-original-installation", "ios")
	if err := s.BindLegacyProDeviceInstallation(context.Background(), "user", "original", "raw-original-installation"); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestProLegacyAliasRecoversSameInstallationAndRevokesAllSessions(t *testing.T) {
	s := legacyAliasStore(t)
	ctx, now := context.Background(), time.Now()
	a, _, err := s.ClaimProDevice(ctx, "user", "original", "mobile", "ios", "", now)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := s.BindLegacyProDeviceInstallation(ctx, "user", "returning", "raw-original-installation"); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"original", "returning"} {
		if got, err := s.CanonicalProDeviceID(ctx, "user", id); err != nil || got != "original" {
			t.Fatalf("%s canonical=%q err=%v", id, got, err)
		}
		if _, err := s.CreateRefreshToken(ctx, RefreshToken{UserID: "user", DeviceID: id, TokenHash: "hash-" + id, ExpiresAt: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	// Compatibility aliases do not make a released session strict-iid ready.
	if _, err := s.ProDeviceRawInstallationID(ctx, "user", "returning"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("alias acquired strict identity: %v", err)
	}
	var links int
	if err := s.db.Reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM pro_device_events WHERE kind='legacy_session_linked'`).Scan(&links); err != nil || links != 1 {
		t.Fatalf("link audit count=%d err=%v", links, err)
	}
	if d, err := s.UpsertDevice(ctx, domain.Device{ID: "returning", UserID: "user", Platform: "windows"}); err != nil || d.Platform != "ios" {
		t.Fatalf("alias platform changed: %#v err=%v", d, err)
	}
	if err := s.RevokeDevice(ctx, "user", "returning"); !errors.Is(err, domain.ErrProDeviceManagementForbidden) {
		t.Fatalf("legacy route bypassed alias protection: %v", err)
	}
	if err := s.RevokeProDevice(ctx, "user", a.ID, "original", "release", false, now); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"original", "returning"} {
		d, err := s.Device(ctx, id)
		if err != nil || d.RevokedAt == nil {
			t.Fatalf("%s device remained active: %v", id, err)
		}
		r, err := s.RefreshTokenByHash(ctx, "hash-"+id)
		if err != nil || r.RevokedAt == nil || r.Reason != "pro_device_revoked" {
			t.Fatalf("%s refresh remained active: %#v %v", id, r, err)
		}
		// Existing login upsert clears the legacy device flag, but must never
		// clear the authorization/alias tombstones.
		proTestDevice(t, s, "user", id, "ios")
		if _, err := s.CanonicalProDeviceID(ctx, "user", id); !errors.Is(err, domain.ErrProDeviceRevoked) {
			t.Fatalf("%s login restored revoked slot: %v", id, err)
		}
	}
	if err := s.BindLegacyProDeviceInstallation(ctx, "user", "returning", "raw-original-installation"); !errors.Is(err, domain.ErrProDeviceRevoked) {
		t.Fatalf("revoked alias rebound: %v", err)
	}
}

func TestProLegacyAliasRejectsConflictingOrRevokedEvidence(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*testing.T, *Store)
	}{
		{"different owner", func(t *testing.T, s *Store) {
			proTestUser(t, s, "other")
			if _, err := s.db.Writer().Exec(`UPDATE app_installations SET user_id='other' WHERE id='installation'`); err != nil {
				t.Fatal(err)
			}
		}},
		{"different platform", func(t *testing.T, s *Store) {
			proTestDevice(t, s, "user", "returning", "android")
		}},
		{"existing authorization", func(t *testing.T, s *Store) {
			if _, _, err := s.ClaimProDevice(context.Background(), "user", "returning", "mobile", "ios", "", time.Now()); err != nil {
				t.Fatal(err)
			}
		}},
		{"conflicting binding", func(t *testing.T, s *Store) {
			proTestInstallation(t, s, "user", "unrelated", "raw-unrelated-installation", "ios")
			if err := s.BindProDeviceInstallation(context.Background(), "user", "returning", "raw-unrelated-installation"); err != nil {
				t.Fatal(err)
			}
		}},
		{"revoked canonical device", func(t *testing.T, s *Store) {
			if _, err := s.db.Writer().Exec(`UPDATE devices SET revoked_at=1 WHERE id='original'`); err != nil {
				t.Fatal(err)
			}
		}},
		{"canonical tombstone after login", func(t *testing.T, s *Store) {
			a, _, err := s.ClaimProDevice(context.Background(), "user", "original", "mobile", "ios", "", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if err := s.RevokeProDevice(context.Background(), "user", a.ID, "original", "release", false, time.Now()); err != nil {
				t.Fatal(err)
			}
			proTestDevice(t, s, "user", "original", "ios")
			if _, err := s.db.Writer().Exec(`UPDATE app_installations SET user_id='user' WHERE id='installation'`); err != nil {
				t.Fatal(err)
			}
		}},
		{"revoked source device", func(t *testing.T, s *Store) {
			if _, err := s.db.Writer().Exec(`UPDATE devices SET revoked_at=1 WHERE id='returning'`); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := legacyAliasStore(t)
			tc.edit(t, s)
			err := s.BindLegacyProDeviceInstallation(context.Background(), "user", "returning", "raw-original-installation")
			if !errors.Is(err, domain.ErrProDeviceIdentity) && !errors.Is(err, domain.ErrProDeviceRevoked) {
				t.Fatalf("conflict accepted or unexpected failure: %v", err)
			}
			var aliases int
			if err := s.db.Reader().QueryRow(`SELECT COUNT(*) FROM pro_device_session_aliases`).Scan(&aliases); err != nil || aliases != 0 {
				t.Fatalf("conflict left alias count=%d err=%v", aliases, err)
			}
		})
	}
}

func TestProLegacyAliasIsImmutableAndAuditAtomic(t *testing.T) {
	s := legacyAliasStore(t)
	ctx := context.Background()
	if _, err := s.db.Writer().ExecContext(ctx, `CREATE TRIGGER fail_alias_audit BEFORE INSERT ON pro_device_events
 WHEN NEW.kind='legacy_session_linked' BEGIN SELECT RAISE(ABORT,'test audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.BindLegacyProDeviceInstallation(ctx, "user", "returning", "raw-original-installation"); err == nil {
		t.Fatal("alias committed without audit")
	}
	if got, err := s.CanonicalProDeviceID(ctx, "user", "returning"); err != nil || got != "returning" {
		t.Fatalf("failed audit left alias: %q %v", got, err)
	}
	if _, err := s.db.Writer().ExecContext(ctx, `DROP TRIGGER fail_alias_audit`); err != nil {
		t.Fatal(err)
	}
	if err := s.BindLegacyProDeviceInstallation(ctx, "user", "returning", "raw-original-installation"); err != nil {
		t.Fatal(err)
	}
	proTestDevice(t, s, "user", "unrelated", "ios")
	proTestInstallation(t, s, "user", "unrelated", "raw-unrelated-installation", "ios")
	if err := s.BindProDeviceInstallation(ctx, "user", "unrelated", "raw-unrelated-installation"); err != nil {
		t.Fatal(err)
	}
	if err := s.BindLegacyProDeviceInstallation(ctx, "user", "returning", "raw-unrelated-installation"); !errors.Is(err, domain.ErrProDeviceIdentity) {
		t.Fatalf("alias retargeted: %v", err)
	}
	if _, err := s.db.Writer().ExecContext(ctx, `UPDATE pro_device_session_aliases SET canonical_device_id='unrelated' WHERE device_id='returning'`); err == nil {
		t.Fatal("database allowed alias identity mutation")
	}
	if got, err := s.CanonicalProDeviceID(ctx, "user", "returning"); err != nil || got != "original" {
		t.Fatalf("original alias changed: %q %v", got, err)
	}
	if _, _, err := s.ClaimProDevice(ctx, "user", "returning", "mobile", "ios", "", time.Now()); !errors.Is(err, domain.ErrProDeviceIdentity) {
		t.Fatalf("stale pre-alias request claimed its own slot: %v", err)
	}
	if _, err := s.CanonicalProDeviceID(ctx, "other", "returning"); !errors.Is(err, domain.ErrProDeviceIdentity) {
		t.Fatalf("foreign session resolved: %v", err)
	}
}

func TestProLegacyAliasRevocationAuditFailureRollsBackCascade(t *testing.T) {
	s := legacyAliasStore(t)
	ctx, now := context.Background(), time.Now()
	if err := s.BindLegacyProDeviceInstallation(ctx, "user", "returning", "raw-original-installation"); err != nil {
		t.Fatal(err)
	}
	a, _, err := s.ClaimProDevice(ctx, "user", "original", "mobile", "ios", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRefreshToken(ctx, RefreshToken{UserID: "user", DeviceID: "returning", TokenHash: "hash-returning", ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Writer().ExecContext(ctx, `CREATE TRIGGER fail_revoke_audit BEFORE INSERT ON pro_device_events
 WHEN NEW.kind='revoked' BEGIN SELECT RAISE(ABORT,'test audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeProDevice(ctx, "user", a.ID, "original", "release", false, now); err == nil {
		t.Fatal("revocation committed without audit")
	}
	if got, err := s.CanonicalProDeviceID(ctx, "user", "returning"); err != nil || got != "original" {
		t.Fatalf("failed revocation damaged alias: %q %v", got, err)
	}
	if r, err := s.RefreshTokenByHash(ctx, "hash-returning"); err != nil || r.RevokedAt != nil {
		t.Fatalf("failed revocation closed refresh: %#v %v", r, err)
	}
}

func TestProLegacyAliasWithoutAuthorizationKeepsLegacyDeviceRemoval(t *testing.T) {
	for _, target := range []string{"original", "returning"} {
		t.Run(target, func(t *testing.T) {
			s := legacyAliasStore(t)
			ctx := context.Background()
			if err := s.BindLegacyProDeviceInstallation(ctx, "user", "returning", "raw-original-installation"); err != nil {
				t.Fatal(err)
			}
			if err := s.RevokeDevice(ctx, "user", target); err != nil {
				t.Fatalf("non-Pro device removal rejected: %v", err)
			}
			if d, err := s.Device(ctx, target); err != nil || d.RevokedAt == nil {
				t.Fatalf("non-Pro device remained active: %#v %v", d, err)
			}
		})
	}
}

func TestProLegacyAliasMigrationFrom0040PreservesExistingOwner(t *testing.T) {
	ctx := context.Background()
	before := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() >= "0041_pro_device_session_aliases.sql" {
			continue
		}
		body, err := fs.ReadFile(migrations.FS, entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		before[entry.Name()] = &fstest.MapFile{Data: body}
	}
	db, err := database.Open(database.Options{Path: filepath.Join(t.TempDir(), "before-0041.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := database.Migrate(ctx, db, before); err != nil {
		t.Fatal(err)
	}
	s := New(db)
	proTestUser(t, s, "existing")
	if _, err := db.Writer().ExecContext(ctx, `INSERT INTO devices(id,user_id,platform,created_at,last_seen_at)
 VALUES('existing-device','existing','ios',1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Writer().ExecContext(ctx, `INSERT INTO pro_device_authorizations
 (id,user_id,device_id,category,platform,first_authorized_at,last_seen_at)
 VALUES('existing-owner','existing','existing-device','mobile','ios',1,1)`); err != nil {
		t.Fatal(err)
	}
	// 0041 runs first; later migrations may follow it in the same upgrade.
	if applied, err := database.Migrate(ctx, db, migrations.FS); err != nil || len(applied) == 0 || applied[0] != "0041_pro_device_session_aliases.sql" {
		t.Fatalf("0040 upgrade=%v err=%v", applied, err)
	}
	if applied, err := database.Migrate(ctx, db, migrations.FS); err != nil || len(applied) != 0 {
		t.Fatalf("repeat migration=%v err=%v", applied, err)
	}
	if a, err := s.ProDeviceAuthorization(ctx, "existing", "existing-device"); err != nil || a.ID != "existing-owner" || a.RevokedAt != nil {
		t.Fatalf("migration changed existing owner: %#v %v", a, err)
	}
	var aliases int
	if err := db.Reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM pro_device_session_aliases`).Scan(&aliases); err != nil || aliases != 0 {
		t.Fatalf("migration inferred aliases=%d err=%v", aliases, err)
	}
	rows, err := db.Reader().QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() || rows.Err() != nil {
		t.Fatalf("foreign key check failed: %v", rows.Err())
	}
}
