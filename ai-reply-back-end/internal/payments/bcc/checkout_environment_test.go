package bcc

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Dummy configuration only: constructors do not make a bank request.
func productionCheckoutFixture(s *CheckoutService) CheckoutConfig {
	cfg := s.cfg
	cfg.Environment, cfg.AppEnv, cfg.ProductionApproved = Production, "production", true
	cfg.PublicBaseURL = "https://payment.example.test"
	cfg.Merchant, cfg.Terminal = "12345678", "12345678"
	cfg.MACKeyHex = "00112233445566778899AABBCCDDEEFF"
	cfg.NotifyUsername, cfg.NotifyPassword, cfg.StateSecret = "production-bank", strings.Repeat("p", 40), strings.Repeat("q", 40)
	return cfg
}

func TestProductionCheckoutRequiresExplicitApprovalAndLiveCredentials(t *testing.T) {
	s, _ := testCheckout(t)
	for name, change := range map[string]func(*CheckoutConfig){
		"no opt in":       func(c *CheckoutConfig) { c.ProductionApproved = false },
		"test backend":    func(c *CheckoutConfig) { c.AppEnv = "test" },
		"sandbox route":   func(c *CheckoutConfig) { c.PublicBaseURL += "/sandbox" },
		"empty URL query": func(c *CheckoutConfig) { c.PublicBaseURL += "?" },
		"public terminal": func(c *CheckoutConfig) { c.Terminal = "88888881" },
		"test terminal":   func(c *CheckoutConfig) { c.Terminal = "88888888" },
		"public key":      func(c *CheckoutConfig) { c.MACKeyHex = strings.ToLower(publicTestKey) },
		"missing mode":    func(c *CheckoutConfig) { c.Environment = "" },
		"unknown mode":    func(c *CheckoutConfig) { c.Environment = "prod" },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := productionCheckoutFixture(s)
			change(&cfg)
			if _, err := NewCheckout(cfg, s.db, s.catalog); err == nil {
				t.Fatal("unsafe production checkout accepted")
			}
		})
	}
}

func TestProductionOrdersStayBoundToTheirEnvironment(t *testing.T) {
	sandbox, _ := testCheckout(t)
	production, err := NewCheckout(productionCheckoutFixture(sandbox), sandbox.db, sandbox.catalog)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	sandOrder, err := sandbox.Start(ctx, "bank-user", "pack30", "sandbox-attempt")
	if err != nil {
		t.Fatal(err)
	}
	prodOrder, err := production.Start(ctx, "bank-user", "pack30", "production-attempt")
	if err != nil {
		t.Fatal(err)
	}
	if production.Environment() != Production || prodOrder.Environment != "production" || production.client.endpoint != ProductionEndpoint {
		t.Fatal("production mode or endpoint lost")
	}
	// Even accidentally sharing a database cannot make one environment's order
	// accessible or fulfillable through the other environment's service.
	if _, err = production.Status(ctx, "bank-user", sandOrder.ID); err == nil {
		t.Fatal("production can read a sandbox order")
	}
	if _, err = production.Start(ctx, "bank-user", "pack30", "sandbox-attempt"); err == nil {
		t.Fatal("cross-environment idempotency reused a sandbox checkout")
	}
	if _, err = production.Refund(ctx, "admin", sandOrder.ID); err == nil {
		t.Fatal("production can refund a sandbox order")
	}
	if code := notifyRequest(production, originalFields(t, sandbox, sandOrder.ID), true).Code; code != http.StatusNotFound {
		t.Fatalf("cross-environment notification returned %d", code)
	}
	for i := 0; i < 2; i++ {
		if code := notifyRequest(production, originalFields(t, production, prodOrder.ID), true).Code; code != http.StatusOK {
			t.Fatalf("production authenticated fixture rejected: %d", code)
		}
	}
	if bankBalance(t, production) != 30 {
		t.Fatal("production notification did not grant exactly one pack")
	}
	orders, err := production.Orders(ctx)
	if err != nil || len(orders) != 1 || orders[0].ID != prodOrder.ID {
		t.Fatal("administrator order list crossed environments")
	}
	if status, err := sandbox.Status(ctx, "bank-user", sandOrder.ID); err != nil || status.Status != "created" {
		t.Fatal("production callback changed sandbox order")
	}
	if sandbox.token("same-id") == production.token("same-id") {
		t.Fatal("checkout capabilities crossed environments")
	}
}

