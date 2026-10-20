package apptest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/repository"
)

// generateWithKey — /ai/reply бір әрекеттің Idempotency-Key кілтімен ("" — кілтсіз).
func (h *harness) generateWithKey(token, text, key string) response {
	headers := h.auth(token)
	if key != "" {
		headers["Idempotency-Key"] = key
	}
	return h.do(http.MethodPost, "/api/v1/ai/reply", map[string]any{
		"source_text": text, "language": "kk", "template_id": "client", "platform": "ios", "app_version": "1.0.0",
	}, headers)
}

// signInOn — демо OTP арқылы берілген платформадан кіреді; сессия жауабын да қайтарады.
func (h *harness) signInOn(identifier, platform string) (session, response) {
	h.t.Helper()
	mustStatus(h.t, h.do(http.MethodPost, "/api/v1/auth/request-otp",
		map[string]any{"identifier": identifier, "locale": "kk"}, nil), http.StatusOK, "")
	verify := h.do(http.MethodPost, "/api/v1/auth/verify-otp", map[string]any{
		"identifier": identifier, "code": "1111",
		"device": map[string]any{"platform": platform, "app_version": "1.0.0", "locale": "kk"},
	}, nil)
	mustStatus(h.t, verify, http.StatusOK, "")
	s := session{access: verify.str("access_token"), refresh: verify.str("refresh_token"),
		userID: verify.str("user", "id")}
	s.isNew, _ = verify.body["is_new_user"].(bool)
	h.consent(s.access)
	return s, verify
}

// addGrant — пакет не pass (сатып алу кейін келеді; ол да осы жолмен жазылады).
func (h *harness) addGrant(userID, kind string, units int, starts time.Time, expires *time.Time, at time.Time) domain.CreditGrant {
	h.t.Helper()
	g, err := h.store.InsertCreditGrant(context.Background(), domain.CreditGrant{
		UserID: userID, Kind: kind, UnitsTotal: units, UnitsRemaining: units, StartsAt: starts, ExpiresAt: expires,
		ProductCode: kind + "_test",
	}, at)
	if err != nil {
		h.t.Fatalf("grant %s: %v", kind, err)
	}
	return g
}

func (h *harness) exec(query string, args ...any) {
	h.t.Helper()
	if _, err := h.db.Writer().Exec(query, args...); err != nil {
		h.t.Fatalf("%s: %v", query, err)
	}
}

// welcomeLeft — бонустың қалдығы дерекқордан.
func (h *harness) welcomeLeft(userID string) int {
	h.t.Helper()
	return h.scalar(`SELECT COALESCE(SUM(units_remaining), -1) FROM credit_grants WHERE user_id = ? AND kind = 'welcome'`, userID)
}

func (h *harness) todayKeys() (string, string) {
	local := h.clock.Now().In(h.cfg.App.Location())
	return local.Format("2006-01-02"), local.Format("2006-01")
}

// Жаңа тіркелгі бонусты бір рет алады: қайта кіру, басқа платформа және сессия жауабы жаңасын бермейді.
func TestNewAccountGetsTheWelcomeBonusOnce(t *testing.T) {
	h := newHarness(t, withWelcomeBonus(50))
	for _, identifier := range []string{"welcome-once@example.com", "+7 701 300 40 50"} {
		first, signUp := h.signInOn(identifier, "ios")
		if !first.isNew {
			t.Fatalf("%s: not a new account", identifier)
		}
		if signUp.num("usage", "welcome", "total") != 50 || signUp.num("usage", "welcome", "remaining") != 50 {
			t.Fatalf("%s: session usage = %s", identifier, signUp.raw)
		}
		me := h.do(http.MethodGet, "/api/v1/me", nil, h.auth(first.access))
		usage, _ := me.body["usage"].(map[string]any)
		if me.num("usage", "remaining_today") != 7 || me.num("usage", "credits") != 0 ||
			me.num("usage", "remaining_total") != 57 || usage["pass"] != nil || usage["bucket"] != nil {
			t.Fatalf("%s: /me usage = %s", identifier, me.raw)
		}

		// Codes for one address are at least 32 s apart.
		h.clock.Advance(time.Minute)
		again, _ := h.signInOn(identifier, "android")
		h.clock.Advance(time.Minute)
		h.signInOn(identifier, "ios")
		if again.isNew || again.userID != first.userID {
			t.Fatalf("%s: second sign-in made another account", identifier)
		}
		if n := h.scalar(`SELECT COUNT(*) FROM credit_grants WHERE user_id = ?`, first.userID); n != 1 {
			t.Fatalf("%s: %d grants, want 1", identifier, n)
		}
		if n := h.scalar(`SELECT COUNT(*) FROM credit_ledger WHERE user_id = ? AND entry = 'grant' AND delta = 50`, first.userID); n != 1 {
			t.Fatalf("%s: %d grant entries, want 1", identifier, n)
		}
		if usage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(again.access)); usage.num("welcome", "total") != 50 {
			t.Fatalf("%s: /me/usage = %s", identifier, usage.raw)
		}
	}

	config := h.do(http.MethodGet, "/api/v1/config", nil, nil)
	for _, flag := range []string{"welcome_bonus", "credits", "idempotent_generation"} {
		if on, _ := config.body["features"].(map[string]any)[flag].(bool); !on {
			t.Fatalf("features.%s is off: %s", flag, config.raw)
		}
	}
}

