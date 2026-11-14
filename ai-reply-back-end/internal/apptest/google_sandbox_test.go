package apptest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/iap/googleplay"
)

func googleSandboxHarness(t *testing.T) *harness {
	t.Helper()
	h := googleHarness(t, withEnv("TEST_GOOGLE_TEST_ONLY", "true"))
	h.openIAP()
	return h
}

func TestGoogleSandboxRejectsRealPromoAndPendingPurchasesBeforeGrantOrAcknowledgement(t *testing.T) {
	h := googleSandboxHarness(t)
	s := h.signIn("sandbox-reject@example.com")
	for i, kind := range []string{"real-pack", "promo-pack", "pending-pack", "real-subscription", "pending-subscription"} {
		token := playToken(400 + i)
		product := packGoogle
		if strings.HasSuffix(kind, "subscription") {
			product = googleBase
			h.playSubscription(s.userID, product, token, h.clock.Now().Add(5*time.Minute), func(p *googleplay.Subscription) {
				if strings.HasPrefix(kind, "pending") {
					p.SubscriptionState = domain.GoogleSubPending
				}
			})
		} else {
			h.playPurchase(s.userID, product, token, func(p *googleplay.ProductPurchase) {
				if strings.HasPrefix(kind, "promo") {
					v := googleplay.TypePromo
					p.PurchaseType = &v
				}
				if strings.HasPrefix(kind, "pending") {
					p.PurchaseState = googleplay.StatePending
				}
			})
		}
		res := h.postGoogle(s.access, product, token)
		if res.status != http.StatusBadRequest || res.str("error", "details", "reason") != domain.PurchaseReasonEnvironment {
			t.Fatalf("%s: %d %s", kind, res.status, res.raw)
		}
	}
	if h.scalar(`SELECT COUNT(*) FROM iap_purchases`)+h.scalar(`SELECT COUNT(*) FROM google_subscriptions`)+h.scalar(`SELECT COUNT(*) FROM credit_grants`) != 0 ||
		h.play.ConsumeCalls.Load() != 0 || h.play.AcknowledgeCalls.Load() != 0 {
		t.Fatal("sandbox recorded, granted, consumed or acknowledged a real purchase")
	}
}

