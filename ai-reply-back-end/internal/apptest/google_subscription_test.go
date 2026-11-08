package apptest

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/iap/googleplay"
)

const (
	googleBase = "kz.aireply.base.monthly"
	googlePro  = "kz.aireply.pro.monthly"
)

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

func (h *harness) enableGoogleSubscriptions() {
	h.t.Helper()
	h.exec(`UPDATE iap_products SET google_product_id=?,is_active=1 WHERE code='base_monthly'`, googleBase)
	h.exec(`UPDATE iap_products SET google_product_id=?,is_active=1 WHERE code='pro_monthly'`, googlePro)
	mustStatus(h.t, h.adminCall(h.signInAdmin(), http.MethodPost, "/settings/iap", map[string]any{"enabled": true}), http.StatusOK, "")
}

// playSubscription — what Google knows about a purchase made for this account.
func (h *harness) playSubscription(userID, productID, token string, expires time.Time, change func(*googleplay.Subscription)) {
	h.t.Helper()
	p := googleplay.Subscription{StartTime: h.clock.Now().Format(time.RFC3339), SubscriptionState: domain.GoogleSubActive,
		LatestOrderID: "GPA.3333-" + token[13:17], AcknowledgementState: "ACKNOWLEDGEMENT_STATE_PENDING", RegionCode: "KZ",
		ExternalAccountIdentifiers: &googleplay.ExternalAccountIdentifiers{ObfuscatedExternalAccountID: h.iap.AccountToken(userID)},
		LineItems: []googleplay.SubscriptionLineItem{{ProductID: productID, ExpiryTime: expires.Format(time.RFC3339),
			AutoRenewingPlan: &googleplay.AutoRenewingPlan{AutoRenewEnabled: true}}}}
	if change != nil {
		change(&p)
	}
	h.play.PutSubscription(token, p)
}

// renewOn — Google took the next payment: a new order, a later end, the same token.
func (h *harness) renewOn(token, order string, expires time.Time) {
	h.t.Helper()
	h.play.UpdateSubscription(token, func(p *googleplay.Subscription) {
		p.LatestOrderID = order
		p.LineItems[0].ExpiryTime = expires.Format(time.RFC3339)
	})
}

func TestGoogleSubscriptionIsGrantedThenAcknowledged(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableGoogleSubscriptions()
	s := h.signIn("gsub@example.com")
	token := playToken(100)
	expires := h.clock.Now().Add(30 * 24 * time.Hour)
	h.playSubscription(s.userID, googleBase, token, expires, nil)

	res := h.postGoogle(s.access, googleBase, token)
	if res.status != http.StatusOK || res.str("status") != "granted" || res.num("usage", "monthly_limit") != 1000 ||
		res.num("usage", "remaining_today") != 1000 {
		t.Fatalf("post: %d %s", res.status, res.raw)
	}
	if !h.play.Subscription(token).Acknowledged() || h.play.AcknowledgeCalls.Load() != 1 {
		t.Fatal("the server did not acknowledge")
	}
	hash := sha256Hex(token)
	if h.scalar(`SELECT COUNT(*) FROM google_subscriptions WHERE token_hash=? AND user_id=? AND state=? AND expires_at=? AND auto_renew=1
		AND acknowledged_at IS NOT NULL AND linked_token_hash='' AND superseded_by='' AND environment='Production'`, hash, s.userID,
		domain.GoogleSubActive, expires.UnixMilli()) != 1 {
		t.Fatal("subscription row")
	}
	if sealed := h.text(`SELECT purchase_token_sealed FROM google_subscriptions WHERE token_hash=?`, hash); !strings.HasPrefix(sealed, "v1:") || strings.Contains(sealed, token) {
		t.Fatalf("sealed token: %q", sealed)
	}
	if h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE provider='google_play' AND transaction_id=? AND original_transaction_id=? AND user_id=?
		AND status='granted' AND units=0 AND product_code='base_monthly' AND store_product_id=? AND order_id=? AND purchase_token_sealed IS NULL`,
		hash, hash, s.userID, googleBase, "GPA.3333-"+token[13:17]) != 1 {
		t.Fatal("purchase record")
	}
	me := h.do(http.MethodGet, "/api/v1/me", nil, h.auth(s.access))
	if me.str("subscription", "source") != domain.ProviderGooglePlay || me.str("subscription", "plan", "code") != "base_monthly" ||
		me.str("subscription", "expires_at") != expires.Format(time.RFC3339) || me.body["subscription"].(map[string]any)["auto_renews"] != true {
		t.Fatalf("me: %s", me.raw)
	}
	gen := h.generate(s.access, "Сәлем")
	if gen.str("usage", "bucket") != domain.BucketDaily || gen.num("usage", "used_month") != 1 {
		t.Fatalf("generation: %s", gen.raw)
	}

	again := h.postGoogle(s.access, googleBase, token)
	if again.status != http.StatusOK || again.str("status") != "already_granted" || h.play.AcknowledgeCalls.Load() != 1 ||
		h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE user_id=?`, s.userID) != 1 || h.scalar(`SELECT COUNT(*) FROM google_subscriptions`) != 1 {
		t.Fatalf("re-post: %d %s", again.status, again.raw)
	}
	if h.dbContains(token) || strings.Contains(h.logs.String(), token) {
		t.Fatal("the raw purchase token was stored or logged")
	}
}

