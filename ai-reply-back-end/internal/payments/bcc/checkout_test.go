package bcc

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/catalog"
	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/migrations"
)

func testCheckout(t *testing.T) (*CheckoutService, *catalog.Service) {
	t.Helper()
	db, err := database.Open(database.Options{Path: filepath.Join(t.TempDir(), "bank.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if _, err = database.Migrate(ctx, db, migrations.FS); err != nil {
		t.Fatal(err)
	}
	prices := catalog.New(db)
	if err = prices.EnsureInitial(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Writer().Exec(`INSERT INTO users(id,email,status,created_at,updated_at)VALUES('bank-user','bank@example.test','active',1,1)`); err != nil {
		t.Fatal(err)
	}
	cfg := CheckoutConfig{Enabled: true, AppEnv: "test", PublicBaseURL: "https://sandbox.example.test", Merchant: "00000001", MerchantName: "TEST MERCHANT", Terminal: "88888881", MACKeyHex: publicTestKey, NotifyUsername: "bank-sandbox", NotifyPassword: strings.Repeat("a", 40), StateSecret: strings.Repeat("b", 40)}
	s, err := NewCheckout(cfg, db, prices)
	if err != nil {
		t.Fatal(err)
	}
	return s, prices
}
func originalFields(t *testing.T, s *CheckoutService, id string) url.Values {
	t.Helper()
	var raw string
	if err := s.db.Reader().QueryRow(`SELECT request_json FROM billing_orders WHERE id=?`, id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var fields url.Values
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		t.Fatal(err)
	}
	fields.Set("ACTION", "0")
	fields.Set("RC", "00")
	fields.Set("RRN", "123456789012")
	fields.Set("INT_REF", "ABCDEF0123456789")
	fields.Set("TRAN_TRTYPE", "1")
	return fields
}
func notifyRequest(s *CheckoutService, fields url.Values, authenticated bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/payments/bcc/notify", strings.NewReader(fields.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if authenticated {
		r.SetBasicAuth(s.cfg.NotifyUsername, s.cfg.NotifyPassword)
	}
	w := httptest.NewRecorder()
	s.Notify(w, r)
	return w
}
func bankBalance(t *testing.T, s *CheckoutService) int {
	t.Helper()
	var n int
	if err := s.db.Reader().QueryRow(`SELECT coalesce(sum(units_remaining),0) FROM credit_grants WHERE user_id='bank-user'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func TestAuthenticatedFinalNotificationGrantsSnapshotOnce(t *testing.T) {
	s, prices := testCheckout(t)
	ctx := context.Background()
	o, err := s.Start(ctx, "bank-user", "pack30", "purchase-attempt-1")
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.Start(ctx, "bank-user", "pack30", "purchase-attempt-1")
	if err != nil || again.ID != o.ID {
		t.Fatal("idempotent checkout created another order")
	}
	fields := originalFields(t, s, o.ID)
	p := catalog.InitialProducts()
	p[5].PriceMinor = 20000
	p[5].Units = 40
	draft, err := prices.SaveDraft(ctx, "admin", 0, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = prices.Publish(ctx, "admin", draft.ID, draft.Revision); err != nil {
		t.Fatal(err)
	}
	if balance := bankBalance(t, s); balance != 0 {
		t.Fatal("pending order already granted")
	}
	// Echoed P_SIGN is deliberately not trusted; Basic Auth authenticates sender,
	// and the original operation's full immutable binding determines fulfillment.
	fields.Set("P_SIGN", "untrusted-echo")
	var wg sync.WaitGroup
	codes := make(chan int, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); codes <- notifyRequest(s, fields, true).Code }()
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != 200 {
			t.Fatalf("duplicate notify code %d", code)
		}
	}
	if balance := bankBalance(t, s); balance != 30 {
		t.Fatalf("snapshot or idempotency broken: %d", balance)
	}
	status, _ := s.Status(ctx, "bank-user", o.ID)
	if status.Status != "succeeded" {
		t.Fatal(status.Status)
	}
	var n int
	if err = s.db.Reader().QueryRow(`SELECT count(*) FROM credit_ledger WHERE entry='grant'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("ledger duplicated: %d %v", n, err)
	}
	o2, err := s.Start(ctx, "bank-user", "pack30", "purchase-attempt-2")
	if err != nil {
		t.Fatal(err)
	}
	if code := notifyRequest(s, originalFields(t, s, o2.ID), true).Code; code != 200 {
		t.Fatal(code)
	}
	if bankBalance(t, s) != 70 {
		t.Fatal("additional package did not accumulate")
	}
}
func TestUntrustedIntermediateOrMismatchedNotifyNeverGrants(t *testing.T) {
	for _, tc := range []struct {
		name   string
		auth   bool
		change func(url.Values)
	}{
		{"missing auth", false, func(v url.Values) {}},
		{"wrong amount", true, func(v url.Values) { v.Set("AMOUNT", "1.00") }},
		{"wrong currency", true, func(v url.Values) { v.Set("CURRENCY", "840") }},
		{"wrong terminal", true, func(v url.Values) { v.Set("TERMINAL", "another") }},
		{"wrong merchant", true, func(v url.Values) { v.Set("MERCHANT", "another") }},
		{"wrong nonce", true, func(v url.Values) { v.Set("NONCE", "another") }},
		{"status query wrapper", true, func(v url.Values) { v.Set("TRTYPE", "90") }},
		{"missing original type", true, func(v url.Values) { v.Del("TRTYPE"); v.Del("TRAN_TRTYPE") }},
		{"missing bank refs", true, func(v url.Values) { v.Del("RRN") }},
		{"3ds intermediate", true, func(v url.Values) { v.Set("ACTION", "5") }},
		{"declined", true, func(v url.Values) { v.Set("ACTION", "2"); v.Set("RC", "58") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := testCheckout(t)
			o, err := s.Start(context.Background(), "bank-user", "pack30", "purchase-attempt")
			if err != nil {
				t.Fatal(err)
			}
			f := originalFields(t, s, o.ID)
			tc.change(f)
			notifyRequest(s, f, tc.auth)
			if bankBalance(t, s) != 0 {
				t.Fatal("unsafe callback granted credits")
			}
		})
	}
}
func TestBrowserReturnAndQueryDoNotProvePayment(t *testing.T) {
	s, _ := testCheckout(t)
	ctx := context.Background()
	o, err := s.Start(ctx, "bank-user", "pack30", "browser-attempt")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "https://sandbox.example.test/api/v1/payments/bcc/orders/"+o.ID+"/return?state="+s.token(o.ID), strings.NewReader("ACTION=0&RC=00"))
	r.SetPathValue("id", o.ID)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.Return(w, r)
	if w.Code != 200 || bankBalance(t, s) != 0 {
		t.Fatal("browser return confirmed payment")
	}
	// A successful query envelope has no original status contract.
	s.client.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"ACTION":"0","RC":"00"}`))}, nil
	})}
	status, err := s.Reconcile(ctx, "bank-user", o.ID)
	if err != nil || status.Status != "verification_pending" || bankBalance(t, s) != 0 {
		t.Fatalf("query granted: %v %v", status, err)
	}
	s.now = func() time.Time { return time.Now().Add(25 * time.Hour) }
	status, err = s.Reconcile(ctx, "bank-user", o.ID)
	if err != nil || status.Status != "needs_review" {
		t.Fatalf("query window: %v %v", status, err)
	}
}
func TestSandboxCheckoutCannotRunInProduction(t *testing.T) {
	s, _ := testCheckout(t)
	cfg := s.cfg
	cfg.AppEnv = "production"
	if _, err := NewCheckout(cfg, s.db, s.catalog); err == nil {
		t.Fatal("sandbox checkout enabled in production")
	}
	if _, err := s.Start(context.Background(), "bank-user", "base_monthly", "not-recurring"); err == nil {
		t.Fatal("unimplemented recurring product sold as one-off")
	}
}

func TestSameHostSandboxPrefixIsKeptInEveryBankURL(t *testing.T) {
	s, _ := testCheckout(t)
	cfg := s.cfg
	cfg.PublicBaseURL = "https://sandbox.example.test/sandbox"
	prefixed, err := NewCheckout(cfg, s.db, s.catalog)
	if err != nil {
		t.Fatal(err)
	}
	order, err := prefixed.Start(context.Background(), "bank-user", "pack30", "prefix-attempt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(order.RedirectURL, "https://sandbox.example.test:443/sandbox/api/v1/payments/bcc/") {
		t.Fatal("checkout lost isolated sandbox path")
	}
	fields := originalFields(t, prefixed, order.ID)
	for _, key := range []string{"BACKREF", "NOTIFY_URL"} {
		if !strings.HasPrefix(fields.Get(key), "https://sandbox.example.test:443/sandbox/api/v1/payments/bcc/") {
			t.Fatalf("%s lost sandbox prefix", key)
		}
	}
	cfg.PublicBaseURL = "https://sandbox.example.test/other"
	if _, err = NewCheckout(cfg, s.db, s.catalog); err == nil {
		t.Fatal("arbitrary public URL path allowed")
	}
}

func TestFullRefundRevokesOnlyUnusedCreditsAndCannotReplayGrant(t *testing.T) {
	s, _ := testCheckout(t)
	ctx := context.Background()
	order, err := s.Start(ctx, "bank-user", "pack30", "refund-attempt")
	if err != nil {
		t.Fatal(err)
	}
	original := originalFields(t, s, order.ID)
	if notifyRequest(s, original, true).Code != 200 {
		t.Fatal("purchase did not complete")
	}
	if _, err = s.db.Writer().Exec(`UPDATE credit_grants SET units_remaining=12 WHERE billing_order_id=?`, order.ID); err != nil {
		t.Fatal(err)
	}
	calls := 0
	s.client.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("TRTYPE") != "14" || r.Form.Get("AMOUNT") != "100.00" || r.Form.Get("RRN") != "123456789012" {
			t.Fatal("refund not bound to paid original")
		}
		response := url.Values{"ACTION": {"0"}, "RC": {"00"}, "ORDER": {r.Form.Get("ORDER")}, "TERMINAL": {r.Form.Get("TERMINAL")}, "AMOUNT": {r.Form.Get("AMOUNT")}, "CURRENCY": {"398"}, "RRN": {"123456789012"}, "INT_REF": {"FEDCBA9876543210"}}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}, Body: io.NopCloser(strings.NewReader(response.Encode()))}, nil
	})}
	refunded, err := s.Refund(ctx, "admin@example.test", order.ID)
	if err != nil || refunded.Status != "refunded" {
		t.Fatalf("refund: %v %v", refunded, err)
	}
	if bankBalance(t, s) != 0 {
		t.Fatal("remaining refunded units retained")
	}
	var delta int
	if err = s.db.Reader().QueryRow(`SELECT delta FROM credit_ledger WHERE entry='revoke'`).Scan(&delta); err != nil || delta != -12 {
		t.Fatalf("used credits incorrectly revoked: %d %v", delta, err)
	}
	if _, err = s.Refund(ctx, "admin@example.test", order.ID); err != nil || calls != 1 {
		t.Fatal("refund sent twice")
	}
	if notifyRequest(s, original, true).Code != 200 || bankBalance(t, s) != 0 {
		t.Fatal("old purchase callback revived refunded credits")
	}
}
func TestUncertainRefundIsNotRetriedAndWaitsForAuthenticatedCallback(t *testing.T) {
	s, _ := testCheckout(t)
	ctx := context.Background()
	order, err := s.Start(ctx, "bank-user", "pack30", "uncertain-refund")
	if err != nil {
		t.Fatal(err)
	}
	if notifyRequest(s, originalFields(t, s, order.ID), true).Code != 200 {
		t.Fatal("purchase did not complete")
	}
	calls := 0
	s.client.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { calls++; return nil, io.ErrUnexpectedEOF })}
	if _, err = s.Refund(ctx, "admin", order.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Refund(ctx, "admin", order.ID); err != nil || calls != 1 {
		t.Fatal("uncertain money request retried")
	}
	if bankBalance(t, s) != 30 {
		t.Fatal("uncertain refund revoked rights")
	}
	fields, err := s.refundRequest(ctx, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	fields.Set("ACTION", "0")
	fields.Set("RC", "00")
	fields.Set("TRAN_TRTYPE", "14")
	fields.Set("RRN", "123456789012")
	fields.Set("INT_REF", "FEDCBA9876543210")
	if notifyRequest(s, fields, true).Code != 200 || bankBalance(t, s) != 0 {
		t.Fatal("authenticated refund did not revoke")
	}
	if notifyRequest(s, fields, true).Code != 200 {
		t.Fatal("duplicate refund failed")
	}
	var n int
	if err = s.db.Reader().QueryRow(`SELECT count(*) FROM credit_ledger WHERE entry='revoke'`).Scan(&n); err != nil || n != 1 {
		t.Fatal("duplicate refund revoked twice")
	}
}

func TestHostedCheckoutUsesRealBrowserIPAndCollectsRequired3DSData(t *testing.T) {
	s, _ := testCheckout(t)
	order, err := s.Start(context.Background(), "bank-user", "pack30", "browser-info-attempt")
	if err != nil {
		t.Fatal(err)
	}
	fields := originalFields(t, s, order.ID)
	if value := fields.Get("MERCH_RN_ID"); len(value) != 16 {
		t.Fatal("mandatory merchant request ID missing")
	}
	r := httptest.NewRequest(http.MethodGet, order.RedirectURL, nil)
	r.SetPathValue("id", order.ID)
	w := httptest.NewRecorder()
	s.CheckoutPageWithClientIP(w, r, "203.0.113.9")
	if w.Code != 200 {
		t.Fatalf("checkout page: %d", w.Code)
	}
	body := w.Body.String()
	for _, required := range []string{`name="CLIENT_IP" value="203.0.113.9"`, `name="M_INFO"`, `billAddrLine1: address`, `browserScreenHeight: height`, `browserScreenWidth: width`, `mobilePhone: { cc: cc, subscriber: subscriber }`, `new TextEncoder()`, `maxlength="50"`} {
		if !strings.Contains(body, required) {
			t.Fatalf("required 3DS field missing: %s", required)
		}
	}
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "script-src 'nonce-") || strings.Contains(w.Header().Get("Content-Security-Policy"), "unsafe-inline") {
		t.Fatal("3DS script lacks nonce-based CSP")
	}
	if fields.Get("CLIENT_IP") != "" || fields.Get("M_INFO") != "" {
		t.Fatal("browser-specific/payer data retained in immutable order")
	}
	invalid := httptest.NewRecorder()
	s.CheckoutPageWithClientIP(invalid, r, "0.0.0.0")
	if invalid.Code != 400 {
		t.Fatal("placeholder client IP allowed")
	}
}
func TestMerchantNameContractMatchesPublishedBankFormat(t *testing.T) {
	s, _ := testCheckout(t)
	for _, name := range []string{"Mixed Case", "NAME LONGER THAN TWENTY ONE CHARACTERS", " MERCHANT", ""} {
		cfg := s.cfg
		cfg.MerchantName = name
		if cfg.Validate() == nil {
			t.Fatalf("invalid merchant name accepted: %q", name)
		}
	}
	cfg := s.cfg
	cfg.Merchant = "non-numeric"
	if cfg.Validate() == nil {
		t.Fatal("non-numeric merchant accepted")
	}
}