func TestProductionHostedFormUsesOnlyProductionEndpoint(t *testing.T) {
	sandbox, _ := testCheckout(t)
	s, err := NewCheckout(productionCheckoutFixture(sandbox), sandbox.db, sandbox.catalog)
	if err != nil {
		t.Fatal(err)
	}
	order, err := s.Start(context.Background(), "bank-user", "pack30", "production-form")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, order.RedirectURL, nil)
	r.SetPathValue("id", order.ID)
	w := httptest.NewRecorder()
	s.CheckoutPageWithClientIP(w, r, "192.0.2.1")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `action="`+ProductionEndpoint+`"`) || strings.Contains(w.Body.String(), "BCC sandbox") || strings.Contains(w.Body.String(), SandboxEndpoint) {
		t.Fatal("production form used a test bank or label")
	}
	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "form-action https://3dsecure.bcc.kz;") || strings.Contains(csp, "test3ds") {
		t.Fatal("production form CSP used the wrong bank")
	}
}

func TestBrowserReturnNavigatesToOwnedOrderWithoutBankClaims(t *testing.T) {
	for name, prefix := range map[string]string{"root": "", "sandbox": "/sandbox"} {
		t.Run(name, func(t *testing.T) {
			s, _ := testCheckout(t)
			s.cfg.PublicBaseURL += prefix
			order, err := s.Start(context.Background(), "bank-user", "pack30", "return-attempt")
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodPost, s.baseURL()+"/return?state="+s.token(order.ID)+"&redirect=https://evil.example", strings.NewReader("ACTION=0&RC=00&success=true"))
			r.SetPathValue("id", order.ID)
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			s.Return(w, r)
			want := "https://sandbox.example.test:443" + prefix + "/payment?order=" + order.ID
			if w.Code != http.StatusSeeOther || w.Header().Get("Location") != want || bankBalance(t, s) != 0 {
				t.Fatal("browser return trusted bank claims or lost isolated account path")
			}
			status, err := s.Status(context.Background(), "bank-user", order.ID)
			if err != nil || status.Status != "verification_pending" {
				t.Fatal("browser return did not preserve pending verification")
			}
			r.URL.RawQuery = "state=invalid"
			w = httptest.NewRecorder()
			s.Return(w, r)
			if w.Code != http.StatusNotFound || w.Header().Get("Location") != "" {
				t.Fatal("invalid browser capability redirected to an order")
			}
		})
	}
}

