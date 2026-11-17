package repository

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/internal/domain"
)

func purchasedOnlyInput(userID string, now time.Time) ReserveInput {
	return ReserveInput{UserID: userID, Mode: "reply", Date: now.Format("2006-01-02"),
		Month: now.Format("2006-01"), PurchasedOnly: true, Now: now}
}

func purchasedOnlyGrant(t *testing.T, store *Store, userID, kind string, units int, now time.Time) domain.CreditGrant {
	t.Helper()
	grant := domain.CreditGrant{UserID: userID, Kind: kind, UnitsTotal: units, UnitsRemaining: units, StartsAt: now.Add(-time.Minute)}
	if kind == domain.GrantPass {
		end := now.Add(time.Hour)
		grant.ExpiresAt = &end
	}
	grant, err := store.InsertCreditGrant(context.Background(), grant, now)
	if err != nil {
		t.Fatal(err)
	}
	return grant
}

func purchasedOnlyScalar(t *testing.T, store *Store, query string, args ...any) int {
	t.Helper()
	var result int
	if err := store.db.Reader().QueryRowContext(context.Background(), query, args...).Scan(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertPurchasedOnlyBalance(t *testing.T, balance domain.CreditBalance, packs int) {
	t.Helper()
	if !reflect.DeepEqual(balance, domain.CreditBalance{Packs: packs}) {
		t.Fatalf("pack-only reservation exposed another allowance: %+v", balance)
	}
}

func TestPurchasedOnlyReservationExcludesEveryOtherAllowance(t *testing.T) {
	for _, unlimited := range []bool{false, true} {
		t.Run(fmt.Sprintf("unlimited_%t", unlimited), func(t *testing.T) {
			store, _ := proTestStore(t)
			const userID = "purchased-only"
			proTestUser(t, store, userID)
			ctx, now := context.Background(), time.Now().UTC()
			grants := map[string]domain.CreditGrant{}
			for _, kind := range []string{domain.GrantWelcome, domain.GrantAdmin, domain.GrantPass, domain.GrantPack} {
				grants[kind] = purchasedOnlyGrant(t, store, userID, kind, 2, now)
			}
			in := purchasedOnlyInput(userID, now)
			in.Unlimited, in.DailyLimit, in.MonthlyLimit = unlimited, 100, 100
			for remaining := 1; remaining >= 0; remaining-- {
				reservation, err := store.ReserveGeneration(ctx, in)
				if err != nil || reservation.Bucket != domain.GrantPack || reservation.GrantID != grants[domain.GrantPack].ID {
					t.Fatalf("restricted reservation used another allowance: %+v %v", reservation, err)
				}
				assertPurchasedOnlyBalance(t, reservation.Balance, remaining)
			}
			if _, err := store.ReserveGeneration(ctx, in); !errors.Is(err, domain.ErrDailyLimit) {
				t.Fatalf("exhausted packs fell back to another allowance: %v", err)
			}
			for kind, grant := range grants {
				want := 2
				if kind == domain.GrantPack {
					want = 0
				}
				if got := purchasedOnlyScalar(t, store, `SELECT units_remaining FROM credit_grants WHERE id=?`, grant.ID); got != want {
					t.Fatalf("%s remaining=%d, want %d", kind, got, want)
				}
			}
			day, month, err := store.Counters(ctx, userID, in.Date, in.Month)
			if err != nil || day != 0 || month != 0 {
				t.Fatalf("restricted request charged plan counters: %d/%d %v", day, month, err)
			}
			if got := purchasedOnlyScalar(t, store, `SELECT COUNT(*) FROM credit_ledger WHERE user_id=? AND entry='consume' AND kind='pack' AND delta=-1`, userID); got != 2 {
				t.Fatalf("paid-pack consume entries=%d, want 2", got)
			}
			// Unrestricted callers still retain the original plan/allowance policy.
			in.PurchasedOnly = false
			ordinary, err := store.ReserveGeneration(ctx, in)
			wantBucket := domain.BucketDaily
			if unlimited {
				wantBucket = domain.BucketUnlimited
			}
			if err != nil || ordinary.Bucket != wantBucket || ordinary.Balance.WelcomeRemaining != 2 || ordinary.Balance.Admin != 2 || ordinary.Balance.PassRemaining != 2 {
				t.Fatalf("ordinary reservation changed: %+v %v", ordinary, err)
			}
		})
	}
}

func TestPurchasedOnlyCannotReplayExcludedReservation(t *testing.T) {
	for _, bucket := range []string{domain.BucketUnlimited, domain.BucketDaily, domain.GrantWelcome, domain.GrantAdmin, domain.GrantPass} {
		t.Run(bucket, func(t *testing.T) {
			store, _ := proTestStore(t)
			const userID = "excluded-replay"
			proTestUser(t, store, userID)
			ctx, now := context.Background(), time.Now().UTC()
			purchasedOnlyGrant(t, store, userID, domain.GrantPack, 3, now)
			ordinary := purchasedOnlyInput(userID, now)
			ordinary.PurchasedOnly, ordinary.Key = false, "excluded-reservation-key"
			switch bucket {
			case domain.BucketUnlimited:
				ordinary.Unlimited = true
			case domain.BucketDaily:
				ordinary.DailyLimit = 10
			default:
				purchasedOnlyGrant(t, store, userID, bucket, 2, now)
			}
			first, err := store.ReserveGeneration(ctx, ordinary)
			if err != nil || first.Bucket != bucket {
				t.Fatalf("wrong original allowance: %+v %v", first, err)
			}
			if committed, err := store.CommitGeneration(ctx, first.ID, now); err != nil || !committed {
				t.Fatalf("commit: %t %v", committed, err)
			}
			restricted := ordinary
			restricted.PurchasedOnly = true
			_, err = store.ReserveGeneration(ctx, restricted)
			var conflict *domain.ConflictError
			if !errors.As(err, &conflict) || conflict.Field != "idempotency_key" || conflict.Reason != "used" {
				t.Fatalf("excluded allowance replay was admitted: %v", err)
			}
			if got := purchasedOnlyScalar(t, store, `SELECT replays FROM usage_reservations WHERE id=?`, first.ID); got != 0 {
				t.Fatalf("rejected replay changed replay allowance: %d", got)
			}
			balance, err := store.CreditBalance(ctx, userID, now)
			if err != nil || balance.Packs != 3 {
				t.Fatalf("excluded replay spent a pack: %+v %v", balance, err)
			}
			if replay, err := store.ReserveGeneration(ctx, ordinary); err != nil || !replay.Replay || replay.Bucket != bucket {
				t.Fatalf("normal replay behavior changed: %+v %v", replay, err)
			}
		})
	}
}

func TestPurchasedOnlyPackReplayWorksAfterLastUnitAndStaysBounded(t *testing.T) {
	store, _ := proTestStore(t)
	const userID = "pack-replay"
	proTestUser(t, store, userID)
	ctx, now := context.Background(), time.Now().UTC()
	for _, kind := range []string{domain.GrantPack, domain.GrantPass, domain.GrantWelcome, domain.GrantAdmin} {
		purchasedOnlyGrant(t, store, userID, kind, 1, now)
	}
	in := purchasedOnlyInput(userID, now)
	in.Key, in.Unlimited = "last-pack-replay-key", true
	first, err := store.ReserveGeneration(ctx, in)
	if err != nil || first.Bucket != domain.GrantPack {
		t.Fatalf("pack reservation: %+v %v", first, err)
	}
	if committed, err := store.CommitGeneration(ctx, first.ID, now); err != nil || !committed {
		t.Fatalf("commit: %t %v", committed, err)
	}
	replay, err := store.ReserveGeneration(ctx, in)
	if err != nil || !replay.Replay || replay.ID != first.ID || replay.Bucket != domain.GrantPack {
		t.Fatalf("last paid unit cannot be replayed: %+v %v", replay, err)
	}
	assertPurchasedOnlyBalance(t, replay.Balance, 0)
	if _, err := store.ReserveGeneration(ctx, in); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("pack replay exceeded its bound: %v", err)
	}
	if got := purchasedOnlyScalar(t, store, `SELECT COUNT(*) FROM credit_ledger WHERE user_id=? AND entry='consume'`, userID); got != 1 {
		t.Fatalf("pack replay was charged twice: %d", got)
	}
}

func TestPurchasedOnlyRefundReturnsSamePackAndRetryChargesAgain(t *testing.T) {
	store, _ := proTestStore(t)
	const userID = "pack-refund"
	proTestUser(t, store, userID)
	ctx, now := context.Background(), time.Now().UTC()
	pack := purchasedOnlyGrant(t, store, userID, domain.GrantPack, 1, now)
	purchasedOnlyGrant(t, store, userID, domain.GrantWelcome, 2, now)
	in := purchasedOnlyInput(userID, now)
	in.Key = "pack-refund-retry-key"
	first, err := store.ReserveGeneration(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if refunded, err := store.RefundGeneration(ctx, first.ID, now); err != nil || !refunded {
		t.Fatalf("refund: %t %v", refunded, err)
	}
	if refunded, err := store.RefundGeneration(ctx, first.ID, now); err != nil || refunded {
		t.Fatalf("duplicate refund: %t %v", refunded, err)
	}
	if remaining := purchasedOnlyScalar(t, store, `SELECT units_remaining FROM credit_grants WHERE id=?`, pack.ID); remaining != 1 {
		t.Fatalf("pack refund returned %d units", remaining)
	}
	retry, err := store.ReserveGeneration(ctx, in)
	if err != nil || retry.Replay || retry.ID == first.ID || retry.GrantID != pack.ID || retry.Bucket != domain.GrantPack {
		t.Fatalf("refund retry did not debit its original pack: %+v %v", retry, err)
	}
	assertPurchasedOnlyBalance(t, retry.Balance, 0)
	if got := purchasedOnlyScalar(t, store, `SELECT COALESCE(SUM(delta),0) FROM credit_ledger WHERE grant_id=?`, pack.ID); got != 0 {
		t.Fatalf("pack ledger does not reconcile after retry: %d", got)
	}
}

func TestPurchasedOnlyLastUnitIsAtomicAcrossConnections(t *testing.T) {
	store, dbPath := proTestStore(t)
	const userID = "pack-race"
	proTestUser(t, store, userID)
	ctx, now := context.Background(), time.Now().UTC()
	purchasedOnlyGrant(t, store, userID, domain.GrantPack, 1, now)
	for _, kind := range []string{domain.GrantPass, domain.GrantWelcome, domain.GrantAdmin} {
		purchasedOnlyGrant(t, store, userID, kind, 5, now)
	}
	otherDB, err := database.Open(database.Options{Path: dbPath, BusyTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer otherDB.Close()
	stores := []*Store{store, New(otherDB)}
	const attempts = 12
	start := make(chan struct{})
	var ready, workers sync.WaitGroup
	ready.Add(attempts)
	workers.Add(attempts)
	errorsByAttempt := make([]error, attempts)
	for i := range errorsByAttempt {
		go func(index int) {
			defer workers.Done()
			in := purchasedOnlyInput(userID, now)
			in.Unlimited, in.DailyLimit, in.MonthlyLimit = true, 100, 100
			in.Key = fmt.Sprintf("pack-race-request-%d", index)
			ready.Done()
			<-start
			_, errorsByAttempt[index] = stores[index%len(stores)].ReserveGeneration(ctx, in)
		}(i)
	}
	ready.Wait()
	close(start)
	workers.Wait()
	succeeded := 0
	for _, err := range errorsByAttempt {
		if err == nil {
			succeeded++
		} else if !errors.Is(err, domain.ErrDailyLimit) {
			t.Fatalf("unexpected concurrent reservation failure: %v", err)
		}
	}
	if succeeded != 1 || purchasedOnlyScalar(t, store, `SELECT COUNT(*) FROM usage_reservations WHERE user_id=?`, userID) != 1 ||
		purchasedOnlyScalar(t, store, `SELECT COUNT(*) FROM credit_ledger WHERE user_id=? AND entry='consume' AND kind='pack' AND delta=-1`, userID) != 1 {
		t.Fatalf("last purchased unit admitted %d concurrent reservations", succeeded)
	}
	balance, err := store.CreditBalance(ctx, userID, now)
	if err != nil || balance.Packs != 0 || balance.WelcomeRemaining != 5 || balance.Admin != 5 || balance.PassRemaining != 5 {
		t.Fatalf("concurrent pack-only requests touched excluded balances: %+v %v", balance, err)
	}
}
