package bcc

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/email"
	"github.com/aireply/ai-reply-back-end/internal/legal"
	"github.com/aireply/ai-reply-back-end/internal/localization"
	"github.com/aireply/ai-reply-back-end/internal/repository"
)

func subscriptionQuote(t *testing.T, s *CheckoutService, code string) ExpectedQuote {
	t.Helper()
	p, version, err := s.catalog.ProductForWeb(context.Background(), code)
	if err != nil {
		t.Fatal(err)
	}
	return ExpectedQuote{CatalogVersion: version, PriceMinor: p.PriceMinor, Units: p.Units, Currency: p.Currency, TermsVersion: legal.WebPurchaseTermsVersion, Locale: "ru", RecurringConsent: true, RecurringTermsVersion: RecurringTermsVersion, RecurringUntil: s.now().UTC().AddDate(2, 0, 0).Format("20060102")}
}

func enableLegacyBaseOffer(t *testing.T, s *CheckoutService) {
	t.Helper()
	// Historical orders sold a renewing 1,000-reply Base bundle. Publish that
	// offer only in this test's database; the current web offer is pack500.
	ctx := context.Background()
	published, err := s.catalog.Published(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for i := range published.Products {
		if published.Products[i].Code == "base_monthly" {
			published.Products[i].Active = true
			published.Products[i].AvailableWeb = true
			published.Products[i].PriceMinor = 99000
			published.Products[i].MonthlyLimit = 1000
			found = true
		}
	}
	if !found {
		t.Fatal("historical Base product is missing")
	}
	draft, err := s.catalog.SaveDraft(ctx, "legacy-fixture", 0, published.Products)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.catalog.Publish(ctx, "legacy-fixture", draft.ID, draft.Revision); err != nil {
		t.Fatal(err)
	}
}

func TestBankMandateWaitsForARenewingGoogleSubscription(t *testing.T) {
	s, _ := testCheckout(t)
	enableLegacyBaseOffer(t, s)
	s.cfg.RecurringEnabled = true
	now := time.Now().UTC().Truncate(time.Millisecond)
	s.now = func() time.Time { return now }
	ctx := context.Background()
	insert := func(state string, expires time.Time) {
		t.Helper()
		if _, err := s.db.Writer().Exec(`INSERT OR REPLACE INTO google_subscriptions(token_hash,user_id,product_id,plan_id,environment,state,started_at,expires_at,auto_renew,granted_at,read_at,updated_at)
			SELECT 'hash','bank-user','kz.aireply.pro.monthly',id,'Production',?,?,?,1,?,?,? FROM plans WHERE code='pro_monthly'`,
			state, now.UnixMilli(), expires.UnixMilli(), now.UnixMilli(), now.UnixMilli(), now.UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
	insert(domain.GoogleSubActive, now.Add(time.Hour))
	if _, err := s.StartExpected(ctx, "bank-user", "base_monthly", "with-google", subscriptionQuote(t, s, "base_monthly")); err == nil ||
		!strings.Contains(err.Error(), "manage_existing_store_subscription") {
		t.Fatalf("a bank mandate next to a renewing Google subscription: %v", err)
	}
	insert(domain.GoogleSubOnHold, now.Add(time.Hour))
	if _, err := s.StartExpected(ctx, "bank-user", "base_monthly", "after-google", subscriptionQuote(t, s, "base_monthly")); err != nil {
		t.Fatalf("a subscription on hold blocks nothing: %v", err)
	}
}

func TestBankRenewalIsPersistentSingleAttemptAndRequiresFinalNotification(t *testing.T) {
	s, _ := testCheckout(t)
	enableLegacyBaseOffer(t, s)
	s.cfg.RecurringEnabled = true
	now := time.Now().UTC().Truncate(time.Millisecond)
	s.now = func() time.Time { return now }
	ctx := context.Background()
	quote := subscriptionQuote(t, s, "base_monthly")
	order, err := s.StartExpected(ctx, "bank-user", "base_monthly", "first-subscription", quote)
	if err != nil {
		t.Fatal(err)
	}
	fields := originalFields(t, s, order.ID)
	if fields.Get("MERCH_TRAN_STATE") != "S" || fields.Get("RECUR_FREQ") != "30" || fields.Get("RECUR_EXP") != quote.RecurringUntil {
		t.Fatal("first payment omitted renewal consent")
	}
	store := repository.New(s.db)
	if _, err := store.ActiveBankSubscription(ctx, "bank-user", now); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("pending access: %v", err)
	}
	if w := notifyRequest(s, fields, true); w.Code != http.StatusOK {
		t.Fatalf("first notify: %d %s", w.Code, w.Body.String())
	}
	first, err := store.ActiveBankSubscription(ctx, "bank-user", now)
	if err != nil || first.ExpiresAt == nil || !first.ExpiresAt.Equal(now.Add(30*24*time.Hour)) {
		t.Fatalf("first paid period: %+v %v", first, err)
	}
	if balance, err := store.CreditBalance(ctx, "bank-user", now); err != nil || balance.Packs != 1000 {
		t.Fatalf("first monthly bundle: %+v %v", balance, err)
	}
	if _, err := s.StartExpected(ctx, "bank-user", "pro_monthly", "duplicate-subscription", subscriptionQuote(t, s, "pro_monthly")); err == nil {
		t.Fatal("second renewing mandate accepted")
	}
	var calls int
	var mu sync.Mutex
	s.client.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("TRTYPE") != "171" || r.Form.Get("MERCH_TRAN_STATE") != "M" || r.Form.Get("RECUR_REF") != fields.Get("RRN") {
			t.Error("renewal is not bound to the initial operation")
		}
		mu.Lock()
		calls++
		mu.Unlock()
		return nil, errors.New("uncertain transport outcome")
	})}
	now = *first.ExpiresAt
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.RunDue(ctx); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls != 1 {
		t.Fatalf("renewal submitted %d times", calls)
	}
	if _, err := store.ActiveBankSubscription(ctx, "bank-user", now); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("an uncertain charge extended access")
	}
	// Reconstructing the service models a restart; the database must prevent retry.
	restarted, err := NewCheckout(s.cfg, s.db, s.catalog)
	if err != nil {
		t.Fatal(err)
	}
	restarted.now = s.now
	restarted.client = s.client
	if err := restarted.RunDue(ctx); err != nil || calls != 1 {
		t.Fatalf("restart repeated a charge: %d %v", calls, err)
	}
	var renewal string
	if err := s.db.Reader().QueryRow(`SELECT order_id FROM bcc_recurring_attempts`).Scan(&renewal); err != nil {
		t.Fatal(err)
	}
	renewalFields := originalFields(t, s, renewal)
	renewalFields.Set("TRAN_TRTYPE", "171")
	for i := 0; i < 2; i++ {
		if w := notifyRequest(s, renewalFields, true); w.Code != http.StatusOK {
			t.Fatalf("renewal notify: %d %s", w.Code, w.Body.String())
		}
	}
	renewed, err := store.ActiveBankSubscription(ctx, "bank-user", now)
	if err != nil || renewed.ExpiresAt == nil || !renewed.ExpiresAt.Equal(now.Add(30*24*time.Hour)) {
		t.Fatalf("renewal access: %+v %v", renewed, err)
	}
	if balance, err := store.CreditBalance(ctx, "bank-user", now); err != nil || balance.Packs != 2000 {
		t.Fatalf("renewal/repeated notification duplicated or lost credits: %+v %v", balance, err)
	}
	if err := s.CancelSubscription(ctx, "bank-user", renewed.ID); err != nil {
		t.Fatal(err)
	}
	if canceled, err := store.ActiveBankSubscription(ctx, "bank-user", now); err != nil || canceled.AutoRenews == nil || *canceled.AutoRenews {
		t.Fatal("cancellation removed paid access or retained renewal")
	}
	now = *renewed.ExpiresAt
	if balance, err := store.CreditBalance(ctx, "bank-user", now.Add(400*24*time.Hour)); err != nil || balance.Packs != 2000 {
		t.Fatalf("cancellation/expiry removed purchased replies: %+v %v", balance, err)
	}
	if err := s.RunDue(ctx); err != nil || calls != 1 {
		t.Fatal("cancellation did not stop the scheduler")
	}
}

