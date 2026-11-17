package apptest

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/audio/audiotest"
	"github.com/aireply/ai-reply-back-end/internal/domain"
)

func proPackFallbackAccount(t *testing.T, plan string, options ...harnessOption) (*harness, session, session) {
	t.Helper()
	options = append([]harnessOption{approvedEntitlements(), legacyProDevices(), withWelcomeBonus(10)}, options...)
	h := newHarness(t, options...)
	phone, _ := h.signInProDevice("pro-pack-fallback@example.com", "pro-pack-owner-phone", "ios", "")
	h.assignPlan(h.signInAdmin(), phone.userID, plan, "")
	phone = h.refresh(phone)
	desktop, _ := h.signInProDevice("pro-pack-fallback@example.com", "pro-pack-owner-desktop", "macos", "")
	return h, phone, desktop
}

func assertProPackFallbackSlots(t *testing.T, h *harness, userID string) {
	t.Helper()
	if h.scalar(`SELECT COUNT(*) FROM pro_device_authorizations WHERE user_id=? AND revoked_at IS NULL`, userID) != 2 ||
		h.scalar(`SELECT COUNT(*) FROM pro_device_authorizations WHERE user_id=? AND category='mobile' AND revoked_at IS NULL`, userID) != 1 ||
		h.scalar(`SELECT COUNT(*) FROM pro_device_authorizations WHERE user_id=? AND category='desktop' AND revoked_at IS NULL`, userID) != 1 {
		t.Fatal("paid fallback changed Pro device slots")
	}
}

func TestProDevicePurchasedPackFallbackMonthlyAndYearly(t *testing.T) {
	for _, plan := range []string{"pro_monthly", "pro_yearly"} {
		t.Run(plan, func(t *testing.T) {
			h, owner, desktop := proPackFallbackAccount(t, plan)
			now, expires := h.clock.Now(), h.clock.Now().Add(time.Hour)
			admin := h.addGrant(owner.userID, domain.GrantAdmin, 7, now, nil, now)
			pass := h.addGrant(owner.userID, domain.GrantPass, 9, now, &expires, now)
			pack := h.addGrant(owner.userID, domain.GrantPack, 3, now, nil, now)
			var extras []session
			for i, platform := range []string{"android", "ios", "windows"} {
				extra, _ := h.signInProDevice("pro-pack-fallback@example.com", fmt.Sprintf("pro-pack-extra-%d", i), platform, "")
				extras = append(extras, extra)
			}
			for _, authorized := range []session{owner, desktop} {
				res := h.generate(authorized.access, "Сәлем")
				mustStatus(t, res, http.StatusOK, "")
				if res.str("usage", "bucket") != domain.BucketUnlimited || res.body["usage"].(map[string]any)["unlimited"] != true ||
					h.scalar(`SELECT units_remaining FROM credit_grants WHERE id=?`, pack.ID) != 3 {
					t.Fatalf("authorized device spent a purchased answer: %s", res.raw)
				}
			}
			for i, extra := range extras {
				var res response
				if i == 1 {
					res = h.compose(extra.access, map[string]any{"instruction": "Әріптесімді құттықта", "language": "kk", "platform": "ios"})
				} else {
					res = h.generate(extra.access, "Сәлем")
				}
				mustStatus(t, res, http.StatusOK, "")
				usage, _ := res.body["usage"].(map[string]any)
				if res.str("usage", "bucket") != domain.BucketPack || usage["unlimited"] != false ||
					res.num("usage", "purchased_credits") != float64(2-i) || res.num("usage", "remaining_total") != float64(2-i) ||
					res.num("usage", "admin_credits") != 0 || res.num("usage", "welcome", "remaining") != 0 || res.num("usage", "pass", "remaining") != 0 {
					t.Fatalf("extra device did not use its paid-only balance: %s", res.raw)
				}
				if h.scalar(`SELECT units_remaining FROM credit_grants WHERE id=?`, pack.ID) != 2-i {
					t.Fatal("extra devices did not share the purchased balance")
				}
			}
			before := h.provider.calls
			for _, extra := range extras {
				mustStatus(t, h.generate(extra.access, "Сәлем"), http.StatusConflict, "PRO_DEVICE_LIMIT_REACHED")
				mustStatus(t, h.do(http.MethodGet, "/api/v1/me", nil, h.auth(extra.access)), http.StatusOK, "")
				usage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(extra.access))
				mustStatus(t, usage, http.StatusOK, "")
				if usage.num("purchased_credits") != 0 {
					t.Fatalf("shared account balance did not reach zero: %s", usage.raw)
				}
			}
			if h.provider.calls != before || h.welcomeLeft(owner.userID) != 10 ||
				h.scalar(`SELECT units_remaining FROM credit_grants WHERE id=?`, admin.ID) != 7 ||
				h.scalar(`SELECT units_remaining FROM credit_grants WHERE id=?`, pass.ID) != 9 {
				t.Fatal("extra device consumed non-purchased credit or called the provider after pack exhaustion")
			}
			for _, authorized := range []session{owner, desktop} {
				res := h.generate(authorized.access, "Сәлем")
				mustStatus(t, res, http.StatusOK, "")
				if res.str("usage", "bucket") != domain.BucketUnlimited {
					t.Fatal("pack exhaustion removed authorized Pro access")
				}
			}
			assertProPackFallbackSlots(t, h, owner.userID)
		})
	}
}

