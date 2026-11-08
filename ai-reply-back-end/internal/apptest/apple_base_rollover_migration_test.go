package apptest

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/internal/repository"
)

func TestAppleBaseRolloverMigrationPreservesExistingBalanceAndSpentReplies(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(database.Options{Path: filepath.Join(t.TempDir(), "before-rollover.db"), MaxReadConns: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err = database.Migrate(ctx, db, migrationsBefore(t, "0036")); err != nil {
		t.Fatal(err)
	}
	loc, err := time.LoadLocation("Asia/Almaty")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 6, 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 2)
	signed := start.Add(time.Minute)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Writer().ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("%s: %v", query, err)
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
	exec(`INSERT INTO users(id,email,status,created_at,updated_at) VALUES('existing','existing@example.com','active',1000,1000)`)
	exec(`INSERT INTO credit_grants(id,user_id,kind,units_total,units_remaining,starts_at,status,created_at,updated_at)
 VALUES('welcome-existing','existing','welcome',20,7,1000,'active',1000,1000)`)
	exec(`INSERT INTO credit_grants(id,user_id,kind,units_total,units_remaining,starts_at,status,created_at,updated_at)
 VALUES('pack-existing','existing','pack',60,49,1000,'active',1000,1000)`)
	exec(`INSERT INTO usage_daily(user_id,usage_date,used,updated_at) VALUES('existing',?,3,?)`, start.Format("2006-01-02"), start.UnixMilli())
	exec(`INSERT INTO usage_monthly(user_id,usage_month,used,updated_at) VALUES('existing',?,3,?)`, start.Format("2006-01"), start.UnixMilli())
	// Only the latest signed period was retained before 0036. An older purchase
	// record must not be mistaken for evidence of another exact paid interval.
	for _, purchase := range []struct {
		id, transaction string
		at              time.Time
	}{
		{"old-purchase", "old-transaction", start.AddDate(0, -1, 0)},
		{"latest-purchase", "latest-transaction", start},
	} {
		exec(`INSERT INTO iap_purchases(id,user_id,provider,environment,store_product_id,product_code,
 transaction_id,original_transaction_id,status,purchased_at,created_at,updated_at)
 VALUES(?,'existing','app_store','Production','kz.aireply.base.monthly','base_monthly',?,'original','granted',?,?,?)`,
			purchase.id, purchase.transaction, purchase.at.UnixMilli(), purchase.at.UnixMilli(), purchase.at.UnixMilli())
	}
	exec(`INSERT INTO apple_subscriptions(original_transaction_id,user_id,transaction_id,product_id,
 plan_id,environment,purchased_at,expires_at,signed_at,auto_renew,updated_at)
 SELECT 'original','existing','latest-transaction','kz.aireply.base.monthly',id,'Production',?,?,?,0,?
 FROM plans WHERE code='base_monthly'`, start.UnixMilli(), end.UnixMilli(), signed.UnixMilli(), signed.UnixMilli())

	assertExisting := func() {
		t.Helper()
		for query, want := range map[string]int{
			`SELECT COUNT(*) FROM users WHERE id='existing'`:                        1,
			`SELECT units_remaining FROM credit_grants WHERE id='welcome-existing'`: 7,
			`SELECT units_remaining FROM credit_grants WHERE id='pack-existing'`:    49,
			`SELECT used FROM usage_daily WHERE user_id='existing'`:                 3,
			`SELECT used FROM usage_monthly WHERE user_id='existing'`:               3,
			`SELECT COUNT(*) FROM usage_reservations WHERE user_id='existing'`:      0,
			`SELECT COUNT(*) FROM iap_purchases WHERE user_id='existing'`:           2,
		} {
			if got := scalar(query); got != want {
				t.Fatalf("%s = %d, want %d", query, got, want)
			}
		}
	}
	target := migrationsBefore(t, "0037")
	applied, err := database.Migrate(ctx, db, target)
	if err != nil || len(applied) != 1 || applied[0] != "0036_apple_base_rollover.sql" {
		t.Fatalf("migrate: applied=%v err=%v", applied, err)
	}
	assertExisting()
	if scalar(`SELECT COUNT(*) FROM credit_grants`) != 2 || scalar(`SELECT COUNT(*) FROM apple_base_credit_periods`) != 1 ||
		scalar(`SELECT COUNT(*) FROM apple_base_credit_days`) != 0 || scalar(`SELECT COUNT(*) FROM apple_base_legacy_charges`) != 0 {
		t.Fatal("migration must seed only the known latest period without minting or spending replies")
	}
	var purchaseID, userID, original string
	var startsAt, expiresAt, signedAt int64
	var units, upgraded int
	var capAt, revokedAt sql.NullInt64
	err = db.Reader().QueryRowContext(ctx, `SELECT purchase_id,user_id,original_transaction_id,starts_at,
 expires_at,daily_units,signed_at,accrual_cap_at,upgraded,revoked_at FROM apple_base_credit_periods`).Scan(
		&purchaseID, &userID, &original, &startsAt, &expiresAt, &units, &signedAt, &capAt, &upgraded, &revokedAt)
	if err != nil || purchaseID != "latest-purchase" || userID != "existing" || original != "original" ||
		startsAt != start.UnixMilli() || expiresAt != end.UnixMilli() || signedAt != signed.UnixMilli() ||
		units != 40 || capAt.Valid || upgraded != 0 || revokedAt.Valid {
		t.Fatalf("seeded period does not preserve exact latest Apple state: purchase=%q user=%q original=%q start=%d end=%d signed=%d units=%d cap=%v upgraded=%d revoked=%v err=%v",
			purchaseID, userID, original, startsAt, expiresAt, signedAt, units, capAt, upgraded, revokedAt, err)
	}
	if again, err := database.Migrate(ctx, db, target); err != nil || len(again) != 0 {
		t.Fatalf("repeat migration: applied=%v err=%v", again, err)
	}

	repo := repository.New(db)
	assertAccrued := func(at time.Time) {
		t.Helper()
		daily, history, err := repo.AccrueAppleBaseCredits(ctx, "existing", at, loc)
		if err != nil || daily != 0 || !history {
			t.Fatalf("expired Base accrual: daily=%d history=%t err=%v", daily, history, err)
		}
		assertExisting()
		if scalar(`SELECT COUNT(*) FROM apple_base_credit_days`) != 2 ||
			scalar(`SELECT COUNT(*) FROM credit_grants WHERE product_code='apple_base_daily' AND expires_at IS NULL`) != 2 ||
			scalar(`SELECT COALESCE(SUM(units_remaining),0) FROM credit_grants WHERE product_code='apple_base_daily'`) != 80-3 {
			t.Fatal("two paid days must yield 80 permanent replies minus 3 previously used replies, without a grant at the expiry midnight")
		}
		balance, err := repo.CreditBalance(ctx, "existing", at)
		if err != nil || balance.Packs != 49+80-3 || balance.WelcomeRemaining != 7 {
			t.Fatalf("balance after rollover: %+v err=%v", balance, err)
		}
	}
	assertAccrued(end)
	ledgerCount := scalar(`SELECT COUNT(*) FROM credit_ledger`)
	assertAccrued(end)
	assertAccrued(end.AddDate(0, 0, 30))
	if scalar(`SELECT COUNT(*) FROM credit_ledger`) != ledgerCount {
		t.Fatal("re-reading an expired period duplicated ledger entries")
	}

	exec(`DELETE FROM users WHERE id='existing'`)
	for _, table := range []string{"apple_base_credit_periods", "apple_base_credit_days", "apple_base_legacy_charges", "apple_subscriptions", "credit_grants", "credit_ledger"} {
		if got := scalar(`SELECT COUNT(*) FROM ` + table); got != 0 {
			t.Fatalf("%s retained %d account-owned rows after deletion", table, got)
		}
	}
	if scalar(`SELECT COUNT(*) FROM iap_purchases`) != 2 || scalar(`SELECT COUNT(*) FROM iap_purchases WHERE user_id IS NULL`) != 2 {
		t.Fatal("financial purchase records must survive with the account reference removed")
	}
}
