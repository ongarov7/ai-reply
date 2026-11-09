package apptest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/iap/appstore/appstoretest"
	"github.com/aireply/ai-reply-back-end/internal/iap/googleplay"
	"github.com/aireply/ai-reply-back-end/internal/repository"
)

const (
	googleBase = "kz.aireply.base.monthly"
	googlePro  = "kz.aireply.pro.monthly"
	googlePack = "kz.aireply.pack30"
)

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// googleHarness — the catalogue as migrated, with Base and Pro mapped to Play.
func googleHarness(t *testing.T, opts ...harnessOption) *harness {
	t.Helper()
	h := newHarness(t, append([]harnessOption{withEnv("TEST_APPROVED_ENTITLEMENTS", "true"), withIAP(t)}, opts...)...)
	h.enableGoogleSubscriptions()
	return h
}

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

// bankPro — a paid bank Pro period for this account.
func (h *harness) bankPro(id, userID string, start, end time.Time) {
	h.t.Helper()
	at := start.UnixMilli()
	if h.scalar(`SELECT COUNT(*) FROM billing_catalog_versions WHERE state='published'`) == 0 {
		h.exec(`INSERT INTO billing_catalog_versions(state,products_json,created_by,updated_by,published_by,created_at,updated_at,published_at) VALUES('published','[]','test','test','test',?,?,?)`, at, at, at)
	}
	h.exec(`INSERT INTO billing_orders(id,user_id,idempotency_key,catalog_version,product_code,snapshot_json,amount_minor,currency,provider,environment,bank_order,status,checkout_token_hash,request_json,created_at,updated_at)
		SELECT ?,?,?,id,'pro_monthly','{}',399000,'KZT','bcc','production',?,'succeeded','hash','{}',?,? FROM billing_catalog_versions WHERE state='published'`, id, userID, id, id, at, at)
	h.exec(`INSERT INTO bcc_subscriptions(id,user_id,environment,root_order_id,plan_id,snapshot_json,amount_minor,period_days,status,mandate_expires_at,merch_rn_id,created_at,updated_at)
		SELECT ?,?,'production',?,id,'{"unlimited":true,"price_minor":399000}',399000,30,'active',?,?,?,? FROM plans WHERE code='pro_monthly'`, id, userID, id, end.UnixMilli(), id, at, at)
	h.exec(`INSERT INTO bcc_subscription_periods(order_id,subscription_id,starts_at,expires_at) VALUES(?,?,?,?)`, id, id, at, end.UnixMilli())
}

// baseReplies — what is left on the account's Google Base bundles.
func (h *harness) baseReplies(userID string) int {
	h.t.Helper()
	return h.scalar(`SELECT COALESCE(SUM(units_remaining),0) FROM credit_grants WHERE user_id=? AND product_code='google_base_monthly' AND status='active'`, userID)
}

func TestGoogleSubscriptionIsGrantedThenAcknowledged(t *testing.T) {
	h := googleHarness(t)
	s := h.signIn("gsub@example.com")
	token := playToken(100)
	expires := h.clock.Now().Add(30 * 24 * time.Hour)
	h.playSubscription(s.userID, googleBase, token, expires, nil)

	res := h.postGoogle(s.access, googleBase, token)
	usage, _ := res.body["usage"].(map[string]any)
	if res.status != http.StatusOK || res.str("status") != "granted" || res.num("usage", "daily_limit") != 0 || res.num("usage", "monthly_limit") != 0 ||
		res.num("usage", "remaining_today") != 0 || res.num("usage", "purchased_credits") != 1000 || res.num("usage", "credits") != 1000 || usage["credits_never_expire"] != true {
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
		AND status='granted' AND units=1000 AND product_code='base_monthly' AND store_product_id=? AND order_id=? AND purchase_token_sealed IS NULL`,
		hash, hash, s.userID, googleBase, "GPA.3333-"+token[13:17]) != 1 {
		t.Fatal("purchase record")
	}
	purchase := h.text(`SELECT id FROM iap_purchases WHERE transaction_id=?`, hash)
	if h.scalar(`SELECT COUNT(*) FROM credit_grants WHERE user_id=? AND kind='pack' AND expires_at IS NULL AND status='active' AND units_total=1000 AND units_remaining=1000
		AND product_code='google_base_monthly' AND granted_by='system' AND iap_purchase_id=?`, s.userID, purchase) != 1 {
		t.Fatal("the bundle is not one permanent grant bound to the order")
	}
	if h.scalar(`SELECT SUM(delta) FROM credit_ledger WHERE user_id=?`, s.userID) != 1000 {
		t.Fatal("ledger")
	}
	me := h.do(http.MethodGet, "/api/v1/me", nil, h.auth(s.access))
	if me.str("subscription", "source") != domain.ProviderGooglePlay || me.str("subscription", "plan", "code") != "base_monthly" ||
		me.str("subscription", "expires_at") != expires.Format(time.RFC3339) || me.body["subscription"].(map[string]any)["auto_renews"] != true {
		t.Fatalf("me: %s", me.raw)
	}
	gen := h.generate(s.access, "Сәлем")
	if gen.str("usage", "bucket") != domain.GrantPack || gen.num("usage", "used_month") != 0 || gen.num("usage", "credits") != 999 {
		t.Fatalf("generation: %s", gen.raw)
	}
	if h.scalar(`SELECT COALESCE(SUM(used),0) FROM usage_monthly WHERE user_id=?`, s.userID) != 0 {
		t.Fatal("a Base reply was counted against a calendar month")
	}

	again := h.postGoogle(s.access, googleBase, token)
	if again.status != http.StatusOK || again.str("status") != "already_granted" || h.play.AcknowledgeCalls.Load() != 1 ||
		h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE user_id=?`, s.userID) != 1 || h.scalar(`SELECT COUNT(*) FROM google_subscriptions`) != 1 ||
		h.creditsLeft(s.access) != 999 {
		t.Fatalf("re-post: %d %s", again.status, again.raw)
	}
	if h.dbContains(token) || strings.Contains(h.logs.String(), token) {
		t.Fatal("the raw purchase token was stored or logged")
	}
}