// Тіркелу кезінде бонус жазылмай қалса, /me оны қалпына келтіреді; кіру бәрібір сәтті.
func TestAMissedWelcomeBonusIsRepairedOnMe(t *testing.T) {
	h := newHarness(t, withWelcomeBonus(50))
	h.exec(`CREATE TRIGGER fail_welcome BEFORE INSERT ON credit_grants BEGIN SELECT RAISE(ABORT, 'disk full'); END`)
	s := h.signIn("welcome-repair@example.com")
	if h.welcomeLeft(s.userID) != -1 || !strings.Contains(h.logs.String(), "welcome bonus at sign-up failed") {
		t.Fatal("the failing write did not stop the bonus — the test would prove nothing")
	}
	h.exec(`DROP TRIGGER fail_welcome`)

	me := h.do(http.MethodGet, "/api/v1/me", nil, h.auth(s.access))
	if me.status != http.StatusOK || me.num("usage", "welcome", "remaining") != 50 || h.welcomeLeft(s.userID) != 50 {
		t.Fatalf("/me after the failure: %s", me.raw)
	}
	h.do(http.MethodGet, "/api/v1/me", nil, h.auth(s.access))
	h.refresh(s)
	if n := h.scalar(`SELECT COUNT(*) FROM credit_grants WHERE user_id = ?`, s.userID); n != 1 {
		t.Fatalf("%d grants after repeated repairs, want 1", n)
	}
}

// Бонус тек жаңа кәдімгі тіркелгіге: бұрынғы тіркелгі, ескі install-token және симулятор алмайды.
func TestWelcomeBonusSkipsExistingLegacyAndSimulatorAccounts(t *testing.T) {
	h := newHarness(t, withWelcomeBonus(50))
	since, err := strconv.ParseInt(h.text(`SELECT value FROM system_settings WHERE key = 'welcome_bonus_since'`), 10, 64)
	if err != nil || since <= 0 {
		t.Fatalf("welcome_bonus_since = %d (%v)", since, err)
	}

	// An account from before the release signs in again, and opens its profile.
	h.exec(`INSERT INTO users (id, phone, status, created_at, updated_at) VALUES ('u-before', '+77015550101', 'active', ?, ?)`,
		since-60_000, since-60_000)
	h.exec(`INSERT INTO user_profiles (user_id, updated_at) VALUES ('u-before', ?)`, since-60_000)
	old := h.signIn("+7 701 555 01 01")
	if old.isNew || old.userID != "u-before" {
		t.Fatalf("existing account: %+v", old)
	}
	me := h.do(http.MethodGet, "/api/v1/me", nil, h.auth(old.access))
	if usage, _ := me.body["usage"].(map[string]any); usage["welcome"] != nil || me.num("usage", "remaining_total") != 7 {
		t.Fatalf("existing account usage: %s", me.raw)
	}

	// The old install-token builds.
	register := h.do(http.MethodPost, "/v1/auth/register", map[string]any{"install_id": "5C4D3B2A-1111-2222-3333-444455550001"}, nil)
	mustStatus(t, register, http.StatusOK, "")
	mustStatus(t, h.do(http.MethodPost, "/v1/reply/generate", map[string]any{"message": "Сәлем"},
		map[string]string{"Authorization": "Bearer " + register.str("token")}), http.StatusOK, "")

	// The simulator's own account.
	simulator := h.signInSimulator()
	mustStatus(t, h.do(http.MethodPost, "/api/v1/simulator/generate", map[string]any{"source_text": "Сәлем"},
		simulator.headers(h.cfg.Admin.CookieName)), http.StatusOK, "")
	if h.scalar(`SELECT COUNT(*) FROM users WHERE kind = 'legacy_install'`) != 1 || h.scalar(`SELECT COUNT(*) FROM users WHERE kind = 'simulator'`) != 1 {
		t.Fatal("the legacy and simulator accounts were not created — the test would prove nothing")
	}
	for _, kind := range []string{"legacy_install", "simulator"} {
		user, err := h.store.UserByID(context.Background(), h.text(`SELECT id FROM users WHERE kind = ?`, kind))
		if err != nil {
			t.Fatal(err)
		}
		if granted, err := h.credits.GrantWelcome(context.Background(), user); err != nil || granted {
			t.Fatalf("%s: granted=%v err=%v", kind, granted, err)
		}
	}
	if n := h.scalar(`SELECT COUNT(*) FROM credit_grants`); n != 0 {
		t.Fatalf("%d grants for accounts that may not have one", n)
	}

	// Switched off: a new account gets nothing, and the apps are told.
	h.exec(`UPDATE system_settings SET value = '0' WHERE key = 'welcome_bonus_units'`)
	off := h.signIn("welcome-off@example.com")
	if !off.isNew || h.welcomeLeft(off.userID) != -1 {
		t.Fatal("a bonus was given while it is off")
	}
	if on, _ := h.do(http.MethodGet, "/api/v1/config", nil, nil).body["features"].(map[string]any)["welcome_bonus"].(bool); on {
		t.Fatal("features.welcome_bonus is on while the bonus is off")
	}
}

