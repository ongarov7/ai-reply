package apptest

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/migrations"
)

// Historical migration tests pin their target version; later business-model migrations
// intentionally supersede defaults and must be verified independently.
func migrationsBefore(t *testing.T, before string) fstest.MapFS {
	t.Helper()
	out := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".sql") && entry.Name() < before {
			body, err := fs.ReadFile(migrations.FS, entry.Name())
			if err != nil {
				t.Fatal(err)
			}
			out[entry.Name()] = &fstest.MapFile{Data: body}
		}
	}
	return out
}

func TestApprovedEntitlementMigrationPreservesExistingBalancesAndPaidContract(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(database.Options{Path: filepath.Join(t.TempDir(), "existing.db"), MaxReadConns: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := database.Migrate(ctx, db, migrationsBefore(t, "0022")); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`INSERT INTO users(id,email,status,kind,created_at,updated_at) VALUES ('existing','existing@example.com','active','account',1000,1000)`,
		`INSERT INTO credit_grants(id,user_id,kind,units_total,units_remaining,starts_at,status,created_at,updated_at) VALUES ('welcome','existing','welcome',20,7,1000,'active',1000,1000)`,
		`INSERT INTO credit_grants(id,user_id,kind,units_total,units_remaining,starts_at,status,created_at,updated_at) VALUES ('purchased','existing','pack',60,49,1000,'active',1000,1000)`,
		`INSERT INTO usage_daily(user_id,usage_date,used,updated_at) VALUES ('existing','2026-10-23',7,1000)`,
		`INSERT INTO subscriptions(id,user_id,plan_id,status,source,started_at,expires_at,created_at,updated_at) SELECT 'legacy-pro','existing',id,'active','payment',1000,9999999999999,1000,1000 FROM plans WHERE code='pro'`,
	} {
		if _, err := db.Writer().ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	recentAccountAt := time.Now().UnixMilli()
	if _, err := db.Writer().ExecContext(ctx, `INSERT INTO users(id,email,status,kind,created_at,updated_at) VALUES ('recent','recent@example.com','active','account',?,?)`, recentAccountAt, recentAccountAt); err != nil {
		t.Fatal(err)
	}
	applied, err := database.Migrate(ctx, db, migrations.FS)
	if err != nil || len(applied) < 1 || applied[0] != "0022_ios_entitlements.sql" {
		t.Fatalf("migration: %v %v", applied, err)
	}
	repo := repository.New(db)
	free, err := repo.PlanByCode(ctx, "free")
	if err != nil {
		t.Fatal(err)
	}
	if free.DailyLimit != 0 || free.EntitlementKind != domain.EntitlementWelcome {
		t.Fatalf("free: %+v", free)
	}
	balance, err := repo.CreditBalance(ctx, "existing", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if balance.WelcomeTotal != 20 || balance.WelcomeRemaining != 7 || balance.Packs != 49 {
		t.Fatalf("existing credits changed: %+v", balance)
	}
	legacy, err := repo.CurrentSubscription(ctx, "existing")
	if err != nil || legacy.ID != "legacy-pro" || !legacy.IsUsable(time.Now()) {
		t.Fatalf("legacy contract changed: %+v %v", legacy, err)
	}
	legacyPlan, err := repo.PlanByCode(ctx, "pro")
	if err != nil || legacyPlan.DailyLimit != 50 || legacyPlan.Price != 349000 {
		t.Fatalf("legacy paid plan changed: %+v %v", legacyPlan, err)
	}
	var units string
	if err := db.Reader().QueryRow(`SELECT value FROM system_settings WHERE key='welcome_bonus_units'`).Scan(&units); err != nil || units != "10" {
		t.Fatalf("new allowance %q %v", units, err)
	}
	var cutoff int64
	if err := db.Reader().QueryRow(`SELECT CAST(value AS INTEGER) FROM system_settings WHERE key='welcome_bonus_since'`).Scan(&cutoff); err != nil || cutoff <= recentAccountAt {
		t.Fatal("missing release cutoff")
	}
	if again, err := database.Migrate(ctx, db, migrations.FS); err != nil || len(again) != 0 {
		t.Fatalf("rerun: %v %v", again, err)
	}
	var count int
	if err := db.Reader().QueryRow(`SELECT COUNT(*) FROM credit_grants WHERE user_id='existing'`).Scan(&count); err != nil || count != 2 {
		t.Fatal("migration regranted credits")
	}
	for code, price := range map[string]int64{"base_monthly": 59900, "base_yearly": 599900, "pro_monthly": 299900, "pro_yearly": 2999000} {
		p, err := repo.PlanByCode(ctx, code)
		if err != nil || p.Price != price {
			t.Fatalf("approved plan %s: %+v %v", code, p, err)
		}
	}
}