func TestGoogleBaseEachPaidOrderAddsOneBundleAndReplaysAddNothing(t *testing.T) {
	h := googleHarness(t)
	s := h.signIn("gbase-orders@example.com")
	token := playToken(300)
	end := h.clock.Now().Add(2 * time.Hour)
	h.playSubscription(s.userID, googleBase, token, end, nil)
	mustStatus(t, h.postGoogle(s.access, googleBase, token), http.StatusOK, "")
	check := func(want int) {
		t.Helper()
		if got := h.baseReplies(s.userID); got != want {
			t.Fatalf("base replies: %d, want %d", got, want)
		}
		if got := h.scalar(`SELECT SUM(delta) FROM credit_ledger WHERE user_id=?`, s.userID); got != want {
			t.Fatalf("ledger: %d, want %d", got, want)
		}
	}
	check(1000)

	// Replays, restore and the worker's reads of the same order add nothing.
	for i := 0; i < 3; i++ {
		if res := h.postGoogle(s.access, googleBase, token); res.status != http.StatusOK || res.str("status") != "already_granted" {
			t.Fatalf("replay: %s", res.raw)
		}
	}
	h.iap.Tick(context.Background())
	h.iap.Tick(context.Background())
	check(1000)

	// A renewal read by the worker.
	next := end.AddDate(0, 1, 0)
	h.renewOn(token, "GPA.3333-0300..0", next)
	h.clock.Advance(61 * time.Minute)
	h.iap.Tick(context.Background())
	check(2000)
	h.clock.Advance(3 * time.Hour)
	h.iap.Tick(context.Background())
	check(2000)
	s = h.refresh(s)
	if res := h.postGoogle(s.access, googleBase, token); res.str("status") != "already_granted" {
		t.Fatalf("replay of the renewal: %s", res.raw)
	}
	check(2000)

	// A renewal the app reports first; each order has its own row and its own grant.
	h.renewOn(token, "GPA.3333-0300..1", next.AddDate(0, 1, 0))
	if res := h.postGoogle(s.access, googleBase, token); res.str("status") != "granted" {
		t.Fatalf("renewal posted by the app: %s", res.raw)
	}
	check(3000)
	if h.scalar(`SELECT COUNT(DISTINCT g.iap_purchase_id) FROM credit_grants g JOIN iap_purchases p ON p.id=g.iap_purchase_id
		WHERE g.user_id=? AND g.product_code='google_base_monthly' AND g.expires_at IS NULL AND g.kind='pack' AND p.original_transaction_id=? AND p.units=1000`, s.userID, sha256Hex(token)) != 3 {
		t.Fatal("orders and grants do not pair up")
	}

	// What was sold does not follow the catalogue, and old orders mint nothing.
	h.exec(`UPDATE plans SET monthly_message_limit=9000 WHERE code='base_monthly'`)
	if res := h.postGoogle(s.access, googleBase, token); res.str("status") != "already_granted" {
		t.Fatalf("replay after a catalogue change: %s", res.raw)
	}
	check(3000)
}

func TestGoogleBaseParallelDeliveryGrantsOnce(t *testing.T) {
	h := googleHarness(t)
	s := h.signIn("gbase-parallel@example.com")
	now := h.clock.Now()
	end := now.AddDate(0, 1, 0)
	in := repository.GoogleSubscription{TokenHash: "parallel-token-hash", UserID: s.userID, ProductID: googleBase, ProductCode: "base_monthly", PlanID: h.planID("base_monthly"),
		Environment: domain.EnvironmentProduction, OrderID: "GPA.parallel", State: domain.GoogleSubActive, StartedAt: now, ExpiresAt: end, AutoRenew: true}
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := h.store.ApplyGoogleSubscription(context.Background(), in, now)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if h.baseReplies(s.userID) != 1000 || h.scalar(`SELECT COUNT(*) FROM credit_grants WHERE user_id=? AND product_code='google_base_monthly'`, s.userID) != 1 {
		t.Fatalf("duplicate delivery: %d", h.baseReplies(s.userID))
	}
	h.provider.err = domain.ErrProviderDown
	mustStatus(t, h.generate(s.access, "Қате"), http.StatusBadGateway, "AI_PROVIDER_UNAVAILABLE")
	if left := h.creditsLeft(s.access); left != 1000 {
		t.Fatalf("a provider failure spent a reply: %v", left)
	}
}

func TestGoogleBaseBalanceSurvivesCancellationAndExpiry(t *testing.T) {
	h := googleHarness(t)
	s := h.signIn("gbase-keep@example.com")
	token := playToken(301)
	end := h.clock.Now().Add(30 * 24 * time.Hour)
	h.playSubscription(s.userID, googleBase, token, end, nil)
	mustStatus(t, h.postGoogle(s.access, googleBase, token), http.StatusOK, "")
	for i := 0; i < 7; i++ {
		mustStatus(t, h.generate(s.access, "Сәлем"), http.StatusOK, "")
	}

	// Renewal switched off: the period runs on, nothing is taken back.
	h.play.UpdateSubscription(token, func(p *googleplay.Subscription) {
		p.SubscriptionState = domain.GoogleSubCanceled
		p.LineItems[0].AutoRenewingPlan.AutoRenewEnabled = false
	})
	mustStatus(t, h.postGoogle(s.access, googleBase, token), http.StatusOK, "")
	if left := h.creditsLeft(s.access); left != 993 || h.text(`SELECT state FROM google_subscriptions`) != domain.GoogleSubCanceled {
		t.Fatalf("after the cancellation: %v", left)
	}

	// The period ends for good.
	h.clock.Advance(end.Sub(h.clock.Now()) + time.Hour)
	h.play.UpdateSubscription(token, func(p *googleplay.Subscription) { p.SubscriptionState = domain.GoogleSubExpired })
	h.iap.Tick(context.Background())
	if ent := h.entitlement(s.userID); !ent.Plan.IsFree || ent.Balance.Packs != 993 {
		t.Fatalf("after the expiry: %+v", ent)
	}

	// A year on, from another platform, they still spend.
	h.clock.Advance(400 * 24 * time.Hour)
	s, _ = h.signInOn("gbase-keep@example.com", "ios")
	usage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(s.access))
	mustStatus(t, usage, http.StatusOK, "")
	if usage.num("daily_limit") != 0 || usage.num("monthly_limit") != 0 || usage.num("purchased_credits") != 993 || usage.body["credits_never_expire"] != true {
		t.Fatalf("usage long after the end: %s", usage.raw)
	}
	mustStatus(t, h.generate(s.access, "Қалған жауап"), http.StatusOK, "")
	if left := h.creditsLeft(s.access); left != 992 {
		t.Fatalf("expired replies not spendable: %v", left)
	}
}

func TestGoogleRefundOfABaseOrderTakesOnlyItsRemainder(t *testing.T) {
	h := googleHarness(t)
	s := h.signIn("gbase-refund@example.com")
	token := playToken(302)
	end := h.clock.Now().Add(2 * time.Hour)
	h.playSubscription(s.userID, googleBase, token, end, nil)
	mustStatus(t, h.postGoogle(s.access, googleBase, token), http.StatusOK, "")
	first := "GPA.3333-" + token[13:17]
	second := first + "..0"
	h.clock.Advance(time.Minute) // the oldest bundle is spent first
	h.renewOn(token, second, end.AddDate(0, 1, 0))
	mustStatus(t, h.postGoogle(s.access, googleBase, token), http.StatusOK, "")
	for i := 0; i < 7; i++ {
		mustStatus(t, h.generate(s.access, "Сәлем"), http.StatusOK, "")
	}
	h.addGrant(s.userID, domain.GrantPack, 30, h.clock.Now(), nil, h.clock.Now())
	if left := h.creditsLeft(s.access); left != 2023 {
		t.Fatalf("before: %v", left)
	}

	// The refund of the first order takes what is left of its bundle, spent stays spent.
	h.play.Void(googleplay.VoidedPurchase{PurchaseToken: token, OrderID: first, VoidedTimeMillis: itoa(h.clock.Now().UnixMilli()), VoidedReason: 1})
	if _, err := h.iap.PollVoided(context.Background()); err != nil {
		t.Fatal(err)
	}
	if left := h.creditsLeft(s.access); left != 1030 || h.baseReplies(s.userID) != 1000 {
		t.Fatalf("after the first refund: %v", left)
	}
	if h.scalar(`SELECT units_remaining FROM credit_grants g JOIN iap_purchases p ON p.id=g.iap_purchase_id WHERE p.transaction_id=? AND g.status='revoked' AND g.revoked_by='system'`, sha256Hex(token)) != 0 ||
		h.scalar(`SELECT COUNT(*) FROM credit_ledger l JOIN credit_grants g ON g.id=l.grant_id JOIN iap_purchases p ON p.id=g.iap_purchase_id WHERE p.transaction_id=? AND l.entry='revoke' AND l.delta=-993`, sha256Hex(token)) != 1 {
		t.Fatal("the remainder was not taken in the ledger")
	}
	if h.entitlement(s.userID).Plan.Code != "base_monthly" {
		t.Fatal("the refund of a finished period took the paid one away")
	}

	// The voided list repeats itself; the result does not.
	if _, err := h.iap.PollVoided(context.Background()); err != nil {
		t.Fatal(err)
	}
	if left := h.creditsLeft(s.access); left != 1030 || h.scalar(`SELECT SUM(delta) FROM credit_ledger WHERE user_id=?`, s.userID) != 1030 {
		t.Fatalf("a repeated refund changed the balance: %v", left)
	}

	// A re-post after the refund mints nothing.
	mustStatus(t, h.postGoogle(s.access, googleBase, token), http.StatusOK, "")
	if left := h.creditsLeft(s.access); left != 1030 {
		t.Fatalf("a replay after the refund: %v", left)
	}

	// The refund of the latest order: its bundle and the access go, the pack stays.
	h.play.Void(googleplay.VoidedPurchase{PurchaseToken: token, OrderID: second, VoidedTimeMillis: itoa(h.clock.Now().UnixMilli()), VoidedReason: 7})
	if revoked, err := h.iap.PollVoided(context.Background()); err != nil || revoked != 1 {
		t.Fatalf("poll: %d %v", revoked, err)
	}
	if ent := h.entitlement(s.userID); !ent.Plan.IsFree || ent.Balance.Packs != 30 {
		t.Fatalf("after the last refund: %+v", ent)
	}
}

