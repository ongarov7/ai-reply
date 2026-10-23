package apptest

import (
	"context"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/migrations"
)

func TestFreeDailyLimitUpgradeRepairsExistingDataAndKeepsAdminEdits(t *testing.T) {
	ctx := context.Background()
	before := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".sql") && entry.Name() < "0021" {
			body, err := fs.ReadFile(migrations.FS, entry.Name())
			if err != nil {
				t.Fatal(err)
			}
			before[entry.Name()] = &fstest.MapFile{Data: body}
		}
	}
	for _, custom := range []bool{false, true} {
		name := "known multilingual boilerplate"
		if custom {
			name = "custom admin copy"
		}
		t.Run(name, func(t *testing.T) {
			db, err := database.Open(database.Options{Path: filepath.Join(t.TempDir(), "existing.db"), MaxReadConns: 2})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			if _, err := database.Migrate(ctx, db, before); err != nil {
				t.Fatal(err)
			}
			old := map[string]string{"kk": "Күніне 20 жауап. Картасыз бастаңыз.", "ru": "20 ответов в день. Начните без карты.", "en": "20 replies per day. Start without a card.", "uz": "Kuniga 20 ta javob. Kartasiz boshlang."}
			want := map[string]string{"kk": "Күніне 7 жауап. Картасыз бастаңыз.", "ru": "7 ответов в день. Начните без карты.", "en": "7 replies per day. Start without a card.", "uz": "Kuniga 7 ta javob. Kartasiz boshlang."}
			if custom {
				old = map[string]string{"kk": "Арнайы 20 бөлім", "ru": "Особые условия 20", "en": "20 custom benefits, edited by admin", "uz": "20 maxsus shart"}
				want = old
			}
			if _, err := db.Writer().ExecContext(ctx, `UPDATE plans SET daily_message_limit=20, name_ru='Мой бесплатный 20',
    description_kk=?, description_ru=?, description_en=?, description_uz=?, monthly_message_limit=100,updated_at=1234 WHERE code='free'`, old["kk"], old["ru"], old["en"], old["uz"]); err != nil {
				t.Fatal(err)
			}
			// A paid plan can independently have the same limit and copy; do not change it.
			if _, err := db.Writer().ExecContext(ctx, `UPDATE plans SET daily_message_limit=20,description_ru='20 ответов в день. Начните без карты.',updated_at=4321 WHERE code='standard'`); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Writer().ExecContext(ctx, `UPDATE system_settings SET value='20' WHERE key='welcome_bonus_units'`); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Writer().ExecContext(ctx, `INSERT INTO users(id,email,status,kind,created_at,updated_at) VALUES('migration-free-user','migration-free@example.com','active','account',1000,1000)`); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Writer().ExecContext(ctx, `INSERT INTO usage_daily(user_id,usage_date,used,updated_at) VALUES('migration-free-user','2026-10-23',9,1000)`); err != nil {
				t.Fatal(err)
			}
			store := repository.New(db)
			freeBefore, err := store.PlanByCode(ctx, "free")
			if err != nil {
				t.Fatal(err)
			}
			paidBefore, err := store.PlanByCode(ctx, "standard")
			if err != nil {
				t.Fatal(err)
			}
			applied, err := database.Migrate(ctx, db, migrations.FS)
			if err != nil || len(applied) != 1 || applied[0] != "0021_free_daily_limit_7.sql" {
				t.Fatalf("upgrade applied=%v err=%v", applied, err)
			}
			freeAfter, err := store.PlanByCode(ctx, "free")
			if err != nil {
				t.Fatal(err)
			}
			if freeAfter.DailyLimit != 7 || !reflect.DeepEqual(freeAfter.Description, want) {
				t.Fatalf("free plan: limit=%d description=%v", freeAfter.DailyLimit, freeAfter.Description)
			}
			if !reflect.DeepEqual(freeAfter.Name, freeBefore.Name) || freeAfter.MonthlyLimit != 100 || freeAfter.IsActive != freeBefore.IsActive || freeAfter.IsVisible != freeBefore.IsVisible {
				t.Fatal("unrelated free plan fields changed")
			}
			paidAfter, err := store.PlanByCode(ctx, "standard")
			if err != nil || !reflect.DeepEqual(paidAfter, paidBefore) {
				t.Fatal("paid plan changed")
			}
			var bonus string
			if err := db.Reader().QueryRowContext(ctx, `SELECT value FROM system_settings WHERE key='welcome_bonus_units'`).Scan(&bonus); err != nil || bonus != "20" {
				t.Fatalf("welcome bonus changed: %q err=%v", bonus, err)
			}
			var used int
			if err := db.Reader().QueryRowContext(ctx, `SELECT used FROM usage_daily WHERE user_id='migration-free-user'`).Scan(&used); err != nil || used != 9 {
				t.Fatalf("usage reset: %d err=%v", used, err)
			}
			// Reapplying the SQL itself is a no-op once the desired state is reached.
			body, err := fs.ReadFile(migrations.FS, "0021_free_daily_limit_7.sql")
			if err != nil {
				t.Fatal(err)
			}
			result, err := db.Writer().ExecContext(ctx, string(body))
			if err != nil {
				t.Fatal(err)
			}
			if affected, _ := result.RowsAffected(); affected != 0 {
				t.Fatalf("SQL is not idempotent: affected=%d", affected)
			}
			// A later admin edit must remain authoritative on every subsequent startup.
			if _, err := db.Writer().ExecContext(ctx, `UPDATE plans SET daily_message_limit=12,description_en='My updated plan' WHERE code='free'`); err != nil {
				t.Fatal(err)
			}
			if again, err := database.Migrate(ctx, db, migrations.FS); err != nil || len(again) != 0 {
				t.Fatalf("second startup=%v err=%v", again, err)
			}
			final, err := store.PlanByCode(ctx, "free")
			if err != nil || final.DailyLimit != 12 || final.Description["en"] != "My updated plan" {
				t.Fatal("later admin edit was reset")
			}
		})
	}
}