func TestGoogleSandboxAcceptsBoundLicensePurchasesOnceAndRetriesBeforeTestRefundWindow(t *testing.T) {
	h := googleSandboxHarness(t)
	s := h.signIn("sandbox-license@example.com")
	pack, sub := playToken(410), playToken(411)
	h.playPurchase(s.userID, packGoogle, pack, func(p *googleplay.ProductPurchase) { p.PurchaseType = new(int) })
	h.playSubscription(s.userID, googleBase, sub, h.clock.Now().Add(5*time.Minute), func(p *googleplay.Subscription) { p.TestPurchase = &googleplay.TestPurchase{} })
	h.play.FailConsumes(1)
	h.play.FailAcknowledges(1)
	mustStatus(t, h.postGoogle(s.access, packGoogle, pack), http.StatusOK, "")
	mustStatus(t, h.postGoogle(s.access, googleBase, sub), http.StatusOK, "")
	if h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE environment='Test' AND status='granted'`) != 2 || h.entitlement(s.userID).Balance.Packs != 1050 {
		t.Fatal("license receipts were not granted to the sandbox account")
	}
	h.clock.Advance(30 * time.Second)
	h.iap.Tick(context.Background())
	if h.play.Purchase(packGoogle, pack).ConsumptionState != googleplay.Consumed || !h.play.Subscription(sub).Acknowledged() ||
		h.play.ConsumeCalls.Load() != 2 || h.play.AcknowledgeCalls.Load() != 2 {
		t.Fatal("sandbox retry missed the license-test acknowledgement window")
	}
	mustStatus(t, h.postGoogle(s.access, packGoogle, pack), http.StatusOK, "")
	mustStatus(t, h.postGoogle(s.access, googleBase, sub), http.StatusOK, "")
	if h.entitlement(s.userID).Balance.Packs != 1050 || h.scalar(`SELECT COUNT(*) FROM iap_purchases`) != 2 {
		t.Fatal("replay minted credits")
	}
	h.renewOn(sub, "GPA.sandbox-renewal", h.clock.Now().Add(10*time.Minute))
	h.clock.Advance(30 * time.Second)
	h.iap.Tick(context.Background())
	if h.baseReplies(s.userID) != 2000 {
		t.Fatal("sandbox worker missed the accelerated renewal")
	}
	if h.dbContains(pack) || h.dbContains(sub) {
		t.Fatal("raw purchase token persisted")
	}
}

func TestGoogleSandboxWorkerRefusesRealRowsAndRealGoogleResponses(t *testing.T) {
	for _, corruptRow := range []bool{false, true} {
		t.Run(map[bool]string{false: "google-response", true: "stored-environment"}[corruptRow], func(t *testing.T) {
			h := googleSandboxHarness(t)
			s := h.signIn("sandbox-worker@example.com")
			pack, sub := playToken(420), playToken(421)
			h.playPurchase(s.userID, packGoogle, pack, func(p *googleplay.ProductPurchase) { p.PurchaseType = new(int) })
			h.playSubscription(s.userID, googleBase, sub, h.clock.Now().Add(5*time.Minute), func(p *googleplay.Subscription) { p.TestPurchase = &googleplay.TestPurchase{} })
			h.play.FailConsumes(1)
			h.play.FailAcknowledges(1)
			mustStatus(t, h.postGoogle(s.access, packGoogle, pack), http.StatusOK, "")
			mustStatus(t, h.postGoogle(s.access, googleBase, sub), http.StatusOK, "")
			if corruptRow {
				h.exec(`UPDATE iap_purchases SET environment='Production'`)
				h.exec(`UPDATE google_subscriptions SET environment='Production'`)
			} else {
				h.play.Update(packGoogle, pack, func(p *googleplay.ProductPurchase) { p.PurchaseType = nil })
				h.play.UpdateSubscription(sub, func(p *googleplay.Subscription) {
					p.TestPurchase = nil
					p.LatestOrderID = "GPA.real-order"
					p.LineItems[0].ExpiryTime = h.clock.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339)
				})
			}
			h.clock.Advance(30 * time.Second)
			h.iap.Tick(context.Background())
			if h.play.ConsumeCalls.Load() != 1 || h.play.AcknowledgeCalls.Load() != 1 || h.scalar(`SELECT COUNT(*) FROM iap_purchases`) != 2 ||
				h.scalar(`SELECT COUNT(*) FROM google_subscriptions WHERE acknowledged_at IS NOT NULL`) != 0 || h.baseReplies(s.userID) != 1000 {
				t.Fatal("worker consumed, acknowledged or granted a production receipt")
			}
		})
	}
}

func TestGoogleSandboxRefundFeedAffectsOnlyKnownLocalTestReceipts(t *testing.T) {
	h := googleSandboxHarness(t)
	s := h.signIn("sandbox-refund@example.com")
	known, unknown := playToken(430), playToken(431)
	h.playPurchase(s.userID, packGoogle, known, func(p *googleplay.ProductPurchase) { p.PurchaseType = new(int) })
	mustStatus(t, h.postGoogle(s.access, packGoogle, known), http.StatusOK, "")
	for _, token := range []string{known, unknown} {
		h.play.Void(googleplay.VoidedPurchase{PurchaseToken: token, OrderID: "GPA.refund", VoidedTimeMillis: itoa(h.clock.Now().UnixMilli()), VoidedReason: 1})
	}
	if revoked, err := h.iap.PollVoided(context.Background()); err != nil || revoked != 1 {
		t.Fatalf("refund: %d %v", revoked, err)
	}
	if h.entitlement(s.userID).Balance.Packs != 0 || h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE transaction_id=?`, sha256Hex(unknown)) != 0 {
		t.Fatal("refund left credits or copied an unrelated receipt into sandbox")
	}
	if revoked, err := h.iap.PollVoided(context.Background()); err != nil || revoked != 0 {
		t.Fatal("refund was applied twice")
	}
}