// Реті: тарифтің күндігі → pass → бонус → әкімші → пакет (ескісі бірінші). Айлық шек жоқ, used тек тарифті санайды.
func TestCreditsAreSpentInOrder(t *testing.T) {
	h := newHarness(t, withWelcomeBonus(2))
	s := h.signIn("spend-order@example.com")
	now := h.clock.Now()
	h.addGrant(s.userID, domain.GrantAdmin, 2, now, nil, now)
	older := h.addGrant(s.userID, domain.GrantPack, 1, now, nil, now.Add(-2*time.Hour))
	newer := h.addGrant(s.userID, domain.GrantPack, 1, now, nil, now.Add(-time.Hour))
	end := now.Add(23 * time.Hour)
	h.addGrant(s.userID, domain.GrantPass, 2, now.Add(-time.Hour), &end, now)
	// A pass waiting behind the running one is not spent while that one has
	// units, and starts (keeping its 25 hours) once it is used up; one that is
	// over is never spent.
	later, past := now.Add(48*time.Hour), now.Add(-time.Minute)
	waiting := h.addGrant(s.userID, domain.GrantPass, 5, end, &later, now)
	h.addGrant(s.userID, domain.GrantPass, 5, now.Add(-25*time.Hour), &past, now)

	want := []string{"daily", "daily", "daily", "daily", "daily", "daily", "daily",
		"pass", "pass", "pass", "pass", "pass", "pass", "pass", "welcome", "welcome", "admin", "admin", "pack", "pack"}
	for i, bucket := range want {
		res := h.generate(s.access, "Сәлеметсіз бе, бағасы қанша?")
		if res.status != http.StatusOK || res.str("usage", "bucket") != bucket {
			t.Fatalf("generation %d: %d bucket %q, want %q (%s)", i+1, res.status, res.str("usage", "bucket"), bucket, res.raw)
		}
		left := len(want) - i - 1
		if i < 8 {
			left -= 5 // the waiting pass counts once it has started
		}
		if res.num("usage", "remaining_total") != float64(left) {
			t.Fatalf("generation %d: remaining_total = %v, want %d", i+1, res.num("usage", "remaining_total"), left)
		}
		if i >= 7 && (res.num("usage", "used_today") != 7 || res.num("usage", "remaining_today") != 0) {
			t.Fatalf("generation %d: extra buckets moved the plan counters: %s", i+1, res.raw)
		}
		if starts := h.scalar(`SELECT starts_at FROM credit_grants WHERE id = ?`, waiting.ID); (i == 7 && starts != int(end.UnixMilli())) ||
			(i == 8 && (starts != int(now.UnixMilli()) ||
				h.scalar(`SELECT expires_at FROM credit_grants WHERE id = ?`, waiting.ID) != int(now.Add(25*time.Hour).UnixMilli()))) {
			t.Fatalf("generation %d: the waiting pass starts at %d", i+1, starts)
		}
		if i == 18 && (h.scalar(`SELECT units_remaining FROM credit_grants WHERE id = ?`, older.ID) != 0 ||
			h.scalar(`SELECT units_remaining FROM credit_grants WHERE id = ?`, newer.ID) != 1) {
			t.Fatal("the newer pack was spent before the older one")
		}
	}

	blocked := h.generate(s.access, "Тағы бір сұрақ")
	mustStatus(t, blocked, http.StatusTooManyRequests, "DAILY_LIMIT_REACHED")
	details, _ := blocked.body["error"].(map[string]any)["details"].(map[string]any)
	if details["welcome_remaining"] != float64(0) || details["credits_remaining"] != float64(0) ||
		details["pass_active"] != true || details["can_buy"] != false || details["daily_limit"] != float64(7) {
		t.Fatalf("limit details = %v", details)
	}
	if h.provider.calls != len(want) {
		t.Fatalf("provider called %d times, want %d", h.provider.calls, len(want))
	}

	usage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(s.access))
	if usage.num("used_today") != 7 || usage.num("remaining_total") != 0 || usage.num("welcome", "total") != 2 ||
		usage.num("welcome", "remaining") != 0 || usage.num("pass", "remaining") != 0 ||
		!strings.HasSuffix(usage.str("pass", "expires_at"), "+05:00") {
		t.Fatalf("/me/usage = %s", usage.raw)
	}
	date, month := h.todayKeys()
	if h.scalar(`SELECT extra_used FROM usage_daily WHERE user_id = ? AND usage_date = ?`, s.userID, date) != 13 ||
		h.scalar(`SELECT used FROM usage_monthly WHERE user_id = ? AND usage_month = ?`, s.userID, month) != 7 ||
		h.scalar(`SELECT extra_used FROM usage_monthly WHERE user_id = ? AND usage_month = ?`, s.userID, month) != 13 {
		t.Fatal("extra_used does not count the extra buckets")
	}
	if n := h.scalar(`SELECT COUNT(*) FROM credit_ledger WHERE user_id = ? AND entry = 'consume' AND delta = -1`, s.userID); n != 13 {
		t.Fatalf("%d consume entries, want 13", n)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM credit_grants WHERE user_id = ? AND units_remaining = 5`, s.userID); n != 1 {
		t.Fatal("a pass that is over was spent")
	}
}

// 25 параллель сұраныс, тариф 7 + бонус 3: дәл 10 жауап, теріс қалдық жоқ.
func TestCreditsAreConcurrencySafe(t *testing.T) {
	h := newHarness(t, withWelcomeBonus(3))
	s := h.signIn("+7 701 666 77 01")

	var wg sync.WaitGroup
	results := make([]response, 25)
	for i := range results {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			results[index] = h.generate(s.access, "Параллель сұраныс")
		}(i)
	}
	wg.Wait()

	success, limited, buckets := 0, 0, map[string]int{}
	for _, res := range results {
		switch {
		case res.status == http.StatusOK:
			success++
			buckets[res.str("usage", "bucket")]++
		case res.errorCode() == "DAILY_LIMIT_REACHED":
			limited++
		default:
			t.Fatalf("unexpected status %d: %s", res.status, res.raw)
		}
	}
	if success != 10 || limited != 15 || buckets["daily"] != 7 || buckets["welcome"] != 3 {
		t.Fatalf("success %d, limited %d, buckets %v; want 10, 15, daily 7 + welcome 3", success, limited, buckets)
	}
	if h.provider.calls != 10 {
		t.Fatalf("provider called %d times, want 10", h.provider.calls)
	}
	date, _ := h.todayKeys()
	if left := h.welcomeLeft(s.userID); left != 0 {
		t.Fatalf("welcome remaining = %d, want 0", left)
	}
	if h.scalar(`SELECT used FROM usage_daily WHERE user_id = ? AND usage_date = ?`, s.userID, date) != 7 ||
		h.scalar(`SELECT extra_used FROM usage_daily WHERE user_id = ? AND usage_date = ?`, s.userID, date) != 3 ||
		h.scalar(`SELECT COUNT(*) FROM usage_reservations WHERE user_id = ? AND status = 'committed'`, s.userID) != 10 ||
		h.scalar(`SELECT COUNT(*) FROM credit_ledger WHERE user_id = ? AND entry = 'consume'`, s.userID) != 3 {
		t.Fatal("counters disagree with the generations")
	}
}

// Провайдер қатесі мен кеткен клиент: бірлік дәл сол бөлікке қайтады.
func TestFailedGenerationsRefundTheWelcomeBonus(t *testing.T) {
	h := newHarness(t, withWelcomeBonus(5))
	s := h.signIn("+7 702 222 33 01")
	h.useQuota(s.userID, 7)
	date, _ := h.todayKeys()

	h.provider.err = domain.ErrProviderDown
	mustStatus(t, h.generate(s.access, "Провайдер құлады"), http.StatusBadGateway, "AI_PROVIDER_UNAVAILABLE")
	if left := h.welcomeLeft(s.userID); left != 5 {
		t.Fatalf("welcome remaining = %d after a provider failure, want 5", left)
	}
	if h.scalar(`SELECT COUNT(*) FROM credit_ledger WHERE user_id = ? AND entry = 'refund' AND delta = 1`, s.userID) != 1 ||
		h.scalar(`SELECT extra_used FROM usage_daily WHERE user_id = ? AND usage_date = ?`, s.userID, date) != 0 {
		t.Fatal("the refund is not in the ledger or the counter")
	}
	h.provider.err = nil
	ok := h.generate(s.access, "Енді жұмыс істейді")
	if ok.status != http.StatusOK || ok.str("usage", "bucket") != "welcome" || ok.num("usage", "welcome", "remaining") != 4 {
		t.Fatalf("after recovery: %s", ok.raw)
	}

	// The keyboard closes while the provider is still writing.
	started := h.provider.holdNext()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	raw, _ := json.Marshal(map[string]any{"source_text": "Клиент кетіп қалды", "language": "kk"})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, h.server.URL+"/api/v1/ai/reply", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.access)
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
	if left := h.welcomeLeft(s.userID); left != 3 {
		t.Fatalf("welcome remaining = %d while the provider works, want the reservation (3)", left)
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("the client request was not cancelled")
	}
	deadline := time.Now().Add(5 * time.Second)
	for h.scalar(`SELECT COUNT(*) FROM usage_reservations WHERE user_id = ? AND status = 'refunded'`, s.userID) < 2 {
		if time.Now().After(deadline) {
			t.Fatal("the cancelled request was never refunded")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if left := h.welcomeLeft(s.userID); left != 4 {
		t.Fatalf("welcome remaining = %d after the client went away, want 4", left)
	}
}

// Бір кілт — бір төлем: қайталау тегін (10 минут ішінде бір рет), кейін 409 used.
func TestIdempotencyKeyNeverChargesTwice(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("+7 702 777 00 01")

	first := h.generateWithKey(s.access, "Сәлем, бағасы қанша?", "reply-action-0001")
	if first.status != http.StatusOK || first.num("usage", "used_today") != 1 || first.str("usage", "bucket") != "daily" {
		t.Fatalf("first: %s", first.raw)
	}
	replay := h.generateWithKey(s.access, "Сәлем, бағасы қанша?", "reply-action-0001")
	if replay.status != http.StatusOK || replay.num("usage", "used_today") != 1 || replay.str("usage", "bucket") != "daily" ||
		replay.str("reply") == "" {
		t.Fatalf("replay: %s", replay.raw)
	}
	if h.provider.calls != 2 {
		t.Fatalf("provider called %d times, want 2 (the replay generates again)", h.provider.calls)
	}
	used := h.generateWithKey(s.access, "Сәлем, бағасы қанша?", "reply-action-0001")
	mustStatus(t, used, http.StatusConflict, "CONFLICT")
	if used.str("error", "details", "field") != "idempotency_key" || used.str("error", "details", "reason") != "used" {
		t.Fatalf("third: %s", used.raw)
	}
	// The same key on another action is not a free generation either.
	other := h.do(http.MethodPost, "/api/v1/ai/compose", map[string]any{"instruction": "Әріптесімді құттықта", "language": "kk"},
		withKey(h.auth(s.access), "reply-action-0001"))
	mustStatus(t, other, http.StatusConflict, "CONFLICT")
	if h.provider.calls != 2 {
		t.Fatalf("provider called %d times after refused replays, want 2", h.provider.calls)
	}

	// Another key is another action; no key is charged every time, as before.
	if res := h.generateWithKey(s.access, "Тағы бір", "reply-action-0002"); res.num("usage", "used_today") != 2 {
		t.Fatalf("new key: %s", res.raw)
	}
	h.generateWithKey(s.access, "Кілтсіз", "")
	if res := h.generateWithKey(s.access, "Кілтсіз", ""); res.num("usage", "used_today") != 4 {
		t.Fatalf("without a key: %s", res.raw)
	}

	bad := h.generateWithKey(s.access, "Сәлем", "short")
	mustStatus(t, bad, http.StatusBadRequest, "INVALID_REQUEST")
	if bad.str("error", "details", "field") != "idempotency_key" || bad.str("error", "details", "reason") != "format" {
		t.Fatalf("bad key: %s", bad.raw)
	}

	// After ten minutes a replay is refused.
	mustStatus(t, h.generateWithKey(s.access, "Кеш", "reply-action-0003"), http.StatusOK, "")
	h.clock.Advance(11 * time.Minute)
	late := h.generateWithKey(s.access, "Кеш", "reply-action-0003")
	mustStatus(t, late, http.StatusConflict, "CONFLICT")
	if late.str("error", "details", "reason") != "used" {
		t.Fatalf("late replay: %s", late.raw)
	}
	if usage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(s.access)); usage.num("used_today") != 5 {
		t.Fatalf("used_today = %v, want 5", usage.num("used_today"))
	}
	// Nothing of the generated text is kept to serve a replay.
	if h.dbContains(h.provider.reply) {
		t.Fatal("the generated text is in the database")
	}
}

// Кілт жұмыс істеп тұрғанда — 409 in_progress; бас тартылған кілт қайта төлей алады.
func TestIdempotencyKeyInFlightAndAfterARefund(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("+7 702 777 00 02")
	const key = "compose-action-01"
	started := h.provider.holdNext()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	raw, _ := json.Marshal(map[string]any{"instruction": "Әріптесімді құттықта", "language": "kk", "platform": "ios"})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, h.server.URL+"/api/v1/ai/compose", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.access)
	req.Header.Set("Idempotency-Key", key)
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

	inFlight := h.do(http.MethodPost, "/api/v1/ai/compose", map[string]any{"instruction": "Әріптесімді құттықта",
		"language": "kk"}, withKey(h.auth(s.access), key))
	mustStatus(t, inFlight, http.StatusConflict, "CONFLICT")
	if inFlight.str("error", "details", "field") != "idempotency_key" || inFlight.str("error", "details", "reason") != "in_progress" {
		t.Fatalf("duplicate: %s", inFlight.raw)
	}

	cancel()
	<-done
	deadline := time.Now().Add(5 * time.Second)
	for h.text(`SELECT status FROM usage_reservations WHERE user_id = ? AND idempotency_key = ?`, s.userID, key) != "refunded" {
		if time.Now().After(deadline) {
			t.Fatal("the cancelled request was never refunded")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if used := h.entitlement(s.userID).UsedToday; used != 0 {
		t.Fatalf("used_today = %d after the refund, want 0", used)
	}
	// The refunded key pays again, once.
	again := h.do(http.MethodPost, "/api/v1/ai/compose", map[string]any{"instruction": "Әріптесімді құттықта",
		"language": "kk"}, withKey(h.auth(s.access), key))
	if again.status != http.StatusOK || again.num("usage", "used_today") != 1 {
		t.Fatalf("after the refund: %s", again.raw)
	}
	replay := h.do(http.MethodPost, "/api/v1/ai/compose", map[string]any{"instruction": "Әріптесімді құттықта",
		"language": "kk"}, withKey(h.auth(s.access), key))
	if replay.status != http.StatusOK || replay.num("usage", "used_today") != 1 {
		t.Fatalf("replay: %s", replay.raw)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM usage_reservations WHERE user_id = ? AND idempotency_key = ?`, s.userID, key); n != 1 {
		t.Fatalf("%d reservations for the key, want 1", n)
	}
}