func TestProDevicePurchasedPackFallbackLastAnswerIsAtomic(t *testing.T) {
	h, owner, _ := proPackFallbackAccount(t, "pro_monthly")
	pack := h.addGrant(owner.userID, domain.GrantPack, 1, h.clock.Now(), nil, h.clock.Now())
	a, _ := h.signInProDevice("pro-pack-fallback@example.com", "pro-pack-race-android", "android", "")
	b, _ := h.signInProDevice("pro-pack-fallback@example.com", "pro-pack-race-ios", "ios", "")
	before := h.provider.calls
	const requests = 8
	start := make(chan struct{})
	results := make(chan response, requests)
	var wg sync.WaitGroup
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			access := a.access
			if index%2 != 0 {
				access = b.access
			}
			results <- h.generateWithKey(access, "Сәлем", fmt.Sprintf("pro-pack-race-%02d", index))
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	succeeded := 0
	for res := range results {
		if res.status == http.StatusOK {
			succeeded++
			if res.str("usage", "bucket") != domain.BucketPack {
				t.Fatalf("race escaped paid-only accounting: %s", res.raw)
			}
		} else {
			mustStatus(t, res, http.StatusConflict, "PRO_DEVICE_LIMIT_REACHED")
		}
	}
	if succeeded != 1 || h.provider.calls != before+1 || h.scalar(`SELECT units_remaining FROM credit_grants WHERE id=?`, pack.ID) != 0 ||
		h.scalar(`SELECT COUNT(*) FROM credit_ledger WHERE user_id=? AND entry='consume' AND kind='pack'`, owner.userID) != 1 || h.welcomeLeft(owner.userID) != 10 {
		t.Fatal("concurrent extra devices overspent the last purchased answer")
	}
	assertProPackFallbackSlots(t, h, owner.userID)
}

func TestProDevicePurchasedPackFallbackCannotReplayUnlimitedButCanRetryPaid(t *testing.T) {
	h, owner, _ := proPackFallbackAccount(t, "pro_monthly")
	const ownerKey, paidKey = "pro-owner-unlimited-key", "pro-extra-purchased-key"
	mustStatus(t, h.generateWithKey(owner.access, "Сәлем", ownerKey), http.StatusOK, "")
	pack := h.addGrant(owner.userID, domain.GrantPack, 1, h.clock.Now(), nil, h.clock.Now())
	extra, _ := h.signInProDevice("pro-pack-fallback@example.com", "pro-pack-replay-extra", "android", "")
	before := h.provider.calls
	res := h.generateWithKey(extra.access, "Сәлем", ownerKey)
	mustStatus(t, res, http.StatusConflict, "CONFLICT")
	if res.str("error", "details", "field") != "idempotency_key" || res.str("error", "details", "reason") != "used" ||
		h.provider.calls != before || h.scalar(`SELECT replays FROM usage_reservations WHERE user_id=? AND idempotency_key=?`, owner.userID, ownerKey) != 0 {
		t.Fatalf("extra device replayed an authorized device's unlimited request: %s", res.raw)
	}
	// The rejected attempt did not take the owner's legitimate bounded retry.
	mustStatus(t, h.generateWithKey(owner.access, "Сәлем", ownerKey), http.StatusOK, "")
	first := h.generateWithKey(extra.access, "Сәлем", paidKey)
	mustStatus(t, first, http.StatusOK, "")
	if first.str("usage", "bucket") != domain.BucketPack || h.scalar(`SELECT units_remaining FROM credit_grants WHERE id=?`, pack.ID) != 0 {
		t.Fatal("paid request did not spend the last pack unit")
	}
	replay := h.generateWithKey(extra.access, "Сәлем", paidKey)
	mustStatus(t, replay, http.StatusOK, "")
	if replay.str("usage", "bucket") != domain.BucketPack || replay.num("usage", "purchased_credits") != 0 ||
		h.scalar(`SELECT units_remaining FROM credit_grants WHERE id=?`, pack.ID) != 0 ||
		h.scalar(`SELECT COUNT(*) FROM credit_ledger WHERE grant_id=? AND entry='consume'`, pack.ID) != 1 {
		t.Fatal("bounded paid retry charged again or became unlimited")
	}
	before = h.provider.calls
	mustStatus(t, h.generateWithKey(extra.access, "Сәлем", paidKey), http.StatusConflict, "CONFLICT")
	mustStatus(t, h.generateWithKey(extra.access, "Сәлем", "pro-extra-new-key-empty"), http.StatusConflict, "PRO_DEVICE_LIMIT_REACHED")
	if h.provider.calls != before {
		t.Fatal("exhausted retry or empty pack reached the provider")
	}
}