func TestGoogleSandboxSubscriptionRefundFindsEarlierAndRenewalOrdersWithoutCopyingProductionTombstones(t *testing.T) {
	h := googleSandboxHarness(t)
	s := h.signIn("sandbox-subscription-refund@example.com")
	token, unknownProduction := playToken(450), playToken(451)
	first, renewal := "GPA.3333-0450", "GPA.3333-0450..0"
	h.playSubscription(s.userID, googleBase, token, h.clock.Now().Add(5*time.Minute), func(p *googleplay.Subscription) {
		p.TestPurchase = &googleplay.TestPurchase{}
		p.LatestOrderID = first
	})
	mustStatus(t, h.postGoogle(s.access, googleBase, token), http.StatusOK, "")
	h.renewOn(token, renewal, h.clock.Now().Add(10*time.Minute))
	h.clock.Advance(30 * time.Second)
	h.iap.Tick(context.Background())
	for i := 0; i < 7; i++ {
		mustStatus(t, h.generate(s.access, "Сәлем"), http.StatusOK, "")
	}
	h.addGrant(s.userID, domain.GrantPack, 30, h.clock.Now(), nil, h.clock.Now())
	if left := h.creditsLeft(s.access); left != 2023 {
		t.Fatalf("renewal was not granted once or consumption changed: %v", left)
	}

	// Renewals keep the token but have another transaction id. The sandbox
	// filter must find their original Test row, even after that row is refunded.
	hash, renewalTransaction := sha256Hex(token), sha256Hex(token)+":"+renewal
	if h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE transaction_id IN (?,?) AND environment='Test' AND status='granted'`, hash, renewalTransaction) != 2 {
		t.Fatal("initial and renewal purchases were not recorded separately")
	}
	h.play.Void(googleplay.VoidedPurchase{PurchaseToken: unknownProduction, OrderID: "GPA.unrelated-real-order", VoidedTimeMillis: itoa(h.clock.Now().UnixMilli()), VoidedReason: 1})
	h.play.Void(googleplay.VoidedPurchase{PurchaseToken: token, OrderID: first, VoidedTimeMillis: itoa(h.clock.Now().UnixMilli()), VoidedReason: 1})
	if revoked, err := h.iap.PollVoided(context.Background()); err != nil || revoked != 0 {
		t.Fatalf("earlier-order refund revoked current access: %d %v", revoked, err)
	}
	if ent := h.entitlement(s.userID); ent.Plan.Code != "base_monthly" || ent.Balance.Packs != 1030 ||
		h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE transaction_id=? AND environment='Test' AND status='refunded'`, hash) != 1 ||
		h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE transaction_id=? AND status='granted'`, renewalTransaction) != 1 {
		t.Fatal("earlier-order refund missed its remainder or removed the renewal")
	}
	if h.scalar(`SELECT COUNT(*) FROM credit_ledger l JOIN credit_grants g ON g.id=l.grant_id JOIN iap_purchases p ON p.id=g.iap_purchase_id WHERE p.transaction_id=? AND l.entry='revoke' AND l.delta=-993`, hash) != 1 ||
		h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE transaction_id=?`, sha256Hex(unknownProduction)) != 0 {
		t.Fatal("earlier refund was not applied once or copied an unrelated production receipt")
	}

	h.play.Void(googleplay.VoidedPurchase{PurchaseToken: token, OrderID: renewal, VoidedTimeMillis: itoa(h.clock.Now().UnixMilli()), VoidedReason: 7})
	if revoked, err := h.iap.PollVoided(context.Background()); err != nil || revoked != 1 {
		t.Fatalf("latest renewal refund was filtered out: %d %v", revoked, err)
	}
	if ent := h.entitlement(s.userID); !ent.Plan.IsFree || ent.Balance.Packs != 30 ||
		h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE transaction_id=? AND environment='Test' AND status='refunded'`, renewalTransaction) != 1 ||
		h.scalar(`SELECT COUNT(*) FROM google_subscriptions WHERE token_hash=? AND revoked_at IS NOT NULL AND revoked_order_id=?`, hash, renewal) != 1 {
		t.Fatal("latest renewal refund failed to revoke its own bundle and access")
	}
	if revoked, err := h.iap.PollVoided(context.Background()); err != nil || revoked != 0 {
		t.Fatal("repeated subscription refunds were applied twice")
	}
	h.iap.Tick(context.Background())
	if ent := h.entitlement(s.userID); !ent.Plan.IsFree || ent.Balance.Packs != 30 ||
		h.scalar(`SELECT COUNT(*) FROM iap_purchases`) != 2 || h.scalar(`SELECT COUNT(*) FROM google_subscriptions`) != 1 ||
		h.scalar(`SELECT SUM(delta) FROM credit_ledger WHERE user_id=?`, s.userID) != 30 {
		t.Fatal("refund replay/worker restored credits or imported an unrelated production tombstone")
	}
}

func TestGoogleSandboxPrefixedAPIRejectsProductionSessionAndKeepsPrimaryBalance(t *testing.T) {
	primary := newHarness(t, withIAP(t), withEnv("GOOGLE_PLAY_ACCEPT_TEST", "false"), withEnv("JWT_ACCESS_SECRET", strings.Repeat("p", 40)))
	primary.openIAP()
	sandbox := googleSandboxHarness(t)
	p, s := primary.signIn("same@example.com"), sandbox.signIn("same@example.com")
	if p.userID == s.userID || primary.iap.AccountToken(p.userID) == sandbox.iap.AccountToken(s.userID) {
		t.Fatal("accounts or store bindings reused")
	}
	token := playToken(440)
	sandbox.playPurchase(s.userID, packGoogle, token, func(p *googleplay.ProductPurchase) { p.PurchaseType = new(int) })
	mux := http.NewServeMux()
	mux.Handle("/sandbox/", http.StripPrefix("/sandbox", sandbox.server.Config.Handler))
	mux.Handle("/", primary.server.Config.Handler)
	server := httptest.NewServer(mux)
	defer server.Close()
	base := sandbox.server.URL
	sandbox.server.URL = server.URL + "/sandbox"
	defer func() { sandbox.server.URL = base }()
	if res := sandbox.postGoogle(p.access, packGoogle, token); res.status != http.StatusUnauthorized {
		t.Fatalf("production JWT accepted: %d %s", res.status, res.raw)
	}
	mustStatus(t, sandbox.postGoogle(s.access, packGoogle, token), http.StatusOK, "")
	if primary.scalar(`SELECT COUNT(*) FROM iap_purchases`) != 0 || primary.entitlement(p.userID).Balance.Packs != 0 || sandbox.entitlement(s.userID).Balance.Packs != 50 {
		t.Fatal("sandbox receipt changed primary purchases or balance")
	}
}