// Құлаған процесс қалдырған брон 5 минуттан кейін бір рет қайтарылады.
func TestStaleReservationsAreRefundedOnce(t *testing.T) {
	h := newHarness(t, withWelcomeBonus(5))
	ctx := context.Background()
	s := h.signIn("stale-reservation@example.com")
	h.useQuota(s.userID, 7)
	date, month := h.todayKeys()
	reserve := func() repository.Reservation {
		t.Helper()
		res, err := h.store.ReserveGeneration(ctx, repository.ReserveInput{UserID: s.userID, Mode: "reply",
			Date: date, Month: month, DailyLimit: 7, Now: h.clock.Now()})
		if err != nil || res.Bucket != domain.BucketWelcome {
			t.Fatalf("reserve: %+v %v", res, err)
		}
		return res
	}
	// A process died between the reservation and the provider's answer, twice.
	first := reserve()
	h.clock.Advance(2 * time.Minute)
	reserve()
	if left := h.welcomeLeft(s.userID); left != 3 {
		t.Fatalf("welcome remaining = %d, want 3", left)
	}
	if n, err := h.credits.RefundStale(ctx); err != nil || n != 0 {
		t.Fatalf("young reservations refunded: %d %v", n, err)
	}
	h.clock.Advance(4 * time.Minute)
	if n, err := h.credits.RefundStale(ctx); err != nil || n != 1 || h.welcomeLeft(s.userID) != 4 {
		t.Fatalf("first sweep: %d %v, welcome %d", n, err, h.welcomeLeft(s.userID))
	}
	if n, _ := h.credits.RefundStale(ctx); n != 0 {
		t.Fatalf("the same reservation was refunded again (%d)", n)
	}
	h.clock.Advance(2 * time.Minute)
	if n, _ := h.credits.RefundStale(ctx); n != 1 || h.welcomeLeft(s.userID) != 5 {
		t.Fatalf("second sweep: %d, welcome %d", n, h.welcomeLeft(s.userID))
	}
	if ok, err := h.store.CommitGeneration(ctx, first.ID, h.clock.Now()); err != nil || ok {
		t.Fatalf("a refunded reservation was committed: %v %v", ok, err)
	}
	if ok, err := h.store.RefundGeneration(ctx, first.ID, h.clock.Now()); err != nil || ok {
		t.Fatalf("a refunded reservation was refunded twice: %v %v", ok, err)
	}
	if h.scalar(`SELECT COUNT(*) FROM credit_ledger WHERE user_id = ? AND entry = 'refund'`, s.userID) != 2 ||
		h.scalar(`SELECT extra_used FROM usage_daily WHERE user_id = ? AND usage_date = ?`, s.userID, date) != 0 {
		t.Fatal("refunds are not in the ledger or the counter")
	}
}