func TestProDevicePurchasedPackFallbackProviderFailureRefundsSamePack(t *testing.T) {
	h, owner, _ := proPackFallbackAccount(t, "pro_monthly")
	pack := h.addGrant(owner.userID, domain.GrantPack, 1, h.clock.Now(), nil, h.clock.Now())
	extra, _ := h.signInProDevice("pro-pack-fallback@example.com", "pro-pack-refund-extra", "android", "")
	h.provider.err = domain.ErrProviderDown
	mustStatus(t, h.generateWithKey(extra.access, "Сәлем", "pro-pack-refund-key"), http.StatusBadGateway, "AI_PROVIDER_UNAVAILABLE")
	if h.scalar(`SELECT units_remaining FROM credit_grants WHERE id=?`, pack.ID) != 1 ||
		h.scalar(`SELECT COUNT(*) FROM credit_ledger WHERE grant_id=? AND entry='refund'`, pack.ID) != 1 || h.welcomeLeft(owner.userID) != 10 {
		t.Fatal("provider failure did not restore the charged purchased answer")
	}
	h.provider.err = nil
	res := h.generateWithKey(extra.access, "Сәлем", "pro-pack-refund-key")
	mustStatus(t, res, http.StatusOK, "")
	if res.str("usage", "bucket") != domain.BucketPack || h.scalar(`SELECT units_remaining FROM credit_grants WHERE id=?`, pack.ID) != 0 || h.welcomeLeft(owner.userID) != 10 {
		t.Fatal("retry after a refund used another balance")
	}
}

func TestProDevicePurchasedPackFallbackNeverBypassesRevocationOrIdentity(t *testing.T) {
	t.Run("revoked legacy device", func(t *testing.T) {
		h, owner, _ := proPackFallbackAccount(t, "pro_monthly")
		pack := h.addGrant(owner.userID, domain.GrantPack, 5, h.clock.Now(), nil, h.clock.Now())
		authorization := h.proDeviceAuthorization(owner.access, "mobile")
		mustStatus(t, h.adminCall(h.signInAdmin(), http.MethodPost, "/users/"+owner.userID+"/pro-devices/"+authorization+"/revoke", map[string]any{"reason": "test revoked owner"}), http.StatusOK, "")
		before := h.provider.calls
		mustStatus(t, h.generate(owner.access, "Сәлем"), http.StatusConflict, "PRO_DEVICE_REVOKED")
		mustStatus(t, h.polish(owner.access, map[string]any{"text": polishNote}), http.StatusConflict, "PRO_DEVICE_REVOKED")
		mustStatus(t, h.transcribe(owner.access, audiotest.M4A(2*time.Second, nil), voiceFields()), http.StatusConflict, "PRO_DEVICE_REVOKED")
		if h.provider.calls != before || len(h.speech.requests()) != 0 || h.scalar(`SELECT units_remaining FROM credit_grants WHERE id=?`, pack.ID) != 5 {
			t.Fatal("paid fallback bypassed a revoked Pro authorization")
		}
	})
	t.Run("mismatched signed installation", func(t *testing.T) {
		h := newHarness(t, approvedEntitlements(), legacyProDevices())
		owner, original := h.signInProDevice("pro-pack-identity@example.com", "pro-pack-identity-owner", "ios", installID(1201))
		h.assignPlan(h.signInAdmin(), owner.userID, "pro_monthly", "")
		mustStatus(t, h.generate(owner.access, "Сәлем"), http.StatusOK, "")
		pack := h.addGrant(owner.userID, domain.GrantPack, 5, h.clock.Now(), nil, h.clock.Now())
		clone, _ := h.signInProDevice("pro-pack-identity@example.com", original.str("device_id"), "ios", installID(1202))
		before := h.provider.calls
		mustStatus(t, h.generate(clone.access, "Сәлем"), http.StatusForbidden, "PRO_DEVICE_IDENTITY_REQUIRED")
		mustStatus(t, h.polish(clone.access, map[string]any{"text": polishNote}), http.StatusForbidden, "PRO_DEVICE_IDENTITY_REQUIRED")
		mustStatus(t, h.transcribe(clone.access, audiotest.M4A(2*time.Second, nil), voiceFields()), http.StatusForbidden, "PRO_DEVICE_IDENTITY_REQUIRED")
		if h.provider.calls != before || len(h.speech.requests()) != 0 || h.scalar(`SELECT units_remaining FROM credit_grants WHERE id=?`, pack.ID) != 5 {
			t.Fatal("paid fallback bypassed a mismatched signed installation")
		}
	})
}

