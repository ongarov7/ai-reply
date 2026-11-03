package apptest

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/credits"
	"github.com/aireply/ai-reply-back-end/internal/domain"
)

func TestWelcomeTwentyRegistrationShowsLifetimeBalanceAndNeverRegrantsOnLogin(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withWelcomeBonus(credits.DefaultWelcomeUnits))
	first, signUp := h.signInOn("welcome-twenty-once@example.com", "ios")
	if !first.isNew || signUp.num("usage", "welcome", "total") != 20 || signUp.num("usage", "welcome", "remaining") != 20 ||
		signUp.num("usage", "daily_limit") != 0 || signUp.num("usage", "remaining_today") != 0 || signUp.num("usage", "remaining_total") != 20 {
		t.Fatalf("new Free registration must expose twenty lifetime replies: %s", signUp.raw)
	}
	firstReply := h.generate(first.access, "Сәлем")
	mustStatus(t, firstReply, http.StatusOK, "")
	if firstReply.str("usage", "bucket") != domain.BucketWelcome || firstReply.num("usage", "welcome", "remaining") != 19 {
		t.Fatalf("first reply: %s", firstReply.raw)
	}
	refreshed := h.refresh(first)
	mustStatus(t, h.do(http.MethodPost, "/api/v1/auth/logout", map[string]any{"refresh_token": refreshed.refresh}, nil), http.StatusOK, "")
	h.clock.Advance(time.Minute)
	again, signIn := h.signInOn("welcome-twenty-once@example.com", "android")
	if again.isNew || again.userID != first.userID || signIn.num("usage", "welcome", "total") != 20 ||
		signIn.num("usage", "welcome", "remaining") != 19 || signIn.num("usage", "remaining_total") != 19 {
		t.Fatalf("login changed the one-time grant: %s", signIn.raw)
	}
	for i := 0; i < 3; i++ {
		me := h.do(http.MethodGet, "/api/v1/me", nil, h.auth(again.access))
		mustStatus(t, me, http.StatusOK, "")
		if me.num("usage", "welcome", "remaining") != 19 {
			t.Fatalf("account read replenished the grant: %s", me.raw)
		}
	}
	if n := h.scalar(`SELECT COUNT(*) FROM credit_grants WHERE user_id=? AND kind='welcome'`, first.userID); n != 1 {
		t.Fatalf("welcome grants = %d, want one", n)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM credit_ledger WHERE user_id=? AND kind='welcome' AND entry='grant' AND delta=20`, first.userID); n != 1 {
		t.Fatalf("welcome grant ledger entries = %d, want one", n)
	}
}

func TestWelcomeTwentyDoesNotResetDailyAndIdempotentRetryDoesNotChargeTwice(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withWelcomeBonus(credits.DefaultWelcomeUnits))
	s := h.signIn("welcome-twenty-lifetime@example.com")
	first := h.generateWithKey(s.access, "Сәлем", "welcome-twenty-idempotent")
	mustStatus(t, first, http.StatusOK, "")
	mustStatus(t, h.generateWithKey(s.access, "Сәлем", "welcome-twenty-idempotent"), http.StatusOK, "")
	if got := h.welcomeLeft(s.userID); got != 19 {
		t.Fatalf("retry charged twice: %d left", got)
	}
	h.clock.Advance(24 * time.Hour)
	s = h.refresh(s)
	usage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(s.access))
	if usage.num("daily_limit") != 0 || usage.num("welcome", "remaining") != 19 || usage.num("remaining_total") != 19 {
		t.Fatalf("next day renewed Free: %s", usage.raw)
	}
	for i := 0; i < 19; i++ {
		mustStatus(t, h.generate(s.access, "Сәлем"), http.StatusOK, "")
	}
	mustStatus(t, h.generate(s.access, "Артық"), http.StatusTooManyRequests, "DAILY_LIMIT_REACHED")
	h.clock.Advance(24 * time.Hour)
	s = h.refresh(s)
	mustStatus(t, h.generate(s.access, "Келесі күні"), http.StatusTooManyRequests, "DAILY_LIMIT_REACHED")
	if got := h.welcomeLeft(s.userID); got != 0 {
		t.Fatalf("spent grant replenished: %d", got)
	}
}

func TestWelcomeTwentyPreservesAnExistingSpentTenReplyGrant(t *testing.T) {
	// A pre-change account already received and spent its historical ten replies.
	h := newHarness(t, approvedEntitlements(), withWelcomeBonus(10))
	old := h.signIn("welcome-twenty-historical@example.com")
	h.exec(`UPDATE credit_grants SET units_remaining=0 WHERE user_id=? AND kind='welcome'`, old.userID)
	if _, _, err := h.credits.SetWelcomeUnits(context.Background(), credits.DefaultWelcomeUnits); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		me := h.do(http.MethodGet, "/api/v1/me", nil, h.auth(old.access))
		mustStatus(t, me, http.StatusOK, "")
		if me.num("usage", "welcome", "total") != 10 || me.num("usage", "welcome", "remaining") != 0 || me.num("usage", "remaining_total") != 0 {
			t.Fatalf("historical grant changed: %s", me.raw)
		}
	}
	h.clock.Advance(time.Minute)
	again, _ := h.signInOn("welcome-twenty-historical@example.com", "macos")
	mustStatus(t, h.generate(again.access, "Артық"), http.StatusTooManyRequests, "DAILY_LIMIT_REACHED")
	if h.scalar(`SELECT COUNT(*) FROM credit_grants WHERE user_id=? AND kind='welcome'`, old.userID) != 1 {
		t.Fatal("login created a second welcome grant")
	}
	fresh, signUp := h.signInOn("welcome-twenty-fresh@example.com", "ios")
	if fresh.userID == old.userID || signUp.num("usage", "welcome", "total") != 20 || signUp.num("usage", "welcome", "remaining") != 20 {
		t.Fatalf("new registration did not use the new allowance: %s", signUp.raw)
	}
}

func TestWelcomeTwentyRepairsAnEligibleMissingGrantOnlyOnce(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withWelcomeBonus(credits.DefaultWelcomeUnits))
	h.exec(`CREATE TRIGGER fail_welcome_twenty BEFORE INSERT ON credit_grants BEGIN SELECT RAISE(ABORT, 'test storage failure'); END`)
	s := h.signIn("welcome-twenty-repair@example.com")
	if h.welcomeLeft(s.userID) != -1 {
		t.Fatal("fixture did not prevent the registration grant")
	}
	h.exec(`DROP TRIGGER fail_welcome_twenty`)
	for i := 0; i < 3; i++ {
		me := h.do(http.MethodGet, "/api/v1/me", nil, h.auth(s.access))
		mustStatus(t, me, http.StatusOK, "")
		if me.num("usage", "welcome", "total") != 20 || me.num("usage", "welcome", "remaining") != 20 {
			t.Fatalf("eligible missing grant was not repaired: %s", me.raw)
		}
	}
	if h.scalar(`SELECT COUNT(*) FROM credit_grants WHERE user_id=? AND kind='welcome'`, s.userID) != 1 ||
		h.scalar(`SELECT COUNT(*) FROM credit_ledger WHERE user_id=? AND kind='welcome' AND entry='grant' AND delta=20`, s.userID) != 1 {
		t.Fatal("repair duplicated a welcome grant or its ledger entry")
	}
}

func TestWelcomeTwentyPreservesPackagesBaseAndUnlimitedPro(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withWelcomeBonus(credits.DefaultWelcomeUnits))
	s := h.signIn("welcome-twenty-paid@example.com")
	h.addGrant(s.userID, domain.GrantPack, 30, h.clock.Now(), nil, h.clock.Now())
	free := h.generate(s.access, "Сәлем")
	mustStatus(t, free, http.StatusOK, "")
	if free.str("usage", "bucket") != domain.BucketWelcome || free.num("usage", "purchased_credits") != 30 || free.num("usage", "welcome", "remaining") != 19 {
		t.Fatalf("Free consumed a purchased message: %s", free.raw)
	}
	admin := h.signInAdmin()
	h.assignPlan(admin, s.userID, "base_monthly", "")
	base := h.generate(s.access, "Сәлем")
	mustStatus(t, base, http.StatusOK, "")
	if base.num("usage", "daily_limit") != 40 || base.str("usage", "bucket") != domain.BucketDaily ||
		base.num("usage", "purchased_credits") != 30 || base.num("usage", "welcome", "remaining") != 19 {
		t.Fatalf("Base allowance or balances changed: %s", base.raw)
	}
	h.assignPlan(admin, s.userID, "pro_monthly", "")
	pro := h.generate(s.access, "Сәлем")
	mustStatus(t, pro, http.StatusOK, "")
	usage, _ := pro.body["usage"].(map[string]any)
	if usage["unlimited"] != true || pro.str("usage", "bucket") != domain.BucketUnlimited ||
		pro.num("usage", "purchased_credits") != 30 || pro.num("usage", "welcome", "remaining") != 19 {
		t.Fatalf("Pro lost unlimited access or consumed stored balances: %s", pro.raw)
	}
	for code, price := range map[string]int64{"base_monthly": 79000, "base_yearly": 790000, "pro_monthly": 345000, "pro_yearly": 3490000} {
		plan, err := h.store.PlanByCode(context.Background(), code)
		if err != nil || plan.Price != price {
			t.Fatalf("%s price changed: %+v %v", code, plan, err)
		}
	}
	if got := h.scalar(`SELECT price_minor FROM iap_products WHERE code='pack30'`); got != 13000 {
		t.Fatalf("purchased package price changed: %d", got)
	}
}