func TestProductionFullRefundUsesLiveEndpointAndCannotBeSentTwice(t *testing.T) {
	for _, viaNotification := range []bool{false, true} {
		name := "direct_response"
		if viaNotification {
			name = "authenticated_notification"
		}
		t.Run(name, func(t *testing.T) {
			sandbox, _ := testCheckout(t)
			s, err := NewCheckout(productionCheckoutFixture(sandbox), sandbox.db, sandbox.catalog)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			order, err := s.Start(ctx, "bank-user", "pack30", "production-refund")
			if err != nil {
				t.Fatal(err)
			}
			original := originalFields(t, s, order.ID)
			if notifyRequest(s, original, true).Code != http.StatusOK {
				t.Fatal("purchase fixture was not confirmed")
			}
			if _, err = s.db.Writer().Exec(`UPDATE credit_grants SET units_remaining=12 WHERE billing_order_id=?`, order.ID); err != nil {
				t.Fatal(err)
			}
			calls := 0
			var refund url.Values
			s.client.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() != ProductionEndpoint {
					t.Fatal("production refund used a test or arbitrary endpoint")
				}
				if err := r.ParseForm(); err != nil {
					t.Fatal(err)
				}
				refund = r.Form
				if refund.Get("TRTYPE") != "14" || refund.Get("ORDER") != order.bankOrder || refund.Get("TERMINAL") != s.cfg.Terminal || refund.Get("AMOUNT") != "100.00" || refund.Get("RRN") != original.Get("RRN") || refund.Get("INT_REF") != original.Get("INT_REF") {
					t.Fatal("refund was not bound to the paid original")
				}
				if viaNotification {
					return nil, ErrUnavailable
				}
				response := url.Values{"ACTION": {"0"}, "RC": {"00"}, "TRTYPE": {"14"}, "ORDER": {refund.Get("ORDER")}, "TERMINAL": {refund.Get("TERMINAL")}, "AMOUNT": {refund.Get("AMOUNT")}, "CURRENCY": {CurrencyKZT}, "RRN": {original.Get("RRN")}, "INT_REF": {"REFUND0123456789"}}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}, Body: io.NopCloser(strings.NewReader(response.Encode()))}, nil
			})}
			result, err := s.Refund(ctx, "admin", order.ID)
			if err != nil {
				t.Fatal(err)
			}
			if viaNotification {
				if result.Status != "succeeded" || result.RefundStatus != "pending" || bankBalance(t, s) != 12 {
					t.Fatal("uncertain bank response revoked credits")
				}
				refund.Set("ACTION", "0")
				refund.Set("RC", "00")
				refund.Set("RRN", original.Get("RRN"))
				refund.Set("INT_REF", "REFUND0123456789")
				if notifyRequest(s, refund, false).Code != http.StatusUnauthorized || bankBalance(t, s) != 12 {
					t.Fatal("unauthenticated refund changed the balance")
				}
				for i := 0; i < 2; i++ {
					if notifyRequest(s, refund, true).Code != http.StatusOK {
						t.Fatal("registered authenticated refund was rejected")
					}
				}
			}
			if bankBalance(t, s) != 0 {
				t.Fatal("refund retained unused credits")
			}
			result, err = s.Refund(ctx, "admin", order.ID)
			if err != nil || result.Status != "refunded" || calls != 1 {
				t.Fatal("refund did not remain idempotent")
			}
			var count, delta int
			if err = s.db.Reader().QueryRow(`SELECT count(*),coalesce(sum(delta),0) FROM credit_ledger WHERE entry='revoke'`).Scan(&count, &delta); err != nil || count != 1 || delta != -12 {
				t.Fatal("refund revoked spent credits or duplicated the ledger")
			}
			if notifyRequest(s, original, true).Code != http.StatusOK || bankBalance(t, s) != 0 {
				t.Fatal("replayed purchase restored refunded credits")
			}
		})
	}
}

func TestOwnedStatusResumesOnlyAnUnexpiredCheckoutAndNeverAssumesUnpaid(t *testing.T) {
	s, _ := testCheckout(t)
	ctx := context.Background()
	order, err := s.Start(ctx, "bank-user", "pack30", "resume-checkout")
	if err != nil {
		t.Fatal(err)
	}
	status, err := s.Status(ctx, "bank-user", order.ID)
	if err != nil || status.RedirectURL != order.RedirectURL {
		t.Fatal("owned unexpired order could not resume")
	}
	if other, err := s.Status(ctx, "another-user", order.ID); err == nil || other.RedirectURL != "" {
		t.Fatal("another account obtained the checkout capability")
	}
	// Rendering does not prove whether the browser submitted the bank form.
	r := httptest.NewRequest(http.MethodGet, order.RedirectURL, nil)
	r.SetPathValue("id", order.ID)
	w := httptest.NewRecorder()
	s.CheckoutPageWithClientIP(w, r, "192.0.2.1")
	if w.Code != http.StatusOK {
		t.Fatal("checkout fixture did not render")
	}
	s.now = func() time.Time { return time.UnixMilli(order.createdAt).Add(16 * time.Minute) }
	status, err = s.Status(ctx, "bank-user", order.ID)
	if err != nil || status.RedirectURL != "" || status.Status != "created" || status.VerificationReason != "checkout_expired_bank_confirmation_pending" {
		t.Fatal("checkout expiry guessed a final payment result")
	}
	replay, err := s.Start(ctx, "bank-user", "pack30", "resume-checkout")
	if err != nil || replay.ID != order.ID || replay.RedirectURL != "" {
		t.Fatal("expired retry replaced or reopened the original order")
	}
	if notifyRequest(s, originalFields(t, s, order.ID), true).Code != http.StatusOK || bankBalance(t, s) != 30 {
		t.Fatal("late valid bank confirmation was lost after URL expiry")
	}
}