func TestGoogleSubscriptionsThatDoNotVerifyGrantNothing(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableGoogleSubscriptions()
	s := h.signIn("gsub-refused@example.com")
	other := h.signIn("gsub-refused-other@example.com")
	expires := h.clock.Now().Add(30 * 24 * time.Hour)
	put := func(seed int, product string, change func(*googleplay.Subscription)) string {
		token := playToken(seed)
		h.playSubscription(s.userID, product, token, expires, change)
		return token
	}
	for _, tc := range []struct {
		name, product, token, reason string
	}{
		{"another account", googleBase, put(110, googleBase, func(p *googleplay.Subscription) {
			p.ExternalAccountIdentifiers.ObfuscatedExternalAccountID = h.iap.AccountToken(other.userID)
		}), "account_mismatch"},
		{"no account id", googleBase, put(111, googleBase, func(p *googleplay.Subscription) { p.ExternalAccountIdentifiers = nil }), "account_mismatch"},
		{"another product's token", googlePro, put(112, googleBase, nil), "product_unknown"},
		{"unknown token", googleBase, playToken(113), "not_found"},
		{"unknown product", "kz.aireply.unknown", put(114, "kz.aireply.unknown", nil), "product_unknown"},
		{"malformed token", googleBase, "bad token!", "malformed"},
		{"no expiry", googleBase, put(115, googleBase, func(p *googleplay.Subscription) { p.LineItems[0].ExpiryTime = "" }), "malformed"},
	} {
		res := h.postGoogle(s.access, tc.product, tc.token)
		if res.status != http.StatusBadRequest || res.errorCode() != "PURCHASE_INVALID" || res.str("error", "details", "reason") != tc.reason {
			t.Errorf("%s: %d %s", tc.name, res.status, res.raw)
		}
	}
	if h.scalar(`SELECT COUNT(*) FROM iap_purchases`)+h.scalar(`SELECT COUNT(*) FROM google_subscriptions`) != 0 || h.play.AcknowledgeCalls.Load() != 0 {
		t.Fatal("a refused subscription left a row or was acknowledged")
	}
	if got := h.entitlement(s.userID); !got.Plan.IsFree {
		t.Fatalf("entitlement: %+v", got)
	}

	// Google is down: 503, nothing written, the app posts again later.
	token := put(116, googleBase, nil)
	h.play.FailNext(1)
	mustStatus(t, h.postGoogle(s.access, googleBase, token), http.StatusServiceUnavailable, "STORE_UNAVAILABLE")
	if h.scalar(`SELECT COUNT(*) FROM google_subscriptions`) != 0 {
		t.Fatal("an unverified subscription was written")
	}
	mustStatus(t, h.postGoogle(s.access, googleBase, token), http.StatusOK, "")

	// Bound to its account: another account cannot take the token over.
	h.play.UpdateSubscription(token, func(p *googleplay.Subscription) {
		p.ExternalAccountIdentifiers.ObfuscatedExternalAccountID = h.iap.AccountToken(other.userID)
	})
	mustStatus(t, h.postGoogle(other.access, googleBase, token), http.StatusConflict, "PURCHASE_BELONGS_TO_ANOTHER_ACCOUNT")
	if got := h.entitlement(other.userID); !got.Plan.IsFree {
		t.Fatalf("entitlement moved: %+v", got)
	}
}

func TestGoogleTestSubscriptionsNeedTheSwitch(t *testing.T) {
	test := func(p *googleplay.Subscription) { p.TestPurchase = &googleplay.TestPurchase{} }
	h := newHarness(t, approvedEntitlements(), withIAP(t), withEnv("GOOGLE_PLAY_ACCEPT_TEST", "false"))
	h.enableGoogleSubscriptions()
	s := h.signIn("gsub-test-off@example.com")
	token := playToken(120)
	h.playSubscription(s.userID, googleBase, token, h.clock.Now().Add(time.Hour), test)
	res := h.postGoogle(s.access, googleBase, token)
	if res.status != http.StatusBadRequest || res.str("error", "details", "reason") != "environment" || h.scalar(`SELECT COUNT(*) FROM google_subscriptions`) != 0 {
		t.Fatalf("test purchase with the switch off: %d %s", res.status, res.raw)
	}

	on := newHarness(t, approvedEntitlements(), withIAP(t), withEnv("GOOGLE_PLAY_ACCEPT_TEST", "true"))
	on.enableGoogleSubscriptions()
	s = on.signIn("gsub-test-on@example.com")
	on.playSubscription(s.userID, googleBase, token, on.clock.Now().Add(time.Hour), test)
	mustStatus(t, on.postGoogle(s.access, googleBase, token), http.StatusOK, "")
	if on.text(`SELECT environment FROM google_subscriptions`) != domain.EnvironmentTest ||
		on.text(`SELECT environment FROM iap_purchases`) != domain.EnvironmentTest {
		t.Fatal("a license test is not marked Test")
	}
}

func TestGooglePendingSubscription(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableGoogleSubscriptions()
	s := h.signIn("gsub-pending@example.com")
	token := playToken(130)
	expires := h.clock.Now().Add(30 * 24 * time.Hour)
	h.playSubscription(s.userID, googleBase, token, expires, func(p *googleplay.Subscription) { p.SubscriptionState = domain.GoogleSubPending })
	for i := 0; i < 2; i++ {
		res := h.postGoogle(s.access, googleBase, token)
		if res.status != http.StatusAccepted || res.str("status") != "pending" || res.body["usage"] != nil {
			t.Fatalf("pending %d: %d %s", i, res.status, res.raw)
		}
	}
	if h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE transaction_id=? AND status='pending' AND user_id=?`, sha256Hex(token), s.userID) != 1 ||
		h.scalar(`SELECT COUNT(*) FROM google_subscriptions`) != 0 || h.play.AcknowledgeCalls.Load() != 0 || !h.entitlement(s.userID).Plan.IsFree {
		t.Fatal("a pending subscription granted something")
	}
	h.play.UpdateSubscription(token, func(p *googleplay.Subscription) { p.SubscriptionState = domain.GoogleSubActive })
	paid := h.postGoogle(s.access, googleBase, token)
	if paid.status != http.StatusOK || paid.str("status") != "granted" || h.play.AcknowledgeCalls.Load() != 1 ||
		h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE transaction_id=? AND status='granted'`, sha256Hex(token)) != 1 {
		t.Fatalf("paid: %s", paid.raw)
	}

	cancelled := playToken(131)
	h.playSubscription(s.userID, googleBase, cancelled, expires, func(p *googleplay.Subscription) { p.SubscriptionState = domain.GoogleSubPending })
	mustStatus(t, h.postGoogle(s.access, googleBase, cancelled), http.StatusAccepted, "")
	h.play.UpdateSubscription(cancelled, func(p *googleplay.Subscription) { p.SubscriptionState = domain.GoogleSubPendingCanceled })
	res := h.postGoogle(s.access, googleBase, cancelled)
	if res.status != http.StatusBadRequest || res.str("error", "details", "reason") != "canceled" ||
		h.text(`SELECT status FROM iap_purchases WHERE transaction_id=?`, sha256Hex(cancelled)) != "rejected" {
		t.Fatalf("cancelled: %s", res.raw)
	}
}