func TestGoogleProIsUnlimitedAndLeavesBaseRepliesAlone(t *testing.T) {
	h := googleHarness(t)
	s := h.signIn("gbase-pro@example.com")
	baseToken, proToken := playToken(303), playToken(304)
	h.playSubscription(s.userID, googleBase, baseToken, h.clock.Now().Add(30*24*time.Hour), nil)
	mustStatus(t, h.postGoogle(s.access, googleBase, baseToken), http.StatusOK, "")

	proEnd := h.clock.Now().Add(30 * 24 * time.Hour)
	h.playSubscription(s.userID, googlePro, proToken, proEnd, func(p *googleplay.Subscription) { p.LinkedPurchaseToken = baseToken })
	res := h.postGoogle(s.access, googlePro, proToken)
	if res.status != http.StatusOK || res.str("status") != "granted" || res.body["usage"].(map[string]any)["unlimited"] != true {
		t.Fatalf("upgrade: %s", res.raw)
	}
	for i := 0; i < 5; i++ {
		gen := h.generate(s.access, "Pro")
		if gen.str("usage", "bucket") != domain.BucketUnlimited || gen.num("usage", "credits") != 1000 {
			t.Fatalf("Pro generation: %s", gen.raw)
		}
	}
	if h.baseReplies(s.userID) != 1000 || h.scalar(`SELECT units FROM iap_purchases WHERE transaction_id=?`, sha256Hex(proToken)) != 0 {
		t.Fatal("Pro touched the Base replies or carried units")
	}

	// Pro ends with its paid period and leaves the balance to spend.
	h.clock.Advance(proEnd.Sub(h.clock.Now()) + time.Hour)
	h.play.UpdateSubscription(proToken, func(p *googleplay.Subscription) {
		p.SubscriptionState = domain.GoogleSubExpired
		p.LineItems[0].AutoRenewingPlan.AutoRenewEnabled = false
	})
	h.iap.Tick(context.Background())
	if ent := h.entitlement(s.userID); ent.Unlimited || !ent.Plan.IsFree || ent.Balance.Packs != 1000 {
		t.Fatalf("after Pro: %+v", ent)
	}
	s, _ = h.signInOn("gbase-pro@example.com", "android")
	if gen := h.generate(s.access, "Base"); gen.str("usage", "bucket") != domain.GrantPack || gen.num("usage", "credits") != 999 {
		t.Fatalf("spending after Pro: %s", gen.raw)
	}
}

// A plan change charges nothing: Google moves the billing date. Going back and
// forth between Base and Pro must not mint bundles; a downgrade still gets one.
func TestGoogleReplacementIsNotANewPayment(t *testing.T) {
	h := googleHarness(t)
	s := h.signIn("gbase-cycle@example.com")
	base, pro, baseAgain := playToken(305), playToken(306), playToken(307)
	h.playSubscription(s.userID, googleBase, base, h.clock.Now().Add(30*24*time.Hour), nil)
	mustStatus(t, h.postGoogle(s.access, googleBase, base), http.StatusOK, "")
	h.clock.Advance(24 * time.Hour)
	s = h.refresh(s)
	h.playSubscription(s.userID, googlePro, pro, h.clock.Now().Add(7*24*time.Hour), func(p *googleplay.Subscription) { p.LinkedPurchaseToken = base })
	mustStatus(t, h.postGoogle(s.access, googlePro, pro), http.StatusOK, "")
	h.playSubscription(s.userID, googleBase, baseAgain, h.clock.Now().Add(29*24*time.Hour), func(p *googleplay.Subscription) { p.LinkedPurchaseToken = pro })
	if res := h.postGoogle(s.access, googleBase, baseAgain); res.status != http.StatusOK || res.str("status") != "granted" {
		t.Fatalf("back to Base: %s", res.raw)
	}
	if got := h.baseReplies(s.userID); got != 1000 {
		t.Fatalf("a plan change minted replies: %d", got)
	}
	if ent := h.entitlement(s.userID); ent.Plan.Code != "base_monthly" || ent.DailyLimit != 0 || ent.MonthlyLimit != 0 {
		t.Fatalf("back on Base: %+v", ent)
	}

	// The next renewal on that token is a real payment again.
	h.renewOn(baseAgain, "GPA.3333-0307..0", h.clock.Now().AddDate(0, 2, 0))
	if res := h.postGoogle(s.access, googleBase, baseAgain); res.str("status") != "granted" {
		t.Fatalf("renewal: %s", res.raw)
	}
	if got := h.baseReplies(s.userID); got != 2000 {
		t.Fatalf("renewal after a plan change: %d", got)
	}

	// Pro to Base with no Base bundle lately: the new period has replies to spend.
	other := h.signIn("gbase-downgrade@example.com")
	oldPro, down := playToken(308), playToken(309)
	h.playSubscription(other.userID, googlePro, oldPro, h.clock.Now().Add(30*24*time.Hour), nil)
	mustStatus(t, h.postGoogle(other.access, googlePro, oldPro), http.StatusOK, "")
	h.playSubscription(other.userID, googleBase, down, h.clock.Now().Add(60*24*time.Hour), func(p *googleplay.Subscription) { p.LinkedPurchaseToken = oldPro })
	mustStatus(t, h.postGoogle(other.access, googleBase, down), http.StatusOK, "")
	if got := h.baseReplies(other.userID); got != 1000 {
		t.Fatalf("downgrade: %d", got)
	}
}