func TestProDevicePurchasedPackFallbackAuxiliaryCallsPreservePackAndCaps(t *testing.T) {
	h, owner, _ := proPackFallbackAccount(t, "pro_monthly", withEnv("LIMIT_POLISH_PER_DAY", "1"), withEnv("LIMIT_TRANSCRIBE_PER_DAY", "1"))
	pack := h.addGrant(owner.userID, domain.GrantPack, 1, h.clock.Now(), nil, h.clock.Now())
	extra, _ := h.signInProDevice("pro-pack-fallback@example.com", "pro-pack-aux-extra", "android", "")
	before := h.provider.calls
	mustStatus(t, h.polish(extra.access, map[string]any{"text": polishNote}), http.StatusOK, "")
	mustStatus(t, h.polish(extra.access, map[string]any{"text": polishNote}), http.StatusOK, "")
	if h.provider.calls != before+1 {
		t.Fatal("paid fallback bypassed the existing polish daily cap")
	}
	recording, fields := audiotest.M4A(2*time.Second, nil), voiceFields()
	mustStatus(t, h.transcribe(extra.access, recording, fields), http.StatusOK, "")
	mustStatus(t, h.transcribe(extra.access, recording, voiceFields()), http.StatusTooManyRequests, "VOICE_LIMIT_REACHED")
	if len(h.speech.requests()) != 1 || h.scalar(`SELECT units_remaining FROM credit_grants WHERE id=?`, pack.ID) != 1 ||
		h.scalar(`SELECT COUNT(*) FROM credit_ledger WHERE grant_id=? AND entry='consume'`, pack.ID) != 0 {
		t.Fatal("auxiliary work spent an answer credit or bypassed the voice cap")
	}
	mustStatus(t, h.generate(extra.access, "Сәлем"), http.StatusOK, "")
	before = h.provider.calls
	// Authorization precedes both the polish cap and cached transcription.
	mustStatus(t, h.polish(extra.access, map[string]any{"text": polishNote}), http.StatusConflict, "PRO_DEVICE_LIMIT_REACHED")
	mustStatus(t, h.transcribe(extra.access, recording, fields), http.StatusConflict, "PRO_DEVICE_LIMIT_REACHED")
	if h.provider.calls != before || len(h.speech.requests()) != 1 || h.scalar(`SELECT units_remaining FROM credit_grants WHERE id=?`, pack.ID) != 0 || h.welcomeLeft(owner.userID) != 10 {
		t.Fatal("empty paid balance reached auxiliary providers or consumed another credit kind")
	}
}