func TestGoogleSubscriptionStatesThatGiveNothing(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableGoogleSubscriptions()
	s := h.signIn("gsub-states@example.com")
	future := h.clock.Now().Add(30 * 24 * time.Hour)
	past := h.clock.Now().Add(-time.Hour)
	for i, tc := range []struct {
		state  string
		expiry time.Time
		reason string
	}{
		{domain.GoogleSubOnHold, past, "on_hold"},
		{domain.GoogleSubPaused, past, "paused"},
		{domain.GoogleSubExpired, past, "expired"},
		{domain.GoogleSubOnHold, future, "on_hold"},
		{domain.GoogleSubCanceled, past, "expired"},
		{domain.GoogleSubActive, past, "expired"},
	} {
		token := playToken(140 + i)
		h.playSubscription(s.userID, googleBase, token, tc.expiry, func(p *googleplay.Subscription) { p.SubscriptionState = tc.state })
		res := h.postGoogle(s.access, googleBase, token)
		if res.status != http.StatusBadRequest || res.errorCode() != "PURCHASE_INVALID" || res.str("error", "details", "reason") != tc.reason {
			t.Errorf("%s: %d %s", tc.state, res.status, res.raw)
		}
	}
	if h.scalar(`SELECT COUNT(*) FROM google_subscriptions`)+h.scalar(`SELECT COUNT(*) FROM iap_purchases`) != 0 || !h.entitlement(s.userID).Plan.IsFree ||
		h.play.AcknowledgeCalls.Load() != 0 {
		t.Fatal("a state without access granted something")
	}

	// A running subscription whose payment failed: the next post takes the access away.
	token := playToken(150)
	h.playSubscription(s.userID, googleBase, token, future, nil)
	mustStatus(t, h.postGoogle(s.access, googleBase, token), http.StatusOK, "")
	if h.entitlement(s.userID).Plan.Code != "base_monthly" {
		t.Fatal("not granted")
	}
	h.play.UpdateSubscription(token, func(p *googleplay.Subscription) { p.SubscriptionState = domain.GoogleSubOnHold })
	mustStatus(t, h.postGoogle(s.access, googleBase, token), http.StatusBadRequest, "PURCHASE_INVALID")
	if !h.entitlement(s.userID).Plan.IsFree || h.text(`SELECT state FROM google_subscriptions`) != domain.GoogleSubOnHold {
		t.Fatal("on hold keeps access")
	}
}

func TestGoogleGraceAndCanceledKeepAccessUntilTheirEnd(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableGoogleSubscriptions()

	grace := h.signIn("gsub-grace@example.com")
	token := playToken(160)
	h.playSubscription(grace.userID, googleBase, token, h.clock.Now().Add(16*24*time.Hour), func(p *googleplay.Subscription) { p.SubscriptionState = domain.GoogleSubGrace })
	if res := h.postGoogle(grace.access, googleBase, token); res.status != http.StatusOK || res.str("status") != "granted" {
		t.Fatalf("grace: %s", res.raw)
	}
	if h.entitlement(grace.userID).Plan.Code != "base_monthly" {
		t.Fatal("grace gives no access")
	}

	// Google may report grace without a future end: the access lasts until the next read.
	past := h.signIn("gsub-grace-past@example.com")
	graceToken := playToken(161)
	h.playSubscription(past.userID, googleBase, graceToken, h.clock.Now().Add(-time.Hour), func(p *googleplay.Subscription) { p.SubscriptionState = domain.GoogleSubGrace })
	mustStatus(t, h.postGoogle(past.access, googleBase, graceToken), http.StatusOK, "")
	if h.entitlement(past.userID).Plan.Code != "base_monthly" {
		t.Fatal("grace without a future end gives no access")
	}

	canceled := h.signIn("gsub-canceled@example.com")
	cancelToken := playToken(162)
	expires := h.clock.Now().Add(2 * time.Hour)
	h.playSubscription(canceled.userID, googlePro, cancelToken, expires, func(p *googleplay.Subscription) {
		p.SubscriptionState = domain.GoogleSubCanceled
		p.LineItems[0].AutoRenewingPlan.AutoRenewEnabled = false
	})
	mustStatus(t, h.postGoogle(canceled.access, googlePro, cancelToken), http.StatusOK, "")
	ent := h.entitlement(canceled.userID)
	if !ent.Unlimited || ent.Subscription == nil || ent.Subscription.AutoRenews == nil || *ent.Subscription.AutoRenews {
		t.Fatalf("canceled until expiry: %+v", ent)
	}
	h.clock.Advance(3 * time.Hour)
	if ent := h.entitlement(canceled.userID); ent.Unlimited || !ent.Plan.IsFree {
		t.Fatalf("canceled past its end: %+v", ent)
	}
}

