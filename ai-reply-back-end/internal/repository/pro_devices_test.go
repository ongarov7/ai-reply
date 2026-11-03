package repository

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/migrations"
)

func proTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pro.sqlite")
	db, err := database.Open(database.Options{Path: path, BusyTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := database.Migrate(context.Background(), db, migrations.FS); err != nil {
		t.Fatal(err)
	}
	return New(db), path
}

func proTestUser(t *testing.T, s *Store, id string) {
	t.Helper()
	_, err := s.CreateUser(context.Background(), domain.User{ID: id, Email: id + "@example.test", Status: domain.UserActive, Kind: "account", Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
}

func proTestDevice(t *testing.T, s *Store, user, id, platform string) {
	t.Helper()
	_, err := s.UpsertDevice(context.Background(), domain.Device{ID: id, UserID: user, Platform: platform})
	if err != nil {
		t.Fatal(err)
	}
}

func TestProFirstDevicePerCategoryWins(t *testing.T) {
	for _, platforms := range [][2]string{{"ios", "android"}, {"android", "ios"}, {"ios", "ios"}, {"macos", "windows"}, {"windows", "macos"}, {"macos", "macos"}, {"ipados", "ios"}} {
		t.Run(platforms[0]+"_"+platforms[1], func(t *testing.T) {
			s, _ := proTestStore(t)
			proTestUser(t, s, "user")
			proTestDevice(t, s, "user", "first", platforms[0])
			proTestDevice(t, s, "user", "second", platforms[1])
			ctx, now := context.Background(), time.Now()
			a, created, err := s.ClaimProDevice(ctx, "user", "first", proCategory(platforms[0]), platforms[0], "Phone", now)
			if err != nil || !created {
				t.Fatalf("first: %v, %v", created, err)
			}
			again, created, err := s.ClaimProDevice(ctx, "user", "first", proCategory(platforms[0]), platforms[0], "Phone", now)
			if err != nil || created || again.ID != a.ID {
				t.Fatalf("repeat: %v, %v", created, err)
			}
			if _, _, err := s.ClaimProDevice(ctx, "user", "second", proCategory(platforms[1]), platforms[1], "Other", now); !errors.Is(err, domain.ErrProDeviceLimit) {
				t.Fatalf("second: %v", err)
			}
			list, err := s.ProDeviceAuthorizations(ctx, "user")
			if err != nil || len(list) != 1 || list[0].DeviceID != "first" {
				t.Fatalf("original displaced: %#v, %v", list, err)
			}
		})
	}
}

func TestProMobileAndDesktopIndependent(t *testing.T) {
	s, _ := proTestStore(t)
	proTestUser(t, s, "user")
	for _, platform := range []string{"android", "windows"} {
		proTestDevice(t, s, "user", platform, platform)
		if _, created, err := s.ClaimProDevice(context.Background(), "user", platform, proCategory(platform), platform, platform, time.Now()); err != nil || !created {
			t.Fatalf("%s: %v, %v", platform, created, err)
		}
	}
}

func TestProRacingClaimsAcrossSQLiteConnections(t *testing.T) {
	for _, platform := range []string{"ios", "windows"} {
		t.Run(platform, func(t *testing.T) {
			s, path := proTestStore(t)
			proTestUser(t, s, "user")
			proTestDevice(t, s, "user", "one", platform)
			proTestDevice(t, s, "user", "two", platform)
			db2, err := database.Open(database.Options{Path: path, BusyTimeout: 5 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			defer db2.Close()
			stores := []*Store{s, New(db2)}
			var wg sync.WaitGroup
			var successes, limits atomic.Int64
			start := make(chan struct{})
			for i, id := range []string{"one", "two"} {
				wg.Add(1)
				go func(store *Store, id string) {
					defer wg.Done()
					<-start
					_, _, err := store.ClaimProDevice(context.Background(), "user", id, proCategory(platform), platform, "", time.Now())
					switch {
					case err == nil:
						successes.Add(1)
					case errors.Is(err, domain.ErrProDeviceLimit):
						limits.Add(1)
					default:
						t.Errorf("claim: %v", err)
					}
				}(stores[i], id)
			}
			close(start)
			wg.Wait()
			if successes.Load() != 1 || limits.Load() != 1 {
				t.Fatalf("successes=%d limits=%d", successes.Load(), limits.Load())
			}
		})
	}
}

func TestProRevocationTombstoneSessionRevocationAndReplacement(t *testing.T) {
	s, _ := proTestStore(t)
	proTestUser(t, s, "user")
	proTestDevice(t, s, "user", "original", "ios")
	proTestDevice(t, s, "user", "replacement", "android")
	ctx, now := context.Background(), time.Now()
	a, _, err := s.ClaimProDevice(ctx, "user", "original", "mobile", "ios", "Original", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRefreshToken(ctx, RefreshToken{UserID: "user", DeviceID: "original", TokenHash: "test-hash", ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeProDevice(ctx, "user", a.ID, "replacement", "replacement", false, now); !errors.Is(err, domain.ErrProDeviceManagementForbidden) {
		t.Fatalf("blocked device evicted owner: %v", err)
	}
	if err := s.RevokeProDevice(ctx, "user", a.ID, "original", "user release", false, now); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeProDevice(ctx, "user", a.ID, "original", "retry release", false, now); err != nil {
		t.Fatalf("idempotent release: %v", err)
	}
	var active int
	if err := s.db.Reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM refresh_tokens WHERE device_id='original' AND revoked_at IS NULL`).Scan(&active); err != nil || active != 0 {
		t.Fatalf("active sessions=%d err=%v", active, err)
	}
	// Legacy login revives the device row; the policy tombstone still wins.
	proTestDevice(t, s, "user", "original", "ios")
	if _, _, err := s.ClaimProDevice(ctx, "user", "original", "mobile", "ios", "", now); !errors.Is(err, domain.ErrProDeviceRevoked) {
		t.Fatalf("revoked original regained access: %v", err)
	}
	if _, _, err := s.ClaimProDevice(ctx, "user", "replacement", "mobile", "android", "", now); err != nil {
		t.Fatalf("replacement: %v", err)
	}
	list, _ := s.ProDeviceAuthorizations(ctx, "user")
	if len(list) != 2 {
		t.Fatalf("history lost: %#v", list)
	}
}

func TestProClaimRejectsOwnershipPlatformAndRevocation(t *testing.T) {
	s, _ := proTestStore(t)
	proTestUser(t, s, "user")
	proTestUser(t, s, "other")
	proTestDevice(t, s, "user", "device", "ios")
	for _, in := range [][4]string{{"other", "device", "mobile", "ios"}, {"user", "missing", "mobile", "ios"}, {"user", "device", "desktop", "windows"}, {"user", "device", "desktop", "ios"}} {
		if _, _, err := s.ClaimProDevice(context.Background(), in[0], in[1], in[2], in[3], "", time.Now()); !errors.Is(err, domain.ErrProDeviceIdentity) {
			t.Fatalf("invalid identity %#v: %v", in, err)
		}
	}
	if err := s.RevokeDevice(context.Background(), "user", "device"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ClaimProDevice(context.Background(), "user", "device", "mobile", "ios", "", time.Now()); !errors.Is(err, domain.ErrProDeviceIdentity) {
		t.Fatalf("revoked device claim: %v", err)
	}
}

func TestProAuthorizationAuditFailureRollsBack(t *testing.T) {
	s, _ := proTestStore(t)
	proTestUser(t, s, "user")
	proTestDevice(t, s, "user", "device", "ios")
	ctx := context.Background()
	if _, err := s.db.Writer().ExecContext(ctx, `CREATE TRIGGER fail_pro_audit BEFORE INSERT ON pro_device_events BEGIN SELECT RAISE(ABORT,'test audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ClaimProDevice(ctx, "user", "device", "mobile", "ios", "", time.Now()); err == nil {
		t.Fatal("claim ignored audit failure")
	}
	list, err := s.ProDeviceAuthorizations(ctx, "user")
	if err != nil || len(list) != 0 {
		t.Fatalf("partial claim survived: %#v, %v", list, err)
	}
}

func TestProAdminAuditFailureRollsBackRevocation(t *testing.T) {
	s, _ := proTestStore(t)
	proTestUser(t, s, "user")
	proTestDevice(t, s, "user", "device", "ios")
	ctx, now := context.Background(), time.Now()
	a, _, err := s.ClaimProDevice(ctx, "user", "device", "mobile", "ios", "", now)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := s.CreateAdmin(ctx, domain.AdminUser{Email: "admin@example.test", PasswordHash: "test", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Writer().ExecContext(ctx, `CREATE TRIGGER fail_admin_audit BEFORE INSERT ON admin_audit_logs BEGIN SELECT RAISE(ABORT,'test audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeProDevice(ctx, "user", a.ID, admin.ID, "support recovery", true, now); err == nil {
		t.Fatal("revocation ignored audit failure")
	}
	stored, err := s.ProDeviceAuthorization(ctx, "user", "device")
	if err != nil || stored.RevokedAt != nil {
		t.Fatalf("partial revocation survived: %#v, %v", stored, err)
	}
	d, err := s.Device(ctx, "device")
	if err != nil || d.RevokedAt != nil {
		t.Fatalf("device row was revoked: %v", err)
	}
}

func TestProShadowObservationsDoNotClaimSlots(t *testing.T) {
	s, _ := proTestStore(t)
	proTestUser(t, s, "user")
	ctx, now := context.Background(), time.Now()
	for i, id := range []string{"first", "second"} {
		proTestDevice(t, s, "user", id, "ios")
		duplicate, err := s.ObserveProDevice(ctx, "user", id, "mobile", "ios", now)
		if err != nil || duplicate != (i == 1) {
			t.Fatalf("observe %s duplicate=%v err=%v", id, duplicate, err)
		}
	}
	for i := 0; i < 5; i++ {
		if _, err := s.ObserveProDevice(ctx, "user", "first", "mobile", "ios", now); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := s.db.Reader().QueryRowContext(ctx, `SELECT attempt_count FROM pro_device_observations WHERE device_id='first'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("observation write throttle: %d, %v", count, err)
	}
	list, err := s.ProDeviceAuthorizations(ctx, "user")
	if err != nil || len(list) != 0 {
		t.Fatalf("shadow assigned a winner: %#v, %v", list, err)
	}
}

func TestProMigrationIdempotencyAndRollback(t *testing.T) {
	s, _ := proTestStore(t)
	if applied, err := database.Migrate(context.Background(), s.db, migrations.FS); err != nil || len(applied) != 0 {
		t.Fatalf("repeat migrate: %#v, %v", applied, err)
	}
	before := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || e.Name() >= "0031_pro_device_limits.sql" {
			continue
		}
		body, err := fs.ReadFile(migrations.FS, e.Name())
		if err != nil {
			t.Fatal(err)
		}
		before[e.Name()] = &fstest.MapFile{Data: body}
	}
	db, err := database.Open(database.Options{Path: filepath.Join(t.TempDir(), "rollback.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := database.Migrate(context.Background(), db, before); err != nil {
		t.Fatal(err)
	}
	old := New(db)
	proTestUser(t, old, "retained")
	body, _ := fs.ReadFile(migrations.FS, "0031_pro_device_limits.sql")
	before["0031_pro_device_limits.sql"] = &fstest.MapFile{Data: append(body, []byte("\nINVALID SQL FOR ROLLBACK;\n")...)}
	if _, err := database.Migrate(context.Background(), db, before); err == nil {
		t.Fatal("broken migration succeeded")
	}
	var tables int
	if err := db.Reader().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('pro_device_authorizations','pro_device_events','pro_device_installations','pro_device_observations')`).Scan(&tables); err != nil || tables != 0 {
		t.Fatalf("partially migrated tables=%d err=%v", tables, err)
	}
	if _, err := old.UserByID(context.Background(), "retained"); err != nil {
		t.Fatalf("migration damaged existing user: %v", err)
	}
}

func proTestInstallation(t *testing.T, s *Store, userID, id, raw, platform string) {
	t.Helper()
	_, err := s.db.Writer().ExecContext(context.Background(), `INSERT INTO app_installations
 (id,installation_id,user_id,platform,first_seen_at,last_seen_at,created_at,updated_at)
 VALUES(?,?,?,?,?,?,?,?)`, id, raw, userID, platform, ms(time.Now()), ms(time.Now()), ms(time.Now()), ms(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
}

func TestProInstallationBindingPinsExistingIdentity(t *testing.T) {
	s, _ := proTestStore(t)
	proTestUser(t, s, "user")
	proTestUser(t, s, "other")
	proTestDevice(t, s, "user", "phone", "ios")
	proTestDevice(t, s, "user", "new-session-device", "ios")
	proTestDevice(t, s, "other", "other-phone", "ios")
	proTestInstallation(t, s, "user", "install-one", "raw-install-one", "ios")
	proTestInstallation(t, s, "user", "install-two", "raw-install-two", "ios")
	ctx := context.Background()
	if ready, err := s.ProDeviceIdentityReady(ctx, "user", "phone"); err != nil || ready {
		t.Fatalf("unbound device ready=%v err=%v", ready, err)
	}
	if _, err := s.ProDeviceRawInstallationID(ctx, "user", "phone"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unbound installation lookup: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := s.BindProDeviceInstallation(ctx, "user", "phone", "raw-install-one"); err != nil {
			t.Fatalf("bind/repeat: %v", err)
		}
	}
	if ready, err := s.ProDeviceIdentityReady(ctx, "user", "phone"); err != nil || !ready {
		t.Fatalf("bound device ready=%v err=%v", ready, err)
	}
	if raw, err := s.ProDeviceRawInstallationID(ctx, "user", "phone"); err != nil || raw != "raw-install-one" {
		t.Fatalf("bound raw lookup: %v", err)
	}
	if err := s.BindProDeviceInstallation(ctx, "user", "phone", "raw-install-two"); !errors.Is(err, domain.ErrProDeviceIdentity) {
		t.Fatalf("cloned ID relink accepted: %v", err)
	}
	if err := s.BindProDeviceInstallation(ctx, "user", "new-session-device", "raw-install-one"); !errors.Is(err, domain.ErrProDeviceIdentity) {
		t.Fatalf("duplicate installation consumed a second identity: %v", err)
	}
	if raw, err := s.ProDeviceRawInstallationID(ctx, "user", "phone"); err != nil || raw != "raw-install-one" {
		t.Fatalf("failed relink damaged original binding: %v", err)
	}
	if err := s.BindProDeviceInstallation(ctx, "other", "other-phone", "raw-install-one"); !errors.Is(err, domain.ErrProDeviceIdentity) {
		t.Fatalf("other owner bound installation: %v", err)
	}
	if d, err := s.DeviceByProInstallation(ctx, "user", "raw-install-one"); err != nil || d.ID != "phone" {
		t.Fatalf("canonical device recovery: %v", err)
	}
	if _, err := s.DeviceByProInstallation(ctx, "other", "raw-install-one"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("other user learned canonical device: %v", err)
	}
}

func TestProInstallationBindingSupportsAccountSwitchWithoutTransfer(t *testing.T) {
	s, _ := proTestStore(t)
	proTestUser(t, s, "first")
	proTestUser(t, s, "second")
	proTestDevice(t, s, "first", "first-device", "ios")
	proTestDevice(t, s, "second", "second-device", "ios")
	proTestInstallation(t, s, "first", "installation", "raw-shared-installation", "ios")
	ctx := context.Background()
	if err := s.BindProDeviceInstallation(ctx, "first", "first-device", "raw-shared-installation"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Writer().ExecContext(ctx, `UPDATE app_installations SET user_id='second' WHERE id='installation'`); err != nil {
		t.Fatal(err)
	}
	if err := s.BindProDeviceInstallation(ctx, "second", "second-device", "raw-shared-installation"); err != nil {
		t.Fatalf("second account could not bind its own device: %v", err)
	}
	if ready, err := s.ProDeviceIdentityReady(ctx, "first", "first-device"); err != nil || ready {
		t.Fatalf("prior account attachment remained ready=%v err=%v", ready, err)
	}
	if _, err := s.ProDeviceRawInstallationID(ctx, "first", "first-device"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("prior attachment still active: %v", err)
	}
	// Successful authentication may recover history, but does not transfer the
	// current push attachment. A later authenticated registration does that.
	if d, err := s.DeviceByProInstallation(ctx, "first", "raw-shared-installation"); err != nil || d.ID != "first-device" {
		t.Fatalf("first account history was replaced: %v", err)
	}
	if _, err := s.db.Writer().ExecContext(ctx, `UPDATE app_installations SET user_id='first' WHERE id='installation'`); err != nil {
		t.Fatal(err)
	}
	if err := s.BindProDeviceInstallation(ctx, "first", "first-device", "raw-shared-installation"); err != nil {
		t.Fatalf("returning account binding: %v", err)
	}
	if ready, err := s.ProDeviceIdentityReady(ctx, "first", "first-device"); err != nil || !ready {
		t.Fatalf("returning account ready=%v err=%v", ready, err)
	}
}

func TestProPushTargetsOnlyCurrentAuthorizedOwner(t *testing.T) {
	s, _ := proTestStore(t)
	proTestUser(t, s, "user")
	proTestUser(t, s, "other")
	ctx, now := context.Background(), time.Now()
	for _, id := range []string{"authorized", "blocked"} {
		proTestDevice(t, s, "user", id, "ios")
		proTestInstallation(t, s, "user", "install-"+id, "raw-"+id, "ios")
		if err := s.BindProDeviceInstallation(ctx, "user", id, "raw-"+id); err != nil {
			t.Fatal(err)
		}
	}
	a, _, err := s.ClaimProDevice(ctx, "user", "authorized", "mobile", "ios", "", now)
	if err != nil {
		t.Fatal(err)
	}
	targets, err := s.ProDevicePushTargets(ctx, "user", "blocked")
	if err != nil || len(targets) != 1 || targets[0] != "install-authorized" {
		t.Fatalf("targets=%v err=%v", targets, err)
	}
	targets, err = s.ProDevicePushTargets(ctx, "user", "authorized")
	if err != nil || len(targets) != 0 {
		t.Fatalf("blocked/self target included: %v, %v", targets, err)
	}
	if _, err := s.db.Writer().ExecContext(ctx, `UPDATE app_installations SET user_id='other' WHERE id='install-authorized'`); err != nil {
		t.Fatal(err)
	}
	if targets, err := s.ProDevicePushTargets(ctx, "user", "blocked"); err != nil || len(targets) != 0 {
		t.Fatalf("other account push leaked: %v, %v", targets, err)
	}
	if _, err := s.db.Writer().ExecContext(ctx, `UPDATE app_installations SET user_id='user' WHERE id='install-authorized'`); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeProDevice(ctx, "user", a.ID, "authorized", "release", false, now); err != nil {
		t.Fatal(err)
	}
	if targets, err := s.ProDevicePushTargets(ctx, "user", "blocked"); err != nil || len(targets) != 0 {
		t.Fatalf("revoked push target included: %v, %v", targets, err)
	}
}

func TestProPinnedPlatformAndWriteThrottle(t *testing.T) {
	s, _ := proTestStore(t)
	proTestUser(t, s, "user")
	proTestDevice(t, s, "user", "phone", "ios")
	ctx, now := context.Background(), time.Now()
	a, _, err := s.ClaimProDevice(ctx, "user", "phone", "mobile", "ios", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.TouchProDevice(ctx, "user", "phone", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	var last int64
	if err := s.db.Reader().QueryRowContext(ctx, `SELECT last_seen_at FROM pro_device_authorizations WHERE id=?`, a.ID).Scan(&last); err != nil || last != ms(now) {
		t.Fatalf("activity was written too frequently: %v", err)
	}
	if err := s.TouchProDevice(ctx, "user", "phone", now.Add(16*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.db.Reader().QueryRowContext(ctx, `SELECT last_seen_at FROM pro_device_authorizations WHERE id=?`, a.ID).Scan(&last); err != nil || last != ms(now.Add(16*time.Minute)) {
		t.Fatalf("activity was not updated: %v", err)
	}
	// Mutable legacy metadata must never move an existing identity between slots.
	proTestDevice(t, s, "user", "phone", "windows")
	if _, _, err := s.ClaimProDevice(ctx, "user", "phone", "desktop", "windows", "", now); !errors.Is(err, domain.ErrProDeviceIdentity) {
		t.Fatalf("existing identity claimed another category: %v", err)
	}
}

func TestProSQLiteWriterLockFailureDoesNotClaimSlot(t *testing.T) {
	s, path := proTestStore(t)
	proTestUser(t, s, "user")
	proTestDevice(t, s, "user", "phone", "ios")
	db2, err := database.Open(database.Options{Path: path, BusyTimeout: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	tx, err := s.db.Writer().BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, _, err := New(db2).ClaimProDevice(context.Background(), "user", "phone", "mobile", "ios", "", time.Now()); err == nil {
		t.Fatal("claim succeeded while another SQLite writer held the lock")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if list, err := s.ProDeviceAuthorizations(context.Background(), "user"); err != nil || len(list) != 0 {
		t.Fatalf("lock failure left partial authorization: %v, %v", list, err)
	}
	if _, _, err := New(db2).ClaimProDevice(context.Background(), "user", "phone", "mobile", "ios", "", time.Now()); err != nil {
		t.Fatalf("claim did not recover after writer released: %v", err)
	}
}

func TestProProtectedPlatformCannotBeChangedByLegacyRegistration(t *testing.T) {
	for _, protect := range []string{"authorization", "binding"} {
		t.Run(protect, func(t *testing.T) {
			s, _ := proTestStore(t)
			proTestUser(t, s, "user")
			proTestDevice(t, s, "user", "original", "ios")
			ctx, now := context.Background(), time.Now()
			if protect == "authorization" {
				if _, _, err := s.ClaimProDevice(ctx, "user", "original", "mobile", "ios", "", now); err != nil {
					t.Fatal(err)
				}
			} else {
				proTestInstallation(t, s, "user", "installation", "raw-original", "ios")
				if err := s.BindProDeviceInstallation(ctx, "user", "original", "raw-original"); err != nil {
					t.Fatal(err)
				}
			}
			returned, err := s.UpsertDevice(ctx, domain.Device{ID: "original", UserID: "user", Platform: "windows"})
			if err != nil {
				t.Fatal(err)
			}
			if returned.Platform != "ios" {
				t.Fatalf("session adopted rejected platform: %s", returned.Platform)
			}
			stored, err := s.Device(ctx, "original")
			if err != nil || stored.Platform != "ios" {
				t.Fatalf("canonical platform was overwritten: %s, %v", stored.Platform, err)
			}
			if protect == "binding" {
				if ready, err := s.ProDeviceIdentityReady(ctx, "user", "original"); err != nil || !ready {
					t.Fatalf("original installation lost readiness: %v, %v", ready, err)
				}
			}
		})
	}
}

func TestProLegacyRevokeCannotRaceFirstAuthorization(t *testing.T) {
	s, _ := proTestStore(t)
	proTestUser(t, s, "user")
	ctx := context.Background()
	for i := 0; i < 40; i++ {
		// Each iteration has an independent account/category slot so the race
		// includes a real first authorization rather than an existing-row read.
		user := "race-" + strconv.Itoa(i)
		deviceID := "device-" + strconv.Itoa(i)
		proTestUser(t, s, user)
		proTestDevice(t, s, user, deviceID, "ios")
		start := make(chan struct{})
		claim := make(chan error, 1)
		revoke := make(chan error, 1)
		go func() {
			<-start
			_, _, err := s.ClaimProDevice(ctx, user, deviceID, "mobile", "ios", "", time.Now())
			claim <- err
		}()
		go func() { <-start; revoke <- s.RevokeDevice(ctx, user, deviceID) }()
		close(start)
		claimErr, revokeErr := <-claim, <-revoke
		switch {
		case claimErr == nil:
			if !errors.Is(revokeErr, domain.ErrProDeviceManagementForbidden) {
				t.Fatalf("claim won, legacy revoke unexpectedly succeeded: %v", revokeErr)
			}
		case revokeErr == nil:
			if !errors.Is(claimErr, domain.ErrProDeviceIdentity) {
				t.Fatalf("revoke won, authorization unexpectedly succeeded: %v", claimErr)
			}
		default:
			t.Fatalf("neither mutation won: claim=%v revoke=%v", claimErr, revokeErr)
		}
		var broken int
		if err := s.db.Reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM pro_device_authorizations p
 JOIN devices d ON d.id=p.device_id WHERE p.user_id=? AND p.revoked_at IS NULL AND d.revoked_at IS NOT NULL`, user).Scan(&broken); err != nil || broken != 0 {
			t.Fatalf("active authorization left on a revoked device: %d, %v", broken, err)
		}
	}
}