// A lapsed subscriber taps Resubscribe in the Play subscriptions center: Google
// charges at once, and only the expired subscription says whose this is.
func TestGoogleResubscribeFromThePlayCenter(t *testing.T) {
	h := googleHarness(t)
	s := h.signIn("gresub@example.com")
	other := h.signIn("gresub-other@example.com")
	old, again := playToken(320), playToken(321)
	end := h.clock.Now().Add(time.Hour)
	h.playSubscription(s.userID, googleBase, old, end, nil)
	mustStatus(t, h.postGoogle(s.access, googleBase, old), http.StatusOK, "")
	h.clock.Advance(2 * time.Hour)
	s = h.refresh(s)
	other = h.refresh(other)
	h.play.UpdateSubscription(old, func(p *googleplay.Subscription) { p.SubscriptionState = domain.GoogleSubExpired })
	h.iap.Tick(context.Background())

	resubscribed := func(p *googleplay.Subscription) {
		p.ExternalAccountIdentifiers = nil
		p.OutOfAppPurchaseContext = &googleplay.OutOfAppPurchaseContext{ExpiredPurchaseToken: old,
			ExpiredExternalAccountIdentifiers: &googleplay.ExternalAccountIdentifiers{ObfuscatedExternalAccountID: h.iap.AccountToken(s.userID)}}
	}
	h.playSubscription(s.userID, googleBase, again, h.clock.Now().Add(30*24*time.Hour), resubscribed)

	// Another account on the same phone cannot take it.
	if res := h.postGoogle(other.access, googleBase, again); res.status != http.StatusBadRequest || res.str("error", "details", "reason") != "account_mismatch" {
		t.Fatalf("another account: %s", res.raw)
	}
	if h.play.AcknowledgeCalls.Load() != 1 || h.scalar(`SELECT COUNT(*) FROM google_subscriptions WHERE token_hash=?`, sha256Hex(again)) != 0 {
		t.Fatal("a refused re-subscription left a trace")
	}

	if res := h.postGoogle(s.access, googleBase, again); res.status != http.StatusOK || res.str("status") != "granted" {
		t.Fatalf("resubscribe: %s", res.raw)
	}
	if !h.play.Subscription(again).Acknowledged() || h.baseReplies(s.userID) != 2000 || h.entitlement(s.userID).Plan.Code != "base_monthly" {
		t.Fatal("the paid re-subscription was not delivered")
	}

	// Google drops the context after the acknowledgement: the row holds the binding.
	h.play.UpdateSubscription(again, func(p *googleplay.Subscription) { p.OutOfAppPurchaseContext = nil })
	if res := h.postGoogle(s.access, googleBase, again); res.status != http.StatusOK || res.str("status") != "already_granted" {
		t.Fatalf("re-post after the acknowledgement: %s", res.raw)
	}
	if res := h.postGoogle(other.access, googleBase, again); res.status != http.StatusBadRequest || res.str("error", "details", "reason") != "account_mismatch" {
		t.Fatalf("another account later: %s", res.raw)
	}
	if h.baseReplies(s.userID) != 2000 {
		t.Fatal("re-posts added replies")
	}

	// Without the old account id, the expired token ties it to the same account.
	byToken := playToken(322)
	h.playSubscription(s.userID, googleBase, byToken, h.clock.Now().Add(30*24*time.Hour), func(p *googleplay.Subscription) {
		p.ExternalAccountIdentifiers = nil
		p.OutOfAppPurchaseContext = &googleplay.OutOfAppPurchaseContext{ExpiredPurchaseToken: old}
	})
	if res := h.postGoogle(other.access, googleBase, byToken); res.status != http.StatusBadRequest {
		t.Fatalf("expired token of another account: %s", res.raw)
	}
	mustStatus(t, h.postGoogle(s.access, googleBase, byToken), http.StatusOK, "")

	// Nothing ties it to anyone: refused.
	unknown := playToken(323)
	h.playSubscription(s.userID, googleBase, unknown, h.clock.Now().Add(30*24*time.Hour), func(p *googleplay.Subscription) {
		p.ExternalAccountIdentifiers = nil
		p.OutOfAppPurchaseContext = &googleplay.OutOfAppPurchaseContext{ExpiredPurchaseToken: playToken(324)}
	})
	if res := h.postGoogle(s.access, googleBase, unknown); res.status != http.StatusBadRequest || res.str("error", "details", "reason") != "account_mismatch" {
		t.Fatalf("no binding: %s", res.raw)
	}
	if h.dbContains(old) || strings.Contains(h.logs.String(), old) {
		t.Fatal("the expired purchase token was stored or logged")
	}
}