func TestGoogleRenewalArrivesThroughTheWorker(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableGoogleSubscriptions()
	s := h.signIn("gsub-renew@example.com")
	token := playToken(170)
	start := h.clock.Now()
	end := start.Add(30 * 24 * time.Hour)
	h.playSubscription(s.userID, googleBase, token, end, nil)
	mustStatus(t, h.postGoogle(s.access, googleBase, token), http.StatusOK, "")
	reads := h.play.SubscriptionCalls.Load()

	// Far from the end nothing is read.
	h.clock.Advance(10 * 24 * time.Hour)
	h.iap.Tick(context.Background())
	if h.play.SubscriptionCalls.Load() != reads {
		t.Fatal("a subscription far from its end was read")
	}

	h.clock.Advance(19*24*time.Hour + 2*time.Hour) // 22 h before the end
	next := end.Add(30 * 24 * time.Hour)
	h.renewOn(token, "GPA.3333-0170..0", next)
	h.iap.Tick(context.Background())
	if h.play.SubscriptionCalls.Load() != reads+1 {
		t.Fatalf("reads: %d", h.play.SubscriptionCalls.Load()-reads)
	}
	if h.scalar(`SELECT COUNT(*) FROM google_subscriptions WHERE expires_at=? AND order_id='GPA.3333-0170..0' AND check_failures=0`, next.UnixMilli()) != 1 {
		t.Fatal("expiry not updated")
	}
	if h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE original_transaction_id=? AND status='granted'`, sha256Hex(token)) != 2 ||
		h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE transaction_id=? AND purchased_at=?`, sha256Hex(token)+":GPA.3333-0170..0", end.UnixMilli()) != 1 {
		t.Fatal("the renewal order was not recorded")
	}
	if h.play.AcknowledgeCalls.Load() != 1 {
		t.Fatal("a renewal must not be acknowledged again")
	}

	// Past the old end the access goes on, and nothing is read until the new one comes near.
	h.clock.Advance(2 * 24 * time.Hour)
	reads = h.play.SubscriptionCalls.Load()
	h.iap.Tick(context.Background())
	if h.play.SubscriptionCalls.Load() != reads || h.entitlement(s.userID).Plan.Code != "base_monthly" {
		t.Fatal("after the renewal")
	}
	s, _ = h.signInOn("gsub-renew@example.com", "android")
	again := h.postGoogle(s.access, googleBase, token)
	if again.str("status") != "already_granted" || h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE original_transaction_id=?`, sha256Hex(token)) != 2 {
		t.Fatalf("re-post after the renewal: %s", again.raw)
	}
}

func TestGoogleRenewalSeenFirstByTheAppAndThenLoss(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableGoogleSubscriptions()
	s := h.signIn("gsub-loss@example.com")
	token := playToken(171)
	end := h.clock.Now().Add(2 * time.Hour)
	h.playSubscription(s.userID, googlePro, token, end, nil)
	mustStatus(t, h.postGoogle(s.access, googlePro, token), http.StatusOK, "")

	// The app comes back after Google renewed: the post alone moves the expiry.
	next := end.Add(30 * 24 * time.Hour)
	h.renewOn(token, "GPA.3333-0171..0", next)
	if res := h.postGoogle(s.access, googlePro, token); res.str("status") != "granted" {
		t.Fatalf("renewal posted by the app: %s", res.raw)
	}
	h.clock.Advance(3 * time.Hour)
	if !h.entitlement(s.userID).Unlimited {
		t.Fatal("renewal lost")
	}

	// Payment fails: on hold, no access, the worker keeps looking.
	h.clock.Advance(next.Sub(h.clock.Now()) - 30*time.Minute)
	h.play.UpdateSubscription(token, func(p *googleplay.Subscription) { p.SubscriptionState = domain.GoogleSubOnHold })
	h.iap.Tick(context.Background())
	if h.entitlement(s.userID).Unlimited || h.text(`SELECT state FROM google_subscriptions`) != domain.GoogleSubOnHold ||
		h.scalar(`SELECT next_check_at IS NOT NULL FROM google_subscriptions`) != 1 {
		t.Fatal("on hold")
	}

	// The card is fixed: Google renews it, access is back.
	h.clock.Advance(7 * time.Hour)
	back := next.Add(30 * 24 * time.Hour)
	h.play.UpdateSubscription(token, func(p *googleplay.Subscription) {
		p.SubscriptionState = domain.GoogleSubActive
		p.LatestOrderID = "GPA.3333-0171..1"
		p.LineItems[0].ExpiryTime = back.Format(time.RFC3339)
	})
	h.iap.Tick(context.Background())
	if !h.entitlement(s.userID).Unlimited {
		t.Fatal("recovered from hold without access")
	}

	// It lapses for good: the row is final, the sealed token is gone, no more reads.
	h.clock.Advance(back.Sub(h.clock.Now()) - time.Hour + time.Minute)
	h.play.UpdateSubscription(token, func(p *googleplay.Subscription) { p.SubscriptionState = domain.GoogleSubExpired })
	h.iap.Tick(context.Background())
	if h.entitlement(s.userID).Unlimited || h.text(`SELECT state FROM google_subscriptions`) != domain.GoogleSubExpired ||
		h.scalar(`SELECT purchase_token_sealed IS NULL AND next_check_at IS NULL FROM google_subscriptions`) != 1 {
		t.Fatal("expired")
	}
	reads := h.play.SubscriptionCalls.Load()
	h.clock.Advance(24 * time.Hour)
	h.iap.Tick(context.Background())
	if h.play.SubscriptionCalls.Load() != reads {
		t.Fatal("a finished subscription is still read")
	}
}

func TestGoogleOutageChangesNothingAndBacksOff(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableGoogleSubscriptions()
	var tokens []string
	var users []string
	end := h.clock.Now().Add(20 * time.Hour)
	for i := 0; i < 3; i++ {
		s := h.signIn("gsub-outage" + string(rune('a'+i)) + "@example.com")
		token := playToken(180 + i)
		h.playSubscription(s.userID, googleBase, token, end, nil)
		mustStatus(t, h.postGoogle(s.access, googleBase, token), http.StatusOK, "")
		tokens, users = append(tokens, token), append(users, s.userID)
	}
	h.clock.Advance(2 * time.Hour)

	h.play.FailNext(1000)
	h.iap.Tick(context.Background())
	if failed := h.scalar(`SELECT SUM(check_failures) FROM google_subscriptions`); failed != 1 {
		t.Fatalf("a round with Google down made %d failed reads", failed)
	}
	h.iap.Tick(context.Background())
	if failed := h.scalar(`SELECT SUM(check_failures) FROM google_subscriptions`); failed != 2 {
		t.Fatalf("second round: %d", failed)
	}
	if h.scalar(`SELECT COUNT(*) FROM google_subscriptions WHERE expires_at=? AND state=?`, end.UnixMilli(), domain.GoogleSubActive) != 3 {
		t.Fatal("an outage changed a subscription")
	}
	for _, user := range users {
		if h.entitlement(user).Plan.Code != "base_monthly" {
			t.Fatal("an outage took access away")
		}
	}
	// A failed row waits longer each time; a success resets it.
	h.iap.Tick(context.Background())
	if h.scalar(`SELECT COUNT(*) FROM google_subscriptions WHERE check_failures=1`) != 3 ||
		h.scalar(`SELECT next_check_at FROM google_subscriptions ORDER BY next_check_at LIMIT 1`) != int(h.clock.Now().Add(30*time.Minute).UnixMilli()) {
		t.Fatal("backoff")
	}
	h.play.FailNext(0)
	h.clock.Advance(31 * time.Minute)
	h.renewOn(tokens[0], "GPA.renewed", end.Add(30*24*time.Hour))
	h.iap.Tick(context.Background())
	if h.scalar(`SELECT SUM(check_failures) FROM google_subscriptions`) != 0 {
		t.Fatal("backoff was not reset")
	}
	if h.scalar(`SELECT COUNT(*) FROM google_subscriptions WHERE order_id='GPA.renewed'`) != 1 {
		t.Fatal("renewal after the outage")
	}
}

func TestGoogleAcknowledgementIsRetriedByTheWorker(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableGoogleSubscriptions()
	s := h.signIn("gsub-ack@example.com")
	token := playToken(190)
	h.playSubscription(s.userID, googleBase, token, h.clock.Now().Add(30*24*time.Hour), nil)
	h.play.FailAcknowledges(1)

	res := h.postGoogle(s.access, googleBase, token)
	if res.status != http.StatusOK || res.str("status") != "granted" || h.entitlement(s.userID).Plan.Code != "base_monthly" {
		t.Fatalf("a failed acknowledgement must not lose the purchase: %s", res.raw)
	}
	if h.scalar(`SELECT acknowledged_at IS NULL FROM google_subscriptions`) != 1 {
		t.Fatal("recorded as acknowledged")
	}
	h.iap.Tick(context.Background()) // too fresh: the request itself just tried
	if h.play.AcknowledgeCalls.Load() != 1 {
		t.Fatalf("acknowledge calls %d", h.play.AcknowledgeCalls.Load())
	}
	h.clock.Advance(2 * time.Minute)
	h.iap.Tick(context.Background())
	if !h.play.Subscription(token).Acknowledged() || h.scalar(`SELECT acknowledged_at IS NOT NULL FROM google_subscriptions`) != 1 {
		t.Fatal("the worker did not acknowledge")
	}
	h.clock.Advance(2 * time.Minute)
	h.iap.Tick(context.Background())
	if h.play.AcknowledgeCalls.Load() != 2 {
		t.Fatalf("acknowledged again: %d", h.play.AcknowledgeCalls.Load())
	}

	// A permanent refusal is read back before anything is given up.
	refused := h.signIn("gsub-ack-refused@example.com")
	refusedToken := playToken(191)
	h.playSubscription(refused.userID, googleBase, refusedToken, h.clock.Now().Add(30*24*time.Hour), nil)
	h.play.RefuseAcknowledges(2)
	mustStatus(t, h.postGoogle(refused.access, googleBase, refusedToken), http.StatusOK, "")
	h.clock.Advance(2 * time.Minute)
	h.iap.Tick(context.Background())
	if h.scalar(`SELECT acknowledged_at IS NULL AND ack_abandoned_at IS NULL FROM google_subscriptions WHERE token_hash=?`, sha256Hex(refusedToken)) != 1 {
		t.Fatal("a refusal of a live subscription stopped the retry")
	}
	h.clock.Advance(2 * time.Minute)
	h.iap.Tick(context.Background())
	if !h.play.Subscription(refusedToken).Acknowledged() {
		t.Fatal("the retry after a refusal")
	}

	// Past Google's window it stops, and the subscription itself keeps being read.
	late := h.signIn("gsub-ack-late@example.com")
	lateToken := playToken(192)
	h.playSubscription(late.userID, googleBase, lateToken, h.clock.Now().Add(30*24*time.Hour), nil)
	h.play.FailAcknowledges(1000)
	mustStatus(t, h.postGoogle(late.access, googleBase, lateToken), http.StatusOK, "")
	calls := h.play.AcknowledgeCalls.Load()
	h.clock.Advance(4*24*time.Hour + time.Minute)
	h.iap.Tick(context.Background())
	if h.play.AcknowledgeCalls.Load() != calls || h.scalar(`SELECT ack_abandoned_at IS NOT NULL AND purchase_token_sealed IS NOT NULL FROM google_subscriptions WHERE token_hash=?`, sha256Hex(lateToken)) != 1 {
		t.Fatal("give up")
	}
}

func TestGoogleUpgradeSupersedesTheOldToken(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableGoogleSubscriptions()
	s := h.signIn("gsub-upgrade@example.com")
	oldToken, newToken := playToken(200), playToken(201)
	end := h.clock.Now().Add(20 * time.Hour)
	h.playSubscription(s.userID, googleBase, oldToken, end, nil)
	mustStatus(t, h.postGoogle(s.access, googleBase, oldToken), http.StatusOK, "")

	proEnd := h.clock.Now().Add(30 * 24 * time.Hour)
	h.playSubscription(s.userID, googlePro, newToken, proEnd, func(p *googleplay.Subscription) { p.LinkedPurchaseToken = oldToken })
	mustStatus(t, h.postGoogle(s.access, googlePro, newToken), http.StatusOK, "")
	ent := h.entitlement(s.userID)
	if !ent.Unlimited || ent.Plan.Code != "pro_monthly" || ent.Subscription.ID != sha256Hex(newToken) {
		t.Fatalf("upgrade: %+v", ent)
	}
	if h.scalar(`SELECT COUNT(*) FROM google_subscriptions WHERE token_hash=? AND superseded_by=? AND purchase_token_sealed IS NULL AND next_check_at IS NULL`,
		sha256Hex(oldToken), sha256Hex(newToken)) != 1 || h.play.AcknowledgeCalls.Load() != 2 {
		t.Fatal("old token not superseded")
	}
	// The old token cannot come back, not even while Google still calls it active.
	if res := h.postGoogle(s.access, googleBase, oldToken); res.status != http.StatusOK || res.str("status") != "already_granted" {
		t.Fatalf("re-post of the old token: %s", res.raw)
	}
	if h.entitlement(s.userID).Plan.Code != "pro_monthly" || h.play.AcknowledgeCalls.Load() != 2 {
		t.Fatal("the old token changed something")
	}
	reads := h.play.SubscriptionCalls.Load()
	h.clock.Advance(2 * time.Hour)
	h.iap.Tick(context.Background())
	if h.play.SubscriptionCalls.Load() != reads {
		t.Fatal("the worker reads a replaced token")
	}

	// The replaced plan is no fallback when the new one ends.
	h.clock.Advance(40 * 24 * time.Hour)
	if !h.entitlement(s.userID).Plan.IsFree {
		t.Fatal("the replaced token gave access")
	}

	// New token first, old one posted afterwards: it never grants.
	other := h.signIn("gsub-upgrade-order@example.com")
	first, second := playToken(202), playToken(203)
	h.playSubscription(other.userID, googlePro, second, h.clock.Now().Add(30*24*time.Hour), func(p *googleplay.Subscription) { p.LinkedPurchaseToken = first })
	mustStatus(t, h.postGoogle(other.access, googlePro, second), http.StatusOK, "")
	h.playSubscription(other.userID, googleBase, first, h.clock.Now().Add(20*24*time.Hour), nil)
	calls := h.play.AcknowledgeCalls.Load()
	if res := h.postGoogle(other.access, googleBase, first); res.str("status") != "already_granted" {
		t.Fatalf("old token after the new one: %s", res.raw)
	}
	if h.scalar(`SELECT COUNT(*) FROM google_subscriptions WHERE token_hash=?`, sha256Hex(first)) != 0 || h.play.AcknowledgeCalls.Load() != calls ||
		h.entitlement(other.userID).Plan.Code != "pro_monthly" {
		t.Fatal("a replaced token was granted")
	}

	// Across accounts the replaced token stops too.
	a, b := h.signIn("gsub-upgrade-a@example.com"), h.signIn("gsub-upgrade-b@example.com")
	aToken, bToken := playToken(204), playToken(205)
	h.playSubscription(a.userID, googleBase, aToken, h.clock.Now().Add(20*24*time.Hour), nil)
	mustStatus(t, h.postGoogle(a.access, googleBase, aToken), http.StatusOK, "")
	h.playSubscription(b.userID, googlePro, bToken, h.clock.Now().Add(30*24*time.Hour), func(p *googleplay.Subscription) { p.LinkedPurchaseToken = aToken })
	mustStatus(t, h.postGoogle(b.access, googlePro, bToken), http.StatusOK, "")
	if !h.entitlement(a.userID).Plan.IsFree || h.entitlement(b.userID).Plan.Code != "pro_monthly" {
		t.Fatal("one payment gave two accounts access")
	}
}

func TestGoogleRefundRevokesTheRunningPeriod(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableGoogleSubscriptions()
	s := h.signIn("gsub-refund@example.com")
	token := playToken(210)
	end := h.clock.Now().Add(30 * 24 * time.Hour)
	h.playSubscription(s.userID, googleBase, token, end, nil)
	mustStatus(t, h.postGoogle(s.access, googleBase, token), http.StatusOK, "")
	h.addGrant(s.userID, domain.GrantPack, 30, h.clock.Now(), nil, h.clock.Now())

	order := "GPA.3333-" + token[13:17]
	voidedAt := h.clock.Now().UnixMilli()
	h.play.Void(googleplay.VoidedPurchase{PurchaseToken: token, OrderID: order, VoidedTimeMillis: itoa(voidedAt), VoidedReason: 1})
	revoked, err := h.iap.PollVoided(context.Background())
	if err != nil || revoked != 1 {
		t.Fatalf("poll: %d %v", revoked, err)
	}
	if ent := h.entitlement(s.userID); !ent.Plan.IsFree || ent.Balance.Packs != 30 {
		t.Fatalf("after the refund: %+v", ent)
	}
	if h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE transaction_id=? AND status='refunded' AND revoke_reason='voided:1' AND revoked_at=?`, sha256Hex(token), voidedAt) != 1 ||
		h.scalar(`SELECT COUNT(*) FROM google_subscriptions WHERE revoked_at=? AND revoke_reason='voided:1' AND revoked_order_id=?`, voidedAt, order) != 1 {
		t.Fatal("not recorded")
	}
	if again, err := h.iap.PollVoided(context.Background()); err != nil || again != 0 {
		t.Fatalf("second poll: %d %v", again, err)
	}

	// Google still calls it active: a re-post cannot bring the refunded order back.
	stale := h.postGoogle(s.access, googleBase, token)
	if stale.status != http.StatusBadRequest || stale.str("error", "details", "reason") != "revoked" || !h.entitlement(s.userID).Plan.IsFree {
		t.Fatalf("re-post after the refund: %s", stale.raw)
	}
	h.iap.Tick(context.Background())
	if !h.entitlement(s.userID).Plan.IsFree {
		t.Fatal("the worker brought a refunded order back")
	}

	// The next renewal is a new paid order: access returns.
	next := end.Add(30 * 24 * time.Hour)
	h.renewOn(token, order+"..0", next)
	h.clock.Advance(30*24*time.Hour - 2*time.Hour)
	h.iap.Tick(context.Background())
	if h.entitlement(s.userID).Plan.Code != "base_monthly" ||
		h.scalar(`SELECT revoked_at IS NULL AND revoked_order_id='' FROM google_subscriptions`) != 1 {
		t.Fatal("a later paid order must restore the subscription")
	}
}

