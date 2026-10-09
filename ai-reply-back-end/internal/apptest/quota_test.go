package apptest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// Тегін тарифте күніне 7 жауап; 8-шісі қабылданбайды.
func TestFreePlanDailyLimit(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 701 555 66 77")

	for i := 1; i <= 7; i++ {
		res := h.generate(session.access, "Сәлеметсіз бе, бағасы қанша?")
		if res.status != http.StatusOK {
			t.Fatalf("generation %d failed: %d %s", i, res.status, res.raw)
		}
		if remaining := res.num("usage", "remaining_today"); int(remaining) != 7-i {
			t.Fatalf("after %d generations remaining = %v, want %d", i, remaining, 7-i)
		}
	}

	blocked := h.generate(session.access, "Тағы бір сұрақ бар")
	if blocked.status != http.StatusTooManyRequests || blocked.errorCode() != "DAILY_LIMIT_REACHED" {
		t.Fatalf("8th request: %d %s, want 429 DAILY_LIMIT_REACHED", blocked.status, blocked.errorCode())
	}
	if h.provider.calls != 7 {
		t.Fatalf("provider called %d times, want 7", h.provider.calls)
	}
}

// Параллель сұраныстар лимитті аттап өте алмайды.
func TestQuotaIsConcurrencySafe(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 701 666 77 88")

	var wg sync.WaitGroup
	results := make([]response, 25)
	for i := range results {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			results[index] = h.generate(session.access, "Параллель сұраныс")
		}(i)
	}
	wg.Wait()

	success, limited := 0, 0
	for _, res := range results {
		switch {
		case res.status == http.StatusOK:
			success++
		case res.errorCode() == "DAILY_LIMIT_REACHED":
			limited++
		default:
			t.Fatalf("unexpected status %d: %s", res.status, res.raw)
		}
	}
	if success != 7 {
		t.Fatalf("%d requests succeeded, want exactly 7", success)
	}
	if limited != len(results)-7 {
		t.Fatalf("%d requests limited, want %d", limited, len(results)-7)
	}
	if h.provider.calls != 7 {
		t.Fatalf("provider called %d times, want 7", h.provider.calls)
	}
}

// Квота келесі күні жаңарады (сервер уақыт белдеуі бойынша).
func TestQuotaResetsNextDay(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 701 777 88 99")

	for i := 0; i < 7; i++ {
		h.generate(session.access, "Бүгінгі сұраныс")
	}
	if blocked := h.generate(session.access, "Артық"); blocked.errorCode() != "DAILY_LIMIT_REACHED" {
		t.Fatalf("limit not reached: %s", blocked.raw)
	}

	h.clock.Advance(24 * time.Hour)
	session = h.refresh(session) // access токен 15 минутта ескіреді
	fresh := h.generate(session.access, "Ертеңгі сұраныс")
	if fresh.status != http.StatusOK {
		t.Fatalf("quota did not reset: %d %s", fresh.status, fresh.raw)
	}
	if used := fresh.num("usage", "used_today"); used != 1 {
		t.Fatalf("used_today = %v after reset, want 1", used)
	}
}

// Әкімші тарифті ауыстырғанда жаңа лимит бірден күшіне енеді (мобильді релизсіз).
func TestAdminPlanChangeAppliesImmediately(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 701 888 99 00")
	adminSession := h.signInAdmin()

	plans := h.do(http.MethodGet, "/api/v1/admin/plans", nil, adminSession.headers(h.cfg.Admin.CookieName))
	var standardID string
	list, _ := plans.body["plans"].([]any)
	for _, item := range list {
		plan, _ := item.(map[string]any)
		if plan["code"] == "standard" {
			standardID, _ = plan["id"].(string)
		}
	}
	if standardID == "" {
		t.Fatal("standard plan missing")
	}

	assign := h.do(http.MethodPost, "/api/v1/admin/users/"+session.userID+"/plan",
		map[string]any{"plan_id": standardID}, adminSession.headers(h.cfg.Admin.CookieName))
	if assign.status != http.StatusOK {
		t.Fatalf("assign plan: %d %s", assign.status, assign.raw)
	}

	usage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(session.access))
	if limit := usage.num("daily_limit"); limit != 30 {
		t.Fatalf("daily limit = %v after plan change, want 30", limit)
	}

	// Лимитті 50-ге өзгертсек — қолданушы бірден 50 алады.
	update := h.do(http.MethodPatch, "/api/v1/admin/plans/"+standardID, map[string]any{
		"code": "standard", "name": map[string]string{"en": "Standard", "kk": "Стандарт", "ru": "Стандарт", "uz": "Standart"},
		"description": map[string]string{}, "price": 199000, "currency": "KZT",
		"daily_message_limit": 50, "monthly_message_limit": 0, "period_days": 30,
		"is_free": false, "is_active": true, "sort_order": 20,
	}, adminSession.headers(h.cfg.Admin.CookieName))
	if update.status != http.StatusOK {
		t.Fatalf("update plan: %d %s", update.status, update.raw)
	}

	after := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(session.access))
	if limit := after.num("daily_limit"); limit != 50 {
		t.Fatalf("daily limit = %v after limit change, want 50", limit)
	}
}

