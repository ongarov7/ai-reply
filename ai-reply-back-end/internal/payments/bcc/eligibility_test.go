package bcc

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/repository"
)

func appleAccessFixture(t *testing.T, s *CheckoutService, now time.Time, autoRenew bool, expires time.Time, revoked bool) {
	t.Helper()
	var revokedAt any
	if revoked {
		revokedAt = now.UnixMilli()
	}
	if _, err := s.db.Writer().Exec(`INSERT INTO apple_subscriptions(original_transaction_id,user_id,transaction_id,product_id,plan_id,environment,purchased_at,expires_at,signed_at,revoked_at,auto_renew,updated_at)
 SELECT 'apple-original','bank-user','apple-transaction','kz.aireply.pro.monthly',id,'Production',?,?,?,?,?,? FROM plans WHERE code='pro_monthly'`,
		now.Add(-time.Hour).UnixMilli(), expires.UnixMilli(), now.UnixMilli(), revokedAt, autoRenew, now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
}

func googleAccessFixture(t *testing.T, s *CheckoutService, now time.Time, state string, autoRenew bool, expires, readAt time.Time, revoked, superseded bool) {
	t.Helper()
	var revokedAt any
	if revoked {
		revokedAt = now.UnixMilli()
	}
	replacement := ""
	if superseded {
		replacement = "replacement-token"
	}
	if _, err := s.db.Writer().Exec(`INSERT INTO google_subscriptions(token_hash,user_id,product_id,plan_id,environment,state,started_at,expires_at,auto_renew,granted_at,read_at,updated_at,revoked_at,superseded_by)
 SELECT 'google-token','bank-user','kz.aireply.pro.monthly',id,'Production',?,?,?,?,?,?,?,?,? FROM plans WHERE code='pro_monthly'`,
		state, now.Add(-72*time.Hour).UnixMilli(), expires.UnixMilli(), autoRenew, now.UnixMilli(), readAt.UnixMilli(), now.UnixMilli(), revokedAt, replacement); err != nil {
		t.Fatal(err)
	}
}

func manualAccessFixture(t *testing.T, s *CheckoutService, now time.Time, status string, expires any) {
	t.Helper()
	if _, err := s.db.Writer().Exec(`INSERT INTO subscriptions(id,user_id,plan_id,status,source,started_at,expires_at,created_at,updated_at)
 SELECT 'manual-pro','bank-user',id,?,'admin',?,?,?,? FROM plans WHERE code='pro_monthly'`,
		status, now.Add(-time.Hour).UnixMilli(), expires, now.UnixMilli(), now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
}

func canceledBankProFixture(t *testing.T, s *CheckoutService) string {
	t.Helper()
	ctx := context.Background()
	order, err := s.StartExpected(ctx, "bank-user", "pro_monthly", "original-bank-pro", subscriptionQuote(t, s, "pro_monthly"))
	if err != nil {
		t.Fatal(err)
	}
	if w := notifyRequest(s, originalFields(t, s, order.ID), true); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	var subscriptionID string
	if err := s.db.Reader().QueryRow(`SELECT bcc_subscription_id FROM billing_orders WHERE id=?`, order.ID).Scan(&subscriptionID); err != nil {
		t.Fatal(err)
	}
	if err := s.CancelSubscription(ctx, "bank-user", subscriptionID); err != nil {
		t.Fatal(err)
	}
	return order.ID
}

func checkoutRecordCounts(t *testing.T, s *CheckoutService) [3]int {
	t.Helper()
	var counts [3]int
	if err := s.db.Reader().QueryRow(`SELECT (SELECT count(*) FROM billing_orders),(SELECT count(*) FROM bcc_subscriptions),(SELECT count(*) FROM billing_web_purchase_consents)`).Scan(&counts[0], &counts[1], &counts[2]); err != nil {
		t.Fatal(err)
	}
	return counts
}

func TestNewProCheckoutRejectsStillUsableUnlimitedFromEveryProvider(t *testing.T) {
	fixtures := map[string]func(*testing.T, *CheckoutService, time.Time){
		"apple_renewing": func(t *testing.T, s *CheckoutService, now time.Time) {
			appleAccessFixture(t, s, now, true, now.Add(time.Hour), false)
		},
		"apple_canceled_but_paid": func(t *testing.T, s *CheckoutService, now time.Time) {
			appleAccessFixture(t, s, now, false, now.Add(time.Hour), false)
		},
		"google_canceled_but_paid": func(t *testing.T, s *CheckoutService, now time.Time) {
			googleAccessFixture(t, s, now, domain.GoogleSubCanceled, false, now.Add(time.Hour), now, false, false)
		},
		"google_grace": func(t *testing.T, s *CheckoutService, now time.Time) {
			googleAccessFixture(t, s, now, domain.GoogleSubGrace, true, now.Add(time.Hour), now, false, false)
		},
		"google_renewal_bridge": func(t *testing.T, s *CheckoutService, now time.Time) {
			googleAccessFixture(t, s, now, domain.GoogleSubActive, true, now.Add(-time.Hour), now.Add(-2*time.Hour), false, false)
			if _, err := repository.New(s.db).ActiveGoogleSubscription(context.Background(), "bank-user", now); err != nil {
				t.Fatalf("the entitlement service should still grant access: %v", err)
			}
		},
		"manual_trial": func(t *testing.T, s *CheckoutService, now time.Time) {
			manualAccessFixture(t, s, now, domain.SubTrial, now.Add(time.Hour).UnixMilli())
		},
		"manual_without_expiry": func(t *testing.T, s *CheckoutService, now time.Time) {
			manualAccessFixture(t, s, now, domain.SubActive, nil)
		},
		"bank_canceled_but_paid": func(t *testing.T, s *CheckoutService, _ time.Time) {
			canceledBankProFixture(t, s)
		},
	}
	for name, setup := range fixtures {
		t.Run(name, func(t *testing.T) {
			s, _ := testCheckout(t)
			s.cfg.RecurringEnabled = true
			now := time.Now().UTC().Truncate(time.Millisecond)
			s.now = func() time.Time { return now }
			setup(t, s, now)
			before := checkoutRecordCounts(t, s)
			for _, code := range []string{"pro_monthly", "pro_yearly"} {
				_, err := s.StartExpected(context.Background(), "bank-user", code, "duplicate-"+code, subscriptionQuote(t, s, code))
				var conflict *domain.ConflictError
				if !errors.As(err, &conflict) || conflict.Field != "subscription" {
					t.Fatalf("%s should refuse another paid Pro checkout: %v", code, err)
				}
				if after := checkoutRecordCounts(t, s); after != before {
					t.Fatalf("refused checkout created financial records: before=%v after=%v", before, after)
				}
			}
			if _, err := s.Start(context.Background(), "bank-user", "pack500", "separate-one-time-pack"); err != nil {
				t.Fatalf("the Pro guard must not reject a separately requested pack: %v", err)
			}
		})
	}
}

func TestNewProCheckoutAllowsAccessThatNoLongerApplies(t *testing.T) {
	fixtures := map[string]func(*testing.T, *CheckoutService, time.Time){
		"apple_expired": func(t *testing.T, s *CheckoutService, now time.Time) {
			appleAccessFixture(t, s, now, false, now, false)
		},
		"apple_revoked": func(t *testing.T, s *CheckoutService, now time.Time) {
			appleAccessFixture(t, s, now, true, now.Add(time.Hour), true)
		},
		"google_canceled_expired": func(t *testing.T, s *CheckoutService, now time.Time) {
			googleAccessFixture(t, s, now, domain.GoogleSubCanceled, false, now, now, false, false)
		},
		"google_revoked": func(t *testing.T, s *CheckoutService, now time.Time) {
			googleAccessFixture(t, s, now, domain.GoogleSubActive, true, now.Add(time.Hour), now, true, false)
		},
		"google_superseded": func(t *testing.T, s *CheckoutService, now time.Time) {
			googleAccessFixture(t, s, now, domain.GoogleSubActive, true, now.Add(time.Hour), now, false, true)
		},
		"google_on_hold": func(t *testing.T, s *CheckoutService, now time.Time) {
			googleAccessFixture(t, s, now, domain.GoogleSubOnHold, true, now.Add(time.Hour), now, false, false)
		},
		"google_renewal_bridge_ended": func(t *testing.T, s *CheckoutService, now time.Time) {
			googleAccessFixture(t, s, now, domain.GoogleSubActive, true, now.Add(-48*time.Hour), now.Add(-49*time.Hour), false, false)
		},
		"google_read_after_expiry": func(t *testing.T, s *CheckoutService, now time.Time) {
			googleAccessFixture(t, s, now, domain.GoogleSubActive, true, now.Add(-time.Hour), now, false, false)
		},
		"manual_expired": func(t *testing.T, s *CheckoutService, now time.Time) {
			manualAccessFixture(t, s, now, domain.SubActive, now.Add(-time.Millisecond).UnixMilli())
		},
		"manual_canceled": func(t *testing.T, s *CheckoutService, now time.Time) {
			manualAccessFixture(t, s, now, domain.SubCancelled, now.Add(time.Hour).UnixMilli())
		},
		"bank_paid_period_ended": func(t *testing.T, s *CheckoutService, now time.Time) {
			id := canceledBankProFixture(t, s)
			if _, err := s.db.Writer().Exec(`UPDATE bcc_subscription_periods SET expires_at=? WHERE order_id=?`, now.UnixMilli(), id); err != nil {
				t.Fatal(err)
			}
		},
		"bank_paid_period_revoked": func(t *testing.T, s *CheckoutService, now time.Time) {
			id := canceledBankProFixture(t, s)
			if _, err := s.db.Writer().Exec(`UPDATE bcc_subscription_periods SET revoked_at=? WHERE order_id=?`, now.UnixMilli(), id); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, setup := range fixtures {
		t.Run(name, func(t *testing.T) {
			s, _ := testCheckout(t)
			s.cfg.RecurringEnabled = true
			now := time.Now().UTC().Truncate(time.Millisecond)
			s.now = func() time.Time { return now }
			setup(t, s, now)
			if _, err := s.StartExpected(context.Background(), "bank-user", "pro_yearly", "new-valid-pro-purchase", subscriptionQuote(t, s, "pro_yearly")); err != nil {
				t.Fatalf("inactive former access must not prevent a new Pro purchase: %v", err)
			}
		})
	}
}

func TestExistingUnpaidProOrderCannotReopenBankFormAfterStoreGrantsPro(t *testing.T) {
	s, _ := testCheckout(t)
	s.cfg.RecurringEnabled = true
	now := time.Now().UTC().Truncate(time.Millisecond)
	s.now = func() time.Time { return now }
	ctx := context.Background()
	quote := subscriptionQuote(t, s, "pro_monthly")
	order, err := s.StartExpected(ctx, "bank-user", "pro_monthly", "before-store-grant", quote)
	if err != nil {
		t.Fatal(err)
	}
	openForm := func() *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, order.RedirectURL, nil)
		r.SetPathValue("id", order.ID)
		s.CheckoutPageWithClientIP(w, r, "192.0.2.10")
		return w
	}
	if w := openForm(); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "<form") {
		t.Fatalf("its own unpaid mandate must not block the initial bank form: %d %s", w.Code, w.Body.String())
	}
	appleAccessFixture(t, s, now, false, now.Add(time.Hour), false)
	before := checkoutRecordCounts(t, s)
	if _, err := s.StartExpected(ctx, "bank-user", "pro_monthly", "before-store-grant", quote); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("idempotent retry reopened an unpaid duplicate Pro order: %v", err)
	}
	w := openForm()
	if w.Code != http.StatusConflict || strings.Contains(w.Body.String(), "<form") || w.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("stale bank form was not blocked safely: %d %s", w.Code, w.Body.String())
	}
	if after := checkoutRecordCounts(t, s); after != before || orderStatus(t, s, order.ID) != "created" {
		t.Fatalf("blocking the form must preserve the original operation: before=%v after=%v", before, after)
	}
	// An operation may already have reached the bank. A verified late approval
	// still delivers the frozen purchase; the checkout guard cannot erase it.
	if w := notifyRequest(s, originalFields(t, s, order.ID), true); w.Code != http.StatusOK {
		t.Fatalf("already charged operation lost its purchase: %d %s", w.Code, w.Body.String())
	}
	if replay, err := s.StartExpected(ctx, "bank-user", "pro_monthly", "before-store-grant", ExpectedQuote{}); err != nil || replay.ID != order.ID || replay.Status != "succeeded" || replay.RedirectURL != "" {
		t.Fatalf("a paid idempotent replay must remain readable: %+v %v", replay, err)
	}
}