func TestGoogleRefundOfAnEarlierOrderLeavesTheCurrentPeriod(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableGoogleSubscriptions()
	s := h.signIn("gsub-old-refund@example.com")
	token := playToken(211)
	end := h.clock.Now().Add(2 * time.Hour)
	h.playSubscription(s.userID, googleBase, token, end, nil)
	mustStatus(t, h.postGoogle(s.access, googleBase, token), http.StatusOK, "")
	first := "GPA.3333-" + token[13:17]
	h.renewOn(token, first+"..0", end.Add(30*24*time.Hour))
	mustStatus(t, h.postGoogle(s.access, googleBase, token), http.StatusOK, "")

	h.play.Void(googleplay.VoidedPurchase{PurchaseToken: token, OrderID: first, VoidedTimeMillis: itoa(h.clock.Now().UnixMilli()), VoidedReason: 1})
	revoked, err := h.iap.PollVoided(context.Background())
	if err != nil || revoked != 0 {
		t.Fatalf("poll: %d %v", revoked, err)
	}
	if h.entitlement(s.userID).Plan.Code != "base_monthly" || h.scalar(`SELECT revoked_at IS NULL FROM google_subscriptions`) != 1 {
		t.Fatal("a refund of a finished period took the paid one away")
	}
	if h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE transaction_id=? AND status='refunded'`, sha256Hex(token)) != 1 ||
		h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE transaction_id=? AND status='granted'`, sha256Hex(token)+":"+first+"..0") != 1 {
		t.Fatal("orders")
	}
}