// Жазылым мерзімі өткенде қолданушы тегін тарифке түседі, қызметсіз қалмайды.
func TestExpiredSubscriptionFallsBackToFreePlan(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 702 111 22 33")
	adminSession := h.signInAdmin()

	plans := h.do(http.MethodGet, "/api/v1/admin/plans", nil, adminSession.headers(h.cfg.Admin.CookieName))
	var proID string
	list, _ := plans.body["plans"].([]any)
	for _, item := range list {
		plan, _ := item.(map[string]any)
		if plan["code"] == "pro" {
			proID, _ = plan["id"].(string)
		}
	}
	expiry := h.clock.Now().Add(48 * time.Hour).Format("2006-01-02")
	assign := h.do(http.MethodPost, "/api/v1/admin/users/"+session.userID+"/plan",
		map[string]any{"plan_id": proID, "expires_at": expiry}, adminSession.headers(h.cfg.Admin.CookieName))
	if assign.status != http.StatusOK {
		t.Fatalf("assign: %d %s", assign.status, assign.raw)
	}
	if limit := h.entitlement(session.userID).DailyLimit; limit != 50 {
		t.Fatalf("pro limit = %d, want 50", limit)
	}

	h.clock.Advance(96 * time.Hour)
	if limit := h.entitlement(session.userID).DailyLimit; limit != 7 {
		t.Fatalf("after expiry limit = %d, want 7 (free plan)", limit)
	}
}

// Провайдер қате берсе — квота қайтарылады, қолданушы алмаған жауап үшін төлемейді.
func TestProviderFailureRefundsQuota(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 702 222 33 44")

	h.provider.err = domain.ErrProviderDown
	failed := h.generate(session.access, "Провайдер құлады")
	if failed.status != http.StatusBadGateway || failed.errorCode() != "AI_PROVIDER_UNAVAILABLE" {
		t.Fatalf("got %d %s, want 502 AI_PROVIDER_UNAVAILABLE", failed.status, failed.errorCode())
	}
	if used := h.entitlement(session.userID).UsedToday; used != 0 {
		t.Fatalf("used_today = %d after provider failure, want 0", used)
	}

	h.provider.err = nil
	ok := h.generate(session.access, "Енді жұмыс істейді")
	if ok.status != http.StatusOK {
		t.Fatalf("recovery request failed: %d", ok.status)
	}
	if used := h.entitlement(session.userID).UsedToday; used != 1 {
		t.Fatalf("used_today = %d, want 1", used)
	}
}

// Уақыт бітсе — бөлек код, бірақ есеп бәрібір қайтарылады.
func TestProviderTimeoutIsReported(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 702 333 44 55")
	h.provider.err = errors.Join(domain.ErrProviderTimeout)

	res := h.generate(session.access, "Күту уақыты")
	if res.status != http.StatusGatewayTimeout || res.errorCode() != "AI_TIMEOUT" {
		t.Fatalf("got %d %s, want 504 AI_TIMEOUT", res.status, res.errorCode())
	}
	if used := h.entitlement(session.userID).UsedToday; used != 0 {
		t.Fatalf("used_today = %d after timeout, want 0", used)
	}
}

// Клиент провайдер жауабын күтпей кетсе (пернетақта жабылды), бронь бәрібір қайтарылады.
func TestCancelledRequestRefundsQuota(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 702 444 55 66")
	started := h.provider.holdNext()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	raw, _ := json.Marshal(map[string]any{"source_text": "Клиент кетіп қалды", "language": "kk"})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, h.server.URL+"/api/v1/ai/reply", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+session.access)
	done := make(chan error, 1)
	go func() {
		res, err := h.server.Client().Do(req)
		if err == nil {
			res.Body.Close()
		}
		done <- err
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the request never reached the provider")
	}
	if used := h.entitlement(session.userID).UsedToday; used != 1 {
		t.Fatalf("used_today = %d while the provider works, want the reservation (1)", used)
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("the client request was not cancelled")
	}

	// The server finishes on its own after the client is gone: wait for its event.
	deadline := time.Now().Add(5 * time.Second)
	for h.scalar(`SELECT COUNT(*) FROM ai_usage_events WHERE user_id = ? AND status = 'error'`, session.userID) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the cancelled request left no usage event")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if used := h.entitlement(session.userID).UsedToday; used != 0 {
		t.Fatalf("used_today = %d after the client went away, want 0", used)
	}
}

// Айлық шегі бар тариф: /ai/reply мен /ai/compose usage блогы айлық санағышты да береді,
// ал remaining_today айдың қалғанынан аспайды.
func TestUsageReportsTheMonthlyCap(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 702 555 66 77")
	if _, err := h.db.Writer().Exec(`UPDATE plans SET monthly_message_limit = 3 WHERE code = 'standard'`); err != nil {
		t.Fatal(err)
	}
	h.assignPlan(h.signInAdmin(), session.userID, "standard", "")

	first := h.generate(session.access, "Сәлеметсіз бе, бағасы қанша?")
	if first.status != http.StatusOK {
		t.Fatalf("reply: %d %s", first.status, first.raw)
	}
	if first.num("usage", "monthly_limit") != 3 || first.num("usage", "used_month") != 1 ||
		first.num("usage", "daily_limit") != 30 || first.num("usage", "remaining_today") != 2 {
		t.Fatalf("usage = %s", first.raw)
	}
	second := h.compose(session.access, map[string]any{"instruction": "Әріптесімді құттықта", "language": "kk"})
	if second.status != http.StatusOK || second.num("usage", "used_month") != 2 || second.num("usage", "remaining_today") != 1 {
		t.Fatalf("compose usage = %d %s", second.status, second.raw)
	}
	mustStatus(t, h.generate(session.access, "Тағы бір сұрақ"), http.StatusOK, "")

	blocked := h.generate(session.access, "Айлық шек бітті")
	mustStatus(t, blocked, http.StatusTooManyRequests, "MONTHLY_LIMIT_REACHED")
	if blocked.num("error", "details", "monthly_limit") != 3 || blocked.num("error", "details", "used_month") != 3 {
		t.Fatalf("details = %s", blocked.raw)
	}
	usage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(session.access))
	if usage.num("remaining_today") != 0 || usage.num("used_today") != 3 {
		t.Fatalf("/me/usage after the monthly cap = %s", usage.raw)
	}
}
