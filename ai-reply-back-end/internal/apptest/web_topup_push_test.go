package apptest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aireply/ai-reply-back-end/internal/auth"
	"github.com/aireply/ai-reply-back-end/internal/catalog"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/middleware"
	"github.com/aireply/ai-reply-back-end/internal/payments/bcc"
	"github.com/aireply/ai-reply-back-end/internal/plans"
	"github.com/aireply/ai-reply-back-end/internal/subscriptions"
	"github.com/aireply/ai-reply-back-end/internal/transport/api"
)

func webOfferHarness(t *testing.T, enabled bool) (*harness, *catalog.Service) {
	t.Helper()
	flag := "false"
	if enabled {
		flag = "true"
	}
	h := newHarness(t, approvedEntitlements(), withEnv("WEB_TOPUP_OFFER_PUSH_ENABLED", flag), withEnv("PUBLIC_BASE_URL", "https://ai-reply.kz"), withEnv("BCC_CHECKOUT_ENABLED", "false"))
	prices := catalog.New(h.db)
	if err := prices.EnsureInitial(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.notify.WithWebTopupOffers(prices, "https://ai-reply.kz/sandbox")
	return h, prices
}

func TestWebsiteOfferWaitsForExplicitConsentAndReadyRegistration(t *testing.T) {
	h, _ := webOfferHarness(t, true)
	s := h.signIn("web-offer@example.com")
	if r := h.do(http.MethodPut, "/api/v1/me/notification-preferences", map[string]any{"preferences": map[string]bool{"marketing": true}}, h.auth(s.access)); r.status != http.StatusOK {
		t.Fatal(r.raw)
	}
	if n := h.scalar(`SELECT count(*) FROM notifications WHERE type='web_topup_offer'`); n != 0 {
		t.Fatal("early opt-in consumed once-only event")
	}
	h.mustRegister(installation(installID(651), "ios", ""), s.access)
	if n := h.scalar(`SELECT count(*) FROM notifications WHERE type='web_topup_offer'`); n != 0 {
		t.Fatal("registration without token consumed event")
	}
	h.mustRegister(installation(installID(651), "ios", fcmToken(651)), s.access)
	h.mustRegister(installation(installID(651), "ios", fcmToken(651)), s.access)
	if n := h.scalar(`SELECT count(*) FROM notifications WHERE type='web_topup_offer'`); n != 1 {
		t.Fatalf("once-only event count %d", n)
	}
	h.tick()
	messages := h.push.messages()
	if len(messages) != 1 {
		t.Fatalf("sent %d messages", len(messages))
	}
	msg := messages[0].msg
	if msg.Category != domain.CategoryMarketing || msg.Data["type"] != domain.TypeWebTopupOffer || msg.Data["link"] != "https://ai-reply.kz/sandbox/payment" || !strings.Contains(msg.Body, "30") || !strings.Contains(msg.Body, "100") {
		t.Fatalf("wrong offer: %+v", msg)
	}
	if n := h.scalar(`SELECT count(*) FROM notification_deliveries WHERE channel='email'`); n != 0 {
		t.Fatal("push consent queued email")
	}
	h.notify.MaybeQueueWebTopupOffer(context.Background(), s.userID)
	h.tick()
	if len(h.push.messages()) != 1 {
		t.Fatal("offer sent twice")
	}
	// Notification retention and a later preference toggle must not restart
	// marketing for an account that already used its one-off promotion.
	if _, err := h.db.Writer().Exec(`DELETE FROM notifications WHERE type='web_topup_offer'`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.notify.SetPreferences(context.Background(), s.userID, map[string]bool{"marketing": false}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.notify.SetPreferences(context.Background(), s.userID, map[string]bool{"marketing": true}); err != nil {
		t.Fatal(err)
	}
	h.mustRegister(installation(installID(651), "ios", fcmToken(651)), s.access)
	if h.scalar(`SELECT count(*) FROM notifications WHERE type='web_topup_offer'`) != 0 {
		t.Fatal("retention reset the once-per-account offer")
	}
	if h.scalar(`SELECT count(*) FROM notification_preferences WHERE user_id=? AND category='marketing' AND web_topup_offer_queued_at IS NOT NULL`, s.userID) != 1 {
		t.Fatal("preference update removed the durable once marker")
	}
}

func TestWebsiteOfferHonorsFeatureConsentAndPublishedOffer(t *testing.T) {
	for _, name := range []string{"feature_off", "no_consent", "opted_out", "catalog_changed"} {
		t.Run(name, func(t *testing.T) {
			h, prices := webOfferHarness(t, name != "feature_off")
			s := h.signIn("web-offer-gates@example.com")
			h.mustRegister(installation(installID(652), "ios", fcmToken(652)), s.access)
			if name != "no_consent" {
				if _, err := h.notify.SetPreferences(context.Background(), s.userID, map[string]bool{"marketing": true}); err != nil {
					t.Fatal(err)
				}
			}
			if name == "opted_out" {
				if _, err := h.notify.SetPreferences(context.Background(), s.userID, map[string]bool{"marketing": false}); err != nil {
					t.Fatal(err)
				}
			}
			if name == "catalog_changed" {
				products := catalog.InitialProducts()
				products[5].PriceMinor = 20000
				draft, err := prices.SaveDraft(context.Background(), "operator", 0, products)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = prices.Publish(context.Background(), "operator", draft.ID, draft.Revision); err != nil {
					t.Fatal(err)
				}
			}
			h.tick()
			if len(h.push.messages()) != 0 {
				t.Fatal("unavailable, stale or unconsented offer was delivered")
			}
		})
	}
}

func TestWebsiteOfferRequiresCompatibleBuildBeforeQueueAndDispatch(t *testing.T) {
	for _, build := range []string{"13", "", "14beta", "+14", "9999999999"} {
		name := build
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			h, _ := webOfferHarness(t, true)
			s := h.signIn("web-offer-version@example.com")
			if _, err := h.notify.SetPreferences(context.Background(), s.userID, map[string]bool{"marketing": true}); err != nil {
				t.Fatal(err)
			}
			body := installation(installID(653), "ios", fcmToken(653))
			body["app_version"], body["app_build"] = "1.2.0", build
			h.mustRegister(body, s.access)
			if h.scalar(`SELECT count(*) FROM notifications WHERE type='web_topup_offer'`) != 0 {
				t.Fatal("unsupported client consumed offer dedupe")
			}
			body["app_build"] = "14"
			h.mustRegister(body, s.access)
			if h.scalar(`SELECT count(*) FROM notifications WHERE type='web_topup_offer'`) != 1 {
				t.Fatal("compatible upgrade did not queue once")
			}
			body["app_build"] = "13"
			h.mustRegister(body, s.access)
			h.tick()
			if len(h.push.messages()) != 0 {
				t.Fatal("downgraded client received external payment offer")
			}
		})
	}
}

func TestWebsiteCatalogOfferWithBankOffStillKeepsCheckoutAndCreditsDisabled(t *testing.T) {
	h, prices := webOfferHarness(t, true)
	ctx := context.Background()
	bank, err := bcc.NewCheckout(h.cfg.BCCCheckout, h.db, prices)
	if err != nil || bank.Enabled() {
		t.Fatalf("fixture must have disabled bank checkout: %v", err)
	}
	s := h.signIn("catalog-offer-bank-off@example.com")
	body := installation(installID(654), "ios", fcmToken(654))
	body["app_version"], body["app_build"] = "1.2.0", "14"
	h.mustRegister(body, s.access)
	if response := h.do(http.MethodPut, "/api/v1/me/notification-preferences", map[string]any{
		"expected_user_id": s.userID, "preferences": map[string]bool{"marketing": true},
	}, h.auth(s.access)); response.status != http.StatusOK {
		t.Fatal("explicit marketing opt-in failed")
	}
	h.tick()
	messages := h.push.messages()
	if len(messages) != 1 || messages[0].msg.Category != domain.CategoryMarketing || messages[0].msg.Data["type"] != domain.TypeWebTopupOffer || messages[0].msg.Data["link"] != "https://ai-reply.kz/sandbox/payment" || !strings.Contains(messages[0].msg.Body, "30") || !strings.Contains(messages[0].msg.Body, "100") {
		t.Fatal("bank readiness blocked or changed the ordinary catalog offer")
	}

	// Use the same authenticated account/database for the website. Advertising
	// the catalog must not enable the independently gated payment handler.
	cfg := h.cfg
	cfg.App.PublicBaseURL = "https://ai-reply.kz/sandbox"
	planSvc := plans.New(h.store)
	server := api.New(api.Deps{Config: cfg, Auth: h.authSvc, Users: h.users, Plans: planSvc,
		Subs: subscriptions.New(h.store, planSvc, cfg.App.Location()), Credits: h.credits,
		Notifications: h.notify, Installations: h.installations, Limiter: middleware.NewLimiter(), Log: discardLog()})
	mux := http.NewServeMux()
	server.RegisterWebAccount(mux, bank, prices)
	handoff, err := h.authSvc.CreateWebPaymentHandoff(ctx, s.userID, "en")
	if err != nil {
		t.Fatal(err)
	}
	web, err := h.authSvc.ConsumeWebPaymentHandoff(ctx, handoff.Code, auth.DeviceInfo{})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "https://ai-reply.kz/sandbox/account/api/checkout", strings.NewReader(`{"product_code":"pack30","accept_terms":true}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://ai-reply.kz")
	r.Header.Set("X-Requested-With", "AIReplyAccount")
	r.AddCookie(&http.Cookie{Name: "aireply_web_sandbox_session", Value: web.RefreshToken})
	w := httptest.NewRecorder()
	http.StripPrefix("/sandbox", mux).ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "WEB_PAYMENTS_UNAVAILABLE") {
		t.Fatal("catalog offer enabled checkout with the bank switched off")
	}
	if h.scalar(`SELECT count(*) FROM billing_orders`) != 0 || h.scalar(`SELECT count(*) FROM credit_grants WHERE kind='pack'`) != 0 {
		t.Fatal("catalog offer created a payment or granted purchased replies")
	}

	other := h.signIn("catalog-offer-opted-out@example.com")
	body = installation(installID(655), "ios", fcmToken(655))
	body["app_version"], body["app_build"] = "1.2.0", "14"
	h.mustRegister(body, other.access)
	if _, err = h.notify.SetPreferences(ctx, other.userID, map[string]bool{"marketing": false}); err != nil {
		t.Fatal(err)
	}
	h.tick()
	if len(h.push.messages()) != 1 || h.scalar(`SELECT count(*) FROM notifications WHERE user_id=? AND type='web_topup_offer'`, other.userID) != 0 {
		t.Fatal("catalog offer ignored marketing opt-out")
	}
}