// Тарифтің айлық шегі кредиттерді жаппайды және оларды санамайды.
func TestMonthlyCapDoesNotBlockCredits(t *testing.T) {
	h := newHarness(t, withWelcomeBonus(2))
	s := h.signIn("+7 702 555 66 01")
	h.exec(`UPDATE plans SET monthly_message_limit = 3 WHERE code = 'standard'`)
	h.assignPlan(h.signInAdmin(), s.userID, "standard", "")

	for i, bucket := range []string{"daily", "daily", "daily", "welcome", "welcome"} {
		res := h.generate(s.access, "Сәлеметсіз бе")
		if res.status != http.StatusOK || res.str("usage", "bucket") != bucket {
			t.Fatalf("generation %d: %d %s", i+1, res.status, res.raw)
		}
		if i >= 3 && (res.num("usage", "used_month") != 3 || res.num("usage", "remaining_today") != 0) {
			t.Fatalf("generation %d moved the plan counters: %s", i+1, res.raw)
		}
	}
	blocked := h.generate(s.access, "Айлық шек бітті")
	mustStatus(t, blocked, http.StatusTooManyRequests, "MONTHLY_LIMIT_REACHED")
	if blocked.num("error", "details", "monthly_limit") != 3 || blocked.num("error", "details", "used_month") != 3 ||
		blocked.body["error"].(map[string]any)["details"].(map[string]any)["welcome_remaining"] != float64(0) {
		t.Fatalf("details = %s", blocked.raw)
	}
	_, month := h.todayKeys()
	if h.scalar(`SELECT extra_used FROM usage_monthly WHERE user_id = ? AND usage_month = ?`, s.userID, month) != 2 {
		t.Fatal("extra_used does not count the welcome replies")
	}
}