func TestBankSubscriptionRejectsMissingOrExpiredRenewalConsent(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*ExpectedQuote)
	}{
		{"unchecked", func(q *ExpectedQuote) { q.RecurringConsent = false }},
		{"old terms", func(q *ExpectedQuote) { q.RecurringTermsVersion = "old" }},
		{"expired card", func(q *ExpectedQuote) { q.RecurringUntil = "20200101" }},
		{"invalid date", func(q *ExpectedQuote) { q.RecurringUntil = "20271340" }},
		{"insufficient mandate", func(q *ExpectedQuote) { q.RecurringUntil = time.Now().UTC().AddDate(0, 0, 10).Format("20060102") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, _ := testCheckout(t)
			enableLegacyBaseOffer(t, s)
			s.cfg.RecurringEnabled = true
			quote := subscriptionQuote(t, s, "base_monthly")
			test.edit(&quote)
			if _, err := s.StartExpected(context.Background(), "bank-user", "base_monthly", "invalid-consent-attempt", quote); err == nil {
				t.Fatal("invalid mandate accepted")
			}
			var count int
			if err := s.db.Reader().QueryRow(`SELECT count(*) FROM billing_orders`).Scan(&count); err != nil || count != 0 {
				t.Fatal("rejected consent persisted an order")
			}
		})
	}
}

type receiptTestSender struct {
	messages []email.Outgoing
	fail     bool
}

