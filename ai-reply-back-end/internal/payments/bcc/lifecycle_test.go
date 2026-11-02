package bcc

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/repository"
)

// bankAnswer answers every request to the bank with the given ACTION/RC and records the forms.
func bankAnswer(t *testing.T, s *CheckoutService, action, rc string, sent *[]url.Values) {
	t.Helper()
	s.client.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		*sent = append(*sent, r.PostForm)
		fields := url.Values{"ACTION": {action}, "RC": {rc}, "ORDER": {r.PostForm.Get("ORDER")}, "TERMINAL": {r.PostForm.Get("TERMINAL")},
			"AMOUNT": {r.PostForm.Get("AMOUNT")}, "CURRENCY": {"398"}}
		if action == "0" {
			fields.Set("RRN", "223456789012")
			fields.Set("INT_REF", "FEDCBA9876543210")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}, Body: io.NopCloser(strings.NewReader(fields.Encode()))}, nil
	})}
}

func orderStatus(t *testing.T, s *CheckoutService, id string) string {
	t.Helper()
	var status string
	if err := s.db.Reader().QueryRow(`SELECT status FROM billing_orders WHERE id=?`, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

func mandateStatus(t *testing.T, s *CheckoutService, rootOrder string) string {
	t.Helper()
	var status string
	if err := s.db.Reader().QueryRow(`SELECT status FROM bcc_subscriptions WHERE root_order_id=?`, rootOrder).Scan(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

func TestAbandonedFirstSubscriptionPaymentStopsBlockingAndStillCountsWhenPaidLate(t *testing.T) {
	s, _ := testCheckout(t)
	s.cfg.RecurringEnabled = true
	now := time.Now().UTC().Truncate(time.Millisecond)
	s.now = func() time.Time { return now }
	ctx := context.Background()
	abandoned, err := s.StartExpected(ctx, "bank-user", "base_monthly", "abandoned-checkout", subscriptionQuote(t, s, "base_monthly"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartExpected(ctx, "bank-user", "base_monthly", "too-early", subscriptionQuote(t, s, "base_monthly")); err == nil {
		t.Fatal("a second mandate was accepted while the first payment could still complete")
	}
	now = now.Add(abandonedFirstPayment + time.Minute)
	retry, err := s.StartExpected(ctx, "bank-user", "base_monthly", "after-abandoning", subscriptionQuote(t, s, "base_monthly"))
	if err != nil {
		t.Fatalf("an abandoned first payment still blocks the account: %v", err)
	}
	if got := mandateStatus(t, s, abandoned.ID); got != "expired" {
		t.Fatalf("abandoned mandate is %s", got)
	}
	if w := notifyRequest(s, originalFields(t, s, retry.ID), true); w.Code != http.StatusOK {
		t.Fatalf("retry notify: %d %s", w.Code, w.Body.String())
	}
	store := repository.New(s.db)
	paid, err := store.ActiveBankSubscription(ctx, "bank-user", now)
	if err != nil || !paid.ExpiresAt.Equal(now.Add(30*24*time.Hour)) {
		t.Fatalf("retry period: %+v %v", paid, err)
	}
	// The bank confirms the abandoned order late: the person paid twice, so they
	// get both periods back to back instead of one period bought twice.
	if w := notifyRequest(s, originalFields(t, s, abandoned.ID), true); w.Code != http.StatusOK {
		t.Fatalf("late notify: %d %s", w.Code, w.Body.String())
	}
	var start, end int64
	if err := s.db.Reader().QueryRow(`SELECT starts_at,expires_at FROM bcc_subscription_periods WHERE order_id=?`, abandoned.ID).Scan(&start, &end); err != nil {
		t.Fatal(err)
	}
	if start != now.Add(30*24*time.Hour).UnixMilli() || end != now.Add(60*24*time.Hour).UnixMilli() {
		t.Fatalf("late period overlaps the paid one: %d..%d", start, end)
	}
	if got := mandateStatus(t, s, abandoned.ID); got != "expired" {
		t.Fatalf("a late first payment revived renewals of the abandoned mandate: %s", got)
	}
}

func TestDeclinedFirstSubscriptionPaymentAllowsAnotherCardAtOnce(t *testing.T) {
	s, _ := testCheckout(t)
	s.cfg.RecurringEnabled = true
	ctx := context.Background()
	first, err := s.StartExpected(ctx, "bank-user", "pro_monthly", "declined-card", subscriptionQuote(t, s, "pro_monthly"))
	if err != nil {
		t.Fatal(err)
	}
	declined := originalFields(t, s, first.ID)
	declined.Set("ACTION", "2")
	declined.Set("RC", "51")
	if w := notifyRequest(s, declined, true); w.Code != http.StatusOK {
		t.Fatalf("decline notify: %d %s", w.Code, w.Body.String())
	}
	if orderStatus(t, s, first.ID) != "failed" || mandateStatus(t, s, first.ID) != "expired" {
		t.Fatalf("decline left order %s mandate %s", orderStatus(t, s, first.ID), mandateStatus(t, s, first.ID))
	}
	if _, err := s.StartExpected(ctx, "bank-user", "pro_monthly", "another-card", subscriptionQuote(t, s, "pro_monthly")); err != nil {
		t.Fatalf("a declined first payment blocks a retry: %v", err)
	}
}

func TestApprovalAfterRecordedDeclineGrantsThePack(t *testing.T) {
	s, _ := testCheckout(t)
	ctx := context.Background()
	o, err := s.Start(ctx, "bank-user", "pack30", "decline-then-approve")
	if err != nil {
		t.Fatal(err)
	}
	fields := originalFields(t, s, o.ID)
	declined := originalFields(t, s, o.ID)
	declined.Set("ACTION", "2")
	declined.Set("RC", "05")
	if w := notifyRequest(s, declined, true); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	if w := notifyRequest(s, fields, true); w.Code != http.StatusOK {
		t.Fatalf("approval after a decline was refused: %d %s", w.Code, w.Body.String())
	}
	if orderStatus(t, s, o.ID) != "succeeded" || bankBalance(t, s) != 30 {
		t.Fatalf("approval after decline: %s balance %d", orderStatus(t, s, o.ID), bankBalance(t, s))
	}
	if w := notifyRequest(s, fields, true); w.Code != http.StatusOK || bankBalance(t, s) != 30 {
		t.Fatal("a repeated approval granted twice")
	}
}

func paidSubscription(t *testing.T, s *CheckoutService, now *time.Time) BankOrder {
	t.Helper()
	s.cfg.RecurringEnabled = true
	s.now = func() time.Time { return *now }
	order, err := s.StartExpected(context.Background(), "bank-user", "base_monthly", "paid-subscription", subscriptionQuote(t, s, "base_monthly"))
	if err != nil {
		t.Fatal(err)
	}
	if w := notifyRequest(s, originalFields(t, s, order.ID), true); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	return order
}

func renewalOrder(t *testing.T, s *CheckoutService) (string, string) {
	t.Helper()
	var id, state string
	if err := s.db.Reader().QueryRow(`SELECT order_id,state FROM bcc_recurring_attempts`).Scan(&id, &state); err != nil {
		t.Fatal(err)
	}
	return id, state
}

func TestRenewalDeclinedInTheDirectAnswerIsRecorded(t *testing.T) {
	s, _ := testCheckout(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	root := paidSubscription(t, s, &now)
	var sent []url.Values
	bankAnswer(t, s, "2", "51", &sent)
	now = now.Add(30 * 24 * time.Hour)
	if err := s.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	renewal, state := renewalOrder(t, s)
	if len(sent) != 1 || state != "declined" || orderStatus(t, s, renewal) != "failed" || mandateStatus(t, s, root.ID) != "needs_review" {
		t.Fatalf("direct decline: calls %d attempt %s order %s mandate %s", len(sent), state, orderStatus(t, s, renewal), mandateStatus(t, s, root.ID))
	}
	if err := s.RunDue(context.Background()); err != nil || len(sent) != 1 {
		t.Fatal("a declined renewal was charged again")
	}
}

func TestRenewalWithoutOutcomeIsEscalatedOnceAfterTheStatusWindow(t *testing.T) {
	s, _ := testCheckout(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	root := paidSubscription(t, s, &now)
	calls := 0
	s.client.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("timeout")
	})}
	now = now.Add(30 * 24 * time.Hour)
	ctx := context.Background()
	if err := s.RunDue(ctx); err != nil {
		t.Fatal(err)
	}
	renewal, state := renewalOrder(t, s)
	if state != "uncertain" || orderStatus(t, s, renewal) != "verification_pending" {
		t.Fatalf("uncertain charge: %s %s", state, orderStatus(t, s, renewal))
	}
	now = now.Add(renewalOutcomeWindow + time.Minute)
	for i := 0; i < 2; i++ {
		if err := s.RunDue(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 || orderStatus(t, s, renewal) != "needs_review" {
		t.Fatalf("escalation: calls %d order %s", calls, orderStatus(t, s, renewal))
	}
	var events int
	if err := s.db.Reader().QueryRow(`SELECT count(*) FROM billing_order_events WHERE order_id=? AND state='needs_review'`, renewal).Scan(&events); err != nil || events != 1 {
		t.Fatalf("escalated %d times: %v", events, err)
	}
	if mandateStatus(t, s, root.ID) != "active" {
		t.Fatal("escalation changed the mandate; a late notification must still extend access")
	}
	// The late authenticated confirmation still extends access.
	late := originalFields(t, s, renewal)
	late.Set("TRAN_TRTYPE", "171")
	if w := notifyRequest(s, late, true); w.Code != http.StatusOK {
		t.Fatalf("late renewal notify: %d %s", w.Code, w.Body.String())
	}
	if _, err := repository.New(s.db).ActiveBankSubscription(ctx, "bank-user", now); err != nil {
		t.Fatalf("late renewal did not extend access: %v", err)
	}
}

func TestSubscriptionRefundCarriesRecurRefAndCanBeSentAgainAfterARefusal(t *testing.T) {
	s, _ := testCheckout(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	root := paidSubscription(t, s, &now)
	ctx := context.Background()
	var sent []url.Values
	bankAnswer(t, s, "3", "-1", &sent)
	refused, err := s.Refund(ctx, "fixture-admin", root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || sent[0].Get("RECUR_REF") != "123456789012" || sent[0].Get("TRTYPE") != "14" {
		t.Fatalf("refund request: %v", sent)
	}
	if refused.RefundStatus != "needs_review" || orderStatus(t, s, root.ID) != "succeeded" {
		t.Fatalf("refused refund: %+v", refused)
	}
	bankAnswer(t, s, "0", "00", &sent)
	done, err := s.Refund(ctx, "fixture-admin", root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sent) != 2 || done.Status != "refunded" || done.RefundStatus != "succeeded" {
		t.Fatalf("second refund: calls %d %+v", len(sent), done)
	}
	if sent[1].Get("NONCE") == sent[0].Get("NONCE") {
		t.Fatal("a resent refund reused the nonce")
	}
	if _, err := s.Refund(ctx, "fixture-admin", root.ID); err != nil || len(sent) != 2 {
		t.Fatal("a completed refund was sent again")
	}
}

func TestPendingRefundIsNeverResent(t *testing.T) {
	s, _ := testCheckout(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	root := paidSubscription(t, s, &now)
	calls := 0
	s.client.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("timeout")
	})}
	for i := 0; i < 2; i++ {
		order, err := s.Refund(context.Background(), "fixture-admin", root.ID)
		if err != nil || order.RefundStatus != "pending" {
			t.Fatalf("uncertain refund: %+v %v", order, err)
		}
	}
	if calls != 1 {
		t.Fatalf("an uncertain refund was sent %d times", calls)
	}
}

func TestResubscribingContinuesAfterTheCanceledPaidPeriod(t *testing.T) {
	s, _ := testCheckout(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	root := paidSubscription(t, s, &now)
	ctx := context.Background()
	var id string
	if err := s.db.Reader().QueryRow(`SELECT id FROM bcc_subscriptions WHERE root_order_id=?`, root.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := s.CancelSubscription(ctx, "bank-user", id); err != nil {
		t.Fatal(err)
	}
	paidUntil := now.Add(30 * 24 * time.Hour)
	now = now.Add(10 * 24 * time.Hour)
	again, err := s.StartExpected(ctx, "bank-user", "base_monthly", "resubscribe", subscriptionQuote(t, s, "base_monthly"))
	if err != nil {
		t.Fatal(err)
	}
	if w := notifyRequest(s, originalFields(t, s, again.ID), true); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	var start, end int64
	if err := s.db.Reader().QueryRow(`SELECT starts_at,expires_at FROM bcc_subscription_periods WHERE order_id=?`, again.ID).Scan(&start, &end); err != nil {
		t.Fatal(err)
	}
	if start != paidUntil.UnixMilli() || end != paidUntil.Add(30*24*time.Hour).UnixMilli() {
		t.Fatalf("new period %d..%d overlaps the paid one ending %d", start, end, paidUntil.UnixMilli())
	}
}
