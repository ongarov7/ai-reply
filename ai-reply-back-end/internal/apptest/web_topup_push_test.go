package apptest

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/aireply/ai-reply-back-end/internal/catalog"
	"github.com/aireply/ai-reply-back-end/internal/domain"
)

func webOfferHarness(t *testing.T, enabled bool) (*harness, *catalog.Service) {
	t.Helper()
	flag := "false"
	if enabled {
		flag = "true"
	}
	h := newHarness(t, approvedEntitlements(), withEnv("WEB_TOPUP_OFFER_PUSH_ENABLED", flag), withEnv("PUBLIC_BASE_URL", "https://ai-reply.kz"))
	prices := catalog.New(h.db)
	if err := prices.EnsureInitial(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.notify.WithWebTopupOffers(func() bool { return true }, prices, "https://ai-reply.kz/sandbox")
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

func TestWebsiteOfferHonorsFeatureBankConsentAndPublishedOffer(t *testing.T) {
	for _, name := range []string{"feature_off", "bank_off", "no_consent", "opted_out", "catalog_changed"} {
		t.Run(name, func(t *testing.T) {
			h, prices := webOfferHarness(t, name != "feature_off")
			bank := name != "bank_off"
			h.notify.WithWebTopupOffers(func() bool { return bank }, prices, "https://ai-reply.kz/sandbox")
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