func TestProDevicePurchasedPackFallbackUsageSurvivesSignInRefreshAndReload(t *testing.T) {
	h, owner, desktop := proPackFallbackAccount(t, "pro_monthly")
	now, expires := h.clock.Now(), h.clock.Now().Add(time.Hour)
	admin := h.addGrant(owner.userID, domain.GrantAdmin, 7, now, nil, now)
	pass := h.addGrant(owner.userID, domain.GrantPass, 9, now, &expires, now)
	pack := h.addGrant(owner.userID, domain.GrantPack, 3, now, nil, now)
	extra, signedIn := h.signInProDevice("pro-pack-fallback@example.com", "pro-pack-projection-extra", "android", "")
	finiteUsage := func(res response, nested bool, want int) {
		t.Helper()
		mustStatus(t, res, http.StatusOK, "")
		usage := res.body
		if nested {
			usage, _ = res.body["usage"].(map[string]any)
			if res.str("subscription", "plan", "code") != "pro_monthly" || res.str("subscription", "status") != "active" {
				t.Fatalf("effective usage projection changed the paid subscription: %s", res.raw)
			}
		}
		u := response{body: usage, raw: res.raw}
		if usage["unlimited"] != false || u.num("purchased_credits") != float64(want) || u.num("credits") != float64(want) ||
			u.num("remaining_total") != float64(want) || u.num("daily_limit") != 0 || u.num("remaining_today") != 0 ||
			u.num("monthly_limit") != 0 || u.num("admin_credits") != 0 || u.num("welcome", "remaining") != 0 || u.num("pass", "remaining") != 0 {
			t.Fatalf("extra device reload restored unlimited or non-purchased access: %s", res.raw)
		}
	}
	finiteUsage(signedIn, true, 3)
	finiteUsage(h.do(http.MethodGet, "/api/v1/me", nil, h.auth(extra.access)), true, 3)
	finiteUsage(h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(extra.access)), false, 3)
	refreshed := h.do(http.MethodPost, "/api/v1/auth/refresh", map[string]any{
		"refresh_token": extra.refresh,
		"device":        map[string]any{"device_id": "pro-pack-projection-extra", "platform": "android"},
	}, nil)
	finiteUsage(refreshed, true, 3)
	extra.access = refreshed.str("access_token")
	for _, authorized := range []session{owner, desktop} {
		for _, path := range []string{"/api/v1/me", "/api/v1/me/usage"} {
			res := h.do(http.MethodGet, path, nil, h.auth(authorized.access))
			mustStatus(t, res, http.StatusOK, "")
			usage := res.body
			if path == "/api/v1/me" {
				usage, _ = res.body["usage"].(map[string]any)
				if res.str("subscription", "plan", "code") != "pro_monthly" {
					t.Fatalf("authorized subscription changed: %s", res.raw)
				}
			}
			if usage["unlimited"] != true || (response{body: usage}).num("purchased_credits") != 3 {
				t.Fatalf("owner lost unlimited access or shared purchased balance: %s", res.raw)
			}
		}
	}
	if h.provider.calls != 0 || h.scalar(`SELECT units_remaining FROM credit_grants WHERE id=?`, pack.ID) != 3 {
		t.Fatal("sign-in, refresh or account reload spent a purchased answer")
	}
	res := h.generate(extra.access, "Сәлем")
	mustStatus(t, res, http.StatusOK, "")
	if res.str("usage", "bucket") != domain.BucketPack {
		t.Fatalf("extra device did not use the purchased pack: %s", res.raw)
	}
	finiteUsage(h.do(http.MethodGet, "/api/v1/me", nil, h.auth(extra.access)), true, 2)
	finiteUsage(h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(extra.access)), false, 2)
	if h.scalar(`SELECT units_remaining FROM credit_grants WHERE id=?`, pack.ID) != 2 || h.welcomeLeft(owner.userID) != 10 ||
		h.scalar(`SELECT units_remaining FROM credit_grants WHERE id=?`, admin.ID) != 7 ||
		h.scalar(`SELECT units_remaining FROM credit_grants WHERE id=?`, pass.ID) != 9 {
		t.Fatal("usage projection changed a balance outside the charged purchased pack")
	}
	for i := 0; i < 2; i++ {
		mustStatus(t, h.generate(extra.access, "Сәлем"), http.StatusOK, "")
	}
	finiteUsage(h.do(http.MethodGet, "/api/v1/me", nil, h.auth(extra.access)), true, 0)
	finiteUsage(h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(extra.access)), false, 0)
	mustStatus(t, h.generate(extra.access, "Сәлем"), http.StatusConflict, "PRO_DEVICE_LIMIT_REACHED")
	ownedUsage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(owner.access))
	mustStatus(t, ownedUsage, http.StatusOK, "")
	if ownedUsage.body["unlimited"] != true || ownedUsage.num("purchased_credits") != 0 {
		t.Fatalf("exhausted shared pack changed the owner's unlimited access: %s", ownedUsage.raw)
	}
	assertProPackFallbackSlots(t, h, owner.userID)
}