// Бонус қалғанда квота хабарламасы жоқ; бітсе — бір рет «бітті».
func TestQuotaNoticesWaitForTheWelcomeBonus(t *testing.T) {
	h := newHarness(t, withWelcomeBonus(2))
	s := h.signIn("notice-bonus@example.com")
	h.mustRegister(installation(installID(661), "android", fcmToken(661)), s.access)
	for i := 1; i <= 8; i++ {
		mustStatus(t, h.generate(s.access, "Сәлем"), http.StatusOK, "")
		if low, out := h.notices(s.userID, domain.TypeQuotaLow), h.notices(s.userID, domain.TypeQuotaExhausted); low+out != 0 {
			t.Fatalf("after %d generations with the bonus left: low %d, exhausted %d", i, low, out)
		}
	}
	mustStatus(t, h.generate(s.access, "Соңғы"), http.StatusOK, "")
	if h.notices(s.userID, domain.TypeQuotaExhausted) != 1 || h.notices(s.userID, domain.TypeQuotaLow) != 0 {
		t.Fatal("the exhausted notice did not come once the bonus was spent")
	}
	// The push lives to the end of its day in Almaty (14:00 now), as any quota notice.
	h.tick()
	if out := h.pushesOf(domain.TypeQuotaExhausted); len(out) != 1 || out[0].TTL != 10*time.Hour {
		t.Fatalf("exhausted push = %+v", out)
	}
	mustStatus(t, h.generate(s.access, "Артық"), http.StatusTooManyRequests, "DAILY_LIMIT_REACHED")
	if h.notices(s.userID, domain.TypeQuotaExhausted) != 1 {
		t.Fatal("the exhausted notice came twice")
	}
}