func (s *receiptTestSender) Send(_ context.Context, msg email.Outgoing) error {
	s.messages = append(s.messages, msg)
	if s.fail {
		s.fail = false
		return errors.New("temporary delivery failure")
	}
	return nil
}

func TestBankReceiptUsesFrozenLocalizedPayloadAndRetriesOnce(t *testing.T) {
	s, _ := testCheckout(t)
	// Explicit fixtures, with no requests to either bank environment.
	cfg := s.cfg
	cfg.Environment = Production
	cfg.AppEnv = "production"
	cfg.ProductionApproved = true
	cfg.Terminal = "12345678"
	cfg.MACKeyHex = "00112233445566778899AABBCCDDEEFF"
	var err error
	s, err = NewCheckout(cfg, s.db, s.catalog)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	s.now = func() time.Time { return now }
	if _, err := s.db.Writer().Exec(`UPDATE users SET email_verified_at=1 WHERE id='bank-user'`); err != nil {
		t.Fatal(err)
	}
	bundle, err := localization.Load()
	if err != nil {
		t.Fatal(err)
	}
	sender := &receiptTestSender{fail: true}
	s.WithReceiptEmail(sender, email.NotificationTemplates{Translate: bundle.T})
	quote := currentQuote(t, s)
	quote.Locale = "ru"
	order, err := s.StartExpected(context.Background(), "bank-user", "pack30", "receipt-purchase", quote)
	if err != nil {
		t.Fatal(err)
	}
	if w := notifyRequest(s, originalFields(t, s, order.ID), true); w.Code != 200 {
		t.Fatalf("notify: %d %s", w.Code, w.Body.String())
	}
	if err := s.SendReceipts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(sender.messages) != 1 || !strings.Contains(sender.messages[0].Content.Subject, "Оплата подтверждена") {
		t.Fatal("receipt did not render in the purchase language")
	}
	now = now.Add(3 * time.Minute)
	if err := s.SendReceipts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.SendReceipts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(sender.messages) != 2 || !reflect.DeepEqual(sender.messages[0], sender.messages[1]) {
		t.Fatal("retry changed the payload/idempotency key or duplicated a sent receipt")
	}
	var status string
	if err := s.db.Reader().QueryRow(`SELECT status FROM billing_receipt_outbox WHERE order_id=?`, order.ID).Scan(&status); err != nil || status != "sent" {
		t.Fatalf("receipt status: %s %v", status, err)
	}
}

func TestBankReceiptTemplatesExistInEveryLocale(t *testing.T) {
	bundle, err := localization.Load()
	if err != nil {
		t.Fatal(err)
	}
	params := map[string]string{"product": "Base", "amount": "599 KZT", "units": "30", "order": "fixture-order", "date": "2026-12-01", "days": "30", "manage_url": "https://example.test/account"}
	for _, locale := range domain.Locales {
		for _, kind := range []string{"bcc_pack_receipt", "bcc_subscription_receipt", "bcc_renewing_receipt"} {
			msg, err := (email.NotificationTemplates{Translate: bundle.T}).Render(kind, locale, params)
			if err != nil || strings.Contains(msg.Text, "{manage_url}") {
				t.Fatalf("%s/%s: %v", locale, kind, err)
			}
		}
	}
}

// An uncertain refund is bound to its registered request, just like purchases.
func TestRefundedBankSubscriptionCannotReturnThroughAnOldPaymentNotify(t *testing.T) {
	s, _ := testCheckout(t)
	enableLegacyBaseOffer(t, s)
	s.cfg.RecurringEnabled = true
	ctx := context.Background()
	order, err := s.StartExpected(ctx, "bank-user", "base_monthly", "refundable-subscription", subscriptionQuote(t, s, "base_monthly"))
	if err != nil {
		t.Fatal(err)
	}
	original := originalFields(t, s, order.ID)
	if w := notifyRequest(s, original, true); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	s.client.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		fields := url.Values{"ACTION": {"0"}, "RC": {"00"}, "ORDER": {r.Form.Get("ORDER")}, "TERMINAL": {r.Form.Get("TERMINAL")}, "AMOUNT": {r.Form.Get("AMOUNT")}, "CURRENCY": {"398"}, "RRN": {"123456789012"}, "INT_REF": {"FEDCBA9876543210"}}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}, Body: io.NopCloser(strings.NewReader(fields.Encode()))}, nil
	})}
	if _, err := s.Refund(ctx, "fixture-admin", order.ID); err != nil {
		t.Fatal(err)
	}
	if w := notifyRequest(s, original, true); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if _, err := repository.New(s.db).ActiveBankSubscription(ctx, "bank-user", time.Now()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("refunded subscription was restored")
	}
	if balance, err := repository.New(s.db).CreditBalance(ctx, "bank-user", time.Now()); err != nil || balance.Packs != 0 {
		t.Fatalf("refunded monthly bundle remained spendable: %+v %v", balance, err)
	}
}