// Google lists a voided renewal before the worker has read that renewal.
func TestGoogleRefundOfAnUnreadRenewalIsNotGranted(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableGoogleSubscriptions()
	s := h.signIn("gsub-unread-refund@example.com")
	token := playToken(212)
	end := h.clock.Now().Add(23 * time.Hour)
	h.playSubscription(s.userID, googleBase, token, end, nil)
	mustStatus(t, h.postGoogle(s.access, googleBase, token), http.StatusOK, "")
	order := "GPA.3333-" + token[13:17] + "..0"
	h.renewOn(token, order, end.Add(30*24*time.Hour))
	h.play.Void(googleplay.VoidedPurchase{PurchaseToken: token, OrderID: order, VoidedTimeMillis: itoa(h.clock.Now().UnixMilli()), VoidedReason: 1})
	if revoked, err := h.iap.PollVoided(context.Background()); err != nil || revoked != 0 {
		t.Fatalf("poll: %d %v", revoked, err)
	}
	h.clock.Advance(2 * time.Hour)
	h.iap.Tick(context.Background())
	if !h.entitlement(s.userID).Plan.IsFree || h.scalar(`SELECT revoked_order_id=? FROM google_subscriptions`, order) != 1 {
		t.Fatal("a refunded renewal was granted")
	}
}