// Науқан аудиториясының квота сүзгісі бонус пен кредиттерді қосып санайды.
func TestAudienceQuotaFilterCountsCredits(t *testing.T) {
	h := newHarness(t)
	headers := h.signInAdmin().headers(h.cfg.Admin.CookieName)
	spent := h.signIn("audience-spent@example.com")
	last := h.signIn("audience-last@example.com")
	plenty := h.signIn("audience-plenty@example.com")
	for i, s := range []session{spent, last, plenty} {
		h.mustRegister(installation(installID(671+i), "android", fcmToken(671+i)), s.access)
		h.useQuota(s.userID, 7)
	}
	now := h.clock.Now()
	h.addGrant(last.userID, domain.GrantAdmin, 1, now, nil, now)
	h.addGrant(plenty.userID, domain.GrantAdmin, 30, now, nil, now)

	count := func(quota string) float64 {
		t.Helper()
		res := h.do(http.MethodPost, "/api/v1/admin/notifications/audience/preview",
			map[string]any{"audience": map[string]any{"quota": quota}, "category": "system"}, headers)
		mustStatus(t, res, http.StatusOK, "")
		return res.num("preview", "users")
	}
	if exhausted, near, left := count("exhausted"), count("near_exhaustion"), count("has_remaining"); exhausted != 1 || near != 1 || left != 1 {
		t.Fatalf("exhausted %v, near %v, has_remaining %v; want 1 each", exhausted, near, left)
	}
}