func TestProGrantCommittedBeforeCheckoutLockPreventsNewOrder(t *testing.T) {
	s, _ := testCheckout(t)
	s.cfg.RecurringEnabled = true
	now := time.Now().UTC().Truncate(time.Millisecond)
	s.now = func() time.Time { return now }
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	quote := subscriptionQuote(t, s, "pro_monthly")
	tx, err := s.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	waits := s.db.Writer().Stats().WaitCount
	result := make(chan error, 1)
	go func() {
		_, err := s.StartExpected(ctx, "bank-user", "pro_monthly", "checkout-after-grant", quote)
		result <- err
	}()
	deadline := time.Now().Add(time.Second)
	for s.db.Writer().Stats().WaitCount == waits && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if s.db.Writer().Stats().WaitCount == waits {
		t.Fatal("checkout did not reach the held writer lock")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO apple_subscriptions(original_transaction_id,user_id,transaction_id,product_id,plan_id,environment,purchased_at,expires_at,signed_at,auto_renew,updated_at)
 SELECT 'racing-apple-original','bank-user','racing-apple-transaction','kz.aireply.pro.monthly',id,'Production',?,?,?,0,? FROM plans WHERE code='pro_monthly'`, now.UnixMilli(), now.Add(time.Hour).UnixMilli(), now.UnixMilli(), now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, domain.ErrConflict) || checkoutRecordCounts(t, s) != [3]int{} {
		t.Fatalf("a Pro grant committed before checkout was missed: %v", err)
	}
}

func TestConcurrentProCheckoutsCreateOneMandateAndItsScheduledRenewalStillWorks(t *testing.T) {
	s, _ := testCheckout(t)
	s.cfg.RecurringEnabled = true
	now := time.Now().UTC().Truncate(time.Millisecond)
	s.now = func() time.Time { return now }
	ctx := context.Background()
	quote := subscriptionQuote(t, s, "pro_monthly")
	type result struct {
		order BankOrder
		err   error
	}
	results := make(chan result, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			order, err := s.StartExpected(ctx, "bank-user", "pro_monthly", fmt.Sprintf("competing-pro-attempt-%d", i), quote)
			results <- result{order, err}
		}(i)
	}
	wg.Wait()
	close(results)
	var first BankOrder
	for result := range results {
		if result.err == nil {
			if first.ID != "" {
				t.Fatal("concurrent attempts created multiple Pro bank forms")
			}
			first = result.order
		} else if !errors.Is(result.err, domain.ErrConflict) {
			t.Fatal(result.err)
		}
	}
	if first.ID == "" || checkoutRecordCounts(t, s) != [3]int{1, 1, 1} {
		t.Fatal("the first checkout did not reserve exactly one mandate")
	}
	if w := notifyRequest(s, originalFields(t, s, first.ID), true); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	var sent []url.Values
	bankAnswer(t, s, "0", "00", &sent)
	now = now.Add(30 * 24 * time.Hour)
	if err := s.RunDue(ctx); err != nil || len(sent) != 1 {
		t.Fatalf("the purchase guard interfered with an existing mandate's renewal: calls=%d err=%v", len(sent), err)
	}
	renewal, _ := renewalOrder(t, s)
	fields := originalFields(t, s, renewal)
	fields.Set("TRAN_TRTYPE", "171")
	if w := notifyRequest(s, fields, true); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	paid, err := repository.New(s.db).ActiveBankSubscription(ctx, "bank-user", now)
	if err != nil || paid.ExpiresAt == nil || !paid.ExpiresAt.Equal(now.Add(30*24*time.Hour)) {
		t.Fatalf("scheduled renewal did not extend the paid Pro period: %+v %v", paid, err)
	}
}