func TestGoogleRefundBeforeThePostIsNeverGranted(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableGoogleSubscriptions()
	s := h.signIn("gsub-early-refund@example.com")
	token := playToken(213)
	end := h.clock.Now().Add(30 * 24 * time.Hour)
	h.playSubscription(s.userID, googleBase, token, end, nil)
	order := "GPA.3333-" + token[13:17]
	h.play.Void(googleplay.VoidedPurchase{PurchaseToken: token, OrderID: order, VoidedTimeMillis: itoa(h.clock.Now().UnixMilli()), VoidedReason: 7})
	if _, err := h.iap.PollVoided(context.Background()); err != nil {
		t.Fatal(err)
	}
	late := h.postGoogle(s.access, googleBase, token)
	if late.status != http.StatusBadRequest || late.str("error", "details", "reason") != "revoked" || !h.entitlement(s.userID).Plan.IsFree {
		t.Fatalf("a voided token was granted: %s", late.raw)
	}
	// A later paid order on the same token is a real purchase.
	h.renewOn(token, order+"..0", end.Add(30*24*time.Hour))
	if res := h.postGoogle(s.access, googleBase, token); res.str("status") != "granted" || h.entitlement(s.userID).Plan.Code != "base_monthly" {
		t.Fatalf("later order: %s", res.raw)
	}
}

func TestGoogleSubscriptionsStayOffUntilConfigured(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableApprovedProducts()
	s := h.signIn("gsub-off@example.com")
	mustStatus(t, h.products(s.access, "android"), http.StatusForbidden, "IAP_UNAVAILABLE")
	if h.feature("iap_android") {
		t.Fatal("android open without a Google id")
	}
	if ios := h.products(s.access, "ios"); len(ios.body["products"].([]any)) != 5 {
		t.Fatalf("iOS catalogue changed: %s", ios.raw)
	}
	token := playToken(220)
	h.playSubscription(s.userID, googleBase, token, h.clock.Now().Add(time.Hour), nil)
	res := h.postGoogle(s.access, googleBase, token)
	if res.status != http.StatusBadRequest || res.str("error", "details", "reason") != "product_unknown" || h.play.SubscriptionCalls.Load() != 0 {
		t.Fatalf("no mapping: %s", res.raw)
	}
	// A product switched off for Android is mapped but not offered.
	h.exec(`UPDATE iap_products SET google_product_id=?,off_android=1 WHERE code='base_monthly'`, googleBase)
	mustStatus(t, h.products(s.access, "android"), http.StatusForbidden, "IAP_UNAVAILABLE")
	if !h.feature("iap_ios") {
		t.Fatal("the Android switch changed iOS")
	}
	// Paid is paid: a purchase made before the switch went off is honoured.
	mustStatus(t, h.postGoogle(s.access, googleBase, token), http.StatusOK, "")
}