func TestGoogleSubscriptionsThatDoNotVerifyGrantNothing(t *testing.T) {
	h := googleHarness(t)
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
	if h.scalar(`SELECT COUNT(*) FROM iap_purchases`)+h.scalar(`SELECT COUNT(*) FROM google_subscriptions`) != 0 || h.play.AcknowledgeCalls.Load() != 0 ||
		h.baseReplies(s.userID) != 0 {
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
	if got := h.entitlement(other.userID); !got.Plan.IsFree || got.Balance.Packs != 0 {
		t.Fatalf("entitlement moved: %+v", got)
	}
}

func TestGoogleTestSubscriptionsNeedTheSwitch(t *testing.T) {
	test := func(p *googleplay.Subscription) { p.TestPurchase = &googleplay.TestPurchase{} }
	h := googleHarness(t, withEnv("GOOGLE_PLAY_ACCEPT_TEST", "false"))
	s := h.signIn("gsub-test-off@example.com")
	token := playToken(120)
	h.playSubscription(s.userID, googleBase, token, h.clock.Now().Add(time.Hour), test)
	res := h.postGoogle(s.access, googleBase, token)
	if res.status != http.StatusBadRequest || res.str("error", "details", "reason") != "environment" || h.scalar(`SELECT COUNT(*) FROM google_subscriptions`) != 0 {
		t.Fatalf("test purchase with the switch off: %d %s", res.status, res.raw)
	}

	on := googleHarness(t, withEnv("GOOGLE_PLAY_ACCEPT_TEST", "true"))
	s = on.signIn("gsub-test-on@example.com")
	on.playSubscription(s.userID, googleBase, token, on.clock.Now().Add(time.Hour), test)
	mustStatus(t, on.postGoogle(s.access, googleBase, token), http.StatusOK, "")
	if on.text(`SELECT environment FROM google_subscriptions`) != domain.EnvironmentTest ||
		on.text(`SELECT environment FROM iap_purchases`) != domain.EnvironmentTest {
		t.Fatal("a license test is not marked Test")
	}
}

func TestGooglePendingSubscription(t *testing.T) {
	h := googleHarness(t)
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
		h.scalar(`SELECT COUNT(*) FROM google_subscriptions`) != 0 || h.play.AcknowledgeCalls.Load() != 0 || !h.entitlement(s.userID).Plan.IsFree ||
		h.baseReplies(s.userID) != 0 {
		t.Fatal("a pending subscription granted something")
	}
	h.play.UpdateSubscription(token, func(p *googleplay.Subscription) { p.SubscriptionState = domain.GoogleSubActive })
	paid := h.postGoogle(s.access, googleBase, token)
	if paid.status != http.StatusOK || paid.str("status") != "granted" || h.play.AcknowledgeCalls.Load() != 1 || h.baseReplies(s.userID) != 1000 ||
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
	h := googleHarness(t)
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
		h.play.AcknowledgeCalls.Load() != 0 || h.baseReplies(s.userID) != 0 {
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
	if h.baseReplies(s.userID) != 1000 {
		t.Fatal("on hold took the replies")
	}
}

func TestGoogleGraceAndCanceledKeepAccessUntilTheirEnd(t *testing.T) {
	h := googleHarness(t)

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

// In grace latestOrderId is the renewal that is failing, not a payment.
func TestGoogleFailingRenewalInGraceAddsNoBundle(t *testing.T) {
	h := googleHarness(t)
	s := h.signIn("gbase-grace@example.com")
	token := playToken(310)
	end := h.clock.Now().Add(2 * time.Hour)
	h.playSubscription(s.userID, googleBase, token, end, nil)
	mustStatus(t, h.postGoogle(s.access, googleBase, token), http.StatusOK, "")
	h.play.UpdateSubscription(token, func(p *googleplay.Subscription) {
		p.SubscriptionState = domain.GoogleSubGrace
		p.LatestOrderID = "GPA.3333-0310..0"
		p.LineItems[0].ExpiryTime = end.Add(16 * 24 * time.Hour).Format(time.RFC3339)
	})
	if res := h.postGoogle(s.access, googleBase, token); res.status != http.StatusOK || res.str("status") != "already_granted" {
		t.Fatalf("grace: %s", res.raw)
	}
	if h.baseReplies(s.userID) != 1000 || h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE user_id=?`, s.userID) != 1 {
		t.Fatal("a declined renewal counted as a paid order")
	}
	// The payment goes through: Google names it as successful, and it counts.
	h.play.UpdateSubscription(token, func(p *googleplay.Subscription) {
		p.SubscriptionState = domain.GoogleSubActive
		p.LineItems[0].LatestSuccessfulOrderID = "GPA.3333-0310..0"
	})
	if res := h.postGoogle(s.access, googleBase, token); res.str("status") != "granted" || h.baseReplies(s.userID) != 2000 {
		t.Fatalf("recovered: %s", res.raw)
	}
}

func TestGoogleRenewalArrivesThroughTheWorker(t *testing.T) {
	h := googleHarness(t)
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
	// Paid for, so it is spendable now, though the renewed period starts at the old end.
	if ent := h.entitlement(s.userID); ent.Balance.Packs != 2000 {
		t.Fatalf("renewal bundle: %+v", ent.Balance)
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
	if again.str("status") != "already_granted" || h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE original_transaction_id=?`, sha256Hex(token)) != 2 ||
		h.baseReplies(s.userID) != 2000 {
		t.Fatalf("re-post after the renewal: %s", again.raw)
	}
}

func TestGoogleRenewalSeenFirstByTheAppAndThenLoss(t *testing.T) {
	h := googleHarness(t)
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
	h := googleHarness(t)
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

// Google has charged the renewal but nothing has read it yet: the old end must
// not cost a paying subscriber the access. The first read after the end decides.
func TestGoogleUnreadRenewalBoundaryKeepsAccess(t *testing.T) {
	h := googleHarness(t)
	s := h.signIn("gsub-boundary@example.com")
	token := playToken(185)
	end := h.clock.Now().Add(40 * time.Minute)
	h.playSubscription(s.userID, googlePro, token, end, nil)
	mustStatus(t, h.postGoogle(s.access, googlePro, token), http.StatusOK, "")
	h.renewOn(token, "GPA.3333-0185..0", end.Add(30*24*time.Hour))

	h.play.FailNext(1000)
	h.clock.Advance(45 * time.Minute)
	h.iap.Tick(context.Background())
	if !h.entitlement(s.userID).Unlimited {
		t.Fatal("an outage at the renewal took the access away")
	}

	// The bridge has a limit: an outage does not give access for ever.
	h.clock.Advance(47 * time.Hour)
	if !h.entitlement(s.userID).Unlimited {
		t.Fatal("the bridge ended early")
	}
	h.clock.Advance(2 * time.Hour)
	if h.entitlement(s.userID).Unlimited {
		t.Fatal("access outlived the bridge")
	}

	// Google is back: the read finds the renewal.
	h.play.FailNext(0)
	h.clock.Advance(7 * time.Hour)
	h.iap.Tick(context.Background())
	if !h.entitlement(s.userID).Unlimited {
		t.Fatal("the renewal was lost")
	}

	// A subscription that was switched off is not bridged.
	gone := h.signIn("gsub-boundary-off@example.com")
	offToken := playToken(186)
	offEnd := h.clock.Now().Add(30 * time.Minute)
	h.playSubscription(gone.userID, googlePro, offToken, offEnd, func(p *googleplay.Subscription) {
		p.SubscriptionState = domain.GoogleSubCanceled
		p.LineItems[0].AutoRenewingPlan.AutoRenewEnabled = false
	})
	mustStatus(t, h.postGoogle(gone.access, googlePro, offToken), http.StatusOK, "")
	h.clock.Advance(time.Hour)
	if h.entitlement(gone.userID).Unlimited {
		t.Fatal("a cancelled subscription kept access past its end")
	}
}

func TestGoogleAcknowledgementIsRetriedByTheWorker(t *testing.T) {
	h := googleHarness(t)
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

func TestGoogleOneStuckAcknowledgementDoesNotHoldTheOthersBack(t *testing.T) {
	h := googleHarness(t)
	var tokens []string
	for i := 0; i < 5; i++ {
		s := h.signIn(fmt.Sprintf("gsub-stuck-%d@example.com", i))
		token := playToken(193 + i)
		h.playSubscription(s.userID, googleBase, token, h.clock.Now().Add(30*24*time.Hour), nil)
		h.play.FailAcknowledgesFor(token, 1)
		mustStatus(t, h.postGoogle(s.access, googleBase, token), http.StatusOK, "")
		tokens = append(tokens, token)
		h.clock.Advance(time.Second)
	}
	// The oldest keeps failing; everyone behind it is still reached.
	h.play.FailAcknowledgesFor(tokens[0], 1000)
	h.clock.Advance(2 * time.Minute)
	h.iap.Tick(context.Background())
	if h.play.Subscription(tokens[0]).Acknowledged() {
		t.Fatal("the stuck token was acknowledged")
	}
	for _, token := range tokens[1:] {
		if !h.play.Subscription(token).Acknowledged() {
			t.Fatal("a stuck purchase held the others back")
		}
	}

	// In an outage a round stops after a few failures.
	for i := 0; i < 5; i++ {
		s := h.signIn(fmt.Sprintf("gsub-outage-ack-%d@example.com", i))
		token := playToken(250 + i)
		h.playSubscription(s.userID, googleBase, token, h.clock.Now().Add(30*24*time.Hour), nil)
		h.play.FailAcknowledgesFor(token, 1)
		mustStatus(t, h.postGoogle(s.access, googleBase, token), http.StatusOK, "")
	}
	h.play.FailAcknowledges(1000)
	h.clock.Advance(2 * time.Minute)
	before := h.play.AcknowledgeCalls.Load()
	h.iap.Tick(context.Background())
	if calls := h.play.AcknowledgeCalls.Load() - before; calls != 3 {
		t.Fatalf("calls in an outage: %d", calls)
	}
}

// A token that cannot be opened now may be sealed again by the app's next post:
// that is no reason to give the acknowledgement up inside Google's window.
func TestGoogleUnreadableTokenDoesNotAbandonTheAcknowledgement(t *testing.T) {
	h := googleHarness(t)
	s := h.signIn("gsub-unreadable@example.com")
	token := playToken(198)
	h.playSubscription(s.userID, googleBase, token, h.clock.Now().Add(30*24*time.Hour), nil)
	h.play.FailAcknowledges(1)
	mustStatus(t, h.postGoogle(s.access, googleBase, token), http.StatusOK, "")
	h.exec(`UPDATE google_subscriptions SET purchase_token_sealed='v1:AAAA'`)
	h.clock.Advance(2 * time.Minute)
	h.iap.Tick(context.Background())
	if h.scalar(`SELECT ack_abandoned_at IS NULL AND acknowledged_at IS NULL FROM google_subscriptions`) != 1 {
		t.Fatal("an unreadable token gave the acknowledgement up")
	}
	s = h.refresh(s)
	mustStatus(t, h.postGoogle(s.access, googleBase, token), http.StatusOK, "")
	if !h.play.Subscription(token).Acknowledged() || h.scalar(`SELECT acknowledged_at IS NOT NULL FROM google_subscriptions`) != 1 {
		t.Fatal("the next post did not finish the acknowledgement")
	}
}

func TestGoogleUpgradeSupersedesTheOldToken(t *testing.T) {
	h := googleHarness(t)
	s := h.signIn("gsub-upgrade@example.com")
	oldToken, newToken := playToken(200), playToken(201)
	end := h.clock.Now().Add(20 * time.Hour)
	h.playSubscription(s.userID, googleBase, oldToken, end, nil)
	mustStatus(t, h.postGoogle(s.access, googleBase, oldToken), http.StatusOK, "")

	proEnd := h.clock.Now().Add(30 * 24 * time.Hour)
	h.playSubscription(s.userID, googlePro, newToken, proEnd, func(p *googleplay.Subscription) { p.LinkedPurchaseToken = oldToken })
	mustStatus(t, h.postGoogle(s.access, googlePro, newToken), http.StatusOK, "")
	ent := h.entitlement(s.userID)
	if !ent.Unlimited || ent.Plan.Code != "pro_monthly" || ent.Subscription.ID != sha256Hex(newToken) || ent.Balance.Packs != 1000 {
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
	if h.entitlement(s.userID).Plan.Code != "pro_monthly" || h.play.AcknowledgeCalls.Load() != 2 || h.baseReplies(s.userID) != 1000 {
		t.Fatal("the old token changed something")
	}
	reads := h.play.SubscriptionCalls.Load()
	h.clock.Advance(2 * time.Hour)
	h.iap.Tick(context.Background())
	if h.play.SubscriptionCalls.Load() != reads {
		t.Fatal("the worker reads a replaced token")
	}

	// The replaced plan is no fallback when the new one ends; the balance stays.
	h.clock.Advance(40 * 24 * time.Hour)
	if ent := h.entitlement(s.userID); !ent.Plan.IsFree || ent.Balance.Packs != 1000 {
		t.Fatalf("the replaced token gave access: %+v", ent)
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
		h.entitlement(other.userID).Plan.Code != "pro_monthly" || h.baseReplies(other.userID) != 0 {
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
	h := googleHarness(t)
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
	if ent := h.entitlement(s.userID); !ent.Plan.IsFree || ent.Balance.Packs != 30 {
		t.Fatal("the worker brought a refunded order back")
	}

	// The next renewal is a new paid order: access returns, with its own bundle.
	next := end.Add(30 * 24 * time.Hour)
	h.renewOn(token, order+"..0", next)
	h.clock.Advance(30*24*time.Hour - 2*time.Hour)
	h.iap.Tick(context.Background())
	ent := h.entitlement(s.userID)
	if ent.Plan.Code != "base_monthly" || ent.Balance.Packs != 1030 ||
		h.scalar(`SELECT revoked_at IS NULL AND revoked_order_id='' FROM google_subscriptions`) != 1 {
		t.Fatal("a later paid order must restore the subscription")
	}
}

func TestGoogleRefundOfAnEarlierOrderLeavesTheCurrentPeriod(t *testing.T) {
	h := googleHarness(t)
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
	if ent := h.entitlement(s.userID); ent.Plan.Code != "base_monthly" || ent.Balance.Packs != 1000 || h.scalar(`SELECT revoked_at IS NULL FROM google_subscriptions`) != 1 {
		t.Fatal("a refund of a finished period took the paid one away")
	}
	if h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE transaction_id=? AND status='refunded'`, sha256Hex(token)) != 1 ||
		h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE transaction_id=? AND status='granted'`, sha256Hex(token)+":"+first+"..0") != 1 {
		t.Fatal("orders")
	}
}

// Google reports a charged-back subscription as expired and lists only the
// order: the list is the only sign of it until the next read, which may be far off.
func TestGoogleVoidedEarlierOrderSchedulesARead(t *testing.T) {
	h := googleHarness(t)
	s := h.signIn("gsub-chargeback@example.com")
	token := playToken(214)
	end := h.clock.Now().Add(2 * time.Hour)
	h.playSubscription(s.userID, googlePro, token, end, nil)
	mustStatus(t, h.postGoogle(s.access, googlePro, token), http.StatusOK, "")
	first := "GPA.3333-" + token[13:17]
	year := end.AddDate(1, 0, 0)
	h.renewOn(token, first+"..0", year)
	mustStatus(t, h.postGoogle(s.access, googlePro, token), http.StatusOK, "")
	if due := h.scalar(`SELECT next_check_at FROM google_subscriptions`); due != int(year.Add(-24*time.Hour).UnixMilli()) {
		t.Fatalf("next read: %d", due)
	}

	h.play.Void(googleplay.VoidedPurchase{PurchaseToken: token, OrderID: first, VoidedTimeMillis: itoa(h.clock.Now().UnixMilli()), VoidedReason: 8})
	h.play.UpdateSubscription(token, func(p *googleplay.Subscription) { p.SubscriptionState = domain.GoogleSubExpired })
	if revoked, err := h.iap.PollVoided(context.Background()); err != nil || revoked != 0 {
		t.Fatalf("poll: %d %v", revoked, err)
	}
	if due := h.scalar(`SELECT next_check_at FROM google_subscriptions`); due != int(h.clock.Now().UnixMilli()) {
		t.Fatalf("no read was scheduled: %d", due)
	}
	if !h.entitlement(s.userID).Unlimited {
		t.Fatal("Google's state decides, at the read")
	}
	h.iap.Tick(context.Background())
	if h.entitlement(s.userID).Unlimited || h.text(`SELECT state FROM google_subscriptions`) != domain.GoogleSubExpired {
		t.Fatal("a charged-back subscription kept its access")
	}

	// The list repeats an order it already reported: no more reads for that.
	still := h.signIn("gsub-chargeback-live@example.com")
	live := playToken(215)
	liveEnd := h.clock.Now().Add(2 * time.Hour)
	h.playSubscription(still.userID, googlePro, live, liveEnd, nil)
	mustStatus(t, h.postGoogle(still.access, googlePro, live), http.StatusOK, "")
	liveFirst := "GPA.3333-" + live[13:17]
	h.renewOn(live, liveFirst+"..0", liveEnd.AddDate(1, 0, 0))
	mustStatus(t, h.postGoogle(still.access, googlePro, live), http.StatusOK, "")
	h.play.Void(googleplay.VoidedPurchase{PurchaseToken: live, OrderID: liveFirst, VoidedTimeMillis: itoa(h.clock.Now().UnixMilli()), VoidedReason: 1})
	if _, err := h.iap.PollVoided(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.iap.Tick(context.Background())
	if !h.entitlement(still.userID).Unlimited {
		t.Fatal("a refund of a finished period took the paid one away")
	}
	reads := h.play.SubscriptionCalls.Load()
	if _, err := h.iap.PollVoided(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.iap.Tick(context.Background())
	if h.play.SubscriptionCalls.Load() != reads {
		t.Fatal("a repeated void read the subscription again")
	}
}

// Google lists a voided renewal before the worker has read that renewal.
func TestGoogleRefundOfAnUnreadRenewalIsNotGranted(t *testing.T) {
	h := googleHarness(t)
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
	if ent := h.entitlement(s.userID); !ent.Plan.IsFree || ent.Balance.Packs != 1000 || h.scalar(`SELECT revoked_order_id=? FROM google_subscriptions`, order) != 1 {
		t.Fatalf("a refunded renewal was granted: %+v", ent)
	}
}

func TestGoogleRefundBeforeThePostIsNeverGranted(t *testing.T) {
	h := googleHarness(t)
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
	if late.status != http.StatusBadRequest || late.str("error", "details", "reason") != "revoked" || !h.entitlement(s.userID).Plan.IsFree || h.baseReplies(s.userID) != 0 {
		t.Fatalf("a voided token was granted: %s", late.raw)
	}
	// A later paid order on the same token is a real purchase.
	h.renewOn(token, order+"..0", end.Add(30*24*time.Hour))
	if res := h.postGoogle(s.access, googleBase, token); res.str("status") != "granted" || h.entitlement(s.userID).Plan.Code != "base_monthly" || h.baseReplies(s.userID) != 1000 {
		t.Fatalf("later order: %s", res.raw)
	}
}

func TestGoogleSubscriptionsStayOffUntilConfigured(t *testing.T) {
	h := monthlyBaseHarness(t)
	s := h.signIn("gsub-off@example.com")
	ios := h.products(s.access, "ios")
	mustStatus(t, ios, http.StatusOK, "")
	if len(ios.body["products"].([]any)) != 4 {
		t.Fatalf("iOS catalogue changed: %s", ios.raw)
	}
	mustStatus(t, h.products(s.access, "android"), http.StatusForbidden, "IAP_UNAVAILABLE")
	if h.feature("iap_android") {
		t.Fatal("android open without a Google id")
	}
	token := playToken(220)
	h.playSubscription(s.userID, googleBase, token, h.clock.Now().Add(time.Hour), nil)
	res := h.postGoogle(s.access, googleBase, token)
	if res.status != http.StatusBadRequest || res.str("error", "details", "reason") != "product_unknown" || h.play.SubscriptionCalls.Load() != 0 {
		t.Fatalf("no mapping: %s", res.raw)
	}
	h.iap.Tick(context.Background())
	usage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(s.access))
	if h.scalar(`SELECT COUNT(*) FROM google_subscriptions`)+h.scalar(`SELECT COUNT(*) FROM iap_purchases`)+h.scalar(`SELECT COUNT(*) FROM credit_grants WHERE product_code='google_base_monthly'`) != 0 ||
		h.play.SubscriptionCalls.Load() != 0 || h.play.AcknowledgeCalls.Load() != 0 || usage.body["credits_never_expire"] != false {
		t.Fatalf("something was written with no Google id: %s", usage.raw)
	}

	// A product switched off for Android is mapped but not offered.
	h.exec(`UPDATE iap_products SET google_product_id=?,off_android=1 WHERE code='base_monthly'`, googleBase)
	mustStatus(t, h.products(s.access, "android"), http.StatusForbidden, "IAP_UNAVAILABLE")
	if !h.feature("iap_ios") {
		t.Fatal("the Android switch changed iOS")
	}
	// Mapping a Google id changes nothing for iOS.
	if after := h.products(s.access, "ios"); string(after.raw) != string(ios.raw) {
		t.Fatalf("iOS answer changed:\n%s\n%s", ios.raw, after.raw)
	}
	// Paid is paid: a purchase made before the switch went off is honoured.
	mustStatus(t, h.postGoogle(s.access, googleBase, token), http.StatusOK, "")
}

func TestAndroidProductsListSubscriptionsLikeIOS(t *testing.T) {
	h := googleHarness(t)
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
		base["credits_never_expire"] != true || base["price"] != float64(99000) || base["currency"] != "KZT" {
		t.Fatalf("base: %v", base)
	}
	if pro["code"] != "pro_monthly" || pro["store_product_id"] != googlePro || pro["entitlement_kind"] != "unlimited" || pro["daily_limit"] != float64(0) ||
		pro["monthly_limit"] != nil || pro["price"] != float64(399000) || pro["credits_never_expire"] != nil {
		t.Fatalf("pro: %v", pro)
	}
	if res.str("account_token") != h.iap.AccountToken(s.userID) || res.body["recommendation"] != nil {
		t.Fatalf("android payload: %s", res.raw)
	}
}

func TestAndroidRecommendsBaseWithItsPlayID(t *testing.T) {
	h := monthlyBaseHarness(t)
	h.enableGoogleSubscriptions()
	h.exec(`UPDATE iap_products SET google_product_id=? WHERE code='pack30'`, googlePack)
	s := h.signIn("gsub-recommend@example.com")
	for i := 0; i < 4; i++ {
		token := playToken(260 + i)
		h.playPurchase(s.userID, googlePack, token, nil)
		mustStatus(t, h.postGoogle(s.access, googlePack, token), http.StatusOK, "")
	}
	android := h.products(s.access, "android")
	if android.num("recommendation", "purchase_count") != 4 || android.num("recommendation", "spent_minor") != 52000 || android.num("recommendation", "base_price_minor") != 99000 ||
		android.str("recommendation", "base_store_product_id") != googleBase || android.str("recommendation", "currency") != "KZT" {
		t.Fatalf("android recommendation: %s", android.raw)
	}
	// iOS counts what Apple signed, as before.
	if ios := h.products(s.access, "ios"); ios.body["recommendation"] != nil {
		t.Fatalf("iOS answer changed: %s", ios.raw)
	}

	// Packs bought from Apple count on Android too, and iOS keeps the Apple id.
	apple := h.signIn("gsub-recommend-apple@example.com")
	for i := 0; i < 5; i++ {
		signed := h.appleTx(apple.userID, fmt.Sprintf("recommend-google-%d", i), "test.pack30", func(tx *appstoretest.Transaction) { tx.Price = 130000; tx.Currency = "KZT" })
		mustStatus(t, h.postApple(apple.access, signed), http.StatusOK, "")
	}
	onIOS := h.products(apple.access, "ios")
	onAndroid := h.products(apple.access, "android")
	if onIOS.str("recommendation", "base_store_product_id") != "test.base_monthly" || onAndroid.str("recommendation", "base_store_product_id") != googleBase ||
		onAndroid.num("recommendation", "spent_minor") != 65000 || onIOS.num("recommendation", "spent_minor") != 65000 {
		t.Fatalf("recommendation:\n%s\n%s", onIOS.raw, onAndroid.raw)
	}

	// A subscriber has nothing to be recommended.
	token := playToken(270)
	h.playSubscription(s.userID, googleBase, token, h.clock.Now().Add(30*24*time.Hour), nil)
	mustStatus(t, h.postGoogle(s.access, googleBase, token), http.StatusOK, "")
	if rec := h.products(s.access, "android").body["recommendation"]; rec != nil {
		t.Fatalf("subscriber recommendation: %v", rec)
	}
}

func TestAndroidGetsNoRecommendationWhileOnlyPacksAreOnSale(t *testing.T) {
	h := newHarness(t, withEnv("TEST_APPROVED_ENTITLEMENTS", "true"), withIAP(t))
	h.exec(`UPDATE iap_products SET google_product_id=?,is_active=1 WHERE code='pack30'`, googlePack)
	mustStatus(t, h.adminCall(h.signInAdmin(), http.MethodPost, "/settings/iap", map[string]any{"enabled": true}), http.StatusOK, "")
	s := h.signIn("gsub-packs-only@example.com")
	for i := 0; i < 4; i++ {
		token := playToken(275 + i)
		h.playPurchase(s.userID, googlePack, token, nil)
		mustStatus(t, h.postGoogle(s.access, googlePack, token), http.StatusOK, "")
	}
	res := h.products(s.access, "android")
	mustStatus(t, res, http.StatusOK, "")
	if len(res.body["products"].([]any)) != 1 || res.body["recommendation"] != nil || h.scalar(`SELECT consumed_at IS NOT NULL FROM iap_purchases LIMIT 1`) != 1 {
		t.Fatalf("packs only: %s", res.raw)
	}
}

func TestAdminPurchasesTellSubscriptionOrdersFromPacks(t *testing.T) {
	h := monthlyBaseHarness(t)
	h.enableGoogleSubscriptions()
	h.exec(`UPDATE iap_products SET google_product_id=? WHERE code='pack30'`, googlePack)
	s := h.signIn("gsub-admin@example.com")
	pack, sub := playToken(280), playToken(281)
	h.playPurchase(s.userID, googlePack, pack, nil)
	mustStatus(t, h.postGoogle(s.access, googlePack, pack), http.StatusOK, "")
	h.playSubscription(s.userID, googleBase, sub, h.clock.Now().Add(30*24*time.Hour), nil)
	mustStatus(t, h.postGoogle(s.access, googleBase, sub), http.StatusOK, "")
	res := h.adminCall(h.signInAdmin(), http.MethodGet, "/iap/purchases", nil)
	mustStatus(t, res, http.StatusOK, "")
	var body struct {
		Purchases []struct {
			ProductCode       string `json:"product_code"`
			Units             int    `json:"units"`
			Consumed          bool   `json:"consumed"`
			SubscriptionOrder bool   `json:"subscription_order"`
		} `json:"purchases"`
	}
	if err := json.Unmarshal([]byte(res.raw), &body); err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, p := range body.Purchases {
		switch p.ProductCode {
		case "base_monthly":
			seen++
			if !p.SubscriptionOrder || p.Units != 1000 {
				t.Fatalf("subscription order: %+v", p)
			}
		case "pack30":
			seen++
			if p.SubscriptionOrder || !p.Consumed {
				t.Fatalf("pack: %+v", p)
			}
		}
	}
	if seen != 2 {
		t.Fatalf("purchases: %s", res.raw)
	}
}

func TestGoogleJoinsAppleAndBankWithoutChangingThem(t *testing.T) {
	h := monthlyBaseHarness(t)
	h.enableGoogleSubscriptions()
	s := h.signIn("gsub-mixed@example.com")
	now := h.clock.Now()
	mustStatus(t, h.postApple(s.access, h.subscriptionTx(s.userID, "mixed-apple", "mixed-chain", "base_monthly", now, now.Add(30*24*time.Hour), nil)), http.StatusOK, "")
	ent := h.entitlement(s.userID)
	if ent.Plan.Code != "base_monthly" || ent.Subscription.Source != domain.ProviderAppStore || ent.Balance.Packs != 1000 {
		t.Fatalf("Apple alone: %+v", ent)
	}
	token := playToken(230)
	h.playSubscription(s.userID, googlePro, token, now.Add(30*24*time.Hour), nil)
	mustStatus(t, h.postGoogle(s.access, googlePro, token), http.StatusOK, "")
	ent = h.entitlement(s.userID)
	if !ent.Unlimited || ent.Subscription.Source != domain.ProviderGooglePlay || ent.Balance.Packs != 1000 {
		t.Fatalf("Google Pro over Apple Base: %+v", ent)
	}
	if h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE provider='app_store' AND user_id=?`, s.userID) != 1 ||
		h.scalar(`SELECT COUNT(*) FROM apple_subscriptions WHERE user_id=?`, s.userID) != 1 {
		t.Fatal("Apple records changed")
	}

	// A paid bank Pro outranks a Google Base, and the Base bundle still arrives.
	bank := h.signIn("gsub-bank@example.com")
	h.bankPro("gsub-bank-pro", bank.userID, now.Add(-time.Hour), now.Add(29*24*time.Hour))
	baseToken := playToken(231)
	h.playSubscription(bank.userID, googleBase, baseToken, now.Add(30*24*time.Hour), nil)
	mustStatus(t, h.postGoogle(bank.access, googleBase, baseToken), http.StatusOK, "")
	ent = h.entitlement(bank.userID)
	if !ent.Unlimited || ent.Subscription.Source != "bcc" || ent.Balance.Packs != 1000 {
		t.Fatalf("bank Pro over Google Base: %+v", ent)
	}
}

// Google has not moved a just-ended period on yet: it is read again, not given up.
func TestGoogleStaleEndKeepsTheSubscriptionBeingRead(t *testing.T) {
	h := googleHarness(t)
	s := h.signIn("gsub-stale@example.com")
	token := playToken(240)
	end := h.clock.Now().Add(time.Hour)
	h.playSubscription(s.userID, googlePro, token, end, nil)
	mustStatus(t, h.postGoogle(s.access, googlePro, token), http.StatusOK, "")

	h.clock.Advance(90 * time.Minute)
	if !h.entitlement(s.userID).Unlimited {
		t.Fatal("nothing has read past the end yet")
	}
	h.iap.Tick(context.Background())
	if h.entitlement(s.userID).Unlimited {
		t.Fatal("access outlived the read that found the old end")
	}
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
	h := googleHarness(t)
	s := h.signIn("gsub-gone@example.com")
	token := playToken(241)
	h.playSubscription(s.userID, googleBase, token, h.clock.Now().Add(30*24*time.Hour), nil)
	mustStatus(t, h.postGoogle(s.access, googleBase, token), http.StatusOK, "")
	mustStatus(t, h.do(http.MethodDelete, "/api/v1/me", nil, h.auth(s.access)), http.StatusOK, "")
	if h.scalar(`SELECT COUNT(*) FROM google_subscriptions`) != 0 || h.scalar(`SELECT COUNT(*) FROM credit_grants WHERE product_code='google_base_monthly'`) != 0 ||
		h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE transaction_id=? AND user_id IS NULL`, sha256Hex(token)) != 1 {
		t.Fatal("account deletion")
	}
	next := h.signIn("gsub-gone-new@example.com")
	h.play.UpdateSubscription(token, func(p *googleplay.Subscription) {
		p.ExternalAccountIdentifiers.ObfuscatedExternalAccountID = h.iap.AccountToken(next.userID)
	})
	mustStatus(t, h.postGoogle(next.access, googleBase, token), http.StatusConflict, "PURCHASE_BELONGS_TO_ANOTHER_ACCOUNT")
}