// GET /me/credits: баланс және журнал, жаңасы бірінші; брон мен кредит идентификаторларынсыз.
func TestMeCreditsListsTheLedger(t *testing.T) {
	h := newHarness(t, withWelcomeBonus(3))
	s := h.signIn("+7 705 222 33 05")
	h.useQuota(s.userID, 7)
	mustStatus(t, h.generate(s.access, "Сәлем"), http.StatusOK, "")
	h.provider.err = domain.ErrProviderDown
	mustStatus(t, h.generate(s.access, "Құлады"), http.StatusBadGateway, "AI_PROVIDER_UNAVAILABLE")
	h.provider.err = nil

	res := h.do(http.MethodGet, "/api/v1/me/credits", nil, h.auth(s.access))
	entries, _ := res.body["entries"].([]any)
	if res.status != http.StatusOK || res.num("balance", "welcome", "total") != 3 || res.num("balance", "welcome", "remaining") != 2 ||
		res.num("balance", "credits") != 0 || len(entries) != 4 {
		t.Fatalf("/me/credits: %s", res.raw)
	}
	for i, want := range []struct {
		entry string
		delta float64
	}{{"refund", 1}, {"consume", -1}, {"consume", -1}, {"grant", 3}} {
		e := entries[i].(map[string]any)
		if e["entry"] != want.entry || e["delta"] != want.delta || e["kind"] != "welcome" || e["created_at"] == "" || len(e) != 4 {
			t.Fatalf("entry %d = %v, want %v", i, e, want)
		}
	}
	if balance, _ := res.body["balance"].(map[string]any); balance["pass"] != nil {
		t.Fatalf("pass without a pass: %s", res.raw)
	}
	mustStatus(t, h.do(http.MethodGet, "/api/v1/me/credits", nil, nil), http.StatusUnauthorized, "UNAUTHORIZED")
}