func TestAndroidProductsListSubscriptionsLikeIOS(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableGoogleSubscriptions()
	s := h.signIn("gsub-products@example.com")
	res := h.products(s.access, "android")
	mustStatus(t, res, http.StatusOK, "")
	items := res.body["products"].([]any)
	if len(items) != 2 {
		t.Fatalf("products: %s", res.raw)
	}
	base, pro := items[0].(map[string]any), items[1].(map[string]any)
	if base["code"] != "base_monthly" || base["kind"] != "subscription" || base["store_product_id"] != googleBase || base["plan_id"] != h.planID("base_monthly") ||
		base["billing_period"] != "month" || base["entitlement_kind"] != "monthly" || base["daily_limit"] != float64(0) || base["monthly_limit"] != float64(1000) ||
		base["price"] != float64(99900) || base["currency"] != "KZT" {
		t.Fatalf("base: %v", base)
	}
	if pro["code"] != "pro_monthly" || pro["store_product_id"] != googlePro || pro["entitlement_kind"] != "unlimited" || pro["daily_limit"] != float64(0) ||
		pro["monthly_limit"] != float64(0) || pro["price"] != float64(399900) {
		t.Fatalf("pro: %v", pro)
	}
	if _, listed := base["credits_never_expire"]; listed {
		t.Fatalf("base: %v", base)
	}
	if res.str("account_token") != h.iap.AccountToken(s.userID) || res.body["recommendation"] != nil {
		t.Fatalf("android payload: %s", res.raw)
	}
}

func TestGoogleJoinsAppleAndBankWithoutChangingThem(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableApprovedProducts()
	h.enableGoogleSubscriptions()
	s := h.signIn("gsub-mixed@example.com")
	now := h.clock.Now()
	mustStatus(t, h.postApple(s.access, h.subscriptionTx(s.userID, "mixed-apple", "mixed-chain", "base_monthly", now, now.Add(30*24*time.Hour), nil)), http.StatusOK, "")
	ent := h.entitlement(s.userID)
	if ent.Plan.Code != "base_monthly" || ent.Subscription.Source != domain.ProviderAppStore {
		t.Fatalf("Apple alone: %+v", ent)
	}
	token := playToken(230)
	h.playSubscription(s.userID, googlePro, token, now.Add(30*24*time.Hour), nil)
	mustStatus(t, h.postGoogle(s.access, googlePro, token), http.StatusOK, "")
	ent = h.entitlement(s.userID)
	if !ent.Unlimited || ent.Subscription.Source != domain.ProviderGooglePlay {
		t.Fatalf("Google Pro over Apple Base: %+v", ent)
	}
	if h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE provider='app_store' AND user_id=?`, s.userID) != 1 ||
		h.scalar(`SELECT COUNT(*) FROM apple_subscriptions WHERE user_id=?`, s.userID) != 1 {
		t.Fatal("Apple records changed")
	}

	// A paid bank Pro outranks a Google Base; a one-time Google pack is untouched.
	bank := h.signIn("gsub-bank@example.com")
	h.bankMandate("gsub-bank-pro", bank.userID, "pro_monthly", 399900, now.Add(-time.Hour), now.Add(29*24*time.Hour))
	baseToken := playToken(231)
	h.playSubscription(bank.userID, googleBase, baseToken, now.Add(30*24*time.Hour), nil)
	mustStatus(t, h.postGoogle(bank.access, googleBase, baseToken), http.StatusOK, "")
	ent = h.entitlement(bank.userID)
	if !ent.Unlimited || ent.Subscription.Source != "bcc" {
		t.Fatalf("bank Pro over Google Base: %+v", ent)
	}
}

// Google has not moved a just-ended period on yet: it is read again, not given up.
func TestGoogleStaleEndKeepsTheSubscriptionBeingRead(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableGoogleSubscriptions()
	s := h.signIn("gsub-stale@example.com")
	token := playToken(240)
	end := h.clock.Now().Add(time.Hour)
	h.playSubscription(s.userID, googlePro, token, end, nil)
	mustStatus(t, h.postGoogle(s.access, googlePro, token), http.StatusOK, "")

	h.clock.Advance(90 * time.Minute)
	if h.entitlement(s.userID).Unlimited {
		t.Fatal("access outlived the paid end")
	}
	h.iap.Tick(context.Background())
	if h.scalar(`SELECT purchase_token_sealed IS NOT NULL AND next_check_at IS NOT NULL FROM google_subscriptions`) != 1 ||
		h.text(`SELECT state FROM google_subscriptions`) != domain.GoogleSubActive {
		t.Fatal("a stale end stopped the reading")
	}
	s = h.refresh(s)
	refused := h.postGoogle(s.access, googlePro, token)
	if refused.status != http.StatusBadRequest || refused.str("error", "details", "reason") != "expired" ||
		h.scalar(`SELECT purchase_token_sealed IS NOT NULL FROM google_subscriptions`) != 1 {
		t.Fatalf("post with a stale end: %s", refused.raw)
	}

	h.renewOn(token, "GPA.3333-0240..0", end.Add(30*24*time.Hour))
	h.clock.Advance(7 * time.Hour)
	h.iap.Tick(context.Background())
	if !h.entitlement(s.userID).Unlimited {
		t.Fatal("the late renewal was lost")
	}
}

func TestDeletedAccountLeavesAFinancialRecordOnly(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableGoogleSubscriptions()
	s := h.signIn("gsub-gone@example.com")
	token := playToken(241)
	h.playSubscription(s.userID, googleBase, token, h.clock.Now().Add(30*24*time.Hour), nil)
	mustStatus(t, h.postGoogle(s.access, googleBase, token), http.StatusOK, "")
	mustStatus(t, h.do(http.MethodDelete, "/api/v1/me", nil, h.auth(s.access)), http.StatusOK, "")
	if h.scalar(`SELECT COUNT(*) FROM google_subscriptions`) != 0 ||
		h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE transaction_id=? AND user_id IS NULL`, sha256Hex(token)) != 1 {
		t.Fatal("account deletion")
	}
	next := h.signIn("gsub-gone-new@example.com")
	h.play.UpdateSubscription(token, func(p *googleplay.Subscription) {
		p.ExternalAccountIdentifiers.ObfuscatedExternalAccountID = h.iap.AccountToken(next.userID)
	})
	mustStatus(t, h.postGoogle(next.access, googleBase, token), http.StatusConflict, "PURCHASE_BELONGS_TO_ANOTHER_ACCOUNT")
}
